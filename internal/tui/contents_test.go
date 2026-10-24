package tui

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/pdftext/pdftest"
	"github.com/blisspixel/nemalo/internal/reader"
	"github.com/blisspixel/nemalo/internal/scanner"
)

type contentsNoScanner struct{ t *testing.T }

func (s contentsNoScanner) Scan(context.Context, string) scanner.Result {
	s.t.Fatal("contents invoked a scanner")
	return scanner.Result{}
}

func contentsService(t *testing.T) app.Service {
	return app.Service{Scanner: contentsNoScanner{t}}
}

func pendingContents(t *testing.T, m *model) tea.Cmd {
	t.Helper()
	cmd := m.openContents()
	if cmd == nil {
		t.Fatal("no contents command")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("unexpected command %#v", batch)
	}
	return batch[1]
}

func TestFullscreenContentsFollowsOneDeclaredEntry(t *testing.T) {
	catalog, root, asset := navigationBook(t)
	before := files(t, root, filepath.Dir(catalog))
	scheduled(t)
	m := newModel(context.Background(), contentsService(t))
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	run(m, m.openPager(catalog, root, asset, "epub-text/2", reader.Position{}, true, 425))
	m.pager.only = true
	press(m, "space")
	p := m.pager
	oldGen := p.gen
	word := p.word
	press(m, "c")
	if p.chooser == nil || len(p.chooser.choices) != 3 || p.playing || p.waiting {
		t.Fatal("contents failed to pause", p.chooser)
	}
	m.Update(pagerTick{gen: oldGen})
	if p.word != word {
		t.Fatal("text advanced behind contents")
	}
	if p.chooser.choices[0].label != p.chooser.choices[1].label || p.chooser.choices[0].entry.Order == p.chooser.choices[1].entry.Order {
		t.Fatal("duplicate declarations collapsed")
	}
	press(m, "enter")
	p = m.pager
	if !p.chapter || p.navEvidence != "nav-doc" || p.navOrder != 0 || !p.only || !p.speed || p.rate != 425 || p.playing || p.chooser != nil {
		t.Fatalf("selection lost source or reader controls: %#v", p)
	}
	var words []string
	for _, it := range p.doc.Items {
		words = append(words, it.Text)
	}
	text := strings.Join(words, " ")
	if !strings.Contains(text, "ALPHAWORD") || strings.Contains(text, "BETAWORD") {
		t.Fatal("read crossed declared end", text)
	}
	press(m, "]")
	if !strings.Contains(p.notice, "one declared entry") {
		t.Fatal("chapter changed implicitly", p.notice)
	}
	press(m, "c", "G", "enter")
	if m.pager.navOrder != 2 || !strings.Contains(m.pagerView(), "BETAWORD") {
		t.Fatal("could not select another entry", m.pagerView())
	}
	if cmd := m.closePager(); cmd == nil {
		t.Fatal("CLI-only reader did not quit")
	}
	if got := files(t, root, filepath.Dir(catalog)); !slices.Equal(before, got) {
		t.Fatal("contents changed source or state")
	}
}

func TestContentsCancellationAndLateResultsKeepThePassage(t *testing.T) {
	catalog, root, asset, _ := readerBook(t, false)
	scheduled(t)
	m := newModel(context.Background(), contentsService(t))
	run(m, m.openPager(catalog, root, asset, "epub-text/2", reader.Position{}, true, 250))
	p := m.pager
	p.word = 8
	at, _ := m.pagerPosition()
	delayed := pendingContents(t, m)
	result := delayed()
	press(m, "esc")
	m.Update(result)
	if p.chooser != nil || p.playing {
		t.Fatal("late response reopened chooser")
	}
	if got, _ := m.pagerPosition(); got != at {
		t.Fatal("escape moved passage", at, got)
	}
	delayed = pendingContents(t, m)
	press(m, "c")
	cancelled := delayed().(contentsResult)
	if !errors.Is(cancelled.err, context.Canceled) {
		t.Fatal("escape did not cancel source work", cancelled.err)
	}
	next := pendingContents(t, m)
	m.Update(result)
	if !p.chooser.fetching || len(p.chooser.choices) != 0 {
		t.Fatal("earlier open populated new chooser")
	}
	run(m, next)
	if !strings.Contains(p.chooser.notice, "No source-declared contents") {
		t.Fatal(p.chooser.notice)
	}
	delayed = pendingContents(t, m)
	oldResult := contentsResult{session: p.session, request: p.chooser.request, data: content.Result{Navigation: []content.NavDocument{{Evidence: "nav-doc", Problem: "unavailable"}}}}
	run(m, m.openPager(catalog, root, asset, "epub-text/2", reader.Position{}, false, 250))
	m.Update(oldResult)
	if m.pager.chooser != nil {
		t.Fatal("old session response applied")
	}
	if !errors.Is(delayed().(contentsResult).err, context.Canceled) {
		t.Fatal("replacement did not cancel chooser")
	}
	pendingContents(t, m)
	m.closePager()
	m.Update(oldResult)
	if m.pager != nil {
		t.Fatal("late response reopened closed reader")
	}
}

func TestContentsProblemsLimitsAndKeyboardAreExplicit(t *testing.T) {
	catalog, root, asset := navigationBook(t)
	m := newModel(context.Background(), contentsService(t))
	run(m, m.openPager(catalog, root, asset, "epub-text/2", reader.Position{}, false, 250))
	pendingContents(t, m)
	n := m.pager.chooser
	entries := make([]content.NavEntry, maxContentsChoices+1)
	zero := 0
	for i := range entries {
		entries[i] = content.NavEntry{Label: "Untrusted\x1b[31m\nlabel", Level: 64, Order: i, Status: "unresolved_anchor"}
	}
	m.contentsResult(contentsResult{session: m.pager.session, request: n.request, data: content.Result{Navigation: []content.NavDocument{{Evidence: "nav-doc", Entries: entries}}}})
	if len(n.choices) != maxContentsChoices || !strings.Contains(n.notice, "limited") {
		t.Fatal("missing display bound", len(n.choices), n.notice)
	}
	press(m, "?")
	if !strings.Contains(m.pagerView(), "Contents keys") || !strings.Contains(m.pagerView(), "return to the choices") {
		t.Fatal("chooser help described passage controls", m.pagerView())
	}
	press(m, "?")
	m.contentsResult(contentsResult{session: m.pager.session, request: n.request, data: content.Result{Navigation: []content.NavDocument{{Evidence: "ncx", Problem: "late duplicate"}}}})
	if len(n.choices) != maxContentsChoices {
		t.Fatal("duplicate response changed choices")
	}
	p := m.pager
	press(m, "enter")
	if m.pager != p || !strings.Contains(n.notice, "no resolved") {
		t.Fatal("unresolved entry opened")
	}
	for _, width := range []int{40, 80, 116} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		view := m.pagerView()
		if strings.Contains(view, "\x1b") || !strings.Contains(view, "q close") {
			t.Fatal("unsafe contents frame", view)
		}
		lines := strings.Split(view, "\n")
		if len(lines) != 24 {
			t.Fatal("frame height", len(lines))
		}
		for _, line := range lines {
			if lipgloss.Width(line) != width {
				t.Fatal("frame width", width, lipgloss.Width(line))
			}
		}
	}
	for _, k := range []string{"j", "down", "space", "pgdown", "G", "end", "k", "up", "b", "pgup", "g", "home"} {
		press(m, k)
		if n.selected < 0 || n.selected >= len(n.choices) {
			t.Fatal("selection escaped bounds", k, n.selected)
		}
	}
	n.choices = []contentsChoice{{label: "Broken declaration", source: "ncx", entry: content.NavEntry{Status: "unavailable"}}}
	n.selected = 0
	press(m, "enter")
	if m.pager != p {
		t.Fatal("problem opened")
	}
	n.choices[0] = contentsChoice{source: "unknown", entry: content.NavEntry{Status: "resolved", Unit: &zero, Offset: &zero}}
	press(m, "enter")
	if m.pager != p {
		t.Fatal("unknown source opened")
	}
	press(m, "c")
	pendingContents(t, m)
	n = m.pager.chooser
	m.contentsResult(contentsResult{session: p.session, request: n.request, err: errors.New("failure\x1b[31m")})
	if n.fetching || !strings.Contains(n.notice, "unavailable") || strings.Contains(n.notice, "\x1b") {
		t.Fatal("failure hidden or unsafely rendered", n.notice)
	}
}

func TestPDFContentsChoosesPhysicalPageWithDisabledBookmarks(t *testing.T) {
	root := t.TempDir()
	_, err := library.Initialize(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "pages.pdf")
	if err = os.WriteFile(source, pdftest.Pages("0 << /S /r /St 4 >>", "First page.", "Second page."), 0600); err != nil {
		t.Fatal(err)
	}
	held, err := library.Import(context.Background(), library.ImportRequest{Library: root, Source: source, Apply: true, Scanner: contentsNoScanner{t}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = library.EnableBookmarks(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if _, err = library.DisableBookmarks(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	before := files(t, root)
	m := newModel(context.Background(), contentsService(t))
	run(m, m.openPager(root, root, held.AssetID, "pdf-text/1", reader.Position{}, false, 250))
	press(m, "c")
	n := m.pager.chooser
	if n == nil || n.fetching || len(n.choices) != 2 || !strings.Contains(n.choices[0].label, "iv") {
		t.Fatal("physical pages or declared labels lost", n)
	}
	press(m, "j", "enter")
	if m.pager.start.Unit != 1 || m.pager.chooser != nil || !strings.Contains(m.pagerView(), "Second page.") {
		t.Fatal("wrong physical page", m.pagerView())
	}
	m.closePager()
	if got := files(t, root); !slices.Equal(before, got) {
		t.Fatal("page chooser changed disabled bookmarks or stored bytes")
	}
}

func TestContentsKeepsNAVAndNCXSeparate(t *testing.T) {
	catalog, root, _ := navigationBook(t)
	path := filepath.Join(root, "book.epub")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		if err = r.Close(); err != nil {
			t.Fatal(err)
		}
		if f.Name == "book.opf" {
			data = []byte(strings.Replace(string(data), "</manifest>", `<item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/></manifest>`, 1))
		}
		if f.Name == "nav.xhtml" {
			data = []byte(strings.Replace(string(data), "</ol></nav>", `<li><a href="chapter.xhtml#missing">Unresolved</a></li></ol></nav>`, 1))
		}
		out, err := w.CreateHeader(&zip.FileHeader{Name: f.Name, Method: f.Method})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = out.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	out, err := w.Create("toc.ncx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = out.Write([]byte(`<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/"><navMap><navPoint id="b" playOrder="1"><navLabel><text>NCX second&#x9b;31m&#x202e;label</text></navLabel><content src="chapter.xhtml#b"/></navPoint></navMap></ncx>`)); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	catalog = filepath.Join(filepath.Dir(catalog), "dual.json")
	snap, err := contentsService(t).Snapshot(context.Background(), root, catalog, inventory.Defaults(), false)
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(context.Background(), contentsService(t))
	run(m, m.openPager(catalog, root, snap.Catalog.Assets[0].ID, "epub-text/2", reader.Position{}, false, 250))
	press(m, "c")
	n := m.pager.chooser
	nav, ncx, unresolved := 0, 0, -1
	for i, c := range n.choices {
		if c.source == "nav-doc" {
			nav++
		} else if c.source == "ncx" {
			ncx++
		}
		if c.entry.Status == "unresolved_anchor" {
			unresolved = i
		}
	}
	if nav != 4 || ncx != 1 || unresolved < 0 {
		t.Fatalf("declarations omitted: %#v", n.choices)
	}
	n.selected = unresolved
	press(m, "enter")
	if m.pager.chooser != n {
		t.Fatal("unresolved choice replaced passage")
	}
	n.selected = len(n.choices) - 1
	press(m, "enter")
	if m.pager.navEvidence != "ncx" || !strings.Contains(m.pagerView(), "BETAWORD") || strings.Contains(m.pagerView(), "ALPHAWORD") {
		t.Fatal("NCX selection was not honored", m.pagerView())
	}
	if name := m.placeName(m.pager.start); name != `NCX second\u009b31m\u202elabel` {
		t.Fatal("closing place borrowed another navigation source", name)
	}
	m.closePager()
	if !strings.Contains(m.closed.Summary, `NCX second\u009b31m\u202elabel`) || strings.ContainsAny(m.closed.Summary, "\u009b\u202e") {
		t.Fatal("closing summary did not preserve the chosen label as visible text", m.closed.Summary)
	}
}
