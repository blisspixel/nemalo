package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/library"
)

func shelf(n int) library.Page {
	p := library.Page{Total: n}
	for i := range n {
		p.Assets = append(p.Assets, library.Asset{ID: fmt.Sprintf("sha256:%064d", i), Bytes: int64(1000 * (i + 1)), Locations: []library.Location{{Path: fmt.Sprintf("book %d of the long shelf with several words.epub", i), CandidateKind: "ebook_candidate"}}})
	}
	return p
}

// Typing after / narrows the page on each keystroke without a request; Esc
// restores the full list and the selection it had.
func TestFilterNarrowsLocallyAndEscRestores(t *testing.T) {
	calls := 0
	m := newModel(context.Background(), app.Service{Providers: map[string]app.Searcher{"openlibrary": countingSearcher{&calls}}})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.switchMode("holdings")
	m.id++
	m.Update(result{id: m.id, data: shelf(12)})
	m.Update(key(tea.KeyF6))
	m.selectEntry(4)
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	if m.filter == nil || !m.filter.editing {
		t.Fatal("/ did not open the filter")
	}
	for i, r := range "book 1" {
		if _, cmd := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)}); cmd != nil && m.busy {
			t.Fatal("typing started work at", i)
		}
	}
	if view := m.View().Content; len(m.entries) != 3 || !strings.Contains(view, "3 of 12") {
		t.Fatalf("book 1 kept %d:\n%s", len(m.entries), view)
	}
	v := m.View()
	if v.Cursor == nil || !strings.Contains(strings.Split(v.Content, "\n")[v.Cursor.Y], "> Filter book 1") {
		t.Fatal("real cursor not in the filter field", v.Cursor)
	}
	// Enter keeps the filter; actions then apply to the filtered selection.
	m.Update(key(tea.KeyEnter))
	if m.filter == nil || m.filter.editing || m.entries[m.selected].title != shelf(12).Assets[1].Locations[0].Path {
		t.Fatal("Enter did not keep the filter", m.entries[m.selected].title)
	}
	m.Update(key(tea.KeyEscape))
	if m.filter != nil || len(m.entries) != 12 || m.selected != 4 {
		t.Fatal("Esc did not restore the list and selection", len(m.entries), m.selected)
	}
	if calls != 0 || m.busy {
		t.Fatal("filtering made a request")
	}
	// A filter does not survive a step change or new results.
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	typeText(m, "zzz")
	m.switchMode("search")
	m.switchMode("holdings")
	if m.filter != nil || len(m.entries) != 12 {
		t.Fatal("filter leaked across steps")
	}
}

// Past the end a local page loads; a network page waits for its own row
// and Enter, and moving away withdraws it.
func TestPagingContinuesLocallyAndAsksBeforeTheNetwork(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.switchMode("holdings")
	m.input.SetValue("catalog.json")
	m.submittedInput = "catalog.json"
	m.id++
	page := shelf(50)
	page.Total = 120
	m.Update(result{id: m.id, data: page})
	m.Update(key(tea.KeyF6))
	m.selectEntry(49)
	if _, cmd := m.Update(key(tea.KeyDown)); cmd == nil || !m.busy || m.offset != 50 {
		t.Fatal("local holdings did not load the next page", m.offset, m.status)
	}
	m.Update(key(tea.KeyEscape))
	calls := 0
	m = newModel(context.Background(), app.Service{Providers: map[string]app.Searcher{"openlibrary": pagedSearcher{&calls}}})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.input.SetValue("verne")
	m.submittedInput = "verne"
	m.id++
	m.Update(result{id: m.id, data: discovery.Page{Total: 25, Results: make([]discovery.Result, 10)}})
	m.Update(key(tea.KeyF6))
	m.selectEntry(9)
	if _, cmd := m.Update(key(tea.KeyDown)); cmd != nil || m.busy || m.loadMore != 1 || !strings.Contains(m.View().Content, "Load the next page (network)") {
		t.Fatal("network page was not offered on its own row", m.status)
	}
	m.Update(key(tea.KeyUp))
	if m.loadMore != 0 {
		t.Fatal("moving away kept the network row")
	}
	m.Update(key(tea.KeyDown))
	m.Update(key(tea.KeyDown))
	_, cmd := m.Update(key(tea.KeyEnter))
	if cmd == nil || !m.busy || m.offset != 10 {
		t.Fatal("Enter did not load the network page", m.status)
	}
	m.Update(cmd())
	if calls != 1 {
		t.Fatal("requests", calls)
	}
	if !strings.Contains(m.View().Content, "11 of 25") {
		t.Fatal("position is not absolute")
	}
}

// Clicks select rows and sections, the wheel moves the selection, a double
// click is Enter, and plain mode leaves the mouse alone.
func TestMouseSelectsAndStaysInSyncWithTheKeyboard(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m.switchMode("holdings")
	m.id++
	m.Update(result{id: m.id, data: shelf(5)})
	if m.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatal("panel layout has no mouse")
	}
	at := func(entry int) hit {
		for _, h := range m.hits {
			if h.entry == entry {
				return h
			}
		}
		t.Fatalf("no click region for %d", entry)
		return hit{}
	}
	h := at(2)
	m.Update(tea.MouseClickMsg{X: h.x0 + 3, Y: h.y0, Button: tea.MouseLeft})
	if !m.resultsFocused || m.selected != 2 {
		t.Fatal("click did not select", m.selected)
	}
	m.Update(key(tea.KeyDown))
	if m.selected != 3 {
		t.Fatal("keyboard lost the clicked selection")
	}
	m.View()
	h = at(3)
	m.Update(tea.MouseWheelMsg{X: h.x0 + 3, Y: h.y0, Button: tea.MouseWheelUp})
	if m.selected != 2 {
		t.Fatal("wheel did not move the selection", m.selected)
	}
	now := time.Unix(100, 0)
	m.clock = func() time.Time { return now }
	m.View()
	h = at(2)
	m.Update(tea.MouseClickMsg{X: h.x0 + 3, Y: h.y0, Button: tea.MouseLeft})
	now = now.Add(200 * time.Millisecond)
	m.Update(tea.MouseClickMsg{X: h.x0 + 3, Y: h.y0, Button: tea.MouseLeft})
	if !m.details {
		t.Fatal("double click was not Enter")
	}
	m.Update(key(tea.KeyEscape))
	for _, s := range m.hits {
		if s.step == "search" {
			m.Update(tea.MouseClickMsg{X: s.x0, Y: s.y0, Button: tea.MouseLeft})
			break
		}
	}
	if m.mode != "search" {
		t.Fatal("clicking a section did not open it", m.mode)
	}
	m.plain = true
	m.resize()
	if m.View().MouseMode != tea.MouseModeNone {
		t.Fatal("plain mode captured the mouse")
	}
	m.Update(tea.MouseClickMsg{X: 1, Y: 1, Button: tea.MouseLeft})
	if m.mode != "search" || m.resultsFocused {
		t.Fatal("plain mode acted on a click")
	}
}

// Columns align, titles clip at a word, and rows carry no pipes.
func TestColumnsAlignAndClipAtWords(t *testing.T) {
	for _, width := range []int{80, 120} {
		m := newModel(context.Background(), app.Service{})
		m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		m.switchMode("holdings")
		m.id++
		m.Update(result{id: m.id, data: shelf(4)})
		rows := m.listRows(60, 30)
		ends := map[int]bool{}
		for i := 0; i < len(rows); i += 3 {
			ends[lipgloss.Width(rows[i])] = true
			if !strings.HasSuffix(strings.TrimSpace(rows[i]), "B") {
				t.Fatalf("right column missing: %q", rows[i])
			}
			title := strings.TrimSpace(rows[i][:strings.LastIndex(rows[i], "  ")])
			if strings.HasSuffix(title, "...") && strings.HasSuffix(strings.TrimSuffix(title, "..."), " ") {
				t.Fatalf("title clipped mid-space: %q", title)
			}
			if strings.Contains(rows[i]+rows[i+1], " | ") || strings.Contains(rows[i+1], "location(s)") {
				t.Fatalf("old row text: %q %q", rows[i], rows[i+1])
			}
		}
		if len(ends) != 1 {
			t.Fatalf("width %d: right column ends differ: %v", width, ends)
		}
	}
}

type pagedSearcher struct{ calls *int }

func (p pagedSearcher) Search(_ context.Context, q string, limit, offset int) (discovery.Page, error) {
	*p.calls++
	return discovery.Page{Query: q, Total: 25, Offset: offset, Results: make([]discovery.Result, min(limit, 25-offset))}, nil
}
