package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// List interaction (decision 0103): aligned columns, a local filter that
// narrows on each keystroke, paging that continues past the ends, and mouse
// selection in the panel layout. Local work may follow a key or a click; a
// network page waits for its own Enter.

// listFilter narrows the loaded results without a request. all is the list
// as loaded and before its selection when the filter opened.
type listFilter struct {
	input   textinput.Model
	all     []entry
	before  int
	editing bool
}

// more is a pending network page: +1 next, -1 previous, 0 none.
type more int

func (m *model) openFilter() tea.Cmd {
	if m.filter == nil {
		in := textinput.New()
		in.Prompt = ""
		in.CharLimit = 200
		in.Placeholder = "type to narrow this page; Esc restores it"
		in.SetVirtualCursor(false)
		in.SetStyles(textinput.Styles{Cursor: textinput.CursorStyle{Blink: true}})
		m.filter = &listFilter{input: in, all: m.entries, before: m.selected}
	}
	m.filter.editing = true
	m.loadMore = 0
	m.status = "Filtering this page locally; nothing is requested. Enter keeps the filter; Esc restores the list."
	m.resize()
	return m.filter.input.Focus()
}

// clearFilter restores the full list and the selection it had.
func (m *model) clearFilter() {
	if m.filter == nil {
		return
	}
	before := m.filter.before
	m.entries, m.filter = m.filter.all, nil
	m.selectEntry(before)
	m.status = "Filter cleared. The full page is shown."
	m.resize()
}

func (m *model) filterKey(msg tea.KeyPressMsg) tea.Cmd {
	f := m.filter
	switch msg.String() {
	case "esc":
		m.clearFilter()
		return nil
	case "enter":
		f.editing = false
		f.input.Blur()
		m.status = fmt.Sprintf("Filter kept: %d of %d on this page. / edits it; Esc restores the list.", len(m.entries), len(f.all))
		m.resize()
		return nil
	case "up":
		m.selectEntry(m.selected - 1)
		return nil
	case "down":
		m.selectEntry(m.selected + 1)
		return nil
	case "ctrl+c":
		if m.cancel != nil {
			m.cancel()
		}
		return tea.Quit
	}
	var cmd tea.Cmd
	f.input, cmd = f.input.Update(msg)
	m.applyFilter()
	return cmd
}

// applyFilter keeps the entries whose title or summary contains the query,
// ignoring case, in their loaded order.
func (m *model) applyFilter() {
	f := m.filter
	q := strings.ToLower(f.input.Value())
	var kept []entry
	for _, e := range f.all {
		if q == "" || strings.Contains(strings.ToLower(e.title+" "+e.summary), q) {
			kept = append(kept, e)
		}
	}
	m.entries = kept
	m.selectEntry(0)
	m.resize()
}

// filterRow is the filter field and how much of the page it keeps.
func (m *model) filterRow(width int) string {
	f := m.filter
	label := "Filter "
	count := fmt.Sprintf("  %d of %d", len(m.entries), len(f.all))
	// The input draws one cell past its width for the cursor.
	f.input.SetWidth(max(1, width-lipgloss.Width(label)-lipgloss.Width(count)-3))
	marker := " "
	if f.editing {
		marker = ">"
	}
	return marker + " " + label + f.input.View() + m.paint(count, muted, false)
}

// highlight paints the first match of the filter in a row.
func (m *model) highlight(row string) string {
	if m.filter == nil || m.filter.input.Value() == "" {
		return row
	}
	q := m.filter.input.Value()
	i := strings.Index(strings.ToLower(row), strings.ToLower(q))
	if i < 0 || i+len(q) > len(row) {
		return row
	}
	return row[:i] + m.paint(row[i:i+len(q)], accent, true) + row[i+len(q):]
}

// localPages reads its next page from local files; other steps use the
// network and wait for Enter.
func (m *model) localPages() bool {
	return m.mode == "holdings" || (m.mode == "search" && m.source == "gutenberg")
}

// morePages reports whether another page exists in that direction.
func (m *model) morePages(forward bool) bool {
	if !pageable(m) || m.filter != nil || m.input.Value() != m.submittedInput || m.secondary.Value() != m.submittedSecond {
		return false
	}
	if forward {
		return m.offset+len(m.entries) < m.total
	}
	return m.offset > 0
}

// continuePast moves past either end of the page: a local page loads, a
// network page is offered on its own row for Enter.
func (m *model) continuePast(forward bool) tea.Cmd {
	if !m.morePages(forward) {
		return nil
	}
	if m.localPages() {
		m.selectLast = !forward
		return m.turnPage(forward)
	}
	m.loadMore = more(-1)
	word := "previous"
	if forward {
		m.loadMore, word = more(1), "next"
	}
	m.status = "The " + word + " page comes from the network. Enter loads it; nothing is requested until then."
	m.resize()
	return nil
}

func (m *model) moreRow(width int) string {
	text := "Load the next page (network). Enter loads it."
	if m.loadMore < 0 {
		text = "Load the previous page (network). Enter loads it."
	}
	return m.paint(clip("> "+text, width), netRole, true)
}

// hit is a clickable region of the last panel frame.
type hit struct {
	y0, y1, x0, x1 int
	entry          int    // result index, or -1
	step           string // a step to open
}

func (m *model) hitAt(x, y int) (hit, bool) {
	for _, h := range m.hits {
		if y >= h.y0 && y <= h.y1 && x >= h.x0 && x <= h.x1 {
			return h, true
		}
	}
	return hit{}, false
}

// mouse handles clicks and the wheel in the panel layout. Plain mode, the
// reader and reviews ignore it; every action stays on the keyboard.
func (m *model) mouse(msg tea.MouseMsg) tea.Cmd {
	if m.plain || m.pager != nil || m.readAux != nil || m.overlay != nil || m.reviewing() || m.filter != nil && m.filter.editing {
		return nil
	}
	mouse := msg.Mouse()
	switch msg.(type) {
	case tea.MouseWheelMsg:
		step := 1
		if mouse.Button == tea.MouseWheelUp {
			step = -1
		}
		if h, ok := m.hitAt(mouse.X, mouse.Y); ok && h.entry >= 0 && !m.details && !m.evidence {
			m.selectEntry(m.selected + step)
			return nil
		}
		for range 3 {
			if step < 0 {
				m.viewport.ScrollUp(1)
				m.preview.ScrollUp(1)
			} else {
				m.viewport.ScrollDown(1)
				m.preview.ScrollDown(1)
			}
		}
		return nil
	case tea.MouseClickMsg:
		if mouse.Button != tea.MouseLeft || m.busy {
			return nil
		}
		h, ok := m.hitAt(mouse.X, mouse.Y)
		if !ok {
			return nil
		}
		if h.step != "" {
			return m.switchMode(h.step)
		}
		if h.entry < 0 || h.entry >= len(m.entries) {
			return nil
		}
		now := m.now()
		double := m.resultsFocused && h.entry == m.selected && now.Sub(m.lastClick) < 500*time.Millisecond
		m.lastClick = now
		if !m.resultsFocused {
			m.resultsFocused = true
			m.input.Blur()
			m.secondary.Blur()
			m.intake.Blur()
		}
		m.loadMore = 0
		m.selectEntry(h.entry)
		m.resize()
		if double {
			m.lastClick = time.Time{}
			_, cmd := m.Update(keyPress("enter"))
			return cmd
		}
	}
	return nil
}

func (m *model) now() time.Time {
	if m.clock != nil {
		return m.clock()
	}
	return time.Now()
}
