package library

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBookmarksSaveNoteDeleteAndRecover(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	journal := filepath.Join(root, ".nemalo", journalName)
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ListBookmarks(ctx, root); !errors.Is(err, ErrBookmarksNotEnabled) {
		t.Fatal(err)
	}
	if _, err := SaveBookmark(ctx, root, testLocation(1, 0, 0)); !errors.Is(err, ErrBookmarksNotEnabled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".nemalo", bookmarkName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refused save created a journal", err)
	}
	state, err := EnableBookmarks(ctx, root)
	if err != nil || !state.Enabled || state.LibraryID == "" || len(state.Bookmarks) != 0 {
		t.Fatal(state, err)
	}
	again, err := EnableBookmarks(ctx, root)
	if err != nil || again.LibraryID != state.LibraryID {
		t.Fatal(again, err)
	}
	file := filepath.Join(root, ".nemalo", bookmarkName)
	enabled, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = EnableBookmarks(ctx, root); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(file); err != nil || !bytes.Equal(enabled, got) {
		t.Fatal("repeating enable appended", err)
	}
	if got, err := os.ReadFile(journal); err != nil || !bytes.Equal(before, got) {
		t.Fatal("bookmarks rewrote the init journal", err)
	}
	if status, err := LibraryStatus(ctx, root); err != nil || status.Status != "initialized" || status.JournalRecords != 2 {
		t.Fatal(status, err)
	}

	first, err := SaveBookmark(ctx, root, testLocation(1, 2, 15))
	if err != nil || first.Unit != 2 || first.Offset != 15 || first.Note != "" || !validStateID(first.ID, "bookmark") {
		t.Fatal(first, err)
	}
	duplicate, err := SaveBookmark(ctx, root, testLocation(1, 2, 15))
	if err != nil || duplicate.ID != first.ID {
		t.Fatal(duplicate, err)
	}
	afterDuplicate, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Count(afterDuplicate, []byte("\n"))
	if lines != 2 {
		t.Fatal("duplicate save appended", lines)
	}
	noted, err := SetBookmarkNote(ctx, root, first.AssetID, "第一章 <keep>")
	if err != nil || noted.ID != first.ID || noted.Note != "第一章 <keep>" || noted.Unit != 2 {
		t.Fatal(noted, err)
	}
	moved, err := SaveBookmark(ctx, root, testLocation(1, 3, 4))
	if err != nil || moved.ID == first.ID || moved.Note != "第一章 <keep>" || moved.Unit != 3 {
		t.Fatal("new place lost the note or kept the old id", moved, err)
	}
	other, err := SaveBookmark(ctx, root, testLocation(2, 0, 8))
	if err != nil || other.AssetID == moved.AssetID || other.Note != "" {
		t.Fatal(other, err)
	}
	listed, err := ListBookmarks(ctx, root)
	if err != nil || len(listed.Bookmarks) != 2 || listed.Bookmarks[0].AssetID != moved.AssetID || listed.Bookmarks[0].Note != "第一章 <keep>" || listed.Bookmarks[1].AssetID != other.AssetID {
		t.Fatal(listed, err)
	}
	cleared, err := SetBookmarkNote(ctx, root, moved.AssetID, "")
	if err != nil || cleared.Note != "" || cleared.ID != moved.ID {
		t.Fatal(cleared, err)
	}
	if _, err := SetBookmarkNote(ctx, root, testLocation(9, 0, 0).AssetID, "no"); err == nil {
		t.Fatal("note without a bookmark succeeded")
	}
	unchanged, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DeleteBookmark(ctx, root, other.AssetID); err != nil {
		t.Fatal(err)
	}
	stable, err := os.ReadFile(file)
	if err != nil || bytes.Equal(stable, unchanged) || !bytes.Contains(stable, []byte(moved.AssetID)) || !bytes.Contains(stable, []byte(`"event":"delete"`)) {
		t.Fatal("delete did not keep history", err)
	}
	if _, err = DeleteBookmark(ctx, root, other.AssetID); err == nil {
		t.Fatal("delete of a missing bookmark wrote")
	}
	if got, err := os.ReadFile(file); err != nil || !bytes.Equal(stable, got) {
		t.Fatal("missing delete changed the journal", err)
	}
	remaining, err := ListBookmarks(ctx, root)
	if err != nil || len(remaining.Bookmarks) != 1 || remaining.Bookmarks[0].AssetID != moved.AssetID || remaining.Bookmarks[0].Note != "" {
		t.Fatal(remaining, err)
	}

	disabled, err := DisableBookmarks(ctx, root)
	if err != nil || disabled.Enabled || len(disabled.Bookmarks) != 1 {
		t.Fatal(disabled, err)
	}
	if _, err = DisableBookmarks(ctx, root); err != nil {
		t.Fatal(err)
	}
	held, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DisableBookmarks(ctx, root); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(file); err != nil || !bytes.Equal(held, got) {
		t.Fatal("repeating disable appended", err)
	}
	if _, err = SaveBookmark(ctx, root, testLocation(1, 1, 1)); !errors.Is(err, ErrBookmarksDisabled) {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(file); err != nil || !bytes.Equal(held, got) {
		t.Fatal("disabled save wrote", err)
	}
	if listed, err = ListBookmarks(ctx, root); err != nil || listed.Enabled || listed.Bookmarks[0].AssetID != moved.AssetID {
		t.Fatal(listed, err)
	}
	if _, err = EnableBookmarks(ctx, root); err != nil {
		t.Fatal(err)
	}
	if _, err = SaveBookmark(ctx, root, testLocation(2, 1, 1)); err != nil {
		t.Fatal(err)
	}
	if listed, err = ListBookmarks(ctx, root); err != nil || len(listed.Bookmarks) != 2 {
		t.Fatal("re-enable lost or merged bookmarks", listed, err)
	}
}

func TestBookmarkTailRecoveryAndClosedFailures(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	if _, err := EnableBookmarks(ctx, root); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, ".nemalo", bookmarkName)
	first, err := SaveBookmark(ctx, root, testLocation(1, 4, 5))
	if err != nil {
		t.Fatal(err)
	}
	good, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(file, append(append([]byte{}, good...), []byte(`{"torn"`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	listed, err := ListBookmarks(ctx, root)
	if err != nil || len(listed.Bookmarks) != 1 || listed.Bookmarks[0].ID != first.ID {
		t.Fatal("partial tail hid the saved bookmark", listed, err)
	}
	second, err := SaveBookmark(ctx, root, testLocation(2, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := os.ReadFile(file)
	if err != nil || bytes.Contains(recovered, []byte("torn")) || !bytes.Contains(recovered, []byte(first.ID)) || !bytes.Contains(recovered, []byte(second.ID)) {
		t.Fatal("torn tail was kept or the first bookmark was dropped", err)
	}
	if !bytes.HasSuffix(recovered, []byte("\n")) {
		t.Fatal("recovered journal has no trailing newline")
	}

	// A complete invalid line is left untouched.
	damaged := append(append([]byte{}, recovered...), []byte("{}\n")...)
	if err = os.WriteFile(file, damaged, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = SaveBookmark(ctx, root, testLocation(3, 0, 0)); !errors.Is(err, ErrStateReview) {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(file); err != nil || !bytes.Equal(damaged, got) {
		t.Fatal("invalid journal was rewritten", err)
	}

	// An entire torn line is not a record. The next save replaces it.
	if err = os.WriteFile(file, []byte(`{"not":"a record"`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = ListBookmarks(ctx, root); !errors.Is(err, ErrBookmarksNotEnabled) {
		t.Fatal(err)
	}
	if _, err = EnableBookmarks(ctx, root); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(file); err != nil || bytes.Contains(got, []byte(`"not"`)) || !bytes.HasSuffix(got, []byte("\n")) {
		t.Fatal("torn file was not replaced by enable", got, err)
	}

	fresh := t.TempDir()
	if _, err := Initialize(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	if _, err := EnableBookmarks(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	kept, err := SaveBookmark(ctx, fresh, testLocation(4, 1, 2))
	if err != nil {
		t.Fatal(err)
	}
	journalFile := filepath.Join(fresh, ".nemalo", bookmarkName)
	body, err := os.ReadFile(journalFile)
	if err != nil {
		t.Fatal(err)
	}
	torn := append(append([]byte{}, body...), []byte("partial")...)
	if err = os.WriteFile(journalFile, torn, 0o600); err != nil {
		t.Fatal(err)
	}
	previous := maxBookmarkBytes
	maxBookmarkBytes = len(body)
	t.Cleanup(func() { maxBookmarkBytes = previous })
	if _, err = SaveBookmark(ctx, fresh, testLocation(5, 0, 0)); err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatal(err)
	}
	got, err := os.ReadFile(journalFile)
	if err != nil || !bytes.Equal(body, got) {
		t.Fatal("failed save dropped the saved bookmark or kept the torn tail", err)
	}
	if listed, err = ListBookmarks(ctx, fresh); err != nil || len(listed.Bookmarks) != 1 || listed.Bookmarks[0].ID != kept.ID {
		t.Fatal(listed, err)
	}
}

func TestBookmarkExportImport(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	if _, err := EnableBookmarks(ctx, root); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveBookmark(ctx, root, testLocation(1, 2, 3)); err != nil {
		t.Fatal(err)
	}
	if _, err := SetBookmarkNote(ctx, root, testLocation(1, 0, 0).AssetID, "margin"); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveBookmark(ctx, root, testLocation(2, 0, 1)); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(root, ".nemalo", "out.json")
	if _, err := ExportBookmarks(ctx, root, inside); err == nil {
		t.Fatal("export into .nemalo succeeded")
	}
	dest := filepath.Join(t.TempDir(), "marks.json")
	exported, err := ExportBookmarks(ctx, root, dest)
	if err != nil || exported.Output != dest || len(exported.Bookmarks) != 2 {
		t.Fatal(exported, err)
	}
	if _, err = ExportBookmarks(ctx, root, dest); err == nil {
		t.Fatal("export overwrote")
	}
	raw, err := os.ReadFile(dest)
	if err != nil || !bytes.Contains(raw, []byte("margin")) || !bytes.Contains(raw, []byte(`"kind":"nemalo-bookmarks/1"`)) {
		t.Fatal(string(raw), err)
	}
	before, err := os.ReadFile(filepath.Join(root, ".nemalo", bookmarkName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ImportBookmarks(ctx, root, dest, false); err != nil {
		t.Fatal("identical import wrote or failed", err)
	}
	if got, err := os.ReadFile(filepath.Join(root, ".nemalo", bookmarkName)); err != nil || !bytes.Equal(before, got) {
		t.Fatal("identical import appended", err)
	}
	edited := bytes.Replace(raw, []byte(`"offset":3`), []byte(`"offset":9`), 1)
	other := filepath.Join(t.TempDir(), "edited.json")
	if err = os.WriteFile(other, edited, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = ImportBookmarks(ctx, root, other, false); err == nil || !strings.Contains(err.Error(), "nothing was imported") {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(root, ".nemalo", bookmarkName)); err != nil || !bytes.Equal(before, got) {
		t.Fatal("rejected import wrote", err)
	}
	if _, err = ImportBookmarks(ctx, root, other, true); err != nil {
		t.Fatal(err)
	}
	listed, err := ListBookmarks(ctx, root)
	if err != nil || len(listed.Bookmarks) != 2 {
		t.Fatal(listed, err)
	}
	var moved Bookmark
	for _, mark := range listed.Bookmarks {
		if mark.AssetID == testLocation(1, 0, 0).AssetID {
			moved = mark
		}
	}
	if moved.Offset != 9 || moved.Note != "margin" {
		t.Fatal("replace did not keep the note on the new place", moved)
	}
	malformed := filepath.Join(t.TempDir(), "bad.json")
	if err = os.WriteFile(malformed, []byte(string(raw[:len(raw)-2])+",\"extra\":true}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	held, err := os.ReadFile(filepath.Join(root, ".nemalo", bookmarkName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ImportBookmarks(ctx, root, malformed, true); err == nil {
		t.Fatal("malformed import succeeded")
	}
	if got, err := os.ReadFile(filepath.Join(root, ".nemalo", bookmarkName)); err != nil || !bytes.Equal(held, got) {
		t.Fatal("malformed import wrote", err)
	}
	if _, err = SetBookmarkNote(ctx, root, testLocation(1, 0, 0).AssetID, string(make([]byte, maxBookmarkNote+1))); err == nil {
		t.Fatal("overlong note stored")
	}
	if _, err = SetBookmarkNote(ctx, root, testLocation(1, 0, 0).AssetID, "bad\x00note"); err == nil {
		t.Fatal("NUL note stored")
	}
}

func testLocation(asset byte, unit, offset int) BookmarkLocation {
	id := strings.Repeat(string([]byte{asset + 0x30}), 64)
	if asset > 9 {
		id = strings.Repeat("ab", 32)
		if asset%2 == 1 {
			id = strings.Repeat("cd", 32)
		}
	}
	return BookmarkLocation{
		AssetID: "sha256:" + id, Representation: "epub-text/2",
		RepresentationID: "sha256:" + strings.Repeat("ef", 32), Unit: unit, Offset: offset,
	}
}
