package present

import (
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/acquisition"
	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/library"
)

func TestWordsListsAndFields(t *testing.T) {
	for in, want := range map[string]string{
		"not_resolved_at_file_level": "not resolved at file level",
		"file_synced; directory entry power-loss durability depends on filesystem": "file synced; directory entry power-loss durability depends on filesystem",
		"The record provides no checksum.":                                         "The record provides no checksum.",
		"epub-text/2":                                                              "epub-text/2",
		"/books/my_file.epub changed":                                              "/books/my_file.epub changed",
		"":                                                                         "",
	} {
		if got := words(in); got != want {
			t.Fatalf("words(%q) = %q, want %q", in, got, want)
		}
	}
	if got := list([]string{"Austen, Jane, 1775-1817", "", "Smith\nAda"}); got != "Austen, Jane, 1775-1817; Smith Ada" {
		t.Fatal(got)
	}
	if statusWithReason("review", "unscanned") != "review (unscanned)" || statusWithReason("checked", "") != "checked" || statusWithReason("checked", "checked") != "checked" {
		t.Fatal("status with reason")
	}
}

// Human output leaves out empty values, shows status tokens as words, and
// never lets a source value start a line of its own (issue 29).
func TestHumanOutputLeavesOutEmptyValuesAndReadsAsWords(t *testing.T) {
	n := int64(50)
	e := Evaluation(discovery.Evaluation{ID: "gutenberg:1342", Complete: true, Title: []string{"Pride and Prejudice\nStatus: checked"}, MediaType: []string{}, Files: []discovery.OfferedFile{{Name: "1342.html.images", Access: "listed_not_acquired", RightsStatus: "not_resolved_at_file_level", DRMStatus: "not_assessed", Bytes: &n, Findings: []string{}}}})
	for _, absent := range []string{`""`, "[]", "Private flag", "Restricted flag", "Media type", "Creator", "_"} {
		if strings.Contains(e, absent) {
			t.Fatalf("%q in\n%s", absent, e)
		}
	}
	for _, want := range []string{"Title: Pride and Prejudice Status: checked\n", "1. 1342.html.images (listed not acquired)\n", "File rights: not resolved at file level\n", "DRM: not assessed\n"} {
		if !strings.Contains(e, want) {
			t.Fatalf("missing %q in\n%s", want, e)
		}
	}
	if strings.Contains(e, "\nStatus: checked") {
		t.Fatal("a source title forged an output line")
	}

	i := Import(library.ImportResult{Applied: true, Library: "/lib", Source: "/intake", AssetID: "sha256:aa", Format: "epub", Bytes: 9, Status: "review", Reason: "unscanned", Titles: []string{}, Languages: []string{}, Findings: []string{}, Identities: []library.Identity{{Role: "unassigned", ID: "stated:0273"}}})
	for _, want := range []string{"Imported\n", "Status: review (unscanned)\n", "Identifier: stated:0273 (unassigned)\n", "Format: epub | Bytes: 9\n"} {
		if !strings.Contains(i, want) {
			t.Fatalf("missing %q in\n%s", want, i)
		}
	}
	for _, absent := range []string{"Reason:", "Title:", "Language:", "Path:", "Antivirus:", `"`} {
		if strings.Contains(i, absent) {
			t.Fatalf("%q in\n%s", absent, i)
		}
	}

	a := Acquisition(acquisition.Result{Status: "untrusted_intake", Complete: true, Output: "/intake", Antivirus: "not_scanned", Checks: &assessment.Checks{Signature: "zip", EPUBPackage: "container_manifest_spine_checked", Warnings: []string{}}})
	for _, want := range []string{"Acquisition: untrusted intake, complete\n", "Antivirus: not scanned\n", "EPUB package: container manifest spine checked\n"} {
		if !strings.Contains(a, want) {
			t.Fatalf("missing %q in\n%s", want, a)
		}
	}
	if strings.Contains(a, "Warning") || strings.Contains(a, "SHA-256") || strings.Contains(a, `"`) {
		t.Fatal(a)
	}
}
