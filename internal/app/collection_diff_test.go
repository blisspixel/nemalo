package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/pdftext/pdftest"
)

func TestCollectionRevisionAcquiresOnlyNewSelectionsAndKeepsEarlierNotes(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := library.Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	provider := &shelf{bodies: map[string][]byte{
		"archive:one": pdftest.Pages("", "First edition."),
		"archive:two": pdftest.Pages("", "Another selected edition."),
	}}
	s := Service{Providers: map[string]Searcher{"archive": provider}, Scanner: kindScanner{}}
	dir := t.TempDir()
	before, after := filepath.Join(dir, "before.json"), filepath.Join(dir, "after.json")
	for path, body := range map[string]string{
		before: `{"schema_version":1,"name":"Old selection","items":[{"source":"archive:one","file":"one.pdf"}]}`,
		after:  `{"schema_version":1,"name":"New selection","items":[{"source":"archive:two","file":"two.pdf"}]}`,
	} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	intake := filepath.Join(t.TempDir(), "intake")
	acquire := func(collection string) CollectionAcquireResult {
		t.Helper()
		r, err := s.CollectionAcquire(ctx, CollectionAcquireRequest{Library: root, Collection: collection, Intake: intake, Apply: true, NoScan: true})
		if err != nil || !r.Complete {
			t.Fatal(r, err)
		}
		return r
	}
	acquire(before)
	if _, err := s.Ingest(ctx, library.IngestRequest{Library: root, Folder: intake, Apply: true}); err != nil {
		t.Fatal(err)
	}
	held, err := s.ManagedHoldings(ctx, root, "", "all", "all", 10, 0)
	if err != nil || held.Total != 1 {
		t.Fatal(held, err)
	}
	oldAsset := held.Holdings[0]
	assetPath := filepath.Join(root, filepath.FromSlash(oldAsset.Path))
	assetBytes, err := os.ReadFile(assetPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnableBookmarks(ctx, root); err != nil {
		t.Fatal(err)
	}
	text, err := s.Content(ctx, content.Request{SchemaVersion: 1, Catalog: root, Root: root, AssetID: oldAsset.ID, Representation: "pdf-text/1", MaxBytes: 4096})
	if err != nil || text.Locator == nil {
		t.Fatal(text, err)
	}
	if _, err := s.SaveBookmark(ctx, BookmarkSave{Library: root, AssetID: oldAsset.ID, Representation: "pdf-text/1", RepresentationID: text.RepresentationID, Unit: text.Locator.Unit, Offset: text.Locator.Start}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetBookmarkNote(ctx, root, oldAsset.ID, "My note stays with the old edition."); err != nil {
		t.Fatal(err)
	}
	control := map[string][]byte{}
	for _, name := range []string{"bookmarks.jsonl", "holdings.jsonl", "operations.jsonl"} {
		b, err := os.ReadFile(filepath.Join(root, ".nemalo", name))
		if err != nil {
			t.Fatal(err)
		}
		control[name] = b
	}
	diff, err := s.CollectionDiff(ctx, before, after)
	if err != nil || !diff.Complete || len(diff.Added) != 1 || len(diff.Removed) != 1 || len(provider.downloads) != 1 {
		t.Fatal(diff, err)
	}
	for name, original := range control {
		got, err := os.ReadFile(filepath.Join(root, ".nemalo", name))
		if err != nil || !bytes.Equal(got, original) {
			t.Fatal("comparison changed library state", name, err)
		}
	}
	if updated := acquire(after); len(updated.Steps) != 1 || updated.Steps[0].Item.Source != "archive:two" || len(provider.downloads) != 2 {
		t.Fatal(updated)
	}
	if _, err := s.Ingest(ctx, library.IngestRequest{Library: root, Folder: intake, Apply: true}); err != nil {
		t.Fatal(err)
	}
	if repeated := acquire(after); len(repeated.Steps) != 0 || len(provider.downloads) != 2 {
		t.Fatal("unchanged revision acquired again", repeated)
	}
	held, err = s.ManagedHoldings(ctx, root, "", "all", "all", 10, 0)
	if err != nil || held.Total != 2 {
		t.Fatal("updated collection removed an earlier holding", held, err)
	}
	if got, err := os.ReadFile(assetPath); err != nil || !bytes.Equal(got, assetBytes) {
		t.Fatal("removed selection changed earlier bytes", err)
	}
	if got, err := os.ReadFile(filepath.Join(root, ".nemalo", "bookmarks.jsonl")); err != nil || !bytes.Equal(got, control["bookmarks.jsonl"]) {
		t.Fatal("collection update changed earlier annotations", err)
	}
}
