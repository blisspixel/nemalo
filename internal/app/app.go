// Package app owns shared application operations for terminal and structured interfaces.
package app

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"

	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/config"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/scanner"
)

type Searcher interface {
	Search(context.Context, string, int, int) (discovery.Page, error)
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

func (s Service) Content(ctx context.Context, req content.Request) (content.Result, error) {
	return content.Get(ctx, req)
}

func (s Service) ContentCapabilities() content.Capabilities { return content.Available() }

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
