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
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/pdftext/pdftest"
	"github.com/blisspixel/nemalo/internal/reader"
)

func queryReader(t *testing.T, m *model, text string) findResult {
	t.Helper()
	_, cmd := m.Update(tea.PasteMsg{Content: text})
	run(m, cmd)
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("query did not start", m.pager.finder.notice)
	}
	msg, ok := cmd().(findResult)
	if !ok {
		t.Fatal("wrong query reply")
	}
	m.Update(msg)
	return msg
}

func TestFindOpensExactCursorAcrossDeclaredEntryBoundary(t *testing.T) {
	catalog, root, asset := navigationBook(t)
	before := files(t, root, filepath.Dir(catalog))
	scheduled(t)
	m := newModel(context.Background(), contentsService(t))
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	run(m, m.openChapter(catalog, root, asset, "nav-doc", 0, reader.Position{}, true))
	m.pager.only, m.pager.rate = true, 425
	press(m, "space")
	p := m.pager
	word, gen := p.word, p.gen
	press(m, "/")
	if p.finder == nil || !p.finder.editing || p.playing || p.waiting {
		t.Fatal("search did not pause")
	}
	m.Update(pagerTick{gen: gen})
	if p.word != word {
		t.Fatal("text advanced behind search")
	}
	msg := queryReader(t, m, "BETAWORD")
	if msg.err != nil || len(p.finder.matches) != 1 || p.finder.representation != "epub-text/2" {
		t.Fatal("whole-book match unavailable", msg.err, msg.data)
	}
	match := p.finder.matches[0]
	press(m, "enter")
	p = m.pager
	if p.chapter || p.representation != "epub-text/2" || p.start != (reader.Position{Unit: match.Unit, Offset: match.Start}) || p.representationID != msg.data.RepresentationID || !p.only || !p.speed || p.rate != 425 || p.playing {
		t.Fatalf("match lost binding or reader mode: %#v", p)
	}
	var words []string
	for _, it := range p.doc.Items {
		if it.Kind == reader.Word {
			words = append(words, it.Text)
		}
	}
	if len(words) == 0 || words[0] != "BETAWORD" || strings.Contains(strings.Join(words, " "), "ALPHAWORD") {
		t.Fatal("not opened at exact result", words)
	}
	if cmd := m.closePager(); cmd == nil {
		t.Fatal("CLI reader lost exit mode")
	}
	if got := files(t, root, filepath.Dir(catalog)); !slices.Equal(before, got) {
		t.Fatal("find changed files")
	}
}

func TestFindInputIsLiteralBoundedAndNeverChangesHiddenRoots(t *testing.T) {
	catalog, root, asset := navigationBook(t)
	m := newModel(context.Background(), contentsService(t))
	run(m, m.openPager(catalog, root, asset, "epub-text/2", reader.Position{}, false, 250))
	m.input.SetValue("selected-library")
	m.secondary.SetValue("selected-root")
	press(m, "/")
	request := m.pager.finder.request
	_, empty := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if empty != nil || !m.pager.finder.editing || m.pager.finder.fetching || m.pager.finder.request != request {
		t.Fatal("empty query became a content read")
	}
	// An IME text commit that spells a key name is text, not that command.
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'e', Text: "enter"})
	run(m, cmd)
	if m.pager.finder.input.Value() != "enter" || !m.pager.finder.editing {
		t.Fatal("literal text became a command")
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'c', Text: "ctrl+c"})
	run(m, cmd)
	if m.pager == nil || m.pager.finder.input.Value() != "enterctrl+c" {
		t.Fatal("text commit closed reader")
	}
	f := m.pager.finder
	f.input.SetValue("abc")
	f.input.SetCursor(3)
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	run(m, cmd)
	if f.input.Value() != "" {
		t.Fatal("editor clear shortcut failed")
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'q', Text: "qm"})
	run(m, cmd)
	if m.pager == nil || f.input.Value() != "qm" {
		t.Fatal("ordinary query letters became reader commands")
	}
	f.input.SetValue("q m")
	f.input.SetCursor(3)
	for _, bad := range []string{"line\nbreak", "tab\tquery", "\x1b[31m", string([]byte{0xff}), "\ufffd", strings.Repeat("é", 501)} {
		before := f.input.Value()
		_, cmd = m.Update(tea.PasteMsg{Content: bad})
		run(m, cmd)
		if f.input.Value() != before || !strings.Contains(f.notice, "not inserted") {
			t.Fatal("paste changed meaning silently", bad, f.input.Value(), f.notice)
		}
	}
	f.input.SetValue("")
	f.input.SetCursor(0)
	_, cmd = m.Update(tea.PasteMsg{Content: strings.Repeat("é", 500)})
	run(m, cmd)
	if len(f.input.Value()) != 1000 {
		t.Fatal("valid UTF-8 boundary refused")
	}
	_, cmd = m.Update(tea.PasteMsg{Content: "x"})
	run(m, cmd)
	if len(f.input.Value()) != 1000 {
		t.Fatal("byte limit exceeded")
	}
	f.input.SetValue("a\u200c東京")
	f.input.SetCursor(2)
	raw, pos := f.input.Value(), f.input.Position()
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	view := m.pagerView()
	if f.input.Value() != raw || f.input.Position() != pos || !strings.Contains(view, `\u200c`) || strings.Contains(view, "\u200c") {
		t.Fatal("rendering changed or hid exact query", view)
	}
	press(m, "esc")
	m.Update(tea.PasteMsg{Content: "unexpected-root"})
	if m.input.Value() != "selected-library" || m.secondary.Value() != "selected-root" {
		t.Fatal("reader paste changed hidden scope")
	}
	press(m, "/")
	_, quit := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if m.pager != nil || quit == nil {
		t.Fatal("actual Ctrl+C did not close the reader")
	}
}

func TestFindLiteralCaseWhitespaceAndUnicodeQueries(t *testing.T) {
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
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
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
		if f.Name == "chapter.xhtml" {
			data = []byte(strings.Replace(string(data), "ALPHAWORD", "αλφα\u200c東京  exact", 1))
		}
		out, err := w.CreateHeader(&zip.FileHeader{Name: f.Name, Method: f.Method})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = out.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	catalog = filepath.Join(filepath.Dir(catalog), "unicode.json")
	snap, err := contentsService(t).Snapshot(context.Background(), root, catalog, inventory.Defaults(), false)
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(context.Background(), contentsService(t))
	run(m, m.openPager(catalog, root, snap.Catalog.Assets[0].ID, "epub-text/2", reader.Position{}, false, 250))
	for _, tc := range []struct {
		query string
		count int
	}{{"αλφα\u200c東京", 1}, {"betaword", 0}, {"BETAWORD", 1}, {"東京  exact", 1}, {"東京 exact", 0}} {
		press(m, "/")
		msg := queryReader(t, m, tc.query)
		if msg.err != nil || len(m.pager.finder.matches) != tc.count || m.pager.finder.query != tc.query {
			t.Fatal("query was normalized or case folded", tc, msg.err, msg.data)
		}
		press(m, "esc")
	}
}

func TestFindCancellationAndLateClipboardOrServiceReplies(t *testing.T) {
	catalog, root, asset := navigationBook(t)
	m := newModel(context.Background(), contentsService(t))
	run(m, m.openPager(catalog, root, asset, "epub-text/2", reader.Position{}, true, 250))
	p := m.pager
	at, _ := m.pagerPosition()
	press(m, "/")
	f := p.finder
	oldInput := findInputResult{session: p.session, request: f.request, message: tea.PasteMsg{Content: "late clipboard"}}
	f.input.SetValue("ALPHAWORD")
	cmd := m.submitFind()
	reply := cmd().(findResult)
	press(m, "esc")
	m.Update(reply)
	if p.finder != nil {
		t.Fatal("late result reopened dialog")
	}
	if got, _ := m.pagerPosition(); got != at || p.playing {
		t.Fatal("cancel moved or resumed passage")
	}
	press(m, "/")
	m.Update(oldInput)
	if p.finder.input.Value() != "" {
		t.Fatal("old clipboard applied to new dialog")
	}
	f = p.finder
	f.input.SetValue("BETAWORD")
	cmd = m.submitFind()
	press(m, "/")
	m.Update(reply)
	if !f.editing || len(f.matches) != 0 {
		t.Fatal("old result applied after edit")
	}
	cancelled := cmd().(findResult)
	if !errors.Is(cancelled.err, context.Canceled) {
		t.Fatal("edit did not cancel source work", cancelled.err)
	}
	f.input.SetValue("ALPHAWORD")
	cmd = m.submitFind()
	reply = cmd().(findResult)
	run(m, m.openPager(catalog, root, asset, "epub-text/2", reader.Position{}, false, 250))
	m.Update(reply)
	if m.pager.finder != nil {
		t.Fatal("old session result applied")
	}
	press(m, "/")
	m.closePager()
	m.Update(reply)
	m.Update(oldInput)
	if m.pager != nil {
		t.Fatal("closed reader reopened")
	}
	// Nested editor commands keep the same session/request fence.
	wrapped := findInputCommand(7, 9, func() tea.Msg { return tea.BatchMsg{func() tea.Msg { return tea.PasteMsg{Content: "text"} }, nil} })
	batch, ok := wrapped().(tea.BatchMsg)
	if !ok || len(batch) != 1 {
		t.Fatal("editor command batch malformed")
	}
	tag := batch[0]().(findInputResult)
	if tag.session != 7 || tag.request != 9 {
		t.Fatal("editor batch lost scope")
	}
}

func TestFindRefusesMissingOrTamperedCursorWithoutOffsetFallback(t *testing.T) {
	for _, token := range []string{"", "not-a-source-reference"} {
		t.Run(token, func(t *testing.T) {
			catalog, root, asset := navigationBook(t)
			m := newModel(context.Background(), contentsService(t))
			run(m, m.openPager(catalog, root, asset, "epub-text/2", reader.Position{}, false, 250))
			press(m, "/")
			queryReader(t, m, "BETAWORD")
			p := m.pager
			p.finder.matches[0].Cursor = token
			press(m, "enter")
			if token == "" {
				if m.pager != p || m.pager.finder == nil {
					t.Fatal("missing reference fell back to offsets")
				}
			} else {
				if !m.pager.blocked || m.pager.doc == nil {
					t.Fatal("tampered reference accepted")
				}
				for _, it := range m.pager.doc.Items {
					if it.Kind == reader.Word {
						t.Fatal("invalid reference published text", it)
					}
				}
			}
		})
	}
}

func TestFindLimitsCoverageAndFramesAreExplicit(t *testing.T) {
	catalog, root, asset, _ := readerBook(t, false)
	m := newModel(context.Background(), contentsService(t))
	run(m, m.openPager(catalog, root, asset, "epub-text/2", reader.Position{}, false, 250))
	press(m, "/")
	queryReader(t, m, "x")
	f := m.pager.finder
	if len(f.matches) != 100 || !strings.Contains(f.notice, "First 100") {
		t.Fatal("truncation hidden", len(f.matches), f.notice)
	}
	for _, width := range []int{40, 80, 116} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		for _, line := range strings.Split(m.pagerView(), "\n") {
			if lipgloss.Width(line) != width {
				t.Fatal("find row overflow", width, lipgloss.Width(line))
			}
		}
	}
	for _, k := range []string{"j", "down", "space", "pgdown", "G", "end", "k", "up", "b", "pgup", "g", "home"} {
		press(m, k)
		if f.selected < 0 || f.selected >= len(f.matches) {
			t.Fatal("selection out of bounds", k)
		}
	}
	press(m, "?")
	if !strings.Contains(m.pagerView(), "Find keys") {
		t.Fatal("wrong result help")
	}
	press(m, "?", "/")
	f = m.pager.finder
	f.input.SetValue("missing")
	m.submitFind()
	id := f.request
	session := m.pager.session
	m.findResult(findResult{session: session, request: id, data: content.Result{Status: "no_matches", UnsupportedUnits: []int{1}}})
	if len(f.matches) != 0 || !strings.Contains(f.notice, "1 unsupported") {
		t.Fatal("unsupported coverage hidden", f.notice)
	}
	m.findResult(findResult{session: session, request: id, data: content.Result{Status: "matches_found", Matches: []content.Match{{Unit: 0, Start: 0}}}})
	if len(f.matches) != 0 {
		t.Fatal("duplicate result applied")
	}
	press(m, "/")
	f.input.SetValue("x")
	m.submitFind()
	m.findResult(findResult{session: session, request: f.request, err: errors.New("bad\x1b[31m")})
	if len(f.matches) != 0 || f.fetching || strings.Contains(f.notice, "\x1b") || !strings.Contains(f.notice, "unavailable") {
		t.Fatal("error hidden or unsafe", f.notice)
	}
}

func TestFindPDFPageAndDisabledBookmarksStayBound(t *testing.T) {
	lib := t.TempDir()
	if _, err := library.Initialize(context.Background(), lib); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "pages.pdf")
	if err := os.WriteFile(source, pdftest.Pages("0 << /S /r /St 4 >>", "First only.", "Unique second."), 0600); err != nil {
		t.Fatal(err)
	}
	held, err := library.Import(context.Background(), library.ImportRequest{Library: lib, Source: source, Apply: true, Scanner: contentsNoScanner{t}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = library.EnableBookmarks(context.Background(), lib); err != nil {
		t.Fatal(err)
	}
	if _, err = library.DisableBookmarks(context.Background(), lib); err != nil {
		t.Fatal(err)
	}
	before := files(t, lib)
	m := newModel(context.Background(), contentsService(t))
	run(m, m.openPager(lib, lib, held.AssetID, "pdf-text/1", reader.Position{}, false, 250))
	press(m, "/")
	msg := queryReader(t, m, "Unique")
	if msg.err != nil || len(m.pager.finder.matches) != 1 || m.pager.finder.matches[0].PageLabel != "v" {
		t.Fatal("PDF declared page lost", msg.data)
	}
	press(m, "enter")
	if m.pager.start.Unit != 1 || m.pager.representation != "pdf-text/1" || m.pager.representationID != msg.data.RepresentationID || !strings.Contains(m.pagerView(), "Unique second.") {
		t.Fatal("wrong PDF match", m.pagerView())
	}
	m.closePager()
	if got := files(t, lib); !slices.Equal(before, got) {
		t.Fatal("find changed disabled bookmarks or holdings")
	}
}
