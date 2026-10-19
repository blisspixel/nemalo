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

// Mode labels stay whole, hints end at a word, and the main footer is one
// line when it fits (issue 33).
func TestLayoutKeepsLabelsAndWordsWhole(t *testing.T) {
	for _, width := range []int{40, 60, 80, 100, 120} {
		m := newModel(context.Background(), app.Service{})
		m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		for i, name := range modes {
			m.switchMode(name)
			nav := m.navigation()
			title := strings.ToUpper(name[:1]) + name[1:]
			number := string(rune('0' + (i+1)%10))
			if strings.Contains(nav, "\n") || !strings.Contains(nav, "["+number+" "+title+"]") || len([]rune(nav)) > width-2 {
				t.Fatalf("width %d mode %s: %q", width, name, nav)
			}
			m.View()
			for _, hint := range []string{m.input.Placeholder, m.secondary.Placeholder} {
				if strings.HasSuffix(hint, "...") {
					shown := strings.TrimSuffix(hint, "...")
					if strings.HasSuffix(shown, " ") || len([]rune(hint)) > width-16 {
						t.Fatalf("width %d mode %s: hint %q", width, name, hint)
					}
				}
			}
			for _, line := range strings.Split(m.View().Content, "\n") {
				if lipgloss.Width(line) > width {
					t.Fatalf("width %d mode %s: line wider than the terminal: %q", width, name, line)
				}
			}
		}
		footer := m.footer()
		if width == 120 && strings.Contains(footer, "\n") {
			t.Fatalf("footer at 120 columns: %q", footer)
		}
	}
	if got := clipWords("Recommended for a file you did not produce; scanner cloud/sample policy applies; not proof of safety", 46); got != "Recommended for a file you did not produce..." {
		t.Fatalf("%q", got)
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
