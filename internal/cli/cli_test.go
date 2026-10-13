package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/config"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/pdftext/pdftest"
)

func TestHealthCommand(t *testing.T) {
	p := filepath.Join(t.TempDir(), "paper.pdf")
	if err := os.WriteFile(p, pdftest.Pages("", "text"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, json := range []bool{false, true} {
		args := []string{"check", p}
		if json {
			args = append(args, "--json")
		}
		code, out, stderr := execute(t, args, nil)
		if code != 0 || stderr != "" || !strings.Contains(out, "limited_checks_passed") || !strings.Contains(out, "not_scanned") {
			t.Fatal(code, out, stderr)
		}
	}
	code, out, _ := execute(t, []string{"check", p, "--expected-bytes", "100", "--json"}, nil)
	if code != 1 || !strings.Contains(out, "size differs") {
		t.Fatal(code, out)
	}
	for _, args := range [][]string{{"check"}, {"check", p, "extra"}, {"check", p, "--expected-sha256", "wrong"}, {"check", p, "--limit", "1"}, {"search", "books", "--scan"}} {
		if code, _, _ := execute(t, args, nil); code != 2 {
			t.Fatal("bad usage accepted", args, code)
		}
	}
}

func TestLibraryCommandsPreserveSourcesAndRequireExplicitRoots(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "paper.pdf")
	if err := os.WriteFile(source, []byte("%PDF-1.7\n%%EOF"), 0600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "catalog.json")
	code, out, stderr := execute(t, []string{"library", "snapshot", root, "--output", file, "--assess", "--json"}, nil)
	if code != 0 || stderr != "" || !strings.Contains(out, `"complete":true`) || !strings.Contains(out, `"not_scanned"`) {
		t.Fatal(code, out, stderr)
	}
	for _, args := range [][]string{
		{"library", "list", file, "--query", "PAPER", "--limit", "1"},
		{"library", "list", file, "--query", "PAPER", "--json"},
		{"library", "list", file, "--format", "pdf", "--json"},
		{"library", "audit", file, "--root", root},
		{"library", "audit", file, "--root", root, "--json"},
	} {
		if code, out, stderr := execute(t, args, nil); code != 0 || stderr != "" || out == "" {
			t.Fatal(args, code, out, stderr)
		}
	}
	for _, args := range [][]string{
		{"library"}, {"library", "unknown", file}, {"library", "list"},
		{"library", "snapshot", root}, {"library", "snapshot", root, "--output", file, "--scan"},
		{"library", "list", file, "--limit", "101"}, {"library", "list", file, "--offset", "-1"},
		{"library", "list", file, "--format", "exe"}, {"search", "books", "--format", "pdf"},
		{"library", "audit", file}, {"library", "audit", file, "--root", root, "--assess"},
	} {
		if code, _, _ := execute(t, args, nil); code != 2 {
			t.Fatal("invalid library invocation accepted", args, code)
		}
	}
	for _, args := range [][]string{
		{"library", "snapshot", root, "--output", file},
		{"library", "snapshot", root, "--output", filepath.Join(root, "inside.json")},
		{"library", "list", filepath.Join(root, "absent")},
		{"library", "audit", filepath.Join(root, "absent"), "--root", root},
	} {
		if code, _, _ := execute(t, args, nil); code != 1 {
			t.Fatal("failed operation passed", args, code)
		}
	}
	if err := os.WriteFile(source, []byte("%PDF-1.8\n%%EOF"), 0600); err != nil {
		t.Fatal(err)
	}
	code, out, _ = execute(t, []string{"library", "audit", file, "--root", root, "--json"}, nil)
	if code != 1 || !strings.Contains(out, `"changed"`) || !strings.Contains(out, `"complete":true`) {
		t.Fatal("same-size replacement not reported", code, out)
	}
	if data, _ := os.ReadFile(source); string(data) != "%PDF-1.8\n%%EOF" {
		t.Fatal("audit mutated source")
	}
}

type searcher struct{ err error }

func (s searcher) Search(_ context.Context, q string, limit, offset int) (discovery.Page, error) {
	return discovery.Page{Query: q, Total: limit, Offset: offset, Results: []discovery.Result{}}, s.err
}

func (s searcher) SearchOrdered(_ context.Context, q, order string, limit, offset int) (discovery.Page, error) {
	return discovery.Page{Query: q, Order: order, Total: limit, Offset: offset, Results: []discovery.Result{}}, s.err
}

func (s searcher) Evaluate(_ context.Context, id string) (discovery.Evaluation, error) {
	return discovery.Evaluation{ID: id, Source: "archive", Access: "restricted_or_uncertain", Files: []discovery.OfferedFile{}, Limitations: []string{"File rights and DRM remain unverified"}}, s.err
}

func TestProviderSearchAndEvaluationCommands(t *testing.T) {
	for _, args := range [][]string{{"search", "Verne", "--source", "archive", "--json"}, {"search", "Verne", "--source", "openlibrary"}, {"evaluate", "archive:demo"}, {"evaluate", "archive:demo", "--json"}} {
		code, out, stderr := execute(t, args, nil)
		if code != 0 || out == "" || stderr != "" {
			t.Fatal(args, code, out, stderr)
		}
	}
	code, out, _ := execute(t, []string{"evaluate", "archive:demo", "--json"}, errors.New("provider unavailable"))
	if code != 1 || !strings.Contains(out, "provider unavailable") {
		t.Fatal(code, out)
	}
	code, out, stderr := execute(t, []string{"search", "frankenstein", "--source", "archive"}, errors.New("provider resolved to a nonpublic destination"))
	if code != 1 || strings.TrimSpace(out) != "" || strings.Contains(out, "matches") || strings.Contains(out, "No results") || !strings.Contains(stderr, "nonpublic destination") {
		t.Fatal(code, out, stderr)
	}
	code, out, stderr = execute(t, []string{"search", "frankenstein", "--source", "archive", "--json"}, errors.New("provider resolved to a nonpublic destination"))
	if code != 1 || stderr != "" || strings.Contains(out, "No results") || !strings.Contains(out, `"error"`) || !strings.Contains(out, "nonpublic destination") {
		t.Fatal(code, out, stderr)
	}
	for _, args := range [][]string{{"evaluate"}, {"evaluate", "demo"}, {"evaluate", "archive:../x"}, {"evaluate", "openlibrary:OL1W"}, {"evaluate", "archive:demo", "--editions"}, {"evaluate", "openlibrary:OL1M", "--editions"}, {"evaluate", "gutenberg:11", "--editions"}, {"evaluate", "archive:demo", "--limit", "1"}, {"evaluate", "openlibrary:OL1W", "--files"}, {"evaluate", "archive:demo", "extra"}, {"evaluate", "archive:demo", "--source", "archive"}, {"search", "x", "--source", "missing"}, {"inspect", "x", "--source", "archive"},
		{"search", "x", "--sort", "identifier"}, {"search", "x", "--source", "archive", "--sort", "popularity"}, {"search", "x", "--sort", "downloads"}, {"search", "x", "--source", "archive", "--sort", ""}, {"evaluate", "archive:demo", "--sort", "relevance"}} {
		if code, _, _ := execute(t, args, nil); code != 2 {
			t.Fatal("invalid provider usage accepted", args, code)
		}
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func execute(t *testing.T, args []string, searchErr error) (int, string, string) {
	t.Helper()
	saved := stdinTerminal
	stdinTerminal = func() bool { return true }
	t.Cleanup(func() { stdinTerminal = saved })
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	code := Execute(context.Background(), args, &out, &errOut, app.Service{Providers: map[string]app.Searcher{"openlibrary": searcher{searchErr}, "archive": searcher{searchErr}}}, func(string) string { return "" }, func() (config.Paths, error) { return config.Paths{Config: filepath.Join(dir, "missing.json")}, nil }, func(context.Context, app.Service) error { return nil })
	return code, out.String(), errOut.String()
}

func TestHelpAndVersion(t *testing.T) {
	for _, args := range [][]string{nil, {"help"}, {"--help"}, {"-h"}, {"inspect", "--help"}, {"library", "--help"}, {"library", "snapshot", "--help"}} {
		code, out, _ := execute(t, args, nil)
		if code != 0 || !strings.Contains(out, "Nemalo") || !strings.Contains(out, "recommended for") || !strings.Contains(out, "--expected-sha256") {
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
	stdinTerminal = func() bool { return true }
	if code := Execute(context.Background(), []string{"tui"}, io.Discard, io.Discard, app.New(), func(string) string { return "" }, paths, func(context.Context, app.Service) error { return errors.New("no terminal") }); code != 1 {
		t.Fatal(code)
	}
	stdinTerminal = func() bool { return false }
	var out, errOut bytes.Buffer
	if code := Execute(context.Background(), []string{"tui"}, &out, &errOut, app.New(), func(string) string { return "" }, paths, func(context.Context, app.Service) error {
		t.Fatal("tui started without a terminal")
		return nil
	}); code != 2 || !strings.Contains(errOut.String(), "tui requires an interactive terminal") {
		t.Fatal(code, out.String(), errOut.String())
	}
}

func TestMissingLibraryCleanupPrintsNoPreview(t *testing.T) {
	lib := filepath.Join(t.TempDir(), "missing")
	folder := t.TempDir()
	code, out, stderr := execute(t, []string{"library", "cleanup", lib, folder}, nil)
	if code != 1 || strings.TrimSpace(out) != "" || strings.Contains(out, "Preview") || strings.Contains(out, "Nothing here is held") {
		t.Fatal(code, out, stderr)
	}
	if !strings.Contains(stderr, "library not found") || !strings.Contains(stderr, "missing") {
		t.Fatal(stderr)
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

func TestGutenbergCatalogCommands(t *testing.T) {
	dir := t.TempDir()
	catalog := filepath.Join(dir, "pg_catalog.csv")
	body := "Text#,Type,Issued,Title,Language,Authors,Subjects,LoCC,Bookshelves\n11,Text,1865-11-26,Alice,en; fr,Carroll,Fantasy,PZ,Children\n"
	if err := os.WriteFile(catalog, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := func() (config.Paths, error) { return config.Paths{Config: filepath.Join(dir, "missing.json")}, nil }
	env := func(string) string { return "" }
	run := func(args ...string) (int, string, string) {
		t.Helper()
		var out, errOut bytes.Buffer
		code := Execute(context.Background(), args, &out, &errOut, app.New(), env, paths, func(context.Context, app.Service) error { return nil })
		return code, out.String(), errOut.String()
	}
	code, out, stderr := run("search", "alice", "--source", "gutenberg", "--catalog", catalog)
	if code != 0 || stderr != "" || !strings.Contains(out, "gutenberg:11") || !strings.Contains(out, "website was not queried") || strings.Contains(out, "ties may move") {
		t.Fatal(code, out, stderr)
	}
	code, out, stderr = run("search", "alice", "--source", "gutenberg", "--catalog", catalog, "--sort", "identifier", "--json")
	if code != 0 || stderr != "" || !strings.Contains(out, `"order":"identifier"`) || !strings.Contains(out, `"id":"gutenberg:11"`) {
		t.Fatal(code, out, stderr)
	}
	code, out, stderr = run("evaluate", "gutenberg:11", "--catalog", catalog)
	if code != 0 || stderr != "" || !strings.Contains(out, "local_catalog") || !strings.Contains(out, "Files listed: 0") || !strings.Contains(out, "download URL") {
		t.Fatal(code, out, stderr)
	}
	code, out, _ = run("evaluate", "gutenberg:11", "--catalog", catalog, "--json")
	if code != 0 || !strings.Contains(out, `"files":[]`) || !strings.Contains(out, `"languages":["en","fr"]`) {
		t.Fatal(code, out)
	}
	for _, args := range [][]string{
		{"search", "alice", "--source", "gutenberg", "--sort", "downloads", "--catalog", catalog},
		{"search", "alice", "--source", "gutenberg", "--catalog", "relative.csv"},
		{"evaluate", "gutenberg:01", "--catalog", catalog},
		{"catalog", "fetch"},
		{"catalog", "fetch", "other", "--output", filepath.Join(dir, "out.csv")},
		{"doctor", "--catalog", catalog},
	} {
		if code, _, _ := run(args...); code != 2 {
			t.Fatal(args, code)
		}
	}
	code, _, stderr = run("search", "alice", "--source", "gutenberg")
	if code != 1 || !strings.Contains(stderr, "does not query") {
		t.Fatal(code, stderr)
	}
	code, _, stderr = run("evaluate", "gutenberg:99", "--catalog", catalog)
	if code != 1 || !strings.Contains(stderr, "no record") {
		t.Fatal(code, stderr)
	}
}

func TestEvaluateFilesListsGutenbergOffers(t *testing.T) {
	rdf := `<?xml version="1.0" encoding="utf-8"?>
<rdf:RDF xml:base="http://www.gutenberg.org/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:pgterms="http://www.gutenberg.org/2009/pgterms/">
  <pgterms:ebook rdf:about="ebooks/1342">
    <dcterms:rights>Public domain in the USA.</dcterms:rights>
    <dcterms:title>Pride and Prejudice</dcterms:title>
    <pgterms:marc520>automatically generated summary should not be stored</pgterms:marc520>
    <dcterms:hasFormat><pgterms:file rdf:about="https://www.gutenberg.org/ebooks/1342.epub.noimages"><dcterms:extent>561111</dcterms:extent><dcterms:format><rdf:Description><rdf:value>application/epub+zip</rdf:value></rdf:Description></dcterms:format></pgterms:file></dcterms:hasFormat>
    <dcterms:hasFormat><pgterms:file rdf:about="https://www.gutenberg.org/ebooks/1342.txt.utf-8"><dcterms:extent>50</dcterms:extent><dcterms:format><rdf:Description><rdf:value>text/plain</rdf:value></rdf:Description></dcterms:format></pgterms:file></dcterms:hasFormat>
  </pgterms:ebook>
</rdf:RDF>`
	var calls []string
	g := discovery.NewGutenberg()
	g.FileClient = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.URL.String())
		return &http.Response{StatusCode: 200, ContentLength: int64(len(rdf)), Header: http.Header{}, Body: io.NopCloser(strings.NewReader(rdf)), Request: r}, nil
	})}
	dir := t.TempDir()
	run := func(args ...string) (int, string, string) {
		t.Helper()
		var out, errOut bytes.Buffer
		code := Execute(context.Background(), args, &out, &errOut, app.Service{Providers: map[string]app.Searcher{"gutenberg": g, "archive": searcher{}}}, func(string) string { return "" }, func() (config.Paths, error) {
			return config.Paths{Config: filepath.Join(dir, "missing.json")}, nil
		}, func(context.Context, app.Service) error { return nil })
		return code, out.String(), errOut.String()
	}
	code, out, stderr := run("evaluate", "gutenberg:1342", "--files")
	if code != 0 || stderr != "" || !strings.Contains(out, "mirror_rdf") || !strings.Contains(out, "1342.epub.noimages") || !strings.Contains(out, "https://gutenberg.pglaf.org/cache/epub/1342/pg1342.epub") || strings.Contains(out, "automatically generated") {
		t.Fatal(code, out, stderr)
	}
	if !strings.Contains(out, "1342.txt.utf-8") || !strings.Contains(out, "not acquired") {
		t.Fatal(out)
	}
	if len(calls) != 1 || !strings.HasSuffix(calls[0], "/pg1342.rdf") || strings.Contains(calls[0], "www.gutenberg.org") {
		t.Fatal(calls)
	}
	code, out, stderr = run("evaluate", "gutenberg:1342", "--files", "--json")
	if code != 0 || stderr != "" || !strings.Contains(out, `"access_status":"public_file_no_source_checksum"`) || !strings.Contains(out, "pg1342.epub") {
		t.Fatal(code, out, stderr)
	}
	if code, _, stderr = run("evaluate", "archive:demo", "--files"); code != 2 || !strings.Contains(stderr, "gutenberg:NUMBER") {
		t.Fatal(code, stderr)
	}
	if code, _, stderr = run("acquire", "gutenberg:1342", "--file", "1342.txt.utf-8", "--output", filepath.Join(dir, "packet"), "--no-scan"); code != 2 || len(calls) != 2 {
		t.Fatal(code, stderr, calls)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSearchOrderSelection(t *testing.T) {
	code, out, stderr := execute(t, []string{"search", "Verne", "--source", "archive", "--sort", "identifier", "--json"}, nil)
	if code != 0 || stderr != "" || !strings.Contains(out, `"order":"identifier"`) {
		t.Fatal(code, out, stderr)
	}
	code, out, _ = execute(t, []string{"search", "Verne", "--source", "archive", "--sort", "identifier"}, nil)
	if code != 0 || !strings.Contains(out, "stable for paging") {
		t.Fatal(code, out)
	}
}
