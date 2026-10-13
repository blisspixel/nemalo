package tui

import (
	"archive/zip"
	"bytes"
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

func TestBookmarksSaveOnlyWhenAskedAndRestoreExactly(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := library.Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	chapter := holdTUIBook(t, ctx, root, "chapters.epub", chapterEPUB(t))
	other := holdTUIBook(t, ctx, root, "other.epub", plainEPUB(t, "OTHERWORD stays."))
	journal := filepath.Join(root, ".nemalo", "bookmarks.jsonl")
	m := newModel(ctx, app.New())
	m.resultsFocused = true
	m.switchMode("read")
	m.input.SetValue(root)
	m.secondary.SetValue(root)
	m.reader.asset = chapter.AssetID
	m.reader.representation = "epub-structure/1"
	m.Update(key(tea.KeyF6))
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	run(m, cmd)
	if len(m.entries) < 1 || !m.entries[0].navResolved {
		t.Fatalf("contents %#v status %s", m.entries, m.status)
	}
	at := reader.Position{Unit: m.entries[0].navUnit, Offset: m.entries[0].navOffset}
	run(m, m.openChapter(root, root, chapter.AssetID, m.entries[0].navEvidence, m.entries[0].navOrder, at, false))
	if m.pager == nil || m.pager.representationID == "" {
		t.Fatal("chapter did not open", m.status)
	}
	press(m, "m")
	if m.pager == nil || !strings.Contains(m.pager.notice, "bookmark enable") {
		t.Fatal(m.pager.notice, m.status)
	}
	if _, err := os.Stat(journal); !os.IsNotExist(err) {
		t.Fatal("save without enablement created a journal", err)
	}
	press(m, "q")
	if m.pager != nil || !strings.Contains(m.status, "Nothing about this reading was recorded") {
		t.Fatal(m.status)
	}
	if _, err := os.Stat(journal); !os.IsNotExist(err) {
		t.Fatal("closing created a journal", err)
	}
	if _, err := library.EnableBookmarks(ctx, root); err != nil {
		t.Fatal(err)
	}
	run(m, m.openChapter(root, root, chapter.AssetID, "nav-doc", 0, at, false))
	press(m, "m")
	if m.pager == nil || !strings.Contains(m.pager.notice, "Saved unit") || !strings.Contains(m.pager.notice, "Closing does not save a bookmark.") {
		t.Fatal(m.pager.notice, m.status)
	}
	saved := mustRead(t, journal)
	if !bytes.Contains(saved, []byte(`"representation":"epub-nav/1"`)) || !bytes.Contains(saved, []byte(`"cursor":"`)) {
		t.Fatal(string(saved))
	}
	press(m, "m")
	if got := mustRead(t, journal); !bytes.Equal(saved, got) {
		t.Fatal("duplicate save appended")
	}
	press(m, "q")
	if !strings.Contains(m.status, "Nothing about this reading was recorded") {
		t.Fatal(m.status)
	}
	if got := mustRead(t, journal); !bytes.Equal(saved, got) {
		t.Fatal("closing appended a bookmark")
	}
	press(m, "'")
	if m.pager == nil || !m.pager.chapter || !strings.Contains(m.status, "Restored the saved location") {
		t.Fatal(m.status, m.pager)
	}
	words := pagerWords(m)
	if !strings.Contains(words, "ALPHAWORD") || strings.Contains(words, "BETAWORD") {
		t.Fatal(words)
	}
	if got := mustRead(t, journal); !bytes.Equal(saved, got) {
		t.Fatal("restore wrote a bookmark")
	}
	press(m, "q")

	// The other work is saved from the last delivered read, with the pager closed.
	got, err := m.service.Content(ctx, content.Request{SchemaVersion: 1, Catalog: root, Root: root, AssetID: other.AssetID, Representation: "epub-text/2", MaxBytes: 4096})
	if err != nil || got.Locator == nil || !strings.Contains(got.Text, "OTHERWORD") {
		t.Fatal(got.Status, err)
	}
	m.reader.asset = other.AssetID
	m.remember(got)
	press(m, "m")
	if !strings.Contains(m.status, "Saved unit") {
		t.Fatal(m.status)
	}
	listed, err := library.ListBookmarks(ctx, root)
	if err != nil || len(listed.Bookmarks) != 2 {
		t.Fatal(listed.Bookmarks, err)
	}
	var chapterMark library.Bookmark
	for _, mark := range listed.Bookmarks {
		if mark.AssetID == chapter.AssetID {
			chapterMark = mark
		}
	}
	if chapterMark.Representation != "epub-nav/1" || chapterMark.Cursor == "" {
		t.Fatal("switching works replaced the first bookmark", chapterMark)
	}
	if _, err = m.service.Content(ctx, content.Request{SchemaVersion: 1, Catalog: root, Root: root, AssetID: chapter.AssetID, Representation: "epub-text/2", MaxBytes: 4096}); err != nil {
		t.Fatal(err)
	}
	again, err := library.ListBookmarks(ctx, root)
	if err != nil || len(again.Bookmarks) != 2 {
		t.Fatal(again.Bookmarks, err)
	}
	for _, mark := range again.Bookmarks {
		if mark.AssetID == chapter.AssetID && (mark.ID != chapterMark.ID || mark.Cursor != chapterMark.Cursor) {
			t.Fatal("a later read moved the bookmark", mark)
		}
	}

	stored := filepath.Join(root, filepath.FromSlash(chapter.Path))
	body, err := os.ReadFile(stored)
	if err != nil || len(body) < 2 {
		t.Fatal(err)
	}
	body[len(body)-1] ^= 0xff
	if err = os.WriteFile(stored, body, 0o600); err != nil {
		t.Fatal(err)
	}
	m.reader.asset = chapter.AssetID
	press(m, "'")
	if m.pager != nil || !strings.Contains(m.status, "bytes changed") || strings.Contains(m.report, "ALPHAWORD") {
		t.Fatal(m.status, m.report, m.pager != nil)
	}
	m.reader.asset = other.AssetID
	press(m, "'")
	if m.pager == nil || m.pager.representation != "epub-text/2" || !strings.Contains(pagerWords(m), "OTHERWORD") {
		t.Fatal(m.status, m.pager)
	}
}

func pagerWords(m *model) string {
	var words strings.Builder
	if m.pager == nil || m.pager.doc == nil {
		return ""
	}
	for _, item := range m.pager.doc.Items {
		if item.Kind == reader.Word {
			words.WriteString(item.Text)
			words.WriteByte(' ')
		}
	}
	return words.String()
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func holdTUIBook(t *testing.T, ctx context.Context, root, name string, data []byte) library.ImportResult {
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

func plainEPUB(t *testing.T, body string) []byte {
	return epubBytes(t, map[string]string{
		"META-INF/container.xml": `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`,
		"book.opf":               `<package xmlns="http://www.idpf.org/2007/opf"><metadata><title>Fixture</title><language>en</language></metadata><manifest><item id="u" href="unit.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="u"/></spine></package>`,
		"unit.xhtml":             "<html><body><p>" + body + "</p></body></html>",
	}, []string{"META-INF/container.xml", "book.opf", "unit.xhtml"})
}

func chapterEPUB(t *testing.T) []byte {
	return epubBytes(t, map[string]string{
		"META-INF/container.xml": `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`,
		"book.opf":               `<package xmlns="http://www.idpf.org/2007/opf"><metadata><title>Chapters</title><language>en</language></metadata><manifest><item id="c" href="chapter.xhtml" media-type="application/xhtml+xml"/><item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/></manifest><spine><itemref idref="c"/></spine></package>`,
		"chapter.xhtml":          `<html><body><h1 id="a">One</h1><p>ALPHAWORD stays.</p><h1 id="b">Two</h1><p>BETAWORD stays out.</p></body></html>`,
		"nav.xhtml":              `<?xml version="1.0" encoding="UTF-8"?><html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><body><nav epub:type="toc"><ol><li><a href="chapter.xhtml#a">第一章</a></li><li><a href="chapter.xhtml#b">Second</a></li></ol></nav></body></html>`,
	}, []string{"META-INF/container.xml", "book.opf", "chapter.xhtml", "nav.xhtml"})
}

func epubBytes(t *testing.T, files map[string]string, order []string) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	stored, err := z.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = stored.Write([]byte("application/epub+zip")); err != nil {
		t.Fatal(err)
	}
	for _, name := range order {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(files[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
