package library

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/scanner"
)

func TestManagedImportPublicationAuditAndResume(t *testing.T) {
	ctx := context.Background()
	libraryRoot := t.TempDir()
	if _, err := Import(ctx, ImportRequest{Library: libraryRoot, Source: filepath.Join(t.TempDir(), "book.epub"), Apply: true}); err == nil {
		t.Fatal("uninitialized library accepted")
	}
	if _, err := os.Stat(filepath.Join(libraryRoot, ".nemalo")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("import created control state")
	}
	if _, err := Initialize(ctx, libraryRoot); err != nil {
		t.Fatal(err)
	}
	initJournal, err := os.ReadFile(filepath.Join(libraryRoot, ".nemalo", journalName))
	if err != nil {
		t.Fatal(err)
	}
	source := writeEPUB(t, t.TempDir())
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := Import(ctx, ImportRequest{Library: libraryRoot, Source: source})
	if err != nil || preview.Applied || preview.Status != "review" || preview.Reason != "unscanned" || preview.Format != "epub" || preview.AssetID == "" {
		t.Fatal(preview, err)
	}
	if _, err := os.Stat(filepath.Join(libraryRoot, ".nemalo", operationsName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preview wrote an operations journal")
	}
	stored, err := Import(ctx, ImportRequest{Library: libraryRoot, Source: source, Apply: true})
	if err != nil || !stored.Applied || stored.AlreadyHeld || stored.Status != "review" || stored.Reason != "unscanned" || stored.Titles[0] != "Test" || stored.Languages[0] != "ja" {
		t.Fatal(stored, err)
	}
	if got, err := os.ReadFile(source); err != nil || !bytes.Equal(got, original) {
		t.Fatal("source changed", err)
	}
	asset := filepath.Join(libraryRoot, filepath.FromSlash(stored.Path))
	if got, err := os.ReadFile(asset); err != nil || !bytes.Equal(got, original) {
		t.Fatal("stored bytes differ", err)
	}
	again, err := Import(ctx, ImportRequest{Library: libraryRoot, Source: source, Apply: true})
	if err != nil || !again.AlreadyHeld {
		t.Fatal(again, err)
	}
	ops, err := os.ReadFile(filepath.Join(libraryRoot, ".nemalo", operationsName))
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "copy.epub")
	if err := os.WriteFile(other, original, 0600); err != nil {
		t.Fatal(err)
	}
	linked, err := Import(ctx, ImportRequest{Library: libraryRoot, Source: other, Apply: true})
	if err != nil || linked.AlreadyHeld || linked.AssetID != stored.AssetID {
		t.Fatal(linked, err)
	}
	lines := strings.Split(strings.TrimSuffix(string(mustRead(t, filepath.Join(libraryRoot, ".nemalo", holdingsName))), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatal(len(lines))
	}
	var latest Holding
	if err := json.Unmarshal([]byte(lines[1]), &latest); err != nil || len(latest.Sources) != 2 || latest.Status != "review" {
		t.Fatal(latest, err)
	}
	audit, err := AuditManaged(ctx, libraryRoot)
	if err == nil || audit.Assets != 1 || len(audit.Findings) != 1 || audit.Findings[0].Status != "review" {
		t.Fatal(audit, err)
	}
	checked, err := Import(ctx, ImportRequest{Library: libraryRoot, Source: source, Apply: true, Scan: true, Scanner: cleanScan{}})
	if err != nil || checked.Status != "checked" || checked.Reason != "checks_and_scan_recorded" || checked.AlreadyHeld {
		t.Fatal(checked, err)
	}
	if _, err := AuditManaged(ctx, libraryRoot); err != nil {
		t.Fatal(err)
	}
	kept, err := Import(ctx, ImportRequest{Library: libraryRoot, Source: source, Apply: true})
	if err != nil || !kept.AlreadyHeld || kept.Status != "checked" {
		t.Fatal("rescanless import downgraded a checked asset", kept, err)
	}
	pdf := filepath.Join(t.TempDir(), "paper.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF-1.7\n%%EOF"), 0600); err != nil {
		t.Fatal(err)
	}
	paper, err := Import(ctx, ImportRequest{Library: libraryRoot, Source: pdf, Apply: true, Scan: true, Scanner: cleanScan{}})
	if err != nil || paper.Status != "review" || paper.Reason != "format_not_checked_for_publication" || paper.Format != "pdf" {
		t.Fatal(paper, err)
	}
	if err := os.Remove(filepath.Join(libraryRoot, filepath.FromSlash(paper.Path))); err != nil {
		t.Fatal(err)
	}
	missing, err := AuditManaged(ctx, libraryRoot)
	if err == nil || missing.Findings[0].Status != "missing" {
		t.Fatal(missing, err)
	}
	packet := t.TempDir()
	packetFile := writeEPUBParagraph(t, packet, "Packet")
	if err := os.Rename(packetFile, filepath.Join(packet, "content.epub")); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(packet, "content.epub"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	receipt := map[string]any{"schema_version": 1, "complete": true, "status": "untrusted_intake", "final_url": "https://example.test/book.epub", "transfer": map[string]any{"bytes": len(body), "sha256": hex.EncodeToString(sum[:])}, "intent": map[string]any{"schema_version": 1, "request": map[string]any{"source_id": "archive:demo", "source_file": "book.epub"}, "selection": map[string]any{"rights_statements": []string{"Public domain in the USA."}, "declared_license_urls": []string{"https://example.test/rights"}}}}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packet, "receipt.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	imported, err := Import(ctx, ImportRequest{Library: libraryRoot, Source: packet, Apply: true})
	if err != nil || imported.SourceKind != "packet" || imported.AlreadyHeld || imported.Status != "review" || imported.Reason != "unscanned" {
		t.Fatal(imported, err)
	}
	var packetHolding Holding
	packetLines := strings.Split(strings.TrimSuffix(string(mustRead(t, filepath.Join(libraryRoot, ".nemalo", holdingsName))), "\n"), "\n")
	if err := json.Unmarshal([]byte(packetLines[len(packetLines)-1]), &packetHolding); err != nil || packetHolding.Status != "review" || packetHolding.ID == stored.AssetID {
		t.Fatal(packetHolding, err)
	}
	foundPacket := false
	for _, item := range packetHolding.Sources {
		if item.Kind == "packet" && item.SourceID == "archive:demo" && item.DeclaredURL == "https://example.test/book.epub" && len(item.DeclaredRights) == 1 && len(item.DeclaredLicenses) == 1 {
			foundPacket = true
		}
	}
	if !foundPacket {
		t.Fatal(packetHolding)
	}
	bad := bytes.Replace(encoded, []byte(hex.EncodeToString(sum[:])), []byte(strings.Repeat("ab", 32)), 1)
	if err := os.WriteFile(filepath.Join(packet, "receipt.json"), bad, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Import(ctx, ImportRequest{Library: libraryRoot, Source: packet, Apply: true}); err == nil || !strings.Contains(err.Error(), "receipt") {
		t.Fatal(err)
	}
	inside := filepath.Join(libraryRoot, "local.epub")
	if err := os.WriteFile(inside, original, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Import(ctx, ImportRequest{Library: libraryRoot, Source: inside, Apply: true}); err == nil {
		t.Fatal("source inside the library was imported")
	}
	if !bytes.Equal(initJournal, mustRead(t, filepath.Join(libraryRoot, ".nemalo", journalName))) {
		t.Fatal("import changed the initialization journal")
	}
	if _, err := Initialize(ctx, libraryRoot); err != nil {
		t.Fatal(err)
	}
	operations := filepath.Join(libraryRoot, ".nemalo", operationsName)
	preserved := mustRead(t, operations)
	preserved[20] ^= 0x1
	if err := os.WriteFile(operations, preserved, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LibraryStatus(ctx, libraryRoot); !errors.Is(err, ErrStateReview) {
		t.Fatal(err)
	}
	if _, err := Import(ctx, ImportRequest{Library: libraryRoot, Source: source, Apply: true}); !errors.Is(err, ErrStateReview) {
		t.Fatal(err)
	}
	if !bytes.Equal(preserved, mustRead(t, operations)) {
		t.Fatal("corrupt operations journal was repaired")
	}
	_ = ops
}

func TestImportCrashRecovery(t *testing.T) {
	for _, stage := range []string{"intent", "copied", "linked", "recorded"} {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			if _, err := Initialize(ctx, root); err != nil {
				t.Fatal(err)
			}
			source := writeEPUB(t, t.TempDir())
			before, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			stop := errors.New("stop after " + stage)
			_, err = importLibrary(ctx, ImportRequest{Library: root, Source: source, Apply: true}, func(got string) error {
				if got == stage {
					return stop
				}
				return nil
			})
			if !errors.Is(err, stop) {
				t.Fatal(err)
			}
			if got, err := os.ReadFile(source); err != nil || !bytes.Equal(got, before) {
				t.Fatal("interrupted import changed the source", err)
			}
			done, err := Import(ctx, ImportRequest{Library: root, Source: source, Apply: true})
			if err != nil || !done.Applied || !done.Resumed || done.Status != "review" {
				t.Fatal(done, err)
			}
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(done.Path)+".partial")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("partial file remained", err)
			}
			if got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(done.Path))); err != nil || !bytes.Equal(got, before) {
				t.Fatal("resumed bytes differ", err)
			}
		})
	}
}

func TestImportRejectsABusyWriter(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	source := writeEPUB(t, t.TempDir())
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := importLibrary(ctx, ImportRequest{Library: root, Source: source, Apply: true}, func(stage string) error {
			if stage == "intent" {
				close(started)
				<-release
			}
			return nil
		})
		done <- err
	}()
	<-started
	if _, err := Import(ctx, ImportRequest{Library: root, Source: source, Apply: true}); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestImportRejectsConflictSharedLinksAndBadParents(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	source := writeEPUB(t, t.TempDir())
	stored, err := Import(ctx, ImportRequest{Library: root, Source: source, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	ops := filepath.Join(root, ".nemalo", operationsName)
	before := mustRead(t, ops)
	asset := filepath.Join(root, filepath.FromSlash(stored.Path))
	if err := os.Link(asset, asset+".alias"); err != nil {
		t.Fatal(err)
	}
	if _, err := Import(ctx, ImportRequest{Library: root, Source: source, Apply: true}); !errors.Is(err, errStoredObject) {
		t.Fatal(err)
	}
	if !bytes.Equal(before, mustRead(t, ops)) {
		t.Fatal("shared asset started another import")
	}
	if err := os.Remove(asset + ".alias"); err != nil {
		t.Fatal(err)
	}
	original := mustRead(t, asset)
	if err := os.WriteFile(asset, []byte("not-the-book"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Import(ctx, ImportRequest{Library: root, Source: source, Apply: true}); !errors.Is(err, errStoredObject) {
		t.Fatal(err)
	}
	if !bytes.Equal(before, mustRead(t, ops)) || !bytes.Equal([]byte("not-the-book"), mustRead(t, asset)) {
		t.Fatal("conflicting asset was replaced or journaled")
	}
	if err := os.WriteFile(asset, original, 0600); err != nil {
		t.Fatal(err)
	}
	blocked := t.TempDir()
	if _, err := Initialize(ctx, blocked); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(blocked, "assets")
	if err := os.WriteFile(parent, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Import(ctx, ImportRequest{Library: blocked, Source: source, Apply: true}); !errors.Is(err, errStoredObject) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(blocked, ".nemalo", operationsName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("store parent failure wrote an intent", err)
	}
	if !bytes.Equal([]byte("file"), mustRead(t, parent)) {
		t.Fatal("non-directory store parent changed")
	}
}

func TestImportDoesNotAdoptASharedPartial(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	source := writeEPUB(t, t.TempDir())
	stop := errors.New("stop after copy")
	stopped, err := importLibrary(ctx, ImportRequest{Library: root, Source: source, Apply: true}, func(stage string) error {
		if stage == "copied" {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) || stopped.Path == "" {
		t.Fatal(stopped, err)
	}
	partial := filepath.Join(root, filepath.FromSlash(stopped.Path)+".partial")
	if err := os.Link(partial, partial+".alias"); err != nil {
		t.Fatal(err)
	}
	if _, err := Import(ctx, ImportRequest{Library: root, Source: source, Apply: true}); !errors.Is(err, errStoredObject) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(stopped.Path))); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("shared partial was linked into the store", err)
	}
}

func TestImportNamesScanGapsAndRejectsCraftedHoldings(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	source := writeEPUB(t, t.TempDir())
	scanned, err := Import(ctx, ImportRequest{Library: root, Source: source, Apply: true, Scan: true, Scanner: statusScan{status: "findings"}})
	if err != nil || scanned.Status != "review" || scanned.Reason != "scan_findings" {
		t.Fatal(scanned, err)
	}
	other := filepath.Join(t.TempDir(), "again.epub")
	if err := os.WriteFile(other, mustRead(t, source), 0600); err != nil {
		t.Fatal(err)
	}
	unknown, err := Import(ctx, ImportRequest{Library: root, Source: other, Apply: true, Scan: true, Scanner: statusScan{status: "weird status"}})
	if err != nil || unknown.Reason != "scan_unrecognized" || unknown.Status != "review" {
		t.Fatal(unknown, err)
	}
	holdings := filepath.Join(root, ".nemalo", holdingsName)
	parts := strings.Split(strings.TrimSuffix(string(mustRead(t, holdings)), "\n"), "\n")
	var holding Holding
	if err := json.Unmarshal([]byte(parts[len(parts)-1]), &holding); err != nil {
		t.Fatal(err)
	}
	holding.Sources[0].DeclaredRights = []string{"a\nb"}
	encoded, err := json.Marshal(holding)
	if err != nil {
		t.Fatal(err)
	}
	parts[len(parts)-1] = string(encoded)
	rewritten := []byte(strings.Join(parts, "\n") + "\n")
	if err := os.WriteFile(holdings, rewritten, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LibraryStatus(ctx, root); !errors.Is(err, ErrStateReview) {
		t.Fatal(err)
	}
	if !bytes.Equal(rewritten, mustRead(t, holdings)) {
		t.Fatal("crafted holding was repaired")
	}
}

func TestSameImportSourceIsIdentityNotAnotherCaseName(t *testing.T) {
	dir := t.TempDir()
	lower := filepath.Join(dir, "book.epub")
	if err := os.WriteFile(lower, []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	if !sameImportSource(lower, lower) {
		t.Fatal("same path differed")
	}
	upper := filepath.Join(dir, "BOOK.EPUB")
	if _, err := os.Lstat(upper); err != nil {
		if err := os.WriteFile(upper, []byte("two"), 0600); err != nil {
			t.Fatal(err)
		}
		if sameImportSource(lower, upper) {
			t.Fatal("different files matched")
		}
		return
	}
	if !sameImportSource(lower, upper) {
		t.Fatal("same file with different case did not match")
	}
}

type cleanScan struct{}

func (cleanScan) Scan(context.Context, string) scanner.Result {
	return scanner.Result{Status: "no_detections_reported", ExitCode: 0}
}

type statusScan struct{ status string }

func (s statusScan) Scan(context.Context, string) scanner.Result {
	return scanner.Result{Status: s.status, ExitCode: 1}
}

func writeEPUB(t *testing.T, dir string) string {
	t.Helper()
	return writeEPUBParagraph(t, dir, "Hello")
}

func writeEPUBParagraph(t *testing.T, dir, paragraph string) string {
	t.Helper()
	files := map[string]string{
		"mimetype":               "application/epub+zip",
		"META-INF/container.xml": `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`,
		"book.opf":               `<package xmlns="http://www.idpf.org/2007/opf"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Test</dc:title><dc:language>ja</dc:language></metadata><manifest><item id="c" href="chapter.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="c"/></spine></package>`,
		"chapter.xhtml":          `<html xmlns="http://www.w3.org/1999/xhtml"><body><p>` + paragraph + `</p></body></html>`,
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	header, err := z.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := header.Write([]byte(files["mimetype"])); err != nil {
		t.Fatal(err)
	}
	delete(files, "mimetype")
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		body := files[name]
		entry, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "book.epub")
	if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

var _ assessment.Scanner = cleanScan{}
