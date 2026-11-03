package cli

import (
	"bytes"
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/config"
	"github.com/blisspixel/nemalo/internal/discovery"
)

type oapenCLI struct{ calls *int }

func (p oapenCLI) Search(context.Context, string, int, int) (discovery.Page, error) {
	panic("unexpected search")
}
func (p oapenCLI) Evaluate(_ context.Context, id string) (discovery.Evaluation, error) {
	*p.calls++
	return discovery.Evaluation{Complete: true, ID: id, Source: "oapen", Access: discovery.OAPENAccess, Title: []string{"OAPEN title"}, Files: []discovery.OfferedFile{}}, nil
}

func TestOAPENEvaluateCLIRoutesTheLibraryHandleOnly(t *testing.T) {
	paths := func() (config.Paths, error) {
		return config.Paths{Config: filepath.Join(t.TempDir(), "missing")}, nil
	}
	for _, id := range []string{"oapen:20.500.12657/25287", "oapen:20.500.12854/26303", "oapen:20.500.12657/12345678901"} {
		calls := 0
		var out, stderr bytes.Buffer
		code := Execute(context.Background(), []string{"evaluate", id, "--json"}, &out, &stderr, app.Service{Providers: map[string]app.Searcher{"oapen": oapenCLI{&calls}}}, func(string) string { return "" }, paths, func(context.Context, app.Service, TerminalOptions) error { return nil })
		if id == "oapen:20.500.12657/25287" {
			if code != 0 || calls != 1 || !strings.Contains(out.String(), `"source":"oapen"`) {
				t.Fatal(code, calls, out.String(), stderr.String())
			}
			continue
		}
		if code != 2 || calls != 0 || stderr.Len() != 0 || !strings.Contains(out.String(), "oapen:20.500.12657/N") || !strings.Contains(out.String(), `"error"`) {
			t.Fatal(code, calls, out.String(), stderr.String())
		}
		calls = 0
		out.Reset()
		stderr.Reset()
		code = Execute(context.Background(), []string{"evaluate", id}, &out, &stderr, app.Service{Providers: map[string]app.Searcher{"oapen": oapenCLI{&calls}}}, func(string) string { return "" }, paths, func(context.Context, app.Service, TerminalOptions) error { return nil })
		if code != 2 || calls != 0 || out.Len() != 0 || !strings.Contains(stderr.String(), "oapen:20.500.12657/N") {
			t.Fatal(code, calls, out.String(), stderr.String())
		}
	}
	var out, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"search", "open access", "--source", "oapen"}, &out, &stderr, app.Service{}, func(string) string { return "" }, paths, func(context.Context, app.Service, TerminalOptions) error { return nil })
	if code != 2 || !strings.Contains(stderr.String(), "openlibrary") {
		t.Fatal(code, stderr.String())
	}
}

type oapenMustNotRun struct{}

func (oapenMustNotRun) Search(context.Context, string, int, int) (discovery.Page, error) {
	panic("search")
}
func (oapenMustNotRun) Evaluate(context.Context, string) (discovery.Evaluation, error) {
	panic("evaluate")
}
func (oapenMustNotRun) Download(context.Context, discovery.OfferedFile) (*http.Response, error) {
	panic("download")
}

func TestOAPENAcquireCLIRefusesTheHandleBeforeAProvider(t *testing.T) {
	service := app.Service{Providers: map[string]app.Searcher{"oapen": oapenMustNotRun{}, "archive": oapenMustNotRun{}}}
	paths := func() (config.Paths, error) {
		return config.Paths{Config: filepath.Join(t.TempDir(), "missing")}, nil
	}
	for _, args := range [][]string{
		{"acquire", "oapen:20.500.12854/26303", "--file", "book.pdf", "--output", filepath.Join(t.TempDir(), "packet")},
		{"acquire", "oapen:20.500.12657/12345678901", "--file", "book.pdf", "--output", filepath.Join(t.TempDir(), "packet")},
		{"acquire", "oapen:20.500.12657/25287", "--file", "notes.txt", "--output", filepath.Join(t.TempDir(), "packet")},
	} {
		var out, stderr bytes.Buffer
		code := Execute(context.Background(), args, &out, &stderr, service, func(string) string { return "" }, paths, nil)
		refused := strings.Contains(stderr.String(), "oapen:20.500.12657/N") || strings.Contains(stderr.String(), "EPUB, PDF, MP3, M4B, and M4A")
		if code != 2 || out.Len() != 0 || !refused {
			t.Fatal(args, code, out.String(), stderr.String())
		}
	}
}

func TestOAPENHelpKeepsSearchOffTheHandle(t *testing.T) {
	paths := func() (config.Paths, error) { return config.Paths{}, nil }
	var out, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"help", "evaluate"}, &out, &stderr, app.Service{}, func(string) string { return "" }, paths, nil)
	if code != 0 || stderr.Len() != 0 || !strings.Contains(out.String(), "oapen:20.500.12657/N") || !strings.Contains(out.String(), "DOAB") || !strings.Contains(out.String(), "There is no") {
		t.Fatal(code, out.String(), stderr.String())
	}
	out.Reset()
	code = Execute(context.Background(), []string{"help", "acquire"}, &out, &stderr, app.Service{}, func(string) string { return "" }, paths, nil)
	if code != 0 || !strings.Contains(out.String(), "nemalo acquire oapen:20.500.12657/N") || !strings.Contains(out.String(), "cannot be resumed") {
		t.Fatal(code, out.String())
	}
	out.Reset()
	code = Execute(context.Background(), []string{"help", "search"}, &out, &stderr, app.Service{}, func(string) string { return "" }, paths, nil)
	if code != 0 || strings.Contains(strings.ToLower(out.String()), "oapen") {
		t.Fatal(code, out.String())
	}
}
