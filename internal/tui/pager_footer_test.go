package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/blisspixel/nemalo/internal/reader"
)

func TestNarrowReaderKeepsExactPositionRateNoticeAndExitVisible(t *testing.T) {
	m, root, held := evidenceModel(t)
	m.Update(tea.WindowSizeMsg{Width: 116, Height: 36})
	run(m, m.openPager(root, root, held.AssetID, "epub-text/2", reader.Position{Unit: 0, Offset: 10}, true, reader.DefaultRate))
	if m.pager == nil || m.pager.doc == nil {
		t.Fatal("reader did not load its source")
	}
	before := terminalJobTree(t, root)
	for _, width := range []int{40, 79, 80, 116} {
		for _, height := range []int{15, 24} {
			for _, playing := range []bool{false, true} {
				m.Update(tea.WindowSizeMsg{Width: width, Height: height})
				m.pager.playing = playing
				m.pager.notice = "Source coverage warning."
				at, ok := m.pagerPosition()
				if !ok {
					t.Fatal("missing source place")
				}
				state := "paused"
				if playing {
					state = "playing"
				}
				view := m.pagerView()
				for _, required := range []string{fmt.Sprintf("Unit %d, byte %d", at.Unit, at.Offset), "250 wpm nominal, " + state, "Source coverage warning.", "q close"} {
					if !strings.Contains(view, required) {
						t.Errorf("%dx%d playing=%t hides %q:\n%s", width, height, playing, required, view)
					}
				}
				if lipgloss.Height(view) != height {
					t.Errorf("height %d, expected %d", lipgloss.Height(view), height)
				}
				for _, line := range strings.Split(view, "\n") {
					if lipgloss.Width(line) != width {
						t.Errorf("line width %d, expected %d", lipgloss.Width(line), width)
					}
				}
			}
		}
	}
	m.pager.playing = false
	if got := terminalJobTree(t, root); !reflect.DeepEqual(before, got) {
		t.Fatal("footer rendering changed library bytes or disabled bookmarks")
	}
}
