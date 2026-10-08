package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/library"
)

func browseFixture() discovery.Page {
	p := discovery.Page{Source: "archive", Query: "stories", Total: 12}
	for i := range 10 {
		p.Results = append(p.Results, discovery.Result{Title: fmt.Sprintf("Story %d 日本語", i), ID: fmt.Sprintf("archive:story%d", i), Authors: []string{"Author"}, Languages: []string{"ja"}, Access: "discovery_only"})
	}
	return p
}

func TestBrowserFocusSelectionAndEvidence(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(key(tea.KeyF5))
	if m.evidence {
		t.Fatal("empty view became a report")
	}
	m.input.SetValue("original query")
	m.Update(result{id: m.id, data: browseFixture()})
	full := m.viewport.GetContent()
	m.Update(key(tea.KeyF5))
	m.Update(key(tea.KeyEscape))
	if m.evidence || m.status != "Complete." {
		t.Fatal("report back action changed operation status")
	}
	m.Update(key(tea.KeyDown))
	if m.selected != 0 {
		t.Fatal("form arrows moved result selection")
	}
	m.Update(key(tea.KeyF6))
	if !m.resultsFocused || m.input.Focused() {
		t.Fatal("result focus not exclusive")
	}
	m.Update(key(tea.KeyDown))
	if m.selected != 1 || !strings.Contains(m.preview.GetContent(), "archive:story1") {
		t.Fatal("selection and preview diverged")
	}
	m.Update(key(tea.KeyEnd))
	m.Update(key(tea.KeyDown))
	if m.selected != 9 {
		t.Fatal("selection exceeded page")
	}
	m.Update(key(tea.KeyHome))
	m.Update(key(tea.KeyUp))
	if m.selected != 0 {
		t.Fatal("selection went negative")
	}
	m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if m.input.Value() != "original query" {
		t.Fatal("result keystrokes edited query")
	}
	_, cmd := m.Update(key(tea.KeyEnter))
	if cmd != nil || !m.details || m.busy {
		t.Fatal("details submitted a request")
	}
	m.Update(key(tea.KeyPgDown))
	m.Update(key(tea.KeyEscape))
	if m.details || !m.resultsFocused {
		t.Fatal("details back navigation lost results focus")
	}
	m.Update(key(tea.KeyF5))
	if !m.evidence || m.viewport.GetContent() != full {
		t.Fatal("full evidence lost")
	}
	m.Update(key(tea.KeyPgDown))
	m.Update(key(tea.KeyPgUp))
	m.Update(key(tea.KeyEscape))
	m.Update(key(tea.KeyEscape))
	if m.resultsFocused || !m.input.Focused() {
		t.Fatal("back did not restore editing")
	}
	m.Update(key(tea.KeyF6))
	m.Update(key(tea.KeyF6))
	if m.resultsFocused {
		t.Fatal("focus toggle stuck")
	}
}

func TestBrowserReturnsToSelectedFormField(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.switchMode("holdings")
	m.Update(key(tea.KeyF6))
	m.Update(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	if m.resultsFocused || !m.secondary.Focused() || m.input.Focused() {
		t.Fatal("field shortcut did not restore exclusive input focus")
	}
	m.Update(key(tea.KeyF6))
	m.Update(key(tea.KeyF6))
	if !m.secondary.Focused() || m.input.Focused() {
		t.Fatal("focus return lost the active second field")
	}
}

func TestBrowserStagesEvaluationWithoutNetworkAndRestoresSelection(t *testing.T) {
	m := newModel(context.Background(), app.Service{}) // No providers available.
	m.Update(result{id: m.id, data: browseFixture()})
	m.Update(key(tea.KeyF6))
	m.Update(key(tea.KeyDown))
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	if m.mode != "evaluate" || m.input.Value() != "archive:story1" || m.busy {
		t.Fatal("evaluation was not staged safely")
	}
	if cmd != nil {
		if _, request := cmd().(result); request {
			t.Fatal("staging requested metadata")
		}
	}
	m.switchMode("search")
	if m.selected != 1 || len(m.entries) != 10 || !strings.Contains(m.preview.GetContent(), "archive:story1") {
		t.Fatal("browser context lost across modes")
	}
	m.entries[1].sourceID = "openlibrary:OL1W"
	m.Update(key(tea.KeyF6))
	m.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
	if m.mode != "search" {
		t.Fatal("non-Archive result prepared Archive lookup")
	}
	m.busy = true
	m.Update(key(tea.KeyF5))
	m.Update(key(tea.KeyF6))
	if m.evidence || !m.resultsFocused {
		t.Fatal("busy browser changed")
	}
}

func TestBrowserEscapingEvidenceAndResponsiveLayouts(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	p := browseFixture()
	p.Results[0].Title = "unsafe\x1b[31m\n" + strings.Repeat("日本語", 100)
	m.Update(result{id: m.id, data: p})
	if strings.Contains(m.entries[0].title, "\x1b") || strings.Contains(m.entries[0].detail, "\x1b") {
		t.Fatal("untrusted terminal escape rendered")
	}
	for _, size := range [][2]int{{40, 20}, {41, 20}, {80, 24}, {100, 30}, {101, 30}, {112, 38}, {160, 45}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, focus := range []bool{false, true} {
			m.resultsFocused = focus
			for _, state := range []string{"list", "detail", "report"} {
				m.details, m.evidence = state == "detail", state == "report"
				m.resize()
				v := m.View().Content
				if lipgloss.Width(v) > size[0] || lipgloss.Height(v) > size[1] {
					t.Fatalf("%s overflow at %v: %dx%d", state, size, lipgloss.Width(v), lipgloss.Height(v))
				}
			}
		}
	}
	m.populate(library.Page{Assets: []library.Asset{{ID: "hash", Bytes: 10, Locations: []library.Location{{Path: "bad\x1b.epub", CandidateKind: "epub"}, {Path: "copy.epub", CandidateKind: "epub"}}}}})
	if !strings.Contains(m.entries[0].detail, "No recorded assessment") || !strings.Contains(m.entries[0].detail, "copy.epub") || strings.Contains(m.entries[0].detail, "\x1b") {
		t.Fatal("unassessed identity or all locations lost")
	}
	m.populate(library.Page{Assets: []library.Asset{{ID: "hash", Bytes: 1 << 20, Health: &assessment.Report{Status: "limited_checks_passed"}}}})
	if !strings.Contains(m.entries[0].detail, "historical") || !strings.Contains(m.entries[0].detail, "limited_checks_passed") {
		t.Fatal("historical assessment hidden")
	}
	m.populate(nil)
	m.selectEntry(100)
	if len(m.entries) != 0 || m.preview.GetContent() != "" {
		t.Fatal("stale entries retained")
	}
	raw := strings.Repeat("日本語", 300) + " END-OF-EVIDENCE"
	m.setReport(raw)
	for _, width := range []int{41, 101, 80} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		if m.report != raw {
			t.Fatal("resize changed original evidence")
		}
		m.viewport.GotoBottom()
		if !strings.Contains(m.viewport.View(), "END-OF-EVIDENCE") {
			t.Fatal("wrapped evidence end unreachable")
		}
	}
	m.Update(result{id: m.id, data: browseFixture(), err: fmt.Errorf("partial source failure")})
	if len(m.entries) != 0 || !m.evidence || !strings.Contains(m.report, "partial source failure") {
		t.Fatal("partial results hid operation failure")
	}
	for _, tc := range []struct {
		n    int64
		want string
	}{{10, "10 B"}, {1024, "1.0 KiB"}, {1 << 20, "1.0 MiB"}} {
		if byteSize(tc.n) != tc.want {
			t.Fatal("byte units", tc)
		}
	}
}
