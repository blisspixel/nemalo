package library

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Regressions from the adversarial review of source cleanup.

func crashAt(stage string) func() {
	n := 0
	cleanupHook = func(s string) error {
		if s == stage {
			n++
			if n == 1 {
				return errors.New("crash")
			}
		}
		return nil
	}
	return func() { cleanupHook = nil }
}

// A folder reached through a link into the library is an overlap, not a
// folder whose files may be moved out of the library.
func TestCleanupRefusesFoldersThatReachIntoTheLibrary(t *testing.T) {
	ctx := context.Background()
	root, _ := cleanupFixture(t)
	link := filepath.Join(t.TempDir(), "lib")
	if err := os.Symlink(root, link); err != nil {
		t.Skip("symbolic links unavailable", err)
	}
	for _, folder := range []string{link, filepath.Join(link, "assets")} {
		if _, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder}); err == nil || !strings.Contains(err.Error(), "contain each other") {
			t.Fatal("overlap through a link accepted", folder, err)
		}
		if _, err := Ingest(ctx, IngestRequest{Library: root, Folder: folder}); err == nil {
			t.Fatal("ingest through a link into the library accepted", folder)
		}
	}
}

// A user's file whose name looks like a restore partial is never removed.
func TestRestoreKeepsLookalikePartials(t *testing.T) {
	ctx := context.Background()
	root, folder := cleanupFixture(t)
	plan, _ := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	applied, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest})
	if err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(folder, "held.epub.nemalo-restore-0123456789abcdef0123456789abcdef")
	if err := os.WriteFile(mine, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(ctx, RestoreRequest{Library: root, CleanupID: applied.CleanupID, Apply: true}); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(mine); err != nil || string(got) != "mine" {
		t.Fatal("user file removed or changed", err)
	}
}

// A packet interrupted after its content moved, whose receipt then vanished,
// still restores its content.
func TestRestoreReturnsEveryHeldFileOfAPartlyMovedPacket(t *testing.T) {
	ctx := context.Background()
	root, folder := cleanupFixture(t)
	content := mustRead(t, filepath.Join(folder, "packet", "content.epub"))
	plan, _ := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	// Entries move in order: held.epub, held.zip, then the packet's content.
	n := 0
	cleanupHook = func(s string) error {
		if s == "held" {
			if n++; n == 3 {
				return errors.New("crash")
			}
		}
		return nil
	}
	_, _ = Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest})
	cleanupHook = nil
	if n < 3 {
		t.Fatal("the crash did not land inside the packet", n)
	}
	if err := os.Remove(filepath.Join(folder, "packet", "receipt.json")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	done, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range done.Entries {
		if e.Path == "packet" && e.Outcome != "source_missing" {
			t.Fatal("packet outcome", e.Outcome)
		}
	}
	if _, err := Restore(ctx, RestoreRequest{Library: root, CleanupID: done.CleanupID, Apply: true}); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, filepath.Join(folder, "packet", "content.epub")); !bytes.Equal(got, content) {
		t.Fatal("packet content not restored")
	}
}

// Archives move only when their bytes are fully accounted for and hold at
// least one held book; a packet needs a real receipt for its content.
func TestCleanupKeepsUnaccountedArchivesAndForeignReceipts(t *testing.T) {
	ctx := context.Background()
	root, folder := cleanupFixture(t)
	member := mustRead(t, filepath.Join(folder, "held.zip"))
	zipOf := func(comment string, files map[string][]byte) []byte {
		var buf bytes.Buffer
		z := zip.NewWriter(&buf)
		for name, body := range files {
			w, _ := z.Create(name)
			_, _ = w.Write(body)
		}
		_ = z.SetComment(comment)
		_ = z.Close()
		return buf.Bytes()
	}
	writeTree(t, folder, map[string][]byte{
		"empty.zip":          zipOf("", nil),
		"dirs.zip":           zipOf("", map[string][]byte{"a/": nil}),
		"prefixed.zip":       append([]byte("%PDF-1.7 prefix "), member...),
		"commented.zip":      append(member[:len(member)-2:len(member)-2], 0, 0),
		"other/content.epub": mustRead(t, filepath.Join(folder, "held.epub")),
		"other/receipt.json": []byte(`{"note":"my own receipt"}`),
	})
	// A real comment, written properly.
	r, _ := zip.NewReader(bytes.NewReader(member), int64(len(member)))
	files := map[string][]byte{}
	for _, f := range r.File {
		rc, _ := f.Open()
		var b bytes.Buffer
		_, _ = b.ReadFrom(rc)
		_ = rc.Close()
		files[f.Name] = b.Bytes()
	}
	writeTree(t, folder, map[string][]byte{"commented.zip": zipOf("hello", files)})
	plan, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	if err != nil {
		t.Fatal(err)
	}
	got := dispositions(plan)
	for _, keep := range []string{"empty.zip", "dirs.zip", "prefixed.zip", "commented.zip", "other"} {
		if got[keep] != "keep" {
			t.Fatal(keep, "planned to move", got)
		}
	}
	if got["held.zip"] != "move" {
		t.Fatal("ordinary archive not planned", got)
	}
}

// A cleanup whose folder disappeared resumes with the held files, and a stuck
// cleanup can be stopped and then restored.
func TestCleanupFinishesWithoutItsFolderAndCanBeStopped(t *testing.T) {
	ctx := context.Background()
	root, folder := cleanupFixture(t)
	plan, _ := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	undo := crashAt("moved")
	_, _ = Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest})
	undo()
	moved := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(folder, moved); err != nil {
		t.Fatal(err)
	}
	done, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest})
	if err != nil || done.Moved != 1 || done.NotMoved != 2 {
		t.Fatal("resume without the folder", done.Moved, done.NotMoved, err)
	}
	if err := os.Rename(moved, folder); err != nil {
		t.Fatal(err)
	}
	// Stop: interrupt a fresh cleanup, then close it and restore what moved.
	root2, folder2 := cleanupFixture(t)
	plan2, _ := Cleanup(ctx, CleanupRequest{Library: root2, Folder: folder2})
	undo = crashAt("moved")
	_, _ = Cleanup(ctx, CleanupRequest{Library: root2, Folder: folder2, Apply: plan2.Digest})
	undo()
	if _, err := Cleanup(ctx, CleanupRequest{Library: root2, Stop: true, Folder: root2}); err != nil {
		t.Fatal("stop", err)
	}
	if _, err := Cleanup(ctx, CleanupRequest{Library: root2, Stop: true, Folder: root2}); err == nil {
		t.Fatal("stop without a pending cleanup accepted")
	}
	stopped, err := Cleanup(ctx, CleanupRequest{Library: root2, Folder: folder2})
	if err != nil || stopped.Move != 2 {
		t.Fatal("plan after stop", stopped.Move, err)
	}
}

// No partial copy is left in the holding area or the folder after an
// interruption between publishing and removing it.
func TestInterruptedPublishLeavesNoPartials(t *testing.T) {
	ctx := context.Background()
	root, folder := cleanupFixture(t)
	plan, _ := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	undo := crashAt("linked")
	_, _ = Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest})
	undo()
	done, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest})
	if err != nil {
		t.Fatal(err)
	}
	for p := range snapshotTree(t, done.HoldingArea) {
		if strings.HasSuffix(p, ".partial") {
			t.Fatal("holding partial left", p)
		}
	}
	undo = crashAt("linked")
	_, _ = Restore(ctx, RestoreRequest{Library: root, CleanupID: done.CleanupID, Apply: true})
	undo()
	if _, err := Restore(ctx, RestoreRequest{Library: root, CleanupID: done.CleanupID, Apply: true}); err != nil {
		t.Fatal(err)
	}
	for p := range snapshotTree(t, folder) {
		if strings.Contains(p, ".nemalo-restore-") {
			t.Fatal("restore partial left", p)
		}
	}
}

// A file too large to plan stays, with a reason, and does not stop the plan.
func TestCleanupKeepsOversizedFiles(t *testing.T) {
	root, folder := cleanupFixture(t)
	f, err := os.Create(filepath.Join(folder, "big.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(257 << 20); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	plan, err := Cleanup(context.Background(), CleanupRequest{Library: root, Folder: folder})
	if err != nil || dispositions(plan)["big.mp3"] != "keep" || dispositions(plan)["held.epub"] != "move" {
		t.Fatal(dispositions(plan), err)
	}
}

// Names that differ only by case both stay.
func TestCleanupKeepsCaseCollisions(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		t.Skip("the folder filesystem is case-insensitive")
	}
	root, folder := cleanupFixture(t)
	held := mustRead(t, filepath.Join(folder, "held.epub"))
	writeTree(t, folder, map[string][]byte{"HELD.epub": held})
	plan, err := Cleanup(context.Background(), CleanupRequest{Library: root, Folder: folder})
	if err != nil || dispositions(plan)["held.epub"] != "keep" || dispositions(plan)["HELD.epub"] != "keep" {
		t.Fatal(dispositions(plan), err)
	}
}

// On Windows a read-only source that cannot be removed keeps its attribute.
func TestCleanupKeepsReadOnlyOnUnremovableSources(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows read-only attribute")
	}
	ctx := context.Background()
	root, folder := cleanupFixture(t)
	target := filepath.Join(folder, "held.epub")
	if err := os.Chmod(target, 0o444); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(target, 0o644)
	f, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	plan, _ := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	if _, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(target); err != nil || info.Mode().Perm()&0o200 != 0 {
		t.Fatal("read-only attribute changed on a kept source", info.Mode(), err)
	}
}

// Second review: a link to a directory below the library root, or a library
// named through a link into the folder, is still an overlap.
func TestCleanupRefusesLinksBelowTheLibraryRoot(t *testing.T) {
	ctx := context.Background()
	root, _ := cleanupFixture(t)
	dir := t.TempDir()
	assets := filepath.Join(dir, "assets-link")
	if err := os.Symlink(filepath.Join(root, "assets"), assets); err != nil {
		t.Skip("symbolic links unavailable", err)
	}
	if _, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: filepath.Join(assets, "sha256")}); err == nil {
		t.Fatal("folder below the library through a link accepted")
	}
	// The library reached through a link into the folder.
	outer := t.TempDir()
	stuff := filepath.Join(outer, "stuff")
	if err := os.MkdirAll(stuff, 0o700); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(stuff, "lib")
	if _, err := Initialize(ctx, mustMkdir(t, inner)); err != nil {
		t.Fatal(err)
	}
	via := filepath.Join(t.TempDir(), "x")
	if err := os.Symlink(stuff, via); err != nil {
		t.Skip(err)
	}
	if _, err := Cleanup(ctx, CleanupRequest{Library: filepath.Join(via, "lib"), Folder: outer}); err == nil {
		t.Fatal("library inside the folder through a link accepted")
	}
}

func mustMkdir(t *testing.T, p string) string {
	t.Helper()
	if err := os.MkdirAll(p, 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

// A folder that holds another library's control state is never cleaned up.
func TestCleanupRefusesFoldersHoldingALibrary(t *testing.T) {
	ctx := context.Background()
	root, folder := cleanupFixture(t)
	if _, err := Initialize(ctx, mustMkdir(t, filepath.Join(folder, "other-library"))); err != nil {
		t.Fatal(err)
	}
	if _, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder}); err == nil || !strings.Contains(err.Error(), ".nemalo") {
		t.Fatal("folder with a library accepted", err)
	}
}

// A journal that cannot hold the whole operation refuses it before any move.
func TestCleanupRefusesWhenTheJournalCannotHoldIt(t *testing.T) {
	ctx := context.Background()
	root, folder := cleanupFixture(t)
	before := snapshotTree(t, folder)
	cleanupJournalLimit = 1024
	defer func() { cleanupJournalLimit = maxCleanupJournal }()
	plan, _ := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	if _, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest}); err == nil || !strings.Contains(err.Error(), "journal") {
		t.Fatal("full journal accepted", err)
	}
	if after := snapshotTree(t, folder); len(after) != len(before) {
		t.Fatal("refused cleanup moved files")
	}
	if _, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder}); err != nil {
		t.Fatal("library left pending", err)
	}
}

// A file that cannot be restored blocks only itself, and an interrupted
// restore can be stopped so other operations continue.
func TestRestoreBlocksPerFileAndCanBeStopped(t *testing.T) {
	ctx := context.Background()
	root, folder := cleanupFixture(t)
	plan, _ := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	applied, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "packet"), []byte("a file where the packet folder was"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := Restore(ctx, RestoreRequest{Library: root, CleanupID: applied.CleanupID, Apply: true})
	if err != nil || r.Blocked != 3 || r.Restore != 2 {
		t.Fatal("blocked restore", r.Blocked, r.Restore, err)
	}
	if _, err := os.Stat(filepath.Join(applied.HoldingArea, "files", "packet", "content.epub")); err != nil {
		t.Fatal("blocked file's held copy removed", err)
	}
	// Interrupt a later restore, then stop it.
	if err := os.Remove(filepath.Join(folder, "packet")); err != nil {
		t.Fatal(err)
	}
	undo := crashAt("restored")
	_, _ = Restore(ctx, RestoreRequest{Library: root, CleanupID: applied.CleanupID, Apply: true})
	undo()
	if _, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder}); err == nil {
		t.Fatal("cleanup started while a restore is pending")
	}
	if _, err := Restore(ctx, RestoreRequest{Library: root, CleanupID: applied.CleanupID, Stop: true}); err != nil {
		t.Fatal("stop restore", err)
	}
	if _, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder}); err != nil {
		t.Fatal("cleanup after a stopped restore", err)
	}
	final, err := Restore(ctx, RestoreRequest{Library: root, CleanupID: applied.CleanupID, Apply: true})
	if err != nil || final.Blocked != 0 {
		t.Fatal("restore after stop", final, err)
	}
	if _, err := os.Stat(filepath.Join(folder, "packet", "content.epub")); err != nil {
		t.Fatal("packet not restored after stop", err)
	}
}
