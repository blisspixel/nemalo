package cli

import (
	"bytes"
	"context"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/config"
	"github.com/blisspixel/nemalo/internal/discovery"
	"path/filepath"
	"strings"
	"testing"
)

type pmcCLI struct{ calls *int }

func (p pmcCLI) Search(context.Context, string, int, int) (discovery.Page, error) {
	panic("unexpected search")
}
func (p pmcCLI) Evaluate(_ context.Context, id string) (discovery.Evaluation, error) {
	*p.calls++
	return discovery.Evaluation{Complete: true, ID: id, Source: "pmc", Access: discovery.PMCAccess, Title: []string{"PMC title"}}, nil
}
func TestPMCEvaluateCLIRoutesCanonicalIDs(t *testing.T) {
	for _, id := range []string{"pmc:PMC13901", "pmc:PMC13901.2", "pmc:PMC13901.0"} {
		calls := 0
		var out, stderr bytes.Buffer
		code := Execute(context.Background(), []string{"evaluate", id, "--json"}, &out, &stderr, app.Service{Providers: map[string]app.Searcher{"pmc": pmcCLI{&calls}}}, func(string) string { return "" }, func() (config.Paths, error) { return config.Paths{Config: filepath.Join(t.TempDir(), "missing")}, nil }, func(context.Context, app.Service, TerminalOptions) error { return nil })
		if strings.HasSuffix(id, ".0") {
			if code != 2 || calls != 0 {
				t.Fatal(code, calls)
			}
		} else if code != 0 || calls != 1 || !strings.Contains(out.String(), `"source":"pmc"`) {
			t.Fatal(code, calls, out.String(), stderr.String())
		}
	}
}
