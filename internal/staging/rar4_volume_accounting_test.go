package staging

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"reflect"
	"testing"
)

func rar4TestVolume(index, count int, reserved bool, records ...[]byte) []byte {
	flags := uint16(0x11)
	if index == 0 {
		flags |= 0x100
	}
	b := append([]byte("Rar!\x1a\x07\x00"), rar4Block(0x73, flags, make([]byte, 6), nil)...)
	for _, r := range records {
		b = append(b, r...)
	}
	flags = 8
	if index+1 < count {
		flags |= 1
	}
	end := binary.LittleEndian.AppendUint16(nil, uint16(index))
	if reserved {
		flags |= 4
		end = append(end, make([]byte, 7)...)
	}
	return append(b, rar4Block(0x7b, flags, end, nil)...)
}

func rar4TestSplit(name string, whole, packed []byte, before, after bool, method byte) []byte {
	return rar4Change(rar4File(name, packed, false), func(h []byte) {
		flags := binary.LittleEndian.Uint16(h[3:])
		if before {
			flags |= 1
		}
		if after {
			flags |= 2
		}
		binary.LittleEndian.PutUint16(h[3:], flags)
		binary.LittleEndian.PutUint32(h[11:], uint32(len(whole)))
		crc := crc32.ChecksumIEEE(whole)
		if after {
			crc = crc32.ChecksumIEEE(packed)
		}
		binary.LittleEndian.PutUint32(h[16:], crc)
		h[25] = method
		if method != 0x30 {
			h[24] = 29
		}
	})
}

func TestRAR4PhysicalVolumeFactsAndCompleteChain(t *testing.T) {
	whole := []byte("exact independently generated whole member split over adjacent physical volumes")
	for _, count := range []int{2, 3, 7} {
		for _, reserved := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/%t", count, reserved), func(t *testing.T) {
				s := rar4SetBinding{}
				for i := range count {
					piece := whole[i*len(whole)/count : (i+1)*len(whole)/count]
					b := rar4TestVolume(i, count, reserved, rar4TestSplit("book.epub", whole, piece, i > 0, i+1 < count, 0x30))
					v, err := RAR4VolumeAccounted(context.Background(), bytes.NewReader(b), int64(len(b)))
					if err != nil || v.Ordinal != i || v.First != (i == 0) || v.More != (i+1 < count) || len(v.Files) != 1 {
						t.Fatal(v, err)
					}
					f := v.Files[0]
					want := crc32.ChecksumIEEE(whole)
					if i+1 < count {
						want = crc32.ChecksumIEEE(piece)
					}
					if f.HeaderOffset != 20 || f.Bytes != int64(len(whole)) || f.Packed != int64(len(piece)) || f.CRC32 != want || f.SplitBefore != (i > 0) || f.SplitAfter != (i+1 < count) {
						t.Fatal(f)
					}
					got, err := RAR4VolumeAccounted(context.Background(), protectedRARData{bytes.NewReader(b), f.DataOffset, f.DataOffset + f.Packed}, int64(len(b)))
					if err != nil || !reflect.DeepEqual(v, got) {
						t.Fatal("framing read payload", got, err)
					}
					if _, err := RAR4Accounted(context.Background(), bytes.NewReader(b), int64(len(b))); !errors.Is(err, ErrRAR4Coverage) {
						t.Fatal("standalone contract broadened", err)
					}
					if err := s.add(context.Background(), v, i, count, 10); err != nil {
						t.Fatal(err)
					}
				}
				if len(s.files) != 1 || s.files[0].Packed != int64(len(whole)) || s.files[0].CRC32 != crc32.ChecksumIEEE(whole) || !reflect.DeepEqual(s.starts, []int{0}) {
					t.Fatal(s)
				}
			})
		}
	}
}

func TestRAR4VolumeRefusesUnaccountedPhysicalRecords(t *testing.T) {
	whole := []byte("whole content")
	file := rar4TestSplit("book.epub", whole, whole[:5], false, true, 0x30)
	main := rar4Block(0x73, 0x111, make([]byte, 6), nil)
	end := rar4Block(0x7b, 9, []byte{0, 0}, nil)
	volume := func(m, f, e []byte) []byte {
		b := append([]byte("Rar!\x1a\x07\x00"), m...)
		b = append(b, f...)
		return append(b, e...)
	}
	for name, b := range map[string][]byte{
		"not_volume":       volume(rar4Block(0x73, 0, make([]byte, 6), nil), file, end),
		"old_naming":       volume(rar4Block(0x73, 0x101, make([]byte, 6), nil), file, end),
		"missing_first":    volume(rar4Block(0x73, 0x11, make([]byte, 6), nil), file, end),
		"encrypted":        volume(rar4Block(0x73, 0x191, make([]byte, 6), nil), file, end),
		"main_reserved":    volume(rar4Block(0x73, 0x111, []byte{0, 0, 0, 0, 1, 0}, nil), file, end),
		"duplicate_main":   volume(main, append(bytes.Clone(main), file...), end),
		"service":          volume(main, append(bytes.Clone(file), rar4Block(0x7a, 0x8000, binary.LittleEndian.AppendUint32(nil, 4), []byte("keep"))...), end),
		"recovery":         volume(main, append(bytes.Clone(file), rar4Block(0x78, 0, nil, nil)...), end),
		"comment":          volume(main, append(bytes.Clone(file), rar4Block(0x75, 0, nil, nil)...), end),
		"unknown":          volume(main, rar4Block(0x7c, 0, nil, nil), end),
		"duplicate_member": volume(main, append(bytes.Clone(file), file...), end),
		"unknown_size":     volume(main, rar4Change(file, func(h []byte) { binary.LittleEndian.PutUint32(h[11:], 0xffffffff) }), end),
		"salt":             volume(main, rar4Change(file, func(h []byte) { binary.LittleEndian.PutUint16(h[3:], 0x8402) }), end),
		"directory_data":   volume(main, rar4File("folder", []byte("x"), true), end),
		"split_directory":  volume(main, rar4Change(rar4File("folder", nil, true), func(h []byte) { binary.LittleEndian.PutUint16(h[3:], 0x80e2) }), end),
		"volume_crc":       volume(main, file, rar4Block(0x7b, 11, []byte{0, 0, 0, 0, 0, 0}, nil)),
		"missing_ordinal":  volume(main, file, rar4Block(0x7b, 1, nil, nil)),
		"wrong_first":      volume(main, file, rar4Block(0x7b, 9, []byte{1, 0}, nil)),
		"ordinal_limit":    volume(rar4Block(0x73, 0x11, make([]byte, 6), nil), file, rar4Block(0x7b, 9, []byte{0xe8, 3}, nil)),
		"unknown_end":      volume(main, file, rar4Block(0x7b, 25, []byte{0, 0}, nil)),
		"short_reserved":   volume(main, file, rar4Block(0x7b, 13, make([]byte, 8), nil)),
		"nonzero_reserved": volume(main, file, rar4Block(0x7b, 13, []byte{0, 0, 0, 0, 0, 0, 0, 0, 1}, nil)),
		"tail":             append(volume(main, file, end), 0),
		"missing_end":      volume(main, file, nil),
	} {
		t.Run(name, func(t *testing.T) {
			v, err := RAR4VolumeAccounted(context.Background(), bytes.NewReader(b), int64(len(b)))
			if !errors.Is(err, ErrRAR4Coverage) || !reflect.DeepEqual(v, RAR4Volume{}) {
				t.Fatal("partial facts escaped refusal", v, err)
			}
		})
	}
	valid := volume(main, file, end)
	for stop := range len(valid) {
		v, err := RAR4VolumeAccounted(context.Background(), bytes.NewReader(valid[:stop]), int64(stop))
		if err == nil || !reflect.DeepEqual(v, RAR4Volume{}) {
			t.Fatal("truncation accepted", stop, v, err)
		}
	}
	for i := range len(valid) {
		b := bytes.Clone(valid)
		b[i] ^= 128
		v, err := RAR4VolumeAccounted(context.Background(), bytes.NewReader(b), int64(len(b)))
		if err == nil {
			f := v.Files[0]
			if int64(i) < f.DataOffset || int64(i) >= f.DataOffset+f.Packed {
				t.Fatal("header mutation accepted", i, v)
			}
		}
	}
}

func TestRAR4SetContinuationsBindAllSupportedMetadata(t *testing.T) {
	whole := []byte("abcdef")
	var volumes []RAR4Volume
	for i := range 2 {
		b := rar4TestVolume(i, 2, false, rar4TestSplit("book.epub", whole, whole[i*3:(i+1)*3], i > 0, i == 0, 0x30))
		v, err := RAR4VolumeAccounted(context.Background(), bytes.NewReader(b), int64(len(b)))
		if err != nil {
			t.Fatal(err)
		}
		volumes = append(volumes, v)
	}
	for name, change := range map[string]func(*RAR4Volume){
		"ordinal": func(v *RAR4Volume) { v.Ordinal = 0 }, "end": func(v *RAR4Volume) { v.More = true }, "solid": func(v *RAR4Volume) { v.Solid = true },
		"first": func(v *RAR4Volume) { v.First = true },
		"name":  func(v *RAR4Volume) { v.Files[0].Name = "other.epub" }, "before": func(v *RAR4Volume) { v.Files[0].SplitBefore = false }, "after": func(v *RAR4Volume) { v.Files[0].SplitAfter = true },
		"size": func(v *RAR4Volume) { v.Files[0].Bytes++ }, "stored_size": func(v *RAR4Volume) { v.Files[0].Packed-- }, "overflow": func(v *RAR4Volume) { v.Files[0].Packed = maxArchiveSnapshot },
		"host": func(v *RAR4Volume) { v.Files[0].HostOS = 2 }, "method": func(v *RAR4Volume) { v.Files[0].CompressionMethod = 0x35 }, "version": func(v *RAR4Volume) { v.Files[0].UnpackVersion = 29 },
		"attribute": func(v *RAR4Volume) { v.Files[0].Attributes ^= 1 }, "time": func(v *RAR4Volume) { v.Files[0].ModifiedDOS++ }, "extra": func(v *RAR4Volume) { v.Files[0].metadata.extra[0] ^= 1 }, "flags": func(v *RAR4Volume) { v.Files[0].metadata.flags ^= 0x20 },
		"directory": func(v *RAR4Volume) { v.Files[0].Directory = true }, "empty": func(v *RAR4Volume) { v.Files = nil },
	} {
		t.Run(name, func(t *testing.T) {
			s := rar4SetBinding{}
			if err := s.add(context.Background(), volumes[0], 0, 2, 10); err != nil {
				t.Fatal(err)
			}
			v := volumes[1]
			v.Files = append([]RAR4VolumeFile{}, v.Files...)
			change(&v)
			if err := s.add(context.Background(), v, 1, 2, 10); err == nil {
				t.Fatal("contradictory continuation accepted", s)
			}
		})
	}
}

func FuzzRAR4VolumeAccounted(f *testing.F) {
	f.Add(rar4TestVolume(0, 2, false, rar4TestSplit("book.epub", []byte("abcdef"), []byte("abc"), false, true, 0x30)))
	f.Add(rar4TestVolume(1, 2, true, rar4TestSplit("book.epub", []byte("abcdef"), []byte("def"), true, false, 0x30)))
	f.Add(rar4Archive(rar4Book("book.epub")))
	f.Fuzz(func(t *testing.T, b []byte) {
		v, err := RAR4VolumeAccounted(context.Background(), bytes.NewReader(b), int64(len(b)))
		if err != nil {
			if !reflect.DeepEqual(v, RAR4Volume{}) {
				t.Fatal("partial facts on error", v)
			}
			return
		}
		if v.Ordinal < 0 || v.Ordinal >= 1000 || v.First != (v.Ordinal == 0) {
			t.Fatal(v)
		}
		at := int64(20)
		for _, p := range v.Files {
			if p.HeaderOffset != at || p.DataOffset < at+33 || p.Packed < 0 || p.Packed > int64(len(b))-p.DataOffset {
				t.Fatal(p)
			}
			at = p.DataOffset + p.Packed
		}
		if at != int64(len(b)-9) && at != int64(len(b)-16) {
			t.Fatal("unpartitioned bytes", at, len(b))
		}
	})
}

func TestRAR4VolumeKnownMetadataStillBindsContinuations(t *testing.T) {
	whole := []byte("abcdef")
	for _, flags := range []uint16{0x100, 0x1000, 0x1100} {
		t.Run(fmt.Sprint(flags), func(t *testing.T) {
			s := rar4SetBinding{}
			for i := range 2 {
				var times []byte
				if flags&0x1000 != 0 {
					times = rar4Times(0xfff0)
				}
				piece := whole[i*3 : (i+1)*3]
				r := rar4MetadataRecord("book.epub", piece, flags, times, false)
				r = rar4Change(r, func(h []byte) {
					f := binary.LittleEndian.Uint16(h[3:])
					if i == 0 {
						f |= 2
					} else {
						f |= 1
					}
					binary.LittleEndian.PutUint16(h[3:], f)
					binary.LittleEndian.PutUint32(h[11:], uint32(len(whole)))
					if i == 1 {
						binary.LittleEndian.PutUint32(h[16:], crc32.ChecksumIEEE(whole))
					}
				})
				b := rar4TestVolume(i, 2, true, r)
				v, err := RAR4VolumeAccounted(context.Background(), bytes.NewReader(b), int64(len(b)))
				if err != nil {
					t.Fatal(v, err)
				}
				if err := s.add(context.Background(), v, i, 2, 10); err != nil {
					t.Fatal(err)
				}
			}
			if len(s.files) != 1 || s.files[0].Bytes != int64(len(whole)) || s.files[0].Packed != int64(len(whole)) {
				t.Fatal(s)
			}
		})
	}
}

func TestRAR4VolumeBoundsFailedReadsAndCancellationReturnNoFacts(t *testing.T) {
	b := rar4TestVolume(0, 2, false, rar4TestSplit("folder/book.epub", []byte("abcdef"), []byte("abc"), false, true, 0x30))
	for _, size := range []int64{-1, (4 << 30) + 1} {
		v, err := RAR4VolumeAccounted(context.Background(), bytes.NewReader(b), size)
		if err == nil || !reflect.DeepEqual(v, RAR4Volume{}) {
			t.Fatal(v, err)
		}
	}
	for target := 1; target <= 7; target++ {
		r := &failingRAR4Reader{source: bytes.NewReader(b), failAt: target}
		v, err := RAR4VolumeAccounted(context.Background(), r, int64(len(b)))
		if err == nil || !reflect.DeepEqual(v, RAR4Volume{}) {
			t.Fatal(target, v, err)
		}
	}
	inner, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &cancelAtRARNames{Context: inner, cancel: cancel}
	r := armRARNames{bytes.NewReader(b), ctx, int64(len(b))}
	if v, err := RAR4VolumeAccounted(ctx, r, int64(len(b))); !errors.Is(err, context.Canceled) || !errors.Is(err, ErrRAR4Coverage) || !reflect.DeepEqual(v, RAR4Volume{}) {
		t.Fatal(v, err)
	}
	l := Defaults()
	l.tree = &treeBudget{reads: allowance{remaining: 32}, proofs: allowance{remaining: 15}}
	for range 2 {
		v, err := RAR4VolumeAccounted(context.Background(), l.proofReader(bytes.NewReader(b)), int64(len(b)))
		if !errors.Is(err, errTreeBudget) || !reflect.DeepEqual(v, RAR4Volume{}) || l.tree.proofs.left() != 0 || l.tree.reads.left() != 18 {
			t.Fatal(v, err, l.tree.proofs.left(), l.tree.reads.left())
		}
	}
}

func TestRAR4SetCompleteSiblingsDirectoriesAndAdjacentChains(t *testing.T) {
	a, b := []byte("abcdefgh"), []byte("uvwxyz")
	for _, method := range []byte{0x30, 0x35} {
		t.Run(fmt.Sprint(method), func(t *testing.T) {
			parts := [][]byte{
				rar4TestVolume(0, 3, false, rar4File("preface.epub", []byte("p"), false), rar4File("folder", nil, true), rar4TestSplit("folder/a.epub", a, a[:3], false, true, method)),
				rar4TestVolume(1, 3, true, rar4TestSplit("folder/a.epub", a, a[3:], true, false, method), rar4TestSplit("b.epub", b, b[:2], false, true, method)),
				rar4TestVolume(2, 3, false, rar4TestSplit("b.epub", b, b[2:], true, false, method), rar4File("after.pdf", []byte("q"), false)),
			}
			s := rar4SetBinding{}
			for i, p := range parts {
				v, err := RAR4VolumeAccounted(context.Background(), bytes.NewReader(p), int64(len(p)))
				if err != nil {
					t.Fatal(err)
				}
				if err := s.add(context.Background(), v, i, 3, 5); err != nil {
					t.Fatal(err)
				}
			}
			if len(s.files) != 5 || !reflect.DeepEqual(s.starts, []int{0, 0, 0, 1, 2}) || !s.files[1].Directory || s.files[2].Packed != int64(len(a)) || s.files[2].CRC32 != crc32.ChecksumIEEE(a) || s.files[3].Packed != int64(len(b)) || s.files[3].CRC32 != crc32.ChecksumIEEE(b) {
				t.Fatal(s)
			}
		})
	}
}

func TestRAR4SetLateCancellationNeverGrantsBinding(t *testing.T) {
	b := rar4TestVolume(0, 2, false, rar4TestSplit("book.epub", []byte("abcdef"), []byte("abc"), false, true, 0x30))
	v, err := RAR4VolumeAccounted(context.Background(), bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := rar4SetBinding{}
	if err := s.add(ctx, v, 0, 2, 10); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled aggregation accepted", err)
	}
	for _, phase := range []string{"metadata", "isolated", "assembled"} {
		t.Run(phase, func(t *testing.T) {
			inner, cancel := context.WithCancel(context.Background())
			defer cancel()
			active := &cancelAtRARNames{Context: inner, cancel: cancel, armed: true}
			switch phase {
			case "metadata":
				many := v
				many.Files = append(append([]RAR4VolumeFile{}, v.Files...), v.Files...)
				if parts, err := rar4SetParts(active, many); !errors.Is(err, context.Canceled) || parts != nil {
					t.Fatal("partial metadata survived cancellation", parts, err)
				}
			case "isolated":
				parts, err := rar4SetParts(context.Background(), v)
				if err != nil {
					t.Fatal(err)
				}
				if rarVolumeTableMatches(active, 0, parts, []sevenEntry{{}}) || active.Err() == nil {
					t.Fatal("canceled isolated table accepted")
				}
			case "assembled":
				facts := rarSetFiles{files: []rarBindingFile{{Name: "book.epub"}}, starts: []int{0}}
				if facts.matchesAssembled(active, []sevenEntry{{}}) || active.Err() == nil {
					t.Fatal("canceled assembled table accepted")
				}
			}
		})
	}
}

func FuzzRAR4VolumeHeaders(f *testing.F) {
	f.Add(uint8(0), uint16(3), uint8(0x80))
	f.Add(uint8(1), uint16(11), uint8(0xff))
	f.Add(uint8(2), uint16(3), uint8(2))
	f.Fuzz(func(t *testing.T, which uint8, offset uint16, value uint8) {
		record := rar4MetadataRecord("book.epub", []byte("abc"), 0x1100, rar4Times(0xfff0), false)
		record = rar4Change(record, func(h []byte) {
			binary.LittleEndian.PutUint16(h[3:], binary.LittleEndian.Uint16(h[3:])|2)
			binary.LittleEndian.PutUint32(h[11:], 6)
		})
		b := rar4TestVolume(0, 2, true, record)
		start := []int{7, 20, len(b) - 16}[which%3]
		size := int(binary.LittleEndian.Uint16(b[start+5:]))
		b[start+int(offset)%size] ^= value
		binary.LittleEndian.PutUint16(b[start:], uint16(crc32.ChecksumIEEE(b[start+2:start+size])))
		v, err := RAR4VolumeAccounted(context.Background(), bytes.NewReader(b), int64(len(b)))
		if err != nil {
			if !reflect.DeepEqual(v, RAR4Volume{}) {
				t.Fatal("partial facts", v)
			}
			return
		}
		if v.Ordinal >= 1000 || v.First != (v.Ordinal == 0) {
			t.Fatal(v)
		}
		at := int64(20)
		for _, p := range v.Files {
			if p.HeaderOffset != at || p.DataOffset < at+33 || p.Packed > int64(len(b))-p.DataOffset {
				t.Fatal(p)
			}
			at = p.DataOffset + p.Packed
		}
		if at != int64(len(b)-9) && at != int64(len(b)-16) {
			t.Fatal("unpartitioned physical bytes", at, len(b))
		}
	})
}
