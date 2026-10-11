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
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/scanner"
)

// messyFolder builds a folder with a loose EPUB, a PDF, an intake packet, a ZIP
// holding two EPUBs and a text file, a nested folder, and an unsupported file.
func messyFolder(t *testing.T) (string, [][]byte) {
	t.Helper()
	dir := t.TempDir()
	loose := mustRead(t, writeEPUBParagraph(t, t.TempDir(), "Loose"))
	member1 := mustRead(t, writeEPUBParagraph(t, t.TempDir(), "Member one"))
	member2 := mustRead(t, writeEPUBParagraph(t, t.TempDir(), "Member two"))
	packetBook := mustRead(t, writeEPUBParagraph(t, t.TempDir(), "Packet"))
	files := map[string][]byte{
		"loose.epub":             loose,
		"papers/paper.pdf":       []byte("%PDF-1.7\n%%EOF"),
		"notes/readme.txt":       []byte("not a book"),
		"intake/p1/content.epub": packetBook,
	}
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sum := sha256.Sum256(packetBook)
	receipt, _ := json.Marshal(map[string]any{"complete": true, "status": "untrusted_intake", "antivirus": "not_scanned", "transfer": map[string]any{"bytes": len(packetBook), "sha256": hex.EncodeToString(sum[:])}})
	if err := os.WriteFile(filepath.Join(dir, "intake", "p1", "receipt.json"), receipt, 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for name, body := range map[string][]byte{"books/one.epub": member1, "books/two.epub": member2, "cover.txt": []byte("x")} {
		w, err := z.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bundle.zip"), buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, [][]byte{loose, member1, member2, packetBook}
}

func snapshotTree(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out[p] = mustRead(t, p)
		}
		return nil
	})
	return out
}

func TestIngestPreviewThenApplyAccountsForEverything(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	folder, _ := messyFolder(t)
	before := snapshotTree(t, folder)
	preview, err := Ingest(ctx, IngestRequest{Library: root, Folder: folder})
	if err != nil || preview.Applied || preview.Imported != 5 || preview.Failed != 0 {
		t.Fatal(preview, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".nemalo", operationsName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preview wrote library journals")
	}
	applied, err := Ingest(ctx, IngestRequest{Library: root, Folder: folder, Apply: true})
	if err != nil || !applied.Applied || applied.Imported != 5 || applied.NotImported < 3 {
		t.Fatal(applied, err)
	}
	outcomes := map[string]string{}
	for _, it := range applied.Items {
		outcomes[it.Source] = it.Outcome
	}
	for source, want := range map[string]string{
		"loose.epub": "review", "papers/paper.pdf": "review", "intake/p1": "review",
		"bundle.zip!/books/one.epub": "review", "bundle.zip!/books/two.epub": "review",
		"bundle.zip!/cover.txt": "not_imported", "bundle.zip": "preserved", "notes/readme.txt": "not_imported",
	} {
		if outcomes[source] != want {
			t.Fatalf("%s: %q, want %q (%v)", source, outcomes[source], want, outcomes)
		}
	}
	page, err := FindManaged(ctx, root, "bundle.zip!/books/one.epub", "all", "all", 10, 0)
	if err != nil || page.Total != 1 || page.Holdings[0].Sources[0].Kind != "archive_member" {
		t.Fatal("archive member provenance", page, err)
	}
	after := snapshotTree(t, folder)
	if len(after) != len(before) {
		t.Fatal("ingest changed the folder")
	}
	for p, b := range before {
		if !bytes.Equal(after[p], b) {
			t.Fatal("ingest changed", p)
		}
	}
	again, err := Ingest(ctx, IngestRequest{Library: root, Folder: folder, Apply: true})
	if err != nil || again.Imported != 5 {
		t.Fatal(again, err)
	}
	for _, it := range again.Items {
		if it.Import != nil && !it.Import.AlreadyHeld {
			t.Fatal("re-ingest wrote again", it.Source)
		}
	}
}

func TestIngestBatchesScansAndBindsThemByHash(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	folder, _ := messyFolder(t)
	var batches []int
	r, err := Ingest(ctx, IngestRequest{Library: root, Folder: folder, Apply: true, Scan: true, Scanner: batchScan{&batches, nil}})
	if err != nil || r.Checked != 4 || r.Review != 1 {
		t.Fatal("expected four checked EPUBs and the PDF in review", r, err)
	}
	// One batch per source directory (loose, papers, staging); the packet and
	// any cache miss fall back to single scans of their own snapshots.
	for _, n := range batches {
		if n > 32 {
			t.Fatal("batch over the bound", batches)
		}
	}
	if len(batches) == 0 {
		t.Fatal("no scan ran", batches)
	}
}

func TestIngestResumesAnInterruptedArchiveMember(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	folder, _ := messyFolder(t)
	// Simulate a crash after the intent for one member was journaled: import
	// that member alone from a staging copy that then disappears.
	staged := filepath.Join(t.TempDir(), "00000.epub")
	member := mustRead(t, writeEPUBParagraph(t, t.TempDir(), "Member one"))
	if err := os.WriteFile(staged, member, 0o600); err != nil {
		t.Fatal(err)
	}
	label := filepath.Join(folder, "bundle.zip") + "!/books/one.epub"
	if _, err := importLibrary(ctx, ImportRequest{Library: root, Source: staged, Apply: true, Label: label}, func(stage string) error {
		if stage == "intent" {
			return errors.New("crash")
		}
		return nil
	}); err == nil {
		t.Fatal("staged crash ignored")
	}
	if err := os.Remove(staged); err != nil {
		t.Fatal(err)
	}
	r, err := Ingest(ctx, IngestRequest{Library: root, Folder: folder, Apply: true})
	if err != nil {
		t.Fatal("re-ingest did not resume the pending member", err, r)
	}
	resumed := false
	for _, it := range r.Items {
		if it.Source == "bundle.zip!/books/one.epub" && it.Import != nil && it.Import.Resumed {
			resumed = true
		}
	}
	if !resumed {
		t.Fatal("pending archive member was not resumed by its label")
	}
	if _, err := LibraryStatus(ctx, root); err != nil {
		t.Fatal(err)
	}
}

func TestIngestRefusesOverlapAndUninitializedLibraries(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Ingest(ctx, IngestRequest{Library: root, Folder: t.TempDir()}); err == nil {
		t.Fatal("uninitialized library accepted")
	}
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(root, "incoming")
	if err := os.Mkdir(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, folder := range []string{inside, filepath.Dir(root)} {
		if _, err := Ingest(ctx, IngestRequest{Library: root, Folder: folder}); err == nil || !strings.Contains(err.Error(), "contain each other") {
			t.Fatal("overlapping folder accepted", folder, err)
		}
	}
}

func writeTree(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func intakeReceipt(t *testing.T, book []byte) []byte {
	t.Helper()
	sum := sha256.Sum256(book)
	receipt, err := json.Marshal(map[string]any{"complete": true, "status": "untrusted_intake", "antivirus": "not_scanned", "transfer": map[string]any{"bytes": len(book), "sha256": hex.EncodeToString(sum[:])}})
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

// Regression: entries beside a packet's parts were dropped, and a packet at
// the top of the folder swallowed every other top-level entry.
func TestIngestAccountsForEntriesBesidePackets(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	top := mustRead(t, writeEPUBParagraph(t, t.TempDir(), "Top packet"))
	inner := mustRead(t, writeEPUBParagraph(t, t.TempDir(), "Inner packet"))
	folder := t.TempDir()
	writeTree(t, folder, map[string][]byte{
		"content.epub": top, "receipt.json": intakeReceipt(t, top),
		"loose.epub":      mustRead(t, writeEPUBParagraph(t, t.TempDir(), "Beside the top packet")),
		"notes.txt":       []byte("notes"),
		"p1/content.epub": inner, "p1/receipt.json": intakeReceipt(t, inner),
		"p1/extra.epub": mustRead(t, writeEPUBParagraph(t, t.TempDir(), "Inside a packet folder")),
		"p1/cover.jpg":  []byte("jpeg"),
	})
	r, err := Ingest(ctx, IngestRequest{Library: root, Folder: folder, Apply: true})
	if err != nil {
		t.Fatal(r, err)
	}
	outcomes := map[string]string{}
	for _, it := range r.Items {
		outcomes[it.Source] = it.Outcome
	}
	for source, want := range map[string]string{
		".": "review", "content.epub": "packet_part", "receipt.json": "packet_part",
		"loose.epub": "review", "notes.txt": "not_imported",
		"p1": "review", "p1/content.epub": "packet_part", "p1/receipt.json": "packet_part",
		"p1/extra.epub": "review", "p1/cover.jpg": "not_imported",
	} {
		if outcomes[source] != want {
			t.Fatalf("%s: %q, want %q (%v)", source, outcomes[source], want, outcomes)
		}
	}
	if r.Imported != 4 || r.NotImported != 2 || len(r.Items) != 10 {
		t.Fatal("counts", r.Imported, r.NotImported, len(r.Items))
	}
}

// stagingWatch records the most archive staging directories present at once.
type stagingWatch struct {
	temp string
	most *int
}

func (s stagingWatch) count() {
	dirs, _ := filepath.Glob(filepath.Join(s.temp, "nemalo-ingest-*", "*"))
	*s.most = max(*s.most, len(dirs))
}

func (s stagingWatch) Scan(context.Context, string) scanner.Result {
	s.count()
	return scanner.Result{Status: "no_detections_reported"}
}

func (s stagingWatch) ScanMany(_ context.Context, files []string) []scanner.Result {
	s.count()
	out := make([]scanner.Result, len(files))
	for i := range out {
		out[i].Status = "no_detections_reported"
	}
	return out
}

// Regression: every archive was extracted before any import, so staging held
// all archives at once. Each archive is now staged, imported, and removed.
func TestIngestStagesOneArchiveAtATime(t *testing.T) {
	ctx := context.Background()
	temp := t.TempDir()
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, temp)
	}
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	folder := t.TempDir()
	for _, name := range []string{"a.zip", "b.zip", "c.zip"} {
		var buf bytes.Buffer
		z := zip.NewWriter(&buf)
		w, err := z.Create("book.epub")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(mustRead(t, writeEPUBParagraph(t, t.TempDir(), "Book in "+name))); err != nil {
			t.Fatal(err)
		}
		if err := z.Close(); err != nil {
			t.Fatal(err)
		}
		writeTree(t, folder, map[string][]byte{name: buf.Bytes()})
	}
	most := 0
	for _, apply := range []bool{false, true} {
		r, err := Ingest(ctx, IngestRequest{Library: root, Folder: folder, Apply: apply, Scan: true, Scanner: stagingWatch{temp, &most}})
		if err != nil || r.Imported != 3 {
			t.Fatal(r, err)
		}
	}
	if most != 1 {
		t.Fatal("staging directories present at once:", most)
	}
	if left, _ := filepath.Glob(filepath.Join(temp, "nemalo-ingest-*")); len(left) != 0 {
		t.Fatal("staging left behind", left)
	}
}

// incompleteBatch reports every batch as incomplete and single scans as clean.
type incompleteBatch struct{ singles *int }

func (b incompleteBatch) Scan(context.Context, string) scanner.Result {
	*b.singles++
	return scanner.Result{Status: "no_detections_reported"}
}

func (b incompleteBatch) ScanMany(_ context.Context, files []string) []scanner.Result {
	out := make([]scanner.Result, len(files))
	for i := range out {
		out[i].Status = "incomplete"
	}
	return out
}

// Regression: an incomplete batch result was cached and reused for the import
// instead of scanning that file again.
func TestIngestRescansWhenTheBatchWasIncomplete(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	folder := t.TempDir()
	writeTree(t, folder, map[string][]byte{"one.epub": mustRead(t, writeEPUBParagraph(t, t.TempDir(), "One"))})
	singles := 0
	r, err := Ingest(ctx, IngestRequest{Library: root, Folder: folder, Apply: true, Scan: true, Scanner: incompleteBatch{&singles}})
	if err != nil || r.Checked != 1 || singles != 1 {
		t.Fatal("incomplete batch result reused", r, err, singles)
	}
}

// Regression: on a case-insensitive filesystem a pending member recorded
// under a differently cased archive path did not match its label.
func TestArchiveLabelsCompareArchiveByIdentityAndMemberExactly(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "Bundle.zip")
	if err := os.WriteFile(archive, []byte("zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "other.zip")
	if err := os.WriteFile(other, []byte("zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	label := archive + "!/books/One.epub"
	if !sameImportSource(label, label) || sameImportSource(label, archive+"!/books/one.epub") || sameImportSource(label, other+"!/books/One.epub") || sameImportSource(label, archive) {
		t.Fatal("label comparison")
	}
	folded := filepath.Join(dir, "BUNDLE.ZIP")
	if _, err := os.Stat(folded); err == nil && !sameImportSource(label, folded+"!/books/One.epub") {
		t.Fatal("case-insensitive archive path did not match its label")
	}
}

// An archive whose path contains the label separator is preserved, not
// imported under a label that would split in the wrong place.
func TestIngestRefusesArchivePathsWithTheLabelSeparator(t *testing.T) {
	if filepath.Separator != '/' {
		t.Skip("the label separator cannot occur in a Windows path")
	}
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	folder := t.TempDir()
	writeTree(t, folder, map[string][]byte{"odd!/a.zip": []byte("PK")})
	r, err := Ingest(ctx, IngestRequest{Library: root, Folder: folder, Apply: true})
	if err == nil || r.Failed != 1 || !strings.Contains(r.Items[0].Detail, "provenance") {
		t.Fatal("separator in archive path accepted", r, err)
	}
}
