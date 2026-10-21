package staging

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

func rar4Block(kind byte, flags uint16, body, data []byte) []byte {
	h := []byte{0, 0, kind}
	h = binary.LittleEndian.AppendUint16(h, flags)
	h = binary.LittleEndian.AppendUint16(h, uint16(7+len(body)))
	h = append(h, body...)
	binary.LittleEndian.PutUint16(h, uint16(crc32.ChecksumIEEE(h[2:])))
	return append(h, data...)
}
func rar4File(name string, data []byte, directory bool) []byte {
	b := binary.LittleEndian.AppendUint32(nil, uint32(len(data)))
	b = binary.LittleEndian.AppendUint32(b, uint32(len(data)))
	b = append(b, 3)
	b = binary.LittleEndian.AppendUint32(b, crc32.ChecksumIEEE(data))
	b = binary.LittleEndian.AppendUint32(b, 0)
	b = append(b, 20, 0x30)
	b = binary.LittleEndian.AppendUint16(b, uint16(len(name)))
	flags, attrs := uint16(0x8000), uint32(0100644)
	if directory {
		flags |= 0xe0
		attrs = 0040755
	}
	b = binary.LittleEndian.AppendUint32(b, attrs)
	b = append(b, name...)
	return rar4Block(0x74, flags, b, data)
}
func rar4Book(name string) []byte { return rar4File(name, []byte("synthetic book"), false) }
func rar4Archive(records ...[]byte) []byte {
	b := append([]byte("Rar!\x1a\x07\x00"), rar4Block(0x73, 0, make([]byte, 6), nil)...)
	for _, r := range records {
		b = append(b, r...)
	}
	return append(b, rar4Block(0x7b, 0, nil, nil)...)
}
func rar4Change(record []byte, change func([]byte)) []byte {
	b := bytes.Clone(record)
	change(b)
	end := min(len(b), int(binary.LittleEndian.Uint16(b[5:7])))
	if end >= 7 {
		binary.LittleEndian.PutUint16(b, uint16(crc32.ChecksumIEEE(b[2:end])))
	}
	return b
}

func TestRAR4FramingPartitionsRecordsWithoutReadingPayload(t *testing.T) {
	dir := rar4File("folder", nil, true)
	first := rar4Book("folder/one.epub")
	second := rar4Book("two.pdf")
	data := rar4Archive(dir, first, second)
	files, err := RAR4Accounted(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil || len(files) != 3 || files[0].HeaderOffset != 20 || !files[0].Directory || files[0].Packed != 0 || files[1].HeaderOffset != 20+int64(len(dir)) || files[2].HeaderOffset != files[1].DataOffset+files[1].Packed || files[2].DataOffset+files[2].Packed != int64(len(data)-7) {
		t.Fatal(files, err)
	}
	f := files[1]
	copy(data[f.DataOffset:f.DataOffset+f.Packed], bytes.Repeat([]byte("x"), int(f.Packed)))
	got, err := RAR4Accounted(context.Background(), protectedRARData{bytes.NewReader(data), f.DataOffset, f.DataOffset + f.Packed}, int64(len(data)))
	if err != nil || !reflect.DeepEqual(got, files) || got[1].CRC32 == crc32.ChecksumIEEE(data[f.DataOffset:f.DataOffset+f.Packed]) {
		t.Fatal("framing was confused with integrity", got, err)
	}
}

func TestRAR4KnownDeclarationsAndEmptyContainer(t *testing.T) {
	for _, kind := range []string{"empty", "Windows", "DOS", "Unix permissions", "compressed", "solid", "skip_known", "directory"} {
		t.Run(kind, func(t *testing.T) {
			record := rar4Book("book.epub")
			switch kind {
			case "Windows", "DOS":
				record = rar4Change(record, func(b []byte) {
					b[15] = 2
					if kind == "DOS" {
						b[15] = 0
					}
					binary.LittleEndian.PutUint32(b[28:32], 32)
				})
			case "Unix permissions":
				record = rar4Change(record, func(b []byte) { binary.LittleEndian.PutUint32(b[28:32], 0644) })
			case "compressed":
				record = rar4Change(record, func(b []byte) { b[24], b[25] = 29, 0x35; binary.LittleEndian.PutUint32(b[11:15], 1000) })
			case "solid":
				record = rar4Change(record, func(b []byte) { binary.LittleEndian.PutUint16(b[3:5], 0x8010) })
			case "skip_known":
				record = rar4Change(record, func(b []byte) { binary.LittleEndian.PutUint16(b[3:5], 0xc000) })
			case "directory":
				record = rar4File("folder", nil, true)
			}
			data := rar4Archive(record)
			if kind == "empty" {
				data = rar4Archive()
			}
			if kind == "skip_known" {
				binary.LittleEndian.PutUint16(data[10:12], 0x400c)
				binary.LittleEndian.PutUint16(data[7:9], uint16(crc32.ChecksumIEEE(data[9:20])))
				end := len(data) - 7
				binary.LittleEndian.PutUint16(data[end+3:end+5], 0x4000)
				binary.LittleEndian.PutUint16(data[end:end+2], uint16(crc32.ChecksumIEEE(data[end+2:])))
			}
			files, err := RAR4Accounted(context.Background(), bytes.NewReader(data), int64(len(data)))
			if err != nil || files == nil || (kind != "empty" && len(files) != 1) || (kind == "empty" && len(files) != 0) {
				t.Fatal(files, err)
			}
		})
	}
}

func TestRAR4FramingRejectsUnsupportedMetadataAndSpans(t *testing.T) {
	variants := map[string][]byte{}
	good := rar4Book("book.epub")
	for _, bit := range []uint16{1, 2, 4, 8, 0x100, 0x200, 0x400, 0x800, 0x1000, 0x2000} {
		variants[fmt.Sprintf("file_flag_%x", bit)] = rar4Archive(rar4Change(good, func(b []byte) { binary.LittleEndian.PutUint16(b[3:5], 0x8000|bit) }))
	}
	variants["no_long_block"] = rar4Archive(rar4Change(good, func(b []byte) { binary.LittleEndian.PutUint16(b[3:5], 0) }))
	for _, kind := range []byte{0x75, 0x76, 0x77, 0x78, 0x79, 0x7a, 0x7c} {
		variants[fmt.Sprintf("block_%x", kind)] = rar4Archive(good, rar4Block(kind, 0xc000, binary.LittleEndian.AppendUint32(nil, 4), []byte("keep")))
	}
	for _, change := range []struct {
		name string
		run  func([]byte)
	}{
		{"host", func(b []byte) { b[15] = 1 }}, {"version_low", func(b []byte) { b[24] = 14 }}, {"version_high", func(b []byte) { b[24] = 41 }},
		{"method_low", func(b []byte) { b[25] = 0x2f }}, {"method_high", func(b []byte) { b[25] = 0x36 }},
		{"stored_size", func(b []byte) { binary.LittleEndian.PutUint32(b[11:15], 999) }},
		{"packed_outside", func(b []byte) { binary.LittleEndian.PutUint32(b[7:11], 0xffffffff) }},
		{"extra", func(b []byte) { binary.LittleEndian.PutUint16(b[26:28], 1) }},
		{"name_empty", func(b []byte) { binary.LittleEndian.PutUint16(b[26:28], 0) }},
		{"header_short", func(b []byte) { binary.LittleEndian.PutUint16(b[5:7], 6) }},
		{"header_outside", func(b []byte) { binary.LittleEndian.PutUint16(b[5:7], 65535) }},
		{"Unix_link", func(b []byte) { binary.LittleEndian.PutUint32(b[28:32], 0120644) }},
		{"Unix_special", func(b []byte) { binary.LittleEndian.PutUint32(b[28:32], 0060644) }},
		{"Unix_high_bits", func(b []byte) { binary.LittleEndian.PutUint32(b[28:32], 0xffffffff) }},
		{"Windows_link", func(b []byte) { b[15] = 2; binary.LittleEndian.PutUint32(b[28:32], 0x400) }},
		{"Windows_directory", func(b []byte) { b[15] = 2; binary.LittleEndian.PutUint32(b[28:32], 16) }},
	} {
		variants[change.name] = rar4Archive(rar4Change(good, change.run))
	}
	variants["directory_data"] = rar4Archive(rar4File("folder", []byte("keep"), true))
	variants["directory_crc"] = rar4Archive(rar4Change(rar4File("folder", nil, true), func(b []byte) { binary.LittleEndian.PutUint32(b[16:20], 1) }))
	variants["directory_method"] = rar4Archive(rar4Change(rar4File("folder", nil, true), func(b []byte) { b[25] = 0x31 }))
	variants["directory_type"] = rar4Archive(rar4Change(rar4File("folder", nil, true), func(b []byte) { binary.LittleEndian.PutUint32(b[28:32], 0100644) }))
	for _, name := range []string{".", "../book.epub", "a/../book.epub", "/book.epub", "a//b.epub", "a\\b.epub", "a:b.epub", "bad\n.epub", "nonascii-\u00e9.epub", "bad\x00.epub"} {
		variants["name_"+name] = rar4Archive(rar4Book(name))
	}
	variants["duplicate"] = rar4Archive(good, good)
	variants["long_name"] = rar4Archive(rar4Book(strings.Repeat("a", 1025)))
	variants["file_prefix"] = rar4Archive(rar4Book("folder"), rar4Book("folder/book.epub"))
	variants["reverse_file_prefix"] = rar4Archive(rar4Book("folder/book.epub"), rar4Book("folder"))
	variants["repeated_main"] = rar4Archive(rar4Block(0x73, 0, make([]byte, 6), nil))
	variants["missing_main"] = append(append([]byte("Rar!\x1a\x07\x00"), good...), rar4Block(0x7b, 0, nil, nil)...)
	variants["tail"] = append(rar4Archive(good), []byte("keep")...)
	for _, bit := range []uint16{1, 2, 0x10, 0x20, 0x40, 0x80, 0x100, 0x200, 0x8000} {
		data := rar4Archive(good)
		main := rar4Change(data[7:20], func(b []byte) { binary.LittleEndian.PutUint16(b[3:5], bit) })
		copy(data[7:20], main)
		variants[fmt.Sprintf("main_flag_%x", bit)] = data
	}
	reserved := rar4Archive(good)
	main := rar4Change(reserved[7:20], func(b []byte) { b[7] = 1 })
	copy(reserved[7:20], main)
	variants["main_reserved"] = reserved
	for _, flags := range []uint16{1, 2, 4, 8, 0x8000} {
		data := rar4Archive(good)
		end := rar4Change(data[len(data)-7:], func(b []byte) { binary.LittleEndian.PutUint16(b[3:5], flags) })
		copy(data[len(data)-7:], end)
		variants[fmt.Sprintf("end_flag_%x", flags)] = data
	}
	for name, data := range variants {
		t.Run(name, func(t *testing.T) {
			files, err := RAR4Accounted(context.Background(), bytes.NewReader(data), int64(len(data)))
			if !errors.Is(err, ErrRAR4Coverage) || files != nil {
				t.Fatal("unsupported material granted framing evidence", files, err)
			}
		})
	}
}

func TestRAR4EveryTruncationAndHeaderChecksum(t *testing.T) {
	data := rar4Archive(rar4Book("book.epub"))
	for n := 0; n < len(data); n++ {
		if files, err := RAR4Accounted(context.Background(), bytes.NewReader(data[:n]), int64(n)); err == nil || files != nil {
			t.Fatal("truncation", n, files, err)
		}
	}
	for _, offset := range []int{0, 7, 9, 20, 22, len(data) - 7, len(data) - 5} {
		b := bytes.Clone(data)
		b[offset] ^= 1
		if files, err := RAR4Accounted(context.Background(), bytes.NewReader(b), int64(len(b))); err == nil || files != nil {
			t.Fatal("checksum/signature", offset, files, err)
		}
	}
}

func TestRAR4BoundsCancellationAndSharedFailedWork(t *testing.T) {
	data := rar4Archive(rar4Book("book.epub"))
	for _, size := range []int64{-1, 4<<30 + 1} {
		if files, err := RAR4Accounted(context.Background(), bytes.NewReader(data), size); err == nil || files != nil {
			t.Fatal(size, files, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if files, err := RAR4Accounted(ctx, bytes.NewReader(data), int64(len(data))); !errors.Is(err, context.Canceled) || files != nil {
		t.Fatal(files, err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	if files, err := RAR4Accounted(ctx, cancelZIPRead{bytes.NewReader(data), cancel}, int64(len(data))); !errors.Is(err, context.Canceled) || files != nil {
		t.Fatal(files, err)
	}
	limits := Defaults()
	limits.tree = &treeBudget{reads: allowance{remaining: 32}, proofs: allowance{remaining: 15}}
	if files, err := RAR4Accounted(context.Background(), limits.proofReader(bytes.NewReader(data)), int64(len(data))); !errors.Is(err, errTreeBudget) || files != nil || limits.tree.proofs.left() != 0 || limits.tree.reads.left() != 18 {
		t.Fatal(files, err, limits.tree.proofs.left(), limits.tree.reads.left())
	}
	if _, err := RAR4Accounted(context.Background(), limits.proofReader(bytes.NewReader(data)), int64(len(data))); !errors.Is(err, errTreeBudget) || limits.tree.proofs.left() != 0 || limits.tree.reads.left() != 18 {
		t.Fatal("failed work refunded", err)
	}
	var records [][]byte
	for n := 0; n < 10001; n++ {
		records = append(records, rar4File(fmt.Sprintf("book-%d.epub", n), nil, false))
	}
	for _, count := range []int{10000, 10001} {
		b := rar4Archive(records[:count]...)
		files, err := RAR4Accounted(context.Background(), bytes.NewReader(b), int64(len(b)))
		if count == 10000 && (err != nil || len(files) != count) || count == 10001 && (err == nil || files != nil) {
			t.Fatal(count, len(files), err)
		}
	}
}

func FuzzRAR4Accounted(f *testing.F) {
	f.Add(rar4Archive(rar4Book("book.epub")))
	f.Add(rar4Archive())
	f.Add([]byte("Rar!\x1a\x07\x00"))
	f.Fuzz(func(t *testing.T, data []byte) { checkRAR4Fuzz(t, data) })
}
func FuzzRAR4FileHeaders(f *testing.F) {
	f.Add(rar4Book("book.epub"))
	f.Add(rar4File("folder", nil, true))
	f.Fuzz(func(t *testing.T, record []byte) {
		if len(record) < 7 || len(record) > 1<<20 {
			return
		}
		record = bytes.Clone(record)
		size := int(binary.LittleEndian.Uint16(record[5:7]))
		if size >= 7 && size <= len(record) {
			binary.LittleEndian.PutUint16(record, uint16(crc32.ChecksumIEEE(record[2:size])))
		}
		checkRAR4Fuzz(t, rar4Archive(record))
	})
}
func checkRAR4Fuzz(t *testing.T, data []byte) {
	t.Helper()
	if len(data) > 4<<20 {
		return
	}
	files, err := RAR4Accounted(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		if files != nil {
			t.Fatal("partial evidence escaped failure")
		}
		return
	}
	last := int64(20)
	for _, file := range files {
		if file.HeaderOffset < last || file.DataOffset <= file.HeaderOffset || file.Packed < 0 || file.DataOffset > int64(len(data))-file.Packed {
			t.Fatal("invalid partition", file)
		}
		last = file.DataOffset + file.Packed
	}
	if last != int64(len(data)-7) {
		t.Fatal("partition did not reach end", last, len(data))
	}
}

type failingRAR4Reader struct {
	source        *bytes.Reader
	failAt, calls int
}

func (r *failingRAR4Reader) ReadAt(p []byte, off int64) (int, error) {
	r.calls++
	if r.calls == r.failAt {
		n, _ := r.source.ReadAt(p[:min(2, len(p))], off)
		return n, io.ErrUnexpectedEOF
	}
	return r.source.ReadAt(p, off)
}
func TestRAR4FailedReadsNeverReturnPartialEvidence(t *testing.T) {
	data := rar4Archive(rar4Book("book.epub"))
	for target := 1; target <= 6; target++ {
		r := &failingRAR4Reader{source: bytes.NewReader(data), failAt: target}
		files, err := RAR4Accounted(context.Background(), r, int64(len(data)))
		if !errors.Is(err, ErrRAR4Coverage) || !errors.Is(err, io.ErrUnexpectedEOF) || files != nil || r.calls != target {
			t.Fatal(target, files, err, r.calls)
		}
	}
}
func TestRAR4ReadOnlyFramingDoesNotEnableExtractionCoverage(t *testing.T) {
	src, dst := sevenFixture(t, []sevenMember{{name: "book.epub", body: []byte("synthetic book"), packed: "14"}})
	data := rar4Archive(rar4Book("book.epub"))
	if err := src.WriteFile("bundle.rar", data, 0600); err != nil {
		t.Fatal(err)
	}
	writeFakeRARFile(t, "wait", "1")
	limits := Defaults()
	limits.Coverage = true
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	x, err := Extract7z(ctx, src, "bundle.rar", dst, books, limits)
	if err == nil || !strings.Contains(err.Error(), "RAR container bytes are not completely accounted for") || ctx.Err() != nil || x.Whole || len(x.Members) != 0 {
		t.Fatal("read-only facts granted coverage", x, err, ctx.Err())
	}
	got, err := src.ReadFile("bundle.rar")
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("source changed", err)
	}
}
