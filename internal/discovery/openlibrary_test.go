package discovery

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func provider(t *testing.T, handler http.HandlerFunc) *OpenLibrary {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &OpenLibrary{endpoint: server.URL, client: server.Client()}
}

func TestSearchContract(t *testing.T) {
	p := provider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "Verne & ocean" || r.URL.Query().Get("limit") != "3" || r.URL.Query().Get("offset") != "2" {
			t.Errorf("query not encoded: %s", r.URL)
		}
		if !strings.Contains(r.UserAgent(), "nemalo/") {
			t.Error("unidentified client")
		}
		_, _ = io.WriteString(w, `{"numFound":4,"docs":[{"key":"/works/OL1W","title":"Vingt mille lieues","author_name":["Jules Verne"],"language":["fre"]},{"key":"/works/OL2W","title":"Another"}]}`)
	})
	page, err := p.Search(context.Background(), " Verne & ocean ", 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 4 || len(page.Results) != 2 || page.Results[0].ID != "openlibrary:OL1W" || page.Results[0].Access != "discovery_only" || page.Results[1].Authors == nil {
		t.Fatalf("wrong normalized results: %+v", page)
	}
}

func TestMalformedAndOversizedCatalog(t *testing.T) {
	for _, body := range []string{"not json", "{}", `{"numFound":0,"docs":null}`, `{"docs":[]}`, `{"numFound":-1,"docs":[]}`, `{"numFound":1,"docs":[{"key":"/works/../../secret","title":"x"}]}`, `{"numFound":1,"docs":[{"key":"/works/OL1W","title":""}]}`, `{"numFound":2,"docs":[{"key":"/works/OL1W","title":"x"},{"key":"/works/OL2W","title":"y"}]}`, strings.Repeat(" ", maxResponse+1)} {
		t.Run(body[:min(len(body), 25)], func(t *testing.T) {
			p := provider(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) })
			if _, err := p.Search(context.Background(), "query", 1, 0); err == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
	p := provider(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"numFound":0,"docs":[]}`) })
	page, err := p.Search(context.Background(), "none", 1, 0)
	if err != nil || page.Results == nil {
		t.Fatalf("valid empty page rejected: %+v %v", page, err)
	}
}

func TestSearchFailures(t *testing.T) {
	for _, status := range []int{403, 429, 500} {
		p := provider(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })
		if _, err := p.Search(context.Background(), "query", 1, 0); err == nil {
			t.Fatal("HTTP failure hidden")
		}
	}
	p := NewOpenLibrary()
	for _, tc := range []struct {
		query         string
		limit, offset int
	}{{"", 1, 0}, {strings.Repeat("x", 1001), 1, 0}, {"x", 0, 0}, {"x", 51, 0}, {"x", 1, -1}, {"x", 1, 10001}} {
		if _, err := p.Search(context.Background(), tc.query, tc.limit, tc.offset); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Search(ctx, "x", 1, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	p = NewOpenLibrary()
	p.endpoint = ":invalid"
	if _, err := p.Search(context.Background(), "x", 1, 0); err == nil {
		t.Fatal("invalid endpoint accepted")
	}
	p = NewOpenLibrary()
	p.endpoint = "ftp://example.invalid"
	if _, err := p.Search(context.Background(), "x", 1, 0); err == nil {
		t.Fatal("failed request accepted")
	}
}

func TestRedirectAndRatePolicy(t *testing.T) {
	p := NewOpenLibrary()
	for _, target := range []string{"http://openlibrary.org/x", "https://evil.invalid/x", "https://openlibrary.org:444/x"} {
		req, _ := http.NewRequest("GET", target, nil)
		if err := p.client.CheckRedirect(req, nil); err == nil {
			t.Fatalf("unsafe redirect accepted: %s", target)
		}
	}
	req, _ := http.NewRequest("GET", "https://openlibrary.org/x", nil)
	if err := p.client.CheckRedirect(req, nil); err != nil {
		t.Fatal(err)
	}
	if err := p.client.CheckRedirect(req, make([]*http.Request, 4)); err == nil {
		t.Fatal("redirect loop accepted")
	}
	p.next = time.Now().Add(time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := p.wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("rate wait ignored cancellation: %v", err)
	}
	if !maxTime(time.Now(), time.Time{}).After(time.Time{}) {
		t.Fatal("bad rate deadline")
	}
	for _, id := range []string{"", "OLW", "XX123W", "OL12X", "OL../W", "OLaW"} {
		if validWorkID(id) {
			t.Fatalf("invalid ID accepted: %q", id)
		}
	}
}
