package tui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/reader"
)

func pagedBook(t *testing.T) (catalog, root, asset string) {
	var long strings.Builder
	for i := range 1200 {
		fmt.Fprintf(&long, "w%d ", i)
	}
	return epubBook(t, [][2]string{
		{"META-INF/container.xml", container},
		{"book.opf", `<package xmlns="http://www.idpf.org/2007/opf"><metadata><title>Paged</title><language>en</language></metadata><manifest><item id="a" href="a.xhtml" media-type="application/xhtml+xml"/><item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/></manifest><spine><itemref idref="a"/></spine></package>`},
		{"a.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><body><p id="piv">Roman preface.</p><p id="p42">PAGEFORTYTWO starts here.</p><p>` + long.String() + `</p></body></html>`},
		{"nav.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><body><nav epub:type="toc"><ol><li><a href="a.xhtml">Text</a></li></ol></nav><nav epub:type="page-list" hidden=""><ol><li><a href="a.xhtml#piv">iv</a></li><li><a href="a.xhtml#p42">42</a></li><li><a href="a.xhtml#gone">43</a></li></ol></nav></body></html>`},
	})
}

// Page labels are the EPUB's declared markers. Listing, reading from one,
// and paging from one record nothing, and a resize never changes the label
// or its location (issue 22).
func TestPageLabelsListReadAndOpenTheirExactMarker(t *testing.T) {
	catalog, root, asset := pagedBook(t)
	before := files(t, root, filepath.Dir(catalog))
	m := readModel(t, catalog, root, asset)
	m.Update(key(tea.KeyF6))
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	m.Update(cmd())
	if len(m.entries) != 3 || m.entries[0].title != "Page iv" || m.entries[1].title != "Page 42" || !strings.Contains(m.entries[2].summary, "unresolved anchor") || m.entries[2].pageRef != "" {
		t.Fatalf("entries %#v", m.entries)
	}
	if !strings.Contains(m.entries[1].detail, "not a screen page") || !strings.Contains(m.entries[1].detail, "not stated by the package") {
		t.Fatal(m.entries[1].detail)
	}
	m.selectEntry(2)
	if cmd := m.startRead(false, 0, "", false); cmd != nil || !strings.Contains(m.status, "no resolved location") {
		t.Fatal("unresolved label read", m.status)
	}
	m.selectEntry(1)
	at := reader.Position{Unit: m.entries[1].pageUnit, Offset: m.entries[1].pageOffset}
	_, cmd = m.Update(key(tea.KeyEnter))
	m.Update(cmd())
	if !strings.Contains(m.report, "PAGEFORTYTWO") || strings.Contains(m.report, "Roman preface") || !strings.Contains(m.report, "Page label 42") {
		t.Fatal(m.status, m.report)
	}
	// Continuing a page read reads on in the book's text.
	m.Update(m.startRead(false, 0, m.reader.next, false)())
	if !strings.Contains(m.report, "Representation: epub-text/2") {
		t.Fatal("continuation", m.status, m.report)
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	m.Update(cmd())
	m.selectEntry(1)
	for _, speed := range []bool{false, true} {
		run(m, m.readSelected(speed))
		if m.pager.start != at || m.pager.playing {
			t.Fatal("opened at", m.pager.start, "want", at)
		}
		w := firstWord(t, m)
		if !strings.HasPrefix(w.Text, "PAGEFORTYTWO") || w.Pos != at {
			t.Fatal(w)
		}
		m.Update(tea.WindowSizeMsg{Width: 44, Height: 18})
		if pos, _ := m.pagerPosition(); pos != at {
			t.Fatal("resize moved the marker's location", pos)
		}
		m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		press(m, "q")
	}
	if after := files(t, root, filepath.Dir(catalog)); !slices.Equal(before, after) {
		t.Fatal("page labels changed files")
	}
}

func TestABookWithoutAPageListSaysSo(t *testing.T) {
	catalog, root, asset := handoffBook(t)
	m := readModel(t, catalog, root, asset)
	m.Update(key(tea.KeyF6))
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	m.Update(cmd())
	if len(m.entries) != 1 || m.entries[0].title != "No declared page list" || !strings.Contains(m.status, "declares no page labels") {
		t.Fatal(m.entries, m.status)
	}
}
