package discovery

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPublicDestinations(t *testing.T) {
	for _, value := range []string{"", "127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1", "0.1.2.3", "192.0.2.1", "198.18.0.1", "198.51.100.2", "203.0.113.1", "240.0.0.1", "224.0.0.1", "::", "::1", "::ffff:127.0.0.1", "fe80::1", "fd00::1", "64:ff9b::808:808", "2001:db8::1", "2002::1", "3fff::1", "2606:4700::1%eth0"} {
		ip, _ := netip.ParseAddr(value)
		if publicIP(ip) {
			t.Fatal("nonpublic/reserved destination permitted", value)
		}
	}
	for _, value := range []string{"8.8.8.8", "::ffff:8.8.8.8", "2606:4700:4700::1111"} {
		if !publicIP(netip.MustParseAddr(value)) {
			t.Fatal("public destination rejected", value)
		}
	}
}

func TestDialValidatesDNSAndConnectsToPinnedIPs(t *testing.T) {
	ctx := context.Background()
	lookups, dials := 0, 0
	ips := []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("1.1.1.1")}
	lookup := func(_ context.Context, network, host string) ([]netip.Addr, error) {
		lookups++
		if network != "ip" || host != "archive.org" {
			t.Error(network, host)
		}
		return ips, nil
	}
	dial := func(_ context.Context, _, address string) (net.Conn, error) {
		dials++
		if address == "8.8.8.8:443" {
			return nil, errors.New("first failed")
		}
		if address != "1.1.1.1:443" {
			t.Error("DNS repeated or wrong port", address)
		}
		a, b := net.Pipe()
		_ = b.Close()
		return a, nil
	}
	p := publicDial("archive.org", lookup, dial)
	for _, address := range []string{"archive.org:80", "evil.invalid:443", "bad"} {
		if _, err := p(ctx, "tcp", address); err == nil {
			t.Fatal("unapproved address accepted", address)
		}
	}
	if lookups != 0 || dials != 0 {
		t.Fatal("rejected address reached resolver")
	}
	conn, err := p(ctx, "tcp", "archive.org:443")
	if err != nil || dials != 2 {
		t.Fatal(err, dials)
	}
	_ = conn.Close()
	ips = append(ips, netip.MustParseAddr("127.0.0.1"))
	if _, err := p(ctx, "tcp", "archive.org:443"); err == nil || dials != 2 {
		t.Fatal("mixed DNS response connected")
	}
	ips = nil
	if _, err := p(ctx, "tcp", "archive.org:443"); err == nil {
		t.Fatal("empty DNS accepted")
	}
	p = publicDial("archive.org", func(context.Context, string, string) ([]netip.Addr, error) { return nil, errors.New("DNS failed") }, dial)
	if _, err := p(ctx, "tcp", "archive.org:443"); err == nil {
		t.Fatal("lookup error swallowed")
	}
	p = publicDial("archive.org", lookup, func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("dial failed") })
	ips = []netip.Addr{netip.MustParseAddr("8.8.8.8")}
	if _, err := p(ctx, "tcp", "archive.org:443"); err == nil {
		t.Fatal("dial failure hidden")
	}
}

func TestSharedMetadataHTTPBoundary(t *testing.T) {
	for _, tc := range []struct {
		contentType, body string
		status            int
		good              bool
	}{
		{"application/json", `{"value":1}`, 200, true}, {"application/problem+json; charset=utf-8", `{}`, 200, true},
		{"text/html", "<html>blocked</html>", 200, false}, {"application/json", "{} {}", 200, false},
		{"application/json", strings.Repeat(" ", maxResponse+1), 200, false}, {"bad;", `{}`, 200, false},
		{"application/json", `{}`, 429, false}, {"application/json", `{}`, 503, false},
	} {
		t.Run(tc.contentType+tc.body[:min(8, len(tc.body))], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Accept") != "application/json" || !strings.Contains(r.UserAgent(), "nemalo/") || r.Header.Get("Authorization") != "" {
					t.Error("unexpected request headers", r.Header)
				}
				w.Header().Set("Content-Type", tc.contentType)
				w.Header().Set("Retry-After", "120")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			p := &metadataClient{client: server.Client()}
			var result any
			err := p.getJSON(context.Background(), server.URL, &result)
			if (err == nil) != tc.good {
				t.Fatal(err)
			}
			if tc.status == 429 || tc.status == 503 {
				if time.Until(p.next) < 110*time.Second {
					t.Fatal("Retry-After ignored")
				}
				ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
				defer cancel()
				if err := p.getJSON(ctx, server.URL, &result); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal("quota wait not cancellable", err)
				}
			}
		})
	}
	p := newMetadataClient("archive.org")
	if p.client.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("proxy bypass enabled")
	}
	for _, target := range []string{"http://archive.org/", "https://archive.org:444/", "https://evil.invalid/", "https://user:secret@archive.org/", "https://archive.org/#fragment"} {
		req, _ := http.NewRequest("GET", target, nil)
		if err := p.client.CheckRedirect(req, nil); err == nil {
			t.Fatal("unsafe redirect accepted", target)
		}
	}
	req, _ := http.NewRequest("GET", "https://archive.org/metadata/demo", nil)
	if err := p.client.CheckRedirect(req, nil); err != nil {
		t.Fatal(err)
	}
	if err := p.client.CheckRedirect(req, make([]*http.Request, 4)); err == nil {
		t.Fatal("redirect budget ignored")
	}
	var result any
	if err := p.getJSON(context.Background(), ":invalid", &result); err == nil {
		t.Fatal("bad URL accepted")
	}
}

func TestRetryAfterDatesAndOverflow(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, tc := range []struct {
		raw     string
		delayed bool
	}{{"", false}, {"nonsense", false}, {"-1", false}, {"0", false}, {"60", true}, {now.Add(time.Hour).Format(http.TimeFormat), true}, {"9223372036854775807", true}} {
		p := &metadataClient{}
		p.deferRetry(tc.raw, now)
		if p.next.After(now) != tc.delayed {
			t.Fatal(tc, p.next)
		}
	}
}

func TestConcurrentRequestSlotsRespectPacing(t *testing.T) {
	p := &metadataClient{}
	if err := p.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	results := make(chan error, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { results <- p.wait(ctx) })
	}
	wg.Wait()
	close(results)
	granted := 0
	for err := range results {
		if err == nil {
			granted++
		} else if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	}
	if granted != 0 {
		t.Fatal("concurrent requests bypassed pacing", granted)
	}
}

func TestDecompressedMetadataBudget(t *testing.T) {
	var compressed bytes.Buffer
	z := gzip.NewWriter(&compressed)
	if _, err := z.Write([]byte(strings.Repeat(" ", maxResponse+1))); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(compressed.Bytes())
	}))
	defer server.Close()
	p := &metadataClient{client: server.Client()}
	var result any
	if err := p.getJSON(context.Background(), server.URL, &result); err == nil || !strings.Contains(err.Error(), "exceeds 2 MiB") {
		t.Fatal("compressed response escaped budget", err)
	}
}
