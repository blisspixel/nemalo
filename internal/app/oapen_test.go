package app

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/acquisition"
	"github.com/blisspixel/nemalo/internal/discovery"
)

type oapenRouteFixture struct {
	Searcher
	calls int
}

func (p *oapenRouteFixture) Evaluate(context.Context, string) (discovery.Evaluation, error) {
	p.calls++
	return discovery.Evaluation{Source: "oapen"}, errors.New("fixture evaluation")
}
func (p *oapenRouteFixture) Download(context.Context, discovery.OfferedFile) (*http.Response, error) {
	panic("must not download")
}

func TestOAPENRoutingDoesNotEnableSearch(t *testing.T) {
	s := New()
	if _, ok := s.Providers["oapen"].(acquisition.Provider); !ok {
		t.Fatal("OAPEN not registered")
	}
	if _, err := s.Providers["oapen"].Search(context.Background(), "open access", 1, 0); err == nil {
		t.Fatal("OAPEN search enabled")
	}
	for _, source := range SearchSources {
		if source == "oapen" {
			t.Fatal("OAPEN included in all-source search")
		}
	}
	p := &oapenRouteFixture{}
	s.Providers["oapen"] = p
	if _, err := s.Evaluate(context.Background(), "oapen:20.500.12657/25287"); err == nil || p.calls != 1 {
		t.Fatal(err, p.calls)
	}
	got, err := s.acquireProvider("oapen:20.500.12657/25287")
	if err != nil || got != p {
		t.Fatal(got, err)
	}
	delete(s.Providers, "oapen")
	if _, err = s.acquireProvider("oapen:20.500.12657/25287"); err == nil {
		t.Fatal("fell back to another provider")
	}
}

type oapenRoundTrip func(*http.Request) (*http.Response, error)

func (f oapenRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOAPENServiceRefusesAnotherPrefixBeforeARequest(t *testing.T) {
	s := New()
	provider, ok := s.Providers["oapen"].(oapenSearcher)
	if !ok || provider.OAPEN == nil {
		t.Fatal("OAPEN registration", ok)
	}
	trips := 0
	refuse := &http.Client{Transport: oapenRoundTrip(func(*http.Request) (*http.Response, error) {
		trips++
		return nil, errors.New("dial")
	})}
	provider.SetHTTPClient(refuse)
	provider.SetDownloadClient(refuse)
	for _, id := range []string{"oapen:20.500.12854/26303", "oapen:20.500.12657/12345678901", "oapen:20.500.12657/../1"} {
		evaluation, err := s.Evaluate(context.Background(), id)
		if err == nil || evaluation.Complete || trips != 0 || !strings.Contains(err.Error(), "oapen:20.500.12657/N") {
			t.Fatal(id, err, trips, evaluation.Complete)
		}
	}
}

type oapenCallCount struct{ calls int }

func (p *oapenCallCount) Search(context.Context, string, int, int) (discovery.Page, error) {
	p.calls++
	return discovery.Page{}, errors.New("search")
}
func (p *oapenCallCount) Evaluate(context.Context, string) (discovery.Evaluation, error) {
	p.calls++
	return discovery.Evaluation{}, errors.New("evaluate")
}
func (p *oapenCallCount) Download(context.Context, discovery.OfferedFile) (*http.Response, error) {
	p.calls++
	return nil, errors.New("download")
}

func TestOAPENAcquireSelectsTheLibraryHandleOnly(t *testing.T) {
	s := New()
	library, archive := &oapenCallCount{}, &oapenCallCount{}
	s.Providers["oapen"] = library
	s.Providers["archive"] = archive
	request := acquisition.Request{SourceID: "oapen:20.500.12854/26303", File: "book.pdf", Output: filepath.Join(t.TempDir(), "packet"), MaxBytes: 1024, NoScan: true}
	if _, err := s.Acquire(context.Background(), request); err == nil || library.calls != 0 || archive.calls != 0 || !strings.Contains(err.Error(), "oapen:20.500.12657/N") {
		t.Fatal(err, library.calls, archive.calls)
	}
	request.SourceID = "oapen:20.500.12657/25287"
	request.Output = filepath.Join(t.TempDir(), "other")
	if _, err := s.Acquire(context.Background(), request); err == nil || library.calls != 1 || archive.calls != 0 || !strings.Contains(err.Error(), "evaluate") {
		t.Fatal(err, library.calls, archive.calls)
	}
}
