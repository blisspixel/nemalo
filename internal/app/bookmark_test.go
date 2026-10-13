package app

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/pdftext/pdftest"
)

func TestBookmarksRestoreExactLocationsAndKeepFailures(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := library.Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	s := Service{}
	epub := holdAppBook(t, ctx, root, "book.epub", epubWithText("ALPHAWORD stays. BETAWORD follows."))
	paper := holdAppBook(t, ctx, root, "paper.pdf", pdftest.Pages("", "PAGEWORD here."))
	read := func(asset, representation string, unit, offset int) content.Result {
		t.Helper()
		got, err := s.Content(ctx, content.Request{SchemaVersion: 1, Catalog: root, Root: root, AssetID: asset, Representation: representation, Unit: unit, Offset: offset, MaxBytes: 4096})
		if err != nil || got.Locator == nil {
			t.Fatal(got.Status, err)
		}
		return got
	}
	// Retrieval before enablement records nothing.
	first := read(epub.AssetID, "epub-text/2", 0, 0)
	if !strings.Contains(first.Text, "ALPHAWORD") {
		t.Fatal(first.Text)
	}
	if _, err := os.Stat(filepath.Join(root, ".nemalo", "bookmarks.jsonl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("reading created a bookmark journal", err)
	}
	if _, err := s.ResumeBookmark(ctx, root, epub.AssetID); err == nil || !errors.Is(err, library.ErrBookmarksNotEnabled) {
		t.Fatal(err)
	}
	if _, err := s.EnableBookmarks(ctx, root); err != nil {
		t.Fatal(err)
	}
	save := func(asset, representation, id string, unit, offset int) library.Bookmark {
		t.Helper()
		mark, err := s.SaveBookmark(ctx, BookmarkSave{Library: root, AssetID: asset, Representation: representation, RepresentationID: id, Unit: unit, Offset: offset})
		if err != nil {
			t.Fatal(err)
		}
		return mark
	}
	mark := save(epub.AssetID, "epub-text/2", first.RepresentationID, first.Locator.Unit, first.Locator.Start)
	journal, err := os.ReadFile(filepath.Join(root, ".nemalo", "bookmarks.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveBookmark(ctx, BookmarkSave{Library: root, AssetID: epub.AssetID, Representation: "epub-text/2", RepresentationID: first.RepresentationID, Unit: first.Locator.Unit, Offset: first.Locator.Start}); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(root, ".nemalo", "bookmarks.jsonl")); err != nil || !bytes.Equal(journal, got) {
		t.Fatal("duplicate save appended", err)
	}
	if _, err = s.SaveBookmark(ctx, BookmarkSave{Library: root, AssetID: epub.AssetID, Representation: "epub-text/2", RepresentationID: "sha256:" + strings.Repeat("ab", 32), Unit: 0, Offset: 0}); err == nil {
		t.Fatal("changed representation id was stored")
	}
	if _, err = s.SaveBookmark(ctx, BookmarkSave{Library: root, AssetID: epub.AssetID, Representation: "epub-source/1", RepresentationID: first.RepresentationID, Unit: 0, Offset: 0}); err == nil {
		t.Fatal("source bytes were accepted as a reading location")
	}
	if _, err = s.SaveBookmark(ctx, BookmarkSave{Library: root, AssetID: epub.AssetID, Representation: "epub-nav/1", RepresentationID: first.RepresentationID, Unit: 0, Offset: 0}); err == nil {
		t.Fatal("navigation bookmark without a cursor was stored")
	}
	if got, err := os.ReadFile(filepath.Join(root, ".nemalo", "bookmarks.jsonl")); err != nil || !bytes.Equal(journal, got) {
		t.Fatal("refused save wrote", err)
	}
	noted, err := s.SetBookmarkNote(ctx, root, epub.AssetID, "my note, not the book")
	if err != nil || noted.ID != mark.ID {
		t.Fatal(noted, err)
	}
	page := read(paper.AssetID, "pdf-text/1", 0, 0)
	if !strings.Contains(page.Text, "PAGEWORD") {
		t.Fatal(page.Text)
	}
	paperMark := save(paper.AssetID, "pdf-text/1", page.RepresentationID, page.Locator.Unit, page.Locator.Start)
	later := read(epub.AssetID, "epub-text/2", 0, strings.Index(first.Text, "BETAWORD"))
	moved := save(epub.AssetID, "epub-text/2", later.RepresentationID, later.Locator.Unit, later.Locator.Start)
	if moved.ID == mark.ID || moved.Note != "my note, not the book" || moved.Offset != later.Locator.Start {
		t.Fatal(moved)
	}
	resumed, err := s.ResumeBookmark(ctx, root, epub.AssetID)
	if err != nil || resumed.Status != "restored" || resumed.Content == nil || !strings.Contains(resumed.Content.Text, "BETAWORD") || strings.Contains(resumed.Content.Text, "my note") {
		t.Fatal(resumed.Status, resumed.Content, err)
	}
	if resumed.Bookmark.Note != "my note, not the book" {
		t.Fatal(resumed.Bookmark)
	}
	other, err := s.ResumeBookmark(ctx, root, paper.AssetID)
	if err != nil || other.Status != "restored" || other.Bookmark.ID != paperMark.ID || other.Content == nil || !strings.Contains(other.Content.Text, "PAGEWORD") {
		t.Fatal(other.Status, other.Bookmark, err)
	}
	// A content read still does not move a bookmark.
	if _, err = s.Content(ctx, content.Request{SchemaVersion: 1, Catalog: root, Root: root, AssetID: epub.AssetID, Representation: "epub-text/2", MaxBytes: 4096}); err != nil {
		t.Fatal(err)
	}
	if again, err := s.ResumeBookmark(ctx, root, epub.AssetID); err != nil || again.Bookmark.ID != moved.ID || again.Bookmark.Offset != moved.Offset {
		t.Fatal(again.Bookmark, err)
	}

	stored := filepath.Join(root, filepath.FromSlash(epub.Path))
	body, err := os.ReadFile(stored)
	if err != nil || len(body) < 2 {
		t.Fatal(err)
	}
	body[len(body)-1] ^= 0xff
	if err = os.WriteFile(stored, body, 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := s.ResumeBookmark(ctx, root, epub.AssetID)
	if err == nil || changed.Status != "changed_source" || changed.Content != nil || changed.Bookmark.Note != "my note, not the book" {
		t.Fatal(changed.Status, changed.Content, err)
	}
	if other, err = s.ResumeBookmark(ctx, root, paper.AssetID); err != nil || other.Status != "restored" {
		t.Fatal("unrelated work stopped", other.Status, err)
	}
	missingID := "sha256:" + strings.Repeat("ab", 32)
	if _, err = library.SaveBookmark(ctx, root, library.BookmarkLocation{AssetID: missingID, Representation: "epub-text/2", RepresentationID: first.RepresentationID, Unit: 0, Offset: 0}); err != nil {
		t.Fatal(err)
	}
	gone, err := s.ResumeBookmark(ctx, root, missingID)
	if err == nil || gone.Status != "unavailable" || gone.Content != nil || gone.Bookmark.AssetID != missingID {
		t.Fatal(gone.Status, gone.Content, err)
	}
	if _, err = s.DisableBookmarks(ctx, root); err != nil {
		t.Fatal(err)
	}
	paused, err := s.ResumeBookmark(ctx, root, paper.AssetID)
	if err == nil || paused.Status != "not_enabled" || paused.Content != nil || paused.Bookmark.ID != paperMark.ID {
		t.Fatal(paused.Status, paused.Content, err)
	}
	if _, err = s.SaveBookmark(ctx, BookmarkSave{Library: root, AssetID: paper.AssetID, Representation: "pdf-text/1", RepresentationID: page.RepresentationID, Unit: 0, Offset: 0}); !errors.Is(err, library.ErrBookmarksDisabled) {
		t.Fatal(err)
	}
}

func holdAppBook(t *testing.T, ctx context.Context, root, name string, data []byte) library.ImportResult {
	t.Helper()
	src := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(src, data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := library.Import(ctx, library.ImportRequest{Library: root, Source: src, Apply: true})
	if err != nil || got.AssetID == "" || got.Path == "" {
		t.Fatal(got, err)
	}
	return got
}

func epubWithText(body string) []byte {
	files := map[string]string{
		"META-INF/container.xml": `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`,
		"book.opf":               `<package xmlns="http://www.idpf.org/2007/opf"><metadata><title>Fixture</title><language>en</language></metadata><manifest><item id="u" href="unit.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="u"/></spine></package>`,
		"unit.xhtml":             "<html><body><p>" + body + "</p></body></html>",
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	f, _ := z.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	_, _ = f.Write([]byte("application/epub+zip"))
	for _, name := range []string{"META-INF/container.xml", "book.opf", "unit.xhtml"} {
		w, _ := z.Create(name)
		_, _ = w.Write([]byte(files[name]))
	}
	_ = z.Close()
	return buf.Bytes()
}
