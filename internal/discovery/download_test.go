package discovery

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/blisspixel/nemalo/internal/transfer"
)

type downloadRoundTrip func(*http.Request) (*http.Response, error)

func (f downloadRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFileClientBoundaries(t *testing.T) {
	client := NewFileClient(ArchiveDownloadHost)
	for _, raw := range []string{"http://archive.org/file", "https://evil.invalid/file", "https://archive.org:443/file", "https://user@archive.org/file", "https://archive.org/file#fragment", "https://127.0.0.1/file", "https://ia800308.us.archive.org.evil.invalid/file",
		"https://dn760008.xx.archive.org/file", "https://dn76000.eu.archive.org/file", "https://dn7600088.eu.archive.org/file", "https://ab760008.eu.archive.org/file", "https://eu.archive.org/file", "https://dn760008.eu.archive.org.invalid/file", "https://evil.dn760008.eu.archive.org/file"} {
		req, _ := http.NewRequest(http.MethodGet, raw, nil)
		if _, err := client.Transport.RoundTrip(req); err == nil {
			t.Fatal("unapproved initial request admitted", raw)
		}
		if err := client.CheckRedirect(req, []*http.Request{{}}); err == nil {
			t.Fatal("unapproved redirect admitted", raw)
		}
	}
	// One delivery node per region Archive is known to redirect to (issue 3).
	for _, host := range []string{"archive.org", "www.archive.org", "ia800308.us.archive.org", "dn800308.ca.archive.org", "dn760008.eu.archive.org", "ia600100.eu.archive.org"} {
		u := &url.URL{Scheme: "https", Host: host, Path: "/file"}
		if err := client.CheckRedirect(&http.Request{URL: u}, []*http.Request{{}}); err != nil {
			t.Fatal(host, err)
		}
	}
	if err := client.CheckRedirect(&http.Request{URL: &url.URL{Scheme: "https", Host: "archive.org"}}, make([]*http.Request, 5)); err == nil {
		t.Fatal("redirect limit absent")
	}
	transport := client.Transport.(fileTransport).base.(*http.Transport)
	if transport.Proxy != nil || !transport.DisableCompression || client.Timeout != 2*time.Hour || stallTimeout != time.Minute || transport.ResponseHeaderTimeout == 0 {
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

// A file request that fails before any body is sent again, within bounds; an
// answer that cannot improve, or a long requested wait, is returned at once.
func TestFileRequestsRepeatOnlyFailuresBeforeTheBody(t *testing.T) {
	saved := downloadPause
	downloadPause = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { downloadPause = saved })
	file := OfferedFile{Name: "a.mp3", Access: "public_file_candidate", DownloadURL: "https://archive.org/download/demo/a.mp3"}
	run := func(answers ...func() (*http.Response, error)) (*http.Response, int, error) {
		p := NewArchive()
		calls := 0
		p.downloadClient = &http.Client{Transport: downloadRoundTrip(func(r *http.Request) (*http.Response, error) {
			a := answers[min(calls, len(answers)-1)]
			calls++
			resp, err := a()
			if resp != nil {
				resp.Request = r
			}
			return resp, err
		})}
		p.next = time.Time{}
		resp, err := p.Download(context.Background(), file)
		return resp, calls, err
	}
	status := func(code int, header ...string) func() (*http.Response, error) {
		return func() (*http.Response, error) {
			h := http.Header{}
			if len(header) == 2 {
				h.Set(header[0], header[1])
			}
			return &http.Response{StatusCode: code, Header: h, Body: io.NopCloser(strings.NewReader("body"))}, nil
		}
	}
	reset := func() (*http.Response, error) { return nil, syscall.ECONNRESET }
	if resp, calls, err := run(status(503), reset, status(200)); err != nil || resp.StatusCode != 200 || calls != 3 {
		t.Fatal("did not recover", err, calls)
	}
	if resp, calls, _ := run(status(502)); resp.StatusCode != 502 || calls != downloadAttempts {
		t.Fatal("unbounded or no repeat", calls)
	}
	if resp, calls, _ := run(status(404), status(200)); resp.StatusCode != 404 || calls != 1 {
		t.Fatal("repeated a definite answer", calls)
	}
	if resp, calls, _ := run(status(429, "Retry-After", "3600"), status(200)); resp.StatusCode != 429 || calls != 1 {
		t.Fatal("waited out a long Retry-After", calls)
	}
	if _, calls, err := run(func() (*http.Response, error) { return nil, errors.New("certificate refused") }, status(200)); err == nil || calls != 1 {
		t.Fatal("repeated a policy failure", calls)
	}
	// Cancellation during the pause stops the requests.
	p := NewArchive()
	ctx, cancel := context.WithCancel(context.Background())
	downloadPause = func(int) time.Duration { return time.Hour }
	p.downloadClient = &http.Client{Transport: downloadRoundTrip(func(r *http.Request) (*http.Response, error) {
		cancel()
		return &http.Response{StatusCode: 503, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	if _, err := p.Download(ctx, file); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

type bodyTrip struct{ body io.ReadCloser }

func (b bodyTrip) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: b.body}, nil
}

// A body that stops sending ends with a transient timeout once no bytes have
// arrived for the stall timeout; a steady body is never cut.
func TestFileBodiesThatStallEndAsTransientTimeouts(t *testing.T) {
	old := stallTimeout
	stallTimeout = 150 * time.Millisecond
	t.Cleanup(func() { stallTimeout = old })
	r, w := io.Pipe()
	trip := fileTransport{bodyTrip{r}, func(*url.URL) bool { return true }}
	req, _ := http.NewRequest("GET", "https://archive.org/download/x/y", nil)
	resp, err := trip.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		// Steady bytes for longer than the timeout, then silence.
		for range 6 {
			_, _ = w.Write([]byte("abcd"))
			time.Sleep(50 * time.Millisecond)
		}
	}()
	got, err := io.ReadAll(resp.Body)
	var timeout interface{ Timeout() bool }
	if string(got) != strings.Repeat("abcd", 6) || !errors.As(err, &timeout) || !timeout.Timeout() || !transfer.Transient(err) {
		t.Fatal(len(got), err)
	}
	_ = resp.Body.Close()
	_ = w.Close()
	// A body read to its end closes without a stall.
	trip = fileTransport{bodyTrip{io.NopCloser(strings.NewReader("whole"))}, func(*url.URL) bool { return true }}
	resp, _ = trip.RoundTrip(req)
	if b, err := io.ReadAll(resp.Body); err != nil || string(b) != "whole" || resp.Body.Close() != nil {
		t.Fatal(string(b), err)
	}
}
