package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/blisspixel/nemalo/internal/app"
)

func TestStateInitializationReviewAndDispatch(t *testing.T) {
	root := t.TempDir()
	m := newModel(context.Background(), app.New())
	m.Update(tea.KeyPressMsg{Code: '9', Mod: tea.ModAlt})
	if m.mode != "state" {
		t.Fatal("direct state navigation unavailable")
	}
	m.Update(key(tea.KeyF7))
	if m.confirmRoot != "" {
		t.Fatal("empty root entered write review")
	}
	m.input.SetValue(root)
	_, cmd := m.Update(key(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("status unavailable")
	}
	m.Update(cmd())
	if !strings.Contains(m.report, "not_initialized") {
		t.Fatal("status hidden", m.report)
	}
	previous := m.report
	m.Update(key(tea.KeyF7))
	if m.confirmRoot != root || !strings.Contains(m.report, strconv.Quote(root)) || !strings.Contains(m.report, "journal.jsonl") {
		t.Fatal("review lost explicit scope", m.report)
	}
	for _, keyMsg := range []tea.KeyPressMsg{{Code: 'x', Text: "x"}, {Code: '1', Mod: tea.ModAlt}, {Code: tea.KeyTab}, {Code: tea.KeyF6}} {
		m.Update(keyMsg)
	}
	if m.mode != "state" || m.input.Value() != root {
		t.Fatal("review scope changed")
	}
	m.Update(key(tea.KeyPgDown))
	m.Update(key(tea.KeyPgUp))
	if _, err := os.Stat(filepath.Join(root, ".nemalo")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("review mutated root")
	}
	m.Update(key(tea.KeyEscape))
	if m.confirmRoot != "" || m.report != previous || !m.input.Focused() {
		t.Fatal("cancel lost previous state")
	}
	m.Update(key(tea.KeyF7))
	_, cmd = m.Update(key(tea.KeyEnter))
	if cmd == nil || !m.busy || m.confirmRoot != "" {
		t.Fatal("confirmed initialization not dispatched")
	}
	if _, err := os.Stat(filepath.Join(root, ".nemalo")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("write happened outside service command")
	}
	m.Update(cmd())
	if m.busy || !strings.Contains(m.report, `Control state: "initialized"`) || !strings.Contains(m.report, "Content was not imported") {
		t.Fatal("initialization evidence missing", m.report)
	}
	if !m.input.Focused() {
		t.Fatal("completed modal operation left editing disabled")
	}
	_, cmd = m.Update(key(tea.KeyEnter))
	m.Update(cmd())
	if !strings.Contains(m.report, "Journal records: 2") {
		t.Fatal("status not routed to shared state")
	}
	m.Update(key(tea.KeyF7))
	_, cmd = m.Update(key(tea.KeyEnter))
	m.Update(key(tea.KeyEscape))
	if m.busy || !m.input.Focused() {
		t.Fatal("cancelled modal operation left editing disabled")
	}
	m.Update(cmd()) // The cancelled generation cannot replace the current state.
}

func TestStateReviewLayoutAndQuit(t *testing.T) {
	m := newModel(context.Background(), app.New())
	m.switchMode("state")
	m.input.SetValue(filepath.Join(t.TempDir(), strings.Repeat("long folder ", 50)))
	for _, size := range [][2]int{{40, 20}, {80, 24}, {112, 38}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.Update(key(tea.KeyF7))
		v := m.View().Content
		if lipgloss.Width(v) > size[0] || lipgloss.Height(v) > size[1] {
			t.Fatal("review overflow", size, lipgloss.Width(v), lipgloss.Height(v))
		}
		m.Update(key(tea.KeyF7))
		if m.confirmRoot != "" {
			t.Fatal("second F7 did not return")
		}
	}
	m.Update(key(tea.KeyF7))
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("quit blocked by review")
	}
}
