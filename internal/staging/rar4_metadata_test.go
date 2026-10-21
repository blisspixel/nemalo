package staging

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"testing"
)

func rar4MetadataRecord(name string, payload []byte, flags uint16, times []byte, directory bool) []byte {
	base := rar4File(name, payload, directory)
	head := bytes.Clone(base[:32])
	if flags&0x100 != 0 {
		head = append(head, make([]byte, 8)...)
	}
	head = append(head, name...)
	head = append(head, times...)
	binary.LittleEndian.PutUint16(head[3:5], binary.LittleEndian.Uint16(head[3:5])|flags)
	binary.LittleEndian.PutUint16(head[5:7], uint16(len(head)))
	binary.LittleEndian.PutUint16(head, uint16(crc32.ChecksumIEEE(head[2:])))
	return append(head, payload...)
}
func rar4Times(mask uint16) []byte {
	b := binary.LittleEndian.AppendUint16(nil, mask)
	for i, shift := range []uint{12, 8, 4} {
		field := byte(mask>>shift) & 15
		if field&8 == 0 {
			continue
		}
		if i != 0 {
			b = binary.LittleEndian.AppendUint32(b, 0x5d54645c)
		}
		n := int(field & 3)
		fraction := uint32(9_999_999) >> uint((3-n)*8)
		for j := 0; j < n; j++ {
			b = append(b, byte(fraction>>uint(j*8)))
		}
	}
	return b
}

func TestRAR4KnownTimeAndZeroHighWordsPreserveExactSpans(t *testing.T) {
	for _, mask := range []uint16{0, 0x8000, 0xc000, 0x9000, 0xa000, 0xb000, 0xf000, 0x8800, 0x8080, 0x8880, 0xfff0} {
		for _, high := range []bool{false, true} {
			flags := uint16(0x1000)
			if high {
				flags |= 0x100
			}
			payload := []byte("synthetic book")
			record := rar4MetadataRecord("book.epub", payload, flags, rar4Times(mask), false)
			data := rar4Archive(record)
			files, err := RAR4Accounted(context.Background(), bytes.NewReader(data), int64(len(data)))
			if err != nil || len(files) != 1 || files[0].Name != "book.epub" || files[0].HeaderOffset != 20 || files[0].DataOffset != 20+int64(len(record)-len(payload)) || files[0].Packed != int64(len(payload)) || files[0].DataOffset+files[0].Packed != int64(len(data)-7) {
				t.Fatal(mask, high, files, err)
			}
		}
	}
	for _, directory := range []bool{false, true} {
		for _, flags := range []uint16{0x100, 0x1000, 0x1100} {
			name := "empty.epub"
			if directory {
				name = "folder"
			}
			var times []byte
			if flags&0x1000 != 0 {
				times = rar4Times(0xfff0)
			}
			data := rar4Archive(rar4MetadataRecord(name, nil, flags, times, directory))
			files, err := RAR4Accounted(context.Background(), bytes.NewReader(data), int64(len(data)))
			if err != nil || len(files) != 1 || files[0].Directory != directory || files[0].Bytes != 0 || files[0].Packed != 0 {
				t.Fatal(files, err)
			}
		}
	}
}

func TestRAR4TimeMetadataRefusesIncompleteOrUnknownRepresentations(t *testing.T) {
	good := rar4Times(0xfff0)
	variants := map[string][]byte{"missing": nil, "short_mask": {0}, "fourth_field": {1, 0}, "mtime_dangling": {0, 0x70}, "ctime_dangling": {0, 7}, "atime_dangling": {0x70, 0}, "trailing": append(bytes.Clone(good), 0), "fraction_outside_subset": {0, 0xb0, 0x80, 0x96, 0x98}}
	for n := 2; n < len(good); n++ {
		variants[fmt.Sprintf("truncated_%d", n)] = bytes.Clone(good[:n])
	}
	for name, times := range variants {
		t.Run(name, func(t *testing.T) {
			data := rar4Archive(rar4MetadataRecord("book.epub", []byte("synthetic book"), 0x1100, times, false))
			files, err := RAR4Accounted(context.Background(), bytes.NewReader(data), int64(len(data)))
			if !errors.Is(err, ErrRAR4Coverage) || files != nil {
				t.Fatal("incomplete metadata granted evidence", files, err)
			}
		})
	}
	data := rar4Archive(rar4MetadataRecord("book.epub", []byte("synthetic book"), 0, rar4Times(0x8000), false))
	if files, err := RAR4Accounted(context.Background(), bytes.NewReader(data), int64(len(data))); err == nil || files != nil {
		t.Fatal("undeclared metadata accepted", files, err)
	}
}

func TestRAR4HighWordsCannotEscapeSourceAndOutputBounds(t *testing.T) {
	for _, offset := range []int{32, 36} {
		record := rar4MetadataRecord("book.epub", []byte("synthetic book"), 0x100, nil, false)
		record = rar4Change(record, func(b []byte) { binary.LittleEndian.PutUint32(b[offset:offset+4], 1) })
		data := rar4Archive(record)
		if files, err := RAR4Accounted(context.Background(), bytes.NewReader(data), int64(len(data))); err == nil || files != nil {
			t.Fatal(offset, files, err)
		}
	}
	record := rar4Book("book.epub")
	record = rar4Change(record, func(b []byte) { binary.LittleEndian.PutUint16(b[3:5], 0x8100) })
	data := rar4Archive(record)
	if files, err := RAR4Accounted(context.Background(), bytes.NewReader(data), int64(len(data))); err == nil || files != nil {
		t.Fatal("missing high words accepted", files, err)
	}
}

func FuzzRAR4TimeMetadata(f *testing.F) {
	for _, mask := range []uint16{0, 0xb000, 0x8880, 0xfff0} {
		f.Add(rar4Times(mask))
	}
	f.Fuzz(func(t *testing.T, times []byte) {
		if len(times) > 4096 {
			return
		}
		data := rar4Archive(rar4MetadataRecord("book.epub", []byte("synthetic book"), 0x1100, times, false))
		checkRAR4Fuzz(t, data)
	})
}
