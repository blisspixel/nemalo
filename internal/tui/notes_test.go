package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/reader"
)

// notesBook has, in its first document: a declared note reference to an
// endnote in another file, a Gutenberg-style link to an anchor at the start
// of a footnote paragraph, an ordinary link, an external link, a missing
// target, and a chain of nested notes deeper than the limit. The footnote
// links back to its reference, and one endnote is longer than a range.
func notesBook(t *testing.T) (catalog, root, asset string) {
	var long strings.Builder
	for i := range 3000 {
		fmt.Fprintf(&long, "long%d ", i)
	}
	var chain strings.Builder
	for i := range 6 {
		fmt.Fprintf(&chain, `<aside epub:type="footnote" id="c%d"><p>Chain %d <a epub:type="noteref" href="notes.xhtml#c%d">deeper</a>.</p></aside>`, i, i, i+1)
	}
	chain.WriteString(`<aside epub:type="footnote" id="c6"><p>Chain end.</p></aside>`)
	main := `<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><body>` +
		`<p id="ref1">The passage cites an endnote<a epub:type="noteref" href="notes.xhtml#n1">1</a> and a footnote<a id="FNanchor_1" href="#Footnote_1">[A]</a>, ` +
		`then <a href="notes.xhtml#n1">the same endnote again</a>, a <a href="https://example.org/x">web page</a>, a <a href="#nowhere">missing note</a>, ` +
		`a <a epub:type="noteref" href="notes.xhtml#long">long note</a>, and a <a epub:type="noteref" href="notes.xhtml#c0">chain</a>.</p>` +
		`<p>MAINTEXT continues here.</p>` +
		`<div class="footnote"><p><a id="Footnote_1" href="#FNanchor_1">[A]</a> FOOTNOTETEXT explains the passage.</p></div>` +
		`</body></html>`
	notes := `<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><body>` +
		`<aside epub:type="endnote" id="n1"><p>ENDNOTETEXT one.</p></aside>` +
		`<aside epub:type="endnote" id="long"><p>` + long.String() + `LONGEND</p></aside>` +
		`<aside epub:type="endnote" id="after"><p>AFTERNOTE must not join the long note.</p></aside>` +
		chain.String() + `</body></html>`
	return epubBook(t, [][2]string{
		{"META-INF/container.xml", container},
		{"book.opf", `<package xmlns="http://www.idpf.org/2007/opf"><metadata><title>Notes</title><language>en</language></metadata><manifest><item id="a" href="a.xhtml" media-type="application/xhtml+xml"/><item id="n" href="notes.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="a"/><itemref idref="n"/></spine></package>`},
		{"a.xhtml", main},
		{"notes.xhtml", notes},
	})
}

func openNotesReader(t *testing.T, speed bool) (m *model, catalog, root string) {
	t.Helper()
	catalog, root, asset := notesBook(t)
	m = readModel(t, catalog, root, asset)
	run(m, m.openPager(catalog, root, asset, "epub-text/2", reader.Position{}, speed, reader.DefaultRate))
	if m.pager == nil || m.pager.doc == nil {
		t.Fatal("reader did not open")
	}
	return m, catalog, root
}

func linkLabels(m *model) []string {
	var out []string
	for _, l := range m.pager.notes.links {
		kind := "link"
		if l.noteref {
			kind = "note"
		}
		out = append(out, kind+":"+l.label+":"+l.status)
	}
	return out
}

func choose(t *testing.T, m *model, label string) {
	t.Helper()
	for i, l := range m.pager.notes.links {
		if l.label == label {
			m.pager.notes.selected = i
			_, cmd := m.Update(key(tea.KeyEnter))
			run(m, cmd)
			return
		}
	}
	t.Fatalf("no link %q in %v", label, linkLabels(m))
}

func noteText(m *model) string {
	f := m.pager.notes.top()
	var b strings.Builder
	for _, it := range f.doc.Items {
		if it.Kind == reader.Word {
			b.WriteString(it.Text + " ")
		}
	}
	return b.String()
}

func TestNotesListTheirSourceAndReturnToThePassage(t *testing.T) {
	m, catalog, root := openNotesReader(t, false)
	before := files(t, root, filepath.Dir(catalog))
	start, _ := m.pagerPosition()
	press(m, "f")
	got := linkLabels(m)
	// The footnote shown on the same page lists its own back link last.
	want := []string{"note:1:resolved", "link:[A]:resolved", "link:the same endnote again:resolved", "link:web page:external_not_fetched", "link:missing note:missing_fragment", "note:long note:resolved", "note:chain:resolved", "link:[A]:resolved"}
	if !slices.Equal(got, want) {
		t.Fatalf("links\n got %v\nwant %v", got, want)
	}
	view := m.View().Content
	if !strings.Contains(view, "Note reference 1: 1 (in this book)") || !strings.Contains(view, "Link 4: web page (outside the book; never fetched)") || !strings.Contains(view, "Link 2: [A] (in this book; it links back)") {
		t.Fatal("listing labels", view)
	}
	// Outside and missing targets are never followed.
	choose(t, m, "web page")
	if len(m.pager.notes.frames) != 0 || !strings.Contains(m.pager.notes.notice, "never fetched") {
		t.Fatal("external link followed", m.pager.notes.notice)
	}
	choose(t, m, "missing note")
	if len(m.pager.notes.frames) != 0 {
		t.Fatal("missing target followed")
	}
	// A cross-file endnote: only its part.
	choose(t, m, "1")
	if text := noteText(m); !strings.Contains(text, "ENDNOTETEXT") || strings.Contains(text, "long0") || strings.Contains(text, "MAINTEXT") {
		t.Fatalf("endnote %q", text)
	}
	// Bookmarks stay with the passage.
	press(m, "m")
	if !strings.Contains(m.pager.notes.notice, "main passage") {
		t.Fatal(m.pager.notes.notice)
	}
	m.Update(tea.WindowSizeMsg{Width: 50, Height: 20})
	press(m, "esc")
	if m.pager.notes.active() || !strings.Contains(m.pager.notice, "Back at the passage") {
		t.Fatal("did not return")
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if at, _ := m.pagerPosition(); at != start {
		t.Fatalf("returned to %v, left %v", at, start)
	}
	// A link to an anchor shows the paragraph it starts, and says so.
	press(m, "f")
	choose(t, m, "[A]")
	f := m.pager.notes.top()
	if text := noteText(m); !strings.Contains(text, "FOOTNOTETEXT") || strings.Contains(text, "MAINTEXT") || !strings.Contains(f.note, "id:Footnote_1") {
		t.Fatalf("footnote %q note %q", text, f.note)
	}
	press(m, "esc")
	if after := files(t, root, filepath.Dir(catalog)); !slices.Equal(before, after) {
		t.Fatal("notes changed files")
	}
}

func TestALongNoteContinuesExplicitlyAndStopsAtItsEnd(t *testing.T) {
	m, _, _ := openNotesReader(t, false)
	press(m, "f")
	choose(t, m, "long note")
	f := m.pager.notes.top()
	if f.next == "" || strings.Contains(noteText(m), "LONGEND") {
		t.Fatal("long note fit one range", f.next)
	}
	for range 400 {
		if f.next == "" {
			break
		}
		press(m, "space")
	}
	text := noteText(m)
	if !strings.Contains(text, "LONGEND") || strings.Contains(text, "AFTERNOTE") {
		t.Fatal("note continued past its end or did not finish")
	}
	for range 400 {
		if strings.Contains(m.pager.notes.notice, "End of this note") {
			break
		}
		press(m, "space")
	}
	if !strings.Contains(m.pager.notes.notice, "End of this note") {
		t.Fatal(m.pager.notes.notice)
	}
}

func TestNestedNotesStopAtTheLimitAndUnwindToThePassage(t *testing.T) {
	m, _, _ := openNotesReader(t, false)
	start, _ := m.pagerPosition()
	press(m, "f")
	choose(t, m, "chain")
	for depth := 2; depth <= maxNoteDepth; depth++ {
		press(m, "f")
		choose(t, m, "deeper")
		if len(m.pager.notes.frames) != depth || !strings.Contains(noteText(m), fmt.Sprintf("Chain %d", depth-1)) {
			t.Fatal("depth", depth, len(m.pager.notes.frames), noteText(m))
		}
	}
	press(m, "f")
	choose(t, m, "deeper")
	if len(m.pager.notes.frames) != maxNoteDepth || !strings.Contains(m.pager.notes.notice, "deep at most") {
		t.Fatal("limit not kept", len(m.pager.notes.frames), m.pager.notes.notice)
	}
	for range maxNoteDepth + 1 {
		press(m, "esc")
	}
	if m.pager == nil || m.pager.notes.active() {
		t.Fatal("unwinding closed the reader or stayed in a note")
	}
	if at, _ := m.pagerPosition(); at != start {
		t.Fatal("position moved", at, start)
	}
}

func TestNotesPauseWordModeAndIgnoreLateResults(t *testing.T) {
	scheduled(t)
	m, _, root := openNotesReader(t, true)
	press(m, "space")
	if !m.pager.playing {
		t.Fatal("did not play")
	}
	word := m.pager.word
	press(m, "f")
	if m.pager.playing || !m.pager.notes.listing {
		t.Fatal("notes did not pause word mode")
	}
	// A result for a note left before it arrived is ignored.
	m.pager.notes.selected = 0
	_, cmd := m.Update(key(tea.KeyEnter))
	press(m, "esc")
	if cmd != nil {
		m.Update(cmd())
	}
	if m.pager.notes.active() {
		t.Fatal("a late note result reopened a note")
	}
	if m.pager.playing || m.pager.word != word || !strings.Contains(m.pager.notice, "paused") {
		t.Fatal("word mode did not return paused at its word", m.pager.playing, m.pager.word, word)
	}
	// A note that cannot be read leaves the passage as it was.
	press(m, "f")
	path := filepath.Join(root, "book.epub")
	data, _ := os.ReadFile(path)
	if err := os.WriteFile(path, append(data, 0), 0o600); err != nil {
		t.Fatal(err)
	}
	m.pager.notes.selected = 0
	_, cmd = m.Update(key(tea.KeyEnter))
	run(m, cmd)
	if f := m.pager.notes.top(); f == nil || f.failure == "" {
		t.Fatal("changed bytes were not reported in the note")
	}
	press(m, "esc")
	if m.pager.word != word || m.pager.notes.active() {
		t.Fatal("failure lost the passage")
	}
}

func TestPDFHasNoLinksToFollow(t *testing.T) {
	m, _, _ := openNotesReader(t, false)
	m.pager.representation = "pdf-text/1"
	press(m, "f")
	if m.pager.notes.active() || !strings.Contains(m.pager.notice, "PDF") {
		t.Fatal(m.pager.notice)
	}
	_ = content.Result{}
}
