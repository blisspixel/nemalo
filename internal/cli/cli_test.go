package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/config"
	"github.com/blisspixel/nemalo/internal/discovery"
)

type searcher struct{ err error }

func (s searcher) Search(_ context.Context, q string, limit, offset int) (discovery.Page, error) {
	return discovery.Page{Query: q, Total: limit, Offset: offset, Results: []discovery.Result{}}, s.err
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func execute(t *testing.T, args []string, searchErr error) (int, string, string) {
	t.Helper()
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	code := Execute(context.Background(), args, &out, &errOut, app.Service{Catalog: searcher{searchErr}}, func(string) string { return "" }, func() (config.Paths, error) { return config.Paths{Config: filepath.Join(dir, "missing.json")}, nil }, func(context.Context, app.Service) error { return nil })
	return code, out.String(), errOut.String()
}

func TestHelpAndVersion(t *testing.T) {
	for _, args := range [][]string{nil, {"help"}, {"--help"}, {"-h"}, {"inspect", "--help"}} {
		code, out, _ := execute(t, args, nil)
		if code != 0 || !strings.Contains(out, "Nemalo") {
			t.Fatal(code, out)
		}
	}
	code, out, _ := execute(t, []string{"version"}, nil)
	if code != 0 || !strings.Contains(out, Version) {
		t.Fatal(code, out)
	}
	code, out, _ = execute(t, []string{"version", "--json"}, nil)
	var result Envelope
	if err := json.Unmarshal([]byte(out), &result); err != nil || code != 0 || result.SchemaVersion != 1 || result.Command != "version" {
		t.Fatal(code, out, err)
	}
	if Run(context.Background(), []string{"version"}, io.Discard, io.Discard) != 0 {
		t.Fatal("real entry failed")
	}
}

func TestSearchJSONAndInterspersedFlags(t *testing.T) {
	code, out, stderr := execute(t, []string{"search", "Jules Verne", "--json", "--limit", "3", "--offset=2"}, nil)
	var envelope struct {
		SchemaVersion int            `json:"schema_version"`
		Data          discovery.Page `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || code != 0 || stderr != "" || envelope.Data.Query != "Jules Verne" || envelope.Data.Total != 3 || envelope.Data.Offset != 2 {
		t.Fatal(code, out, stderr, err)
	}
	code, out, _ = execute(t, []string{"search", "--json", "--", "-title"}, nil)
	if code != 0 || !strings.Contains(out, "-title") {
		t.Fatal(code, out)
	}
	code, out, _ = execute(t, []string{"search", "books", "--json"}, errors.New("quota unavailable"))
	if code != 1 || !strings.Contains(out, "quota unavailable") {
		t.Fatal(code, out)
	}
	code, _, stderr = execute(t, []string{"search", "books"}, errors.New("bad\x1b[31m"))
	if code != 1 || strings.Contains(stderr, "\x1b") {
		t.Fatal("terminal injection", stderr)
	}
}

func TestInspectAndDoctor(t *testing.T) {
	code, out, _ := execute(t, []string{"inspect", t.TempDir(), "--json", "--hashes"}, nil)
	if code != 0 || !strings.Contains(out, `"not_scanned"`) || !strings.Contains(out, `"complete":true`) {
		t.Fatal(code, out)
	}
	code, out, _ = execute(t, []string{"inspect", filepath.Join(t.TempDir(), "missing"), "--json"}, nil)
	if code != 1 || !strings.Contains(out, `"complete":false`) {
		t.Fatal(code, out)
	}
	code, out, _ = execute(t, []string{"doctor", "--json"}, nil)
	if code != 0 || !strings.Contains(out, `"capabilities"`) {
		t.Fatal(code, out)
	}
	code, out, _ = execute(t, []string{"doctor"}, nil)
	if code != 0 || !strings.Contains(out, "not_scanned") {
		t.Fatal(code, out)
	}
	code, _, _ = execute(t, []string{"tui"}, nil)
	if code != 0 {
		t.Fatal(code)
	}
}

func TestUsageAndConfigurationFailures(t *testing.T) {
	for _, args := range [][]string{{"unknown"}, {"version", "extra"}, {"doctor", "extra"}, {"search"}, {"search", "books", "--limit=0"}, {"search", "books", "--offset=-1"}, {"inspect"}, {"inspect", "a", "b"}, {"tui", "--json"}, {"tui", "extra"}, {"search", "--limit"}, {"search", "--bad"}, {"doctor", "--library=relative"}, {"doctor", "--config=missing"}, {"search", "x", "--hashes"}, {"doctor", "--limit=2"}, {"version", "--library=x"}} {
		code, _, _ := execute(t, args, nil)
		if code != 2 {
			t.Fatalf("%v: exit %d", args, code)
		}
	}
	paths := func() (config.Paths, error) { return config.Paths{}, errors.New("no paths") }
	if code := Execute(context.Background(), []string{"doctor"}, io.Discard, io.Discard, app.New(), func(string) string { return "" }, paths, nil); code != 2 {
		t.Fatal(code)
	}
	dir := t.TempDir()
	paths = func() (config.Paths, error) { return config.Paths{Config: filepath.Join(dir, "missing")}, nil }
	if code := Execute(context.Background(), []string{"tui"}, io.Discard, io.Discard, app.New(), func(string) string { return "" }, paths, func(context.Context, app.Service) error { return errors.New("no terminal") }); code != 1 {
		t.Fatal(code)
	}
}

func TestOutputFailures(t *testing.T) {
	for _, args := range [][]string{nil, {"version"}, {"version", "--json"}, {"doctor"}, {"doctor", "--help"}} {
		dir := t.TempDir()
		code := Execute(context.Background(), args, brokenWriter{}, io.Discard, app.New(), func(string) string { return "" }, func() (config.Paths, error) { return config.Paths{Config: filepath.Join(dir, "missing")}, nil }, nil)
		if code != 1 {
			t.Fatalf("output error hidden: %v code %d", args, code)
		}
	}
	f := flag.NewFlagSet("test", flag.ContinueOnError)
	f.Bool("json", false, "")
	f.Int("limit", 1, "")
	if err := parse(f, []string{"title", "--json", "--limit=2"}); err != nil || f.Arg(0) != "title" {
		t.Fatal(err)
	}
}
