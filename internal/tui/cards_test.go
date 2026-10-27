package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/scanner"
	"github.com/charmbracelet/colorprofile"
)

const cardHash = "sha256:627526d9cfa73d7aa2351efe2e30cb3a1cbd7fd7ada98c5c4e2d9cddf851a834"

func cardPage() library.Page {
	return library.Page{Total: 1, Assets: []library.Asset{{
		ID: cardHash, SHA256: strings.TrimPrefix(cardHash, "sha256:"), Bytes: 1373452,
		Locations: []library.Location{{Path: "book-1.epub", CandidateKind: "ebook_candidate"}, {Path: "book-4.epub", CandidateKind: "ebook_candidate"}},
		Health:    &assessment.Report{File: "book-1.epub", Bytes: 1373452, SHA256: strings.TrimPrefix(cardHash, "sha256:"), Status: "limited_checks_passed", Antivirus: scanner.Result{Status: "not_scanned"}, Limitations: []string{"Limited checks do not establish safety."}, Checks: assessment.Checks{EPUB: &assessment.EPUBFacts{Titles: []string{"Essays"}, Languages: []string{"en"}}}},
	}}}
}

// Each fact appears once in a card, the identity is short, and the full
// report keeps the full identity and every recorded limitation.
func TestCardsShowEachFactOnceAndKeepTheFullReport(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.switchMode("holdings")
	m.id++
	m.Update(result{id: m.id, data: cardPage()})
	card := m.entries[0].detail
	if strings.Contains(card, cardHash) || !strings.Contains(card, "sha256:627526d9cfa7...a834") {
		t.Fatalf("identity not shortened:\n%s", card)
	}
	for _, once := range []string{"1373452", "book-1.epub", "Limited checks passed", "Not scanned", "627526d9cfa7"} {
		if n := strings.Count(card, once); n != 1 {
			t.Fatalf("%q shown %d times:\n%s", once, n, card)
		}
	}
	if !strings.Contains(m.report, cardHash) || !strings.Contains(m.report, "Limited checks do not establish safety.") {
		t.Fatalf("full report lost a recorded field:\n%s", m.report)
	}
	// At 80 columns the card wraps only between words.
	m.Update(key(tea.KeyF6))
	m.Update(key(tea.KeyEnter))
	for _, line := range strings.Split(m.preview.View(), "\n") {
		if lipgloss.Width(line) > m.preview.Width() {
			t.Fatalf("card line wider than its pane: %q", line)
		}
	}
	if !strings.Contains(m.View().Content, "sha256:627526d9cfa7...a834") {
		t.Fatal("identity split across lines at 80 columns")
	}
}

// y copies the whole identity through the terminal clipboard request and
// says that a terminal may ignore it; an item without one says so.
func TestCopyIdentityReportsWhatHappened(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.switchMode("holdings")
	m.id++
	m.Update(result{id: m.id, data: cardPage()})
	m.Update(key(tea.KeyF6))
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd == nil || !strings.Contains(m.status, "OSC 52") || !strings.Contains(m.status, "ignores it") {
		t.Fatal("copy", m.status)
	}
	if got := cmd(); got == nil {
		t.Fatal("no clipboard request")
	} else if s, ok := got.(interface{ String() string }); ok && s.String() != cardHash {
		t.Fatal("copied", s.String())
	}
	m.entries[0].identity = ""
	if _, cmd = m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"}); cmd != nil || !strings.Contains(m.status, "no identity to copy") || m.outcome != outcomeAttention {
		t.Fatal("missing identity was not reported", m.status)
	}
	// Typed in a field, y is text.
	m.Update(key(tea.KeyF6))
	m.Init()
	m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if !strings.HasSuffix(m.input.Value(), "y") {
		t.Fatal("y was taken from a field")
	}
}

// Chips come from typed status values, never from words in untrusted text,
// and a missing or partial scan is never shown as clean.
func TestChipsComeFromTypedStatus(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.ColorProfileMsg{Profile: colorprofile.ANSI})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	page := cardPage()
	page.Assets[0].Locations[0].Path = "Limited checks passed No detections reported.epub"
	page.Assets[0].Health = nil
	m.populate(page)
	card := m.entries[0].detail
	if !strings.Contains(card, m.paint("Not assessed", muted, false)) || strings.Contains(card, m.paint("Limited checks passed", success, true)) {
		t.Fatalf("chip followed words:\n%q", card)
	}
	for status, want := range map[string]chip{"no_detections_reported": {"No detections reported", success}, "not_scanned": {"Not scanned", muted}, "findings": {"Detection", danger}, "incomplete": {"Scan incomplete", warning}, "": {"Not scanned", muted}} {
		if got := antivirusChip(status); got != want {
			t.Fatalf("%q: %v", status, got)
		}
	}
	if got := holdingChip("review", "unscanned"); got.role != warning || got.text != "Review: unscanned" {
		t.Fatal(got)
	}
	if jobChip("running").role != warning || jobChip("succeeded").role != success || jobChip("failed").role != danger {
		t.Fatal("job chips")
	}
}
