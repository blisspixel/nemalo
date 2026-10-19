package acquisition

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/discovery"
)

func TestAcquireGutenbergEPUBChecksTheResponseLength(t *testing.T) {
	body := minimalEPUB(t)
	extent := int64(len(body) + 9)
	stub := &mirrorStub{t: t, rdf: mirrorRDF("1342", extent, 2000), body: body, etag: `"6abe5e40-88fce"`}
	r := gutenbergRequest(t)
	result, err := Acquire(context.Background(), stub.provider(), r)
	if err != nil || !result.Complete || result.Status != "untrusted_intake" || !result.StrongValidator || result.Checks == nil || result.Checks.EPUB == nil {
		t.Fatal(result, err)
	}
	if result.ContentPath != filepath.Join(r.Output, "content.epub") {
		t.Fatal(result.ContentPath)
	}
	got, err := os.ReadFile(result.ContentPath)
	sum := sha256.Sum256(body)
	if err != nil || !bytes.Equal(got, body) || result.Transfer.SHA256 != hex.EncodeToString(sum[:]) || result.Transfer.Bytes != int64(len(body)) {
		t.Fatal(err, result.Transfer)
	}
	note := fmt.Sprintf("The bibliographic record declared %d bytes. The response length was %d. The transfer checked the response length. The record has no checksum.", extent, len(body))
	if result.SizeNote != note {
		t.Fatal(result.SizeNote)
	}
	raw, err := os.ReadFile(filepath.Join(r.Output, "receipt.json"))
	if err != nil || strings.Contains(string(raw), "strong_validator") || strings.Contains(string(raw), "automatically generated") {
		t.Fatal(err, string(raw))
	}
	receipt := readReceipt(t, r.Output)
	if receipt.SizeNote != note || receipt.Status != "untrusted_intake" || len(receipt.Intent.Selection.Files) != 1 || receipt.Intent.Selection.Files[0].Name != "1342.epub.noimages" {
		t.Fatal(receipt)
	}
	var rec transferRecord
	if err := json.Unmarshal(mustRead(t, filepath.Join(r.Output, "transfer.json")), &rec); err != nil || rec.ContentLength != int64(len(body)) || !rec.StrongETag || rec.ETag != stub.etag {
		t.Fatal(rec, err)
	}
	if len(stub.calls) != 2 || !strings.HasSuffix(stub.calls[0], "/pg1342.rdf") || !strings.HasSuffix(stub.calls[1], "/pg1342.epub") {
		t.Fatal(stub.calls)
	}
	for _, call := range stub.calls {
		if strings.Contains(call, "www.gutenberg.org") || strings.Contains(call, "-images") {
			t.Fatal("unselected or website object requested", call)
		}
	}
	if _, err := Acquire(context.Background(), stub.provider(), r); err == nil {
		t.Fatal("completed packet replaced")
	}
}

func TestAcquireGutenbergRefusesAnythingButTheNamedEPUB(t *testing.T) {
	stub := &mirrorStub{t: t, rdf: mirrorRDF("1342", 100, 2000), body: []byte("unused")}
	for _, name := range []string{"1342.txt.utf-8", "13421.epub.noimages", "1342.epub"} {
		r := gutenbergRequest(t)
		r.File = name
		if _, err := Acquire(context.Background(), stub.provider(), r); err == nil || !strings.Contains(err.Error(), "1342.epub.noimages") {
			t.Fatal(name, err)
		}
		if _, err := os.Lstat(r.Output); !os.IsNotExist(err) {
			t.Fatal("rejected name created a packet", name, err)
		}
	}
	if len(stub.calls) != 0 {
		t.Fatal("validation reached the mirror", stub.calls)
	}
	plain := strings.Replace(mirrorRDF("1342", 100, 2000), "application/epub+zip", "text/plain", 1)
	stub.rdf = plain
	r := gutenbergRequest(t)
	if _, err := Acquire(context.Background(), stub.provider(), r); err == nil || !strings.Contains(err.Error(), "offered Gutenberg EPUB") {
		t.Fatal(err)
	}
	if _, err := os.Lstat(r.Output); !os.IsNotExist(err) {
		t.Fatal("a listed non-EPUB created a packet", err)
	}
	if len(stub.calls) != 1 || !strings.HasSuffix(stub.calls[0], ".rdf") {
		t.Fatal(stub.calls)
	}
}

func TestAcquireGutenbergMissingMirrorWritesNothing(t *testing.T) {
	stub := &mirrorStub{t: t, fail: true}
	r := gutenbergRequest(t)
	if _, err := Acquire(context.Background(), stub.provider(), r); err == nil || !strings.Contains(err.Error(), "gutenberg.pglaf.org") {
		t.Fatal(err)
	}
	if _, err := os.Lstat(r.Output); !os.IsNotExist(err) {
		t.Fatal("mirror miss created a packet", err)
	}
	// A dropped connection is tried again at the mirror only.
	if len(stub.calls) < 1 {
		t.Fatal(stub.calls)
	}
	for _, c := range stub.calls {
		if strings.Contains(c, "www.gutenberg.org") || !strings.HasSuffix(c, "/pg1342.rdf") {
			t.Fatal(stub.calls)
		}
	}
	stub.fail = false
	stub.status = 404
	r = gutenbergRequest(t)
	if _, err := Acquire(context.Background(), stub.provider(), r); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatal(err)
	}
	if _, err := os.Lstat(r.Output); !os.IsNotExist(err) {
		t.Fatal("HTTP miss created a packet", err)
	}
}

func TestAcquireGutenbergRequiresAResponseLengthBeforeTheTransferRecord(t *testing.T) {
	body := minimalEPUB(t)
	missing := int64(-1)
	stub := &mirrorStub{t: t, rdf: mirrorRDF("1342", 100, 2000), body: body, forceLength: &missing}
	r := gutenbergRequest(t)
	if _, err := Acquire(context.Background(), stub.provider(), r); err == nil || !strings.Contains(err.Error(), "Content-Length") {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r.Output, "intent.json")); err != nil {
		t.Fatal("intent lost", err)
	}
	if _, err := os.Stat(filepath.Join(r.Output, "transfer.json")); !os.IsNotExist(err) {
		t.Fatal("transfer record written without a length", err)
	}
	over := int64(r.MaxBytes + 1)
	stub.forceLength = &over
	r = gutenbergRequest(t)
	if _, err := Acquire(context.Background(), stub.provider(), r); err == nil || !strings.Contains(err.Error(), "Content-Length") {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r.Output, "transfer.json")); !os.IsNotExist(err) {
		t.Fatal("over-budget length recorded", err)
	}
}

func TestAcquireGutenbergWithoutAStrongValidatorCannotResume(t *testing.T) {
	body := minimalEPUB(t)
	stub := &mirrorStub{t: t, rdf: mirrorRDF("1342", int64(len(body)), 2000), body: body, cut: 40}
	r := gutenbergRequest(t)
	result, err := Acquire(context.Background(), stub.provider(), r)
	if err == nil || result.StrongValidator || !result.TransferRecorded || result.Transfer.Bytes == 0 {
		t.Fatal(result, err)
	}
	if info, statErr := os.Stat(filepath.Join(r.Output, "content.epub")); statErr != nil || info.Size() != 40 {
		t.Fatal("partial epub not retained", statErr)
	}
	before := mustRead(t, filepath.Join(r.Output, "content.epub"))
	r.Resume = true
	if _, err := Acquire(context.Background(), stub.provider(), r); err == nil || !strings.Contains(err.Error(), "strong validator") || !strings.Contains(err.Error(), "start a new output") {
		t.Fatal(err)
	}
	if got := mustRead(t, filepath.Join(r.Output, "content.epub")); string(got) != string(before) {
		t.Fatal("refused resume changed the packet")
	}
	if _, err := os.Stat(filepath.Join(r.Output, "receipt.json")); !os.IsNotExist(err) {
		t.Fatal("resume wrote a receipt", err)
	}
	if len(stub.ranges) != 0 {
		t.Fatal("range requested without a strong validator", stub.ranges)
	}
}

func TestAcquireGutenbergResumeUsesTheRecordedResponseLength(t *testing.T) {
	body := minimalEPUB(t)
	extent := int64(len(body) + 9)
	stub := &mirrorStub{t: t, rdf: mirrorRDF("1342", extent, 2000), body: body, etag: `"6abe5e40-88fce"`, cut: 40}
	r := gutenbergRequest(t)
	saved := maxContinuations
	maxContinuations = 0
	if _, err := Acquire(context.Background(), stub.provider(), r); err == nil {
		t.Fatal("cut transfer succeeded")
	}
	maxContinuations = saved
	stub.cut = 0
	r.Resume = true
	result, err := Acquire(context.Background(), stub.provider(), r)
	note := fmt.Sprintf("The bibliographic record declared %d bytes. The response length was %d. The transfer checked the response length. The record has no checksum.", extent, len(body))
	if err != nil || !result.Complete || result.ResumedFrom != 40 || result.SizeNote != note {
		t.Fatal(result, err)
	}
	if got := mustRead(t, filepath.Join(r.Output, "content.epub")); !bytes.Equal(got, body) {
		t.Fatal("resumed bytes differ", len(got), len(body))
	}
	if len(stub.ranges) != 1 || stub.ranges[0] != `bytes=40-|"6abe5e40-88fce"` {
		t.Fatal(stub.ranges)
	}
	if readReceipt(t, r.Output).SizeNote != note {
		t.Fatal("receipt omitted the size note")
	}
}

func TestAcquireGutenbergDetectionQuarantinesTheEPUB(t *testing.T) {
	body := minimalEPUB(t)
	stub := &mirrorStub{t: t, rdf: mirrorRDF("1342", int64(len(body)), 2000), body: body, etag: `"6abe5e40-88fce"`}
	r := gutenbergRequest(t)
	r.Quarantine = filepath.Join(t.TempDir(), "quarantine")
	r.Scanner = status("findings")
	result, err := Acquire(context.Background(), stub.provider(), r)
	if !errors.Is(err, ErrDetected) || result.Status != "detected_quarantined" || result.Complete {
		t.Fatal(result, err)
	}
	if _, statErr := os.Stat(filepath.Join(r.Output, "content.epub")); !os.IsNotExist(statErr) {
		t.Fatal("detected epub left in the packet", statErr)
	}
	got, readErr := os.ReadFile(result.QuarantinedPath)
	if readErr != nil || !bytes.Equal(got, body) || !strings.HasSuffix(result.QuarantinedPath, "content.epub.quarantined") {
		t.Fatal(result.QuarantinedPath, readErr)
	}
	if len(result.Alternatives) != 1 || result.Alternatives[0].File != "1342.epub.images" || result.Alternatives[0].Format != "epub" {
		t.Fatal(result.Alternatives)
	}
	if result.SearchSuggestion != "Pride and Prejudice Austen, Jane" {
		t.Fatal(result.SearchSuggestion)
	}
	for _, call := range stub.calls {
		if strings.Contains(call, "-images") {
			t.Fatal("unselected epub downloaded", call)
		}
	}
}

type mirrorStub struct {
	t           *testing.T
	rdf         string
	body        []byte
	etag        string
	cut         int
	status      int
	fail        bool
	forceLength *int64
	calls       []string
	ranges      []string
}

func (m *mirrorStub) provider() discovery.Files {
	return discovery.Files{Gutenberg: &discovery.Gutenberg{FileClient: &http.Client{Transport: m}}}
}

func (m *mirrorStub) RoundTrip(r *http.Request) (*http.Response, error) {
	m.t.Helper()
	m.calls = append(m.calls, r.URL.String())
	if m.fail {
		return nil, io.ErrUnexpectedEOF
	}
	if m.status != 0 {
		return &http.Response{StatusCode: m.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r, ContentLength: 0}, nil
	}
	if strings.HasSuffix(r.URL.Path, ".rdf") {
		return &http.Response{StatusCode: 200, ContentLength: int64(len(m.rdf)), Header: http.Header{}, Body: io.NopCloser(strings.NewReader(m.rdf)), Request: r}, nil
	}
	if !strings.HasSuffix(r.URL.Path, "/pg1342.epub") {
		m.t.Fatalf("unexpected object %s", r.URL)
	}
	h := http.Header{}
	if m.etag != "" {
		h.Set("ETag", m.etag)
	}
	length := int64(len(m.body))
	if m.forceLength != nil {
		length = *m.forceLength
	}
	if rng := r.Header.Get("Range"); rng != "" {
		m.ranges = append(m.ranges, rng+"|"+r.Header.Get("If-Range"))
		offset, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(rng, "bytes="), "-"), 10, 64)
		if err != nil || offset < 0 || offset > int64(len(m.body)) {
			m.t.Fatal(rng)
		}
		rest := m.body[offset:]
		h.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, len(m.body)-1, len(m.body)))
		return &http.Response{StatusCode: 206, ContentLength: int64(len(rest)), Header: h, Body: io.NopCloser(bytes.NewReader(rest)), Request: r}, nil
	}
	return &http.Response{StatusCode: 200, ContentLength: length, Header: h, Body: io.NopCloser(&cutReader{r: bytes.NewReader(m.body), cut: m.cut}), Request: r}, nil
}

func gutenbergRequest(t *testing.T) Request {
	t.Helper()
	return Request{SourceID: "gutenberg:1342", File: "1342.epub.noimages", Output: filepath.Join(t.TempDir(), "packet"), MaxBytes: 1 << 20}
}

func mirrorRDF(number string, noimages, images int64) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<rdf:RDF xml:base="http://www.gutenberg.org/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:pgterms="http://www.gutenberg.org/2009/pgterms/">
  <pgterms:ebook rdf:about="ebooks/%s">
    <dcterms:publisher>Project Gutenberg</dcterms:publisher>
    <dcterms:license rdf:resource="license"/>
    <dcterms:rights>Public domain in the USA.</dcterms:rights>
    <dcterms:creator><pgterms:agent><pgterms:name>Austen, Jane</pgterms:name></pgterms:agent></dcterms:creator>
    <dcterms:title>Pride and Prejudice</dcterms:title>
    <pgterms:marc520>automatically generated summary should not be stored</pgterms:marc520>
    <dcterms:hasFormat><pgterms:file rdf:about="https://www.gutenberg.org/ebooks/%s.epub.noimages"><dcterms:extent>%d</dcterms:extent><dcterms:format><rdf:Description><rdf:value>application/epub+zip</rdf:value></rdf:Description></dcterms:format></pgterms:file></dcterms:hasFormat>
    <dcterms:hasFormat><pgterms:file rdf:about="https://www.gutenberg.org/ebooks/%s.epub.images"><dcterms:extent>%d</dcterms:extent><dcterms:format><rdf:Description><rdf:value>application/epub+zip</rdf:value></rdf:Description></dcterms:format></pgterms:file></dcterms:hasFormat>
    <dcterms:hasFormat><pgterms:file rdf:about="https://www.gutenberg.org/ebooks/%s.txt.utf-8"><dcterms:extent>50</dcterms:extent><dcterms:format><rdf:Description><rdf:value>text/plain; charset=utf-8</rdf:value></rdf:Description></dcterms:format></pgterms:file></dcterms:hasFormat>
  </pgterms:ebook>
</rdf:RDF>`, number, number, noimages, number, images, number)
}

func minimalEPUB(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	files := []struct{ name, body string }{
		{"mimetype", "application/epub+zip"},
		{"META-INF/container.xml", `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`},
		{"book.opf", `<package xmlns="http://www.idpf.org/2007/opf"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Pride and Prejudice</dc:title><dc:language>en</dc:language></metadata><manifest><item id="c" href="chapter.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="c"/></spine></package>`},
		{"chapter.xhtml", "<html><body>It is a truth universally acknowledged.</body></html>"},
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
	return out.Bytes()
}

func mustRead(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
