package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/acquisition"
	"github.com/blisspixel/nemalo/internal/config"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/scanner"
)

type catalog struct{}

func (catalog) Search(_ context.Context, q string, limit, offset int) (discovery.Page, error) {
	return discovery.Page{Query: q, Offset: offset, Total: limit}, nil
}

type evaluator struct{ catalog }

func (evaluator) Evaluate(_ context.Context, id string) (discovery.Evaluation, error) {
	return discovery.Evaluation{ID: id}, nil
}

func TestProviderRegistryAndEvaluation(t *testing.T) {
	s := Service{Providers: map[string]Searcher{"archive": evaluator{}, "openlibrary": catalog{}}}
	if p, err := s.SearchSource(context.Background(), "archive", "books", 2, 1); err != nil || p.Query != "books" {
		t.Fatal(p, err)
	}
	if _, err := s.SearchSource(context.Background(), "missing", "books", 2, 1); err == nil {
		t.Fatal("unavailable provider accepted")
	}
	if e, err := s.Evaluate(context.Background(), "archive:demo"); err != nil || e.ID != "archive:demo" {
		t.Fatal(e, err)
	}
	for _, id := range []string{"demo", "openlibrary:OL1W", "missing:id"} {
		if _, err := s.Evaluate(context.Background(), id); err == nil {
			t.Fatal("unsupported evaluation accepted", id)
		}
	}
}

func TestSharedServices(t *testing.T) {
	s := Service{Providers: map[string]Searcher{"openlibrary": catalog{}}}
	p, err := s.Search(context.Background(), "books", 4, 3)
	if err != nil || p.Query != "books" || p.Total != 4 || p.Offset != 3 {
		t.Fatal(p, err)
	}
	r, err := s.Inspect(context.Background(), t.TempDir(), inventory.Defaults())
	if err != nil || !r.Complete {
		t.Fatal(r, err)
	}
	d := s.Doctor(config.Paths{State: "state"}, config.Config{Library: "library"}, func(name string) (string, error) {
		if name == "7z" {
			return "7z", nil
		}
		return "", errors.New("not installed")
	})
	if d.Security != "not_scanned" || len(d.Capabilities) != 5 || d.Capabilities[0].Available || !d.Capabilities[2].Available {
		t.Fatalf("tool availability became scan coverage: %+v", d)
	}
	bridge := d.Capabilities[4]
	if bridge.Name != "descriptor_bridge" || !bridge.Available {
		t.Fatal(bridge)
	}
	if d.ScannerRecommendation == "" || d.ChecksumRecommendation == "" || !strings.Contains(d.ChecksumRecommendation, "does not prove") {
		t.Fatal(d.ScannerRecommendation, d.ChecksumRecommendation)
	}
	switch runtime.GOOS {
	case "windows":
		if d.Capabilities[0].Recommended || !d.Capabilities[1].Recommended || !strings.Contains(d.ScannerRecommendation, "Microsoft Defender") {
			t.Fatal(d.Capabilities[:2], d.ScannerRecommendation)
		}
	case "darwin":
		if !d.Capabilities[0].Recommended || d.Capabilities[1].Recommended || !strings.Contains(d.ScannerRecommendation, "macOS does not provide") {
			t.Fatal(d.Capabilities[:2], d.ScannerRecommendation)
		}
	default:
		if !d.Capabilities[0].Recommended || d.Capabilities[1].Recommended || !strings.Contains(d.ScannerRecommendation, "including Omarchy") {
			t.Fatal(d.Capabilities[:2], d.ScannerRecommendation)
		}
	}
	if d.Capabilities[2].Recommended || d.Capabilities[3].Recommended {
		t.Fatal(d.Capabilities)
	}
	if New().Providers["openlibrary"] == nil {
		t.Fatal("missing catalog")
	}
	if _, err := Lookup("nemalo-nonexistent-fixture-tool"); err == nil {
		t.Fatal("unexpected tool")
	}
	_, _ = Lookup("MpCmdRun.exe")
}

type orderedCatalog struct{ catalog }

func (orderedCatalog) SearchOrdered(_ context.Context, q, order string, limit, offset int) (discovery.Page, error) {
	return discovery.Page{Query: q, Order: order, Offset: offset, Total: limit}, nil
}

func TestSearchOrderRouting(t *testing.T) {
	s := Service{Providers: map[string]Searcher{"archive": orderedCatalog{}, "openlibrary": catalog{}}}
	for _, order := range []string{"", discovery.OrderRelevance} {
		if p, err := s.SearchSourceOrdered(context.Background(), "openlibrary", "books", order, 2, 0); err != nil || p.Query != "books" {
			t.Fatal(order, p, err)
		}
	}
	if p, err := s.SearchSourceOrdered(context.Background(), "archive", "books", discovery.OrderIdentifier, 2, 0); err != nil || p.Order != discovery.OrderIdentifier {
		t.Fatal(p, err)
	}
	// A provider without another order must fail, not silently use relevance.
	if _, err := s.SearchSourceOrdered(context.Background(), "openlibrary", "books", discovery.OrderIdentifier, 2, 0); err == nil {
		t.Fatal("unsupported order silently accepted")
	}
	if _, err := s.SearchSourceOrdered(context.Background(), "missing", "books", discovery.OrderRelevance, 2, 0); err == nil {
		t.Fatal("unavailable provider accepted")
	}
}

type kindScanner struct {
	kind string
	err  error
}

func (k kindScanner) Scan(context.Context, string) scanner.Result { return scanner.Result{} }
func (k kindScanner) Kind() (string, error)                       { return k.kind, k.err }

func TestScanGuidanceNamesOneNextStep(t *testing.T) {
	installed := Service{Scanner: kindScanner{kind: "clamav"}}
	missing := Service{Scanner: kindScanner{err: errors.New("missing")}}
	for _, tc := range []struct {
		s         Service
		antivirus string
		requested bool
		want      string
	}{
		{installed, "not_scanned", false, "Add --scan to scan this file with ClamAV."},
		{Service{Scanner: kindScanner{kind: "defender"}}, "not_scanned", false, "Microsoft Defender"},
		{missing, "not_scanned", false, "Install"},
		{installed, "unavailable", true, "Install"},
		{installed, "incomplete", true, "did not complete"},
		{installed, "no_detections_reported", true, ""},
		{installed, "findings", true, ""},
		{installed, "not_scanned", true, ""},
	} {
		got := tc.s.ScanGuidance(tc.antivirus, tc.requested)
		if runtime.GOOS == "windows" && strings.HasPrefix(tc.want, "Install") {
			tc.want = "Microsoft Defender"
		}
		if (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
			t.Fatalf("%s/%t: %q", tc.antivirus, tc.requested, got)
		}
	}
}

func TestDoctorReportsSignaturesOrSetup(t *testing.T) {
	defaultScanner := "clamscan"
	if runtime.GOOS == "windows" {
		defaultScanner = "MpCmdRun.exe"
	}
	sig := func() scanner.Signatures { return scanner.Signatures{Tool: "test", Status: "stale"} }
	with := Service{}.doctor(config.Paths{}, config.Config{}, func(name string) (string, error) {
		if name == defaultScanner {
			return name, nil
		}
		return "", errors.New("missing")
	}, sig)
	if with.Signatures == nil || with.Signatures.Status != "stale" || with.ScannerSetup != "" {
		t.Fatal("installed scanner without signature evidence", with)
	}
	without := Service{}.doctor(config.Paths{}, config.Config{}, func(string) (string, error) { return "", errors.New("missing") }, func() scanner.Signatures {
		t.Fatal("signatures read without a scanner")
		return scanner.Signatures{}
	})
	if without.Signatures != nil || without.ScannerSetup == "" {
		t.Fatal("missing scanner without setup guidance", without)
	}
}

func TestAcquireAndRescanUseServiceScannerAndQuarantine(t *testing.T) {
	s := Service{Providers: map[string]Searcher{"archive": catalog{}}}
	if _, err := s.Acquire(context.Background(), acquisition.Request{}); err == nil {
		t.Fatal("provider without acquisition accepted")
	}
	if _, err := (Service{Scanner: kindScanner{kind: "clamav"}}).Rescan(context.Background(), library.RescanRequest{Library: t.TempDir()}); err == nil {
		t.Fatal("rescan of an uninitialized library accepted")
	}
}

type detectingProvider struct{}

func (detectingProvider) Search(context.Context, string, int, int) (discovery.Page, error) {
	return discovery.Page{}, nil
}
func (detectingProvider) Evaluate(context.Context, string) (discovery.Evaluation, error) {
	return discovery.Evaluation{}, errors.New("stop before any request")
}
func (detectingProvider) Download(context.Context, discovery.OfferedFile) (*http.Response, error) {
	return nil, errors.New("unreachable")
}

func TestAcquireAppliesServiceDetectionPolicy(t *testing.T) {
	s := Service{Providers: map[string]Searcher{"archive": detectingProvider{}}, Scanner: kindScanner{kind: "clamav"}, OnDetection: "delete"}
	// With "delete" no quarantine directory is needed, so validation passes and
	// the provider is reached; without it the request is refused first.
	_, err := s.Acquire(context.Background(), acquisition.Request{SourceID: "archive:demo", File: "a.epub", Output: filepath.Join(t.TempDir(), "p"), MaxBytes: 10})
	if err == nil || !strings.Contains(err.Error(), "stop before any request") {
		t.Fatal("service detection policy not applied", err)
	}
	s.OnDetection = ""
	_, err = s.Acquire(context.Background(), acquisition.Request{SourceID: "archive:demo", File: "a.epub", Output: filepath.Join(t.TempDir(), "p"), MaxBytes: 10})
	if err == nil || !strings.Contains(err.Error(), "quarantine directory") {
		t.Fatal("quarantine requirement not enforced", err)
	}
}

func TestOpenHandsAReaderOnlyAVerifiedCopy(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := library.Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "paper.pdf")
	if err := os.WriteFile(src, []byte("%PDF-1.7\n%%EOF"), 0o600); err != nil {
		t.Fatal(err)
	}
	held, err := library.Import(ctx, library.ImportRequest{Library: root, Source: src, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	var launched []string
	s := Service{Handoff: filepath.Join(t.TempDir(), "handoff"), Launch: func(_ context.Context, app, file string) error {
		launched = append(launched, app+"|"+file)
		return nil
	}}
	r, err := s.Open(ctx, root, held.AssetID, "")
	if err != nil || r.Launched != "default application" || len(launched) != 1 || !strings.HasPrefix(r.Path, s.Handoff) || strings.HasPrefix(r.Path, root) {
		t.Fatal("reader was not handed a handoff copy", r, launched, err)
	}
	if _, err := s.Open(ctx, root, held.AssetID, "reader-app"); err != nil || launched[1] != "reader-app|"+r.Path {
		t.Fatal("explicit application or copy reuse", launched, err)
	}
	s.Launch = func(context.Context, string, string) error { return errors.New("no display") }
	if _, err := s.Open(ctx, root, held.AssetID, ""); err == nil || !strings.Contains(err.Error(), r.Path) {
		t.Fatal("launch failure must name the verified copy", err)
	}
	if _, err := (Service{}).Open(ctx, root, held.AssetID, ""); err == nil {
		t.Fatal("open without a handoff directory accepted")
	}
}
