package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxResponse = 2 << 20

type metadataClient struct {
	client *http.Client
	mu     sync.Mutex
	next   time.Time
}

func newMetadataClient(host string) *metadataClient {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Explicit provider requests use direct connections. Environment proxies could
	// bypass destination checks and need a separate, deliberate trust policy.
	transport.Proxy = nil
	transport.MaxConnsPerHost = 2
	transport.DialContext = publicDial(host, net.DefaultResolver.LookupNetIP, (&net.Dialer{Timeout: 10 * time.Second}).DialContext)
	return &metadataClient{client: &http.Client{Timeout: 30 * time.Second, Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 3 || req.URL.Scheme != "https" || req.URL.Host != host || req.URL.User != nil || req.URL.Fragment != "" {
				return errors.New("catalog redirect outside approved source")
			}
			return nil
		},
	}}
}

type lookupIP func(context.Context, string, string) ([]netip.Addr, error)
type dialIP func(context.Context, string, string) (net.Conn, error)

func publicDial(host string, lookup lookupIP, dial dialIP) dialIP {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		h, port, err := net.SplitHostPort(address)
		if err != nil || h != host || port != "443" {
			return nil, errors.New("connection outside approved provider")
		}
		ips, err := lookup(ctx, "ip", h)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, errors.New("provider has no addresses")
		}
		for _, ip := range ips {
			if !publicIP(ip) {
				return nil, errors.New("provider resolved to a nonpublic destination")
			}
		}
		var attempts error
		for _, ip := range ips {
			conn, err := dial(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			attempts = errors.Join(attempts, err)
			if ctx.Err() != nil {
				break
			}
		}
		return nil, attempts
	}
}

var nonglobal = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("3fff::/20"),
}

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.Zone() != "" {
		return false
	}
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, prefix := range nonglobal {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

// getJSON bounds the decompressed body and shares pacing across a provider's
// search and item lookups. It never follows links supplied in response metadata.
func (p *metadataClient) getJSON(ctx context.Context, endpoint string, target any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := p.wait(ctx); err != nil {
		return err
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "nemalo/0.1 (+https://github.com/blisspixel/nemalo)")
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == 429 || resp.StatusCode == 503 {
			p.deferRetry(resp.Header.Get("Retry-After"), time.Now())
		}
		return fmt.Errorf("catalog returned HTTP %d; no automatic retry", resp.StatusCode)
	}
	contentType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || (contentType != "application/json" && !(strings.HasPrefix(contentType, "application/") && strings.HasSuffix(contentType, "+json"))) {
		return errors.New("catalog did not return a JSON content type")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return err
	}
	if len(body) > maxResponse {
		return errors.New("catalog response exceeds 2 MiB")
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode catalog: %w", err)
	}
	return nil
}

func (p *metadataClient) deferRetry(raw string, now time.Time) {
	var until time.Time
	if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil && seconds >= 0 {
		until = now.Add(time.Duration(min(seconds, int64(math.MaxInt64/int64(time.Second)))) * time.Second)
	} else if date, err := http.ParseTime(raw); err == nil {
		until = date
	}
	p.mu.Lock()
	p.next = maxTime(p.next, until)
	p.mu.Unlock()
}

func (p *metadataClient) wait(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		p.mu.Lock()
		now := time.Now()
		if !now.Before(p.next) {
			p.next = now.Add(time.Second)
			p.mu.Unlock()
			return nil
		}
		delay := p.next.Sub(now)
		p.mu.Unlock()
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
			// Another response may have extended Retry-After while this caller
			// waited. Recheck before granting the next request slot.
		}
	}
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
