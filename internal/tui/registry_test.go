package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	keybind "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/app"
)

// candidateKeys is every key a person could press outside the reader.
func candidateKeys() []string {
	var keys []string
	for i := 1; i <= 12; i++ {
		keys = append(keys, fmt.Sprintf("f%d", i))
	}
	for r := 'a'; r <= 'z'; r++ {
		keys = append(keys, "ctrl+"+string(r), "alt+"+string(r), string(r))
	}
	for r := '0'; r <= '9'; r++ {
		keys = append(keys, "alt+"+string(r), string(r))
	}
	for _, r := range "?:/'\"!@#$%^&*()-_=+[]{};,.<>`~\\|" {
		keys = append(keys, string(r))
	}
	return append(keys, "enter", "esc", "tab", "shift+tab", "up", "down", "home", "end", "pgup", "pgdown", "space", "left", "right", "backspace", "delete", "ctrl+left", "ctrl+right")
}

// typing is true for keys a focused field consumes as text editing.
func typing(m *model, pressed string) bool {
	if len([]rune(pressed)) == 1 || pressed == "space" {
		return true
	}
	k := m.input.KeyMap
	for _, b := range []keybind.Binding{k.CharacterForward, k.CharacterBackward, k.WordForward, k.WordBackward, k.DeleteWordBackward, k.DeleteWordForward, k.DeleteAfterCursor, k.DeleteBeforeCursor, k.DeleteCharacterBackward, k.DeleteCharacterForward, k.LineStart, k.LineEnd, k.Paste, k.AcceptSuggestion, k.NextSuggestion, k.PrevSuggestion} {
		if slices.Contains(b.Keys(), pressed) {
			return true
		}
	}
	return false
}

func parityModel(t *testing.T, step string, list, busy bool) *model {
	t.Helper()
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m.Init()
	m.switchMode(step)
	m.input.SetValue("value")
	m.secondary.SetValue("second")
	m.intake.SetValue("/intake")
	m.submittedInput, m.submittedSecond = "value", "second"
	m.setReport("report")
	m.entries = []entry{{title: "One", summary: "s", detail: "d", sourceID: "archive:one", managed: true, recording: "archive:rec"}, {title: "Two", detail: "d2", sourceID: "archive:two"}}
	if step == "read" {
		m.reader = readerState{asset: "sha256:x", representation: "epub-structure/1", next: "cursor"}
	}
	if list {
		m.Update(keyPress("f6"))
	}
	m.busy = busy
	return m
}

func fingerprint(m *model) string {
	return fmt.Sprintf("%s|%s|%q|%q|%t|%t|%t|%t|%d|%t|%t|%q|%q|%q|%s|%s|%s|%t|%t|%t|%t|%s|%s|%t|%t|%t|%t|%t|%t|%t|%t|%d|%d|%d|%t",
		m.mode, m.status, m.report, m.viewport.GetContent(), m.busy, m.resultsFocused, m.details, m.evidence, m.selected,
		m.secondaryFocused, m.intakeFocused, m.input.Value(), m.secondary.Value(), m.intake.Value(), m.source, m.order, m.format,
		m.assess, m.largeBudget, m.collectionLargeBudget, m.overlay != nil, m.confirmKind, m.confirmRoot,
		m.downloadReview != nil, m.recordingReview != nil, m.collectionReview != nil, m.readAux != nil, m.pager != nil,
		m.initializeAction || m.importAction || m.rescanAction || m.cleanupPlan || m.purgePlan, m.restorePlan, m.restoreAction,
		m.offset, m.id, len(m.entries), m.viewport.YOffset() > 0)
}

// Every key that changes the main view is in the registry for that step and
// focus, and every listed, available key does something (decision 0101).
func TestRegistryMatchesTheHandlers(t *testing.T) {
	for _, step := range steps() {
		for _, list := range []bool{false, true} {
			for _, busy := range []bool{false, true} {
				for _, pressed := range candidateKeys() {
					m := parityModel(t, step, list, busy)
					if !list && !busy && typing(m, pressed) {
						continue
					}
					c, listed := m.lookup(pressed)
					before := fingerprint(m)
					_, cmd := m.Update(keyPress(pressed))
					changed := fingerprint(m) != before || cmd != nil
					where := fmt.Sprintf("%s list=%t busy=%t key %q", step, list, busy, pressed)
					switch {
					case changed && !listed:
						t.Errorf("%s: handled but missing from the registry", where)
					case listed && !changed && !c.unprobed && c.when(m) == "" && !(busy && typing(m, pressed) && !list):
						t.Errorf("%s: listed as available (%s) but did nothing", where, c.name)
					}
				}
			}
		}
	}
}

func TestRegistryKeysRoundTrip(t *testing.T) {
	for _, c := range registry {
		if c.key == "" {
			if c.nav != "jobs" {
				t.Errorf("%q has no key", c.name)
			}
			continue
		}
		if got := keyPress(c.key).String(); got != c.key {
			t.Errorf("%s sends %q", c.key, got)
		}
		if c.name == "" || c.when == nil {
			t.Errorf("%s is unnamed or has no availability", c.key)
		}
	}
	for _, step := range steps() {
		if stepTitle(step) == "" || !slices.Contains(sectionOf(step).steps, step) {
			t.Errorf("step %s has no title or section", step)
		}
	}
	if !slices.Equal(steps()[:2], []string{"search", "evaluate"}) || len(steps()) != len(modes)+1 {
		t.Fatal("steps", steps())
	}
}

// The footer has at most six keys, ends with quit, and follows focus.
func TestFooterComesFromTheRegistry(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	for _, step := range steps() {
		m.switchMode(step)
		keys := m.footerKeys()
		if len(keys) > 6 || len(keys) == 0 || keys[len(keys)-1].key != "ctrl+c" {
			t.Fatalf("%s footer %v", step, keys)
		}
		footer := m.footer()
		if strings.Contains(footer, "\n") {
			t.Fatalf("%s footer wraps: %q", step, footer)
		}
	}
	m.switchMode("holdings")
	m.entries = []entry{{title: "One", detail: "d"}, {title: "Two", detail: "d"}}
	m.Update(keyPress("f6"))
	if f := m.footer(); !strings.Contains(f, "Up/Down select") || !strings.Contains(f, "Space item actions") || !strings.Contains(f, "? help") {
		t.Fatal(f)
	}
	m.busy = true
	if f := m.footer(); f != "Esc cancel  Ctrl+C quit" {
		t.Fatal(f)
	}
}
