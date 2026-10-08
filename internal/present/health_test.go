package present

import (
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/scanner"
)

func TestHealthEscapesAndMeasurements(t *testing.T) {
	r := assessment.Report{File: "bad\x1b[31m.epub", Status: "needs_review", Checks: assessment.Checks{EPUB: &assessment.EPUBFacts{Titles: []string{"title\x1b"}, Languages: []string{"ja"}, TextCharacters: 100, ReadingOrderDocuments: 2}}, Antivirus: scanner.Result{Status: "findings", Output: "danger\x1b", Limitations: []string{"limited"}}, Findings: []string{"review\x1b"}, Limitations: []string{"no guarantees"}}
	r.Antivirus.Error = "error\x1b"
	r.Antivirus.OutputTruncated = true
	out := Health(r)
	if strings.Contains(out, "\x1b") || !strings.Contains(out, "100 non-whitespace") || !strings.Contains(out, "Pages: unknown") || !strings.Contains(out, "Antivirus: findings") || !strings.Contains(out, "limited") {
		t.Fatal(out)
	}
}
