package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/testfs"
)

type reparseLibraryFixture struct {
	root, folder, name string
	book               []byte
	imported           ImportResult
}

func newReparseLibraryFixture(t *testing.T) reparseLibraryFixture {
	t.Helper()
	f := reparseLibraryFixture{root: t.TempDir(), folder: t.TempDir()}
	ctx := context.Background()
	if _, err := Initialize(ctx, f.root); err != nil {
		t.Fatal(err)
	}
	f.book = mustRead(t, writeEPUBParagraph(t, t.TempDir(), "Native reparse boundary"))
	hash := sha256.Sum256(f.book)
	f.name = "books/" + hex.EncodeToString(hash[:]) + ".epub"
	writeTree(t, f.folder, map[string][]byte{f.name: f.book})
	var err error
	f.imported, err = Import(ctx, ImportRequest{Library: f.root, Source: filepath.Join(f.folder, filepath.FromSlash(f.name)), Apply: true, Scan: true, Scanner: cleanScan{}})
	if err != nil || f.imported.Path == "" {
		t.Fatal(f.imported, err)
	}
	return f
}

func (f reparseLibraryFixture) cleanup(t *testing.T) CleanupResult {
	t.Helper()
	ctx := context.Background()
	p, err := Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder})
	if err != nil || p.Move != 1 {
		t.Fatal(p, err)
	}
	r, err := Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest})
	if err != nil || r.Moved != 1 || r.NotMoved != 0 {
		t.Fatal(r, err)
	}
	return r
}

func TestWindowsReparseIntakeAndOrdinaryRecovery(t *testing.T) {
	f := newReparseLibraryFixture(t)
	ctx := context.Background()
	outside := t.TempDir()
	writeTree(t, outside, map[string][]byte{"outside.epub": f.book})
	before := snapshotTree(t, outside)
	testfs.Junction(t, filepath.Join(f.folder, "junction"), outside)
	testfs.Symlink(t, filepath.Join(f.folder, "link.epub"), filepath.Join(outside, "outside.epub"))
	r, err := Ingest(ctx, IngestRequest{Library: f.root, Folder: f.folder, Apply: true, Scan: true, Scanner: cleanScan{}})
	if err != nil || !r.Complete || r.Imported != 1 || r.NotImported != 2 || len(r.Items) != 3 {
		t.Fatal(r, err)
	}
	for _, item := range r.Items {
		if item.Source == f.name {
			continue
		}
		kind, detail := "link", "not_followed"
		if item.Source == "junction" {
			kind, detail = "special", "not_opened"
		}
		if item.Kind != kind || item.Outcome != "not_imported" || item.Detail != detail {
			t.Fatal("intake followed a link", item)
		}
	}
	plan, err := Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder})
	if err != nil || plan.Move != 1 || plan.Keep != 2 || dispositions(plan)["junction"] != "keep" || dispositions(plan)["link.epub"] != "keep" {
		t.Fatal(plan, err)
	}
	cleanup := f.cleanup(t)
	restored, err := Restore(ctx, RestoreRequest{Library: f.root, CleanupID: cleanup.CleanupID, Apply: true})
	if err != nil || restored.Restore != 1 || restored.Changed != 0 || restored.Conflicts != 0 {
		t.Fatal(restored, err)
	}
	if got := mustRead(t, filepath.Join(f.folder, filepath.FromSlash(f.name))); string(got) != string(f.book) {
		t.Fatal("ordinary source was not restored")
	}
	cleanup = f.cleanup(t)
	p, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: cleanup.CleanupID})
	if err != nil || p.Purge != 1 {
		t.Fatal(p, err)
	}
	p, err = Purge(ctx, PurgeRequest{Library: f.root, CleanupID: cleanup.CleanupID, Apply: p.Digest})
	if err != nil || p.Purged != 1 {
		t.Fatal(p, err)
	}
	if !reflect.DeepEqual(snapshotTree(t, outside), before) {
		t.Fatal("outside source tree changed during ingest, cleanup, restore or purge")
	}
	if got := mustRead(t, filepath.Join(f.root, filepath.FromSlash(f.imported.Path))); string(got) != string(f.book) {
		t.Fatal("ordinary stored holding changed")
	}
}

func TestWindowsReparseControlAndSourceRoots(t *testing.T) {
	t.Run("source_root", func(t *testing.T) {
		f := newReparseLibraryFixture(t)
		alias := filepath.Join(t.TempDir(), "alias")
		testfs.Junction(t, alias, f.folder)
		before := snapshotTree(t, f.folder)
		if _, err := Ingest(context.Background(), IngestRequest{Library: f.root, Folder: alias, Apply: true, Scanner: cleanScan{}}); err == nil {
			t.Fatal("intake accepted a junction root")
		}
		if _, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: alias}); err == nil {
			t.Fatal("cleanup accepted a junction root")
		}
		if !reflect.DeepEqual(snapshotTree(t, f.folder), before) {
			t.Fatal("source-root refusal changed bytes")
		}
	})
	t.Run("control_root", func(t *testing.T) {
		f := newReparseLibraryFixture(t)
		control := filepath.Join(f.root, ".nemalo")
		outside := filepath.Join(t.TempDir(), "control")
		if err := os.Rename(control, outside); err != nil {
			t.Fatal(err)
		}
		before := snapshotTree(t, outside)
		testfs.Junction(t, control, outside)
		ctx := context.Background()
		if _, err := Initialize(ctx, f.root); err == nil {
			t.Fatal("initialization accepted junction control")
		}
		if r, err := LibraryStatus(ctx, f.root); err == nil && r.Status == "initialized" {
			t.Fatal("linked control reported initialized", r)
		}
		if _, err := Import(ctx, ImportRequest{Library: f.root, Source: filepath.Join(f.folder, filepath.FromSlash(f.name)), Apply: true, Scanner: cleanScan{}}); err == nil {
			t.Fatal("import accepted junction control")
		}
		if _, err := Ingest(ctx, IngestRequest{Library: f.root, Folder: f.folder, Apply: true, Scanner: cleanScan{}}); err == nil {
			t.Fatal("ingest accepted junction control")
		}
		if _, err := Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder}); err == nil {
			t.Fatal("cleanup accepted junction control")
		}
		if !reflect.DeepEqual(snapshotTree(t, outside), before) {
			t.Fatal("linked control journals changed")
		}
	})
}

func TestWindowsReparseStoredAssetCannotAuthorizeCleanup(t *testing.T) {
	f := newReparseLibraryFixture(t)
	asset := filepath.Join(f.root, filepath.FromSlash(f.imported.Path))
	parent := filepath.Dir(asset)
	outside := filepath.Join(t.TempDir(), "stored")
	if err := os.Rename(parent, outside); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, outside)
	testfs.Junction(t, parent, outside)
	controlBefore := snapshotTree(t, filepath.Join(f.root, ".nemalo"))
	plan, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
	// A stored-byte verification error aborts the plan before mutation. It
	// does not turn the redirected holding into a per-source keep result.
	if !errors.Is(err, os.ErrPathEscapes) || plan.Move != 0 || plan.Applied || len(plan.Entries) != 0 {
		t.Fatal("outside stored bytes authorized cleanup", plan, err)
	}
	if _, err := Import(context.Background(), ImportRequest{Library: f.root, Source: filepath.Join(f.folder, filepath.FromSlash(f.name)), Apply: true, Scanner: cleanScan{}}); err == nil {
		t.Fatal("import accepted a redirected store")
	}
	if !reflect.DeepEqual(snapshotTree(t, outside), before) {
		t.Fatal("redirected stored assets changed")
	}
	if !reflect.DeepEqual(snapshotTree(t, filepath.Join(f.root, ".nemalo")), controlBefore) {
		t.Fatal("refused store traversal changed control journals")
	}
	if got := mustRead(t, filepath.Join(f.folder, filepath.FromSlash(f.name))); string(got) != string(f.book) {
		t.Fatal("source lost after stored-asset refusal")
	}
}

func TestWindowsReparseHeldCopiesCannotBeRestoredOrPurged(t *testing.T) {
	for _, level := range []string{"files", "books"} {
		t.Run(level, func(t *testing.T) {
			f := newReparseLibraryFixture(t)
			cleanup := f.cleanup(t)
			files := filepath.Join(cleanup.HoldingArea, "files")
			linked := files
			if level == "books" {
				linked = filepath.Join(files, "books")
			}
			// Move, never discard, the real recovery copies. The planted target
			// has the exact expected names and bytes, inside the library store.
			retained := filepath.Join(t.TempDir(), "retained")
			if err := os.Rename(linked, retained); err != nil {
				t.Fatal(err)
			}
			stored := filepath.Dir(filepath.Join(f.root, filepath.FromSlash(f.imported.Path)))
			target := stored
			if level == "files" {
				target = t.TempDir()
				testfs.Junction(t, filepath.Join(target, "books"), stored)
			}
			before := snapshotTree(t, stored)
			recoveryBefore := snapshotTree(t, retained)
			testfs.Junction(t, linked, target)
			ctx := context.Background()
			p, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: cleanup.CleanupID})
			if err != nil || p.Purge != 0 || purgeDispositions(p)[f.name] != "held_copy_changed" {
				t.Fatal("redirected held copy authorized purge", p, err)
			}
			if r, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: cleanup.CleanupID, Apply: p.Digest}); err != nil || r.Purged != 0 {
				t.Fatal(r, err)
			}
			if r, err := Restore(ctx, RestoreRequest{Library: f.root, CleanupID: cleanup.CleanupID, Apply: true}); err != nil || r.Changed != 1 || r.Restore != 0 {
				t.Fatal("restore trusted redirected held copy", r, err)
			}
			if _, err := os.Stat(filepath.Join(f.folder, filepath.FromSlash(f.name))); !os.IsNotExist(err) {
				t.Fatal("restore published redirected bytes", err)
			}
			if !reflect.DeepEqual(snapshotTree(t, stored), before) || !reflect.DeepEqual(snapshotTree(t, retained), recoveryBefore) {
				t.Fatal("store or retained recovery copies changed")
			}
			check, _ := CheckLibrary(ctx, f.root)
			if check.Healthy || check.HeldFiles != 0 {
				t.Fatal("redirected recovery area counted as healthy held data", check)
			}
		})
	}
}

func TestWindowsReparseRestoreDestinationAndPendingCleanup(t *testing.T) {
	t.Run("restore_destination", func(t *testing.T) {
		f := newReparseLibraryFixture(t)
		cleanup := f.cleanup(t)
		outside := t.TempDir()
		writeTree(t, outside, map[string][]byte{"canary": []byte("unchanged")})
		before := snapshotTree(t, outside)
		books := filepath.Join(f.folder, "books")
		if _, err := os.Lstat(books); err == nil {
			if err := os.Remove(books); err != nil {
				t.Fatal(err)
			}
		}
		testfs.Junction(t, books, outside)
		heldBefore := snapshotTree(t, filepath.Join(cleanup.HoldingArea, "files"))
		r, err := Restore(context.Background(), RestoreRequest{Library: f.root, CleanupID: cleanup.CleanupID, Apply: true})
		if err != nil || r.Blocked != 1 || r.Restore != 0 {
			t.Fatal("restore destination junction was not blocked", r, err)
		}
		if !reflect.DeepEqual(snapshotTree(t, outside), before) || !reflect.DeepEqual(snapshotTree(t, filepath.Join(cleanup.HoldingArea, "files")), heldBefore) {
			t.Fatal("blocked restore changed destination or lost held copy")
		}
	})
	t.Run("pending_cleanup", func(t *testing.T) {
		f := newReparseLibraryFixture(t)
		ctx := context.Background()
		p, err := Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder})
		if err != nil {
			t.Fatal(err)
		}
		crash := errors.New("synthetic interruption after verified held copy")
		cleanupHook = func(stage string) error {
			if stage == "held" {
				return crash
			}
			return nil
		}
		t.Cleanup(func() { cleanupHook = nil })
		interrupted, err := Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest})
		cleanupHook = nil
		if !errors.Is(err, crash) {
			t.Fatal("interruption was not reached", interrupted, err)
		}
		books := filepath.Join(interrupted.HoldingArea, "files", "books")
		retained := filepath.Join(t.TempDir(), "retained")
		if err := os.Rename(books, retained); err != nil {
			t.Fatal(err)
		}
		before := snapshotTree(t, retained)
		testfs.Junction(t, books, retained)
		r, err := Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest})
		if !errors.Is(err, ErrStateReview) || !r.Resumed || !strings.Contains(err.Error(), "folder") {
			t.Fatal("pending cleanup accepted a junction recovery directory", r, err)
		}
		if !reflect.DeepEqual(snapshotTree(t, retained), before) {
			t.Fatal("pending cleanup changed redirected recovery copies")
		}
		if got := mustRead(t, filepath.Join(f.folder, filepath.FromSlash(f.name))); string(got) != string(f.book) {
			t.Fatal("pending cleanup removed source despite redirected recovery state")
		}
	})
}
