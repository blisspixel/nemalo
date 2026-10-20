package staging

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"strings"
	"testing"
)

func rar5Numbers(values ...uint64) []byte {
	var b []byte
	for _, n := range values {
		b = binary.AppendUvarint(b, n)
	}
	return b
}

// Write stored synthetic records only. No reader or decoder is used in tests.
func rar5Record(kind, flags uint64, body, data []byte) []byte {
	return rar5RecordExtra(kind, flags, body, nil, data)
}

func rar5RecordExtra(kind, flags uint64, body, extra, data []byte) []byte {
	h := rar5Numbers(kind, flags)
	if flags&1 != 0 {
		h = append(h, rar5Numbers(uint64(len(extra)))...)
	}
	if flags&2 != 0 {
		h = append(h, rar5Numbers(uint64(len(data)))...)
	}
	h = append(h, body...)
	h = append(h, extra...)
	return append(rar5RawHeader(h), data...)
}

func rar5RawHeader(h []byte) []byte {
	checked := append(rar5Numbers(uint64(len(h))), h...)
	return append(binary.LittleEndian.AppendUint32(nil, crc32.ChecksumIEEE(checked)), checked...)
}

func rar5ExtraRecord(kind uint64, fields []byte) []byte {
	b := append(rar5Numbers(kind), fields...)
	return append(rar5Numbers(uint64(len(b))), b...)
}

func rar5TimeFields(flags uint64) []byte {
	b := rar5Numbers(flags)
	for _, bit := range []uint64{2, 4, 8} {
		if flags&bit != 0 {
			b = binary.LittleEndian.AppendUint32(b, 123)
			if flags&1 == 0 {
				b = binary.LittleEndian.AppendUint32(b, 456)
			}
		}
	}
	if flags&17 == 17 {
		for _, bit := range []uint64{2, 4, 8} {
			if flags&bit != 0 {
				b = binary.LittleEndian.AppendUint32(b, 999_999_999)
			}
		}
	}
	return b
}

func TestRAR5KnownMetadataPreservesTheExactHeaderPartition(t *testing.T) {
	for _, flags := range []uint64{2, 4, 8, 14, 3, 15, 19, 31} {
		t.Run(fmt.Sprint(flags), func(t *testing.T) {
			// An extended zero locator is ordinary preallocation, not a
			// new offset or permission to skip any block in the traversal.
			locator := rar5ExtraRecord(1, append(rar5Numbers(1), []byte{0x80, 0x80, 0x80, 0}...))
			b := append([]byte("Rar!\x1a\x07\x01\x00"), rar5RecordExtra(1, 5, rar5Numbers(0), locator, nil)...)
			payload := []byte("synthetic book")
			record := rar5RecordExtra(2, 7, rar5FileBody("book.epub", payload, 4, 0100644, 0, 1), rar5ExtraRecord(3, rar5TimeFields(flags)), payload)
			start := len(b)
			b = append(b, record...)
			b = append(b, rar5Record(5, 4, rar5Numbers(0), nil)...)
			files, err := RAR5Accounted(context.Background(), bytes.NewReader(b), int64(len(b)))
			if err != nil || len(files) != 1 || files[0].Name != "book.epub" || files[0].HeaderOffset != int64(start) || files[0].DataOffset+files[0].Packed != int64(start+len(record)) {
				t.Fatal(files, err)
			}
		})
	}
}

func TestRAR5MetadataNeverSkipsUnknownOrMalformedEvidence(t *testing.T) {
	goodTime := rar5TimeFields(19)
	badNanos := append([]byte{}, goodTime...)
	binary.LittleEndian.PutUint32(badNanos[len(badNanos)-4:], 1_000_000_000)
	variants := map[string]struct {
		main  bool
		extra []byte
	}{
		"locator_target":     {true, rar5ExtraRecord(1, rar5Numbers(1, 7))},
		"locator_recovery":   {true, rar5ExtraRecord(1, rar5Numbers(2, 7))},
		"locator_flags":      {true, rar5ExtraRecord(1, rar5Numbers(4))},
		"locator_missing":    {true, rar5ExtraRecord(1, rar5Numbers(1))},
		"locator_trailing":   {true, rar5ExtraRecord(1, rar5Numbers(0, 0))},
		"archive_metadata":   {true, rar5ExtraRecord(2, rar5Numbers(0))},
		"unknown_type":       {false, rar5ExtraRecord(42, []byte("retained metadata"))},
		"time_flags":         {false, rar5ExtraRecord(3, rar5TimeFields(35))},
		"time_nanos_windows": {false, rar5ExtraRecord(3, rar5TimeFields(18))},
		"time_no_field":      {false, rar5ExtraRecord(3, rar5TimeFields(1))},
		"time_nanos_range":   {false, rar5ExtraRecord(3, badNanos)},
		"time_short":         {false, rar5ExtraRecord(3, goodTime[:len(goodTime)-1])},
		"time_duplicate":     {false, append(rar5ExtraRecord(3, goodTime), rar5ExtraRecord(3, goodTime)...)},
		"short_extra":        {false, []byte{127, 3}},
		"extra_overflow":     {false, bytes.Repeat([]byte{0xff}, 11)},
	}
	for name, v := range variants {
		t.Run(name, func(t *testing.T) {
			var b []byte
			if v.main {
				b = append([]byte("Rar!\x1a\x07\x01\x00"), rar5RecordExtra(1, 5, rar5Numbers(0), v.extra, nil)...)
				b = append(b, rar5Book("book.epub")...)
				b = append(b, rar5Record(5, 0, rar5Numbers(0), nil)...)
			} else {
				payload := []byte("synthetic book")
				b = rar5Archive(rar5RecordExtra(2, 3, rar5FileBody("book.epub", payload, 4, 0100644, 0, 1), v.extra, payload))
			}
			if files, err := RAR5Accounted(context.Background(), bytes.NewReader(b), int64(len(b))); !errors.Is(err, ErrRAR5Coverage) || files != nil {
				t.Fatal("metadata conferred coverage", files, err)
			}
		})
	}
	bad := append([]byte("Rar!\x1a\x07\x01\x00"), rar5RawHeader(rar5Numbers(1, 1, 1<<40, 0))...)
	if files, err := RAR5Accounted(context.Background(), bytes.NewReader(bad), int64(len(bad))); err == nil || files != nil {
		t.Fatal("extra area escaped its header", files, err)
	}
}

func rar5FileBody(name string, data []byte, flags, attributes, compression, host uint64) []byte {
	b := rar5Numbers(flags, uint64(len(data)), attributes)
	if flags&2 != 0 {
		b = binary.LittleEndian.AppendUint32(b, 123)
	}
	if flags&4 != 0 {
		b = binary.LittleEndian.AppendUint32(b, crc32.ChecksumIEEE(data))
	}
	b = append(b, rar5Numbers(compression, host, uint64(len(name)))...)
	return append(b, name...)
}

func rar5Book(name string) []byte {
	data := []byte("synthetic book")
	return rar5Record(2, 2, rar5FileBody(name, data, 6, 0100644, 0, 1), data)
}

func rar5Archive(records ...[]byte) []byte {
	b := append([]byte("Rar!\x1a\x07\x01\x00"), rar5Record(1, 0, rar5Numbers(0), nil)...)
	for _, r := range records {
		b = append(b, r...)
	}
	return append(b, rar5Record(5, 0, rar5Numbers(0), nil)...)
}

func TestRAR5FramingPartitionsExactDeclaredRecords(t *testing.T) {
	dir := rar5Record(2, 0, rar5FileBody("parts", nil, 1, 0040755, 0, 1), nil)
	data := rar5Archive(dir, rar5Book("parts/one.epub"), rar5Book("two.pdf"))
	files, err := RAR5Accounted(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil || len(files) != 3 {
		t.Fatal(files, err)
	}
	if !files[0].Directory || files[0].Bytes != 0 || files[0].Packed != 0 || files[0].HasCRC {
		t.Fatal("directory record invented data", files[0])
	}
	end := int64(8 + len(rar5Record(1, 0, rar5Numbers(0), nil)))
	for _, f := range files {
		if f.HeaderOffset != end || f.DataOffset <= f.HeaderOffset || f.DataOffset+f.Packed > int64(len(data)) {
			t.Fatal("records did not partition bytes", files)
		}
		end = f.DataOffset + f.Packed
		if !f.Directory && (!f.HasCRC || f.CRC32 != crc32.ChecksumIEEE([]byte("synthetic book")) || f.Bytes != 14 || f.Packed != 14 || f.HostOS != 1 || f.Attributes != 0100644) {
			t.Fatal("declared facts changed", f)
		}
	}
	if end+int64(len(rar5Record(5, 0, rar5Numbers(0), nil))) != int64(len(data)) || files[1].Name != "parts/one.epub" {
		t.Fatal(files, end)
	}
}

type protectedRARData struct {
	data       *bytes.Reader
	start, end int64
}

func (r protectedRARData) ReadAt(p []byte, off int64) (int, error) {
	if off < r.end && off+int64(len(p)) > r.start {
		return 0, errors.New("framing attempted to read payload")
	}
	return r.data.ReadAt(p, off)
}

func TestRAR5FramingDoesNotReadOrVouchForPayload(t *testing.T) {
	data := rar5Archive(rar5Book("book.epub"))
	plain, err := RAR5Accounted(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	f := plain[0]
	copy(data[f.DataOffset:f.DataOffset+f.Packed], bytes.Repeat([]byte("x"), int(f.Packed)))
	r := protectedRARData{bytes.NewReader(data), f.DataOffset, f.DataOffset + f.Packed}
	got, err := RAR5Accounted(context.Background(), r, int64(len(data)))
	if err != nil || len(got) != 1 || got[0].CRC32 == crc32.ChecksumIEEE(data[f.DataOffset:f.DataOffset+f.Packed]) {
		t.Fatal("framing was confused with integrity", got, err)
	}
}

func TestRAR5FramingKnownMetadataAndEmptyContainers(t *testing.T) {
	for _, metadata := range []struct {
		name                          string
		attributes, compression, host uint64
	}{
		{"Windows", 32, 0, 0},
		{"Unix permissions", 0644, 0, 1},
		{"version 0 compressed declaration", 0100644, 1<<7 | 15<<10, 1},
		{"version 1 compressed declaration", 0100644, 1 | 5<<7 | 23<<10 | 3<<15 | 1<<20, 1},
	} {
		t.Run(metadata.name, func(t *testing.T) {
			payload := []byte("declared payload")
			name := strings.Repeat("x", 1024)
			data := rar5Archive(rar5Record(2, 2, rar5FileBody(name, payload, 4, metadata.attributes, metadata.compression, metadata.host), payload))
			files, err := RAR5Accounted(context.Background(), bytes.NewReader(data), int64(len(data)))
			if err != nil || len(files) != 1 || files[0].Name != name || files[0].Compression != metadata.compression || files[0].HostOS != metadata.host {
				t.Fatal(files, err)
			}
		})
	}
	empty := rar5Archive()
	if files, err := RAR5Accounted(context.Background(), bytes.NewReader(empty), int64(len(empty))); err != nil || len(files) != 0 {
		t.Fatal(files, err)
	}
}

func TestRAR5FramingRejectsUnaccountedOrUnsupportedBytes(t *testing.T) {
	book := rar5Book("book.epub")
	data := []byte("synthetic book")
	file := func(flags, attributes, compression, host uint64, name string) []byte {
		return rar5Record(2, 2, rar5FileBody(name, data, flags, attributes, compression, host), data)
	}
	variants := map[string][]byte{
		"stub":                       append([]byte("stub"), rar5Archive(book)...),
		"rar4":                       []byte("Rar!\x1a\x07\x00"),
		"missing_main":               append([]byte("Rar!\x1a\x07\x01\x00"), book...),
		"second_main":                rar5Archive(rar5Record(1, 0, rar5Numbers(0), nil), book),
		"volume":                     append(append([]byte("Rar!\x1a\x07\x01\x00"), rar5Record(1, 0, rar5Numbers(1), nil)...), book...),
		"recovery_flag":              append([]byte("Rar!\x1a\x07\x01\x00"), rar5Record(1, 0, rar5Numbers(8), nil)...),
		"main_extra":                 append([]byte("Rar!\x1a\x07\x01\x00"), rar5Record(1, 1, rar5Numbers(0, 0), nil)...),
		"service":                    rar5Archive(book, rar5Record(3, 2, rar5FileBody("ZZZ", data, 4, 0, 0, 1), data)),
		"encryption":                 rar5Archive(book, rar5Record(4, 0, nil, nil)),
		"unknown_block":              rar5Archive(book, rar5Record(6, 4, nil, nil)),
		"unknown_file_flag":          rar5Archive(file(20, 0100644, 0, 1, "book.epub")),
		"unknown_size":               rar5Archive(file(12, 0100644, 0, 1, "book.epub")),
		"no_crc":                     rar5Archive(file(0, 0100644, 0, 1, "book.epub")),
		"file_extra":                 rar5Archive(rar5Record(2, 3, rar5Numbers(0), nil)),
		"split":                      rar5Archive(rar5Record(2, 10, rar5FileBody("book.epub", data, 4, 0100644, 0, 1), data)),
		"dependent":                  rar5Archive(rar5Record(2, 34, nil, data)),
		"directory_data":             rar5Archive(file(5, 0040755, 0, 1, "folder")),
		"unix_link":                  rar5Archive(file(4, 0120777, 0, 1, "book.epub")),
		"unix_mode":                  rar5Archive(file(4, 1<<32, 0, 1, "book.epub")),
		"windows_reparse":            rar5Archive(file(4, 1024, 0, 0, "book.epub")),
		"windows_directory_mismatch": rar5Archive(file(4, 16, 0, 0, "book.epub")),
		"unknown_host":               rar5Archive(file(4, 0, 0, 2, "book.epub")),
		"unknown_version":            rar5Archive(file(4, 0100644, 2, 1, "book.epub")),
		"unknown_method":             rar5Archive(file(4, 0100644, 6<<7, 1, "book.epub")),
		"unknown_compression_bits":   rar5Archive(file(4, 0100644, 1<<24, 1, "book.epub")),
		"v0_dictionary":              rar5Archive(file(4, 0100644, 16<<10, 1, "book.epub")),
		"v0_fraction":                rar5Archive(file(4, 0100644, 1<<15, 1, "book.epub")),
		"v1_dictionary":              rar5Archive(file(4, 0100644, 1|24<<10, 1, "book.epub")),
		"duplicate":                  rar5Archive(book, book),
		"file_is_parent":             rar5Archive(rar5Book("folder"), rar5Book("folder/book.epub")),
		"tail":                       append(rar5Archive(book), []byte("retained bytes")...),
		"missing_end":                rar5Archive(book)[:len(rar5Archive(book))-7],
		"end_more":                   append(rar5Archive(book)[:len(rar5Archive(book))-8], rar5Record(5, 0, rar5Numbers(1), nil)...),
		"end_extra":                  rar5Archive(book, rar5Record(5, 1, rar5Numbers(0), nil)),
		"bad_integer":                rar5Archive(rar5Record(2, 0, bytes.Repeat([]byte{0xff}, 11), nil)),
		"oversized_file":             rar5Archive(rar5Record(2, 0, rar5Numbers(4, 1<<40, 0), nil)),
		"oversized_header":           append([]byte("Rar!\x1a\x07\x01\x00"), []byte{0, 0, 0, 0, 0xff, 0xff, 0xff, 0xff}...),
	}
	for _, name := range []string{".", "../book.epub", "/book.epub", "C:book.epub", "folder\\book.epub", "bad\x00.epub", "line\nbook.epub", "rtl\u202ebook.epub", "bad\ufffe.epub", "\xff.epub", strings.Repeat("x", 1025)} {
		variants["name_"+fmt.Sprintf("%x", []byte(name[:min(len(name), 16)]))] = rar5Archive(file(4, 0100644, 0, 1, name))
	}
	badCRC := rar5Archive(book)
	badCRC[8] ^= 1
	variants["header_crc"] = badCRC
	for name, archive := range variants {
		t.Run(name, func(t *testing.T) {
			files, err := RAR5Accounted(context.Background(), bytes.NewReader(archive), int64(len(archive)))
			if !errors.Is(err, ErrRAR5Coverage) || files != nil {
				t.Fatal("unsupported bytes returned coverage", files, err)
			}
		})
	}
}

func TestRAR5EveryTruncationRetainsAllEvidence(t *testing.T) {
	data := rar5Archive(rar5Book("book.epub"))
	for n := 0; n < len(data); n++ {
		files, err := RAR5Accounted(context.Background(), bytes.NewReader(data[:n]), int64(n))
		if err == nil || files != nil {
			t.Fatalf("truncation %d returned coverage: %+v %v", n, files, err)
		}
	}
}

func TestRAR5FramingCancellationAndSharedFailedReadWork(t *testing.T) {
	data := rar5Archive(rar5Book("book.epub"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if files, err := RAR5Accounted(ctx, bytes.NewReader(data), int64(len(data))); !errors.Is(err, context.Canceled) || files != nil {
		t.Fatal(files, err)
	}
	active, stop := context.WithCancel(context.Background())
	if files, err := RAR5Accounted(active, cancelZIPRead{bytes.NewReader(data), stop}, int64(len(data))); !errors.Is(err, context.Canceled) || files != nil {
		t.Fatal(files, err)
	}
	limits := Defaults()
	limits.tree = &treeBudget{reads: allowance{remaining: 32}, proofs: allowance{remaining: 15}}
	if files, err := RAR5Accounted(context.Background(), limits.proofReader(bytes.NewReader(data)), int64(len(data))); !errors.Is(err, errTreeBudget) || files != nil || limits.tree.proofs.left() != 0 || limits.tree.reads.left() != 17 {
		t.Fatal(files, err, limits.tree.proofs.left(), limits.tree.reads.left())
	}
	if _, err := RAR5Accounted(context.Background(), limits.proofReader(bytes.NewReader(data)), int64(len(data))); !errors.Is(err, errTreeBudget) || limits.tree.reads.left() != 17 {
		t.Fatal("failed work was refunded", err, limits.tree.reads.left())
	}
}

func TestRAR5FramingBoundsFilesAndSourceSize(t *testing.T) {
	var records [][]byte
	for n := 0; n < 10001; n++ {
		records = append(records, rar5Record(2, 0, rar5FileBody(fmt.Sprintf("book-%d.epub", n), nil, 4, 0100644, 0, 1), nil))
	}
	for _, count := range []int{10000, 10001} {
		data := rar5Archive(records[:count]...)
		files, err := RAR5Accounted(context.Background(), bytes.NewReader(data), int64(len(data)))
		if count == 10000 && (err != nil || len(files) != count) {
			t.Fatal(count, len(files), err)
		}
		if count == 10001 && (err == nil || files != nil) {
			t.Fatal("member limit ignored", len(files), err)
		}
	}
	for _, size := range []int64{-1, 4<<30 + 1} {
		if _, err := RAR5Accounted(context.Background(), bytes.NewReader(nil), size); err == nil {
			t.Fatal(size)
		}
	}
}

func FuzzRAR5Accounted(f *testing.F) {
	f.Add(rar5Archive(rar5Book("book.epub")))
	payload := []byte("synthetic book")
	f.Add(rar5Archive(rar5RecordExtra(2, 7, rar5FileBody("book.epub", payload, 4, 0100644, 0, 1), rar5ExtraRecord(3, rar5TimeFields(19)), payload)))
	f.Add(rar5Archive(rar5Record(3, 0, []byte("service"), nil)))
	f.Add([]byte("Rar!\x1a\x07\x01\x00"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4<<20 {
			return
		}
		files, err := RAR5Accounted(context.Background(), bytes.NewReader(data), int64(len(data)))
		if err != nil {
			if files != nil {
				t.Fatal("partial evidence escaped a failure")
			}
			return
		}
		last := int64(8)
		for _, file := range files {
			if file.HeaderOffset < last || file.DataOffset <= file.HeaderOffset || file.Packed < 0 || file.DataOffset > int64(len(data))-file.Packed {
				t.Fatal("invalid covered span", file)
			}
			last = file.DataOffset + file.Packed
		}
	})
}

// Recompute only the outer header CRC so mutations reach extra-field parsing
// rather than spending the whole run at the checksum boundary.
func FuzzRAR5Extras(f *testing.F) {
	f.Add(true, rar5ExtraRecord(1, rar5Numbers(1, 0)))
	f.Add(false, rar5ExtraRecord(3, rar5TimeFields(19)))
	f.Add(false, rar5ExtraRecord(42, []byte("unknown metadata")))
	f.Fuzz(func(t *testing.T, main bool, extra []byte) {
		if len(extra) > 4096 {
			return
		}
		var b []byte
		if main {
			b = append([]byte("Rar!\x1a\x07\x01\x00"), rar5RecordExtra(1, 5, rar5Numbers(0), extra, nil)...)
			b = append(b, rar5Book("book.epub")...)
			b = append(b, rar5Record(5, 4, rar5Numbers(0), nil)...)
		} else {
			payload := []byte("synthetic book")
			b = rar5Archive(rar5RecordExtra(2, 3, rar5FileBody("book.epub", payload, 4, 0100644, 0, 1), extra, payload))
		}
		files, err := RAR5Accounted(context.Background(), bytes.NewReader(b), int64(len(b)))
		if err != nil && files != nil {
			t.Fatal("partial evidence escaped unsupported metadata")
		}
		if err == nil && (len(files) != 1 || files[0].Name != "book.epub" || files[0].Bytes != 14 || files[0].Packed != 14) {
			t.Fatal("metadata changed the bound selection", files)
		}
	})
}

var _ io.ReaderAt = protectedRARData{}
