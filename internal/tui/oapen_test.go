package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/discovery"
)

func TestOAPENReviewKeepsTheHandleAndRefusesResume(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.mode = "evaluate"
	m.submittedInput = "oapen:20.500.12657/25287"
	m.lastEvaluation = &discovery.Evaluation{ID: m.submittedInput, Source: "oapen"}
	m.entries = []entry{{sourceID: "9789085551201_OA_version.pdf"}}
	m.secondary.SetValue(filepath.Join(t.TempDir(), "packet"))
	m.input.SetValue(m.submittedInput)
	m.requestDownload()
	if m.downloadReview == nil || m.downloadReview.SourceID != m.submittedInput || m.downloadReview.Resume || !strings.Contains(m.report, discovery.OAPENHost) || !strings.Contains(m.report, "ORIGINAL") || !strings.Contains(m.report, "cannot be resumed") {
		t.Fatal(m.status, m.report, m.downloadReview)
	}
	dir := t.TempDir()
	for _, name := range []string{"intent.json", "transfer.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m.secondary.SetValue(dir)
	m.downloadReview = nil
	m.requestDownload()
	if m.downloadReview != nil || !strings.Contains(m.status, "cannot resume") {
		t.Fatal(m.status, m.downloadReview)
	}
	for _, name := range []string{"intent.json", "transfer.json"} {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err != nil || string(b) != "{}" {
			t.Fatal("retained packet changed", err)
		}
	}
	m.mode = "evaluate"
	if !strings.Contains(m.emptyHelp(), "oapen:20.500.12657/N") || !strings.Contains(m.emptyHelp(), "ORIGINAL files only") {
		t.Fatal(m.emptyHelp())
	}
	m.formFields()
	if !strings.Contains(m.inputHint, "oapen:20.500.12657/N") {
		t.Fatal(m.inputHint)
	}
	m.mode = "search"
	if strings.Contains(m.emptyHelp(), "oapen") {
		t.Fatal(m.emptyHelp())
	}
}
