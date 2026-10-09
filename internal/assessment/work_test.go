package assessment

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

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
