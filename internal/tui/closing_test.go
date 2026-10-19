package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/reader"
)

// managedHandoffBook is handoffBook imported into an initialized library
// with bookmarks enabled.
func managedHandoffBook(t *testing.T) (lib, asset string) {
	t.Helper()
	_, root, _ := handoffBook(t)
	lib = t.TempDir()
	ctx := context.Background()
	if _, err := library.Initialize(ctx, lib); err != nil {
		t.Fatal(err)
	}
	r, err := library.Import(ctx, library.ImportRequest{Library: lib, Source: filepath.Join(root, "book.epub"), Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := library.EnableBookmarks(ctx, lib); err != nil {
		t.Fatal(err)
	}
	return lib, r.AssetID
}

// The closing line names the declared contents entry and how far through
// the book it is, says nothing was recorded, and fits 80 columns. The exact
// unit and byte stay in the full report (issue 35).
func TestClosingNamesThePlaceAndKeepsTheExactPosition(t *testing.T) {
	catalog, root, asset := handoffBook(t)
	m := newModel(context.Background(), app.New())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	run(m, m.openPager(catalog, root, asset, "epub-text/2", reader.Position{Unit: 1, Offset: 0}, false, reader.DefaultRate))
	press(m, "q")
	if !strings.HasPrefix(m.status, "Closed at One, ") || !strings.HasSuffix(m.status, "% through. Nothing about this reading was recorded.") || len([]rune(m.status)) > 80 {
		t.Fatalf("%q", m.status)
	}
	if strings.Contains(m.status, "bookmark") {
		t.Fatal("a bookmark sentence without a bookmark", m.status)
	}
	if !strings.Contains(m.report, "Reader closed at unit 1, byte 0") || !strings.Contains(m.report, "--unit 1 --text-offset 0") {
		t.Fatal(m.report)
	}
	if m.closed == nil || m.closed.At != (reader.Position{Unit: 1}) {
		t.Fatal(m.closed)
	}
}

func TestClosingNamesASavedBookmarkOnlyWhenItIsElsewhere(t *testing.T) {
	lib, asset := managedHandoffBook(t)
	service := app.New()
	units, err := service.Content(context.Background(), content.Request{SchemaVersion: 1, Catalog: lib, Root: lib, AssetID: asset, Representation: "epub-text/2", UnitsOnly: true, MaxBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SaveBookmark(context.Background(), app.BookmarkSave{Library: lib, AssetID: asset, Representation: "epub-text/2", RepresentationID: units.RepresentationID, Unit: 0, Offset: 0}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(lib, ".nemalo", "bookmarks.jsonl"))
	for _, tc := range []struct {
		at   reader.Position
		says bool
	}{{reader.Position{Unit: 1}, true}, {reader.Position{}, false}} {
		m := newModel(context.Background(), service)
		m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		run(m, m.openPager(lib, lib, asset, "epub-text/2", tc.at, false, reader.DefaultRate))
		press(m, "q")
		if says := strings.Contains(m.status, "Closing does not save a bookmark; yours is still at One."); says != tc.says {
			t.Fatalf("at %v: %q", tc.at, m.status)
		}
	}
	after, _ := os.ReadFile(filepath.Join(lib, ".nemalo", "bookmarks.jsonl"))
	if string(before) != string(after) {
		t.Fatal("closing changed the bookmark journal")
	}
}
