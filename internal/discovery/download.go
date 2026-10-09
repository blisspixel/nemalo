package discovery

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var archiveDeliveryHost = regexp.MustCompile(`^(ia|dn)[0-9]{6}\.(us|ca)\.archive\.org$`)

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
	u, err := url.Parse(file.DownloadURL)
	if err != nil || u.Scheme != "https" || u.Host != "archive.org" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !strings.HasPrefix(u.Path, "/download/") || file.Access != "public_file_candidate" {
		return nil, errors.New("file is not an Archive public download candidate")
	}
	item, name, ok := strings.Cut(strings.TrimPrefix(u.Path, "/download/"), "/")
	if !ok || !ValidArchiveID("archive:"+item) || name != file.Name {
		return nil, errors.New("download URL does not match the selected Archive file")
	}
	if err := p.wait(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "nemalo/0.1 (+https://github.com/blisspixel/nemalo)")
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := p.fileClient().Do(req)
	if err == nil && (resp.StatusCode == 429 || resp.StatusCode == 503) {
		p.deferRetry(resp.Header.Get("Retry-After"), time.Now())
	}
	return resp, err
}

func (p *Archive) fileClient() *http.Client {
	if p.downloadClient != nil {
		return p.downloadClient
	}
	return NewFileClient(ArchiveDownloadHost)
}
