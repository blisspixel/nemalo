package acquisition

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/scanner"
)

type scanFunc func(context.Context, string) scanner.Result

func (f scanFunc) Scan(ctx context.Context, p string) scanner.Result { return f(ctx, p) }

func status(s string) scanFunc {
	return func(context.Context, string) scanner.Result { return scanner.Result{Status: s, Tool: "test-scanner"} }
}

// withAlternatives adds an eligible EPUB, a restricted file, and an
// unsupported format beside the selected PDF.
func withAlternatives(p *fixtureProvider) {
	n := int64(10)
	md := md5.Sum([]byte("x"))
	sum := hex.EncodeToString(md[:])
	p.e.Creators = []string{"Mary \"Shelley\""}
	p.e.Title = []string{"Frankenstein: (1818)"}
	p.e.Files = append(p.e.Files,
		discovery.OfferedFile{Name: "book.epub", Bytes: &n, MD5: sum, Access: "public_file_candidate", DownloadURL: "https://archive.org/download/demo/book.epub"},
		discovery.OfferedFile{Name: "loan.epub", Bytes: &n, MD5: sum, Access: "restricted_or_uncertain"},
		discovery.OfferedFile{Name: "cover.jpg", Bytes: &n, MD5: sum, Access: "public_file_candidate", DownloadURL: "https://archive.org/download/demo/cover.jpg"},
	)
}

func readReceipt(t *testing.T, dir string) Result {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "receipt.json"))
	if err != nil {
		t.Fatal(err)
	}
	var r Result
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAcquireScansExactIntakeBytes(t *testing.T) {
	p, r := fixture(), request(t)
	r.Quarantine = filepath.Join(t.TempDir(), "quarantine")
	calls := 0
	r.Scanner = scanFunc(func(_ context.Context, path string) scanner.Result {
		calls++
		got, err := os.ReadFile(path)
		if err != nil || string(got) != p.body || filepath.Base(path) != "content.pdf" {
			t.Fatal("scanner did not receive the intake bytes", path, err)
		}
		return scanner.Result{Status: "no_detections_reported", Tool: "clamscan"}
	})
	result, err := Acquire(context.Background(), p, r)
	if err != nil || !result.Complete || result.Status != "untrusted_intake" || result.Antivirus != "no_detections_reported" || result.Scan == nil || calls != 1 {
		t.Fatal(result, err)
	}
	receipt := readReceipt(t, r.Output)
	if receipt.Antivirus != "no_detections_reported" || receipt.Scan == nil || receipt.Scan.Tool != "clamscan" || receipt.Transfer.SHA256 != result.Transfer.SHA256 {
		t.Fatal("scan evidence not recorded in the receipt", receipt)
	}
	if _, err := os.Stat(r.Quarantine); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("quarantine created without a detection", err)
	}
}

func TestAcquireScanGapsKeepIntakeAndOptOutSkipsScanner(t *testing.T) {
	for _, s := range []string{"unavailable", "incomplete"} {
		p, r := fixture(), request(t)
		r.Quarantine, r.Scanner = filepath.Join(t.TempDir(), "q"), status(s)
		result, err := Acquire(context.Background(), p, r)
		if err != nil || !result.Complete || result.Status != "untrusted_intake" || result.Antivirus != s {
			t.Fatal("a scan gap must not block or bless intake", s, result, err)
		}
	}
	p, r := fixture(), request(t)
	r.NoScan, r.Quarantine = true, filepath.Join(t.TempDir(), "q")
	r.Scanner = scanFunc(func(context.Context, string) scanner.Result {
		t.Fatal("--no-scan invoked the scanner")
		return scanner.Result{}
	})
	result, err := Acquire(context.Background(), p, r)
	if err != nil || result.Antivirus != "not_scanned" || result.Scan != nil {
		t.Fatal(result, err)
	}
}

func TestAcquireDetectionQuarantinesAndOffersAlternatives(t *testing.T) {
	p, r := fixture(), request(t)
	withAlternatives(p)
	r.Quarantine, r.Scanner = filepath.Join(t.TempDir(), "nemalo", "quarantine"), status("findings")
	result, err := Acquire(context.Background(), p, r)
	if !errors.Is(err, ErrDetected) || result.Complete || result.Status != "detected_quarantined" || result.Antivirus != "findings" || result.ContentPath != "" {
		t.Fatal(result, err)
	}
	if _, err := os.Stat(filepath.Join(r.Output, "content.pdf")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("detected bytes left in the intake packet", err)
	}
	got, err := os.ReadFile(result.QuarantinedPath)
	if err != nil || string(got) != p.body || !strings.HasSuffix(result.QuarantinedPath, "content.pdf.quarantined") || !strings.HasPrefix(result.QuarantinedPath, r.Quarantine) {
		t.Fatal("quarantined bytes", result.QuarantinedPath, err)
	}
	if len(result.Alternatives) != 1 || result.Alternatives[0].File != "book.epub" || result.Alternatives[0].Format != "epub" {
		t.Fatal("alternatives must list only eligible supported files", result.Alternatives)
	}
	if result.SearchSuggestion != "Frankenstein 1818 Mary Shelley" {
		t.Fatalf("search suggestion %q", result.SearchSuggestion)
	}
	receipt := readReceipt(t, r.Output)
	if receipt.Status != "detected_quarantined" || receipt.Complete || receipt.QuarantinedPath != result.QuarantinedPath {
		t.Fatal("receipt does not record the detection", receipt)
	}
}

func TestAcquireDetectionCanDeleteInstead(t *testing.T) {
	p, r := fixture(), request(t)
	r.OnDetection, r.Scanner = "delete", status("findings")
	result, err := Acquire(context.Background(), p, r)
	if !errors.Is(err, ErrDetected) || result.Status != "detected_deleted" || result.QuarantinedPath != "" {
		t.Fatal(result, err)
	}
	if _, err := os.Stat(filepath.Join(r.Output, "content.pdf")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("detected bytes not deleted", err)
	}
	if readReceipt(t, r.Output).Status != "detected_deleted" {
		t.Fatal("receipt does not record the deletion")
	}
}

func TestAcquireDetectionFailuresNeverBecomeIntake(t *testing.T) {
	// The quarantine root is a regular file, so the move cannot happen.
	p, r := fixture(), request(t)
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	r.Quarantine, r.Scanner = blocker, status("findings")
	result, err := Acquire(context.Background(), p, r)
	if !errors.Is(err, ErrDetected) || result.Status != "detected_retained" || result.Complete {
		t.Fatal(result, err)
	}
	if readReceipt(t, r.Output).Status != "detected_retained" {
		t.Fatal("a retained detection must still be recorded")
	}
	// Host antivirus removing the file during the scan is not a clean result.
	p, r = fixture(), request(t)
	r.Quarantine = filepath.Join(t.TempDir(), "q")
	r.Scanner = scanFunc(func(_ context.Context, path string) scanner.Result {
		if err := os.Remove(path); err != nil {
			t.Skip("open files cannot be removed on this platform", err)
		}
		return scanner.Result{Status: "no_detections_reported"}
	})
	result, err = Acquire(context.Background(), p, r)
	if err == nil || result.Complete || result.Antivirus != "incomplete" {
		t.Fatal("vanished intake reported clean", result, err)
	}
	if _, err := os.Stat(filepath.Join(r.Output, "receipt.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("receipt published for removed bytes", err)
	}
}

func TestAcquireScanRequiresAQuarantineOrDelete(t *testing.T) {
	p, r := fixture(), request(t)
	r.Scanner = status("no_detections_reported")
	if _, err := Acquire(context.Background(), p, r); err == nil || p.evaluations != 0 {
		t.Fatal("scan without a detection destination accepted", err)
	}
	for _, bad := range []Request{{OnDetection: "ignore"}, {Quarantine: "relative/q"}} {
		req := request(t)
		req.OnDetection, req.Quarantine = bad.OnDetection, bad.Quarantine
		if req.Validate() == nil {
			t.Fatal("invalid detection policy accepted", bad)
		}
	}
}

func TestCleanScanAfterTransferDeadlineKeepsTheReceipt(t *testing.T) {
	// The deadline leaves a slow runner ample time for the local transfer; the
	// scan then ends only after the deadline has certainly passed.
	transferDeadline, transferRate = 2*time.Second, 1<<40
	t.Cleanup(func() { transferDeadline, transferRate = 5*time.Minute, 512<<10 })
	p, r := fixture(), request(t)
	r.Quarantine = filepath.Join(t.TempDir(), "q")
	started := time.Now()
	r.Scanner = scanFunc(func(context.Context, string) scanner.Result {
		time.Sleep(time.Until(started.Add(transferDeadline + 200*time.Millisecond)))
		return scanner.Result{Status: "no_detections_reported"}
	})
	result, err := Acquire(context.Background(), p, r)
	if err != nil || !result.Complete || result.Antivirus != "no_detections_reported" {
		t.Fatal("a clean scan after the transfer deadline lost the receipt", result, err)
	}
	if readReceipt(t, r.Output).Status != "untrusted_intake" {
		t.Fatal("receipt missing")
	}
	ctx, cancel := context.WithCancel(context.Background())
	p, r = fixture(), request(t)
	r.Quarantine = filepath.Join(t.TempDir(), "q")
	r.Scanner = scanFunc(func(context.Context, string) scanner.Result {
		cancel() // The caller's own cancellation still stops publication.
		return scanner.Result{Status: "no_detections_reported"}
	})
	if result, err := Acquire(ctx, p, r); err == nil || result.Complete {
		t.Fatal("cancelled acquisition published a receipt", result, err)
	}
}

func TestDetectionSurvivesHostAntivirusActing(t *testing.T) {
	p, r := fixture(), request(t)
	withAlternatives(p)
	r.Quarantine = filepath.Join(t.TempDir(), "q")
	r.Scanner = scanFunc(func(_ context.Context, path string) scanner.Result {
		if err := os.Remove(path); err != nil {
			t.Skip("open files cannot be removed on this platform", err)
		}
		return scanner.Result{Status: "findings"}
	})
	result, err := Acquire(context.Background(), p, r)
	if !errors.Is(err, ErrDetected) || result.Status != "detected_changed_during_scan" || result.Antivirus != "findings" || len(result.Alternatives) != 1 {
		t.Fatal("a confirmed detection was downgraded", result, err)
	}
	if got := readReceipt(t, r.Output); got.Status != "detected_changed_during_scan" || got.Antivirus != "findings" {
		t.Fatal("detection not recorded", got)
	}
}

func TestTransferBudgetGrowsWithTheBytes(t *testing.T) {
	if transferBudget(0) != 5*time.Minute || transferBudget(2<<30) != 5*time.Minute+4096*time.Second || transferBudget(-1) != 5*time.Minute {
		t.Fatal(transferBudget(0), transferBudget(2<<30))
	}
}

// Each file is held to its own format's limit: a 300 MiB M4B is eligible
// under the widest budget and an EPUB of that size is not.
func TestEligibilityUsesEachFormatsLimit(t *testing.T) {
	big := int64(300 << 20)
	file := func(name string) discovery.OfferedFile {
		return discovery.OfferedFile{Name: name, Bytes: &big, MD5: strings.Repeat("a", 32), Access: "public_file_candidate", DownloadURL: "https://archive.org/download/demo/" + name}
	}
	e := discovery.Evaluation{Complete: true, ID: "archive:demo", Source: "archive", Access: "no_item_restriction_declared", Files: []discovery.OfferedFile{file("book.m4b"), file("book.epub")}}
	got := Eligible(e, assessment.MaxMP4Bytes)
	if len(got) != 1 || got[0].File != "book.m4b" {
		t.Fatal(got)
	}
	if got := Eligible(e, 256<<20); len(got) != 0 {
		t.Fatal("a lower budget admitted a larger file", got)
	}
	if DefaultMaxBytes("book.m4b") != assessment.MaxMP4Bytes || DefaultMaxBytes("1342.epub.noimages") != assessment.MaxParsedBytes {
		t.Fatal("default budgets")
	}
}

// While one acquisition writes an output folder, another into the same folder
// is refused before any request, and runs once the first is done.
func TestOneAcquisitionWritesAnOutputAtATime(t *testing.T) {
	p, r := fixture(), request(t)
	release, err := lockOutput(r.Output)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(context.Background(), p, r); err == nil || !strings.Contains(err.Error(), "another acquisition") || p.evaluations != 0 {
		t.Fatal("a second acquisition ran", err, p.evaluations)
	}
	release()
	if result, err := Acquire(context.Background(), p, r); err != nil || !result.Complete {
		t.Fatal(result, err)
	}
}

// An arXiv acquisition names an explicit version and its PDF; its file needs
// a declared length within budget and no checksum, which arXiv does not give.
func TestArxivAcquisitionNeedsAnExplicitVersion(t *testing.T) {
	out := filepath.Join(t.TempDir(), "p")
	for name, r := range map[string]Request{
		"unversioned": {SourceID: "arxiv:1706.03762", File: "1706.03762v7.pdf", Output: out, MaxBytes: 1 << 20},
		"wrong name":  {SourceID: "arxiv:1706.03762v7", File: "1706.03762v6.pdf", Output: out, MaxBytes: 1 << 20},
		"not a paper": {SourceID: "arxiv:nope", File: "nope.pdf", Output: out, MaxBytes: 1 << 20},
	} {
		if r.Validate() == nil {
			t.Error(name, "accepted")
		}
	}
	r := Request{SourceID: "arxiv:1706.03762v7", File: "1706.03762v7.pdf", Output: out, MaxBytes: 100}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	n := int64(14)
	e := discovery.Evaluation{Complete: true, ID: r.SourceID, Source: "arxiv", Access: discovery.ArxivAccess, Files: []discovery.OfferedFile{{Name: r.File, Format: "PDF", Bytes: &n, Access: discovery.ArxivPDFAccess, DownloadURL: "https://export.arxiv.org/pdf/1706.03762v7"}}}
	if _, err := selectFile(e, r); err != nil {
		t.Fatal(err)
	}
	e.Files[0].Bytes = nil
	if _, err := selectFile(e, r); err == nil {
		t.Fatal("a PDF without a declared length was selected")
	}
	big := int64(1000)
	e.Files[0].Bytes = &big
	if _, err := selectFile(e, r); err == nil {
		t.Fatal("a PDF over budget was selected")
	}
}
