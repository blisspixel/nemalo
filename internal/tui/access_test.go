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

// offered reports whether the registry offers key here, available now.
func offered(m *model, key string) bool {
	c, ok := m.lookup(key)
	return ok && c.when(m) == ""
}

func typeText(m *model, text string) {
	for _, r := range text {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func shownNames(m *model) []string {
	var names []string
	for _, it := range m.overlay.shown {
		names = append(names, it.cmd.name)
	}
	return names
}

// pick opens the palette, filters by query and runs the first match.
func pick(t *testing.T, m *model, query string) tea.Cmd {
	t.Helper()
	m.Update(ctrlO())
	if m.overlay == nil || m.overlay.kind != palette {
		t.Fatal("Ctrl+O did not open the palette")
	}
	typeText(m, query)
	if len(m.overlay.shown) == 0 {
		t.Fatalf("%q matched nothing", query)
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	return cmd
}

// Every function-key and control-key action of every step is reachable by
// name from the palette (decision 0094's guarantee, kept by 0101).
func TestPaletteReachesEveryKeyedActionByName(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Update(ctrlO())
	listed := map[string]bool{}
	for _, it := range m.overlay.all {
		listed[it.cmd.key+"|"+it.cmd.name] = true
	}
	for _, c := range registry {
		if c.key == "" || c.nav == m.mode || paletteHidden(c) || len(c.key) == 1 || c.key == "space" {
			continue
		}
		if c.key == "esc" || c.key == "f6" || c.key == "enter" && c.focus == listFocus {
			continue // focus-dependent; listed for the focus that applies
		}
		if !listed[c.key+"|"+c.name] {
			t.Errorf("palette omits %s %q", c.key, c.name)
		}
	}
	m.Update(key(tea.KeyEscape))
	if m.overlay != nil {
		t.Fatal("Escape did not close the palette")
	}
}

func TestPaletteSearchesAndExplainsUnavailableActions(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m.Update(ctrlO())
	typeText(m, "acq")
	if names := shownNames(m); len(names) == 0 || !strings.Contains(strings.ToLower(names[0]), "acquir") {
		t.Fatalf("acq found %v", names)
	}
	view := m.View().Content
	if !strings.Contains(view, "review") || !strings.Contains(view, "unavailable: Select an item") || !strings.Contains(view, "Unavailable: Select an item in Evaluate first.") {
		t.Fatalf("tags or reasons missing:\n%s", view)
	}
	m.Update(ctrlO()) // Ctrl+O also closes.
	if m.overlay != nil {
		t.Fatal("Ctrl+O did not close the palette")
	}
	// An unavailable choice states why and changes nothing.
	m.switchMode("search")
	m.Update(ctrlO())
	typeText(m, "result order")
	m.Update(key(tea.KeyEnter))
	if m.overlay == nil || !strings.Contains(m.status, "Unavailable: Only Internet Archive and Gutenberg") || m.order != "" {
		t.Fatal("unavailable action ran or gave no reason", m.status)
	}
	m.Update(key(tea.KeyEscape))
	// Up and Down stop at the ends; typing filters rather than moving.
	m.Update(ctrlO())
	for range 200 {
		m.Update(key(tea.KeyDown))
	}
	if m.overlay.selected != len(m.overlay.shown)-1 {
		t.Fatal("selection passed the last item")
	}
	for range 200 {
		m.Update(key(tea.KeyUp))
	}
	if m.overlay.selected != 0 {
		t.Fatal("selection passed the first item")
	}
	typeText(m, "zzzzqqq")
	if len(m.overlay.shown) != 0 || !strings.Contains(m.View().Content, "No action matches") {
		t.Fatal("nonsense query matched", shownNames(m))
	}
	m.Update(key(tea.KeyEscape))
}

func TestPaletteRunsTheSameHandlerAsTheKey(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.switchMode("holdings")
	pick(t, m, "format filter")
	viaPalette := m.format
	m.Update(key(tea.KeyF4))
	if viaPalette != "pdf" || m.format != "mp3" {
		t.Fatalf("palette %q then key %q", viaPalette, m.format)
	}
	// An action of another step moves there first.
	pick(t, m, "search provider")
	if m.mode != "search" || m.source != "archive" {
		t.Fatal("provider did not change", m.mode, m.source)
	}
	pick(t, m, "jobs: jobs")
	if m.mode != "jobs" {
		t.Fatal("palette did not open Jobs", m.mode)
	}
}

// Enter, network actions and unreviewed writes are prepared, never run.
func TestPalettePreparesInsteadOfRunning(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.input.SetValue("frankenstein")
	if cmd := pick(t, m, "search the selected provider"); cmd != nil && m.busy {
		t.Fatal("network search ran from the palette")
	}
	if m.busy || !strings.Contains(m.status, "Press Enter to search the selected provider. It uses the network.") {
		t.Fatal("search was not prepared", m.status)
	}
	m.switchMode("read")
	pick(t, m, "save a bookmark")
	if m.busy || !strings.Contains(m.status, "Press m to save a bookmark. It writes a file.") {
		t.Fatal("bookmark was not prepared", m.status)
	}
	m.switchMode("scan")
	m.input.SetValue("/x")
	pick(t, m, "antivirus")
	if m.busy || !strings.Contains(m.status, "cloud and sample policy") {
		t.Fatal("scan was not prepared", m.status)
	}
}

func TestPaletteCannotOpenWhileBusyOrConfirmAReview(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.switchMode("state")
	m.busy = true
	m.Update(ctrlO())
	if m.overlay != nil {
		t.Fatal("palette opened while busy")
	}
	m.busy = false
	folder := t.TempDir()
	m.input.SetValue(folder)
	if cmd := pick(t, m, "review library initialization"); cmd != nil {
		t.Fatal("choosing a review ran work", cmd)
	}
	if m.confirmKind != "init" || m.confirmRoot == "" {
		t.Fatal("initialization review was not requested", m.status)
	}
	// The palette is not available during a review, and only the review's
	// own physical Enter confirms it.
	m.Update(ctrlO())
	if m.overlay != nil {
		t.Fatal("palette opened over a review")
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

func TestPaletteCtrlCCancelsAndQuits(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	cancelled := false
	m.cancel = func() { cancelled = true }
	m.Update(ctrlO())
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if m.overlay != nil || cmd == nil || !cancelled {
		t.Fatal("Ctrl+C did not close the palette, cancel work and quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("Ctrl+C did not quit")
	}
}

// Space, ? and : act only with results focused; in a field they are text.
func TestListKeysNeverTakeTypedText(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Init()
	m.entries = []entry{{title: "One", sourceID: "archive:one", detail: "d"}}
	typeText(m, "a ?:")
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if m.overlay != nil || m.input.Value() != "a ?: " {
		t.Fatal("list keys took typed text", m.input.Value())
	}
	m.Update(key(tea.KeyF6))
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if m.overlay == nil || m.overlay.kind != itemMenu || !slices.Contains(shownNames(m), "Prepare evaluation of the selected result") {
		t.Fatal("Space did not list item actions", m.overlay)
	}
	m.Update(key(tea.KeyEscape))
	m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	if m.overlay == nil || m.overlay.kind != helpView || !strings.Contains(m.View().Content, "Check files") {
		t.Fatal("? did not open help")
	}
	m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	if m.overlay != nil {
		t.Fatal("? did not close help")
	}
	m.Update(tea.KeyPressMsg{Code: ':', Text: ":"})
	if m.overlay == nil || m.overlay.kind != palette {
		t.Fatal(": did not open the palette")
	}
	// The item menu runs the item's key: e prepares an evaluation.
	m.Update(key(tea.KeyEscape))
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	typeText(m, "evaluation")
	m.Update(key(tea.KeyEnter))
	if m.mode != "evaluate" || m.input.Value() != "archive:one" {
		t.Fatal("item menu did not run e", m.mode, m.input.Value())
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
		if !strings.Contains(row, "> Query") {
			t.Fatalf("cursor row %d is not the focused field: %q", v.Cursor.Y, row)
		}
		if want := m.fieldCol + m.fieldX[0] + lipgloss.Width(value); v.Cursor.X != want {
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
	m.Update(key(tea.KeyDown))
	v = m.View()
	if v.Cursor == nil || !strings.Contains(strings.Split(v.Content, "\n")[v.Cursor.Y], "> "+keyLabel(m.overlay.shown[1].cmd.key)) {
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
