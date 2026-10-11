package library

import (
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
	"time"

	"github.com/blisspixel/nemalo/internal/scanner"
)

// batchScan records each batch and reports a detection for marked snapshots.
type batchScan struct {
	batches *[]int
	detect  func(path string) bool
}

func (b batchScan) Scan(ctx context.Context, path string) scanner.Result {
	return b.ScanMany(ctx, []string{path})[0]
}

func (b batchScan) ScanMany(_ context.Context, files []string) []scanner.Result {
	*b.batches = append(*b.batches, len(files))
	out := make([]scanner.Result, len(files))
	for i, f := range files {
		out[i] = scanner.Result{Status: "no_detections_reported"}
		data, err := os.ReadFile(f)
		if err == nil && b.detect != nil && b.detect(string(data)) {
			out[i].Status = "findings"
		}
	}
	return out
}

func importUnscanned(t *testing.T, root string, paragraphs ...string) []ImportResult {
	t.Helper()
	results := []ImportResult{}
	for _, p := range paragraphs {
		r, err := Import(context.Background(), ImportRequest{Library: root, Source: writeEPUBParagraph(t, t.TempDir(), p), Apply: true})
		if err != nil || r.Status != "review" || r.Reason != "unscanned" {
			t.Fatal(r, err)
		}
		results = append(results, r)
	}
	return results
}

func journals(t *testing.T, root string) []byte {
	t.Helper()
	return append(mustRead(t, filepath.Join(root, ".nemalo", operationsName)), mustRead(t, filepath.Join(root, ".nemalo", holdingsName))...)
}

func TestRescanPromotesUnscannedHoldingsInOneBatch(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	books := importUnscanned(t, root, "Alpha", "Beta", "Gamma")
	pdf := filepath.Join(t.TempDir(), "paper.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF-1.7\n%%EOF"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Import(ctx, ImportRequest{Library: root, Source: pdf, Apply: true}); err != nil {
		t.Fatal(err)
	}
	// "Beta" is detected; EPUB bytes are stored uncompressed in this fixture.
	var batches []int
	av := batchScan{&batches, func(data string) bool { return strings.Contains(data, "Beta") }}
	before := journals(t, root)
	preview, err := Rescan(ctx, RescanRequest{Library: root, Scanner: av})
	if err != nil || preview.Applied || len(preview.Scanned) != 3 || preview.Promoted != 2 || preview.Detections != 1 || len(preview.Skipped) != 1 || preview.Skipped[0].Format != "pdf" {
		t.Fatal(preview, err)
	}
	if len(batches) != 1 || batches[0] != 3 {
		t.Fatal("expected one scanner batch of three files", batches)
	}
	if !bytes.Equal(before, journals(t, root)) {
		t.Fatal("dry run wrote library journals")
	}
	applied, err := Rescan(ctx, RescanRequest{Library: root, Apply: true, Scanner: av})
	if err != nil || !applied.Applied || applied.Promoted != 2 || applied.Detections != 1 {
		t.Fatal(applied, err)
	}
	statuses := map[string]string{}
	for _, item := range applied.Scanned {
		if !item.Changed {
			t.Fatal("status change not recorded", item)
		}
		statuses[item.AssetID] = item.After + "/" + item.AfterReason
	}
	if statuses[books[0].AssetID] != "checked/checks_and_scan_recorded" || statuses[books[1].AssetID] != "review/scan_findings" {
		t.Fatal(statuses)
	}
	if state, err := LibraryStatus(ctx, root); err != nil || state.Status != "initialized" {
		t.Fatal("rescan left an invalid journal", state, err)
	}
	audit, err := AuditManaged(ctx, root)
	if err == nil || audit.Matching != 2 {
		t.Fatal("expected two checked holdings and review findings", audit, err)
	}
	for _, b := range books {
		got := mustRead(t, filepath.Join(root, filepath.FromSlash(b.Path)))
		if sum := sha256.Sum256(got); hex.EncodeToString(sum[:]) != b.SHA256 {
			t.Fatal("rescan changed stored bytes")
		}
	}
	// The detection is not rescanned automatically; nothing else remains.
	batches = nil
	again, err := Rescan(ctx, RescanRequest{Library: root, Apply: true, Scanner: av})
	if err != nil || len(again.Scanned) != 0 || len(batches) != 0 || len(again.Skipped) != 2 {
		t.Fatal(again, batches, err)
	}
	for _, s := range again.Skipped {
		if s.AssetID == books[1].AssetID && !strings.Contains(s.Detail, "detection") {
			t.Fatal("previous detection not explained", s)
		}
	}
}

func TestRescanScanGapsAndChangedBytesRecordNothingFalse(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	books := importUnscanned(t, root, "One", "Two")
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(books[1].Path)), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := Rescan(ctx, RescanRequest{Library: root, Apply: true, Scanner: statusScan{"unavailable"}})
	if err != nil || len(r.Scanned) != 2 || r.Promoted != 0 || r.Incomplete != 2 {
		t.Fatal(r, err)
	}
	for _, item := range r.Scanned {
		switch item.AssetID {
		case books[0].AssetID:
			if item.After != "review" || item.AfterReason != "scan_unavailable" || !item.Changed {
				t.Fatal("scan gap not named", item)
			}
		case books[1].AssetID:
			if item.After != "" || item.Changed || !strings.Contains(item.Detail, "library audit") {
				t.Fatal("changed bytes received a status", item)
			}
		}
	}
	if _, err := LibraryStatus(ctx, root); err != nil {
		t.Fatal(err)
	}
}

func TestRescanRollsForwardAnInterruptedIntent(t *testing.T) {
	for _, holdingWritten := range []bool{false, true} {
		ctx := context.Background()
		root := t.TempDir()
		if _, err := Initialize(ctx, root); err != nil {
			t.Fatal(err)
		}
		book := importUnscanned(t, root, "Crash")[0]
		control, _, err := openControl(root, true)
		if err != nil {
			t.Fatal(err)
		}
		events, lines, err := lockedManaged(control, book.LibraryID)
		if err != nil {
			t.Fatal(err)
		}
		h := latestHoldings(lines)[book.AssetID]
		intent := operation{SchemaVersion: 1, Sequence: len(events) + 1, LibraryID: h.LibraryID, OperationID: newStateID("operation"), Event: "rescan_intent", Source: h.Path, AssetID: h.ID, Format: h.Format, Bytes: h.Bytes, Status: "checked", Reason: "checks_and_scan_recorded", At: time.Now().UTC().Format(time.RFC3339Nano)}
		if err := appendCanonical(ctx, control, operationsName, maxOperations, intent); err != nil {
			t.Fatal(err)
		}
		if holdingWritten {
			next := h
			next.Status, next.Reason = intent.Status, intent.Reason
			if err := appendCanonical(ctx, control, holdingsName, maxHoldings, next); err != nil {
				t.Fatal(err)
			}
		}
		if err := control.close(); err != nil {
			t.Fatal(err)
		}
		// A pending rescan is valid state, not corruption.
		if _, err := LibraryStatus(ctx, root); err != nil {
			t.Fatal(err)
		}
		// The next writer, here an unrelated import, completes it first.
		if _, err := Import(ctx, ImportRequest{Library: root, Source: writeEPUBParagraph(t, t.TempDir(), "Other"), Apply: true}); err != nil {
			t.Fatal(err)
		}
		events, lines, err = func() ([]operation, []Holding, error) {
			c, _, err := openControl(root, false)
			if err != nil {
				return nil, nil, err
			}
			defer c.close()
			return lockedManaged(c, book.LibraryID)
		}()
		if err != nil {
			t.Fatal(err)
		}
		if got := latestHoldings(lines)[book.AssetID]; got.Status != "checked" {
			t.Fatal("interrupted rescan not completed", got)
		}
		completed := 0
		for _, e := range events {
			if e.Event == "rescan_completed" && e.AssetID == book.AssetID {
				completed++
			}
		}
		if completed != 1 {
			t.Fatal("expected exactly one rescan completion", completed)
		}
	}
}

func TestRescanRefusesPendingImportAndInvalidJournalKinds(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	source := writeEPUB(t, t.TempDir())
	if _, err := importLibrary(ctx, ImportRequest{Library: root, Source: source, Apply: true}, func(stage string) error {
		if stage == "intent" {
			return errors.New("crash")
		}
		return nil
	}); err == nil {
		t.Fatal("staged crash ignored")
	}
	if _, err := Rescan(ctx, RescanRequest{Library: root, Apply: true, Scanner: cleanScan{}}); err == nil || !strings.Contains(err.Error(), "pending") {
		t.Fatal("rescan ignored a pending import", err)
	}
	if _, err := Rescan(ctx, RescanRequest{Library: t.TempDir()}); err == nil {
		t.Fatal("uninitialized library accepted")
	}
	if _, err := Rescan(ctx, RescanRequest{}); err == nil {
		t.Fatal("empty library path accepted")
	}
	base := operation{SchemaVersion: 1, Sequence: 1, LibraryID: "library:x", OperationID: newStateID("operation"), Event: "rescan_intent", Source: "assets/sha256/aa/x.epub", AssetID: "sha256:" + strings.Repeat("a", 64), Format: "epub", Bytes: 1, Status: "review", Reason: "unscanned", At: "t"}
	wrong := base
	wrong.Sequence, wrong.Event = 2, "import_completed"
	var buf bytes.Buffer
	for _, e := range []operation{base, wrong} {
		b, _ := json.Marshal(e)
		buf.Write(append(b, '\n'))
	}
	if _, err := parseOperations(buf.Bytes(), "library:x"); !errors.Is(err, ErrStateReview) {
		t.Fatal("mismatched completion kind accepted", err)
	}
	b, _ := json.Marshal(base)
	events, err := parseOperations(append(b, '\n'), "library:x")
	if err != nil {
		t.Fatal(err)
	}
	// A pending rescan must refer to an existing holding.
	if err := consistent(events, nil); !errors.Is(err, ErrStateReview) {
		t.Fatal("rescan of an unknown holding accepted", err)
	}
}

func TestImportUsesBoundIntakeReceiptScan(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	count := 0
	packet := func(antivirus string, scan *scanner.Result, hash string) string {
		dir := t.TempDir()
		count++ // Distinct bytes per packet, so no earlier holding status carries over.
		if err := os.Rename(writeEPUBParagraph(t, dir, "Receipt "+strings.Repeat("x", count)), filepath.Join(dir, "content.epub")); err != nil {
			t.Fatal(err)
		}
		body := mustRead(t, filepath.Join(dir, "content.epub"))
		sum := sha256.Sum256(body)
		if hash == "" {
			hash = hex.EncodeToString(sum[:])
		}
		receipt := map[string]any{"complete": true, "status": "untrusted_intake", "antivirus": antivirus, "scan": scan, "transfer": map[string]any{"bytes": len(body), "sha256": hash}}
		b, _ := json.Marshal(receipt)
		if err := os.WriteFile(filepath.Join(dir, "receipt.json"), b, 0600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	clean := &scanner.Result{Status: "no_detections_reported", Tool: "clamscan", Output: "x: OK"}
	r, err := Import(ctx, ImportRequest{Library: root, Source: packet("no_detections_reported", clean, ""), Apply: true})
	if err != nil || r.Status != "checked" || r.Reason != "checks_and_scan_recorded" || r.Antivirus != "no_detections_reported" {
		t.Fatal("bound intake scan not accepted", r, err)
	}
	for _, tc := range []struct {
		antivirus string
		scan      *scanner.Result
		want      string
	}{
		{"not_scanned", nil, "unscanned"},
		{"no_detections_reported", nil, "unscanned"},
		{"no_detections_reported", &scanner.Result{Status: "incomplete"}, "unscanned"},
		{"incomplete", clean, "unscanned"},
	} {
		r, err := Import(ctx, ImportRequest{Library: root, Source: packet(tc.antivirus, tc.scan, ""), Apply: true})
		if err != nil || r.Status != "review" || r.Reason != tc.want {
			t.Fatal("unbound or unclean receipt scan accepted", tc, r, err)
		}
	}
	if _, err := Import(ctx, ImportRequest{Library: root, Source: packet("findings", &scanner.Result{Status: "findings"}, "")}); err == nil {
		t.Fatal("packet with a detection accepted")
	}
	if _, err := Import(ctx, ImportRequest{Library: root, Source: packet("no_detections_reported", clean, strings.Repeat("ab", 32))}); err == nil {
		t.Fatal("receipt scan for other bytes accepted")
	}
}

// Regression: a scan upgrade of an already held EPUB that stopped after its new
// holding line, before completion, was reported as corrupt state.
func TestImportUpgradeCrashAfterHoldingLineResumes(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	source := writeEPUB(t, t.TempDir())
	if r, err := Import(ctx, ImportRequest{Library: root, Source: source, Apply: true}); err != nil || r.Status != "review" {
		t.Fatal(r, err)
	}
	_, err := importLibrary(ctx, ImportRequest{Library: root, Source: source, Apply: true, Scan: true, Scanner: cleanScan{}}, func(stage string) error {
		if stage == "recorded" {
			return errors.New("crash")
		}
		return nil
	})
	if err == nil {
		t.Fatal("staged crash ignored")
	}
	if _, err := LibraryStatus(ctx, root); err != nil {
		t.Fatal("interrupted upgrade reported as corrupt", err)
	}
	r, err := Import(ctx, ImportRequest{Library: root, Source: source, Apply: true})
	if err != nil || !r.Resumed || r.Status != "checked" {
		t.Fatal(r, err)
	}
	if _, err := AuditManaged(ctx, root); err != nil {
		t.Fatal(err)
	}
}

// Regression: an older clean receipt scan must never replace a later
// detection, and may only fill a holding that has no scan evidence.
func TestReceiptScanNeverReplacesLaterScanEvidence(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Rename(writeEPUBParagraph(t, dir, "Evidence order"), filepath.Join(dir, "content.epub")); err != nil {
		t.Fatal(err)
	}
	body := mustRead(t, filepath.Join(dir, "content.epub"))
	sum := sha256.Sum256(body)
	receipt, _ := json.Marshal(map[string]any{"complete": true, "status": "untrusted_intake", "antivirus": "no_detections_reported", "scan": scanner.Result{Status: "no_detections_reported"}, "transfer": map[string]any{"bytes": len(body), "sha256": hex.EncodeToString(sum[:])}})
	if err := os.WriteFile(filepath.Join(dir, "receipt.json"), receipt, 0600); err != nil {
		t.Fatal(err)
	}
	// The same bytes held first without scan evidence: the receipt fills it.
	plain := filepath.Join(t.TempDir(), "book.epub")
	if err := os.WriteFile(plain, body, 0600); err != nil {
		t.Fatal(err)
	}
	if r, err := Import(ctx, ImportRequest{Library: root, Source: plain, Apply: true}); err != nil || r.Reason != "unscanned" {
		t.Fatal(r, err)
	}
	if r, err := Import(ctx, ImportRequest{Library: root, Source: dir, Apply: true}); err != nil || r.Status != "checked" {
		t.Fatal("receipt scan did not fill missing scan evidence", r, err)
	}
	// A later live scan reports a detection.
	if r, err := Import(ctx, ImportRequest{Library: root, Source: dir, Apply: true, Scan: true, Scanner: statusScan{"findings"}}); err != nil || r.Reason != "scan_findings" {
		t.Fatal(r, err)
	}
	// Re-importing the packet without --scan keeps the detection.
	r, err := Import(ctx, ImportRequest{Library: root, Source: dir, Apply: true})
	if err != nil || r.Status != "review" || r.Reason != "scan_findings" {
		t.Fatal("older receipt evidence replaced a detection", r, err)
	}
}

func TestPendingRescanMustDescribeItsHolding(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	book := importUnscanned(t, root, "Identity")[0]
	control, _, err := openControl(root, true)
	if err != nil {
		t.Fatal(err)
	}
	events, lines, err := lockedManaged(control, book.LibraryID)
	if err != nil {
		t.Fatal(err)
	}
	h := latestHoldings(lines)[book.AssetID]
	intent := operation{SchemaVersion: 1, Sequence: len(events) + 1, LibraryID: h.LibraryID, OperationID: newStateID("operation"), Event: "rescan_intent", Source: h.Path, AssetID: h.ID, Format: h.Format, Bytes: h.Bytes + 1, Status: "checked", Reason: "checks_and_scan_recorded", At: "t"}
	if err := consistent(append(events, intent), lines); !errors.Is(err, ErrStateReview) {
		t.Fatal("pending rescan with other bytes accepted", err)
	}
	if _, _, err := rollForwardRescan(ctx, control, append(events, intent), lines); !errors.Is(err, ErrStateReview) {
		t.Fatal("roll-forward wrote for a mismatched intent", err)
	}
	intent.Bytes, intent.Source = h.Bytes, "assets/sha256/00/other.epub"
	if err := consistent(append(events, intent), lines); !errors.Is(err, ErrStateReview) {
		t.Fatal("pending rescan for another path accepted", err)
	}
	if err := control.close(); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, filepath.Join(root, ".nemalo", operationsName)); bytes.Contains(got, []byte("rescan")) {
		t.Fatal("a mismatched intent was written")
	}
}
