package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// cleanedFixture is cleanupFixture after an applied cleanup: held.epub,
// held.zip, and the packet's content, receipt, and intent are held.
func cleanedFixture(t *testing.T) (root string, cleanup CleanupResult) {
	t.Helper()
	ctx := context.Background()
	root, folder := cleanupFixture(t)
	plan, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	if err != nil {
		t.Fatal(err)
	}
	cleanup, err = Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest})
	if err != nil || cleanup.Moved != 3 {
		t.Fatal(cleanup, err)
	}
	return root, cleanup
}

func purgeDispositions(r PurgeResult) map[string]string {
	out := map[string]string{}
	for _, f := range r.Files {
		out[f.Path] = f.Disposition
		if f.Outcome != "" {
			out[f.Path] = f.Outcome
		}
	}
	return out
}

// A purge removes exactly the held copies whose bytes the library holds and
// verifies, keeps a packet's records, and leaves the library's own copies.
func TestPurgeRemovesOnlyHeldCopiesTheLibraryHolds(t *testing.T) {
	ctx := context.Background()
	root, cleanup := cleanedFixture(t)
	files := filepath.Join(cleanup.HoldingArea, "files")
	before := snapshotTree(t, files)
	stored := snapshotTree(t, filepath.Join(root, "assets"))
	preview, err := Purge(ctx, PurgeRequest{Library: root, CleanupID: cleanup.CleanupID})
	if err != nil || preview.Applied || preview.Purge != 3 || preview.Keep != 2 || !validHash(preview.Digest) {
		t.Fatal(preview, err)
	}
	want := map[string]string{"held.epub": "purge", "held.zip": "purge", "packet/content.epub": "purge", "packet/receipt.json": "provenance_record", "packet/intent.json": "provenance_record"}
	for p, d := range want {
		if got := purgeDispositions(preview)[p]; got != d {
			t.Fatalf("%s: %s, want %s", p, got, d)
		}
	}
	if len(snapshotTree(t, files)) != len(before) {
		t.Fatal("a preview removed a file")
	}
	if _, err := Purge(ctx, PurgeRequest{Library: root, CleanupID: cleanup.CleanupID, Apply: strings.Repeat("0", 64)}); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatal("a wrong digest was accepted", err)
	}
	applied, err := Purge(ctx, PurgeRequest{Library: root, CleanupID: cleanup.CleanupID, Apply: preview.Digest})
	if err != nil || !applied.Applied || applied.Purged != 3 || applied.PurgedBytes != preview.PurgeBytes {
		t.Fatal(applied, err)
	}
	left := snapshotTree(t, files)
	if len(left) != 2 || left[filepath.Join(files, "packet", "receipt.json")] == nil || left[filepath.Join(files, "packet", "intent.json")] == nil {
		t.Fatal("purge left the wrong files", len(left))
	}
	if _, err := os.Stat(filepath.Join(files, "held.epub")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("held copy kept")
	}
	after := snapshotTree(t, filepath.Join(root, "assets"))
	for p, b := range stored {
		if string(after[p]) != string(b) {
			t.Fatal("a stored asset changed", p)
		}
	}
	check, err := CheckLibrary(ctx, root)
	if err != nil || !check.Healthy || check.HeldFiles != 2 {
		t.Fatal(check.Issues, check.HeldFiles, err)
	}
	// A second purge finds nothing more to remove; restore names what is gone.
	again, err := Purge(ctx, PurgeRequest{Library: root, CleanupID: cleanup.CleanupID})
	if err != nil || again.Purge != 0 || purgeDispositions(again)["held.epub"] != "not_held" {
		t.Fatal(again, err)
	}
	restore, err := Restore(ctx, RestoreRequest{Library: root, CleanupID: cleanup.CleanupID})
	if err != nil || restore.Purged != 3 || restore.Restore != 2 {
		t.Fatal(restore, err)
	}
	restored, err := Restore(ctx, RestoreRequest{Library: root, CleanupID: cleanup.CleanupID, Apply: true})
	if err != nil || restored.Purged != 3 || restored.Restore != 2 {
		t.Fatal(restored, err)
	}
}

// A held copy whose library holding no longer verifies, or whose own bytes
// changed, stays.
func TestPurgeKeepsWhatTheLibraryCannotVouchFor(t *testing.T) {
	ctx := context.Background()
	root, cleanup := cleanedFixture(t)
	files := filepath.Join(cleanup.HoldingArea, "files")
	// Damage the library's copy of held.epub, and change the held zip.
	held := mustRead(t, filepath.Join(files, "held.epub"))
	damaged := false
	for p, b := range snapshotTree(t, filepath.Join(root, "assets")) {
		if string(b) == string(held) {
			if err := os.Chmod(p, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, append([]byte("x"), b[1:]...), 0o600); err != nil {
				t.Fatal(err)
			}
			damaged = true
		}
	}
	if !damaged {
		t.Fatal("no stored copy of held.epub")
	}
	zipPath := filepath.Join(files, "held.zip")
	z := mustRead(t, zipPath)
	z[len(z)-1] ^= 0xff
	if err := os.WriteFile(zipPath, z, 0o600); err != nil {
		t.Fatal(err)
	}
	preview, err := Purge(ctx, PurgeRequest{Library: root, CleanupID: cleanup.CleanupID})
	if err != nil {
		t.Fatal(err)
	}
	d := purgeDispositions(preview)
	if d["held.epub"] != "not_in_library" || d["held.zip"] != "held_copy_changed" || d["packet/content.epub"] != "purge" || preview.Purge != 1 {
		t.Fatal(d)
	}
	applied, err := Purge(ctx, PurgeRequest{Library: root, CleanupID: cleanup.CleanupID, Apply: preview.Digest})
	if err != nil || applied.Purged != 1 {
		t.Fatal(applied, err)
	}
	for _, kept := range []string{"held.epub", "held.zip"} {
		if _, err := os.Stat(filepath.Join(files, kept)); err != nil {
			t.Fatal("a copy the library cannot vouch for was removed", kept, err)
		}
	}
}

// An interrupted purge blocks other cleanup operations and resumes from its
// own manifest, never from a new plan.
func TestPurgeResumesFromEveryInterruption(t *testing.T) {
	for _, stage := range []string{"purge manifest", "purged"} {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			root, cleanup := cleanedFixture(t)
			preview, err := Purge(ctx, PurgeRequest{Library: root, CleanupID: cleanup.CleanupID})
			if err != nil {
				t.Fatal(err)
			}
			cleanupHook = func(s string) error {
				if s == stage {
					return errors.New("crash")
				}
				return nil
			}
			_, err = Purge(ctx, PurgeRequest{Library: root, CleanupID: cleanup.CleanupID, Apply: preview.Digest})
			cleanupHook = nil
			if err == nil {
				t.Fatal("crash not reached")
			}
			if stage == "purged" {
				if _, err := Restore(ctx, RestoreRequest{Library: root, CleanupID: cleanup.CleanupID, Apply: true}); err == nil || !strings.Contains(err.Error(), "pending") {
					t.Fatal("restore ran over a pending purge", err)
				}
				if _, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "purge") {
					t.Fatal("cleanup ran over a pending purge", err)
				}
				if _, err := Purge(ctx, PurgeRequest{Library: root, CleanupID: cleanup.CleanupID}); err == nil || !strings.Contains(err.Error(), preview.Digest) {
					t.Fatal("a pending purge was not named", err)
				}
			}
			done, err := Purge(ctx, PurgeRequest{Library: root, CleanupID: cleanup.CleanupID, Apply: preview.Digest})
			if err != nil || !done.Applied || done.Purged != 3 {
				t.Fatal("resume", done, err)
			}
			if stage == "purged" && !done.Resumed {
				t.Fatal("an interrupted purge was not resumed")
			}
			if left := snapshotTree(t, filepath.Join(cleanup.HoldingArea, "files")); len(left) != 2 {
				t.Fatal("resume left", len(left))
			}
			if check, err := CheckLibrary(ctx, root); err != nil || !check.Healthy {
				t.Fatal(check.Issues, err)
			}
		})
	}
}

func TestPurgeRefusesWhatIsNotACompletedCleanup(t *testing.T) {
	ctx := context.Background()
	root, folder := cleanupFixture(t)
	if _, err := Purge(ctx, PurgeRequest{Library: root, CleanupID: "bad"}); err == nil {
		t.Fatal("an invalid ID was accepted")
	}
	if _, err := Purge(ctx, PurgeRequest{Library: root, CleanupID: "cleanup:" + strings.Repeat("a", 32)}); err == nil || !strings.Contains(err.Error(), "no completed cleanup") {
		t.Fatal("an unknown cleanup was accepted", err)
	}
	plan, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	if err != nil {
		t.Fatal(err)
	}
	cleanupHook = func(s string) error {
		if s == "moved" {
			return errors.New("crash")
		}
		return nil
	}
	_, _ = Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest})
	cleanupHook = nil
	records := pendingCleanupID(t, root)
	if _, err := Purge(ctx, PurgeRequest{Library: root, CleanupID: records}); err == nil || !strings.Contains(err.Error(), "no completed cleanup") {
		t.Fatal("a pending cleanup was purged", err)
	}
}

// pendingCleanupID reads the ID of the one cleanup in the journal.
func pendingCleanupID(t *testing.T, root string) string {
	t.Helper()
	control, _, err := openControl(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer control.close()
	state, err := LibraryStatus(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	records, err := readCleanup(control, state.LibraryID)
	if err != nil || len(records) == 0 {
		t.Fatal(records, err)
	}
	return records[0].CleanupID
}

// A link planted in the holding area that leads to the library's stored
// assets is refused at every step: the asset is never removed or counted as
// a held copy.
func TestPurgeAndRestoreNeverFollowALinkOutOfTheHoldingArea(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need privileges on Windows")
	}
	ctx := context.Background()
	root, folder := t.TempDir(), t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	book := mustRead(t, writeEPUBParagraph(t, t.TempDir(), "Linked"))
	sum := sha256.Sum256(book)
	name := hex.EncodeToString(sum[:]) + ".epub"
	writeTree(t, folder, map[string][]byte{name: book})
	imported, err := Import(ctx, ImportRequest{Library: root, Source: filepath.Join(folder, name), Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	if err != nil {
		t.Fatal(err)
	}
	cleanup, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest})
	if err != nil || cleanup.Moved != 1 {
		t.Fatal(cleanup, err)
	}
	files := filepath.Join(cleanup.HoldingArea, "files")
	if err := os.RemoveAll(files); err != nil {
		t.Fatal(err)
	}
	asset := filepath.Join(root, filepath.FromSlash(imported.Path))
	if err := os.Symlink(filepath.Dir(asset), files); err != nil {
		t.Fatal(err)
	}
	preview, err := Purge(ctx, PurgeRequest{Library: root, CleanupID: cleanup.CleanupID})
	if err != nil || preview.Purge != 0 || purgeDispositions(preview)[name] != "held_copy_changed" {
		t.Fatal(preview, err)
	}
	if _, err := Purge(ctx, PurgeRequest{Library: root, CleanupID: cleanup.CleanupID, Apply: preview.Digest}); err != nil {
		t.Fatal(err)
	}
	if r, err := Restore(ctx, RestoreRequest{Library: root, CleanupID: cleanup.CleanupID, Apply: true}); err != nil || r.Changed != 1 {
		t.Fatal(r, err)
	}
	if got := mustRead(t, asset); string(got) != string(book) {
		t.Fatal("the stored asset changed")
	}
	if check, _ := CheckLibrary(ctx, root); check.Healthy || check.HeldFiles != 0 {
		t.Fatal("a linked holding area was counted as held", check.HeldFiles, check.Issues)
	}
}

// A purge that cannot finish, here because its manifest was removed, is
// closed with Stop, and other cleanup operations work again.
func TestAPurgeThatCannotFinishCanBeStopped(t *testing.T) {
	ctx := context.Background()
	root, cleanup := cleanedFixture(t)
	preview, err := Purge(ctx, PurgeRequest{Library: root, CleanupID: cleanup.CleanupID})
	if err != nil {
		t.Fatal(err)
	}
	removals := 0
	cleanupHook = func(s string) error {
		if s == "purged" {
			if removals++; removals == 2 {
				return errors.New("crash")
			}
		}
		return nil
	}
	_, _ = Purge(ctx, PurgeRequest{Library: root, CleanupID: cleanup.CleanupID, Apply: preview.Digest})
	cleanupHook = nil
	manifests, _ := filepath.Glob(filepath.Join(cleanup.HoldingArea, "purge-*.json"))
	for _, m := range manifests {
		if err := os.Remove(m); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Purge(ctx, PurgeRequest{Library: root, CleanupID: cleanup.CleanupID, Apply: preview.Digest}); !errors.Is(err, ErrStateReview) {
		t.Fatal("a purge without its manifest resumed", err)
	}
	if _, err := Restore(ctx, RestoreRequest{Library: root, CleanupID: cleanup.CleanupID}); err == nil || !strings.Contains(err.Error(), "--stop") {
		t.Fatal("the pending purge was not named with its way out", err)
	}
	if _, err := Purge(ctx, PurgeRequest{Library: root, CleanupID: cleanup.CleanupID, Stop: true, Apply: preview.Digest}); err == nil {
		t.Fatal("stop accepted a plan")
	}
	stopped, err := Purge(ctx, PurgeRequest{Library: root, CleanupID: cleanup.CleanupID, Stop: true})
	if err != nil || !stopped.Applied || stopped.Purged != 1 {
		t.Fatal(stopped, err)
	}
	// The second removal happened before the interruption could journal it,
	// so with its manifest gone that copy is simply not held.
	restore, err := Restore(ctx, RestoreRequest{Library: root, CleanupID: cleanup.CleanupID, Apply: true})
	if err != nil || restore.Purged != 1 || restore.NotHeld != 1 || restore.Restore != 3 {
		t.Fatal(restore, err)
	}
	if _, err := Purge(ctx, PurgeRequest{Library: root, CleanupID: cleanup.CleanupID, Stop: true}); err == nil {
		t.Fatal("stop without a pending purge")
	}
}

// A held copy that cannot be removed stays with its outcome; the purge still
// finishes rather than staying pending.
func TestAHeldCopyThatCannotBeRemovedStays(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a folder the test user cannot write")
	}
	ctx := context.Background()
	root, cleanup := cleanedFixture(t)
	files := filepath.Join(cleanup.HoldingArea, "files")
	preview, err := Purge(ctx, PurgeRequest{Library: root, CleanupID: cleanup.CleanupID})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(files, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(files, 0o700) })
	applied, err := Purge(ctx, PurgeRequest{Library: root, CleanupID: cleanup.CleanupID, Apply: preview.Digest})
	if err != nil || !applied.Applied || purgeDispositions(applied)["held.epub"] != "not_removed" || applied.Purged != 1 {
		t.Fatal(purgeDispositions(applied), applied.Purged, err)
	}
	if _, err := os.Stat(filepath.Join(files, "held.epub")); err != nil {
		t.Fatal("a copy reported as not removed is gone", err)
	}
	if _, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: t.TempDir()}); err != nil {
		t.Fatal("the purge stayed pending", err)
	}
}

// A partial that is a second name for a purged copy goes with it; one that
// is a different file stays.
func TestPurgeRemovesAPartialThatIsTheSameFile(t *testing.T) {
	ctx := context.Background()
	root, cleanup := cleanedFixture(t)
	files := filepath.Join(cleanup.HoldingArea, "files")
	if err := os.Link(filepath.Join(files, "held.epub"), filepath.Join(files, "held.epub.partial")); err != nil {
		t.Skip("hard links unavailable", err)
	}
	if err := os.WriteFile(filepath.Join(files, "held.zip.partial"), []byte("other"), 0o600); err != nil {
		t.Fatal(err)
	}
	preview, err := Purge(ctx, PurgeRequest{Library: root, CleanupID: cleanup.CleanupID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Purge(ctx, PurgeRequest{Library: root, CleanupID: cleanup.CleanupID, Apply: preview.Digest}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(files, "held.epub.partial")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a second name for a purged copy stayed", err)
	}
	if b, err := os.ReadFile(filepath.Join(files, "held.zip.partial")); err != nil || string(b) != "other" {
		t.Fatal("a different partial was removed", err)
	}
}
