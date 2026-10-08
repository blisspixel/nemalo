// Package app owns shared application operations for terminal and structured interfaces.
package app

import (
	"context"
	"errors"
	"os/exec"
	"runtime"

	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/config"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/scanner"
)

type Searcher interface {
	Search(context.Context, string, int, int) (discovery.Page, error)
}

type Service struct {
	Catalog Searcher
	Scanner assessment.Scanner
}

func New() Service { return Service{Catalog: discovery.NewOpenLibrary()} }

func (s Service) Search(ctx context.Context, query string, limit, offset int) (discovery.Page, error) {
	return s.Catalog.Search(ctx, query, limit, offset)
}

func (s Service) Inspect(ctx context.Context, root string, limits inventory.Limits) (inventory.Report, error) {
	return inventory.Inspect(ctx, root, limits)
}

func (s Service) Check(ctx context.Context, file string, options assessment.Options) (assessment.Report, error) {
	return assessment.Check(ctx, file, options, s.Scanner)
}

type Capability struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Use       string `json:"use"`
}

type Doctor struct {
	Platform     string        `json:"platform"`
	Go           string        `json:"go"`
	Paths        config.Paths  `json:"paths"`
	Config       config.Config `json:"configuration"`
	Security     string        `json:"security_status"`
	Capabilities []Capability  `json:"capabilities"`
}

// Doctor checks availability only; it never executes scanners or claims scan coverage.
func (s Service) Doctor(paths config.Paths, cfg config.Config, lookup func(string) (string, error)) Doctor {
	d := Doctor{Platform: runtime.GOOS + "/" + runtime.GOARCH, Go: runtime.Version(), Paths: paths, Config: cfg, Security: "not_scanned", Capabilities: []Capability{}}
	for _, item := range []struct{ name, use string }{{"clamscan", "optional antivirus for check --scan"}, {"MpCmdRun.exe", "optional Windows Defender antivirus for check --scan"}, {"7z", "optional future archive adapter"}, {"epubcheck", "optional future EPUB conformance check"}} {
		_, err := lookup(item.name)
		d.Capabilities = append(d.Capabilities, Capability{Name: item.name, Available: err == nil, Use: item.use})
	}
	return d
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
