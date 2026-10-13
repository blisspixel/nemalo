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
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/reader"
)

func navigationBook(t *testing.T) (catalog, root, asset string) {
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
	files := []struct{ name, data string }{
		{"META-INF/container.xml", `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`},
		{"book.opf", `<package xmlns="http://www.idpf.org/2007/opf"><metadata><title>Chapters</title><language>en</language></metadata><manifest><item id="c" href="chapter.xhtml" media-type="application/xhtml+xml"/><item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/></manifest><spine><itemref idref="c"/></spine></package>`},
		{"chapter.xhtml", `<html><body><h1 id="a">One</h1><p>ALPHAWORD stays.</p><h1 id="b">Two</h1><p>BETAWORD stays out.</p></body></html>`},
		{"nav.xhtml", `<?xml version="1.0" encoding="UTF-8"?><html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><body><nav epub:type="toc"><ol><li><a href="chapter.xhtml#a">第一章</a><ol><li><a href="chapter.xhtml#a">第一章</a></li></ol></li><li><a href="chapter.xhtml#b">Second</a></li></ol></nav></body></html>`},
	}
	for _, file := range files {
		w, err := z.Create(file.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(file.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	root = t.TempDir()
	if err = os.WriteFile(filepath.Join(root, "book.epub"), buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog = filepath.Join(t.TempDir(), "catalog.json")
	service := app.New()
	if _, err = service.Snapshot(context.Background(), root, catalog, inventory.Defaults(), false); err != nil {
		t.Fatal(err)
	}
	snap, err := library.Load(catalog)
	if err != nil || len(snap.Assets) != 1 {
		t.Fatal(err)
	}
	return catalog, root, snap.Assets[0].ID
}

func TestNavigationListAndBoundedRead(t *testing.T) {
	catalog, root, asset := navigationBook(t)
	m := newModel(context.Background(), app.New())
	m.switchMode("read")
	m.reader.asset = asset
	m.reader.representation = "epub-structure/1"
	m.input.SetValue(catalog)
	m.secondary.SetValue(root)
	m.Update(key(tea.KeyF6))
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	if cmd == nil {
		t.Fatal("contents list did not start", m.status)
	}
	m.Update(cmd())
	if len(m.entries) != 3 || !strings.Contains(m.entries[0].title, "第一章") || !m.entries[0].nav || m.entries[0].navOrder != 0 || m.entries[1].navOrder != 1 || !m.entries[1].navResolved {
		t.Fatalf("entries %#v", m.entries)
	}
	if m.entries[0].navOrder == m.entries[1].navOrder || m.entries[0].title == m.entries[1].title {
		t.Fatal("duplicate labels collapsed", m.entries[0].title, m.entries[1].title)
	}
	at := reader.Position{Unit: m.entries[0].navUnit, Offset: m.entries[0].navOffset}
	_, cmd = m.Update(key(tea.KeyEnter))
	m.Update(cmd())
	if !strings.Contains(m.report, "ALPHAWORD") || strings.Contains(m.report, "BETAWORD") {
		t.Fatal(m.status, m.report)
	}
	// A read replaces the contents list. The pager opens the same declared entry.
	run(m, m.openChapter(catalog, root, asset, "nav-doc", 0, at, false))
	if m.pager == nil || m.pager.doc == nil {
		t.Fatal("chapter pager did not open", m.status, m.pager)
	}
	var words strings.Builder
	for _, item := range m.pager.doc.Items {
		if item.Kind == reader.Word {
			words.WriteString(item.Text)
			words.WriteByte(' ')
		}
	}
	if !strings.Contains(words.String(), "ALPHAWORD") || strings.Contains(words.String(), "BETAWORD") {
		t.Fatalf("pager %q notice %q", words.String(), m.pager.notice)
	}
	press(m, "]")
	if !strings.Contains(m.pager.notice, "one declared entry") {
		t.Fatal(m.pager.notice)
	}
	press(m, "m")
	if m.pager == nil || !strings.Contains(m.pager.notice, "initialized library") {
		t.Fatal(m.status, m.pager)
	}
	press(m, "q")
	if m.pager != nil || !strings.Contains(m.status, "Nothing about this reading was recorded") {
		t.Fatal(m.status)
	}
	if _, err := os.Stat(filepath.Join(root, ".nemalo", "bookmarks.jsonl")); !os.IsNotExist(err) {
		t.Fatal("reading created a bookmark file", err)
	}
}
