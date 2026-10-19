package present

import (
	"runtime"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/acquisition"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/library"
)

func TestShellArgQuotesEverythingAShellWouldInterpret(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PowerShell quoting is checked on Windows below")
	}
	for in, want := range map[string]string{
		"gutenberg:1342":                    "gutenberg:1342",
		"/srv/books/intake-1":               "/srv/books/intake-1",
		"My Books":                          "'My Books'",
		"$(rm -rf ~)":                       "'$(rm -rf ~)'",
		"`id`;x|y&z>w":                      "'`id`;x|y&z>w'",
		"it's":                              `'it'\''s'`,
		"第一章.epub":                          "'第一章.epub'",
		`a"b\c`:                             `'a"b\c'`,
		"sha256:" + strings.Repeat("a", 64): "sha256:" + strings.Repeat("a", 64),
	} {
		if got, ok := ShellArg(in); !ok || got != want {
			t.Fatalf("%q quoted as %q, want %q", in, got, want)
		}
	}
	for _, refused := range []string{"", "a\x1b[2Jb", "line\nbreak", "tab\there", "bidi\u202ename"} {
		if _, ok := ShellArg(refused); ok {
			t.Fatalf("%q must not be offered for copying", refused)
		}
	}
}

func TestShellArgUsesPowerShellQuotingOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("POSIX quoting is checked above")
	}
	for in, want := range map[string]string{`C:\Books\intake`: `C:\Books\intake`, "My Books": "'My Books'", "it's": "'it''s'", "$env:X": "'$env:X'"} {
		if got, ok := ShellArg(in); !ok || got != want {
			t.Fatalf("%q quoted as %q, want %q", in, got, want)
		}
	}
}

func TestNextStepsFollowTheFirstBookPath(t *testing.T) {
	local := discovery.Evaluation{ID: "gutenberg:1342", Source: "gutenberg", Access: "local_catalog"}
	if got := NextAfterEvaluation(local, nil); got != "Next: nemalo evaluate gutenberg:1342 --files\n" {
		t.Fatal(got)
	}
	record := discovery.Evaluation{ID: "gutenberg:1342", Source: "gutenberg", Files: []discovery.OfferedFile{{Name: "x"}}}
	eligible := []acquisition.Alternative{{File: "1342.mp3", Format: "mp3"}, {File: "book.pdf", Format: "pdf"}, {File: "1342.epub.noimages", Format: "epub"}}
	if got := NextAfterEvaluation(record, eligible); got != "Next: nemalo acquire gutenberg:1342 --file 1342.epub.noimages --output NEW_DIRECTORY\n" {
		t.Fatal("an EPUB comes first", got)
	}
	if got := NextAfterEvaluation(record, eligible[:2]); !strings.Contains(got, "--file book.pdf ") {
		t.Fatal("then a PDF", got)
	}
	if got := NextAfterEvaluation(record, eligible[:1]); got != "" {
		t.Fatal("a single MP3 is not the book path", got)
	}
	// A provider file name that a terminal cannot show is never offered.
	if got := NextAfterEvaluation(record, []acquisition.Alternative{{File: "a\x1b]0;x\x07.epub", Format: "epub"}}); got != "" {
		t.Fatal(got)
	}
	if got := NextAfterEvaluation(record, []acquisition.Alternative{{File: "$(touch x).epub", Format: "epub"}}); !strings.Contains(got, "'$(touch x).epub'") && runtime.GOOS != "windows" {
		t.Fatal(got)
	}

	done := acquisition.Result{Status: "untrusted_intake", Complete: true, Output: "/srv/intake one"}
	got := NextAfterAcquisition(done, "")
	if !strings.HasPrefix(got, "Next: nemalo library import LIBRARY '/srv/intake one' --apply\n") || !strings.Contains(got, "nemalo library init LIBRARY") {
		t.Fatal(got)
	}
	if got := NextAfterAcquisition(done, "/srv/library"); got != "Next: nemalo library import /srv/library '/srv/intake one' --apply\n" {
		t.Fatal("a configured library is named", got)
	}
	for _, r := range []acquisition.Result{{Status: "detected_quarantined", Complete: true, Output: "/x"}, {Status: "transfer_incomplete", Output: "/x"}, {Complete: true}} {
		if got := NextAfterAcquisition(r, ""); got != "" {
			t.Fatal("no import after a detection or an incomplete packet", r, got)
		}
	}

	asset := "sha256:" + strings.Repeat("e", 64)
	preview := library.ImportResult{Library: "/srv/library", Source: "/srv/intake", AssetID: asset, Format: "epub"}
	if got := NextAfterImport(preview); got != "Next: nemalo library import /srv/library /srv/intake --apply\n" {
		t.Fatal(got)
	}
	applied := preview
	applied.Applied = true
	if got := NextAfterImport(applied); got != "Next: nemalo read /srv/library --root /srv/library --asset "+asset+"\n" {
		t.Fatal(got)
	}
	held := preview
	held.AlreadyHeld, held.Format = true, "pdf"
	if got := NextAfterImport(held); !strings.HasPrefix(got, "Next: nemalo read ") {
		t.Fatal("an already held book can be read", got)
	}
	audio := applied
	audio.Format = "mp3"
	if got := NextAfterImport(audio); got != "Next: nemalo library open /srv/library --asset "+asset+"\n" {
		t.Fatal(got)
	}
	if got := NextAfterImport(library.ImportResult{Library: "/srv/library"}); got != "" {
		t.Fatal(got)
	}
}

func TestSuggestedCommandsNeverExpandProviderNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX quoting")
	}
	r := acquisition.Result{Status: "detected_quarantined", Intent: &acquisition.Intent{Request: acquisition.Request{SourceID: "archive:item"}},
		Alternatives:     []acquisition.Alternative{{File: "$(touch pwned).epub", Format: "epub", Bytes: 9}, {File: "bad\x1b[2J.pdf", Format: "pdf", Bytes: 9}},
		SearchSuggestion: "`id` Austen"}
	out := Acquisition(r)
	if !strings.Contains(out, "nemalo acquire archive:item --file '$(touch pwned).epub' --output NEW_DIRECTORY") || strings.Contains(out, "bad") || !strings.Contains(out, "nemalo search '`id` Austen' --source archive") {
		t.Fatal(out)
	}
	rec := Recording(acquisition.RecordingResult{Status: "complete", Complete: true, Output: "/srv/$HOME"})
	if !strings.Contains(rec, "Next: nemalo library ingest LIBRARY '/srv/$HOME' --apply") {
		t.Fatal(rec)
	}
}
