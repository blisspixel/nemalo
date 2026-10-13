package tui

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/discovery"
)

func TestOpenLibraryWorkListsEditionsBeforeArchive(t *testing.T) {
	var paths []string
	library := discovery.NewOpenLibrary()
	library.SetHTTPClient(&http.Client{Transport: transportRoundTrip(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Host+r.URL.Path)
		body := `{"links":{"work":"/works/OL45804W"},"size":1,"entries":[{"key":"/books/OL7353617M","title":"Fantastic Mr. Fox","works":[{"key":"/works/OL45804W"}],"ocaid":"demoitem","languages":[{"key":"/languages/eng"}]}]}`
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})})
	archive := discovery.NewArchive()
	archive.SetHTTPClient(&http.Client{Transport: transportRoundTrip(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Host+r.URL.Path)
		t.Error("edition list contacted Archive")
		return &http.Response{StatusCode: 500, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
	})})
	m := newModel(context.Background(), app.Service{Providers: map[string]app.Searcher{"openlibrary": library, "archive": archive}})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.mode = "search"
	m.resultsFocused = true
	m.entries = []entry{{title: `"Fantastic Mr. Fox"`, sourceID: "openlibrary:OL45804W"}}
	m.Update(tea.KeyPressMsg{Text: "e", Code: 'e'})
	if m.mode != "evaluate" || m.input.Value() != "openlibrary:OL45804W" {
		t.Fatal(m.mode, m.input.Value())
	}
	_, cmd := m.Update(key(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("edition list did not start")
	}
	m.Update(cmd())
	if len(m.entries) != 1 || m.entries[0].sourceID != "openlibrary:OL7353617M" || !strings.Contains(m.entries[0].summary, "archive:demoitem") {
		t.Fatal(m.entries)
	}
	if len(paths) != 1 || paths[0] != "openlibrary.org/works/OL45804W/editions.json" {
		t.Fatal(paths)
	}
	m.resultsFocused = true
	m.Update(tea.KeyPressMsg{Text: "e", Code: 'e'})
	if m.input.Value() != "openlibrary:OL7353617M" || !strings.Contains(m.status, "Selected edition") {
		t.Fatal(m.input.Value(), m.status)
	}
}

func TestResolvedEditionAcquiresTheNamedArchiveItem(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.mode = "evaluate"
	m.resultsFocused = true
	m.submittedInput = "openlibrary:OL7353617M"
	m.input.SetValue(m.submittedInput)
	m.secondary.SetValue(filepath.Join(t.TempDir(), "packet"))
	m.lastEvaluation = &discovery.Evaluation{ID: "archive:demoitem", Source: "archive", OpenLibraryEdition: "openlibrary:OL7353617M", Complete: true, Access: "no_item_restriction_declared"}
	m.entries = []entry{{sourceID: "fox.epub"}}
	m.Update(tea.KeyPressMsg{Text: "d", Code: 'd'})
	if m.downloadReview == nil || m.downloadReview.SourceID != "archive:demoitem" || m.downloadReview.File != "fox.epub" {
		t.Fatal(m.downloadReview, m.status)
	}
}

type transportRoundTrip func(*http.Request) (*http.Response, error)

func (f transportRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
