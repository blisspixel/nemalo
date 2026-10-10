package present

import (
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/library"
)

func TestLibraryEvidenceAndTerminalEscaping(t *testing.T) {
	a := library.Asset{ID: "hash\x1b", Bytes: 10, Locations: []library.Location{{Path: "book\x00.epub", CandidateKind: "kind\x1b"}, {Path: "copy.epub"}}, Health: &assessment.Report{Status: "needs_review\x1b", Checks: assessment.Checks{EPUB: &assessment.EPUBFacts{Titles: []string{"Title\x1b"}, Languages: []string{"ja\x00"}}}}}
	s := Snapshot(library.Snapshot{Catalog: library.Catalog{ID: "snapshot\x1b", RootHint: "root\x00", Assets: []library.Asset{a}}, Output: "output\x1b", Durability: "unsaved\x00"})
	p := Holdings(library.Page{SnapshotID: "snapshot\x1b", Assets: []library.Asset{a}})
	u := Audit(library.Audit{Root: "root\x1b", Findings: []library.Finding{{Path: "path\x1b", Status: "status\x00", Detail: "detail\x1b"}}})
	for _, text := range []string{s, p, u} {
		if strings.ContainsAny(text, "\x1b\x00") {
			t.Fatal("terminal controls emitted", text)
		}
	}
	if !strings.Contains(s, "Unique byte assets: 1") || !strings.Contains(s, "File locations: 2") || !strings.Contains(p, "historical") || !strings.Contains(p, "Title:") || !strings.Contains(p, "Language:") || !strings.Contains(u, "not_scanned") {
		t.Fatal(s, p, u)
	}
	imported := Import(library.ImportResult{Library: "lib", Source: "book", IdentitiesRecorded: true, IdentifiersOmitted: 1, Identities: []library.Identity{{Role: "edition", ID: "isbn:9780306406157"}}})
	if !strings.Contains(imported, `Identity: "edition" "isbn:9780306406157"`) || !strings.Contains(imported, "Package identifiers recorded: 1") || !strings.Contains(imported, "Identifiers omitted: 1") {
		t.Fatal(imported)
	}
}
