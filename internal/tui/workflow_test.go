package tui

import (
	"context"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/charmbracelet/colorprofile"
)

type pagedCatalog struct{ offsets *[]int }

func (p pagedCatalog) Search(_ context.Context, query string, _, offset int) (discovery.Page, error) {
	*p.offsets = append(*p.offsets, offset)
	return discovery.Page{Source: "openlibrary", Query: query, Offset: offset, Total: 25, Results: []discovery.Result{{Title: fmt.Sprintf("Page at %d", offset)}}}, nil
}

func TestNavigationPreservesScopedDraftsAndResults(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.input.SetValue("Dante")
	m.status = "Previous search"
	m.setReport("Search evidence")
	m.Update(tea.KeyPressMsg{Code: '6', Mod: tea.ModAlt})
	if m.mode != "holdings" || m.input.Value() != "" || strings.Contains(m.viewport.GetContent(), "Search evidence") {
		t.Fatal("search leaked into holdings")
	}
	m.input.SetValue("catalog.json")
	m.secondary.SetValue("Italian")
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.mode != "snapshot" {
		t.Fatal("reverse navigation ignored")
	}
	m.Update(tea.KeyPressMsg{Code: '1', Mod: tea.ModAlt})
	if m.input.Value() != "Dante" || m.status != "Previous search" || m.viewport.GetContent() != "Search evidence" {
		t.Fatal("search draft lost")
	}
	m.switchMode("holdings")
	if m.input.Value() != "catalog.json" || m.secondary.Value() != "Italian" {
		t.Fatal("library fields lost")
	}
	m.seedLibraryForms("new.json", "new-root")
	if m.drafts["holdings"].input != "catalog.json" {
		t.Fatal("active draft unexpectedly replaced")
	}
	m.switchMode("search")
	m.seedLibraryForms("other.json", "other-root")
	if m.drafts["holdings"].input != "catalog.json" || m.drafts["audit"].input != "new.json" {
		t.Fatal("existing library context overwritten")
	}
	m.busy = true
	m.Update(tea.KeyPressMsg{Code: '8', Mod: tea.ModAlt})
	if m.mode != "search" {
		t.Fatal("busy shortcut switched operations")
	}
}

func TestSearchPagingAndEditedInputGuard(t *testing.T) {
	offsets := []int{}
	m := newModel(context.Background(), app.Service{Providers: map[string]app.Searcher{"openlibrary": pagedCatalog{&offsets}}})
	m.input.SetValue("Dante")
	m.Update(m.start(true)())
	for _, expected := range []int{10, 20} {
		_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModCtrl})
		if cmd == nil {
			t.Fatal("next page unavailable")
		}
		m.Update(cmd())
		if m.offset != expected {
			t.Fatal("wrong search offset", m.offset)
		}
	}
	if m.turnPage(true) != nil {
		t.Fatal("advanced past last page")
	}
	m.Update(m.turnPage(false)())
	if m.offset != 10 {
		t.Fatal("previous page ignored")
	}
	m.Update(result{id: m.id, data: discovery.Page{}, err: fmt.Errorf("temporary source failure")})
	if m.total != 25 || m.offset != 10 {
		t.Fatal("failed page erased navigation context")
	}
	m.input.SetValue("changed")
	if m.turnPage(true) != nil || !strings.Contains(m.status, "Inputs changed") {
		t.Fatal("paging submitted edited query")
	}
	m.Update(m.start(true)())
	if m.offset != 0 || len(offsets) != 5 {
		t.Fatal("new query retained old offset", offsets)
	}
	m.Update(key(tea.KeyF2))
	if m.total != 0 || m.viewport.GetContent() != "" {
		t.Fatal("old provider results retained")
	}
	m.busy = true
	if m.turnPage(true) != nil {
		t.Fatal("busy paging dispatched")
	}
}

func TestHoldingsPaginationAndBudget(t *testing.T) {
	root := t.TempDir()
	for i := range 51 {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("%02d.txt", i)), []byte(fmt.Sprint(i)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	catalogFile := filepath.Join(t.TempDir(), "catalog.json")
	service := app.New()
	if _, err := service.Snapshot(context.Background(), root, catalogFile, inventory.Defaults(), false); err != nil {
		t.Fatal(err)
	}
	m := newModel(context.Background(), service)
	m.format = "all"
	m.switchMode("holdings")
	m.input.SetValue(catalogFile)
	m.Update(m.start(true)())
	m.Update(m.turnPage(true)())
	if m.offset != 50 || m.total != 51 || !strings.Contains(m.viewport.GetContent(), "offset: 50") {
		t.Fatal("last holdings asset inaccessible")
	}
	if m.turnPage(true) != nil {
		t.Fatal("past last holdings page")
	}
	m.Update(m.turnPage(false)())
	if m.offset != 0 {
		t.Fatal("holdings previous page ignored")
	}
	m.switchMode("inspect")
	m.Update(key(tea.KeyF3))
	if !m.largeBudget {
		t.Fatal("larger budget unavailable")
	}
	if !strings.Contains(m.contextLine(), "5 GiB") {
		t.Fatal("budget not disclosed")
	}
	m.input.SetValue(root)
	m.Update(m.start(true)())
	m.Update(key(tea.KeyF3))
	if m.largeBudget {
		t.Fatal("budget not restored")
	}
	m.switchMode("check")
	m.Update(key(tea.KeyF3))
	if m.largeBudget {
		t.Fatal("irrelevant budget shortcut changed policy")
	}
	m.switchMode("holdings")
	m.format = "epub"
	m.total = 10
	m.setReport("old EPUB results")
	m.Update(key(tea.KeyF4))
	if m.format != "pdf" || m.total != 0 || m.viewport.GetContent() != "" {
		t.Fatal("format switch retained stale results")
	}
	m.busy = true
	m.Update(key(tea.KeyF4))
	if m.format != "pdf" {
		t.Fatal("format changed during operation")
	}
	m.busy = false
	for _, expected := range []string{"mp3", "all", "epub"} {
		m.Update(key(tea.KeyF4))
		if m.format != expected {
			t.Fatal("format cycling incomplete")
		}
	}
}

func TestResponsiveFramesAndLongErrors(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	for _, size := range [][2]int{{40, 20}, {80, 24}, {110, 34}, {160, 45}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, name := range modes {
			m.switchMode(name)
			view := m.View().Content
			if lipgloss.Height(view) > size[1] || lipgloss.Width(view) > size[0] {
				t.Fatalf("%s overflows %dx%d: %dx%d", name, size[0], size[1], lipgloss.Width(view), lipgloss.Height(view))
			}
		}
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	err := fmt.Errorf("unavailable %s", strings.Repeat("detail ", 200))
	m.Update(result{id: m.id, data: discovery.Page{}, err: err})
	if lipgloss.Height(m.View().Content) > 24 || !strings.Contains(m.report, err.Error()) {
		t.Fatal("long error breaks layout or loses evidence")
	}
	for _, size := range [][2]int{{1, 1}, {30, 8}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		if lipgloss.Height(m.View().Content) > size[1] || lipgloss.Width(m.View().Content) > size[0] {
			t.Fatal("small terminal overflow")
		}
	}
}

func TestColorPreferencesAndBusyInput(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.ColorProfileMsg{Profile: colorprofile.TrueColor})
	if !m.color || !strings.Contains(m.View().Content, "\x1b[") {
		t.Fatal("capable terminal has no styles")
	}
	m.Update(tea.BackgroundColorMsg{Color: color.RGBA{R: 255, G: 255, B: 255, A: 255}})
	if m.dark || !strings.Contains(m.paint("accent", accent, true), "67;56;202") {
		t.Fatal("light palette not applied")
	}
	m.Update(tea.BackgroundColorMsg{Color: color.Black})
	if !m.dark {
		t.Fatal("dark palette not restored")
	}
	t.Setenv("NO_COLOR", "1")
	m.Update(tea.ColorProfileMsg{Profile: colorprofile.TrueColor})
	if m.color || strings.Contains(m.paint("plain", accent, true), "\x1b") {
		t.Fatal("NO_COLOR ignored")
	}
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "dumb")
	m.Update(tea.ColorProfileMsg{Profile: colorprofile.ANSI})
	if m.color {
		t.Fatal("dumb terminal styled")
	}
	t.Setenv("TERM", "xterm-256color")
	m.Update(tea.ColorProfileMsg{Profile: colorprofile.NoTTY})
	if m.color {
		t.Fatal("nonterminal styled")
	}
	m.input.SetValue("original")
	m.busy = true
	m.Update(tea.KeyPressMsg{Text: "x", Code: 'x'})
	if m.input.Value() != "original" {
		t.Fatal("running operation's input changed")
	}
}
