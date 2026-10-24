package present

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/library"
)

func TestPortableEvidenceAndCitationKeepAllFactsWithInertText(t *testing.T) {
	metadata := library.HoldingMetadata{Schema: "holding-metadata/1", LibraryID: "library:exact", AssetID: "sha256:asset", HoldingSHA256: "holding", ByteStatus: "journal_evidence", Titles: []string{"title\x1b[2J\u202e"}, Languages: []string{"en"}, Identities: []library.Identity{{ID: "doi:10.example/test", Role: "version"}}}
	e := content.Evidence{Schema: "nemalo-evidence/1", Metadata: metadata, MetadataSHA256: "metadata-digest", Extractor: "extractor/1", RepresentationID: "sha256:representation", Reference: "reference", Quotation: "line one\nline two\x1b", Coverage: content.EvidenceCoverage{Scope: "returned_range", ReturnedUnits: []int{2}, UnsupportedUnits: []int{3}, Gaps: []content.Gap{{Reason: "omitted\x1b"}}}, Limitations: []string{"bounded\x1b"}}
	report := Content(content.Result{Status: "partial_range", Evidence: &e, Text: e.Quotation})
	marker := "Portable evidence with declared metadata, coverage and limits (JSON):\n"
	_, portable, ok := strings.Cut(report, marker)
	if !ok || strings.ContainsAny(report, "\x1b\u202e") || !strings.Contains(report, "Metadata SHA-256: metadata-digest") {
		t.Fatal("unsafe or missing evidence presentation", report)
	}
	if at := strings.Index(report, "line one\nline two"); at < 0 || at > strings.Index(report, marker) {
		t.Fatal("portable sidecar hides the actual quotation")
	}
	var got content.Evidence
	if err := json.NewDecoder(strings.NewReader(portable)).Decode(&got); err != nil || !reflect.DeepEqual(got, e) {
		t.Fatal("portable evidence changed facts", got, err)
	}
	c := app.CitationResult{Schema: "nemalo-citation/1", Status: "available", Metadata: &metadata, MetadataSHA256: "metadata-digest", CSLJSON: []app.CSLItem{{ID: "sha256:asset", Title: "title\x1b", Type: "document"}}, Fields: []app.CitationField{{Field: "title", Origin: "recorded", Status: "ambiguous", Candidates: []string{"one\x1b", "two"}}}, UnknownFields: []string{"author", "issued"}, Limitations: []string{"declarations only"}}
	report = Citation(c)
	_, portable, ok = strings.Cut(report, "Portable citation with provenance and uncertainty (JSON):\n")
	if !ok || strings.ContainsAny(report, "\x1b\u202e") || !strings.Contains(report, "Unknown fields: author, issued") {
		t.Fatal("unsafe or missing citation sidecar", report)
	}
	var citation app.CitationResult
	if err := json.Unmarshal([]byte(portable), &citation); err != nil || !reflect.DeepEqual(citation, c) {
		t.Fatal("portable citation changed facts", citation, err)
	}
}
