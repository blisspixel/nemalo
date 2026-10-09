package cli

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/config"
	"github.com/blisspixel/nemalo/internal/discovery"
)

type displayCatalog struct {
	payload string
	fail    bool
}

type intakeCatalog struct{ displayCatalog }

func (intakeCatalog) Evaluate(context.Context, string) (discovery.Evaluation, error) {
	n := int64(len("%PDF-1.7\n%%EOF"))
	md := md5.Sum([]byte("%PDF-1.7\n%%EOF"))
	return discovery.Evaluation{ID: "archive:demo", Source: "archive", Complete: true, Access: "no_item_restriction_declared", Files: []discovery.OfferedFile{{Name: "book.pdf", Bytes: &n, MD5: hex.EncodeToString(md[:]), Access: "public_file_candidate", DownloadURL: "https://archive.org/download/demo/book.pdf"}}}, nil
}
func (intakeCatalog) Download(context.Context, discovery.OfferedFile) (*http.Response, error) {
	return &http.Response{StatusCode: 200, ContentLength: 14, Body: io.NopCloser(strings.NewReader("%PDF-1.7\n%%EOF")), Header: http.Header{}}, nil
}

func TestAcquireCommandAndSubsequentLocalWorkflow(t *testing.T) {
	s := app.Service{Providers: map[string]app.Searcher{"archive": intakeCatalog{}}}
	run := func(args []string) (int, string) {
		t.Helper()
		var out, stderr bytes.Buffer
		code := Execute(context.Background(), args, &out, &stderr, s, func(string) string { return "" }, func() (config.Paths, error) { return config.Paths{Config: filepath.Join(t.TempDir(), "none")}, nil }, nil)
		return code, out.String() + stderr.String()
	}
	for _, jsonMode := range []bool{false, true} {
		output := filepath.Join(t.TempDir(), "packet")
		args := []string{"acquire", "archive:demo", "--file", "book.pdf", "--output", output}
		if jsonMode {
			args = append(args, "--json")
		}
		if code, out := run(args); code != 0 || !strings.Contains(out, "untrusted_intake") || !strings.Contains(out, "not_scanned") {
			t.Fatal(code, out)
		}
		if code, _ := run(args); code != 1 {
			t.Fatal("repeat overwrote output")
		}
		content := filepath.Join(output, "content.pdf")
		if _, err := os.Stat(content); err != nil {
			t.Fatal(err)
		}
		catalog := filepath.Join(t.TempDir(), "catalog.json")
		for _, next := range [][]string{{"check", content, "--json"}, {"library", "snapshot", output, "--output", catalog, "--assess"}, {"library", "list", catalog, "--format", "pdf"}, {"library", "audit", catalog, "--root", output}} {
			if code, out := run(next); code != 0 {
				t.Fatal(next, code, out)
			}
		}
	}
	for _, args := range [][]string{{"acquire"}, {"acquire", "archive:demo"}, {"acquire", "archive:demo", "--file", "../x.epub", "--output", "x"}, {"acquire", "archive:demo", "--file", "x.epub", "--output", "x", "--scan"}, {"acquire", "archive:demo", "--file", "x.epub", "--output", "x", "--max-file-bytes", "268435457"}} {
		if code, _ := run(args); code != 2 {
			t.Fatal("bad acquire usage", args, code)
		}
	}
}

func (c displayCatalog) Search(context.Context, string, int, int) (discovery.Page, error) {
	page := discovery.Page{Results: []discovery.Result{{Title: c.payload}}}
	if c.fail {
		return page, errors.New(c.payload)
	}
	return page, nil
}

func TestEnvelopeAndFallbackJSONPreserveDataWithoutRawDisplayControls(t *testing.T) {
	payload := "日本語 café\u009b\u009d\u202e\u2066\u2069\U000e0001 literal \\u009b"
	assertSafe := func(s string) {
		t.Helper()
		for _, r := range s {
			if unicode.Is(unicode.Cf, r) || (unicode.IsControl(r) && r != '\n' && r != '\t') {
				t.Fatalf("raw display control %U", r)
			}
		}
	}
	for _, fail := range []bool{false, true} {
		var out, stderr bytes.Buffer
		s := app.Service{Providers: map[string]app.Searcher{"openlibrary": displayCatalog{payload, fail}}}
		code := Execute(context.Background(), []string{"search", "example", "--json"}, &out, &stderr, s, func(string) string { return "" }, func() (config.Paths, error) { return config.Paths{Config: filepath.Join(t.TempDir(), "missing")}, nil }, nil)
		if (code == 0) == fail || stderr.Len() != 0 {
			t.Fatal(code, out.String(), stderr.String())
		}
		assertSafe(out.String())
		var parsed struct {
			Data  discovery.Page
			Error string
		}
		if err := json.Unmarshal(out.Bytes(), &parsed); err != nil || parsed.Data.Results[0].Title != payload || (fail && parsed.Error != payload) {
			t.Fatal(parsed, err)
		}
	}
	var out bytes.Buffer
	want := map[string]string{payload: payload}
	if err := printData(&out, want); err != nil {
		t.Fatal(err)
	}
	assertSafe(out.String())
	var got map[string]string
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || got[payload] != payload || !strings.Contains(out.String(), "日本語 café") {
		t.Fatal(got, err)
	}
}
