package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestGutenbergRecordOffersThePinnedEPUBs(t *testing.T) {
	const number = "1342"
	body := sampleRDF(number)
	g := &Gutenberg{FileClient: rdfClient(t, body, 200, nil)}
	got, err := g.EvaluateFiles(context.Background(), "gutenberg:"+number)
	if err != nil || !got.Complete || got.Access != GutenbergMirrorAccess || got.MetadataURL != gutenbergRDFURL(number) {
		t.Fatal(got, err)
	}
	sum := sha256.Sum256([]byte(body))
	if got.MetadataSHA256 != hex.EncodeToString(sum[:]) || got.Rights[0] != "Public domain in the USA." || got.Title[0] != "Pride and Prejudice" || got.Creators[0] != "Austen, Jane" || got.Languages[0] != "en" || got.LicenseURLs[0] != "http://www.gutenberg.org/license" {
		t.Fatal(got)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "automatically generated summary") {
		t.Fatal("stored a generated summary")
	}
	if len(got.Files) != 4 {
		t.Fatal(len(got.Files), got.Files)
	}
	noimages, images, text, main := got.Files[0], got.Files[1], got.Files[2], got.Files[3]
	wantNo, _ := gutenbergEPUBURL(number, "epub.noimages")
	wantImages, _ := gutenbergEPUBURL(number, "epub.images")
	if noimages.Name != "1342.epub.noimages" || noimages.Access != GutenbergEPUBAccess || noimages.DownloadURL != wantNo || *noimages.Bytes != 561111 || noimages.Format != "application/epub+zip" || noimages.MD5 != "" || noimages.SHA1 != "" {
		t.Fatal(noimages)
	}
	if images.Name != "1342.epub.images" || images.DownloadURL != wantImages || images.DownloadURL == noimages.DownloadURL {
		t.Fatal(images)
	}
	if text.Name != "1342.txt.utf-8" || text.Access != GutenbergListedAccess || text.DownloadURL != "" || text.Format != "text/plain; charset=utf-8" {
		t.Fatal(text)
	}
	if main.DownloadURL != "" || main.Access != GutenbergListedAccess {
		t.Fatal(main)
	}
	if _, err := g.Download(context.Background(), text); err == nil {
		t.Fatal("listed text was downloaded")
	}
}

func TestGutenbergRecordFailsClosed(t *testing.T) {
	g := &Gutenberg{}
	full := sampleRDF("1342")
	cases := []struct {
		name, id, body, want string
	}{
		{"truncated", "gutenberg:1342", full[:len(full)/2], "truncated"},
		{"wrong text", "gutenberg:1342", strings.ReplaceAll(full, "ebooks/1342", "ebooks/999"), "not text 1342"},
		{"doctype", "gutenberg:1342", `<!DOCTYPE rdf [<!ENTITY x "y">]>` + full, "document type"},
		{"duplicate", "gutenberg:1342", strings.Replace(full, "1342.epub.images", "1342.epub.noimages", 1), "repeats a file name"},
	}
	for _, tc := range cases {
		g.FileClient = rdfClient(t, tc.body, 200, nil)
		if _, err := g.EvaluateFiles(context.Background(), tc.id); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatal(tc.name, err)
		}
	}
	plain := strings.Replace(full, "application/epub+zip", "text/plain", 1)
	g.FileClient = rdfClient(t, plain, 200, nil)
	got, err := g.EvaluateFiles(context.Background(), "gutenberg:1342")
	if err != nil || got.Files[0].Access != GutenbergListedAccess || got.Files[0].DownloadURL != "" || !strings.Contains(got.Files[0].Findings[0], "application/epub+zip") {
		t.Fatal(got.Files[0], err)
	}
}

func TestGutenbergMirrorMissDoesNotFallBack(t *testing.T) {
	saved := recordPause
	recordPause = func(int) time.Duration { return 0 }
	t.Cleanup(func() { recordPause = saved })
	calls := []string{}
	g := &Gutenberg{FileClient: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.URL.String())
		return nil, io.ErrUnexpectedEOF
	})}}
	if _, err := g.EvaluateFiles(context.Background(), "gutenberg:1342"); err == nil || !strings.Contains(err.Error(), GutenbergMirrorHost) {
		t.Fatal(err)
	}
	// A dropped connection is tried again at the mirror, never elsewhere.
	if len(calls) != metadataAttempts {
		t.Fatal(calls)
	}
	for _, c := range calls {
		if c != gutenbergRDFURL("1342") || strings.Contains(c, "www.gutenberg.org") {
			t.Fatal(calls)
		}
	}
	g.FileClient = rdfClient(t, "", 404, nil)
	if _, err := g.EvaluateFiles(context.Background(), "gutenberg:1342"); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatal(err)
	}
}

func TestGutenbergDownloadUsesTheNamedObject(t *testing.T) {
	var got *http.Request
	g := &Gutenberg{FileClient: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		got = r
		return &http.Response{StatusCode: 206, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}}
	file := OfferedFile{Name: "1342.epub.noimages", Access: GutenbergEPUBAccess}
	file.DownloadURL, _ = gutenbergEPUBURL("1342", "epub.noimages")
	if _, err := g.DownloadFrom(context.Background(), file, 10, `"6abe5e40-88fce"`); err != nil {
		t.Fatal(err)
	}
	if got.URL.String() != file.DownloadURL || got.Header.Get("Range") != "bytes=10-" || got.Header.Get("If-Range") != `"6abe5e40-88fce"` || got.Header.Get("Accept-Encoding") != "identity" {
		t.Fatal(got.URL, got.Header)
	}
	other := file
	other.DownloadURL = "https://www.gutenberg.org/cache/epub/1342/pg1342.epub"
	if _, err := g.Download(context.Background(), other); err == nil {
		t.Fatal("website host admitted")
	}
	images := file
	images.DownloadURL, _ = gutenbergEPUBURL("1342", "epub.images")
	if _, err := g.Download(context.Background(), images); err == nil {
		t.Fatal("a different EPUB object was admitted for the no-images name")
	}
	if _, err := g.DownloadFrom(context.Background(), file, 10, `W/"weak"`); err == nil {
		t.Fatal("weak validator resumed")
	}
}

func sampleRDF(number string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<rdf:RDF xml:base="http://www.gutenberg.org/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:pgterms="http://www.gutenberg.org/2009/pgterms/">
  <pgterms:ebook rdf:about="ebooks/` + number + `">
    <dcterms:publisher>Project Gutenberg</dcterms:publisher>
    <dcterms:license rdf:resource="license"/>
    <dcterms:issued>1998-06-01</dcterms:issued>
    <dcterms:rights>Public domain in the USA.</dcterms:rights>
    <dcterms:creator><pgterms:agent rdf:about="2009/agents/68"><pgterms:name>Austen, Jane</pgterms:name></pgterms:agent></dcterms:creator>
    <dcterms:title>Pride and Prejudice</dcterms:title>
    <pgterms:marc520>automatically generated summary should not be stored</pgterms:marc520>
    <dcterms:language><rdf:Description><rdf:value>en</rdf:value></rdf:Description></dcterms:language>
    <dcterms:hasFormat><pgterms:file rdf:about="https://www.gutenberg.org/ebooks/` + number + `.epub.noimages"><dcterms:extent>561111</dcterms:extent><dcterms:format><rdf:Description><rdf:value>application/epub+zip</rdf:value></rdf:Description></dcterms:format></pgterms:file></dcterms:hasFormat>
    <dcterms:hasFormat><pgterms:file rdf:about="https://www.gutenberg.org/ebooks/` + number + `.epub.images"><dcterms:extent>2000</dcterms:extent><dcterms:format><rdf:Description><rdf:value>application/epub+zip</rdf:value></rdf:Description></dcterms:format></pgterms:file></dcterms:hasFormat>
    <dcterms:hasFormat><pgterms:file rdf:about="https://www.gutenberg.org/ebooks/` + number + `.txt.utf-8"><dcterms:extent>50</dcterms:extent><dcterms:format><rdf:Description><rdf:value>text/plain; charset=utf-8</rdf:value></rdf:Description></dcterms:format></pgterms:file></dcterms:hasFormat>
    <dcterms:hasFormat><pgterms:file rdf:about="https://www.gutenberg.org/files/` + number + `/` + number + `-0.txt"><dcterms:extent>40</dcterms:extent><dcterms:format><rdf:Description><rdf:value>text/plain</rdf:value></rdf:Description></dcterms:format></pgterms:file></dcterms:hasFormat>
  </pgterms:ebook>
</rdf:RDF>`
}

func rdfClient(t *testing.T, body string, status int, err error) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r, ContentLength: int64(len(body))}, nil
	})}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
