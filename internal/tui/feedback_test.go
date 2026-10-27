package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/scanner"
	"github.com/charmbracelet/colorprofile"
)

func clocked(m *model) *time.Time {
	now := time.Unix(1000, 0)
	m.clock = func() time.Time { return now }
	return &now
}

// Running work shows elapsed time and a moving spinner; reduced motion
// shows neither a spinner nor a line that changes more than every five
// seconds, and ticks once a second.
func TestBusyShowsElapsedTimeAndRespectsReducedMotion(t *testing.T) {
	for _, reduced := range []bool{false, true} {
		m := newModel(context.Background(), app.Service{})
		m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		m.reduceMotion = reduced
		now := clocked(m)
		m.switchMode("holdings")
		m.input.SetValue(t.TempDir())
		if _, cmd := m.Update(key(tea.KeyEnter)); cmd == nil || !m.busy {
			t.Fatal("no work started")
		}
		*now = now.Add(3 * time.Second)
		first := m.busyLabel()
		m.Update(tick{})
		second := m.busyLabel()
		*now = now.Add(4 * time.Second)
		late := m.busyLabel()
		switch {
		case !reduced && (first == second || !strings.Contains(first, "3s") || !strings.Contains(late, "7s")):
			t.Fatalf("motion: %q %q %q", first, second, late)
		case reduced && (first != second || strings.ContainsAny(first, `|/\`) || !strings.Contains(first, "0s") || !strings.Contains(late, "5s")):
			t.Fatalf("reduced: %q %q %q", first, second, late)
		}
		if !strings.Contains(m.View().Content, "Elapsed") {
			t.Fatal("busy content has no elapsed time")
		}
		m.Update(key(tea.KeyEscape))
		if m.busy || !m.started.IsZero() {
			t.Fatal("cancel kept the busy state")
		}
	}
}

// A bar appears only for counted bytes and stays below 100% while running.
func TestProgressBarOnlyForCountedWork(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.busy = true
	report := m.trackProgress()
	report("transfer", 50, 100)
	lines := strings.Join(m.busyLines(80), "\n")
	if !strings.Contains(lines, "Phase: transfer") || !strings.Contains(lines, "] 50%") {
		t.Fatal(lines)
	}
	report("transfer", 100, 100)
	if lines = strings.Join(m.busyLines(80), "\n"); strings.Contains(lines, "100%") || !strings.Contains(lines, "] 99%") {
		t.Fatal("bar reached 100% before a result:", lines)
	}
	report("transfer", 10, 0)
	if lines = strings.Join(m.busyLines(80), "\n"); strings.Contains(lines, "%") || !strings.Contains(lines, "the total is not known") {
		t.Fatal(lines)
	}
}

// Completion text comes from typed results and never overclaims.
func TestCompletionTextIsOutcomeSpecific(t *testing.T) {
	for want, data := range map[string]any{
		"Imported Essays as review (unscanned).":                                       library.ImportResult{Applied: true, Titles: []string{"Essays"}, Status: "review", Reason: "unscanned"},
		"Checks: limited checks passed; no detections reported (not proof of safety).": assessment.Report{Status: "limited_checks_passed", Antivirus: scanner.Result{Status: "no_detections_reported"}},
		"Checks: needs review; not scanned.":                                           assessment.Report{Status: "needs_review", Antivirus: scanner.Result{Status: "not_scanned"}},
		"Rescanned 0: 2 promoted, 1 with detections, 0 incomplete.":                    library.RescanResult{Applied: true, Promoted: 2, Detections: 1},
		"Library check: needs attention; 1 of 2 holdings verified.":                    library.LibraryCheck{Holdings: 2, Verified: 1},
		"Complete.": struct{}{},
	} {
		if got := summary(data); got != want {
			t.Errorf("%T: %q, want %q", data, got, want)
		}
	}
}

// A finished result raises a toast in its outcome's colour: success fades,
// failure stays until a key, and reduced motion keeps only the status line.
func TestToastsFollowTheTypedOutcome(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.ColorProfileMsg{Profile: colorprofile.ANSI})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	now := clocked(m)
	m.busy = true
	m.Update(result{id: m.id, data: library.ImportResult{Applied: true, Titles: []string{"Failed attempts, a history"}, Status: "checked"}})
	if m.toast == nil || m.toast.outcome != outcomeOK || !strings.Contains(m.footerWithToast(m.footer()), m.paint("Done: Imported Failed attempts", success, false)[:12]) {
		t.Fatal("success toast", m.toast)
	}
	if strings.Contains(m.footerWithToast(m.footer()), "\x1b[31m") {
		t.Fatal("a word in the title turned the toast red")
	}
	*now = now.Add(6 * time.Second)
	m.Update(tick{})
	if m.toast != nil {
		t.Fatal("success toast did not fade")
	}
	m.busy = true
	m.Update(result{id: m.id, err: errors.New("disk full")})
	if m.toast == nil || m.toast.outcome != outcomeFailed {
		t.Fatal("failure toast", m.toast)
	}
	*now = now.Add(time.Hour)
	m.Update(tick{})
	if m.toast == nil || !strings.Contains(m.footerWithToast(m.footer()), "any key dismisses") {
		t.Fatal("failure toast faded")
	}
	m.Update(key(tea.KeyF6))
	if m.toast != nil {
		t.Fatal("a key did not dismiss the failure toast")
	}
	m.reduceMotion = true
	m.busy = true
	m.Update(result{id: m.id, data: library.ImportResult{Applied: true, Status: "checked"}})
	if m.toast != nil {
		t.Fatal("reduced motion raised a toast")
	}
}

// Every status of the session is logged with its time; L shows the log
// from results, is text in a field, and the log is bounded.
func TestMessageLogKeepsTheSession(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Init()
	clocked(m)
	m.busy = true
	m.Update(result{id: m.id, data: library.ImportResult{Applied: true, Titles: []string{"One"}, Status: "checked"}})
	m.Update(key(tea.KeyF2))
	if len(m.messages) < 2 || m.messages[0].at.IsZero() {
		t.Fatal("messages", m.messages)
	}
	m.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	if m.logOpen || !strings.HasSuffix(m.input.Value(), "L") {
		t.Fatal("L in a field opened the log")
	}
	m.entries = []entry{{title: "x"}}
	m.Update(key(tea.KeyF6))
	m.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	view := m.View().Content
	if !m.logOpen || !strings.Contains(view, "Messages") || !strings.Contains(view, "Imported One") || !strings.Contains(view, time.Unix(1000, 0).Format("15:04:05")) {
		t.Fatalf("log:\n%s", view)
	}
	m.Update(key(tea.KeyEscape))
	if m.logOpen {
		t.Fatal("Escape did not close the log")
	}
	for i := range logLimit + 10 {
		m.status = strings.Repeat("x", i+1)
		m.afterUpdate(false, nil)
	}
	if len(m.messages) != logLimit {
		t.Fatal("log not bounded", len(m.messages))
	}
}
