package tui

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/jobs"
	"github.com/blisspixel/nemalo/internal/library"
)

func terminalJobFixture(t *testing.T) (string, string, string) {
	t.Helper()
	root, dir := t.TempDir(), t.TempDir()
	state, err := library.Initialize(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := library.EnableBookmarks(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if _, err := library.DisableBookmarks(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	type record struct {
		SchemaVersion int            `json:"schema_version"`
		Event         string         `json:"event"`
		At            string         `json:"at"`
		Kind          string         `json:"kind,omitempty"`
		Request       any            `json:"request,omitempty"`
		Context       any            `json:"context,omitempty"`
		Progress      *jobs.Progress `json:"progress,omitempty"`
	}
	var data []byte
	for _, r := range []record{
		{SchemaVersion: 1, Event: "queued", At: "2026-01-01T00:00:00Z", Kind: "library_rescan", Request: map[string]string{"library": root, "private": "hidden request"}, Context: app.JobLibraryContext{Schema: "nemalo-job-library/1", LibraryID: state.LibraryID}},
		{SchemaVersion: 1, Event: "running", At: "2026-01-01T00:00:00Z"},
		{SchemaVersion: 1, Event: "progress", At: "2026-01-01T00:00:00Z", Progress: &jobs.Progress{Phase: "scan\x1b[31m", Done: 1, Total: 2, Unit: "files"}},
	} {
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		data = append(append(data, b...), '\n')
	}
	data = append(data, []byte(`{"torn`)...)
	id := strings.Repeat("a", 32)
	for suffix, b := range map[string][]byte{".jsonl": data, ".lock": []byte("retained"), ".cancel": []byte("retained")} {
		if err := os.WriteFile(filepath.Join(dir, id+suffix), b, 0400); err != nil {
			t.Fatal(err)
		}
	}
	return root, dir, "job:" + id
}

func terminalJobTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	if err := filepath.WalkDir(root, func(name string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		if e.IsDir() {
			out[name] = info.Mode().String()
			return nil
		}
		b, err := os.ReadFile(name)
		out[name] = info.Mode().String() + string(b)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTerminalJobsExplicitReadPreservesJournalsAndDisabledBookmarks(t *testing.T) {
	root, dir, id := terminalJobFixture(t)
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	before, libraryBefore := terminalJobTree(t, dir), terminalJobTree(t, root)
	m := newModel(context.Background(), app.Service{JobsDirectory: dir})
	m.switchMode("state")
	m.input.SetValue(root)
	m.secondary.SetValue("retained intake selection")
	if m.busy || m.report != "" {
		t.Fatal("navigation observed jobs automatically")
	}
	for range 3 {
		_, cmd := m.Update(key(tea.KeyF10))
		if cmd == nil || !m.busy {
			t.Fatal("explicit jobs observation unavailable")
		}
		if _, extra := m.Update(key(tea.KeyF10)); extra != nil {
			t.Fatal("overlapping jobs observation dispatched")
		}
		msg := cmd()
		m.Update(msg)
		if m.busy || len(m.entries) != 1 || m.entries[0].title != shortID(id) || m.entries[0].identity != id || m.outcome != outcomeAttention || !strings.Contains(m.report, "Job observation: incomplete") || !strings.Contains(m.status, "Incomplete recorded job observation") {
			t.Fatal(m.status, m.report, m.entries)
		}
		if strings.Contains(m.report, "hidden request") || strings.ContainsRune(m.report, '\x1b') || strings.ContainsRune(m.entries[0].detail, '\x1b') || !strings.Contains(m.entries[0].detail, "not observed") {
			t.Fatal("raw metadata or live-owner claim", m.report, m.entries)
		}
		m.Update(key(tea.KeyF6))
		m.Update(key(tea.KeyEnter))
		m.Update(key(tea.KeyPgDown))
		m.Update(key(tea.KeyEscape))
		m.Update(key(tea.KeyF5))
		m.Update(key(tea.KeyEscape))
		if m.secondary.Value() != "retained intake selection" {
			t.Fatal("jobs changed state source selection")
		}
	}
	if !reflect.DeepEqual(before, terminalJobTree(t, dir)) || !reflect.DeepEqual(libraryBefore, terminalJobTree(t, root)) {
		t.Fatal("terminal observation mutated job or library state")
	}
	for _, size := range [][2]int{{40, 24}, {80, 24}, {116, 36}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		v := m.View().Content
		if lipgloss.Width(v) > size[0] || lipgloss.Height(v) > size[1] {
			t.Fatal("job layout overflow", size, v)
		}
	}
}

func TestTerminalJobsCancellationAndLateResultsCannotReplaceNewSelection(t *testing.T) {
	root, dir, id := terminalJobFixture(t)
	m := newModel(context.Background(), app.Service{JobsDirectory: dir})
	m.switchMode("state")
	m.input.SetValue(root)
	_, old := m.Update(key(tea.KeyF10))
	oldID := m.id
	if old == nil {
		t.Fatal("missing first observation")
	}
	m.Update(key(tea.KeyEscape))
	if m.busy || m.id == oldID {
		t.Fatal("cancel did not invalidate generation")
	}
	other := t.TempDir()
	if _, err := library.Initialize(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	m.input.SetValue(other)
	_, current := m.Update(key(tea.KeyF10))
	if current == nil {
		t.Fatal("new selection unavailable")
	}
	m.Update(current())
	accepted := m.report
	m.Update(old())
	if m.busy || m.report != accepted || strings.Contains(m.report, id) || len(m.entries) != 0 || m.input.Value() != other {
		t.Fatal("late result adopted across library selection", m.report)
	}
}

func TestTerminalJobsMissingStateReviewAndDeadlineHaveNoWrites(t *testing.T) {
	root := t.TempDir()
	if _, err := library.Initialize(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing", "jobs")
	m := newModel(context.Background(), app.Service{JobsDirectory: missing})
	if _, cmd := m.Update(key(tea.KeyF10)); cmd != nil {
		t.Fatal("jobs ran outside State")
	}
	m.switchMode("state")
	if _, cmd := m.Update(key(tea.KeyF10)); cmd != nil {
		t.Fatal("empty scope observed jobs")
	}
	m.input.SetValue(root)
	m.Update(key(tea.KeyF7))
	if _, cmd := m.Update(key(tea.KeyF10)); cmd != nil || m.confirmRoot != root {
		t.Fatal("jobs escaped write review")
	}
	m.Update(key(tea.KeyEscape))
	_, cmd := m.Update(key(tea.KeyF10))
	if cmd == nil {
		t.Fatal("missing-store observation not dispatched")
	}
	m.Update(cmd())
	if m.outcome != outcomeNone || !strings.Contains(m.report, "Jobs: 0") {
		t.Fatal(m.report, m.outcome)
	}
	if _, err := os.Stat(filepath.Dir(missing)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("jobs created missing state", err)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	m = newModel(ctx, app.Service{JobsDirectory: missing})
	m.switchMode("state")
	m.input.SetValue(root)
	_, cmd = m.Update(key(tea.KeyF10))
	m.Update(cmd())
	if m.outcome != outcomeFailed || len(m.entries) != 0 || !strings.Contains(m.report, "deadline exceeded") {
		t.Fatal("deadline did not discard selected results", m.report)
	}
}
