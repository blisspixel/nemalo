package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func issueKinds(r LibraryCheck) map[string]int {
	out := map[string]int{}
	for _, i := range r.Issues {
		out[i.Kind]++
	}
	return out
}

func TestCheckLibraryReportsAHealthyLibraryWithReviewCounts(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	importUnscanned(t, root, "One", "Two")
	r, err := CheckLibrary(ctx, root)
	if err != nil || !r.Healthy || r.Holdings != 2 || r.Verified != 2 || r.Review["unscanned"] != 2 || len(r.Issues) != 0 {
		t.Fatal(r, err)
	}
	if len(r.Next) != 1 || !strings.Contains(r.Next[0], "library rescan") {
		t.Fatal("next step", r.Next)
	}
}

func TestCheckLibraryFindsDamageWithoutRepairingIt(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	books := importUnscanned(t, root, "Changed", "Missing", "Intact")
	page, err := FindManaged(ctx, root, "", "all", "all", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{}
	for _, h := range page.Holdings {
		paths[h.ID] = filepath.Join(root, filepath.FromSlash(h.Path))
	}
	changed, missing := paths[books[0].AssetID], paths[books[1].AssetID]
	_ = os.Chmod(changed, 0o644)
	if err := os.WriteFile(changed, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(missing, 0o644)
	if err := os.Remove(missing); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(root, "assets", "sha256", "zz", "stray.epub")
	writeTree(t, root, map[string][]byte{"assets/sha256/zz/stray.epub": []byte("x"), "assets/sha256/zz/half.epub.partial": []byte("y")})
	r, err := CheckLibrary(ctx, root)
	kinds := issueKinds(r)
	if err == nil || r.Healthy || kinds["changed"] != 1 || kinds["missing"] != 1 || kinds["untracked"] != 1 || kinds["partial"] != 1 || r.Verified != 1 {
		t.Fatal(kinds, r.Verified, err)
	}
	if got, _ := os.ReadFile(changed); string(got) != "changed" {
		t.Fatal("check repaired a changed file")
	}
	if _, err := os.Stat(stray); err != nil {
		t.Fatal("check removed an untracked file", err)
	}
}

func TestCheckLibraryVerifiesHeldCleanupCopiesAndPendingWork(t *testing.T) {
	ctx := context.Background()
	root, folder := cleanupFixture(t)
	plan, _ := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	applied, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest})
	if err != nil {
		t.Fatal(err)
	}
	r, err := CheckLibrary(ctx, root)
	if err != nil || r.Cleanups != 1 || r.HeldFiles != 5 || !r.Healthy {
		t.Fatal(r.Cleanups, r.HeldFiles, r.Issues, err)
	}
	if err := os.WriteFile(filepath.Join(applied.HoldingArea, "files", "held.epub"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, cleanupDir, "0123456789abcdef0123456789abcdef"), 0o700); err != nil {
		t.Fatal(err)
	}
	// An interrupted cleanup in another library is pending work.
	root3, folder3 := cleanupFixture(t)
	plan3, _ := Cleanup(ctx, CleanupRequest{Library: root3, Folder: folder3})
	undo := crashAt("moved")
	_, _ = Cleanup(ctx, CleanupRequest{Library: root3, Folder: folder3, Apply: plan3.Digest})
	undo()
	r, err = CheckLibrary(ctx, root)
	if kinds := issueKinds(r); err == nil || kinds["held_changed"] != 1 || kinds["cleanup_orphan"] != 1 {
		t.Fatal(kinds, err)
	}
	r3, err := CheckLibrary(ctx, root3)
	if kinds := issueKinds(r3); err == nil || kinds["pending_cleanup"] != 1 {
		t.Fatal(kinds, err)
	}
}

func TestCheckLibraryReportsAPendingImport(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	source := writeEPUBParagraph(t, t.TempDir(), "Pending")
	if _, err := importLibrary(ctx, ImportRequest{Library: root, Source: source, Apply: true}, func(stage string) error {
		if stage == "intent" {
			return errors.New("crash")
		}
		return nil
	}); err == nil {
		t.Fatal("crash not reached")
	}
	r, err := CheckLibrary(ctx, root)
	if kinds := issueKinds(r); err == nil || kinds["pending_import"] != 1 {
		t.Fatal(kinds, err)
	}
	if _, err := CheckLibrary(ctx, t.TempDir()); err == nil {
		t.Fatal("uninitialized folder checked")
	}
}
