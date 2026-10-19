package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/acquisition"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/scanner"
	"github.com/charmbracelet/colorprofile"
)

func TestOutcomeComesFromTheResultNotItsWords(t *testing.T) {
	for _, tc := range []struct {
		name string
		data any
		err  error
		want outcome
	}{
		{"refused request", nil, errors.New("request to archive.org failed"), outcomeFailed},
		{"detection", acquisition.Result{Status: "detected_quarantined", Complete: true}, nil, outcomeFailed},
		{"acquired", acquisition.Result{Status: "untrusted_intake", Complete: true}, nil, outcomeOK},
		{"imported", library.ImportResult{Applied: true, Status: "review"}, nil, outcomeOK},
		{"preview", library.ImportResult{}, nil, outcomeNone},
		{"ingest with a failure", library.IngestResult{Applied: true, Complete: true, Failed: 1}, nil, outcomeFailed},
		{"rescan detection", library.RescanResult{Applied: true, Detections: 1}, nil, outcomeFailed},
		{"library needs attention", library.LibraryCheck{}, nil, outcomeAttention},
		{"healthy library", library.LibraryCheck{Healthy: true}, nil, outcomeOK},
		{"scan found", assessment.Report{Status: "review", Antivirus: scanner.Result{Status: "findings"}}, nil, outcomeFailed},
		{"checks passed", assessment.Report{Status: "limited_checks_passed"}, nil, outcomeOK},
		{"listing", library.ManagedPage{}, nil, outcomeNone},
	} {
		if got := resultOutcome(tc.data, tc.err); got != tc.want {
			t.Fatalf("%s: outcome %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestStatusColourFollowsTheOutcome(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Update(tea.ColorProfileMsg{Profile: colorprofile.TrueColor})
	status := func() string {
		for _, line := range strings.Split(m.View().Content, "\n") {
			if strings.Contains(line, m.status[:min(len(m.status), 10)]) {
				return line
			}
		}
		t.Fatal("status line not shown", m.status)
		return ""
	}
	if line := status(); !strings.Contains(line, "\x1b[34m") {
		t.Fatalf("Ready is not in the accent: %q", line)
	}
	// Wording alone never colours the line.
	m.status = "Checked 3 files, 0 errors."
	if line := status(); strings.Contains(line, "\x1b[31m") {
		t.Fatal("a status turned red by its words")
	}
	m.id++
	m.Update(result{id: m.id, data: library.ImportResult{Applied: true, Library: "/l", Source: "/s"}})
	if line := status(); !strings.Contains(line, "\x1b[32m") {
		t.Fatalf("import not green: %q", line)
	}
	m.id++
	m.Update(result{id: m.id, data: acquisition.Result{Status: "detected_quarantined"}, err: acquisition.ErrDetected})
	if line := status(); !strings.Contains(line, "\x1b[31m") {
		t.Fatalf("detection not red: %q", line)
	}
	// A key press clears the outcome.
	m.Update(tea.KeyPressMsg{Code: tea.KeyF5})
	if m.outcome != outcomeNone {
		t.Fatal("outcome kept after a key press")
	}
}
