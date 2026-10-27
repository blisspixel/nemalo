package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Keyboard and cursor access (decisions 0094 and 0101). Ctrl+O opens a
// searchable palette of every registry action; Space lists the selected
// item's actions and ? shows help. Running an entry sends its key through the
// normal handler, so availability and policy stay there. Entries that run on
// Enter, use the network or write without a review only move to their step
// and name the key; a write review still needs its own physical Enter.

type overlayKind int

const (
	palette overlayKind = iota
	itemMenu
	helpView
)

type overlayItem struct {
	cmd     command
	step    string // the step the action runs in
	heading string // help groups items under section headings
	why     string // why it is unavailable, if it is
	text    string // lowercased search text
}

type overlay struct {
	kind     overlayKind
	query    textinput.Model
	all      []overlayItem
	shown    []overlayItem
	selected int
}

func (o *overlay) title() string {
	switch o.kind {
	case itemMenu:
		return "Item actions"
	case helpView:
		return "Help"
	}
	return "Actions"
}

func (m *model) openOverlay(kind overlayKind) {
	if m.busy {
		return
	}
	q := textinput.New()
	q.Prompt = ""
	q.CharLimit = 100
	q.Placeholder = "type to filter"
	q.SetVirtualCursor(false)
	q.SetStyles(textinput.Styles{Cursor: textinput.CursorStyle{Blink: true}})
	q.Focus()
	o := &overlay{kind: kind, query: q}
	switch kind {
	case palette:
		o.all = m.paletteItems()
	case itemMenu:
		o.all = m.menuItems()
	case helpView:
		o.all = m.helpItems()
	}
	if len(o.all) == 0 {
		return
	}
	o.filter()
	m.overlay = o
	m.resize()
}

func (m *model) itemFor(c command, step string) overlayItem {
	it := overlayItem{cmd: c, step: step}
	switch {
	case m.busy:
		it.why = reasonBusy
	case step == m.mode || c.steps == nil || c.nav != "":
		it.why = c.when(m)
	case c.focus == listFocus:
		it.why = "Select an item in " + stepTitle(step) + " first"
	}
	where := ""
	if c.steps != nil && c.nav == "" {
		where = sectionOf(step).name + " " + stepTitle(step)
	}
	it.text = strings.ToLower(strings.Join([]string{c.name, keyLabel(c.key), where, effectTag(m.effectOf(c))}, " "))
	return it
}

// paletteItems is every action once: the current step's first, then moves
// to other steps and their actions in section order.
func (m *model) paletteItems() []overlayItem {
	var current, other []overlayItem
	for _, c := range registry {
		switch {
		case c.nav != "":
			if c.nav != m.mode {
				other = append(other, m.itemFor(c, c.nav))
			}
		case c.inStep(m.mode):
			// Escape and F6 mean different things in each focus; list the one
			// that applies now.
			if (c.key == "esc" || c.key == "f6") && !c.applies(m) || paletteHidden(c) {
				continue
			}
			if !slices.ContainsFunc(current, func(it overlayItem) bool { return it.cmd.key == c.key && it.cmd.focus == c.focus }) {
				current = append(current, m.itemFor(c, m.mode))
			}
		case c.steps != nil:
			step := c.steps[0]
			if !slices.ContainsFunc(other, func(it overlayItem) bool { return it.cmd.key == c.key && it.cmd.name == c.name }) {
				other = append(other, m.itemFor(c, step))
			}
		}
	}
	// Available actions here come first; the registry order is kept otherwise.
	slices.SortStableFunc(current, func(a, b overlayItem) int {
		return boolRank(a.why != "") - boolRank(b.why != "")
	})
	return append(current, other...)
}

// paletteHidden leaves out keys that only open this list or move within it.
func paletteHidden(c command) bool {
	switch c.key {
	case "ctrl+o", ":", "up", "down", "home", "end", "pgup", "pgdown":
		return true
	}
	return false
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// menuItems is what can be done with the selected item in this step.
func (m *model) menuItems() []overlayItem {
	var items []overlayItem
	for _, c := range registry {
		if c.focus != listFocus || c.steps == nil && c.key != "enter" || !c.inStep(m.mode) {
			continue
		}
		if slices.ContainsFunc(items, func(it overlayItem) bool { return it.cmd.key == c.key }) {
			continue
		}
		items = append(items, m.itemFor(c, m.mode))
	}
	if f5, ok := m.lookup("f5"); ok {
		items = append(items, m.itemFor(f5, m.mode))
	}
	return items
}

// helpItems is every key, grouped by section.
func (m *model) helpItems() []overlayItem {
	var items []overlayItem
	add := func(heading string, c command, step string) {
		if c.key == "" {
			return
		}
		it := m.itemFor(c, step)
		it.heading, it.why = heading, ""
		items = append(items, it)
	}
	for _, c := range registry {
		if c.steps == nil && c.nav == "" {
			add("Everywhere", c, m.mode)
		}
	}
	for _, s := range sections {
		for _, c := range registry {
			if c.nav != "" && slices.Contains(s.steps, c.nav) {
				add(s.name, c, c.nav)
			}
		}
		for _, c := range registry {
			if c.steps != nil && slices.Contains(s.steps, c.steps[0]) {
				add(s.name, c, c.steps[0])
			}
		}
	}
	return items
}

// filter keeps the items matching the query, best first; help keeps its
// section order.
func (o *overlay) filter() {
	q := strings.ToLower(strings.TrimSpace(o.query.Value()))
	type scored struct {
		it    overlayItem
		score int
	}
	var kept []scored
	for _, it := range o.all {
		if s := matchScore(q, it.text); s >= 0 {
			kept = append(kept, scored{it, s})
		}
	}
	if q != "" && o.kind != helpView {
		slices.SortStableFunc(kept, func(a, b scored) int { return b.score - a.score })
	}
	o.shown = o.shown[:0]
	for _, k := range kept {
		o.shown = append(o.shown, k.it)
	}
	o.selected = max(0, min(o.selected, len(o.shown)-1))
}

// matchScore ranks a whole-word start above a substring above letters in
// order; -1 means no match.
func matchScore(q, text string) int {
	if q == "" {
		return 0
	}
	if i := strings.Index(text, q); i >= 0 {
		if i == 0 || text[i-1] == ' ' {
			return 300 - min(i, 100)
		}
		return 200 - min(i, 100)
	}
	at := 0
	for _, r := range q {
		i := strings.IndexRune(text[at:], r)
		if i < 0 {
			return -1
		}
		at += i + len(string(r))
	}
	return 100 - min(at, 99)
}

func (m *model) overlayKey(msg tea.KeyPressMsg) tea.Cmd {
	o := m.overlay
	switch msg.String() {
	case "up":
		o.selected = max(0, o.selected-1)
		return nil
	case "down":
		o.selected = max(0, min(len(o.shown)-1, o.selected+1))
		return nil
	case "pgup":
		o.selected = max(0, o.selected-10)
		return nil
	case "pgdown":
		o.selected = max(0, min(len(o.shown)-1, o.selected+10))
		return nil
	case "esc", "ctrl+o":
		m.overlay = nil
		m.resize()
		return nil
	case "?":
		if o.kind == helpView {
			m.overlay = nil
			m.resize()
			return nil
		}
	case "ctrl+c":
		// The normal handler also cancels running work before quitting.
		m.overlay = nil
		_, cmd := m.Update(msg)
		return cmd
	case "enter":
		if o.kind == helpView || len(o.shown) == 0 {
			m.overlay = nil
			m.resize()
			return nil
		}
		return m.runItem(o.shown[o.selected])
	}
	var cmd tea.Cmd
	o.query, cmd = o.query.Update(msg)
	o.filter()
	return cmd
}

// prepareOnly actions are not run from a list: the person presses the key.
func (m *model) prepareOnly(c command) bool {
	if c.key == "enter" && c.focus == formFocus {
		return true
	}
	switch m.effectOf(c) {
	case network, writes, scans:
		return true
	}
	return false
}

func (m *model) runItem(it overlayItem) tea.Cmd {
	c := it.cmd
	if it.why != "" {
		m.status = "Unavailable: " + it.why + "."
		return nil
	}
	m.overlay = nil
	if c.nav != "" {
		return m.switchMode(c.nav)
	}
	var cmds []tea.Cmd
	if it.step != m.mode {
		cmds = append(cmds, m.switchMode(it.step))
	}
	switch {
	case c.focus == listFocus && !m.resultsFocused:
		m.resultsFocused = true
		m.input.Blur()
		m.secondary.Blur()
		m.intake.Blur()
	case c.focus == formFocus && m.resultsFocused:
		cmds = append(cmds, m.focusForm())
	}
	if m.prepareOnly(c) {
		m.status = fmt.Sprintf("Press %s to %s.", keyLabel(c.key), lowerFirst(c.name))
		switch m.effectOf(c) {
		case network:
			m.status += " It uses the network."
		case writes:
			m.status += " It writes a file."
		case scans:
			m.status += " The scanner's own cloud and sample policy applies."
		}
		m.resize()
		return tea.Batch(cmds...)
	}
	m.resize()
	_, cmd := m.Update(keyPress(c.key))
	return tea.Batch(append(cmds, cmd)...)
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// overlayLines renders the open list in width cells and height rows, and
// returns the row of the chosen item for the real cursor (-1 when hidden).
func (m *model) overlayLines(width, height int) ([]string, int) {
	o := m.overlay
	o.query.SetWidth(max(1, width-7))
	lines := []string{"Find: " + o.query.View()}
	var rows []string
	selectedRow := -1
	heading := ""
	for i, it := range o.shown {
		if it.heading != "" && it.heading != heading {
			heading = it.heading
			rows = append(rows, m.paint(heading, accent, true))
		}
		marker := "  "
		if i == o.selected {
			marker = "> "
			selectedRow = len(rows)
		}
		key := lipgloss.NewStyle().Width(12).Render(keyLabel(it.cmd.key))
		row := marker + key + it.cmd.name
		tag := effectTag(m.effectOf(it.cmd))
		var notes []string
		if it.why != "" {
			notes = append(notes, "unavailable: "+it.why)
		} else if it.cmd.steps != nil && it.step != m.mode && it.cmd.nav == "" {
			where := sectionOf(it.step).name
			if where != stepTitle(it.step) {
				where += " / " + stepTitle(it.step)
			}
			notes = append(notes, "in "+where)
		}
		suffix := strings.Join(notes, "; ")
		switch {
		case it.why != "":
			row = m.paint(row, muted, false)
		case i == o.selected:
			row = m.paint(row, accent, true)
		}
		if tag != "" {
			r := warning
			if tag == "network" {
				r = netRole
			}
			if it.why != "" {
				r = muted
			}
			row += "  " + m.paint(tag, r, false)
		}
		if suffix != "" {
			row += "  " + m.paint(suffix, muted, false)
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		rows = append(rows, m.paint("No action matches.", muted, false))
	}
	room := max(1, height-1)
	start := 0
	if selectedRow >= room {
		start = selectedRow - room + 1
	}
	end := min(len(rows), start+room)
	for _, r := range rows[start:end] {
		lines = append(lines, clip(r, width))
	}
	if selectedRow >= 0 {
		selectedRow = selectedRow - start + 1
	}
	return lines, selectedRow
}

// inputCursor returns the real terminal cursor for a focused input whose
// value starts at column x on row y. The column is the cell width of the
// visible text before the edit position, as drawn.
func inputCursor(in textinput.Model, x, y, width int) *tea.Cursor {
	c := in.Cursor()
	if c == nil {
		return nil
	}
	runes := []rune(in.Value())
	before := string(runes[:min(len(runes), max(0, in.Position()))])
	// A value wider than the field scrolls; the edit position then stays at
	// the field's right edge, where Bubbles also caps it.
	c.Position.X = min(max(0, width-1), x+min(lipgloss.Width(before), in.Width()))
	c.Position.Y = y
	return c
}
