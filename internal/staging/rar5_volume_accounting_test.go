package staging

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"reflect"
	"testing"
)

func TestRAR5VolumeFactsAndCompleteChainUseDifferentCRCs(t *testing.T) {
	whole := []byte("one complete stored member continued through three physical volumes")
	s := rar5SetBinding{}
	for i := range 3 {
		piece := whole[i*len(whole)/3 : (i+1)*len(whole)/3]
		b := rar5TestVolume(i, 3, rar5TestSplit("book.epub", whole, piece, i > 0, i < 2, 0))
		v, err := RAR5VolumeAccounted(context.Background(), bytes.NewReader(b), int64(len(b)))
		if err != nil || v.Ordinal != i || v.More != (i < 2) || len(v.Files) != 1 {
			t.Fatal(v, err)
		}
		f := v.Files[0]
		wantCRC := crc32.ChecksumIEEE(whole)
		if i < 2 {
			wantCRC = crc32.ChecksumIEEE(piece)
		}
		if f.Bytes != int64(len(whole)) || f.Packed != int64(len(piece)) || f.CRC32 != wantCRC || f.SplitBefore != (i > 0) || f.SplitAfter != (i < 2) || f.HeaderOffset < 8 || f.DataOffset+f.Packed >= int64(len(b)) {
			t.Fatal(f)
		}
		if err := s.add(context.Background(), v, i, 3, Defaults().Members); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.files) != 1 || s.files[0].Packed != int64(len(whole)) || s.files[0].CRC32 != crc32.ChecksumIEEE(whole) || !reflect.DeepEqual(s.starts, []int{0}) {
		t.Fatal(s)
	}
}

func TestRAR5VolumeFramingRejectsUnsupportedPhysicalBytes(t *testing.T) {
	whole := []byte("whole member")
	file := rar5TestSplit("book.epub", whole, whole[:4], false, true, 0)
	volume := func(main, record, end []byte) []byte {
		b := append([]byte("Rar!\x1a\x07\x01\x00"), main...)
		b = append(b, record...)
		return append(b, end...)
	}
	main, end := rar5Record(1, 0, rar5Numbers(1), nil), rar5Record(5, 0, rar5Numbers(1), nil)
	for name, b := range map[string][]byte{
		"not_volume":             volume(rar5Record(1, 0, rar5Numbers(0), nil), file, end),
		"explicit_first_ordinal": volume(rar5Record(1, 0, rar5Numbers(3, 0), nil), file, end),
		"ordinal_limit":          volume(rar5Record(1, 0, rar5Numbers(3, 1000), nil), file, end),
		"ordinal_without_volume": volume(rar5Record(1, 0, rar5Numbers(2, 1), nil), file, end),
		"unknown_main_flag":      volume(rar5Record(1, 0, rar5Numbers(9), nil), file, end),
		"main_data":              volume(rar5Record(1, 2, rar5Numbers(1), []byte("x")), file, end),
		"main_service":           volume(main, append(file, rar5Record(3, 4, nil, nil)...), end),
		"encryption":             volume(main, rar5Record(4, 0, nil, nil), end),
		"unknown_block":          volume(main, rar5Record(99, 4, nil, nil), end),
		"unknown_end_flag":       volume(main, file, rar5Record(5, 0, rar5Numbers(2), nil)),
		"end_data":               volume(main, file, rar5Record(5, 2, rar5Numbers(1), []byte("x"))),
		"end_extra":              volume(main, file, rar5RecordExtra(5, 1, rar5Numbers(1), []byte("x"), nil)),
		"duplicate_main":         volume(main, append(main, file...), end),
		"duplicate_file":         volume(main, append(file, file...), end),
		"tail":                   append(volume(main, file, end), 0),
		"missing_end":            volume(main, file, nil),
		"directory_split":        rar5TestVolume(0, 2, rar5Record(2, 18, rar5FileBody("folder", nil, 5, 0040755, 0, 1), nil)),
		"unknown_size":           rar5TestVolume(0, 2, rar5Record(2, 18, rar5FileBody("book.epub", whole, 12, 0100644, 0, 1), whole)),
		"dependent_file":         rar5TestVolume(0, 2, rar5Record(2, 50, rar5FileBody("book.epub", whole, 4, 0100644, 0, 1), whole)),
	} {
		t.Run(name, func(t *testing.T) {
			v, err := RAR5VolumeAccounted(context.Background(), bytes.NewReader(b), int64(len(b)))
			if !errors.Is(err, ErrRAR5Coverage) || len(v.Files) != 0 || v.Ordinal != 0 || v.More {
				t.Fatal("partial evidence escaped refusal", v, err)
			}
		})
	}
	valid := rar5TestVolume(0, 2, file)
	for end := range len(valid) {
		if v, err := RAR5VolumeAccounted(context.Background(), bytes.NewReader(valid[:end]), int64(end)); err == nil || len(v.Files) != 0 {
			t.Fatal("truncation accepted", end, v, err)
		}
	}
	for i := range len(valid) {
		mutated := append([]byte{}, valid...)
		mutated[i] ^= 128
		// Payload is deliberately not authenticated by the framing accountant.
		v, err := RAR5VolumeAccounted(context.Background(), bytes.NewReader(mutated), int64(len(mutated)))
		if err == nil {
			f := v.Files[0]
			if int64(i) < f.DataOffset || int64(i) >= f.DataOffset+f.Packed {
				t.Fatal("header mutation accepted", i, v)
			}
		}
	}
}

func TestRAR5SetAggregationRefusesContradictoryContinuations(t *testing.T) {
	whole := []byte("abcdef")
	firstBytes := rar5TestVolume(0, 2, rar5TestSplit("book.epub", whole, whole[:3], false, true, 0))
	first, err := RAR5VolumeAccounted(context.Background(), bytes.NewReader(firstBytes), int64(len(firstBytes)))
	if err != nil {
		t.Fatal(err)
	}
	b := rar5TestVolume(1, 2, rar5TestSplit("book.epub", whole, whole[3:], true, false, 0))
	last, err := RAR5VolumeAccounted(context.Background(), bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*RAR5Volume){
		"ordinal": func(v *RAR5Volume) { v.Ordinal = 0 }, "end": func(v *RAR5Volume) { v.More = true }, "solid": func(v *RAR5Volume) { v.Solid = true },
		"missing_before": func(v *RAR5Volume) { v.Files[0].SplitBefore = false }, "extra_after": func(v *RAR5Volume) { v.Files[0].SplitAfter = true },
		"name": func(v *RAR5Volume) { v.Files[0].Name = "other.epub" }, "whole_size": func(v *RAR5Volume) { v.Files[0].Bytes++ },
		"compression": func(v *RAR5Volume) { v.Files[0].Compression = 128 }, "host": func(v *RAR5Volume) { v.Files[0].HostOS = 0 },
		"attributes": func(v *RAR5Volume) { v.Files[0].Attributes = 0100600 }, "time": func(v *RAR5Volume) { v.Files[0].metadata.modified++ },
		"time_flags": func(v *RAR5Volume) { v.Files[0].metadata.flags ^= 2 }, "extra": func(v *RAR5Volume) { v.Files[0].metadata.extra[0] ^= 1 },
		"stored_sum": func(v *RAR5Volume) { v.Files[0].Packed-- }, "overflow": func(v *RAR5Volume) { v.Files[0].Packed = maxArchiveSnapshot },
		"directory": func(v *RAR5Volume) { v.Files[0].Directory = true }, "empty": func(v *RAR5Volume) { v.Files = nil },
	} {
		t.Run(name, func(t *testing.T) {
			s := rar5SetBinding{}
			if err := s.add(context.Background(), first, 0, 2, 10); err != nil {
				t.Fatal(err)
			}
			v := last
			v.Files = append([]RAR5VolumeFile{}, last.Files...)
			change(&v)
			if err := s.add(context.Background(), v, 1, 2, 10); err == nil {
				t.Fatal("contradictory continuation accepted", s)
			}
		})
	}
}

func TestRAR5SetCompleteSiblingsDirectoriesAndCompressionFacts(t *testing.T) {
	whole := []byte("abcdef")
	for _, compression := range []uint64{0, 0x280} {
		t.Run(fmt.Sprint(compression), func(t *testing.T) {
			prefix := rar5Record(2, 2, rar5FileBody("preface.epub", []byte("p"), 4, 0100644, 0, 1), []byte("p"))
			dir := rar5Record(2, 2, rar5FileBody("folder", nil, 5, 0040755, 0, 1), nil)
			suffix := rar5Record(2, 2, rar5FileBody("after.epub", []byte("q"), 4, 0100644, 0, 1), []byte("q"))
			parts := [][]byte{rar5TestVolume(0, 2, prefix, dir, rar5TestSplit("folder/book.epub", whole, whole[:3], false, true, compression)), rar5TestVolume(1, 2, rar5TestSplit("folder/book.epub", whole, whole[3:], true, false, compression), suffix)}
			s := rar5SetBinding{}
			for i, b := range parts {
				v, err := RAR5VolumeAccounted(context.Background(), bytes.NewReader(b), int64(len(b)))
				if err != nil {
					t.Fatal(err)
				}
				if err := s.add(context.Background(), v, i, 2, 4); err != nil {
					t.Fatal(err)
				}
			}
			if len(s.files) != 4 || !reflect.DeepEqual(s.starts, []int{0, 0, 0, 1}) || !s.files[1].Directory || s.files[2].CRC32 != crc32.ChecksumIEEE(whole) {
				t.Fatal(s)
			}
		})
	}
}

func TestRAR5VolumeSharesBudgetsAndCancelsWithoutFacts(t *testing.T) {
	b := rar5TestVolume(0, 2, rar5TestSplit("book.epub", []byte("abcdef"), []byte("abc"), false, true, 0))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if v, err := RAR5VolumeAccounted(ctx, bytes.NewReader(b), int64(len(b))); !errors.Is(err, context.Canceled) || len(v.Files) != 0 {
		t.Fatal(v, err)
	}
	l := rar5SetLimits()
	l.tree = newTreeBudget(l)
	l.tree.proofs.remaining = 100
	if _, err := RAR5VolumeAccounted(context.Background(), l.proofReader(bytes.NewReader(b)), int64(len(b))); err != nil {
		t.Fatal(err)
	}
	remaining := l.tree.proofs.left()
	if remaining >= 100 || remaining <= 0 {
		t.Fatal(remaining)
	}
	if v, err := RAR5VolumeAccounted(context.Background(), l.proofReader(bytes.NewReader(b)), int64(len(b))); !errors.Is(err, errTreeBudget) || len(v.Files) != 0 || l.tree.proofs.left() != 0 {
		t.Fatal(v, err, l.tree.proofs.left())
	}
	before := l.tree.reads.left()
	if _, err := RAR5VolumeAccounted(context.Background(), l.proofReader(bytes.NewReader(b)), int64(len(b))); !errors.Is(err, errTreeBudget) || l.tree.reads.left() != before {
		t.Fatal(err)
	}
	s := rar5SetBinding{}
	v, err := RAR5VolumeAccounted(context.Background(), bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.add(ctx, v, 0, 2, 10); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if rar5VolumeTableMatches(ctx, v, nil) || s.matchesAssembled(ctx, nil) {
		t.Fatal("cancelled match succeeded")
	}
}

func TestRAR5SetMayCloseOneMemberAndStartAnotherInMiddleVolume(t *testing.T) {
	a, b := []byte("first whole book"), []byte("second whole book")
	parts := [][]byte{
		rar5TestVolume(0, 3, rar5TestSplit("first.epub", a, a[:4], false, true, 0)),
		rar5TestVolume(1, 3, rar5TestSplit("first.epub", a, a[4:], true, false, 0), rar5TestSplit("second.epub", b, b[:5], false, true, 0)),
		rar5TestVolume(2, 3, rar5TestSplit("second.epub", b, b[5:], true, false, 0)),
	}
	s := rar5SetBinding{}
	for i, part := range parts {
		v, err := RAR5VolumeAccounted(context.Background(), bytes.NewReader(part), int64(len(part)))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.add(context.Background(), v, i, 3, 2); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.files) != 2 || !reflect.DeepEqual(s.starts, []int{0, 1}) || s.files[0].Packed != int64(len(a)) || s.files[1].Packed != int64(len(b)) || s.files[0].CRC32 != crc32.ChecksumIEEE(a) || s.files[1].CRC32 != crc32.ChecksumIEEE(b) {
		t.Fatal(s)
	}
	for _, suffix := range []string{"before.epub", "before.epub/child.epub"} {
		t.Run(suffix, func(t *testing.T) {
			prefix := rar5Record(2, 2, rar5FileBody("before.epub", []byte("p"), 4, 0100644, 0, 1), []byte("p"))
			last := rar5Record(2, 2, rar5FileBody(suffix, []byte("q"), 4, 0100644, 0, 1), []byte("q"))
			first := rar5TestVolume(0, 2, prefix, rar5TestSplit("book.epub", a, a[:4], false, true, 0))
			second := rar5TestVolume(1, 2, rar5TestSplit("book.epub", a, a[4:], true, false, 0), last)
			s := rar5SetBinding{}
			for i, part := range [][]byte{first, second} {
				v, err := RAR5VolumeAccounted(context.Background(), bytes.NewReader(part), int64(len(part)))
				if err != nil {
					t.Fatal(err)
				}
				err = s.add(context.Background(), v, i, 2, 3)
				if (i == 0 && err != nil) || (i == 1 && err == nil) {
					t.Fatal("cross-volume selection alias", i, err)
				}
			}
		})
	}
}

func FuzzRAR5VolumeAccounted(f *testing.F) {
	f.Add(rar5TestVolume(0, 2, rar5TestSplit("book.epub", []byte("abcdef"), []byte("abc"), false, true, 0)))
	f.Add(rar5TestVolume(1, 2, rar5TestSplit("book.epub", []byte("abcdef"), []byte("def"), true, false, 0)))
	f.Add([]byte("not a RAR volume"))
	f.Fuzz(func(t *testing.T, b []byte) {
		v, err := RAR5VolumeAccounted(context.Background(), bytes.NewReader(b), int64(len(b)))
		if err != nil {
			return
		}
		if len(v.Files) > 10000 || v.Ordinal < 0 || v.Ordinal >= 1000 {
			t.Fatal(v)
		}
		var end int64 = 8
		for _, file := range v.Files {
			if file.HeaderOffset < end || file.DataOffset <= file.HeaderOffset || file.Packed < 0 || file.DataOffset+file.Packed > int64(len(b)) {
				t.Fatal(file)
			}
			end = file.DataOffset + file.Packed
		}
		if files, err := RAR5Accounted(context.Background(), bytes.NewReader(b), int64(len(b))); err == nil || files != nil {
			t.Fatal("volume acquired standalone authority")
		}
	})
}
