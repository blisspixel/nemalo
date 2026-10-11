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
	"github.com/blisspixel/nemalo/internal/scanner"
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

type fakeScanner string

func (f fakeScanner) Scan(context.Context, string) scanner.Result {
	return scanner.Result{Status: string(f), Tool: "fake"}
}

func TestAcquireCommandAndSubsequentLocalWorkflow(t *testing.T) {
	// The fake reports no installed scanner; tests never run a real one.
	s := app.Service{Providers: map[string]app.Searcher{"archive": intakeCatalog{}}, Scanner: fakeScanner("unavailable")}
	state := t.TempDir()
	run := func(args []string) (int, string) {
		t.Helper()
		var out, stderr bytes.Buffer
		code := Execute(context.Background(), args, &out, &stderr, s, func(string) string { return "" }, func() (config.Paths, error) {
			return config.Paths{Config: filepath.Join(t.TempDir(), "none"), State: state}, nil
		}, nil)
		return code, out.String() + stderr.String()
	}
	for _, jsonMode := range []bool{false, true} {
		output := filepath.Join(t.TempDir(), "packet")
		args := []string{"acquire", "archive:demo", "--file", "book.pdf", "--output", output}
		if jsonMode {
			args = append(args, "--json")
		}
		code, out := run(args)
		if code != 0 || !strings.Contains(out, "untrusted_intake") || !strings.Contains(out, "unavailable") {
			t.Fatal(code, out)
		}
		// Human output names the install step once; JSON stays a single value.
		if strings.Contains(out, "Next: ") == jsonMode {
			t.Fatal("scanner setup guidance", jsonMode, out)
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
	if code, out := run([]string{"acquire", "archive:demo", "--file", "book.pdf", "--output", filepath.Join(t.TempDir(), "a", "b", "packet"), "--no-scan"}); code != 0 || !strings.Contains(out, "not_scanned") || strings.Contains(out, "Next: ") {
		t.Fatal("missing parents or --no-scan", code, out)
	}
	s.Scanner = fakeScanner("findings")
	code, out := run([]string{"acquire", "archive:demo", "--file", "book.pdf", "--output", filepath.Join(t.TempDir(), "detected")})
	if code != 1 || !strings.Contains(out, "detected_quarantined") || !strings.Contains(out, "will not be imported") || !strings.Contains(out, "content.pdf.quarantined") {
		t.Fatal("detection", code, out)
	}
	for _, args := range [][]string{{"acquire"}, {"acquire", "archive:demo"}, {"acquire", "archive:demo", "--file", "../x.epub", "--output", "x"}, {"acquire", "archive:demo", "--file", "x.epub", "--output", "x", "--scan"}, {"acquire", "archive:demo", "--file", "x.epub", "--output", "x", "--max-file-bytes", "268435457"}, {"acquire", "archive:demo", "--file", "x.epub", "--output", "x", "--on-detection", "keep"}, {"check", "x", "--no-scan"}} {
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

func TestAcquireDetectionPolicyFromConfigurationAndFlag(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfg, []byte(`{"on_detection":"delete"}`), 0600); err != nil {
		t.Fatal(err)
	}
	s := app.Service{Providers: map[string]app.Searcher{"archive": intakeCatalog{}}, Scanner: fakeScanner("findings")}
	run := func(args ...string) string {
		var out bytes.Buffer
		Execute(context.Background(), append(args, "--json", "--config", cfg), &out, io.Discard, s, func(string) string { return "" }, func() (config.Paths, error) {
			return config.Paths{Config: cfg, State: filepath.Join(dir, "state")}, nil
		}, nil)
		return out.String()
	}
	if out := run("acquire", "archive:demo", "--file", "book.pdf", "--output", filepath.Join(dir, "a")); !strings.Contains(out, "detected_deleted") {
		t.Fatal("configured delete policy ignored", out)
	}
	if out := run("acquire", "archive:demo", "--file", "book.pdf", "--output", filepath.Join(dir, "b"), "--on-detection", "quarantine"); !strings.Contains(out, "detected_quarantined") {
		t.Fatal("flag did not override configuration", out)
	}
}

// recordingCatalog declares a two-track recording; fail names a track whose
// download fails until it is cleared.
type recordingCatalog struct {
	displayCatalog
	fail *string
}

func (recordingCatalog) Evaluate(context.Context, string) (discovery.Evaluation, error) {
	e := discovery.Evaluation{ID: "archive:demo", Source: "archive", Complete: true, Access: "no_item_restriction_declared"}
	r := discovery.Recording{Format: "64Kbps MP3", OrderSource: "declared_track", Acquirable: true}
	for i, name := range []string{"one_64kb.mp3", "two_64kb.mp3"} {
		body := "ID3 " + name
		n := int64(len(body))
		md := md5.Sum([]byte(body))
		e.Files = append(e.Files, discovery.OfferedFile{Name: name, Bytes: &n, MD5: hex.EncodeToString(md[:]), Access: "public_file_candidate", DownloadURL: "https://archive.org/download/demo/" + name})
		r.Tracks = append(r.Tracks, discovery.RecordingTrack{Position: i + 1, File: name, Bytes: n})
		r.Bytes += n
	}
	e.Recordings = []discovery.Recording{r}
	return e, nil
}

func (c recordingCatalog) Download(_ context.Context, f discovery.OfferedFile) (*http.Response, error) {
	if *c.fail == f.Name {
		return nil, errors.New("connection reset")
	}
	body := "ID3 " + f.Name
	return &http.Response{StatusCode: 200, ContentLength: int64(len(body)), Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
}

func TestAcquireRecordingCommandResumesAndIngestsInOrder(t *testing.T) {
	fail := "two_64kb.mp3"
	s := app.Service{Providers: map[string]app.Searcher{"archive": recordingCatalog{fail: &fail}}, Scanner: fakeScanner("unavailable")}
	state := t.TempDir()
	run := func(args ...string) (int, string) {
		t.Helper()
		var out, stderr bytes.Buffer
		code := Execute(context.Background(), args, &out, &stderr, s, func(string) string { return "" }, func() (config.Paths, error) {
			return config.Paths{Config: filepath.Join(t.TempDir(), "none"), State: state}, nil
		}, nil)
		return code, out.String() + stderr.String()
	}
	output := filepath.Join(t.TempDir(), "recording")
	code, out := run("acquire", "archive:demo", "--recording", "64Kbps MP3", "--output", output, "--no-scan")
	if code != 1 || !strings.Contains(out, "--resume") || !strings.Contains(out, "connection reset") {
		t.Fatal("interrupted recording", code, out)
	}
	fail = ""
	code, out = run("acquire", "archive:demo", "--recording", "64Kbps MP3", "--output", output, "--no-scan", "--resume", "--json")
	if code != 0 || !strings.Contains(out, `"reused":true`) || !strings.Contains(out, `"complete":true`) {
		t.Fatal("resume", code, out)
	}
	library := t.TempDir()
	for _, args := range [][]string{{"library", "init", library}, {"library", "ingest", library, output, "--apply"}} {
		if code, out := run(args...); code != 0 {
			t.Fatal(args, code, out)
		}
	}
	if code, out := run("library", "list", library, "--format", "mp3"); code != 0 || !strings.Contains(out, "Track 1 of 2") || !strings.Contains(out, "Track 2 of 2") {
		t.Fatal("recording order not listed", code, out)
	}
	for _, args := range [][]string{
		{"acquire", "archive:demo", "--resume", "--output", output},
		{"acquire", "archive:demo", "--recording", "64Kbps MP3", "--file", "one_64kb.mp3", "--output", output},
		{"acquire", "archive:demo", "--recording", "64Kbps MP3"},
		{"acquire", "archive:demo", "--recording", "64Kbps MP3", "--output", output, "--max-recording-bytes", "17179869185"},
		{"acquire", "archive:demo", "--recording", "64Kbps MP3", "--output", output, "--max-file-bytes", "1000"},
		{"acquire", "archive:demo", "--file", "one_64kb.mp3", "--output", output, "--max-recording-bytes", "1000"},
	} {
		if code, _ := run(args...); code != 2 {
			t.Fatal("bad recording usage", args, code)
		}
	}
}
