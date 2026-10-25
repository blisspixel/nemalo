package tui

import (
	"context"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/discovery"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPMCReviewUsesResolvedVersion(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.mode = "evaluate"
	m.submittedInput = "pmc:PMC13901"
	m.lastEvaluation = &discovery.Evaluation{ID: "pmc:PMC13901.2", Source: "pmc"}
	m.entries = []entry{{sourceID: "PMC13901.2.pdf"}}
	m.secondary.SetValue(filepath.Join(t.TempDir(), "packet"))
	m.input.SetValue(m.submittedInput)
	m.requestDownload()
	if m.downloadReview == nil || m.downloadReview.SourceID != "pmc:PMC13901.2" || !strings.Contains(m.report, "NLM") || !strings.Contains(m.report, discovery.PMCHost) {
		t.Fatal(m.status, m.report)
	}
}

func TestPMCInterruptedPacketDoesNotOfferResume(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.mode = "evaluate"
	m.submittedInput = "pmc:PMC13901.2"
	m.input.SetValue(m.submittedInput)
	m.entries = []entry{{sourceID: "PMC13901.2.pdf"}}
	dir := t.TempDir()
	for _, name := range []string{"intent.json", "transfer.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m.secondary.SetValue(dir)
	m.requestDownload()
	if m.downloadReview != nil || !strings.Contains(m.status, "cannot resume") {
		t.Fatal(m.status, m.downloadReview)
	}
	for _, name := range []string{"intent.json", "transfer.json"} {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err != nil || string(b) != "{}" {
			t.Fatal("retained packet changed", err)
		}
	}
}
