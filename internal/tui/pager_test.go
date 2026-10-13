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
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/pdftext/pdftest"
	"github.com/blisspixel/nemalo/internal/reader"
)

// readerBook writes an EPUB whose first document spans several delivered
// ranges and holds an image, whose second is an SVG with no text (or short
// XHTML), and whose third is short. It returns the catalog, root, asset, and
// the first document's words in order.
func readerBook(t *testing.T, svg bool) (catalog, root, asset string, words []string) {
	t.Helper()
	var body strings.Builder
	for i := range 2400 {
		body.WriteString(fmt.Sprintf("w%d", i))
		switch {
		case i%40 == 39:
			body.WriteString(".</p><p>")
		case i%9 == 8:
			body.WriteString(". ")
		case i%5 == 4:
			body.WriteString(", ")
		default:
			body.WriteString(" ")
		}
		if i == 1200 {
			body.WriteString(`<img src="map.png" alt="A map of the island"/>`)
		}
		// Padding words of at most 24 cells push the document past one
		// 64 KiB range without being shown in chunks.
		body.WriteString(strings.Repeat("x", 20) + fmt.Sprint(i) + " ")
	}
	// Words from the source text, with their trailing punctuation.
	text := strings.NewReplacer("</p><p>", " ", `<img src="map.png" alt="A map of the island"/>`, " ").Replace(body.String())
	words = strings.Fields(text)
	middle, media := "b.svg", "image/svg+xml"
	if !svg {
		middle, media = "b.xhtml", "application/xhtml+xml"
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	f, err := z.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write([]byte("application/epub+zip")); err != nil {
		t.Fatal(err)
	}
	for _, file := range []struct{ name, data string }{
		{"META-INF/container.xml", `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`},
		{"book.opf", `<package xmlns="http://www.idpf.org/2007/opf"><metadata><title>Reader</title><language>en</language></metadata><manifest><item id="a" href="a.xhtml" media-type="application/xhtml+xml"/><item id="b" href="` + middle + `" media-type="` + media + `"/><item id="c" href="c.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="a"/><itemref idref="b"/><itemref idref="c"/></spine></package>`},
		{"a.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><body><p>` + body.String() + `</p></body></html>`},
		{"b.svg", `<svg xmlns="http://www.w3.org/2000/svg"/>`},
		{"b.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><body><p>Middle.</p></body></html>`},
		{"c.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><body><p>After the picture.</p></body></html>`},
	} {
		f, err := z.Create(file.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write([]byte(file.data)); err != nil {
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
	return catalog, root, s.Catalog.Assets[0].ID, words
}

// run executes a command and feeds its messages back, as the program would.
// Scheduled words are not run; tests deliver pagerTick by hand. A command that
// waits, such as a cursor blink, is abandoned.
func run(m *model, cmd tea.Cmd) {
	for depth := 0; cmd != nil && depth < 1000; depth++ {
		done := make(chan tea.Msg, 1)
		go func(c tea.Cmd) { done <- c() }(cmd)
		var next tea.Msg
		select {
		case next = <-done:
		case <-time.After(5 * time.Second):
			return
		}
		switch msg := next.(type) {
		case nil:
			return
		case tea.BatchMsg:
			for _, c := range msg {
				run(m, c)
			}
			return
		default:
			_, cmd = m.Update(msg)
		}
	}
}

func press(m *model, keys ...string) {
	for _, k := range keys {
		msg := tea.KeyPressMsg{Text: k}
		switch k {
		case "space":
			msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		case "left":
			msg = tea.KeyPressMsg{Code: tea.KeyLeft}
		case "right":
			msg = tea.KeyPressMsg{Code: tea.KeyRight}
		case "up":
			msg = tea.KeyPressMsg{Code: tea.KeyUp}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		default:
			msg.Code = []rune(k)[0]
		}
		_, cmd := m.Update(msg)
		run(m, cmd)
	}
}

func scheduled(t *testing.T) *[]time.Duration {
	t.Helper()
	var waits []time.Duration
	saved := after
	after = func(d time.Duration, _ func(time.Time) tea.Msg) tea.Cmd {
		waits = append(waits, d)
		return nil
	}
	t.Cleanup(func() { after = saved })
	return &waits
}

func files(t *testing.T, dirs ...string) []string {
	t.Helper()
	var out []string
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			out = append(out, fmt.Sprint(p, info.Size(), info.ModTime().UnixNano()))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func TestReaderPagesThroughEveryWordAndStopsAtAnUnreadableUnit(t *testing.T) {
	catalog, root, asset, words := readerBook(t, true)
	before := files(t, root, filepath.Dir(catalog))
	m := newModel(context.Background(), app.New())
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	run(m, m.openPager(catalog, root, asset, "epub-text/2", reader.Position{}, false, reader.DefaultRate))
	p := m.pager
	if p == nil || p.doc == nil || p.doc.End || p.doc.Next == "" || p.notice != "" {
		t.Fatal("first range not delivered", p.notice)
	}
	if first := p.doc.Bytes; first > 65536 {
		t.Fatal("read ahead further than needed", first)
	}
	view := m.pagerView()
	if !strings.Contains(view, "Document 1 of 3") || !strings.Contains(view, "w0 ") {
		t.Fatal(view)
	}
	// Every row fits the terminal, and paging shows every word once, in order,
	// across delivered ranges and the image.
	var seen []string
	image, early := false, false
	for pages := 0; pages < 500; pages++ {
		if m.pager.failed >= 0 && !m.pagerAtEnd() {
			// Read-ahead met the unreadable unit; it is not reported yet.
			early = true
			if strings.Contains(m.pagerView(), "no supported text") {
				t.Fatal("failure reported before the reader reached it")
			}
		}
		view := m.pagerView()
		for _, row := range strings.Split(view, "\n") {
			if reader.Width(row) > m.width {
				t.Fatalf("row wider than the terminal: %q", row)
			}
		}
		body := m.pager.layout.Lines[m.pager.top:min(len(m.pager.layout.Lines), m.pager.top+m.pagerBody())]
		for _, line := range body {
			switch line.Kind {
			case reader.Word:
				if line.Pos.Unit == 0 {
					seen = append(seen, strings.Fields(m.pager.layout.Text(line))...)
				}
			case reader.Gap:
				image = image || strings.Contains(m.pager.layout.Text(line), `[image: "A map of the island"]`)
			}
		}
		if m.pager.failed >= 0 && m.pagerAtEnd() {
			break
		}
		press(m, "space")
	}
	if !slices.Equal(seen, words) {
		t.Fatalf("paging showed %d words, want %d", len(seen), len(words))
	}
	if !image || !early {
		t.Fatal("the image was not shown where it was omitted, or read-ahead did not run early", image, early)
	}
	p = m.pager
	if p.failed != 1 || !p.blocked || !strings.Contains(m.pagerView(), "Document 2 of 3 has no supported text. Nothing was skipped; ] opens the next unit.") {
		t.Fatal(p.failed, m.pagerView())
	}
	request := p.request
	press(m, "space", "space")
	if m.pager.request != request {
		t.Fatal("continued past an unreadable unit without a choice")
	}
	press(m, "]")
	if !strings.Contains(m.pagerView(), "Document 3 of 3") || !strings.Contains(m.pagerView(), "After the picture.") || !strings.Contains(m.pagerView(), "Opened by your choice after Document 2 of 3, which was not read.") {
		t.Fatal(m.pagerView())
	}
	press(m, "space")
	if !strings.Contains(m.pager.notice, "End of the source.") {
		t.Fatal(m.pager.notice)
	}
	press(m, "[")
	if m.pager.failed != 1 || !strings.Contains(m.pagerView(), "has no supported text") {
		t.Fatal("going back to the unreadable unit must say so again", m.pagerView())
	}
	press(m, "]")
	if !strings.Contains(m.pagerView(), "After the picture.") {
		t.Fatal("] after a failed jump opens the unit after it", m.pagerView())
	}
	press(m, "q")
	if m.pager != nil || m.readerStop == nil || !strings.Contains(m.status, "Nothing about this reading was recorded") {
		t.Fatal(m.status)
	}
	if after := files(t, root, filepath.Dir(catalog)); !slices.Equal(before, after) {
		t.Fatal("reading changed files", before, after)
	}
}

func TestReaderKeepsItsPlaceAcrossResizeAndWidthChanges(t *testing.T) {
	catalog, root, asset, _ := readerBook(t, true)
	m := newModel(context.Background(), app.New())
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	run(m, m.openPager(catalog, root, asset, "epub-text/2", reader.Position{}, false, reader.DefaultRate))
	press(m, "space", "space", "space")
	at, _ := m.pagerPosition()
	if m.pager.layout.Width != defaultMeasure {
		t.Fatal(m.pager.layout.Width)
	}
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 50, Height: 24})
	run(m, cmd)
	moved, _ := m.pagerPosition()
	if m.pager.layout.Width != 46 || moved.Less(reader.Position{Unit: at.Unit, Offset: at.Offset - 200}) || at.Less(moved) {
		t.Fatal("resize lost the place", at, moved, m.pager.layout.Width)
	}
	press(m, "-", "-", "-", "-", "-", "-", "-", "-")
	if m.pager.measure != minMeasure {
		t.Fatal(m.pager.measure)
	}
	_, cmd = m.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	run(m, cmd)
	for range 30 {
		press(m, "+")
	}
	if m.pager.measure != maxMeasure || m.pager.layout.Width != maxMeasure {
		t.Fatal("measure must stop at its maximum", m.pager.measure)
	}
	press(m, "b", "b", "b", "b", "b", "b")
	if m.pager.top != 0 || !strings.Contains(m.pager.notice, "Start of this session") {
		t.Fatal(m.pager.top, m.pager.notice)
	}
	press(m, "G")
	if m.pager.top == 0 {
		t.Fatal("end of loaded text")
	}
	press(m, "g", "j", "j", "k")
	if m.pager.top != 1 {
		t.Fatal(m.pager.top)
	}
}

func TestSpeedReadingStartsPausedAndPacesBySentence(t *testing.T) {
	waits := scheduled(t)
	catalog, root, asset, _ := readerBook(t, true)
	m := newModel(context.Background(), app.New())
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	run(m, m.openPager(catalog, root, asset, "epub-text/2", reader.Position{}, true, reader.DefaultRate))
	p := m.pager
	if p.playing || len(*waits) != 0 {
		t.Fatal("speed reading must not start on its own")
	}
	view := m.pagerView()
	if !strings.Contains(view, "250 wpm nominal, paused") || !strings.Contains(view, "Document 1 of 3") {
		t.Fatal(view)
	}
	press(m, "space")
	if !p.playing || len(*waits) != 1 || (*waits)[0] != 750*time.Millisecond {
		t.Fatal("heading dwell", *waits)
	}
	gen := p.gen
	// A stale tick is ignored.
	m.Update(pagerTick{gen: gen - 1})
	if p.word != 0 {
		t.Fatal("stale tick advanced", p.word)
	}
	base := time.Minute / reader.DefaultRate
	var got []time.Duration
	for range 24 {
		m.Update(pagerTick{gen: p.gen})
		got = append(got, (*waits)[len(*waits)-1])
	}
	// The source pauses at the clause after w4 and the sentence after w8.
	text := []string{}
	for i := 1; i <= 24; i++ {
		text = append(text, p.doc.Items[i].Text)
	}
	for i, d := range got {
		it := p.doc.Items[p.doc.NextWord(i+1)]
		if d != reader.Dwell(it, reader.DefaultRate) {
			t.Fatal("dwell differs from the pace rule", i, it, d)
		}
	}
	if !slices.Contains(got, 2*base) || !slices.Contains(got, base*3/2) {
		t.Fatal("no sentence or clause pause", got, text)
	}
	press(m, "space")
	word := p.word
	m.Update(pagerTick{gen: p.gen})
	if p.word != word || p.playing {
		t.Fatal("a tick advanced while paused")
	}
	view = m.pagerView()
	if !strings.Contains(view, "[") || !strings.Contains(view, "|") {
		t.Fatal("paused view lacks guide or context", view)
	}
	press(m, "left")
	if start := p.doc.SentenceStart(word); p.word != start {
		t.Fatal("left did not return to the sentence start", p.word, start)
	}
	press(m, "right")
	if p.word <= word {
		t.Fatal("right did not move to the next sentence")
	}
	for range 40 {
		press(m, "up")
	}
	if p.rate != reader.MaxRate || !strings.Contains(m.pagerView(), "treat this as skimming") {
		t.Fatal(p.rate)
	}
	for range 60 {
		press(m, "down")
	}
	if p.rate != reader.MinRate || strings.Contains(m.pagerView(), "skimming") {
		t.Fatal(p.rate)
	}
	at, _ := m.pagerPosition()
	press(m, "p")
	if p.speed || p.layout.Lines[p.top].Pos.Unit != at.Unit || at.Less(p.layout.Lines[p.top].Pos) {
		t.Fatal("pages did not open at the word", at, p.layout.Lines[p.top].Pos)
	}
	press(m, "s")
	if !p.speed || p.playing {
		t.Fatal("switching to speed must pause")
	}
}

func TestSpeedReadingWaitsForTheNextRangeAndResumes(t *testing.T) {
	scheduled(t)
	catalog, root, asset, _ := readerBook(t, true)
	m := newModel(context.Background(), app.New())
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	run(m, m.openPager(catalog, root, asset, "epub-text/2", reader.Position{}, true, reader.DefaultRate))
	p := m.pager
	press(m, "space")
	// Step to the end of the delivered text while the read-ahead request is
	// held back, then deliver it.
	var held tea.Cmd
	for steps := 0; !p.waiting; steps++ {
		if steps > 100000 {
			t.Fatal("never waited")
		}
		_, cmd := m.Update(pagerTick{gen: p.gen})
		if cmd != nil && held == nil && p.fetching {
			held = cmd
		}
	}
	if held == nil || !p.playing || p.word != len(p.doc.Items) || !strings.Contains(m.pagerView(), "Retrieving the next range.") {
		t.Fatal("waiting without the read-ahead request", m.pagerView())
	}
	before := len(p.doc.Items)
	run(m, held)
	if p.waiting || !p.playing || p.word != p.doc.NextWord(before) || p.word >= len(p.doc.Items) {
		t.Fatal("did not resume at the first new word", p.word, before)
	}
}

func TestReaderStartsMidUnitAndReportsFailures(t *testing.T) {
	catalog, root, asset, _ := readerBook(t, true)
	m := newModel(context.Background(), app.New())
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	run(m, m.openPager(catalog, root, asset, "epub-text/2", reader.Position{Unit: 2, Offset: 4}, false, reader.DefaultRate))
	if !strings.Contains(m.pagerView(), "Document 3 of 3, from byte 4") || !strings.Contains(m.pagerView(), "the picture.") {
		t.Fatal(m.pagerView())
	}
	run(m, m.openPager(catalog, root, asset, "epub-text/2", reader.Position{Unit: 9}, false, reader.DefaultRate))
	if !strings.Contains(m.pager.notice, "has 3 unit(s); unit 9 does not exist") {
		t.Fatal(m.pager.notice)
	}
	press(m, "]")
	if !strings.Contains(m.pager.notice, "No unit there.") {
		t.Fatal(m.pager.notice)
	}
	run(m, m.openPager(catalog, root, asset, "pdf-text/1", reader.Position{}, false, reader.DefaultRate))
	if !strings.Contains(m.pagerView(), "No text representation could list this source") {
		t.Fatal(m.pagerView())
	}
	press(m, "space", "s")
	run(m, m.openPager(catalog, root, asset, "epub-text/2", reader.Position{Unit: 0, Offset: 1}, false, reader.DefaultRate))
	if m.pager.doc == nil || m.pager.notice != "" {
		t.Fatal("a mid-word start is the reader's explicit choice", m.pager.notice)
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil || m.pager != nil {
		t.Fatal("ctrl+c must close the reader and quit")
	}
}

func TestReadModeOpensTheReaderOnTheSelectedUnit(t *testing.T) {
	catalog, root, _, _ := readerBook(t, false)
	m := newModel(context.Background(), app.New())
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.switchMode("holdings")
	m.input.SetValue(catalog)
	m.Update(m.start(true)())
	m.Update(key(tea.KeyF6))
	m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m.secondary.SetValue(root)
	if !m.resultsFocused {
		m.Update(key(tea.KeyF6))
	}
	// Without a units listing, the reader opens at the first unit.
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	run(m, cmd)
	if m.pager == nil || m.pager.start.Unit != 0 || m.pager.speed || !strings.Contains(m.pagerView(), "Document 1 of 3") {
		t.Fatal("reader did not open at the first unit", m.status)
	}
	press(m, "q")
	m.Update(m.startRead(true, 0, "", false)())
	if len(m.entries) != 3 {
		t.Fatal(m.entries)
	}
	if !m.resultsFocused {
		m.Update(key(tea.KeyF6))
	}
	m.Update(key(tea.KeyDown))
	m.Update(key(tea.KeyDown))
	_, cmd = m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	run(m, cmd)
	if m.pager == nil || !m.pager.speed || m.pager.start.Unit != 2 || m.pager.representation != "epub-text/2" {
		t.Fatal("reader not opened on the selected unit", m.pager)
	}
	if !strings.Contains(m.View().Content, "Document 3 of 3") {
		t.Fatal(m.View().Content)
	}
	press(m, "esc")
	if m.pager != nil || m.mode != "read" || !strings.Contains(m.status, "unit 2, byte 0") {
		t.Fatal(m.status)
	}
	m.reader.representation = "mpeg-audio/1"
	m.readSelected(false)
	if m.pager != nil || !strings.Contains(m.status, "Audio has no text") {
		t.Fatal(m.status)
	}
}

func TestReaderPagesAManagedPDFByPhysicalPage(t *testing.T) {
	dir := t.TempDir()
	if _, err := library.Initialize(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "paper.pdf")
	if err := os.WriteFile(source, pdftest.Pages("0 << /S /r /St 4 >>", "First page line.", "Second page line."), 0o600); err != nil {
		t.Fatal(err)
	}
	imported, err := library.Import(context.Background(), library.ImportRequest{Library: dir, Source: source, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	units, err := app.New().TextUnits(context.Background(), dir, dir, imported.AssetID)
	if err != nil || units.Representation != "pdf-text/1" {
		t.Fatal(units.Representation, err)
	}
	m := newModel(context.Background(), app.New())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	run(m, m.openPager(dir, dir, imported.AssetID, units.Representation, reader.Position{}, false, reader.DefaultRate))
	if m.pager.measure != pdfMeasure || m.pager.layout.Width != 76 {
		t.Fatal("PDF pages start as wide as the terminal allows", m.pager.measure, m.pager.layout.Width)
	}
	view := m.pagerView()
	if !strings.Contains(view, "Page 1 of 2 (printed iv)") || !strings.Contains(view, "First page line.") || !strings.Contains(view, "Page 2 of 2 (printed v)") || !strings.Contains(view, "Second page line.") {
		t.Fatal(view)
	}
}

func TestAClosedReadersLateResultsNeverReachTheNextOne(t *testing.T) {
	waits := scheduled(t)
	catalog, root, asset, _ := readerBook(t, false)
	m := newModel(context.Background(), app.New())
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	old := m.openPager(catalog, root, asset, "epub-text/2", reader.Position{Unit: 2}, true, reader.DefaultRate)
	oldGen := m.pager.gen
	press(m, "q")
	first := m.openPager(catalog, root, asset, "epub-text/2", reader.Position{}, true, reader.DefaultRate)
	// The closed session's listing, cancelled or not, is ignored.
	run(m, old)
	if m.pager.failure != "" || m.pager.units != nil || !m.pager.fetching {
		t.Fatal("a closed session's result was accepted", m.pager.failure)
	}
	run(m, first)
	if m.pager.doc == nil || !strings.Contains(m.pagerView(), "Document 1 of 3") {
		t.Fatal(m.pagerView())
	}
	press(m, "space")
	n := len(*waits)
	m.Update(pagerTick{gen: oldGen})
	m.Update(pagerTick{gen: 0})
	if m.pager.word != 0 || len(*waits) != n {
		t.Fatal("an earlier session's tick advanced this one")
	}
}

func TestSpeedReadingResumesAfterARateChangeWhileWaiting(t *testing.T) {
	scheduled(t)
	catalog, root, asset, _ := readerBook(t, true)
	m := newModel(context.Background(), app.New())
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	run(m, m.openPager(catalog, root, asset, "epub-text/2", reader.Position{}, true, reader.DefaultRate))
	p := m.pager
	press(m, "space")
	var held tea.Cmd
	for steps := 0; !p.waiting; steps++ {
		if steps > 100000 {
			t.Fatal("never waited")
		}
		if _, cmd := m.Update(pagerTick{gen: p.gen}); cmd != nil && held == nil && p.fetching {
			held = cmd
		}
	}
	press(m, "up", "down", "up")
	if !p.waiting || !p.playing {
		t.Fatal("a rate change while waiting dropped the wait")
	}
	before := len(p.doc.Items)
	run(m, held)
	if p.waiting || !p.playing || p.word != p.doc.NextWord(before) || p.word >= len(p.doc.Items) {
		t.Fatal("playback did not resume", p.word, before)
	}
}

func TestAFullSessionContinuesExactlyWhereItStopped(t *testing.T) {
	catalog, root, asset, words := readerBook(t, true)
	m := newModel(context.Background(), app.New())
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	run(m, m.openPager(catalog, root, asset, "epub-text/2", reader.Position{}, false, reader.DefaultRate))
	p := m.pager
	p.doc.Bytes = reader.MaxText
	press(m, "G")
	if p.resume == nil || !strings.Contains(m.pagerView(), "continues from here in a new session") {
		t.Fatal("limit not reported", p.notice)
	}
	var seen []string
	for _, line := range p.layout.Lines {
		if line.Kind == reader.Word {
			seen = append(seen, strings.Fields(p.layout.Text(line))...)
		}
	}
	resume := *p.resume
	press(m, "]")
	if m.pager.start != resume || !strings.Contains(m.pagerView(), fmt.Sprintf("Document 1 of 3, from byte %d", resume.Offset)) || !strings.Contains(m.pagerView(), "Continued in a new session.") {
		t.Fatal(m.pagerView())
	}
	for pages := 0; pages < 500; pages++ {
		for _, line := range m.pager.layout.Lines[m.pager.top:min(len(m.pager.layout.Lines), m.pager.top+m.pagerBody())] {
			if line.Kind == reader.Word && line.Pos.Unit == 0 {
				seen = append(seen, strings.Fields(m.pager.layout.Text(line))...)
			}
		}
		if m.pagerAtEnd() && (m.pager.doc.End || m.pager.failed >= 0) {
			break
		}
		press(m, "space")
	}
	if !slices.Equal(seen, words) {
		t.Fatalf("across the two sessions: %d words, want %d", len(seen), len(words))
	}
}

func TestTheReaderOpensAtTheUnitOfTheLastRangeRead(t *testing.T) {
	catalog, root, _, _ := readerBook(t, false)
	m := newModel(context.Background(), app.New())
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.switchMode("holdings")
	m.input.SetValue(catalog)
	m.Update(m.start(true)())
	m.Update(key(tea.KeyF6))
	m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m.secondary.SetValue(root)
	m.Update(m.startRead(false, 1, "", false)())
	if !strings.Contains(m.report, "Middle.") {
		t.Fatal(m.report)
	}
	run(m, m.readSelected(false))
	if m.pager == nil || m.pager.start.Unit != 1 {
		t.Fatal("reader did not open at the unit just read")
	}
}

func TestReaderFooterKeepsCloseAndFillsTheFrame(t *testing.T) {
	catalog, root, asset, _ := readerBook(t, true)
	m := newModel(context.Background(), app.New())
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	run(m, m.openPager(catalog, root, asset, "epub-text/2", reader.Position{}, false, reader.DefaultRate))
	press(m, "space")
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 160, Height: 50})
	run(m, cmd)
	press(m, "s")
	if !m.pager.speed {
		t.Fatal("speed mode did not open after a resize")
	}
	view := m.pagerView()
	lines := strings.Split(view, "\n")
	if len(lines) != 50 {
		t.Fatal("frame height", len(lines))
	}
	for i, line := range lines {
		if lipgloss.Width(line) != 160 {
			t.Fatalf("line %d width %d", i, lipgloss.Width(line))
		}
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	view = m.pagerView()
	lines = strings.Split(view, "\n")
	if len(lines) != 24 || !strings.Contains(view, "q close") || !strings.Contains(view, "? keys") || strings.Contains(view, "...") {
		t.Fatal(view)
	}
	for i, line := range lines {
		if lipgloss.Width(line) != 80 {
			t.Fatalf("80-column line %d width %d", i, lipgloss.Width(line))
		}
	}
	press(m, "?")
	if !m.pager.help || !strings.Contains(m.pagerView(), "play or pause") || !strings.Contains(m.pagerView(), "q close") {
		t.Fatal(m.pagerView())
	}
	press(m, "space")
	if !m.pager.help || m.pager.playing {
		t.Fatal("the key list accepted play", m.pager.playing)
	}
	press(m, "?")
	if m.pager.help || !strings.Contains(m.pagerView(), "wpm") {
		t.Fatal(m.pagerView())
	}
}

func TestReadAndTUIRefuseAPipe(t *testing.T) {
	in, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = in.Close() })
	out, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = out.Close() })
	if _, err := Read(context.Background(), app.New(), in, out, ReadOptions{}); err == nil || err.Error() != "read requires an interactive terminal" {
		t.Fatal(err)
	}
	if err := Run(context.Background(), app.New(), in, out); err == nil || err.Error() != "tui requires an interactive terminal" {
		t.Fatal(err)
	}
}
