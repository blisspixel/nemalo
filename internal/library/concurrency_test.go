package library

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"
)

// holdImport starts an import that stops after its intent is journaled and
// waits there, holding the writer lock, until the returned release is called.
func holdImport(t *testing.T, root, source string) (release func()) {
	t.Helper()
	started, wait := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := importLibrary(context.Background(), ImportRequest{Library: root, Source: source, Apply: true}, func(stage string) error {
			if stage == "intent" {
				close(started)
				<-wait
			}
			return nil
		})
		done <- err
	}()
	<-started
	return func() {
		close(wait)
		if err := <-done; err != nil {
			t.Fatal("the held import failed", err)
		}
	}
}

// While one writer holds a library, every other writing operation is refused
// as busy before it writes, and the held operation then finishes cleanly.
func TestEveryWriterIsRefusedWhileAnotherHoldsTheLibrary(t *testing.T) {
	ctx := context.Background()
	root, folder := cleanupFixture(t)
	plan, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	if err != nil {
		t.Fatal(err)
	}
	ops := filepath.Join(root, ".nemalo", operationsName)
	release := holdImport(t, root, writeEPUBParagraph(t, t.TempDir(), "Held while others try"))
	before := mustRead(t, ops)
	other := writeEPUBParagraph(t, t.TempDir(), "Another book")
	attempts := map[string]func() error{
		"import": func() error {
			_, err := Import(ctx, ImportRequest{Library: root, Source: other, Apply: true})
			return err
		},
		"ingest": func() error {
			_, err := Ingest(ctx, IngestRequest{Library: root, Folder: filepath.Dir(other), Apply: true})
			return err
		},
		"cleanup": func() error {
			_, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest})
			return err
		},
		"rescan": func() error {
			_, err := Rescan(ctx, RescanRequest{Library: root, Apply: true, Scanner: cleanScan{}})
			return err
		},
	}
	for name, attempt := range attempts {
		if err := attempt(); !errors.Is(err, ErrBusy) {
			t.Fatal(name, "was not refused as busy", err)
		}
		if !bytes.Equal(before, mustRead(t, ops)) {
			t.Fatal(name, "wrote while another writer held the library")
		}
	}
	release()
	if c := healthyAfter(t, root); c.Holdings == 0 {
		t.Fatal(c)
	}
	if _, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest}); err != nil {
		t.Fatal("cleanup could not run after the writer finished", err)
	}
}

// Many imports racing for one library each either finish or are refused as
// busy; retried, all are held and the journal is consistent.
func TestRacingImportsLeaveAConsistentLibrary(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	const n = 8
	sources := make([]string, n)
	for i := range sources {
		sources[i] = writeEPUBParagraph(t, t.TempDir(), "Racing book "+strconv.Itoa(i))
	}
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for _, source := range sources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				_, err := Import(ctx, ImportRequest{Library: root, Source: source, Apply: true})
				if !errors.Is(err, ErrBusy) {
					errs <- err
					return
				}
				runtime.Gosched()
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if c := healthyAfter(t, root); c.Holdings != n {
		t.Fatal("holdings", c.Holdings)
	}
}

// A store or control folder the process may not write refuses the import,
// keeps the source, and once writable again the import finishes.
func TestPermissionDeniedKeepsTheSourceAndRecovers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores permission bits")
	}
	ctx := context.Background()
	for _, locked := range []string{"assets", ".nemalo"} {
		root := t.TempDir()
		if _, err := Initialize(ctx, root); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(root, locked)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		source := writeEPUB(t, t.TempDir())
		before := mustRead(t, source)
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		_, err := Import(ctx, ImportRequest{Library: root, Source: source, Apply: true})
		if chmodErr := os.Chmod(dir, 0o700); chmodErr != nil {
			t.Fatal(chmodErr)
		}
		if !errors.Is(err, os.ErrPermission) {
			t.Fatal(locked, "the denied write was not reported", err)
		}
		if !bytes.Equal(before, mustRead(t, source)) {
			t.Fatal(locked, "the source changed")
		}
		done, err := Import(ctx, ImportRequest{Library: root, Source: source, Apply: true})
		if err != nil || !done.Applied {
			t.Fatal(locked, "the import did not finish once writable", done, err)
		}
		if c := healthyAfter(t, root); c.Holdings != 1 {
			t.Fatal(locked, c)
		}
	}
}
