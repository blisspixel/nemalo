package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/scanner"
)

type catalog struct{ err error }

func (c catalog) Search(_ context.Context, q string, _ int, _ int) (discovery.Page, error) {
	return discovery.Page{Query: q, Results: []discovery.Result{}}, c.err
}
func key(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

func TestTerminalWorkflow(t *testing.T) {
	m := newModel(context.Background(), app.Service{Catalog: catalog{}})
	if m.Init() == nil || !strings.Contains(m.View().Content, "Nemalo") {
		t.Fatal("missing initial view/focus")
	}
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 12})
	if m.viewport.Width() != 38 || m.viewport.Height() != 3 {
		t.Fatal("resize ignored")
	}
	_, cmd := m.Update(key(tea.KeyEnter))
	if cmd != nil {
		t.Fatal("empty query submitted")
	}
	m.input.SetValue("Jules Verne")
	_, cmd = m.Update(key(tea.KeyEnter))
	if cmd == nil || !m.busy {
		t.Fatal("search not dispatched")
	}
	_, duplicate := m.Update(key(tea.KeyEnter))
	if duplicate != nil {
		t.Fatal("duplicate operation")
	}
	m.Update(key(tea.KeyTab))
	if m.mode != "search" {
		t.Fatal("busy mode changed")
	}
	m.Update(cmd())
	if m.busy || !strings.Contains(m.viewport.GetContent(), "Jules Verne") {
		t.Fatal("search result missing")
	}
	m.Update(key(tea.KeyTab))
	if m.mode != "inspect" {
		t.Fatal("mode not changed")
	}
	m.input.SetValue(t.TempDir())
	_, cmd = m.Update(key(tea.KeyEnter))
	m.Update(cmd())
	if !strings.Contains(m.viewport.GetContent(), "not scanned") {
		t.Fatal("inspection missing")
	}
	m.Update(key(tea.KeyTab))
	if m.mode != "check" {
		t.Fatal("health mode unavailable")
	}
	p := filepath.Join(t.TempDir(), "paper.pdf")
	if err := os.WriteFile(p, []byte("%PDF-1.7\n%%EOF"), 0600); err != nil {
		t.Fatal(err)
	}
	m.input.SetValue(p)
	_, cmd = m.Update(key(tea.KeyEnter))
	m.Update(cmd())
	if !strings.Contains(m.viewport.GetContent(), "limited_checks_passed") || !strings.Contains(m.viewport.GetContent(), "Antivirus: not_scanned") {
		t.Fatal("health result missing")
	}
	m.Update(key(tea.KeyTab))
	if m.mode != "scan" {
		t.Fatal("explicit antivirus mode unavailable")
	}
	m.service.Scanner = fakeScanner{}
	m.input.SetValue(p)
	_, cmd = m.Update(key(tea.KeyEnter))
	m.Update(cmd())
	if !strings.Contains(m.viewport.GetContent(), "Antivirus: unavailable") {
		t.Fatal("scanner unavailable result hidden")
	}
	m.Update(key(tea.KeyTab))
	if m.mode != "search" {
		t.Fatal("mode not restored")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	m.Update(tea.KeyPressMsg{Text: "a", Code: 'a'})
	if !strings.Contains(m.input.Value(), "a") {
		t.Fatal("text entry failed")
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("quit unavailable")
	}
}

func TestLongResultsRemainVisible(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.viewport.SetWidth(10)
	m.viewport.SetHeight(5)
	m.viewport.SetContent(strings.Repeat("a", 20) + "TAIL")
	if !m.viewport.SoftWrap || !strings.Contains(m.viewport.View(), "TAIL") {
		t.Fatal("long health evidence clipped", m.viewport.View())
	}
}

type fakeScanner struct{}

func (fakeScanner) Scan(context.Context, string) scanner.Result {
	return scanner.Result{Status: "unavailable"}
}

func TestCancellationStaleResultsAndFailures(t *testing.T) {
	m := newModel(context.Background(), app.Service{Catalog: catalog{errors.New("unavailable\x1b[31m")}})
	m.input.SetValue("books")
	_, cmd := m.Update(key(tea.KeyEnter))
	old := cmd()
	m.Update(key(tea.KeyEscape))
	m.Update(old)
	if m.busy || !strings.Contains(m.status, "Cancelled") {
		t.Fatal("late result replaced cancellation")
	}
	_, cmd = m.Update(key(tea.KeyEnter))
	m.Update(cmd())
	if !strings.Contains(m.status, "Incomplete") || strings.Contains(m.status, "\x1b") {
		t.Fatal("error rendering unsafe", m.status)
	}
	m.Update(result{id: m.id, data: make(chan int)})
	if !strings.Contains(m.status, "Output error") {
		t.Fatal("serialization failure hidden")
	}
	m.input.SetValue("books")
	m.Update(key(tea.KeyEnter))
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("active operation cannot quit")
	}
	m.Update(tea.WindowSizeMsg{Width: 1, Height: 1})
	if m.viewport.Height() != 1 {
		t.Fatal("tiny terminal failed")
	}
}
