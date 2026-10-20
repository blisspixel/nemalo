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
	if !strings.Contains(s, "Unique byte assets: 1") || !strings.Contains(s, "File locations: 2") || !strings.Contains(p, "historical") || !strings.Contains(p, "Title:") || !strings.Contains(p, "Language:") || !strings.Contains(u, "not scanned") {
		t.Fatal(s, p, u)
	}
	imported := Import(library.ImportResult{Library: "lib", Source: "book", IdentitiesRecorded: true, IdentifiersOmitted: 1, Identities: []library.Identity{{Role: "edition", ID: "isbn:9780306406157"}}})
	if !strings.Contains(imported, "Identifier: isbn:9780306406157 (edition)") || !strings.Contains(imported, "Package identifiers recorded: 1") || !strings.Contains(imported, "Identifiers omitted: 1") {
		t.Fatal(imported)
	}
	if Cleanup(library.CleanupResult{Library: "lib", Folder: "folder", Limitations: []string{"kept"}}) != "" {
		t.Fatal("a cleanup that failed before planning rendered a preview")
	}
	planned := Cleanup(library.CleanupResult{Library: "lib", Folder: "folder", Digest: strings.Repeat("ab", 32)})
	if !strings.Contains(planned, "Nothing here is held") || !strings.Contains(planned, "Preview only") {
		t.Fatal(planned)
	}
}

func TestRecordingByteIdentityPresentationEscapesSourceLabels(t *testing.T) {
	r := library.RecordingsResult{ByteIdentities: []library.RecordingByteIdentity{
		{ID: "recording-sha256:single", Recordings: []library.RecordingRef{{SourceID: "archive:single"}}},
		{ID: "recording-sha256:shared", Recordings: []library.RecordingRef{
			{SourceID: "archive:first\x1b", Format: "MP3\nforged line", Kind: "tracks"},
			{SourceID: "archive:mirror", Format: "MP3", Kind: "tracks"},
		}},
	}}
	text := Recordings(r)
	if strings.Contains(text, "\x1b") || strings.Contains(text, "\nforged line") || strings.Contains(text, "recording-sha256:single") || !strings.Contains(text, "Same ordered track bytes: recording-sha256:shared (2 recording declarations)") || !strings.Contains(text, "archive:mirror MP3 (tracks)") {
		t.Fatal(text)
	}
}

func TestCollectionDiffPresentationKeepsChangesSeparateAndEscapesText(t *testing.T) {
	a := library.CollectionItem{Source: "archive:a", File: "a.epub", Note: "new\x1b"}
	b := library.CollectionItem{Source: "archive:b", Recording: "MP3\nforged"}
	r := library.CollectionDiffResult{Complete: true, Changed: true, NameChanged: true,
		Before: library.CollectionRevision{Name: "Old\x1b", Path: "before", SHA256: "old-hash", Items: 2},
		After:  library.CollectionRevision{Name: "New", Path: "after", SHA256: "new-hash", Items: 2},
		Added:  []library.CollectionPosition{{Position: 2, Item: b}}, Removed: []library.CollectionPosition{{Position: 2, Item: b}},
		NotesChanged:  []library.CollectionNoteChange{{Item: a, BeforeNote: "old\x00", BeforePosition: 1, AfterPosition: 2}},
		RetainedOrder: []library.CollectionOrderEntry{{Item: a, BeforePosition: 1, AfterPosition: 2}},
	}
	text := CollectionDiff(r)
	for _, want := range []string{"old-hash", "new-hash", "Name changed:", "Added (after 2):", "Removed (before 2):", "Note changed: archive:a", "Retained selection order changed", "1 -> 2: archive:a"} {
		if !strings.Contains(text, want) {
			t.Fatal("missing diff fact", want, text)
		}
	}
	if strings.ContainsAny(text, "\x1b\x00") || strings.Contains(text, "\nforged") || CollectionDiff(library.CollectionDiffResult{}) != "" {
		t.Fatal("untrusted controls or incomplete evidence rendered", text)
	}
	if text := CollectionDiff(library.CollectionDiffResult{Complete: true}); !strings.Contains(text, "No declaration changes.") {
		t.Fatal(text)
	}
}
