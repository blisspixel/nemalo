package assessment

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/scanner"
)

func book(t *testing.T, body string, mutate func(map[string]string)) []byte {
	t.Helper()
	files := map[string]string{
		"mimetype":               "application/epub+zip",
		"META-INF/container.xml": `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`,
		"book.opf":               `<package xmlns="http://www.idpf.org/2007/opf"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Test</dc:title><dc:language>ja</dc:language></metadata><manifest><item id="c" href="chapter.xhtml" media-type="application/xhtml+xml"/><item id="i" href="image.png" media-type="image/png"/></manifest><spine><itemref idref="c"/></spine></package>`,
		"chapter.xhtml":          body, "image.png": "image candidate",
	}
	if mutate != nil {
		mutate(files)
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	f, err := z.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(files["mimetype"])); err != nil {
		t.Fatal(err)
	}
	delete(files, "mimetype")
	for name, data := range files {
		f, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func write(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

type scanFunc func(context.Context, string) scanner.Result

func (f scanFunc) Scan(ctx context.Context, p string) scanner.Result { return f(ctx, p) }

func TestMeasuredEPUB(t *testing.T) {
	data := book(t, `<html><head><title>Ignore</title><style>x</style></head><body> A &amp; B 日本 <style>hidden</style></body></html>`, nil)
	p := write(t, "book.epub", data)
	h := sha256.Sum256(data)
	r, err := Check(context.Background(), p, Options{ExpectedBytes: int64(len(data)), ExpectedSHA256: strings.ToUpper(hex.EncodeToString(h[:]))}, nil)
	if err != nil || r.Status != "limited_checks_passed" || r.DetectedFormat != "epub" || r.Antivirus.Status != "not_scanned" {
		t.Fatal(r, err)
	}
	e := r.Checks.EPUB
	if e.TextCharacters != 5 || e.ReadingOrderDocuments != 1 || e.Images != 1 || e.Languages[0] != "ja" || e.Titles[0] != "Test" || e.ExpandedBytes == 0 || !strings.HasPrefix(e.PageCountStatus, "unknown") {
		t.Fatal(e)
	}
	got, _ := os.ReadFile(p)
	if !bytes.Equal(got, data) {
		t.Fatal("source mutated")
	}
}

func TestAssessmentRemainsWithinExplicitRoot(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "nested", "book.epub"), book(t, "<html><body>Knowledge</body></html>", nil), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	r, err := CheckInRoot(context.Background(), root, "nested/book.epub", Options{}, nil)
	if err != nil || r.Checks.EPUB == nil || r.Checks.EPUB.TextCharacters != 9 {
		t.Fatal(r, err)
	}
	for _, name := range []string{".", "../escape.pdf", "/absolute.pdf", `nested\book.epub`, "C:/escape.pdf", "bad\x00.pdf"} {
		if _, err := CheckInRoot(context.Background(), root, name, Options{}, nil); err == nil {
			t.Fatal("unsafe path accepted", name)
		}
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.pdf"), []byte("%PDF-1.7\n%%EOF"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "escape")); err != nil {
		t.Log("symlink privilege unavailable:", err)
		return
	}
	if _, err := CheckInRoot(context.Background(), root, "escape/secret.pdf", Options{}, nil); err == nil {
		t.Fatal("parent link escaped assessment root")
	}
}

func TestReviewAndMalformedFiles(t *testing.T) {
	for _, tc := range []struct {
		name           string
		data           []byte
		options        Options
		status, format string
	}{
		{"empty.epub", nil, Options{}, "incomplete", ""},
		{"spoof.epub", []byte("MZexecutable"), Options{}, "invalid_or_unsupported", "executable"},
		{"archive.epub", []byte("Rar!12345"), Options{}, "invalid_or_unsupported", "rar"},
		{"broken.epub", []byte("PK\x03\x04broken"), Options{}, "invalid_or_unsupported", "zip"},
		{"truncated.pdf", []byte("%PDF-1.7 no end"), Options{}, "invalid_or_unsupported", "pdf"},
		{"short.pdf", []byte("%PDF-1.7\n%%EOF"), Options{ExpectedBytes: 99}, "needs_review", "pdf"},
		{"hash.pdf", []byte("%PDF-1.7\n%%EOF"), Options{ExpectedSHA256: strings.Repeat("0", 64)}, "needs_review", "pdf"},
		{"wrong.epub", []byte("%PDF-1.7\n%%EOF"), Options{}, "needs_review", "pdf"},
		{"unknown.bin", []byte("unknown"), Options{}, "invalid_or_unsupported", "unknown"},
		{"emptytext.epub", book(t, `<html><body><img src="image.png"/></body></html>`, nil), Options{}, "needs_review", "epub"},
		{"active.epub", book(t, `<html><body onload="x()">Text<script>x()</script><iframe src="https://example.org"/></body></html>`, func(m map[string]string) {
			m["META-INF/encryption.xml"] = "encryption"
			m["book.opf"] = strings.Replace(m["book.opf"], `id="c"`, `id="c" properties="scripted"`, 1)
		}), Options{}, "needs_review", "epub"},
		{"encoding.epub", book(t, "<html><body>\xff</body></html>", nil), Options{}, "invalid_or_unsupported", "zip"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := Check(context.Background(), write(t, tc.name, tc.data), tc.options, nil)
			if err == nil || r.Status != tc.status || r.DetectedFormat != tc.format {
				t.Fatal(r, err)
			}
		})
	}
}

func TestSnapshotScannerAndCleanup(t *testing.T) {
	data := []byte("%PDF-1.7\n%%EOF")
	p := write(t, "test.pdf", data)
	var snapshot string
	av := scanFunc(func(ctx context.Context, name string) scanner.Result {
		snapshot = name
		got, err := os.ReadFile(name)
		if err != nil || !bytes.Equal(got, data) || name == p {
			t.Fatal("wrong snapshot", err)
		}
		return scanner.Result{Status: "no_detections_reported"}
	})
	r, err := Check(context.Background(), p, Options{Scan: true}, av)
	if err != nil || r.Antivirus.Status != "no_detections_reported" {
		t.Fatal(r, err)
	}
	if _, err := os.Stat(filepath.Dir(snapshot)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("temporary snapshot retained", err)
	}
	for _, status := range []string{"unavailable", "incomplete", "findings"} {
		r, err := Check(context.Background(), p, Options{Scan: true}, scanFunc(func(context.Context, string) scanner.Result { return scanner.Result{Status: status} }))
		if err == nil || r.Status != "needs_review" || r.Antivirus.Status != status {
			t.Fatal(r, err)
		}
	}
	r, err = Check(context.Background(), p, Options{Scan: true}, scanFunc(func(_ context.Context, name string) scanner.Result {
		if err := os.WriteFile(name, []byte("changed"), 0600); err != nil {
			t.Fatal(err)
		}
		return scanner.Result{Status: "no_detections_reported"}
	}))
	if err == nil || r.Status != "incomplete" {
		t.Fatal("scanner mutation hidden", r, err)
	}
}

func TestInputBoundariesAndCancellation(t *testing.T) {
	for _, o := range []Options{{ExpectedBytes: -1}, {ExpectedBytes: 256<<20 + 1}, {ExpectedSHA256: "xx"}, {ExpectedSHA256: "ff"}} {
		if o.Validate() == nil {
			t.Fatal("invalid option accepted", o)
		}
	}
	if _, err := Check(context.Background(), "missing", Options{ExpectedBytes: -1}, nil); err == nil {
		t.Fatal("invalid options accepted")
	}
	for _, p := range []string{filepath.Join(t.TempDir(), "missing"), t.TempDir()} {
		if _, err := Check(context.Background(), p, Options{}, nil); err == nil {
			t.Fatal("bad input accepted", p)
		}
	}
	p := write(t, "audio.mp3", []byte("ID3candidate"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Check(ctx, p, Options{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := (contextAt{ctx, bytes.NewReader([]byte("x"))}).ReadAt(make([]byte, 1), 0); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if r, err := Check(context.Background(), p, Options{}, nil); err != nil || r.Checks.Signature != "mp3_candidate_only" {
		t.Fatal(r, err)
	}
	link := filepath.Join(t.TempDir(), "link.mp3")
	if err := os.Symlink(p, link); err == nil {
		if _, err := Check(context.Background(), link, Options{}, nil); err == nil {
			t.Fatal("symlink accepted")
		}
	}
	large := write(t, "large.pdf", []byte("x"))
	if err := os.Truncate(large, 256<<20+1); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(context.Background(), large, Options{}, nil); err == nil {
		t.Fatal("large file accepted")
	}
}

func TestHTMLTokenizerLimitsAndIndicators(t *testing.T) {
	for _, body := range []string{`<html><body><object data="https://example.org"/>x</body></html>`, `<body><a href="javascript:x()">x</a></body>`, `<body><embed src="//example.org"/>x</body>`} {
		if n, a, err := htmlFacts([]byte(body)); err != nil || !a || n != 1 {
			t.Fatal(n, a, err)
		}
	}
	if _, _, err := htmlFacts([]byte("<body>" + strings.Repeat("x", 1<<20+1))); err == nil {
		t.Fatal("oversized token accepted")
	}
	if _, err := Inspect(bytes.NewReader(nil), 0, "epub"); err == nil {
		t.Fatal("empty accepted")
	}
}

func TestReferenceBudgetsAndCancellation(t *testing.T) {
	for _, href := range []string{"%ZZ", "/chapter.xhtml", "chapter.xhtml?query=1", "%5Cchapter.xhtml"} {
		data := book(t, "<body>Text</body>", func(m map[string]string) { m["book.opf"] = strings.Replace(m["book.opf"], "chapter.xhtml", href, 1) })
		if _, err := Inspect(bytes.NewReader(data), int64(len(data)), "epub"); err == nil {
			t.Fatal("bad reference accepted", href)
		}
	}
	data := book(t, "<body>Text</body>", func(m map[string]string) {
		m["chapter space.xhtml"] = m["chapter.xhtml"]
		delete(m, "chapter.xhtml")
		m["book.opf"] = strings.Replace(m["book.opf"], "chapter.xhtml", "chapter%20space.xhtml", 1)
	})
	if _, err := Inspect(bytes.NewReader(data), int64(len(data)), "epub"); err != nil {
		t.Fatal("valid escaped member rejected", err)
	}
	data = book(t, "<body>Text</body>", func(m map[string]string) {
		m["book.opf"] = strings.Replace(m["book.opf"], `<item id="i"`, `<item id="d" href="chapter.xhtml" media-type="application/xhtml+xml"/><item id="i"`, 1)
		m["book.opf"] = strings.Replace(m["book.opf"], `<itemref idref="c"/>`, `<itemref idref="c"/><itemref idref="c"/>`, 1)
	})
	c, err := Inspect(bytes.NewReader(data), int64(len(data)), "epub")
	if err != nil || c.EPUB.TextCharacters != 4 || !strings.Contains(strings.Join(c.Warnings, ";"), "repeated reading-order") {
		t.Fatal(c, err)
	}
	r := &metadataReader{r: bytes.NewReader([]byte("hello")), remaining: 2, limited: true}
	if _, err := r.ReadAt(make([]byte, 3), 0); err == nil {
		t.Fatal("metadata budget ignored")
	}
	r.limited = false
	if n, err := r.ReadAt(make([]byte, 3), 0); err != nil || n != 3 {
		t.Fatal(n, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := InspectContext(ctx, bytes.NewReader(data), int64(len(data)), "epub"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, _, err := htmlFactsContext(ctx, []byte("<body>Text</body>")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
