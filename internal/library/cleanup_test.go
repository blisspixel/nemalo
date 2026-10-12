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

// cleanupFixture builds a folder and a library that holds some of it:
//   - held.epub (imported), stray.epub (not imported), notes.txt
//   - packet/ with receipt.json, intent.json, content.epub (imported)
//   - held.zip (every member imported), mixed.zip (one member is not a book)
func cleanupFixture(t *testing.T) (root, folder string) {
	t.Helper()
	ctx := context.Background()
	root, folder = t.TempDir(), t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	book := func(text string) []byte { return mustRead(t, writeEPUBParagraph(t, t.TempDir(), text)) }
	held, packet, member := book("Held"), book("Packet"), book("Member")
	zipOf := func(files map[string][]byte) []byte {
		var buf bytes.Buffer
		z := zip.NewWriter(&buf)
		for name, body := range files {
			w, err := z.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write(body); err != nil {
				t.Fatal(err)
			}
		}
		if err := z.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	writeTree(t, folder, map[string][]byte{
		"held.epub":           held,
		"stray.epub":          book("Stray"),
		"notes.txt":           []byte("notes"),
		"packet/content.epub": packet,
		"packet/receipt.json": intakeReceipt(t, packet),
		"packet/intent.json":  []byte(`{"schema_version":1}`),
		"held.zip":            zipOf(map[string][]byte{"in/member.epub": member}),
		"mixed.zip":           zipOf(map[string][]byte{"member.epub": member, "cover.txt": []byte("x")}),
	})
	for _, source := range []string{filepath.Join(folder, "held.epub"), filepath.Join(folder, "packet")} {
		if _, err := Import(ctx, ImportRequest{Library: root, Source: source, Apply: true}); err != nil {
			t.Fatal(err)
		}
	}
	memberFile := filepath.Join(t.TempDir(), "member.epub")
	if err := os.WriteFile(memberFile, member, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Import(ctx, ImportRequest{Library: root, Source: memberFile, Apply: true}); err != nil {
		t.Fatal(err)
	}
	return root, folder
}

func dispositions(r CleanupResult) map[string]string {
	out := map[string]string{}
	for _, e := range r.Entries {
		out[e.Path] = e.Disposition
	}
	return out
}

func TestCleanupPlansEveryEntryAndWritesNothing(t *testing.T) {
	root, folder := cleanupFixture(t)
	before := snapshotTree(t, folder)
	plan, err := Cleanup(context.Background(), CleanupRequest{Library: root, Folder: folder})
	if err != nil {
		t.Fatal(plan, err)
	}
	want := map[string]string{"held.epub": "move", "stray.epub": "keep", "notes.txt": "keep", "packet": "move", "held.zip": "move", "mixed.zip": "keep"}
	got := dispositions(plan)
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s: %q, want %q (%v)", k, got[k], v, got)
		}
	}
	if len(got) != len(want) || plan.Move != 3 || plan.Keep != 3 || !validHash(plan.Digest) || plan.Applied {
		t.Fatal(plan)
	}
	if _, err := os.Stat(filepath.Join(root, cleanupDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("dry run wrote a holding area")
	}
	if after := snapshotTree(t, folder); len(after) != len(before) {
		t.Fatal("dry run changed the folder")
	}
}

func TestCleanupApplyMovesOnlyHeldSourcesAndRestoreReturnsThem(t *testing.T) {
	ctx := context.Background()
	root, folder := cleanupFixture(t)
	before := snapshotTree(t, folder)
	plan, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: strings.Repeat("0", 64)}); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatal("wrong digest accepted", err)
	}
	applied, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest})
	if err != nil || !applied.Applied || applied.Moved != 3 || applied.NotMoved != 0 {
		t.Fatal(applied, err)
	}
	for _, gone := range []string{"held.epub", "held.zip", "packet/content.epub", "packet/receipt.json", "packet/intent.json"} {
		if _, err := os.Stat(filepath.Join(folder, filepath.FromSlash(gone))); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("moved source still in the folder", gone)
		}
		held := filepath.Join(applied.HoldingArea, "files", filepath.FromSlash(gone))
		if !bytes.Equal(mustRead(t, held), before[filepath.Join(folder, filepath.FromSlash(gone))]) {
			t.Fatal("holding copy differs", gone)
		}
	}
	if _, err := os.Stat(filepath.Join(folder, "packet")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("emptied packet folder kept")
	}
	for _, kept := range []string{"stray.epub", "notes.txt", "mixed.zip"} {
		if !bytes.Equal(mustRead(t, filepath.Join(folder, kept)), before[filepath.Join(folder, kept)]) {
			t.Fatal("kept source changed", kept)
		}
	}
	// The library and its other operations still read the control state.
	if state, err := LibraryStatus(ctx, root); err != nil || state.Status != "initialized" {
		t.Fatal(state, err)
	}
	// Stored bytes are untouched: audit reports only review status, no damage.
	audit, _ := AuditManaged(ctx, root)
	for _, f := range audit.Findings {
		if f.Status != "review" {
			t.Fatal("stored bytes changed", f)
		}
	}
	again, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	if err != nil || again.Move != 0 {
		t.Fatal("second plan", again, err)
	}

	preview, err := Restore(ctx, RestoreRequest{Library: root, CleanupID: applied.CleanupID})
	if err != nil || preview.Restore != 5 || preview.Applied {
		t.Fatal(preview, err)
	}
	restored, err := Restore(ctx, RestoreRequest{Library: root, CleanupID: applied.CleanupID, Apply: true})
	if err != nil || !restored.Applied || restored.Restore != 5 {
		t.Fatal(restored, err)
	}
	after := snapshotTree(t, folder)
	if len(after) != len(before) {
		t.Fatal("restore did not return every file", len(after), len(before))
	}
	for p, b := range before {
		if !bytes.Equal(after[p], b) {
			t.Fatal("restored bytes differ", p)
		}
	}
	if left := snapshotTree(t, filepath.Join(applied.HoldingArea, "files")); len(left) != 0 {
		t.Fatal("held copies left after restore", len(left))
	}
	// Restoring again finds everything present and changes nothing.
	if again, err := Restore(ctx, RestoreRequest{Library: root, CleanupID: applied.CleanupID, Apply: true}); err != nil || again.Present != 5 || again.Restore != 0 {
		t.Fatal(again, err)
	}
}

func TestCleanupRefusesChangedSourcesAndKeepsDamagedHoldings(t *testing.T) {
	ctx := context.Background()
	root, folder := cleanupFixture(t)
	plan, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	if err != nil {
		t.Fatal(err)
	}
	// A source changed after the dry run: the plan no longer matches.
	if err := os.WriteFile(filepath.Join(folder, "held.epub"), []byte("replaced"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest}); err == nil {
		t.Fatal("changed folder applied")
	}
	if got := mustRead(t, filepath.Join(folder, "held.epub")); string(got) != "replaced" {
		t.Fatal("refused apply changed the folder")
	}
	// Stored bytes that no longer verify are not a reason to move a source.
	root2, folder2 := cleanupFixture(t)
	page, err := FindManaged(ctx, root2, "Packet", "all", "all", 10, 0)
	if err != nil || page.Total != 1 {
		t.Fatal(page, err)
	}
	stored := filepath.Join(root2, filepath.FromSlash(page.Holdings[0].Path))
	_ = os.Chmod(stored, 0o644)
	if err := os.WriteFile(stored, []byte("damaged"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan2, err := Cleanup(ctx, CleanupRequest{Library: root2, Folder: folder2})
	if err != nil || dispositions(plan2)["packet"] != "keep" {
		t.Fatal("damaged holding still justified a move", dispositions(plan2), err)
	}
}

// Every crash point leaves each planned file in the folder or the holding area
// with its planned bytes, and repeating the apply finishes the cleanup.
func TestCleanupRecoversFromEveryInterruption(t *testing.T) {
	for _, stage := range []string{"manifest", "partial", "held", "moved"} {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			root, folder := cleanupFixture(t)
			before := snapshotTree(t, folder)
			plan, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
			if err != nil {
				t.Fatal(err)
			}
			cleanupHook = func(s string) error {
				if s == stage {
					return errors.New("crash")
				}
				return nil
			}
			_, err = Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest})
			cleanupHook = nil
			if err == nil {
				t.Fatal("crash not reached")
			}
			// Another folder cannot start a cleanup while this one is pending.
			if stage != "manifest" {
				if _, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: t.TempDir(), Apply: plan.Digest}); err == nil || !strings.Contains(err.Error(), "pending") {
					t.Fatal("second cleanup started over a pending one", err)
				}
			}
			done, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest})
			if err != nil || !done.Applied || done.Moved != 3 {
				t.Fatal("resume", done, err)
			}
			if stage != "manifest" && !done.Resumed {
				t.Fatal("interrupted cleanup was not resumed")
			}
			after := snapshotTree(t, folder)
			held := snapshotTree(t, filepath.Join(done.HoldingArea, "files"))
			for p, b := range before {
				rel, _ := filepath.Rel(folder, p)
				inHolding := held[filepath.Join(done.HoldingArea, "files", rel)]
				if !bytes.Equal(after[p], b) && !bytes.Equal(inHolding, b) {
					t.Fatal("planned file lost", rel)
				}
			}
			for p := range held {
				if strings.HasSuffix(p, ".partial") {
					t.Fatal("partial copy left", p)
				}
			}
		})
	}
}

func TestCleanupTreatsATamperedHoldingAreaAsReview(t *testing.T) {
	ctx := context.Background()
	root, folder := cleanupFixture(t)
	plan, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	if err != nil {
		t.Fatal(err)
	}
	cleanupHook = func(s string) error {
		if s == "held" {
			return errors.New("crash")
		}
		return nil
	}
	_, _ = Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest})
	cleanupHook = nil
	held := snapshotTree(t, filepath.Join(root, cleanupDir))
	tampered := false
	for p := range held {
		if strings.Contains(p, string(filepath.Separator)+"files"+string(filepath.Separator)) {
			if err := os.WriteFile(p, []byte("tampered"), 0o600); err != nil {
				t.Fatal(err)
			}
			tampered = true
			break
		}
	}
	if !tampered {
		t.Fatal("no held copy to tamper with")
	}
	if _, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest}); !errors.Is(err, ErrStateReview) {
		t.Fatal("tampered holding copy accepted", err)
	}
}

func TestRestoreNeverReplacesAndKeepsConflicts(t *testing.T) {
	ctx := context.Background()
	root, folder := cleanupFixture(t)
	plan, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	if err != nil {
		t.Fatal(err)
	}
	applied, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "held.epub"), []byte("a new file"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := Restore(ctx, RestoreRequest{Library: root, CleanupID: applied.CleanupID, Apply: true})
	if err != nil || r.Conflicts != 1 || r.Restore != 4 {
		t.Fatal(r, err)
	}
	if got := mustRead(t, filepath.Join(folder, "held.epub")); string(got) != "a new file" {
		t.Fatal("restore replaced a file")
	}
	if _, err := os.Stat(filepath.Join(applied.HoldingArea, "files", "held.epub")); err != nil {
		t.Fatal("conflicting held copy removed", err)
	}
	for _, bad := range []string{"", "cleanup:zz", "cleanup:" + strings.Repeat("0", 32)} {
		if _, err := Restore(ctx, RestoreRequest{Library: root, CleanupID: bad}); err == nil {
			t.Fatal("unknown cleanup accepted", bad)
		}
	}
}

func TestCleanupRefusesOverlapAndIncompleteFolders(t *testing.T) {
	ctx := context.Background()
	root, _ := cleanupFixture(t)
	inside := filepath.Join(root, "inbox")
	if err := os.Mkdir(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: inside}); err == nil || !strings.Contains(err.Error(), "contain each other") {
		t.Fatal("overlap accepted", err)
	}
	if _, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: t.TempDir(), Apply: "short"}); err == nil {
		t.Fatal("malformed digest accepted")
	}
	deep := t.TempDir()
	p := deep
	for i := 0; i < 40; i++ {
		p = filepath.Join(p, "d")
	}
	if err := os.MkdirAll(p, 0o700); err != nil {
		t.Skip("deep paths unavailable", err)
	}
	if _, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: deep}); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatal("incomplete inventory planned", err)
	}
}

// A source that cannot be removed stays where it is and is reported; its held
// copy is kept, and restore later finds it already present.
func TestCleanupReportsSourcesItCannotRemove(t *testing.T) {
	if runtime.GOOS != "windows" && os.Getuid() == 0 {
		t.Skip("root can remove files from a read-only directory")
	}
	ctx := context.Background()
	root, folder := cleanupFixture(t)
	plan, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	if err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(folder, "packet")
	if runtime.GOOS == "windows" {
		// Windows refuses to delete a file that is open without delete sharing.
		f, err := os.Open(filepath.Join(locked, "content.epub"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
	} else {
		if err := os.Chmod(locked, 0o500); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(locked, 0o700)
	}
	applied, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest})
	if err != nil || applied.Moved != 2 || applied.NotMoved != 1 {
		t.Fatal(applied.Moved, applied.NotMoved, err)
	}
	for _, e := range applied.Entries {
		if e.Path == "packet" && e.Outcome != "source_not_removed" {
			t.Fatal("outcome", e.Outcome)
		}
	}
	if _, err := os.Stat(filepath.Join(locked, "content.epub")); err != nil {
		t.Fatal("source lost", err)
	}
}

// An interrupted acquisition's content stays for review even when the same
// bytes are held, as ingest leaves it.
func TestCleanupKeepsReceiptlessPacketContent(t *testing.T) {
	ctx := context.Background()
	root, folder := cleanupFixture(t)
	held := mustRead(t, filepath.Join(folder, "held.epub"))
	writeTree(t, folder, map[string][]byte{"attempt/content.epub": held, "attempt/intent.json": []byte(`{"schema_version":1}`)})
	plan, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	if err != nil {
		t.Fatal(err)
	}
	got := dispositions(plan)
	if got["attempt/content.epub"] != "keep" || got["attempt/intent.json"] != "keep" || got["held.epub"] != "move" {
		t.Fatal(got)
	}
}
