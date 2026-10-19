package discovery

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/blisspixel/nemalo/internal/transfer"
)

// Archive assigns each item to a numbered data node in one of its delivery
// regions, independent of the client's location. The anchored pattern admits
// only those nodes, never arbitrary archive.org subdomains.
var archiveDeliveryHost = regexp.MustCompile(`^(ia|dn)[0-9]{6}\.(us|ca|eu)\.archive\.org$`)

func ArchiveDownloadHost(host string) bool {
	return host == "archive.org" || host == "www.archive.org" || archiveDeliveryHost.MatchString(host)
}

// NewFileClient reuses the metadata client's checked, pinned-IP direct dial.
// Callers select permitted delivery hosts; every initial request and redirect is
// checked. Environment proxies, cookies, and automatic decompression are disabled.
func NewFileClient(allowedHost func(string) bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DisableCompression = true
	transport.MaxConnsPerHost = 2
	transport.ResponseHeaderTimeout = 30 * time.Second
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || port != "443" || !allowedHost(host) {
			return nil, errors.New("connection outside approved download source")
		}
		return publicDial(host, net.DefaultResolver.LookupNetIP, (&net.Dialer{Timeout: 10 * time.Second}).DialContext)(ctx, network, address)
	}
	valid := func(u *url.URL) bool {
		return u.Scheme == "https" && u.User == nil && u.Port() == "" && u.Fragment == "" && allowedHost(u.Host)
	}
	return &http.Client{Timeout: 5 * time.Minute, Transport: fileTransport{transport, valid}, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !valid(req.URL) {
			return errors.New("download redirect outside approved source")
		}
		return nil
	}}
}

type fileTransport struct {
	base  http.RoundTripper
	valid func(*url.URL) bool
}

func (t fileTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !t.valid(req.URL) {
		return nil, errors.New("download request outside approved source")
	}
	return t.base.RoundTrip(req)
}

// Download opens only the canonical URL constructed by a fresh Archive
// evaluation. Source-provided server URLs and saved evaluation files are unused.
func (p *Archive) Download(ctx context.Context, file OfferedFile) (*http.Response, error) {
	return p.DownloadFrom(ctx, file, 0, "")
}

// DownloadFrom requests the file from offset onward, only if it still matches
// validator (a strong ETag): with If-Range, a changed file answers with the
// whole file and status 200 instead of a range, which the caller refuses.
func (p *Archive) DownloadFrom(ctx context.Context, file OfferedFile, offset int64, validator string) (*http.Response, error) {
	if offset < 0 || (offset > 0 && !StrongETag(validator)) {
		return nil, errors.New("a resumed download needs a positive offset and a strong validator")
	}
	u, err := url.Parse(file.DownloadURL)
	if err != nil || u.Scheme != "https" || u.Host != "archive.org" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !strings.HasPrefix(u.Path, "/download/") || file.Access != "public_file_candidate" {
		return nil, errors.New("file is not an Archive public download candidate")
	}
	item, name, ok := strings.Cut(strings.TrimPrefix(u.Path, "/download/"), "/")
	if !ok || !ValidArchiveID("archive:"+item) || name != file.Name {
		return nil, errors.New("download URL does not match the selected Archive file")
	}
	return requestFile(ctx, p.fileClient(), p.metadataClient, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "nemalo/0.1 (+https://github.com/blisspixel/nemalo)")
		req.Header.Set("Accept-Encoding", "identity")
		if offset > 0 {
			req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
			req.Header.Set("If-Range", validator)
		}
		return req, nil
	})
}

func (p *Archive) fileClient() *http.Client {
	if p.downloadClient != nil {
		return p.downloadClient
	}
	return NewFileClient(ArchiveDownloadHost)
}

// StrongETag reports whether an ETag header value is a strong validator
// (RFC 9110, 8.8.3): quoted, without the W/ weak prefix, with no quotes or
// control characters inside.
func StrongETag(v string) bool {
	if len(v) < 2 || len(v) > 256 || v[0] != '"' || v[len(v)-1] != '"' {
		return false
	}
	for _, c := range v[1 : len(v)-1] {
		if c == '"' || c < 0x21 || c == 0x7f {
			return false
		}
	}
	return true
}

// downloadAttempts bounds the requests for one file response; tests shorten
// downloadPause. A server that asks to wait longer than maxRetryWait gets its
// answer returned instead: the caller fails and keeps what it recorded.
var (
	downloadAttempts = 3
	downloadPause    = func(attempt int) time.Duration { return time.Duration(attempt) * 2 * time.Second }
)

const maxRetryWait = 30 * time.Second

// requestFile sends a file request and repeats it when it fails before any
// body was read: a transient transport error, or a 429, 500, 502, 503, or 504
// answer. Nothing has been written then, so the same request again is the
// whole recovery. limiter, when set, spaces requests and honors Retry-After.
// Any other answer, and the last attempt's, is returned to the caller.
func requestFile(ctx context.Context, client *http.Client, limiter *metadataClient, build func() (*http.Request, error)) (*http.Response, error) {
	for attempt := 1; ; attempt++ {
		if limiter != nil {
			if err := limiter.wait(ctx); err != nil {
				return nil, err
			}
		} else if err := ctx.Err(); err != nil {
			return nil, err
		}
		req, err := build()
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		retry, pause := false, downloadPause(attempt)
		switch {
		case err != nil:
			retry = transfer.Transient(err) && ctx.Err() == nil
		case resp.StatusCode == 429 || resp.StatusCode == 500 || resp.StatusCode == 502 || resp.StatusCode == 503 || resp.StatusCode == 504:
			after := resp.Header.Get("Retry-After")
			if limiter != nil && (resp.StatusCode == 429 || resp.StatusCode == 503) {
				limiter.deferRetry(after, time.Now())
			}
			wait := time.Until(retryUntil(after, time.Now()))
			retry, pause = wait <= maxRetryWait, max(pause, wait)
		}
		if !retry || attempt >= downloadAttempts {
			return resp, err
		}
		if resp != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			_ = resp.Body.Close()
		}
		timer := time.NewTimer(pause)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
