package tui

import (
	"context"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/reader"
)

func ctrlO() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl} }

func actionNames(m *model) []string {
	var keys []string
	for _, a := range m.actions.items {
		keys = append(keys, a.key)
	}
	return keys
}

// chooseAction opens Actions, moves to key and presses Enter, returning the command.
func chooseAction(t *testing.T, m *model, key string) tea.Cmd {
	t.Helper()
	m.Update(ctrlO())
	if m.actions == nil {
		t.Fatal("Ctrl+O did not open Actions")
	}
	i := slices.Index(actionNames(m), key)
	if i < 0 {
		t.Fatalf("%s not offered: %v", key, actionNames(m))
	}
	for range i {
		m.Update(key2("down"))
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.actions != nil {
		t.Fatal("Actions stayed open after Enter")
	}
	return cmd
}

func key2(name string) tea.KeyPressMsg {
	switch name {
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	}
	return tea.KeyPressMsg{Code: rune(name[0]), Text: name}
}

func TestActionsListOnlyTheKeysEachViewAccepts(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	for _, tc := range []struct {
		mode, source string
		want         []string
	}{
		{"search", "openlibrary", []string{"f6", "f2", "ctrl+right", "ctrl+left"}},
		{"search", "archive", []string{"f6", "f2", "f3", "ctrl+right", "ctrl+left"}},
		{"holdings", "", []string{"f6", "ctrl+n", "f4", "ctrl+right", "ctrl+left"}},
		{"snapshot", "", []string{"f6", "ctrl+n", "f3", "ctrl+a"}},
		{"state", "", []string{"f6", "ctrl+n", "f10", "f7", "f8", "f9", "ctrl+r", "ctrl+p"}},
		{"read", "", []string{"f6", "ctrl+n", "f2", "f4"}},
		{"collections", "", []string{"f6", "ctrl+n", "f8", "f3"}},
		{"scan", "", []string{"f6"}},
	} {
		m.switchMode(tc.mode)
		if tc.source != "" {
			m.source = tc.source
		}
		m.Update(ctrlO())
		if m.actions == nil || !slices.Equal(actionNames(m), tc.want) {
			t.Fatalf("%s/%s: %v", tc.mode, tc.source, m.actions)
		}
		for _, item := range m.actions.items {
			if _, ok := actionKeys[item.key]; !ok || item.label == "" {
				t.Fatalf("%s: unmapped or unlabelled %+v", tc.mode, item)
			}
			// The synthetic key must be the one the handlers compare.
			if got := tea.KeyPressMsg(actionKeys[item.key]).String(); got != item.key {
				t.Fatalf("%s sends %q", item.key, got)
			}
		}
		content := m.View().Content
		if !strings.Contains(content, "Actions (") || !strings.Contains(content, keyLabel(tc.want[len(tc.want)-1])) {
			t.Fatalf("%s: Actions not shown: %s", tc.mode, content)
		}
		m.Update(key(tea.KeyEscape))
		if m.actions != nil {
			t.Fatal("Escape did not close Actions")
		}
	}
	m.report = "a report"
	m.switchMode("scan")
	m.report = "a report"
	m.Update(ctrlO())
	if !slices.Contains(actionNames(m), "f5") {
		t.Fatal("full report missing while a report exists")
	}
	m.Update(ctrlO()) // Ctrl+O also closes.
	if m.actions != nil {
		t.Fatal("Ctrl+O did not close Actions")
	}
}

func TestActionsRunTheSameHandlerAsTheKey(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.switchMode("holdings")
	chooseAction(t, m, "f4")
	viaMenu := m.format
	m.Update(key(tea.KeyF4))
	if viaMenu != "pdf" || m.format != "mp3" {
		t.Fatalf("menu %q then key %q", viaMenu, m.format)
	}
	m.switchMode("search")
	chooseAction(t, m, "f2")
	if m.source != "archive" {
		t.Fatal("provider did not change", m.source)
	}
	m.Update(ctrlO())
	for range 20 {
		m.Update(key2("down"))
	}
	if m.actions.selected != len(m.actions.items)-1 {
		t.Fatal("selection passed the last item")
	}
	for range 20 {
		m.Update(key2("k"))
	}
	if m.actions.selected != 0 {
		t.Fatal("selection passed the first item")
	}
	m.Update(key(tea.KeyEscape))
}

func TestActionsCannotOpenWhileBusyOrConfirmAReview(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.switchMode("state")
	m.busy = true
	m.Update(ctrlO())
	if m.actions != nil {
		t.Fatal("Actions opened while busy")
	}
	m.busy = false
	folder := t.TempDir()
	m.input.SetValue(folder)
	if cmd := chooseAction(t, m, "f7"); cmd != nil {
		t.Fatal("choosing a review ran work", cmd)
	}
	if m.confirmKind != "init" || m.confirmRoot == "" {
		t.Fatal("initialization review was not requested", m.status)
	}
	// Ctrl+O is not available during a review, and only the review's own
	// physical Enter confirms it.
	m.Update(ctrlO())
	if m.actions != nil {
		t.Fatal("Actions opened over a review")
	}
	m.Update(tea.KeyPressMsg{Code: 'e', Text: "enter"})
	if m.confirmRoot == "" {
		t.Fatal("text spelling enter dismissed the review")
	}
	if entries, err := os.ReadDir(folder); err != nil || len(entries) != 0 {
		t.Fatal("review wrote before confirmation", entries, err)
	}
	m.Update(key(tea.KeyEscape))
}

func TestActionsCtrlCCancelsAndQuits(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	cancelled := false
	m.cancel = func() { cancelled = true }
	m.Update(ctrlO())
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if m.actions != nil || cmd == nil || !cancelled {
		t.Fatal("Ctrl+C did not close Actions, cancel work and quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("Ctrl+C did not quit")
	}
}

// The real cursor sits at the edit position: after the field label and the
// cell width of the text before it.
func TestRealCursorFollowsTheFocusedField(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Init()
	for _, tc := range []struct {
		value, before string
		pos           int
	}{{"", "", 0}, {"abc", "abc", 3}, {"日本 x", "日本 x", 4}, {"日本 x", "日本", 2}} {
		// Wide characters take two cells: the column is the drawn width.
		value := tc.before
		m.input.SetValue(tc.value)
		m.input.SetCursor(tc.pos)
		v := m.View()
		lines := strings.Split(v.Content, "\n")
		if v.Cursor == nil {
			t.Fatal("no real cursor in the form")
		}
		row := lines[v.Cursor.Y]
		if !strings.HasPrefix(row, "> ") {
			t.Fatalf("cursor row %d is not the focused field: %q", v.Cursor.Y, row)
		}
		if want := m.fieldX[0] + lipgloss.Width(value); v.Cursor.X != want {
			t.Fatalf("%q at %d: cursor x %d, want %d", tc.value, tc.pos, v.Cursor.X, want)
		}
	}
	m.switchMode("collections")
	m.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	v := m.View()
	if !m.secondary.Focused() || v.Cursor == nil || v.Cursor.Y != m.fieldRow+1 {
		t.Fatal("second field cursor", v.Cursor, m.fieldRow)
	}
	m.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	v = m.View()
	if !m.intake.Focused() || v.Cursor == nil || v.Cursor.Y != m.fieldRow+2 || !strings.Contains(strings.Split(v.Content, "\n")[v.Cursor.Y], "Intake") {
		t.Fatal("intake field cursor", v.Cursor, m.fieldRow)
	}
	m.Update(key(tea.KeyF6))
	if m.View().Cursor != nil {
		t.Fatal("a cursor stayed in the form while results are focused")
	}
	m.Update(key(tea.KeyF6))
	m.Update(ctrlO())
	v = m.View()
	if v.Cursor == nil || !strings.HasPrefix(strings.Split(v.Content, "\n")[v.Cursor.Y], "> F6") {
		t.Fatal("cursor not on the chosen action", v.Cursor)
	}
}

func TestRealCursorInTheReaderFindField(t *testing.T) {
	catalog, root, asset := navigationBook(t)
	m := newModel(context.Background(), contentsService(t))
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	run(m, m.openPager(catalog, root, asset, "epub-text/2", reader.Position{}, false, 250))
	if m.View().Cursor != nil {
		t.Fatal("reading pages placed a cursor")
	}
	press(m, "/")
	for _, r := range "Knowledge" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	v := m.View()
	lines := strings.Split(v.Content, "\n")
	if v.Cursor == nil || v.Cursor.Y != 2 || !strings.Contains(lines[2], "> Knowledge") {
		t.Fatal("find cursor row", v.Cursor)
	}
	if prompt := strings.Index(lines[2], "> "); v.Cursor.X != prompt+2+len("Knowledge") {
		t.Fatalf("find cursor x %d, line %q", v.Cursor.X, lines[2])
	}
}

func TestEvidenceReplayNeedsNoFunctionKey(t *testing.T) {
	m, root, _ := evidenceModel(t)
	evidenceExcerpt(t, m)
	before := terminalJobTree(t, root)
	auxAction(t, m, tea.KeyF4)
	if m.readAux == nil || m.readAux.reference == "" || !strings.Contains(m.readAuxView(), "F3/r replay") {
		t.Fatal("evidence sidecar missing replay")
	}
	token, report := m.readAux.reference, m.readAux.report
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if cmd == nil {
		t.Fatal("r did not replay")
	}
	m.Update(cmd())
	if m.readAux == nil || m.readAux.reference != token || m.readAux.report != report || !reflect.DeepEqual(before, terminalJobTree(t, root)) {
		t.Fatal("r replay differed from the evidence or changed the library")
	}
}
