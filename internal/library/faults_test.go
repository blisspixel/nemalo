package library

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// fullDisk accepts room bytes, then fails every write the way a full disk
// does, after writing what still fits.
type fullDisk struct {
	w    io.Writer
	room int
}

func (d *fullDisk) Write(p []byte) (int, error) {
	if len(p) <= d.room {
		d.room -= len(p)
		return d.w.Write(p)
	}
	n, _ := d.w.Write(p[:d.room])
	d.room = 0
	return n, &os.PathError{Op: "write", Path: "disk", Err: syscall.ENOSPC}
}

// failWrite makes the nth write of stage fill the disk after room bytes.
func failWrite(t *testing.T, stage string, nth, room int) {
	t.Helper()
	saved := writeFault
	count := 0
	writeFault = func(s string, w io.Writer) io.Writer {
		if s != stage {
			return w
		}
		count++
		if count == nth {
			return &fullDisk{w: w, room: room}
		}
		return w
	}
	t.Cleanup(func() { writeFault = saved })
}

func healthyAfter(t *testing.T, root string) LibraryCheck {
	t.Helper()
	c, err := CheckLibrary(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range c.Issues {
		if i.Kind == "interrupted_journal_write" || strings.HasPrefix(i.Kind, "pending_") {
			t.Fatal("left unfinished", i)
		}
	}
	return c
}

// A disk that fills while an import copies leaves no partial file, keeps the
// source, and a repeated import finishes it.
func TestAFullDiskDuringAnImportCopyIsRecoverable(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	source := writeEPUB(t, t.TempDir())
	before, _ := os.ReadFile(source)
	failWrite(t, "import copy", 1, 100)
	if _, err := Import(ctx, ImportRequest{Library: root, Source: source, Apply: true}); !errors.Is(err, syscall.ENOSPC) {
		t.Fatal("the full disk was not reported", err)
	}
	if got, _ := os.ReadFile(source); !bytes.Equal(got, before) {
		t.Fatal("the source changed")
	}
	_ = filepath.WalkDir(filepath.Join(root, "assets"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			t.Fatal("a partial copy remained", p)
		}
		return nil
	})
	c, _ := CheckLibrary(ctx, root)
	if c.Healthy || len(c.Issues) == 0 || c.Issues[0].Kind != "pending_import" {
		t.Fatal("the interrupted import is not reported", c.Issues)
	}
	writeFault = func(_ string, w io.Writer) io.Writer { return w }
	done, err := Import(ctx, ImportRequest{Library: root, Source: source, Apply: true})
	if err != nil || !done.Applied || !done.Resumed {
		t.Fatal(done, err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(done.Path))); !bytes.Equal(got, before) {
		t.Fatal("the stored copy differs")
	}
	if c := healthyAfter(t, root); c.Holdings != 1 {
		t.Fatal(c)
	}
}

// A disk that fills while a journal line is written leaves a torn last line.
// It holds no record: readers ignore it, the check names it, and the next
// write removes it before it appends.
func TestATornJournalLineIsIgnoredReportedAndRepaired(t *testing.T) {
	ctx := context.Background()
	for _, nth := range []int{1, 2, 3} {
		// The first journal write is the import intent, the second the
		// holding, and the third the completion.
		root := t.TempDir()
		if _, err := Initialize(ctx, root); err != nil {
			t.Fatal(err)
		}
		source := writeEPUB(t, t.TempDir())
		failWrite(t, "journal", nth, 20)
		if _, err := Import(ctx, ImportRequest{Library: root, Source: source, Apply: true}); !errors.Is(err, syscall.ENOSPC) {
			t.Fatal(nth, err)
		}
		c, err := CheckLibrary(ctx, root)
		if err != nil && len(c.Issues) == 0 {
			t.Fatal("a torn line stopped the check", nth, err)
		}
		torn := false
		for _, i := range c.Issues {
			torn = torn || i.Kind == "interrupted_journal_write"
		}
		if !torn {
			t.Fatal("the torn line was not reported", nth, c.Issues)
		}
		if _, err := FindManaged(ctx, root, "", "all", "all", 10, 0); err != nil {
			t.Fatal("a torn line stopped listing", nth, err)
		}
		writeFault = func(_ string, w io.Writer) io.Writer { return w }
		done, err := Import(ctx, ImportRequest{Library: root, Source: source, Apply: true})
		if err != nil || !done.Applied {
			t.Fatal(nth, done, err)
		}
		for _, name := range []string{operationsName, holdingsName} {
			b, _ := os.ReadFile(filepath.Join(root, ".nemalo", name))
			if len(b) > 0 && b[len(b)-1] != '\n' {
				t.Fatal("the torn line was not removed", name)
			}
		}
		if c := healthyAfter(t, root); c.Holdings != 1 {
			t.Fatal(nth, c)
		}
	}
}

// A torn tail longer than one record is not a torn write: it is kept for
// review and never cut.
func TestALongUnterminatedJournalTailIsKeptForReview(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	if _, err := Import(ctx, ImportRequest{Library: root, Source: writeEPUB(t, t.TempDir()), Apply: true}); err != nil {
		t.Fatal(err)
	}
	ops := filepath.Join(root, ".nemalo", operationsName)
	f, err := os.OpenFile(ops, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write(bytes.Repeat([]byte("x"), maxRecord+1))
	_ = f.Close()
	before, _ := os.ReadFile(ops)
	if _, err := Import(ctx, ImportRequest{Library: root, Source: writeEPUB(t, t.TempDir()), Apply: true}); !errors.Is(err, ErrStateReview) {
		t.Fatal("a long unterminated tail was accepted", err)
	}
	if after, _ := os.ReadFile(ops); !bytes.Equal(after, before) {
		t.Fatal("a long unterminated tail was cut")
	}
}

// A disk that fills while cleanup copies a source into the holding area
// leaves the source in its folder, and the cleanup can be finished.
func TestAFullDiskDuringACleanupCopyKeepsTheSource(t *testing.T) {
	ctx := context.Background()
	root, folder := cleanupFixture(t)
	before := snapshotTree(t, folder)
	plan, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	if err != nil {
		t.Fatal(err)
	}
	failWrite(t, "cleanup copy", 1, 50)
	if _, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest}); !errors.Is(err, syscall.ENOSPC) {
		t.Fatal("the full disk was not reported", err)
	}
	for path, data := range before {
		if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, data) {
			t.Fatal("a source was lost or changed when the copy failed", path, err)
		}
	}
	writeFault = func(_ string, w io.Writer) io.Writer { return w }
	applied, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest})
	if err != nil || !applied.Applied || applied.Moved != 3 {
		t.Fatal("the cleanup could not be finished", applied, err)
	}
}

// A disk that fills while a holding is exported leaves no file at the
// destination.
func TestAFullDiskDuringAnExportLeavesNothing(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	r, err := Import(ctx, ImportRequest{Library: root, Source: writeEPUB(t, t.TempDir()), Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	failWrite(t, "export copy", 1, 64)
	if _, err := Export(ctx, ExportRequest{Library: root, AssetID: r.AssetID, Directory: out}); !errors.Is(err, syscall.ENOSPC) {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(out); len(entries) != 0 {
		t.Fatal("a partial export remained", entries[0].Name())
	}
}

// A scanner that fails, times out, or cannot run never makes an import
// checked: the holding stays in review with the reason, and the bytes are kept.
func TestAFailingScannerKeepsTheImportInReview(t *testing.T) {
	ctx := context.Background()
	for _, status := range []string{"unavailable", "incomplete", "error", "timeout", "unexpected_output"} {
		root := t.TempDir()
		if _, err := Initialize(ctx, root); err != nil {
			t.Fatal(err)
		}
		r, err := Import(ctx, ImportRequest{Library: root, Source: writeEPUB(t, t.TempDir()), Apply: true, Scan: true, Scanner: statusScan{status: status}})
		if err != nil || !r.Applied || r.Status != "review" || r.Reason == "checks_and_scan_recorded" {
			t.Fatal(status, r.Status, r.Reason, err)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(r.Path))); err != nil {
			t.Fatal("the bytes were not kept for review", status, err)
		}
	}
}
