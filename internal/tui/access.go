package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Keyboard and cursor access (decision 0094). Ctrl+O lists the function- and
// control-key actions the current view accepts. Running one sends that same
// key through the normal handler, so availability and policy stay there; a
// write review still needs its own physical Enter.

type action struct{ key, label string }

type actionMenu struct {
	items    []action
	selected int
}

var actionKeys = map[string]tea.Key{
	"f2": {Code: tea.KeyF2}, "f3": {Code: tea.KeyF3}, "f4": {Code: tea.KeyF4}, "f5": {Code: tea.KeyF5},
	"f6": {Code: tea.KeyF6}, "f7": {Code: tea.KeyF7}, "f8": {Code: tea.KeyF8}, "f9": {Code: tea.KeyF9}, "f10": {Code: tea.KeyF10},
	"ctrl+a": {Code: 'a', Mod: tea.ModCtrl}, "ctrl+n": {Code: 'n', Mod: tea.ModCtrl}, "ctrl+p": {Code: 'p', Mod: tea.ModCtrl}, "ctrl+r": {Code: 'r', Mod: tea.ModCtrl},
	"ctrl+left": {Code: tea.KeyLeft, Mod: tea.ModCtrl}, "ctrl+right": {Code: tea.KeyRight, Mod: tea.ModCtrl},
}

// availableActions mirrors the keys each mode's handlers accept, labelled
// with what they do. The handlers remain the authority.
func (m *model) availableActions() []action {
	if m.busy {
		return nil
	}
	var items []action
	add := func(key, label string) { items = append(items, action{key, label}) }
	if m.resultsFocused {
		add("f6", "Edit the fields")
	} else {
		add("f6", "Browse the results")
	}
	if m.report != "" {
		add("f5", "Show or hide the full report")
	}
	if m.form() {
		add("ctrl+n", "Move to the next field")
	}
	switch m.mode {
	case "search":
		add("f2", "Change the provider")
		if m.source == "archive" || m.source == "gutenberg" {
			add("f3", "Change the result order")
		}
	case "holdings":
		add("f4", "Change the format filter")
	case "inspect", "audit":
		add("f3", "Change the read budget")
	case "snapshot":
		add("f3", "Change the read budget")
		add("ctrl+a", "Turn health metadata on or off")
	case "collections":
		add("f8", "Review the acquisition")
		add("f3", "Change the transfer cap")
	case "state":
		add("f10", "Show recorded jobs")
		add("f7", "Review library initialization")
		add("f8", "Review an import")
		add("f9", "Review a cleanup")
		add("ctrl+r", "Review a rescan")
		add("ctrl+p", "Review a purge")
	case "read":
		add("f2", "Citation report")
		add("f4", "Excerpt evidence")
	}
	if m.mode == "search" || m.mode == "holdings" {
		add("ctrl+right", "Next results page")
		add("ctrl+left", "Previous results page")
	}
	return items
}

func keyLabel(key string) string {
	parts := strings.Split(key, "+")
	for i, p := range parts {
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "+")
}

func (m *model) openActions() {
	if items := m.availableActions(); len(items) > 0 {
		m.actions = &actionMenu{items: items}
	}
}

func (m *model) actionKey(msg tea.KeyPressMsg) tea.Cmd {
	a := m.actions
	switch msg.Keystroke() {
	case "up", "k":
		a.selected = max(0, a.selected-1)
	case "down", "j":
		a.selected = min(len(a.items)-1, a.selected+1)
	case "esc", "ctrl+o":
		m.actions = nil
	case "ctrl+c":
		// The normal handler also cancels running work before quitting.
		m.actions = nil
		_, cmd := m.Update(msg)
		return cmd
	case "enter":
		item := a.items[a.selected]
		m.actions = nil
		_, cmd := m.Update(tea.KeyPressMsg(actionKeys[item.key]))
		return cmd
	}
	return nil
}

func (m *model) actionsView(height int) string {
	lines := []string{"Actions (" + modeTitle(m.mode) + ")"}
	for i, item := range m.actions.items {
		marker := "  "
		if i == m.actions.selected {
			marker = "> "
		}
		lines = append(lines, marker+lipgloss.NewStyle().Width(12).Render(keyLabel(item.key))+item.label)
	}
	return strings.Join(lines[:min(len(lines), max(1, height))], "\n")
}

func modeTitle(mode string) string { return strings.ToUpper(mode[:1]) + mode[1:] }

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
