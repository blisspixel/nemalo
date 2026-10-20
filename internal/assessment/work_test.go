package assessment

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestOpenZIPBoundsDirectoryAndKeepsMemberLifetimeBudget(t *testing.T) {
	data := book(t, "<html><body>Reader budget.</body></html>", nil)
	if _, err := OpenZIP(context.Background(), bytes.NewReader(data), int64(len(data)), 1, 1<<20, 10000); !errors.Is(err, ErrLimit) {
		t.Fatal("directory parsing escaped its budget", err)
	}
	z, err := OpenZIP(context.Background(), bytes.NewReader(data), int64(len(data)), 1<<20, int64(len(data))*3, 10000)
	if err != nil {
		t.Fatal(err)
	}
	limited := false
	for i := 0; i < 100; i++ {
		r, err := z.File[0].Open()
		if err == nil {
			_, err = io.Copy(io.Discard, r)
			_ = r.Close()
		}
		if errors.Is(err, ErrLimit) {
			limited = true
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if !limited {
		t.Fatal("reopening members reset their compressed-reader budget")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := OpenZIP(ctx, bytes.NewReader(data), int64(len(data)), 1<<20, 1<<20, 10000); !errors.Is(err, context.Canceled) {
		t.Fatal("directory reads ignored cancellation", err)
	}
}

func TestCompressedLayoutRejectsReplayBeforeDecompression(t *testing.T) {
	data := book(t, "<html><body>普通の本</body></html>", nil)
	var entries []int
	for offset := 0; offset+46 < len(data); offset++ {
		if bytes.Equal(data[offset:offset+4], []byte{'P', 'K', 1, 2}) {
			entries = append(entries, offset)
		}
	}
	if len(entries) < 2 {
		t.Fatal("fixture has no central entries")
	}
	// Distinct names now reference exactly the same compressed range.
	first, second := entries[0], entries[1]
	copy(data[second+20:second+28], data[first+20:first+28])
	copy(data[second+42:second+46], data[first+42:first+46])
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := z.File[0].DataOffset()
	b, _ := z.File[1].DataOffset()
	if a != b {
		t.Fatal("replay fixture failed")
	}
	if _, err := Inspect(bytes.NewReader(data), int64(len(data)), "epub"); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatal("replay reached decompression", err)
	}
}

func TestCompressedLayoutBoundsAndNormalEmptyMembers(t *testing.T) {
	data := book(t, "<html><body>Text</body></html>", func(files map[string]string) { files["empty"] = "" })
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if err := compressedLayout(z.File, int64(len(data))); err != nil {
		t.Fatal(err)
	}
	z.File[0].CompressedSize64 = ^uint64(0)
	if err := compressedLayout(z.File, int64(len(data))); !errors.Is(err, ErrLimit) {
		t.Fatal("overflow accepted", err)
	}
	if err := compressedLayout(z.File, -1); err == nil {
		t.Fatal("negative size accepted")
	}
	// A truncated local header fails before any member is opened.
	cut := bytes.Index(data, []byte{'P', 'K', 1, 2})
	if cut < 0 {
		t.Fatal("fixture directory missing")
	}
	mutated := append([]byte(nil), data...)
	binary.LittleEndian.PutUint32(mutated[cut+42:cut+46], uint32(len(data)-1))
	z, err = zip.NewReader(bytes.NewReader(mutated), int64(len(mutated)))
	if err != nil {
		t.Fatal(err)
	}
	if err := compressedLayout(z.File, int64(len(mutated))); err == nil {
		t.Fatal("bad local offset accepted")
	}
}

func TestCompressedReaderLifetimeBudgetAndCancellation(t *testing.T) {
	r := &metadataReader{r: bytes.NewReader([]byte("abcdefgh")), ctx: context.Background(), remaining: 100, workRemaining: 5, limited: true}
	if n, err := r.ReadAt(make([]byte, 3), 0); err != nil || n != 3 {
		t.Fatal(n, err)
	}
	r.limited = false
	if _, err := r.ReadAt(make([]byte, 3), 0); !errors.Is(err, ErrLimit) {
		t.Fatal("disabling metadata limit disabled lifetime budget", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.ctx = ctx
	if _, err := r.ReadAt(make([]byte, 1), 0); !errors.Is(err, context.Canceled) {
		t.Fatal("compressed input ignored cancellation", err)
	}
	if _, err := InspectContext(ctx, bytes.NewReader([]byte("%PDF-1.7\n%%EOF")), 14, "pdf"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

// tailOnlyZIP exposes a large logical file without allocating its payload.
// Only the standard end records matter to the preallocation-budget regression.
type tailOnlyZIP struct {
	size int64
	tail []byte
}

func (r tailOnlyZIP) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= r.size {
		return 0, io.EOF
	}
	n := min(int64(len(p)), r.size-off)
	clear(p[:n])
	start := r.size - int64(len(r.tail))
	lo, hi := max(off, start), min(off+n, r.size)
	if hi > lo {
		copy(p[lo-off:hi-off], r.tail[lo-start:hi-start])
	}
	if n < int64(len(p)) {
		return int(n), io.EOF
	}
	return int(n), nil
}

func zip64EndForTest(records, directorySize, directoryOffset, recordOffset uint64) []byte {
	b := make([]byte, 98)
	copy(b, "PK\x06\x06")
	binary.LittleEndian.PutUint64(b[4:], 44)
	binary.LittleEndian.PutUint16(b[12:], 45)
	binary.LittleEndian.PutUint16(b[14:], 45)
	binary.LittleEndian.PutUint64(b[24:], records)
	binary.LittleEndian.PutUint64(b[32:], records)
	binary.LittleEndian.PutUint64(b[40:], directorySize)
	binary.LittleEndian.PutUint64(b[48:], directoryOffset)
	copy(b[56:], "PK\x06\x07")
	binary.LittleEndian.PutUint64(b[64:], recordOffset)
	binary.LittleEndian.PutUint32(b[72:], 1)
	copy(b[76:], "PK\x05\x06")
	binary.LittleEndian.PutUint16(b[84:], 0xffff)
	binary.LittleEndian.PutUint16(b[86:], 0xffff)
	binary.LittleEndian.PutUint32(b[88:], 0xffffffff)
	binary.LittleEndian.PutUint32(b[92:], 0xffffffff)
	return b
}

func TestZIP64CountIsBoundedBeforeStandardReaderPreallocation(t *testing.T) {
	const size = 64 << 20
	tail := zip64EndForTest(1<<20, 0, 0, size-98)
	if _, err := OpenZIP(context.Background(), tailOnlyZIP{size, tail}, size, 2<<20, 1<<30, 10000); !errors.Is(err, ErrLimit) || !strings.Contains(err.Error(), "ZIP64 declared member count") {
		t.Fatal("ZIP64 count reached decoder allocation", err)
	}
	// A normal bounded ZIP64 end record remains supported.
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	out, err := w.CreateHeader(&zip.FileHeader{Name: "book.epub", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = out.Write([]byte("ZIP64 member")); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	end := len(raw) - 22
	dirSize := binary.LittleEndian.Uint32(raw[end+12:])
	dirOffset := binary.LittleEndian.Uint32(raw[end+16:])
	data := append(append([]byte{}, raw[:end]...), zip64EndForTest(1, uint64(dirSize), uint64(dirOffset), uint64(end))...)
	z, err := OpenZIP(context.Background(), bytes.NewReader(data), int64(len(data)), 2<<20, 1<<30, 10000)
	if err != nil || len(z.File) != 1 {
		t.Fatal("valid ZIP64 refused", err)
	}
	r, err := z.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	body, err := io.ReadAll(r)
	if err != nil || string(body) != "ZIP64 member" {
		t.Fatal(string(body), err)
	}
}
