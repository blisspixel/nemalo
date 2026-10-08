// Package app owns shared application operations for terminal and structured interfaces.
package app

import (
	"context"
	"os/exec"
	"runtime"

	"github.com/blisspixel/nemalo/internal/config"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/inventory"
)

type Searcher interface {
	Search(context.Context, string, int, int) (discovery.Page, error)
}

type Service struct{ Catalog Searcher }

func New() Service { return Service{Catalog: discovery.NewOpenLibrary()} }

func (s Service) Search(ctx context.Context, query string, limit, offset int) (discovery.Page, error) {
	return s.Catalog.Search(ctx, query, limit, offset)
}

func (s Service) Inspect(ctx context.Context, root string, limits inventory.Limits) (inventory.Report, error) {
	return inventory.Inspect(ctx, root, limits)
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
	for _, item := range []struct{ name, use string }{{"clamscan", "optional future antivirus adapter"}, {"7z", "optional future archive adapter"}, {"epubcheck", "optional future EPUB conformance check"}} {
		_, err := lookup(item.name)
		d.Capabilities = append(d.Capabilities, Capability{Name: item.name, Available: err == nil, Use: item.use})
	}
	return d
}

func Lookup(name string) (string, error) { return exec.LookPath(name) }
