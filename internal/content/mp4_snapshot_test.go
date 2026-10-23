package content

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/blisspixel/nemalo/internal/audio"
	"github.com/blisspixel/nemalo/internal/safeio"
)

type packetReaderAtFunc func([]byte, int64) (int, error)

func (f packetReaderAtFunc) ReadAt(p []byte, off int64) (int, error) { return f(p, off) }

func packetScratch(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	t.Setenv("TMP", dir)
	t.Setenv("TEMP", dir)
	return dir
}
func scratchEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("private scratch retained", entries, err)
	}
}

func TestPacketSnapshotBindsReadsDespiteOriginalMutationAndReversion(t *testing.T) {
	dir := packetScratch(t)
	data := mp4Audio(4)
	req := audioFixture(t, data)
	sourcePath := filepath.Join(req.Root, "track.mp3")
	info, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	var retained io.ReaderAt
	err = resolveFile(context.Background(), req, &Result{}, func(source io.ReaderAt, size int64) error {
		return withPacketSnapshot(context.Background(), source, size, req.AssetID[7:], func(snapshot io.ReaderAt) error {
			retained = snapshot
			// The original changes during extraction and returns to its exact identity
			// before the outer final hash. Payload must still be the verified copy.
			changed := bytes.Clone(data)
			for i := len(changed) - 800; i < len(changed); i++ {
				changed[i] ^= 255
			}
			if err := os.WriteFile(sourcePath, changed, 0600); err != nil {
				return err
			}
			index, err := audio.IndexMP4(context.Background(), snapshot, size)
			if err != nil {
				return err
			}
			selected, err := index.ReadRange(context.Background(), 0, 65536)
			if err != nil {
				return err
			}
			if !bytes.Equal(selected.Data, data[len(data)-800:]) {
				t.Fatal("mutable original supplied payload")
			}
			if err := os.WriteFile(sourcePath, data, 0600); err != nil {
				return err
			}
			return os.Chtimes(sourcePath, info.ModTime(), info.ModTime())
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	scratchEmpty(t, dir)
	if _, err := retained.ReadAt(make([]byte, 1), 0); !errors.Is(err, os.ErrClosed) {
		t.Fatal("snapshot handle escaped its lifetime", err)
	}
}

func TestPacketSnapshotRejectsFailedChangedCanceledAndOversizedCopies(t *testing.T) {
	data := mp4Audio(4)
	for _, name := range []string{"changed", "short", "read_error", "cancel_copy", "cancel_use", "callback_error", "too_large", "zero", "bad_temp"} {
		t.Run(name, func(t *testing.T) {
			dir := packetScratch(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var source io.ReaderAt = bytes.NewReader(data)
			size := int64(len(data))
			want := error(nil)
			calls := 0
			switch name {
			case "changed":
				changed := bytes.Clone(data)
				changed[10] ^= 255
				source = bytes.NewReader(changed)
				want = errPacketSnapshotChanged
			case "short":
				source = bytes.NewReader(data[:len(data)-1])
				want = errPacketSnapshotChanged
			case "read_error":
				source = packetReaderAtFunc(func([]byte, int64) (int, error) { return 0, io.ErrUnexpectedEOF })
				want = io.ErrUnexpectedEOF
			case "cancel_copy":
				source = packetReaderAtFunc(func(p []byte, off int64) (int, error) {
					n, e := bytes.NewReader(data).ReadAt(p, off)
					cancel()
					return n, e
				})
				want = context.Canceled
			case "too_large":
				size = 2<<30 + 1
				want = errBudget
			case "zero":
				size = 0
				want = errBudget
			case "bad_temp":
				t.Setenv("TMPDIR", filepath.Join(dir, "absent"))
				t.Setenv("TMP", filepath.Join(dir, "absent"))
				t.Setenv("TEMP", filepath.Join(dir, "absent"))
			}
			err := withPacketSnapshot(ctx, source, size, digest(data), func(r io.ReaderAt) error {
				calls++
				if name == "cancel_use" {
					cancel()
					_, err := r.ReadAt(make([]byte, 1), 0)
					return err
				}
				if name == "callback_error" {
					return io.ErrNoProgress
				}
				return nil
			})
			if err == nil {
				t.Fatal("failed copy/use succeeded")
			}
			if want != nil && !errors.Is(err, want) {
				t.Fatal("wrong failure", err, want)
			}
			if name == "cancel_use" {
				if calls != 1 || !errors.Is(err, context.Canceled) {
					t.Fatal(calls, err)
				}
			} else if name == "callback_error" {
				if calls != 1 || !errors.Is(err, io.ErrNoProgress) {
					t.Fatal(calls, err)
				}
			} else if calls != 0 {
				t.Fatal("unverified copy reached parser", calls)
			}
			scratchEmpty(t, dir)
		})
	}
}

func TestPacketSnapshotRefusesDiskBeforeSourceReads(t *testing.T) {
	dir := packetScratch(t)
	saved := safeio.FreeSpace
	t.Cleanup(func() { safeio.FreeSpace = saved })
	safeio.FreeSpace = func(string, *os.Root) (int64, error) { return 0, nil }
	req, _ := packetRequest(t, 4)
	r, err := Get(context.Background(), req)
	noPacketPayload(t, r, err)
	if r.Status != "budget_exceeded" || !errors.Is(err, safeio.ErrNoSpace) {
		t.Fatal(r.Status, err)
	}
	reads := 0
	source := packetReaderAtFunc(func([]byte, int64) (int, error) { reads++; return 0, io.EOF })
	err = withPacketSnapshot(context.Background(), source, 10, digest(nil), func(io.ReaderAt) error { t.Fatal("disk refusal reached parser"); return nil })
	if !errors.Is(err, safeio.ErrNoSpace) || reads != 0 {
		t.Fatal(reads, err)
	}
	scratchEmpty(t, dir)
}

func TestPacketSnapshotLifetimeBudgetChargesFailedReadsAndCancellation(t *testing.T) {
	calls := 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := packetReaderAtFunc(func(p []byte, off int64) (int, error) { calls++; copy(p, "abc"); return 3, io.ErrUnexpectedEOF })
	r := packetSnapshotReader{ctx: ctx, reader: source, remaining: 5}
	n, err := r.ReadAt(make([]byte, 4), 0)
	if n != 3 || !errors.Is(err, io.ErrUnexpectedEOF) || r.remaining != 2 {
		t.Fatal(n, err, r.remaining)
	}
	n, err = r.ReadAt(make([]byte, 3), 0)
	if n != 0 || !errors.Is(err, errBudget) || calls != 1 || r.remaining != 2 {
		t.Fatal("over-budget read reached source", n, err, calls)
	}
	cancel()
	n, err = r.ReadAt(make([]byte, 1), 0)
	if n != 0 || !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal(n, err, calls)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	r = packetSnapshotReader{ctx: ctx, remaining: 2, reader: packetReaderAtFunc(func(p []byte, _ int64) (int, error) { p[0] = 1; cancel(); return 1, nil })}
	if n, err = r.ReadAt(make([]byte, 1), 0); n != 1 || !errors.Is(err, context.Canceled) || r.remaining != 1 {
		t.Fatal(n, err, r.remaining)
	}
}
