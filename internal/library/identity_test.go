package library

import (
	"testing"

	"github.com/blisspixel/nemalo/internal/assessment"
)

func TestClassifyPackageIdentifierEvidence(t *testing.T) {
	isbn, ok := classifyPackageIdentifier(assessment.EPUBIdentifier{Value: "978-0-306-40615-7", Scheme: "ISBN", PackageUnique: true})
	if !ok || isbn.Role != "edition" || isbn.ID != "isbn:9780306406157" || isbn.Confidence != confidenceStated || isbn.Origin != originEPUBIdentifier || !isbn.PackageUnique {
		t.Fatal(isbn, ok)
	}
	isbn10, ok := classifyPackageIdentifier(assessment.EPUBIdentifier{Value: "urn:isbn:0-306-40615-2"})
	if !ok || isbn10.Role != "edition" || isbn10.ID != "isbn:0306406152" || isbn10.Scheme != "" {
		t.Fatal(isbn10, ok)
	}
	doi, ok := classifyPackageIdentifier(assessment.EPUBIdentifier{Value: "https://doi.org/10.1000/182", Scheme: "URL"})
	if !ok || doi.Role != "publication_version" || doi.ID != "doi:10.1000/182" {
		t.Fatal(doi, ok)
	}
	bare, ok := classifyPackageIdentifier(assessment.EPUBIdentifier{Value: "10.1000/182", Scheme: "DOI"})
	if !ok || bare.ID != doi.ID {
		t.Fatal(bare, ok)
	}
	work, ok := classifyPackageIdentifier(assessment.EPUBIdentifier{Value: "https://openlibrary.org/works/OL1W/"})
	if !ok || work.Role != "work" || work.ID != "openlibrary_work:OL1W" {
		t.Fatal(work, ok)
	}
	edition, ok := classifyPackageIdentifier(assessment.EPUBIdentifier{Value: "https://openlibrary.org/books/OL1M", Scheme: "OpenLibrary"})
	if !ok || edition.Role != "edition" || edition.ID != "openlibrary_edition:OL1M" {
		t.Fatal(edition, ok)
	}
	bad, ok := classifyPackageIdentifier(assessment.EPUBIdentifier{Value: "978-0-306-40615-8", Scheme: "ISBN"})
	if !ok || bad.Role != "unassigned" || bad.Confidence != confidencePreserved || bad.ID == isbn.ID {
		t.Fatal(bad, ok)
	}
	for _, value := range []string{"urn:uuid:123e4567-e89b-12d3-a456-426614174000", "http://openlibrary.org/works/OL1W", "archive:demo"} {
		got, ok := classifyPackageIdentifier(assessment.EPUBIdentifier{Value: value})
		if !ok || got.Role != "unassigned" {
			t.Fatal(value, got, ok)
		}
	}
	if _, ok := classifyPackageIdentifier(assessment.EPUBIdentifier{Value: "97803\n06406157"}); ok {
		t.Fatal("control character accepted")
	}
	report := assessment.Report{DetectedFormat: "epub", Checks: assessment.Checks{EPUB: &assessment.EPUBFacts{Identifiers: []assessment.EPUBIdentifier{
		{Value: "9780306406157", Scheme: "ISBN"},
		{Value: "978-0-306-40615-7", Scheme: "isbn", PackageUnique: true},
		{Value: "https://openlibrary.org/works/OL1W"},
	}}}}
	list, omitted, recorded := identitiesFrom(report)
	if !recorded || omitted != 0 || len(list) != 2 || !list[0].PackageUnique || list[0].Role != "edition" || list[1].Role != "work" {
		t.Fatal(list, omitted, recorded)
	}
	pdf, omitted, recorded := identitiesFrom(assessment.Report{DetectedFormat: "pdf"})
	if pdf != nil || omitted != 0 || recorded {
		t.Fatal(pdf, omitted, recorded)
	}
	if validIdentities([]Identity{{Role: "recording", ID: "track:1", Raw: "1", Origin: originEPUBIdentifier, Confidence: confidenceStated}}, true, 0) {
		t.Fatal("recording role accepted")
	}
}
