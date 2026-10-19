package tui

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/reader"
)

// epubBook writes one EPUB from its members and snapshots it.
func epubBook(t *testing.T, files [][2]string) (catalog, root, asset string) {
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
	for _, f := range files {
		w, err := z.Create(f[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(f[1])); err != nil {
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
	s, err := app.New().Snapshot(context.Background(), root, catalog, inventory.Defaults(), false)
	if err != nil {
		t.Fatal(err)
	}
	return catalog, root, s.Catalog.Assets[0].ID
}

const container = `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`

// handoffBook has a long first document of numbered words with multibyte text
// in every word, so excerpt boundaries fall inside UTF-8 runs, and a declared
// entry that continues from one spine file into the next.
func handoffBook(t *testing.T) (catalog, root, asset string) {
	var long, rest strings.Builder
	for i := range 900 {
		fmt.Fprintf(&long, "語%d ", i)
	}
	for i := range 900 {
		fmt.Fprintf(&rest, "續%d ", i)
	}
	return epubBook(t, [][2]string{
		{"META-INF/container.xml", container},
		{"book.opf", `<package xmlns="http://www.idpf.org/2007/opf"><metadata><title>Handoff</title><language>ja</language></metadata><manifest><item id="a" href="a.xhtml" media-type="application/xhtml+xml"/><item id="b" href="b.xhtml" media-type="application/xhtml+xml"/><item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/></manifest><spine><itemref idref="a"/><itemref idref="b"/></spine></package>`},
		{"a.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><body><h1 id="one">One</h1><p>` + long.String() + `</p></body></html>`},
		{"b.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><body><p>` + rest.String() + `</p><h1 id="two">Two</h1><p>TWOWORD.</p></body></html>`},
		{"nav.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><body><nav epub:type="toc"><ol><li><a href="a.xhtml#one">One</a></li><li><a href="b.xhtml#two">Two</a></li></ol></nav></body></html>`},
	})
}

func readModel(t *testing.T, catalog, root, asset string) *model {
	t.Helper()
	m := newModel(context.Background(), app.New())
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.switchMode("read")
	m.reader.asset, m.reader.representation = asset, "epub-structure/1"
	m.input.SetValue(catalog)
	m.secondary.SetValue(root)
	return m
}

// firstWord is the first word the pager delivered, with its position.
func firstWord(t *testing.T, m *model) reader.Item {
	t.Helper()
	if m.pager == nil || m.pager.doc == nil {
		t.Fatal("pager did not open", m.status)
	}
	for _, it := range m.pager.doc.Items {
		if it.Kind == reader.Word {
			return it
		}
	}
	t.Fatal("pager delivered no word", m.pager.notice, m.pager.failure)
	return reader.Item{}
}

// The reader opens where the last excerpt began, not at its unit's start and
// not at its continuation, which names text not yet delivered (issue 20).
func TestPagesOpenAtTheExcerptJustRead(t *testing.T) {
	catalog, root, asset := handoffBook(t)
	before := files(t, root, filepath.Dir(catalog))
	m := readModel(t, catalog, root, asset)
	m.Update(m.startRead(false, 0, "", false)())
	m.Update(m.startRead(false, 0, m.reader.next, false)())
	if !m.reader.markOK || m.reader.markUnit != 0 || m.reader.markOffset == 0 {
		t.Fatal("continued excerpt not delivered", m.status, m.reader.markOffset)
	}
	at := reader.Position{Unit: 0, Offset: m.reader.markOffset}
	next := m.reader.next
	// A failed request does not replace the last successful location.
	m.Update(m.startRead(false, 0, "not-a-cursor", false)())
	if !strings.Contains(m.status, "Incomplete") || m.reader.markOffset != at.Offset {
		t.Fatal("a failed read moved the location", m.status, m.reader.markOffset)
	}
	for _, speed := range []bool{false, true} {
		run(m, m.readSelected(speed))
		w := firstWord(t, m)
		if m.pager.start != at || m.pager.representation != "epub-text/2" || m.pager.chapter {
			t.Fatalf("opened at %v in %s, want %v", m.pager.start, m.pager.representation, at)
		}
		if w.Pos.Less(at) || !strings.HasPrefix(w.Text, "語") {
			t.Fatalf("first word %q at %v", w.Text, w.Pos)
		}
		if speed != m.pager.speed || m.pager.playing {
			t.Fatal("word mode must open paused", m.pager.speed, m.pager.playing)
		}
		press(m, "q")
	}
	if next == "" {
		t.Fatal("excerpt had no continuation")
	}
	// Another root or another asset never reuses this location.
	m.secondary.SetValue(t.TempDir())
	if _, ok := m.handoff("epub-text/2"); ok {
		t.Fatal("location reused for another root")
	}
	m.secondary.SetValue(root)
	m.reader.asset = "sha256:" + strings.Repeat("0", 64)
	if _, ok := m.handoff("epub-text/2"); ok {
		t.Fatal("location reused for another asset")
	}
	m.reader.asset = asset
	if _, ok := m.handoff("pdf-text/1"); ok {
		t.Fatal("EPUB coordinates reused for PDF text")
	}
	if after := files(t, root, filepath.Dir(catalog)); !slices.Equal(before, after) {
		t.Fatal("reading and handoffs changed files", before, after)
	}
}

// A listing on screen is a deliberate choice and comes before the last
// excerpt.
func TestAListingSelectionComesBeforeTheLastExcerpt(t *testing.T) {
	catalog, root, asset := handoffBook(t)
	m := readModel(t, catalog, root, asset)
	m.Update(m.startRead(false, 0, "", false)())
	m.Update(m.startRead(false, 0, m.reader.next, false)())
	m.Update(m.startRead(true, 0, "", false)())
	if len(m.entries) != 2 {
		t.Fatal("units listing", m.entries)
	}
	m.selectEntry(1)
	run(m, m.readSelected(false))
	if m.pager.start != (reader.Position{Unit: 1}) {
		t.Fatal("selection ignored", m.pager.start)
	}
}

// A declared entry continued across a spine boundary reopens as that entry at
// the excerpt, and pages stop at its declared end.
func TestAChapterExcerptKeepsItsDeclaredEndInPages(t *testing.T) {
	catalog, root, asset := handoffBook(t)
	m := readModel(t, catalog, root, asset)
	m.Update(key(tea.KeyF6))
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	m.Update(cmd())
	if len(m.entries) != 2 || !m.entries[0].nav {
		t.Fatal("contents", m.entries)
	}
	_, cmd = m.Update(key(tea.KeyEnter))
	m.Update(cmd())
	for range 40 {
		if m.reader.markUnit == 1 || m.reader.next == "" {
			break
		}
		m.Update(m.startRead(false, 0, m.reader.next, false)())
	}
	if m.reader.markUnit != 1 || !m.reader.markNav || m.reader.markRepresentation != "epub-nav/1" {
		t.Fatal("excerpt did not cross into the second spine file", m.reader.markUnit, m.reader.markRepresentation, m.status)
	}
	at := reader.Position{Unit: 1, Offset: m.reader.markOffset}
	run(m, m.readSelected(false))
	w := firstWord(t, m)
	if !m.pager.chapter || m.pager.start != at || w.Pos.Less(at) || !strings.HasPrefix(w.Text, "續") {
		t.Fatalf("chapter %t start %v first %q at %v", m.pager.chapter, m.pager.start, w.Text, w.Pos)
	}
	for range 60 {
		press(m, "space")
	}
	for _, it := range m.pager.doc.Items {
		if strings.Contains(it.Text, "TWOWORD") {
			t.Fatal("pages continued into the next declared entry")
		}
	}
	if m.pager.notice != "End of this declared entry." {
		t.Fatal(m.pager.notice)
	}
}

// When the book's bytes change after the excerpt, the reader reports it and
// shows no text, rather than mapping the location onto other bytes.
func TestAChangedSourceIsReportedNotRemapped(t *testing.T) {
	catalog, root, asset := handoffBook(t)
	m := readModel(t, catalog, root, asset)
	m.Update(m.startRead(false, 0, "", false)())
	m.Update(m.startRead(false, 0, m.reader.next, false)())
	path := filepath.Join(root, "book.epub")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, 0), 0o600); err != nil {
		t.Fatal(err)
	}
	run(m, m.readSelected(false))
	if m.pager == nil || m.pager.failure == "" {
		t.Fatal("changed bytes were not reported", m.pager)
	}
	if m.pager.doc != nil {
		for _, it := range m.pager.doc.Items {
			if it.Kind == reader.Word {
				t.Fatal("text shown from changed bytes", it.Text)
			}
		}
	}
}
