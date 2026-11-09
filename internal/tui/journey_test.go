package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/blisspixel/nemalo/internal/acquisition"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/reader"
)

// Every step's first status names the next action and stays readable at 40 columns.
func TestArrivalNamesTheNextAction(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	m.mode = "jobs"
	wants := map[string]string{
		"home":     "download",
		"search":   "Nothing is downloaded",
		"evaluate": "download",
		"holdings": "F8",
		"read":     "one word at a time",
		"state":    "F8",
		"inspect":  "Ready.",
	}
	for step, want := range wants {
		m.switchMode(step)
		if !strings.Contains(m.status, want) || !fitsStatus(m.status) || strings.Contains(m.statusText(), "Full error") {
			t.Fatalf("%s status %q", step, m.status)
		}
	}
	m.switchMode("inspect")
	m.drafts["search"] = draft{entries: []entry{{title: "One", sourceID: "archive:one", detail: "d"}}}
	m.switchMode("search")
	if !strings.Contains(m.status, "does not download") || !fitsStatus(m.status) {
		t.Fatal(m.status)
	}
	m.switchMode("home")
	m.lastEvaluation = &discovery.Evaluation{Access: discovery.OpenLibraryEditionList}
	m.drafts["evaluate"] = draft{entries: []entry{{title: "Edition", sourceID: "openlibrary:OL1M", detail: "d"}}}
	m.switchMode("evaluate")
	if !strings.Contains(m.status, "selects this edition") || strings.Contains(m.status, "d reviews") || !fitsStatus(m.status) {
		t.Fatal(m.status)
	}
	m.switchMode("home")
	m.lastEvaluation = &discovery.Evaluation{OpenAccess: &discovery.OpenAccess{}}
	m.drafts["evaluate"] = draft{entries: []entry{{title: "Location", sourceID: "pmc:PMC1", detail: "d"}}}
	m.switchMode("evaluate")
	if !strings.Contains(m.status, "pmc:") || strings.Contains(m.status, "d reviews") || !fitsStatus(m.status) {
		t.Fatal(m.status)
	}
}

// A finished result keeps its own summary and adds the next key when it fits.
func TestResultsNameTheNextKey(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(result{id: m.id, data: discovery.Page{Total: 12, Results: []discovery.Result{{Title: "Story", ID: "archive:story"}}}})
	if !strings.Contains(m.status, "discovery metadata only") || !strings.Contains(m.status, "F6, then e evaluates.") || !fitsStatus(m.status) {
		t.Fatal(m.status)
	}
	m.id++
	m.Update(result{id: m.id, data: discovery.Evaluation{ID: "x", Files: []discovery.OfferedFile{{}}}})
	if m.status != "1 file. F6, then d reviews a download." || !fitsStatus(m.status) {
		t.Fatal(m.status)
	}
	if !strings.Contains(m.report, "Files listed: 1") {
		t.Fatal("evaluation report lost the file count")
	}
	m.id++
	m.Update(result{id: m.id, data: discovery.Evaluation{ID: "W", OpenAccess: &discovery.OpenAccess{}}})
	if strings.Contains(m.status, "d reviews") || !strings.Contains(m.status, "discovery only") {
		t.Fatal(m.status)
	}
	m.id++
	m.Update(result{id: m.id, data: library.ManagedPage{Total: 2}})
	if m.status != "2 matching holdings. F6, then r reads or o opens." || !fitsStatus(m.status) {
		t.Fatal(m.status)
	}
	m.id++
	m.Update(result{id: m.id, data: library.ImportResult{Applied: true, Titles: []string{"Essays"}, Status: "review", Reason: "unscanned"}})
	if m.status != "Imported Essays as review (unscanned). Open Holdings, then r." || !fitsStatus(m.status) {
		t.Fatal(m.status)
	}
	m.id++
	m.Update(result{id: m.id, data: library.ImportResult{Applied: true, Titles: []string{strings.Repeat("A very long title ", 8)}, Status: "review"}})
	if strings.Contains(m.status, "Open Holdings") {
		t.Fatal(m.status)
	}
}

// Holdings r stages Read and says how pages and one-word reading open.
func TestReadHandoffNamesPagesAndWords(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.switchMode("holdings")
	m.resultsFocused = true
	m.submittedInput = "C:/library"
	m.entries = []entry{{title: "Book", sourceID: "sha256:abc", managed: true, detail: "d"}}
	m.Update(keyPress("r"))
	if m.mode != "read" || m.secondary.Value() != "C:/library" || !strings.Contains(m.status, "p for pages") || !strings.Contains(m.status, "s for words") || !fitsStatus(m.status) {
		t.Fatalf("%s %q root %q", m.mode, m.status, m.secondary.Value())
	}
	m.switchMode("holdings")
	m.resultsFocused = true
	m.entries = []entry{{title: "Loose", sourceID: "sha256:def", detail: "d"}}
	m.Update(keyPress("r"))
	if m.secondary.Value() != "" || !strings.Contains(m.status, "Enter a root") || !fitsStatus(m.status) {
		t.Fatalf("%q root %q", m.status, m.secondary.Value())
	}
	m.id++
	m.Update(result{id: m.id, data: content.Result{Status: "units_available", Representation: "epub-structure/1"}})
	if !strings.Contains(m.status, "p for pages") || !strings.Contains(m.status, "one word at a time") || !fitsStatus(m.status) {
		t.Fatal(m.status)
	}
	m.switchMode("holdings")
	m.resultsFocused = true
	m.submittedInput = "C:/library"
	m.entries = []entry{{title: "Talk", sourceID: "sha256:aud", managed: true, audio: true, detail: "d"}}
	m.Update(keyPress("r"))
	if m.reader.representation != "mpeg-audio/1" || strings.Contains(m.status, "p for pages") || !strings.Contains(m.status, "not played here") || !fitsStatus(m.status) {
		t.Fatalf("%s %q", m.reader.representation, m.status)
	}
	m.id++
	m.Update(result{id: m.id, data: content.Result{Status: "units_available", Representation: "mpeg-audio/1"}})
	if strings.Contains(m.status, "p for pages") || !strings.Contains(m.status, "not played here") || !fitsStatus(m.status) {
		t.Fatal(m.status)
	}
}

// The list footer names download, open, pages and words.
func TestFooterNamesTheJourney(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m.switchMode("evaluate")
	m.resultsFocused = true
	m.entries = []entry{{title: "File", sourceID: "archive:item/file.epub", detail: "d"}}
	if f := m.footer(); !strings.Contains(f, "d download") {
		t.Fatal(f)
	}
	m.downloadReview = &acquisition.Request{SourceID: "archive:one", File: "a.epub", Output: t.TempDir()}
	if f := m.footer(); !strings.Contains(f, "Enter download") {
		t.Fatal(f)
	}
	m.downloadReview = nil
	m.switchMode("holdings")
	m.resultsFocused = true
	m.entries = []entry{{title: "Book", sourceID: "sha256:abc", managed: true, detail: "d"}, {title: "Two", detail: "d"}}
	if f := m.footer(); !strings.Contains(f, "r read") || !strings.Contains(f, "o open") {
		t.Fatal(f)
	}
	m.switchMode("read")
	m.resultsFocused = true
	m.entries = []entry{{title: "Unit 0", detail: "d"}, {title: "Unit 1", detail: "d"}}
	if f := m.footer(); !strings.Contains(f, "p pages") || !strings.Contains(f, "s words") {
		t.Fatal(f)
	}
}

// The selected row names the next key after the status line has a result.
func TestSelectedRowNamesTheNextKey(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.switchMode("home")
	if !strings.Contains(m.View().Content, "Enter opens this.") {
		t.Fatal("home did not name Enter")
	}
	show := func(step string, entries []entry) string {
		m.switchMode(step)
		m.resultsFocused = false
		m.entries = entries
		m.selectEntry(0)
		m.setReport("kept")
		m.resize()
		return m.View().Content
	}
	view := show("search", []entry{{title: "Story", sourceID: "archive:story", summary: "one", detail: "d"}, {title: "Two", sourceID: "archive:two", summary: "two", detail: "d"}})
	if strings.Count(view, "e prepares Evaluate. It does not download.") != 1 || !strings.Contains(view, "F6, then e prepares Evaluate.") {
		t.Fatal("search cue", view)
	}
	m.resultsFocused = true
	m.resize()
	view = m.View().Content
	if strings.Contains(view, "F6, then") || !strings.Contains(view, "e prepares Evaluate. It does not download.") {
		t.Fatal("focused search cue", view)
	}
	view = show("holdings", []entry{{title: "Loose", sourceID: "sha256:abc", summary: "review", detail: "d"}})
	if m.layout().contentH < 18 || !strings.Contains(view, "F6, then r reads this here.") {
		t.Fatalf("holdings rows %d\n%s", m.layout().contentH, view)
	}
	m.entries[0].managed = true
	m.resize()
	if !strings.Contains(m.View().Content, "o opens a verified copy.") {
		t.Fatal(m.View().Content)
	}
	m.entries[0].audio = true
	m.resize()
	if strings.Contains(m.View().Content, "r reads here") || !strings.Contains(m.View().Content, "o opens a verified copy in a player.") {
		t.Fatal(m.View().Content)
	}
	m.switchMode("evaluate")
	m.lastEvaluation = &discovery.Evaluation{ID: "archive:item", Files: []discovery.OfferedFile{{Name: "a.epub"}}}
	view = show("evaluate", []entry{{title: "a.epub", sourceID: "a.epub", summary: "public", detail: "d"}})
	if !strings.Contains(view, "d reviews one download into a new folder.") {
		t.Fatal(view)
	}
	m.lastEvaluation.Access = discovery.OpenLibraryEditionList
	m.resize()
	if !strings.Contains(m.View().Content, "e selects this edition. It does not download.") {
		t.Fatal(m.View().Content)
	}
	view = show("read", []entry{{title: "Unit 0", summary: "text", detail: "d"}})
	if !strings.Contains(view, "p reads pages. s reads one word at a time, paused.") {
		t.Fatal(view)
	}
	m.reader.representation = "mpeg-audio/1"
	m.resize()
	view = m.View().Content
	if strings.Contains(view, "p reads pages") || !strings.Contains(view, "Audio is not played here.") {
		t.Fatal(view)
	}
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > 80 {
			t.Fatalf("wide line %q", line)
		}
	}
}

// A finished download and an applied import say how to continue in this screen.
func TestReportsNameTheFollowingScreen(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.switchMode("evaluate")
	m.id++
	m.Update(result{id: m.id, data: acquisition.Result{Status: "untrusted_intake", Complete: true, Output: t.TempDir(), Antivirus: "not_scanned"}})
	if !strings.Contains(m.report, guideAfterDownload) || !strings.Contains(m.status, "without a scan") {
		t.Fatal(m.status, m.report)
	}
	m.id++
	m.Update(result{id: m.id, data: acquisition.Result{Status: "detected_quarantined", Complete: true, Output: t.TempDir()}})
	if strings.Contains(m.report, guideAfterDownload) {
		t.Fatal("a detection offered import")
	}
	m.id++
	m.Update(result{id: m.id, data: library.ImportResult{Applied: true, Titles: []string{"Essays"}, Status: "review", Reason: "unscanned", Library: t.TempDir()}})
	if !strings.Contains(m.report, "press F6, then r") || !strings.Contains(m.status, "Open Holdings, then r.") {
		t.Fatal(m.status, m.report)
	}
}

// One-word reading opens paused and says how to start.
func TestSpeedReadingNamesSpace(t *testing.T) {
	catalog, root, asset, _ := readerBook(t, false)
	m := newModel(context.Background(), app.New())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	run(m, m.openPager(catalog, root, asset, "epub-text/2", reader.Position{}, true, reader.DefaultRate))
	view := m.View().Content
	if !strings.Contains(view, "Paused. Space plays. Closing saves nothing.") || !strings.Contains(view, "250 wpm nominal, paused") {
		t.Fatal(view)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if m.pager == nil || !m.pager.playing || strings.Contains(m.View().Content, "Paused. Space plays.") {
		t.Fatal("the pause hint stayed while playing")
	}
}

// Ctrl+\ cycles the sidebar and the side details, and leaves plain mode linear.
func TestPaneCycle(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m.switchMode("holdings")
	m.entries = []entry{{title: "Book", detail: "d"}, {title: "Two", detail: "d"}}
	m.resize()
	see := func() (bool, bool) {
		view := m.View().Content
		return strings.Contains(view, "╭─ Sections"), strings.Contains(view, "╭─ Details")
	}
	if side, detail := see(); !side || !detail {
		t.Fatalf("start side %t detail %t", side, detail)
	}
	for i, want := range []struct{ side, detail bool }{
		{false, true},
		{true, false},
		{false, false},
		{true, true},
	} {
		m.Update(keyPress("ctrl+\\"))
		side, detail := see()
		if side != want.side || detail != want.detail || !fitsStatus(m.status) {
			t.Fatalf("press %d side %t detail %t status %q", i+1, side, detail, m.status)
		}
	}
	m.busy = true
	panes := m.panes
	m.Update(keyPress("ctrl+\\"))
	if m.panes != panes {
		t.Fatal("busy cycle changed the layout")
	}
	m.busy = false
	m.confirmRoot = "library"
	m.Update(keyPress("ctrl+\\"))
	if m.panes != panes {
		t.Fatal("a review changed the layout")
	}
	m.confirmRoot = ""
	m.plain = true
	m.resize()
	m.Update(keyPress("ctrl+\\"))
	view := m.View().Content
	if m.panes != panes || strings.ContainsAny(view, "╭┏") || !strings.Contains(m.status, "screen readers") || !fitsStatus(m.status) {
		t.Fatalf("plain panes %d status %q", m.panes, m.status)
	}
}
