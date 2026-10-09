package acquisition

import (
	"archive/zip"
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

	"github.com/blisspixel/nemalo/internal/discovery"
)

func TestAcquireSupportedEPUBAndAudioUseSharedChecks(t *testing.T) {
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	files := []struct{ name, body string }{
		{"mimetype", "application/epub+zip"},
		{"META-INF/container.xml", `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`},
		{"book.opf", `<package xmlns="http://www.idpf.org/2007/opf"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>日本語</dc:title><dc:language>ja</dc:language></metadata><manifest><item id="c" href="chapter.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="c"/></spine></package>`},
		{"chapter.xhtml", "<html><body>知識を育てる</body></html>"},
	}
	for _, file := range files {
		method := uint16(zip.Deflate)
		if file.name == "mimetype" {
			method = zip.Store
		}
		w, err := z.CreateHeader(&zip.FileHeader{Name: file.name, Method: method})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, file.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ format, body string }{{"epub", out.String()}, {"mp3", "ID3audio candidate"}} {
		p, r := fixture(), request(t)
		p.body = item.body
		p.length = int64(len(item.body))
		n := p.length
		md := md5.Sum([]byte(item.body))
		r.File = "book." + item.format
		r.MaxBytes = 16384
		p.e.Files[0] = discovery.OfferedFile{Name: r.File, Bytes: &n, MD5: hex.EncodeToString(md[:]), Access: "public_file_candidate", DownloadURL: "https://archive.org/download/demo/" + r.File}
		result, err := Acquire(context.Background(), p, r)
		if err != nil || !result.Complete || result.Checks == nil {
			t.Fatal(result, err)
		}
		if item.format == "epub" && (result.Checks.EPUB == nil || result.Checks.EPUB.Titles[0] != "日本語") {
			t.Fatal("EPUB metadata lost", result.Checks)
		}
		if item.format == "mp3" && result.Checks.Signature != "mp3_candidate_only" {
			t.Fatal("audio evidence overstated", result.Checks)
		}
	}
}

type fixtureProvider struct {
	e                      discovery.Evaluation
	body                   string
	status                 int
	length                 int64
	encoding               string
	evalErr, downloadErr   error
	evaluations, downloads int
	beforeDownload         func()
}

func fixture() *fixtureProvider {
	body := "%PDF-1.7\n%%EOF"
	n := int64(len(body))
	md := md5.Sum([]byte(body))
	return &fixtureProvider{body: body, status: 200, length: n, e: discovery.Evaluation{Complete: true, ID: "archive:demo", Source: "archive", Access: "no_item_restriction_declared", Title: []string{"A source title"}, Rights: []string{"Unverified declaration"}, Files: []discovery.OfferedFile{{Name: "folder/book.PDF", Bytes: &n, MD5: hex.EncodeToString(md[:]), Access: "public_file_candidate", DownloadURL: "https://archive.org/download/demo/folder/book.PDF"}}}}
}
func (p *fixtureProvider) Evaluate(context.Context, string) (discovery.Evaluation, error) {
	p.evaluations++
	return p.e, p.evalErr
}
func (p *fixtureProvider) Download(context.Context, discovery.OfferedFile) (*http.Response, error) {
	p.downloads++
	if p.beforeDownload != nil {
		p.beforeDownload()
	}
	if p.downloadErr != nil {
		return nil, p.downloadErr
	}
	return &http.Response{StatusCode: p.status, ContentLength: p.length, Header: http.Header{"Content-Encoding": []string{p.encoding}}, Body: io.NopCloser(strings.NewReader(p.body))}, nil
}
func request(t *testing.T) Request {
	t.Helper()
	return Request{SourceID: "archive:demo", File: "folder/book.PDF", Output: filepath.Join(t.TempDir(), "packet"), MaxBytes: 1000}
}

func TestAcquirePreservesIntentAndVerifiedIntake(t *testing.T) {
	p, r := fixture(), request(t)
	p.beforeDownload = func() {
		b, err := os.ReadFile(filepath.Join(r.Output, "intent.json"))
		if err != nil {
			t.Fatal("request preceded durable intent", err)
		}
		var intent Intent
		if err := json.Unmarshal(b, &intent); err != nil || intent.Request.File != r.File || len(intent.Selection.Files) != 1 {
			t.Fatal(intent, err)
		}
	}
	result, err := Acquire(context.Background(), p, r)
	if err != nil || !result.Complete || !result.TransferComplete || result.Status != "untrusted_intake" || result.Antivirus != "not_scanned" || result.Checks == nil || p.evaluations != 1 || p.downloads != 1 {
		t.Fatal(result, err)
	}
	bytes, err := os.ReadFile(result.ContentPath)
	if err != nil || string(bytes) != p.body {
		t.Fatal(err, string(bytes))
	}
	data, err := os.ReadFile(filepath.Join(r.Output, "receipt.json"))
	if err != nil {
		t.Fatal(err)
	}
	var receipt Result
	if err := json.Unmarshal(data, &receipt); err != nil || receipt.Transfer.SHA256 != result.Transfer.SHA256 || !receipt.Complete {
		t.Fatal(receipt, err)
	}
	if _, err := Acquire(context.Background(), p, r); err == nil || p.evaluations != 1 || p.downloads != 1 {
		t.Fatal("existing packet overwritten or requested", err)
	}
	data2, _ := os.ReadFile(filepath.Join(r.Output, "receipt.json"))
	if string(data2) != string(data) {
		t.Fatal("receipt changed")
	}
}

func TestAcquireRejectsSourceEvidenceBeforeWrites(t *testing.T) {
	for _, mutate := range []func(*fixtureProvider){
		func(p *fixtureProvider) { p.evalErr = errors.New("offline") },
		func(p *fixtureProvider) { p.e.Complete = false },
		func(p *fixtureProvider) { p.e.ID = "archive:other" },
		func(p *fixtureProvider) { p.e.Source = "other" },
		func(p *fixtureProvider) { p.e.Access = "restricted_or_uncertain" },
		func(p *fixtureProvider) { p.e.Files = nil },
		func(p *fixtureProvider) { p.e.Files = append(p.e.Files, p.e.Files[0]) },
		func(p *fixtureProvider) { p.e.Files[0].Access = "restricted_or_uncertain" },
		func(p *fixtureProvider) { p.e.Files[0].DownloadURL = "" },
		func(p *fixtureProvider) { p.e.Files[0].Bytes = nil },
		func(p *fixtureProvider) { n := int64(0); p.e.Files[0].Bytes = &n },
		func(p *fixtureProvider) { n := int64(1001); p.e.Files[0].Bytes = &n },
		func(p *fixtureProvider) { p.e.Files[0].MD5 = "" },
		func(p *fixtureProvider) { p.e.Files[0].MD5 = "malformed" },
	} {
		p, r := fixture(), request(t)
		mutate(p)
		if result, err := Acquire(context.Background(), p, r); err == nil || result.Complete || p.downloads != 0 {
			t.Fatal(result, err)
		}
		if _, err := os.Lstat(r.Output); !os.IsNotExist(err) {
			t.Fatal("failed source selection created output", err)
		}
	}
}

func TestAcquireRetainsFailuresWithoutReceipt(t *testing.T) {
	for _, mutate := range []func(*fixtureProvider){
		func(p *fixtureProvider) { p.downloadErr = errors.New("interrupted") },
		func(p *fixtureProvider) { p.status = 403 },
		func(p *fixtureProvider) { p.status = 206 },
		func(p *fixtureProvider) { p.length = 1001 },
		func(p *fixtureProvider) { p.length = 1 },
		func(p *fixtureProvider) { p.encoding = "gzip" },
		func(p *fixtureProvider) { p.body = "changed"; p.length = -1 },
		func(p *fixtureProvider) { p.body = strings.Repeat("x", 1002); p.length = -1 },
		func(p *fixtureProvider) { p.e.Files[0].MD5 = strings.Repeat("0", 32) },
		func(p *fixtureProvider) {
			p.body = "invalid file!"
			n := int64(len(p.body))
			p.length = n
			p.e.Files[0].Bytes = &n
			md := md5.Sum([]byte(p.body))
			p.e.Files[0].MD5 = hex.EncodeToString(md[:])
		},
	} {
		p, r := fixture(), request(t)
		mutate(p)
		result, err := Acquire(context.Background(), p, r)
		if err == nil || result.Complete || result.Output == "" {
			t.Fatal(result, err)
		}
		if _, err := os.Stat(filepath.Join(r.Output, "intent.json")); err != nil {
			t.Fatal("intent lost", err)
		}
		if _, err := os.Stat(filepath.Join(r.Output, "receipt.json")); !os.IsNotExist(err) {
			t.Fatal("failed packet marked complete", err)
		}
		if _, err := Acquire(context.Background(), p, r); err == nil {
			t.Fatal("failed packet silently reused")
		}
	}
}

func TestRequestBoundariesAndCancellation(t *testing.T) {
	for _, mutate := range []func(*Request){
		func(r *Request) { r.SourceID = "http://localhost" }, func(r *Request) { r.File = "../book.epub" }, func(r *Request) { r.File = "a\\book.epub" }, func(r *Request) { r.File = "." }, func(r *Request) { r.File = "x.zip" }, func(r *Request) { r.File = "a\x00.epub" }, func(r *Request) { r.File = strings.Repeat("x", 1001) + ".epub" }, func(r *Request) { r.Output = "" }, func(r *Request) { r.MaxBytes = 0 }, func(r *Request) { r.MaxBytes = 256<<20 + 1 },
	} {
		r := request(t)
		mutate(&r)
		if r.Validate() == nil {
			t.Fatal("invalid request accepted", r)
		}
	}
	p, r := fixture(), request(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Acquire(ctx, p, r); !errors.Is(err, context.Canceled) || p.evaluations != 0 {
		t.Fatal(err)
	}
	r.Output = filepath.Join(t.TempDir(), "missing", "packet")
	if _, err := Acquire(context.Background(), p, r); err == nil || p.evaluations != 0 {
		t.Fatal("missing parent accepted", err)
	}
	p, r = fixture(), request(t)
	ctx, cancel = context.WithCancel(context.Background())
	p.beforeDownload = cancel
	result, err := Acquire(ctx, p, r)
	if !errors.Is(err, context.Canceled) || result.Complete {
		t.Fatal(result, err)
	}
	if _, err := os.Stat(filepath.Join(r.Output, "intent.json")); err != nil {
		t.Fatal("cancel removed intent", err)
	}
}

func TestRecordsAndContentReplacement(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := writeRecord(root, "test.json", map[string]string{"v": "x"}); err != nil {
		t.Fatal(err)
	}
	if err := writeRecord(root, "test.json", map[string]string{"v": "replacement"}); err == nil {
		t.Fatal("overwrote record")
	}
	if err := writeRecord(root, "large.json", strings.Repeat("x", 4<<20)); err == nil {
		t.Fatal("unbounded record")
	}
	if err := writeRecord(root, "bad.json", make(chan int)); err == nil {
		t.Fatal("marshal error hidden")
	}
	f, err := root.OpenFile("content", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := sameContent(root, "content", f); err != nil {
		t.Fatal(err)
	}
	if err := sameContent(root, "missing", f); err == nil {
		t.Fatal("missing accepted")
	}
	if err := root.Rename("content", "moved"); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile("content", []byte("other"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := sameContent(root, "content", f); err == nil {
		t.Fatal("replacement accepted")
	}
}
