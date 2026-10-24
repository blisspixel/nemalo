package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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
	if d.Security != "not_scanned" || len(d.Capabilities) != 7 || d.Capabilities[0].Available || !d.Capabilities[2].Available || d.Capabilities[4].Name != "ffmpeg" {
		t.Fatalf("tool availability became scan coverage: %+v", d)
	}
	bridge := d.Capabilities[6]
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
	providers := New().Providers
	if providers["openlibrary"] == nil || providers["archive"] == nil || providers["gutenberg"] == nil {
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

// Homebrew's sevenzip formula installs only 7zz. Doctor must report the same
// 7-Zip that archive intake uses, not only an executable literally named 7z.
func TestLookupReportsEvery7ZipNameIntakeUses(t *testing.T) {
	for _, name := range []string{"7zz", "7za"} {
		dir := t.TempDir()
		file := filepath.Join(dir, name)
		if runtime.GOOS == "windows" {
			file += ".exe"
		}
		// Never executed: LookPath only checks presence and permission.
		if err := os.WriteFile(file, nil, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", dir)
		got, err := Lookup("7z")
		if err != nil || !strings.EqualFold(got, file) {
			t.Fatalf("%s: got %q, %v; want %q", name, got, err, file)
		}
	}
	t.Setenv("PATH", t.TempDir())
	if got, err := Lookup("7z"); err == nil {
		t.Fatalf("an empty PATH reported 7-Zip at %q", got)
	}
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

func TestAcquireGutenbergUsesThePinnedMirror(t *testing.T) {
	var calls []string
	g := discovery.NewGutenberg()
	g.FileClient = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.URL.String())
		return nil, errors.New("stop")
	})}
	s := Service{Providers: map[string]Searcher{"gutenberg": g}}
	output := filepath.Join(t.TempDir(), "packet")
	_, err := s.Acquire(context.Background(), acquisition.Request{SourceID: "gutenberg:1342", File: "1342.epub.noimages", Output: output, MaxBytes: 1000, NoScan: true})
	if err == nil || !strings.Contains(err.Error(), "gutenberg.pglaf.org") {
		t.Fatal(err)
	}
	if len(calls) != 1 || !strings.HasSuffix(calls[0], "/cache/epub/1342/pg1342.rdf") || strings.Contains(calls[0], "www.gutenberg.org") {
		t.Fatal(calls)
	}
	if _, statErr := os.Lstat(output); !os.IsNotExist(statErr) {
		t.Fatal("mirror miss created a packet", statErr)
	}
	_, err = (Service{}).Acquire(context.Background(), acquisition.Request{SourceID: "gutenberg:1342", File: "1342.epub.noimages", Output: output, MaxBytes: 1000, NoScan: true})
	if err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatal(err)
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOpenLibraryEditionResolvesOnlyItsOCAID(t *testing.T) {
	var archivePaths []string
	library := discovery.NewOpenLibrary()
	library.SetHTTPClient(&http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "openlibrary.org" || r.URL.Path != "/books/OL7353617M.json" {
			t.Errorf("open library request %s", r.URL)
		}
		body := `{"key":"/books/OL7353617M","title":"Fantastic Mr. Fox","type":{"key":"/type/edition"},"languages":[{"key":"/languages/eng"}],"works":[{"key":"/works/OL45804W"}],"ocaid":"demoitem","source_records":["ia:otheritem","ia:demoitem"]}`
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})})
	archive := discovery.NewArchive()
	archive.SetHTTPClient(&http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		archivePaths = append(archivePaths, r.URL.Path)
		if r.URL.Host != "archive.org" || r.URL.Path != "/metadata/demoitem" {
			t.Errorf("archive request %s", r.URL)
		}
		payload := map[string]any{"metadata": map[string]any{"identifier": "demoitem", "title": "Fantastic Mr. Fox", "creator": "Roald Dahl", "language": "eng"}, "files": []any{
			map[string]any{"name": "fox.epub", "format": "EPUB", "size": "100", "md5": strings.Repeat("a", 32)},
		}}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}, nil
	})})
	s := Service{Providers: map[string]Searcher{"openlibrary": library, "archive": archive}}
	if _, err := s.Evaluate(context.Background(), "openlibrary:OL45804W"); err == nil || !strings.Contains(err.Error(), "no selected edition") {
		t.Fatal(err)
	}
	if len(archivePaths) != 0 {
		t.Fatal("a work evaluation contacted Archive", archivePaths)
	}
	got, err := s.Evaluate(context.Background(), "openlibrary:OL7353617M")
	if err != nil || got.ID != "archive:demoitem" || got.OpenLibraryEdition != "openlibrary:OL7353617M" || got.Source != "archive" || len(got.Files) != 1 || got.Files[0].Name != "fox.epub" {
		t.Fatal(got, err)
	}
	joined := strings.Join(got.Limitations, " ")
	if !strings.Contains(joined, "archive:otheritem") || !strings.Contains(joined, "were not selected") || strings.Contains(joined, "work-level ia") && strings.Contains(joined, "selected the first") {
		t.Fatal(joined)
	}
	if len(archivePaths) != 1 || archivePaths[0] != "/metadata/demoitem" {
		t.Fatal(archivePaths)
	}
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

type failing struct{}

func (failing) Search(context.Context, string, int, int) (discovery.Page, error) {
	return discovery.Page{}, errors.New("provider unavailable")
}

// A search of every source reports each source's outcome: one failure does
// not hide another's results, Gutenberg without a catalog is not configured,
// and only a search where nothing completed is an error.
func TestSearchAllKeepsEachSourcesOutcome(t *testing.T) {
	s := Service{Providers: map[string]Searcher{"openlibrary": catalog{}, "archive": failing{}, "gutenberg": discovery.NewGutenberg()}}
	m, err := s.SearchAll(context.Background(), "books", 3)
	if err != nil || len(m.Sources) != 3 {
		t.Fatal(m, err)
	}
	got := map[string]string{}
	for _, p := range m.Sources {
		got[p.Source] = p.Status
	}
	if got["openlibrary"] != "complete" || got["archive"] != "failed" || got["gutenberg"] != "not_configured" || m.Sources[0].Page.Total != 3 || !strings.Contains(m.Sources[1].Detail, "unavailable") {
		t.Fatalf("%+v", m)
	}
	s = Service{Providers: map[string]Searcher{"openlibrary": failing{}, "archive": failing{}}}
	if _, err := s.SearchAll(context.Background(), "books", 3); err == nil {
		t.Fatal("a search where nothing completed succeeded")
	}
}
