package app

import (
	"context"
	"errors"
	"github.com/blisspixel/nemalo/internal/acquisition"
	"github.com/blisspixel/nemalo/internal/discovery"
	"net/http"
	"testing"
)

type pmcRouteFixture struct {
	Searcher
	calls int
}

func (p *pmcRouteFixture) Evaluate(context.Context, string) (discovery.Evaluation, error) {
	p.calls++
	return discovery.Evaluation{Source: "pmc"}, errors.New("fixture evaluation")
}
func (p *pmcRouteFixture) Download(context.Context, discovery.OfferedFile) (*http.Response, error) {
	panic("must not download")
}
func TestPMCRoutingDoesNotEnableSearch(t *testing.T) {
	s := New()
	if _, ok := s.Providers["pmc"].(acquisition.Provider); !ok {
		t.Fatal("PMC not registered")
	}
	if _, err := s.Providers["pmc"].Search(context.Background(), "anything", 1, 0); err == nil {
		t.Fatal("PMC search enabled")
	}
	for _, source := range SearchSources {
		if source == "pmc" {
			t.Fatal("PMC included in all-source search")
		}
	}
	p := &pmcRouteFixture{}
	s.Providers["pmc"] = p
	if _, err := s.Evaluate(context.Background(), "pmc:PMC13901.2"); err == nil || p.calls != 1 {
		t.Fatal(err, p.calls)
	}
	got, err := s.acquireProvider("pmc:PMC13901.2")
	if err != nil || got != p {
		t.Fatal(got, err)
	}
	delete(s.Providers, "pmc")
	if _, err = s.acquireProvider("pmc:PMC13901.2"); err == nil {
		t.Fatal("fell back to another provider")
	}
}
