// Package app owns shared application operations for terminal and structured interfaces.
package app

import (
	"context"
	"errors"
	"github.com/blisspixel/nemalo/internal/acquisition"
	"os/exec"
	"runtime"
	"strings"

	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/config"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/safeio"
	"github.com/blisspixel/nemalo/internal/scanner"
)

type Searcher interface {
	Search(context.Context, string, int, int) (discovery.Page, error)
}

func (s Service) Acquire(ctx context.Context, request acquisition.Request) (acquisition.Result, error) {
	provider, ok := s.Providers["archive"].(acquisition.Provider)
	if !ok {
		return acquisition.Result{}, errors.New("archive acquisition is unavailable")
	}
	return acquisition.Acquire(ctx, provider, request)
}

type Service struct {
	Providers map[string]Searcher
	Scanner   assessment.Scanner
}

func New() Service {
	return Service{Providers: map[string]Searcher{"openlibrary": discovery.NewOpenLibrary(), "archive": discovery.NewArchive()}}
}

func (s Service) Search(ctx context.Context, query string, limit, offset int) (discovery.Page, error) {
	return s.SearchSource(ctx, "openlibrary", query, limit, offset)
}

func (s Service) SearchSource(ctx context.Context, source, query string, limit, offset int) (discovery.Page, error) {
	p, ok := s.Providers[source]
	if !ok || p == nil {
		return discovery.Page{}, errors.New("search provider unavailable")
	}
	return p.Search(ctx, query, limit, offset)
}

func (s Service) Evaluate(ctx context.Context, id string) (discovery.Evaluation, error) {
	source, _, ok := strings.Cut(id, ":")
	if !ok {
		return discovery.Evaluation{}, errors.New("evaluation requires a namespaced item identifier")
	}
	p, ok := s.Providers[source].(interface {
		Evaluate(context.Context, string) (discovery.Evaluation, error)
	})
	if !ok {
		return discovery.Evaluation{}, errors.New("provider does not support item evaluation")
	}
	return p.Evaluate(ctx, id)
}

func (s Service) Inspect(ctx context.Context, root string, limits inventory.Limits) (inventory.Report, error) {
	return inventory.Inspect(ctx, root, limits)
}

func (s Service) Check(ctx context.Context, file string, options assessment.Options) (assessment.Report, error) {
	return assessment.Check(ctx, file, options, s.Scanner)
}

func (s Service) Snapshot(ctx context.Context, directory, output string, limits inventory.Limits, assess bool) (library.Snapshot, error) {
	if output == "" {
		return library.Snapshot{}, errors.New("snapshot requires an explicit output path")
	}
	if err := library.CheckDestination(directory, output); err != nil {
		return library.Snapshot{}, err
	}
	r, err := library.Build(ctx, directory, limits, assess)
	if err != nil {
		return r, err
	}
	err = library.Save(ctx, output, &r)
	return r, err
}

func (s Service) Holdings(file, query string, limit, offset int) (library.Page, error) {
	return s.HoldingsFormat(file, query, "all", limit, offset)
}

func (s Service) HoldingsFormat(file, query, format string, limit, offset int) (library.Page, error) {
	c, err := library.Load(file)
	if err != nil {
		return library.Page{}, err
	}
	return library.FindFormat(c, query, format, limit, offset)
}

func (s Service) Audit(ctx context.Context, file, root string, limits inventory.Limits) (library.Audit, error) {
	c, err := library.Load(file)
	if err != nil {
		return library.Audit{}, err
	}
	return library.Verify(ctx, c, root, limits)
}

func (s Service) InitializeLibrary(ctx context.Context, root string) (library.State, error) {
	return library.Initialize(ctx, root)
}

func (s Service) LibraryStatus(ctx context.Context, root string) (library.State, error) {
	return library.LibraryStatus(ctx, root)
}

func (s Service) Import(ctx context.Context, request library.ImportRequest) (library.ImportResult, error) {
	if request.Scan && request.Scanner == nil {
		request.Scanner = s.Scanner
	}
	return library.Import(ctx, request)
}

func (s Service) AuditManaged(ctx context.Context, directory string) (library.ManagedAudit, error) {
	return library.AuditManaged(ctx, directory)
}

func (s Service) Content(ctx context.Context, req content.Request) (content.Result, error) {
	return content.Get(ctx, req)
}

func (s Service) ContentCapabilities() content.Capabilities { return content.Available() }

type Capability struct {
	Name        string `json:"name"`
	Available   bool   `json:"available"`
	Recommended bool   `json:"recommended,omitempty"`
	Use         string `json:"use"`
}

type Doctor struct {
	Platform               string        `json:"platform"`
	Go                     string        `json:"go"`
	Paths                  config.Paths  `json:"paths"`
	Config                 config.Config `json:"configuration"`
	Security               string        `json:"security_status"`
	ScannerRecommendation  string        `json:"scanner_recommendation"`
	ChecksumRecommendation string        `json:"checksum_recommendation"`
	Capabilities           []Capability  `json:"capabilities"`
}

// Doctor checks availability only; it never executes scanners or claims scan coverage.
func (s Service) Doctor(paths config.Paths, cfg config.Config, lookup func(string) (string, error)) Doctor {
	d := Doctor{Platform: runtime.GOOS + "/" + runtime.GOARCH, Go: runtime.Version(), Paths: paths, Config: cfg, Security: "not_scanned", ScannerRecommendation: scannerRecommendation(), ChecksumRecommendation: checksumRecommendation, Capabilities: []Capability{}}
	for _, item := range []struct {
		name, use string
	}{
		{"clamscan", "Recommended on Linux and macOS for a file you did not produce. Windows uses Microsoft Defender instead. A no-detection result is not proof the file is safe."},
		{"MpCmdRun.exe", "Recommended on Windows for a file you did not produce. This is the built-in Microsoft Defender scanner. A no-detection result is not proof the file is safe."},
		{"7z", "optional future archive adapter"},
		{"epubcheck", "optional future EPUB conformance check"},
	} {
		_, err := lookup(item.name)
		d.Capabilities = append(d.Capabilities, Capability{Name: item.name, Available: err == nil, Recommended: recommendedScanner(item.name), Use: item.use})
	}
	d.Capabilities = append(d.Capabilities, Capability{Name: "descriptor_bridge", Available: safeio.DescriptorBridge() == nil, Use: "required on Linux (/proc/self/fd) and macOS (/dev/fd) to reopen a library directory"})
	return d
}

const checksumRecommendation = "When a publisher supplies a SHA-256, pass it to check --expected-sha256 or keep the intake receipt. A match shows the bytes are the published bytes. Nemalo also records the SHA-256 it measured. A checksum does not prove those bytes are free of malware."

func recommendedScanner(name string) bool {
	if runtime.GOOS == "windows" {
		return name == "MpCmdRun.exe"
	}
	return name == "clamscan"
}

func scannerRecommendation() string {
	switch runtime.GOOS {
	case "windows":
		return "Microsoft Defender is the default scanner. Run check FILE --scan or library import --scan. ClamAV is a free second engine and is not the default on Windows. Nothing is installed automatically."
	case "darwin":
		return "ClamAV is the default free local scanner Nemalo can run. macOS does not provide an equivalent command-line scanner. Install ClamAV so clamscan is on PATH, update signatures with freshclam, then run check FILE --scan. Nothing is installed automatically."
	default:
		return "ClamAV is the default free local scanner. Install the distributor package that provides clamscan, update signatures with freshclam, then run check FILE --scan. On Arch, including Omarchy, that package is clamav. Nothing is installed automatically."
	}
}

func Lookup(name string) (string, error) {
	if name == "MpCmdRun.exe" {
		kind, path, err := scanner.Resolve()
		if err == nil && kind == "defender" {
			return path, nil
		}
		return "", errors.New("installed Defender unavailable")
	}
	return exec.LookPath(name)
}
