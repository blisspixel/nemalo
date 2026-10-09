package discovery

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type downloadRoundTrip func(*http.Request) (*http.Response, error)

func (f downloadRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFileClientBoundaries(t *testing.T) {
	client := NewFileClient(ArchiveDownloadHost)
	for _, raw := range []string{"http://archive.org/file", "https://evil.invalid/file", "https://archive.org:443/file", "https://user@archive.org/file", "https://archive.org/file#fragment", "https://127.0.0.1/file", "https://ia800308.us.archive.org.evil.invalid/file"} {
		req, _ := http.NewRequest(http.MethodGet, raw, nil)
		if _, err := client.Transport.RoundTrip(req); err == nil {
			t.Fatal("unapproved initial request admitted", raw)
		}
		if err := client.CheckRedirect(req, []*http.Request{{}}); err == nil {
			t.Fatal("unapproved redirect admitted", raw)
		}
	}
	for _, host := range []string{"archive.org", "www.archive.org", "ia800308.us.archive.org", "dn800308.ca.archive.org"} {
		u := &url.URL{Scheme: "https", Host: host, Path: "/file"}
		if err := client.CheckRedirect(&http.Request{URL: u}, []*http.Request{{}}); err != nil {
			t.Fatal(host, err)
		}
	}
	if err := client.CheckRedirect(&http.Request{URL: &url.URL{Scheme: "https", Host: "archive.org"}}, make([]*http.Request, 5)); err == nil {
		t.Fatal("redirect limit absent")
	}
	transport := client.Transport.(fileTransport).base.(*http.Transport)
	if transport.Proxy != nil || !transport.DisableCompression || client.Timeout != 5*time.Minute || transport.ResponseHeaderTimeout == 0 {
		t.Fatal("unbounded or proxy/decompression policy")
	}
	for _, address := range []string{"evil.invalid:443", "archive.org:80", "bad"} {
		if _, err := transport.DialContext(context.Background(), "tcp", address); err == nil {
			t.Fatal("unapproved dial", address)
		}
	}
}

func TestArchiveDownloadSelectionAndQuota(t *testing.T) {
	p := NewArchive()
	calls := 0
	p.downloadClient = &http.Client{Transport: downloadRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != "https://archive.org/download/demo/a%20book.pdf" || r.Header.Get("Accept-Encoding") != "identity" || !strings.HasPrefix(r.UserAgent(), "nemalo/") {
			t.Error(r.URL, r.Header)
		}
		return &http.Response{StatusCode: 503, Header: http.Header{"Retry-After": []string{"60"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	file := OfferedFile{Name: "a book.pdf", Access: "public_file_candidate", DownloadURL: "https://archive.org/download/demo/a%20book.pdf"}
	for _, raw := range []string{"https://evil.invalid/download/demo/a%20book.pdf", "http://archive.org/download/demo/a%20book.pdf", "https://archive.org/download/demo/other.pdf", "https://archive.org/metadata/demo", "https://archive.org/download/demo/a%20book.pdf?x=1", "https://archive.org/download/demo/a%20book.pdf#x"} {
		bad := file
		bad.DownloadURL = raw
		if _, err := p.Download(context.Background(), bad); err == nil {
			t.Fatal("unsafe selection admitted", raw)
		}
	}
	bad := file
	bad.Access = "restricted_or_uncertain"
	if _, err := p.Download(context.Background(), bad); err == nil || calls != 0 {
		t.Fatal("restricted candidate requested", err)
	}
	resp, err := p.Download(context.Background(), file)
	if err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
	_ = resp.Body.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Download(ctx, file); err == nil || calls != 1 {
		t.Fatal("cancelled/quota request sent", err, calls)
	}
	p.mu.Lock()
	next := p.next
	p.mu.Unlock()
	if time.Until(next) < 50*time.Second {
		t.Fatal("download response did not update shared quota")
	}
	p.downloadClient = nil
	if p.fileClient() == nil {
		t.Fatal("missing bounded default client")
	}
}
