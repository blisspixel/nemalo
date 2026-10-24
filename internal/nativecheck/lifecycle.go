package nativecheck

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/library"
)

var requiredCases = []string{
	"version", "doctor", "initialize", "status", "format-check", "import-preview", "stale-import-refused", "fresh-import-preview", "import-apply", "duplicate-import",
	"units", "evidence", "evidence-replay", "malformed-evidence-refused", "exact-find", "navigation", "citation", "bookmark-enable", "bookmark-save", "bookmark-disable", "disabled-bookmark-refused", "export", "zip-ingest", "integrity-after-intake", "missing-scanner",
	"cleanup-preview", "stale-cleanup-refused", "fresh-cleanup-preview", "cleanup-apply", "restore-conflict-preview", "restore-conflict-apply", "restore-after-conflict", "second-cleanup-preview", "second-cleanup-apply", "purge-preview", "wrong-purge-refused", "purge-apply", "purged-restore", "final-integrity",
}

func (s *suite) lifecycle() {
	for _, dir := range []string{"temp", "tools", "config", "state", "cache", "source", "library", "intake", "exports", "cleanup-source"} {
		s.mkdir(dir)
	}
	s.write("config.json", []byte("{}\n"))
	book, err := bookFixture("Knowledge is power. " + strings.Repeat("Synthetic 日本語 passage. ", 400))
	s.fail(err)
	other, err := bookFixture("A distinct synthetic archive member.")
	s.fail(err)
	archive, err := zipFixture([][2]string{{"nested/", ""}, {"nested/book.epub", string(other)}})
	s.fail(err)
	s.result.FixtureSHA256 = map[string]string{}
	for name, data := range map[string][]byte{"book": book, "archive_book": other, "zip": archive} {
		digest := sha256.Sum256(data)
		s.result.FixtureSHA256[name] = hex.EncodeToString(digest[:])
	}
	source := "source/book space 日本.epub"
	s.write(source, book)
	s.write("intake/book.zip", archive)
	s.write("cleanup-source/held.epub", book)
	s.write("cleanup-source/held.zip", archive)
	s.write("cleanup-source/uncertain.bin", []byte("preserve this uncertain neighbor"))
	if s.err != nil {
		return
	}
	intakeBefore, sourceBefore := s.snapshot("intake"), s.snapshot("source")
	lib, src := s.path("library"), s.path(source)
	var version string
	s.call("version", 0, &version, "version")
	s.require(version != "", "version was empty")
	var doctor struct {
		Security string                           `json:"security_status"`
		Config   struct{ Library, Review string } `json:"configuration"`
	}
	s.call("doctor", 0, &doctor, "doctor")
	s.require(doctor.Security == "not_scanned" && doctor.Config.Library == "" && doctor.Config.Review == "", "doctor loaded an unrelated library or claimed scanning")
	s.call("initialize", 0, nil, "library", "init", lib)
	s.call("status", 0, nil, "library", "status", lib)
	var checked assessment.Report
	s.call("format-check", 0, &checked, "check", src)
	s.require(checked.DetectedFormat == "epub" && checked.Antivirus.Status == "not_scanned", "format and scanner evidence were conflated")
	var plan, held library.ImportResult
	before := s.snapshot("library")
	s.call("import-preview", 0, &plan, "library", "import", lib, src)
	s.require(plan.Plan != "" && !plan.Applied, "import preview had no plan or wrote bytes")
	s.unchanged("library", before)
	s.write(source, other)
	s.call("stale-import-refused", 1, nil, "library", "import", lib, src, "--apply", "--plan", plan.Plan)
	s.unchanged("library", before)
	s.require(bytes.Equal(s.read(source), other), "stale import changed its source")
	s.write(source, book)
	s.call("fresh-import-preview", 0, &plan, "library", "import", lib, src)
	s.call("import-apply", 0, &held, "library", "import", lib, src, "--apply", "--plan", plan.Plan)
	s.require(held.Applied && held.AssetID != "" && held.Status == "review" && held.Reason == "unscanned", "unscanned import did not stay in review")
	before = s.snapshot("library")
	var duplicate library.ImportResult
	s.call("duplicate-import", 0, &duplicate, "library", "import", lib, src, "--apply")
	s.require(duplicate.AlreadyHeld && duplicate.AssetID == held.AssetID, "repeat import did not resolve the same asset")
	s.unchanged("library", before)
	s.unchanged("source", sourceBefore)
	s.content(lib, held.AssetID)
	var exported library.ExportResult
	before = s.snapshot("library")
	s.call("export", 0, &exported, "library", "export", lib, "--asset", held.AssetID, "--output", s.path("exports"))
	s.require(exported.AssetID == held.AssetID && exported.SHA256 == held.SHA256 && exported.Path != s.path("library/"+held.Path), "export did not identify a separate exact copy")
	exports := s.snapshot("exports")
	found := false
	for name, f := range exports {
		if name != "." && f.SHA256 == held.SHA256 {
			found = true
		}
	}
	s.require(found, "exported bytes did not match the holding")
	s.unchanged("library", before)
	var ingested library.IngestResult
	s.call("zip-ingest", 0, &ingested, "library", "ingest", lib, s.path("intake"), "--apply")
	s.require(ingested.Complete && ingested.Imported == 1 && ingested.Review == 1 && ingested.Checked == 0 && ingested.Failed == 0, "ZIP ingest did not account for the review member")
	s.unchanged("intake", intakeBefore)
	before = s.snapshot("library")
	var health library.LibraryCheck
	s.call("integrity-after-intake", 0, &health, "library", "check", lib)
	s.require(health.Healthy && health.Verified == 2 && health.Checked == 0 && health.Review["unscanned"] == 2, "byte integrity incorrectly became checked publication")
	s.unchanged("library", before)
	s.call("missing-scanner", 1, &checked, "check", src, "--scan")
	s.require(checked.Antivirus.Status == "unavailable" && checked.Status != "limited_checks_passed", "missing scanner was treated as coverage")
	s.unchanged("library", before)
	s.unchanged("source", sourceBefore)
	assets, bookmarks := s.snapshot("library/assets"), s.read("library/.nemalo/bookmarks.jsonl")
	s.cleanup(lib, book, other)
	s.unchanged("library/assets", assets)
	s.require(bytes.Equal(bookmarks, s.read("library/.nemalo/bookmarks.jsonl")), "cleanup/restore/purge changed disabled bookmarks")
	s.unchanged("source", sourceBefore)
	s.unchanged("intake", intakeBefore)
	s.call("final-integrity", 0, &health, "library", "check", lib)
	s.require(health.Healthy && health.Verified == 2 && health.Checked == 0 && health.HeldFiles == 0, "final library integrity or review status changed")
	if s.err == nil {
		actual := make([]string, len(s.result.Commands))
		for i, c := range s.result.Commands {
			actual[i] = c.Name
		}
		a, _ := json.Marshal(actual)
		b, _ := json.Marshal(requiredCases)
		s.require(bytes.Equal(a, b), "native acceptance case sequence is incomplete")
	}
}

func (s *suite) content(lib, asset string) {
	before := s.snapshot("library")
	args := []string{"content", "read", lib, "--root", lib, "--asset", asset, "--representation", "epub-text/2", "--max-bytes", "64"}
	var units, text, replay, find content.Result
	s.call("units", 0, &units, "content", "units", lib, "--root", lib, "--asset", asset, "--representation", "epub-text/2")
	s.require(len(units.Units) == 1, "source units changed")
	s.call("evidence", 0, &text, append(append([]string{}, args...), "--evidence")...)
	if s.err != nil {
		return
	}
	s.require(text.Evidence != nil && text.Locator != nil && strings.Contains(text.Text, "Knowledge is power.") && text.Continuation != "", "bounded evidence lost the source range")
	if s.err != nil {
		return
	}
	token := text.Evidence.Reference
	s.call("evidence-replay", 0, &replay, append(append([]string{}, args...), "--evidence-reference", token)...)
	a, _ := json.Marshal(text)
	b, _ := json.Marshal(replay)
	s.require(bytes.Equal(a, b), "exact evidence replay changed")
	s.call("malformed-evidence-refused", 1, &replay, append(append([]string{}, args...), "--evidence-reference", token+"x")...)
	s.require(replay.Text == "" && replay.Resource == nil && replay.Evidence == nil && replay.Continuation == "", "failed evidence exposed a payload")
	s.call("exact-find", 0, &find, "content", "locate", lib, "--root", lib, "--asset", asset, "--text", "Knowledge is power.")
	s.require(len(find.Matches) == 1, "exact source quotation was not found")
	s.call("navigation", 0, &units, "content", "navigation", lib, "--root", lib, "--asset", asset)
	s.require(len(units.Navigation) == 1 && len(units.Navigation[0].Entries) == 1, "declared navigation missing")
	var citation struct {
		MetadataSHA256 string          `json:"metadata_sha256"`
		CSLJSON        json.RawMessage `json:"csl_json"`
		Unknown        []string        `json:"unknown_fields"`
	}
	s.call("citation", 0, &citation, "library", "citation", lib, "--asset", asset)
	s.require(citation.MetadataSHA256 == text.Evidence.MetadataSHA256 && len(citation.CSLJSON) > 2 && len(citation.Unknown) > 0, "citation lost metadata binding or uncertainty")
	s.unchanged("library", before)
	s.call("bookmark-enable", 0, nil, "bookmark", "enable", lib)
	bookmarkArgs := []string{"bookmark", "save", lib, "--asset", asset, "--representation", "epub-text/2", "--representation-id", text.RepresentationID, "--unit", "0", "--text-offset", "0"}
	s.call("bookmark-save", 0, nil, bookmarkArgs...)
	s.call("bookmark-disable", 0, nil, "bookmark", "disable", lib)
	before = s.snapshot("library")
	s.call("disabled-bookmark-refused", 1, nil, bookmarkArgs...)
	s.unchanged("library", before)
}

func (s *suite) cleanup(lib string, book, other []byte) {
	folder := s.path("cleanup-source")
	var plan, moved library.CleanupResult
	before, sourceBefore := s.snapshot("library"), s.snapshot("cleanup-source")
	s.call("cleanup-preview", 0, &plan, "library", "cleanup", lib, folder)
	s.require(plan.Move == 2 && plan.Keep == 1 && plan.Digest != "" && !plan.Applied, "cleanup did not preserve the uncertain neighbor")
	s.unchanged("library", before)
	s.unchanged("cleanup-source", sourceBefore)
	s.write("cleanup-source/held.epub", other)
	s.call("stale-cleanup-refused", 1, nil, "library", "cleanup", lib, folder, "--apply", "--plan", plan.Digest)
	s.unchanged("library", before)
	s.require(bytes.Equal(s.read("cleanup-source/held.epub"), other), "stale cleanup moved changed bytes")
	s.write("cleanup-source/held.epub", book)
	s.call("fresh-cleanup-preview", 0, &plan, "library", "cleanup", lib, folder)
	s.call("cleanup-apply", 0, &moved, "library", "cleanup", lib, folder, "--apply", "--plan", plan.Digest)
	s.require(moved.Applied && moved.Moved == 2 && moved.CleanupID != "", "cleanup did not move the exact held sources")
	var restore library.RestoreResult
	replacement := []byte("preserve this replacement")
	s.write("cleanup-source/held.epub", replacement)
	before = s.snapshot("library")
	conflictBefore := s.snapshot("cleanup-source")
	s.call("restore-conflict-preview", 0, &restore, "library", "restore", lib, "--cleanup", moved.CleanupID)
	s.require(restore.Conflicts == 1 && restore.Restore == 1 && !restore.Applied, "restore preview did not distinguish its conflict")
	s.unchanged("library", before)
	s.unchanged("cleanup-source", conflictBefore)
	s.call("restore-conflict-apply", 0, &restore, "library", "restore", lib, "--cleanup", moved.CleanupID, "--apply")
	s.require(restore.Conflicts == 1 && restore.Restore == 1 && bytes.Equal(s.read("cleanup-source/held.epub"), replacement), "restore replaced a conflicting file")
	s.remove("cleanup-source/held.epub") // Only the replacement generated above.
	s.call("restore-after-conflict", 0, &restore, "library", "restore", lib, "--cleanup", moved.CleanupID, "--apply")
	s.require(restore.Restore == 1 && restore.Conflicts == 0, "held bytes did not restore after removing the owned replacement")
	s.unchanged("cleanup-source", sourceBefore)
	s.call("second-cleanup-preview", 0, &plan, "library", "cleanup", lib, folder)
	s.call("second-cleanup-apply", 0, &moved, "library", "cleanup", lib, folder, "--apply", "--plan", plan.Digest)
	s.require(moved.Moved == 2, "second cleanup did not establish a fresh operation")
	var purge library.PurgeResult
	before = s.snapshot("library")
	purgedSource := s.snapshot("cleanup-source")
	s.call("purge-preview", 0, &purge, "library", "purge", lib, "--cleanup", moved.CleanupID)
	s.require(purge.Purge == 2 && purge.Digest != "" && !purge.Applied, "purge preview did not bind both held sources")
	s.unchanged("library", before)
	s.unchanged("cleanup-source", purgedSource)
	digest := purge.Digest
	s.require(digest != strings.Repeat("0", 64), "fixture purge digest collided with refusal sentinel")
	s.call("wrong-purge-refused", 1, nil, "library", "purge", lib, "--cleanup", moved.CleanupID, "--apply", "--plan", strings.Repeat("0", 64))
	s.unchanged("library", before)
	s.call("purge-apply", 0, &purge, "library", "purge", lib, "--cleanup", moved.CleanupID, "--apply", "--plan", digest)
	s.require(purge.Applied && purge.Purged == 2, "purge did not remove the approved held copies")
	s.call("purged-restore", 0, &restore, "library", "restore", lib, "--cleanup", moved.CleanupID, "--apply")
	s.require(restore.Purged == 2 && restore.Restore == 0, "restore reconstructed already purged copies")
	s.unchanged("cleanup-source", purgedSource)
	s.require(sha256.Sum256(s.read("cleanup-source/uncertain.bin")) == sha256.Sum256([]byte("preserve this uncertain neighbor")), "uncertain neighbor was changed")
}
