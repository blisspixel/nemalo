package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/reader"
)

// Every section is named at 80, 100 and 120 columns, nothing overflows from
// 40x20 to 120x36, and hints end at a whole word (issues 33 and 66).
func TestLayoutNamesSectionsAndFitsEverySize(t *testing.T) {
	for _, plain := range []bool{false, true} {
		for _, size := range [][2]int{{40, 20}, {60, 24}, {80, 24}, {99, 30}, {100, 30}, {120, 36}} {
			m := newModel(context.Background(), app.Service{})
			m.plain = plain
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			for _, step := range steps() {
				m.switchMode(step)
				for _, focus := range []bool{false, true} {
					m.resultsFocused = focus
					m.entries = nil
					if focus {
						m.entries = []entry{{title: strings.Repeat("Long untrusted title 日本 ", 10), summary: "s", detail: "d"}}
					}
					m.resize()
					view := m.View().Content
					if lipgloss.Height(view) != size[1] && !plain || lipgloss.Height(view) > size[1] {
						t.Fatalf("plain=%t %v %s: %d rows", plain, size, step, lipgloss.Height(view))
					}
					for _, line := range strings.Split(view, "\n") {
						if lipgloss.Width(line) > size[0] {
							t.Fatalf("plain=%t %v %s: line wider than the terminal: %q", plain, size, step, line)
						}
					}
					if !plain && (strings.Contains(view, " | ") || strings.Contains(view, "-----")) {
						t.Fatalf("%v %s: divider or dashed rule in the panel layout", size, step)
					}
					if !plain && size[0] >= 80 {
						for _, s := range sections {
							if !strings.Contains(view, s.name) {
								t.Fatalf("%v %s: section %s not named", size, step, s.name)
							}
						}
					}
				}
				for _, hint := range []string{m.input.Placeholder, m.secondary.Placeholder} {
					if shown, cut := strings.CutSuffix(hint, "..."); cut && strings.HasSuffix(shown, " ") {
						t.Fatalf("%v %s: hint %q", size, step, hint)
					}
				}
			}
		}
	}
	if got := clipWords("Recommended for a file you did not produce; scanner cloud/sample policy applies; not proof of safety", 46); got != "Recommended for a file you did not produce..." {
		t.Fatalf("%q", got)
	}
}

// At 80x24 Holdings shows at least 18 rows of results (13 before).
func TestHoldingsRowsAt80x24(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.switchMode("holdings")
	if f := m.layout(); f.contentH < 18 {
		t.Fatalf("holdings content rows %d", f.contentH)
	}
}

// Focus is shown by the border and by > without colour; location by brackets.
func TestFocusAndLocationWithoutColour(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Init()
	m.switchMode("holdings")
	view := m.View().Content
	for _, want := range []string{"┏━ Holdings", "> Catalog", "[Library]", "[Holdings]", "╭─ Sections"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "\x1b[") {
		t.Fatal("styles without a colour profile")
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	view = m.View().Content
	if !strings.Contains(view, "[Library]") || !strings.Contains(view, "[Holdings]  Read") {
		t.Fatalf("narrow location:\n%s", view)
	}
	// Plain mode keeps the linear layout: no borders, a location line.
	m.plain = true
	m.resize()
	view = m.View().Content
	if strings.ContainsAny(view, "╭┏│┃") || !strings.Contains(view, "Library: [Holdings]  Read") || !strings.Contains(view, "> Catalog") {
		t.Fatalf("plain layout:\n%s", view)
	}
}

func TestReaderShowsTheUnitLabelOnce(t *testing.T) {
	catalog, root, asset, _ := readerBook(t, false)
	m := newModel(context.Background(), app.New())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	run(m, m.openPager(catalog, root, asset, "epub-text/2", reader.Position{}, false, reader.DefaultRate))
	if n := strings.Count(m.View().Content, "Document 1 of 3"); n != 1 {
		t.Fatal("label shown", n, "times")
	}
	press(m, "space")
	if !strings.Contains(strings.SplitN(m.View().Content, "\n", 2)[0], "Document 1 of 3") {
		t.Fatal("header lost the label past the unit heading")
	}
}
