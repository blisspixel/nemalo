package discovery

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// arxivTransport answers arXiv requests from fixed bodies and records them.
type arxivTransport struct {
	mu       sync.Mutex
	feeds    map[string]string // id_list value to Atom body
	oai      string
	head     int64
	requests []string
	at       []time.Time
}

func (t *arxivTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.requests = append(t.requests, req.Method+" "+req.URL.Host+req.URL.Path+"?"+req.URL.RawQuery)
	t.at = append(t.at, time.Now())
	t.mu.Unlock()
	reply := func(status int, body string, header http.Header) *http.Response {
		if header == nil {
			header = http.Header{}
		}
		return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: req}
	}
	switch {
	case req.URL.Host == ArxivExportHost && req.URL.Path == "/api/query":
		return reply(200, t.feeds[req.URL.Query().Get("id_list")], nil), nil
	case req.URL.Host == ArxivOAIHost:
		return reply(200, t.oai, nil), nil
	case req.URL.Host == ArxivExportHost && strings.HasPrefix(req.URL.Path, "/pdf/") && req.Method == http.MethodHead:
		r := reply(200, "", http.Header{"Content-Type": {"application/pdf"}})
		r.ContentLength = t.head
		return r, nil
	case req.URL.Host == ArxivExportHost && strings.HasPrefix(req.URL.Path, "/pdf/"):
		return reply(200, "%PDF-1.7\n%%EOF", http.Header{"Content-Type": {"application/pdf"}}), nil
	}
	return reply(404, "", nil), nil
}

func feed(id string, version int, title string) string {
	if title == "" {
		return `<feed xmlns="http://www.w3.org/2005/Atom" xmlns:opensearch="http://a9.com/-/spec/opensearch/1.1/"><opensearch:totalResults>0</opensearch:totalResults></feed>`
	}
	return `<feed xmlns="http://www.w3.org/2005/Atom" xmlns:opensearch="http://a9.com/-/spec/opensearch/1.1/" xmlns:arxiv="http://arxiv.org/schemas/atom"><opensearch:totalResults>1</opensearch:totalResults><entry><id>http://arxiv.org/abs/` + id + `v` + string(rune('0'+version)) + `</id><title>` + title + `</title><published>2017-06-12T17:57:34Z</published><updated>2023-08-02T00:41:18Z</updated><author><name>A. Author</name></author><arxiv:doi>10.1000/x</arxiv:doi><arxiv:primary_category term="cs.CL"/></entry></feed>`
}

func oaiFor(id, license string) string {
	return `<OAI-PMH xmlns="http://www.openarchives.org/OAI/2.0/"><GetRecord><record><metadata><arXiv xmlns="http://arxiv.org/OAI/arXiv/"><id>` + id + `</id><license>` + license + `</license></arXiv></metadata></record></GetRecord></OAI-PMH>`
}

func testArxiv(t *testing.T, tr *arxivTransport) *Arxiv {
	t.Helper()
	// Wide enough for coarse Windows timers; the product pace is three seconds.
	saved := arxivPace
	arxivPace = 100 * time.Millisecond
	t.Cleanup(func() { arxivPace = saved })
	return &Arxiv{Client: &http.Client{Transport: tr}}
}

// An unversioned identifier resolves to the latest version, whose license
// applies; an older version keeps its own record and no license.
func TestArxivEvaluateResolvesVersionsAndLicenses(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &arxivTransport{feeds: map[string]string{"1706.03762": feed("1706.03762", 7, "Attention"), "1706.03762v1": feed("1706.03762", 1, "Attention, first")}, oai: oaiFor("1706.03762", "http://arxiv.org/licenses/nonexclusive-distrib/1.0/"), head: 2215244}
		a := &Arxiv{Client: &http.Client{Transport: tr}}
		e, err := a.Evaluate(context.Background(), "arxiv:1706.03762")
		if err != nil || !e.Complete || e.ID != "arxiv:1706.03762v7" || len(e.LicenseURLs) != 1 || len(e.Files) != 1 {
			t.Fatalf("%+v %v", e, err)
		}
		f := e.Files[0]
		if f.Name != "1706.03762v7.pdf" || f.Bytes == nil || *f.Bytes != 2215244 || f.MD5 != "" || f.DownloadURL != "https://export.arxiv.org/pdf/1706.03762v7" {
			t.Fatalf("%+v", f)
		}
		for _, r := range tr.requests {
			if !strings.Contains(r, ArxivExportHost) && !strings.Contains(r, ArxivOAIHost) {
				t.Fatal("request outside arXiv's hosts", r)
			}
		}
		for i := 1; i < len(tr.at); i++ {
			if gap := tr.at[i].Sub(tr.at[i-1]); gap < 3*time.Second {
				t.Fatalf("request gap %s is less than the three-second product pace", gap)
			}
		}
		old, err := a.Evaluate(context.Background(), "arxiv:1706.03762v1")
		if err != nil || old.ID != "arxiv:1706.03762v1" || len(old.LicenseURLs) != 0 || !strings.Contains(strings.Join(old.Rights, " "), "version 1 is not established") || old.Title[0] != "Attention, first" {
			t.Fatalf("%+v %v", old, err)
		}
		if _, err := a.Evaluate(context.Background(), "arxiv:1706.03762v9"); err == nil || !strings.Contains(err.Error(), "latest is 7") {
			t.Fatal(err)
		}
	})
}

func TestArxivWaitingCallRechecksAnExtendedRetryAfter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		a := &Arxiv{requestPacer: requestPacer{next: start.Add(arxivPace)}}
		granted := make(chan time.Time, 1)
		go func() {
			if err := a.wait(context.Background()); err != nil {
				t.Error(err)
			}
			granted <- time.Now()
		}()
		synctest.Wait()
		postponed := start.Add(9 * time.Second)
		a.afterResponse(&http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Retry-After": {"9"}}})
		time.Sleep(arxivPace)
		synctest.Wait()
		select {
		case at := <-granted:
			t.Fatalf("waiting call granted at %s before extended Retry-After %s", at, postponed)
		default:
		}
		if at := <-granted; at.Before(postponed) {
			t.Fatal("extended Retry-After was bypassed", at, postponed)
		}
	})
}

func TestArxivWaitingRequestRefusesANewLongRetryAfter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &statusTransport{codes: []int{200}}
		a := &Arxiv{Client: &http.Client{Transport: tr}, requestPacer: requestPacer{next: time.Now().Add(arxivPace)}}
		result := make(chan error, 1)
		go func() {
			_, err := a.get(context.Background(), http.MethodGet, "https://export.arxiv.org/api/query")
			result <- err
		}()
		synctest.Wait()
		a.afterResponse(&http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Retry-After": {"120"}}})
		if err := <-result; err == nil || !strings.Contains(err.Error(), "not to be asked again") || tr.calls != 0 {
			t.Fatal("waiting request ignored a new long Retry-After", tr.calls, err)
		}
	})
}

func TestArxivConcurrentWaitersAndCancelledCallsRespectSlots(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := &Arxiv{}
		start := time.Now()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := a.wait(ctx); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if err := a.wait(context.Background()); err != nil || !time.Now().Equal(start) {
			t.Fatal("cancelled call consumed the available slot", err)
		}
		granted := make(chan time.Time, 3)
		for range 3 {
			go func() {
				if err := a.wait(context.Background()); err != nil {
					t.Error(err)
				}
				granted <- time.Now()
			}()
		}
		synctest.Wait()
		prior := start
		for range 3 {
			at := <-granted
			if gap := at.Sub(prior); gap < 3*time.Second {
				t.Fatal("concurrent grants bypassed the product pace", gap)
			}
			prior = at
		}
	})
}

func TestArxivRefusesWhatItCannotVouchFor(t *testing.T) {
	tr := &arxivTransport{feeds: map[string]string{"1706.03762": feed("1706.03762", 2, "A"), "2999.99999": feed("", 0, ""), "1111.11111": feed("1706.03762", 2, "Other")}, oai: oaiFor("9999.00000", "http://x/"), head: 0}
	a := testArxiv(t, tr)
	for _, id := range []string{"arxiv:2999.99999", "arxiv:1111.11111"} {
		if _, err := a.Evaluate(context.Background(), id); err == nil {
			t.Fatal(id, "evaluated")
		}
	}
	// A license record naming another paper, and a PDF without a length.
	e, err := a.Evaluate(context.Background(), "arxiv:1706.03762")
	if err != nil || len(e.LicenseURLs) != 0 || e.Files[0].Bytes != nil || len(e.Files[0].Findings) == 0 {
		t.Fatalf("%+v %v", e, err)
	}
	if _, err := a.Download(context.Background(), OfferedFile{Name: "1706.03762v2.pdf", Access: ArxivPDFAccess, DownloadURL: "https://evil.example/pdf/1706.03762v2"}); err == nil {
		t.Fatal("a download outside the pinned URL was sent")
	}
	resp, err := a.Download(context.Background(), OfferedFile{Name: "1706.03762v2.pdf", Access: ArxivPDFAccess, DownloadURL: "https://export.arxiv.org/pdf/1706.03762v2"})
	if err != nil || resp.StatusCode != 200 {
		t.Fatal(resp, err)
	}
	resp.Body.Close()
}

func TestArxivIdentifiersAndNames(t *testing.T) {
	for id, want := range map[string]bool{"arxiv:1706.03762": true, "arxiv:1706.03762v7": true, "arxiv:2301.0123": false, "arxiv:0704.0001": true, "arxiv:1412.12345": false, "arxiv:1713.01234": false, "arxiv:hep-th/9901001v1": true, "arxiv:math.GT/0309136": false, "arxiv:-/9901001": false, "arxiv:hep-th/0801001": false, "arxiv:hep-th/9107001": false, "arxiv:cond-mat/0703001": true,
		"arxiv:1706.0376": false, "arxiv:1706.03762v0": false, "arxiv:../etc": false, "arxiv:hep-th/99010": false, "1706.03762": false, "arxiv:1706.03762vx": false} {
		if ValidArxivID(id) != want {
			t.Errorf("%s: %v", id, !want)
		}
	}
	if name := ArxivPDFName("hep-th/9901001", 2); name != "hep-th_9901001v2.pdf" {
		t.Fatal(name)
	}
	if paper, v, ok := splitArxivPDFName("hep-th_9901001v2.pdf"); !ok || paper != "hep-th/9901001" || v != 2 {
		t.Fatal(paper, v, ok)
	}
	if _, _, ok := splitArxivPDFName("1706.03762.pdf"); ok {
		t.Fatal("an unversioned PDF name was accepted")
	}
}

type statusTransport struct {
	codes []int
	calls int
}

type arxivRedirectTransport struct {
	target string
	at     []time.Time
}

type delayedArxivTransport struct {
	at []time.Time
}

type heldArxivTransport struct {
	started chan struct{}
	release chan struct{}
	calls   int
}

func (h *heldArxivTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	h.calls++
	if h.calls == 1 {
		close(h.started)
		select {
		case <-h.release:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
}

func TestArxivDispatchWaitsForHeadersAndQueuedCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &heldArxivTransport{started: make(chan struct{}), release: make(chan struct{})}
		unblock := sync.OnceFunc(func() { close(tr.release) })
		defer unblock()
		a := &Arxiv{Client: &http.Client{Transport: tr}}
		first := make(chan error, 1)
		go func() {
			resp, err := a.get(context.Background(), http.MethodGet, "https://export.arxiv.org/api/query")
			if resp != nil {
				resp.Body.Close()
			}
			first <- err
		}()
		<-tr.started
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		second := make(chan error, 1)
		go func() {
			resp, err := a.get(ctx, http.MethodGet, "https://oaipmh.arxiv.org/oai")
			if resp != nil {
				resp.Body.Close()
			}
			second <- err
		}()
		synctest.Wait()
		time.Sleep(2 * arxivPace)
		synctest.Wait()
		if tr.calls != 1 {
			t.Fatal("another request started before the first returned headers", tr.calls)
		}
		cancel()
		if err := <-second; !errors.Is(err, context.Canceled) {
			t.Fatal("queued dispatch ignored cancellation", err)
		}
		unblock()
		if err := <-first; err != nil {
			t.Fatal(err)
		}
	})
}

func (d *delayedArxivTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Model a scheduler or transport delay after a permit was granted but
	// before the actual request starts. The next interval must not shrink.
	if len(d.at) == 0 {
		time.Sleep(time.Second)
	}
	d.at = append(d.at, time.Now())
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
}

func TestArxivLateDispatchDoesNotCompressTheNextRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := &delayedArxivTransport{}
		a := &Arxiv{Client: &http.Client{Transport: tr}}
		for range 2 {
			resp, err := a.get(context.Background(), http.MethodGet, "https://export.arxiv.org/api/query")
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
		}
		if len(tr.at) != 2 || tr.at[1].Sub(tr.at[0]) < 3*time.Second {
			t.Fatal("late dispatch shortened the product interval", tr.at)
		}
	})
}

func (r *arxivRedirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.at = append(r.at, time.Now())
	status, header := http.StatusOK, http.Header{}
	if len(r.at) == 1 {
		status = http.StatusFound
		header.Set("Location", r.target)
	}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
}

func TestArxivRedirectsShareThePaceAndKeepFixedHosts(t *testing.T) {
	for _, test := range []struct {
		name, target string
		lazy         bool
	}{
		{"same host", "https://export.arxiv.org/api/query?normalized=1", false},
		{"other allowed host", "https://oaipmh.arxiv.org/oai", false},
		{"lazy client shares owner", "https://oaipmh.arxiv.org/oai", true},
		{"off host", "https://other.example/query", false},
		{"insecure scheme", "http://export.arxiv.org/api/query", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				tr := &arxivRedirectTransport{target: test.target}
				a := NewArxiv()
				if test.lazy {
					a = &Arxiv{}
				}
				// Keep the production client's redirect policy; substitute only I/O.
				a.client().Transport = tr
				resp, err := a.get(context.Background(), http.MethodGet, "https://export.arxiv.org/api/query")
				if resp != nil {
					_ = resp.Body.Close()
				}
				allowed := strings.HasPrefix(test.target, "https://export.arxiv.org/") || strings.HasPrefix(test.target, "https://oaipmh.arxiv.org/")
				if !allowed {
					if err == nil || len(tr.at) != 1 {
						t.Fatal("redirect left the fixed hosts or scheme", len(tr.at), err)
					}
					return
				}
				if err != nil || len(tr.at) != 2 || tr.at[1].Sub(tr.at[0]) < 3*time.Second {
					t.Fatal("redirect bypassed the product pace", tr.at, err)
				}
			})
		})
	}
}

func (s *statusTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	code := s.codes[min(s.calls, len(s.codes)-1)]
	s.calls++
	h := http.Header{}
	if code == 429 {
		h.Set("Retry-After", "0")
	}
	return &http.Response{StatusCode: code, Header: h, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
}

// A quota answer is tried again at the same pace; a refusal is not.
func TestArxivQuotaIsRetriedAndRefusalIsNot(t *testing.T) {
	quota := &statusTransport{codes: []int{429, 429, 429}}
	a := testArxiv(t, nil)
	a.Client = &http.Client{Transport: quota}
	if _, err := a.Evaluate(context.Background(), "arxiv:1706.03762"); err == nil || quota.calls != 3 {
		t.Fatal("quota", quota.calls, err)
	}
	refused := &statusTransport{codes: []int{403}}
	a.Client = &http.Client{Transport: refused}
	if _, err := a.Evaluate(context.Background(), "arxiv:1706.03762"); err == nil || refused.calls != 1 || !strings.Contains(err.Error(), "403") {
		t.Fatal("refusal", refused.calls, err)
	}
}

type retryAfterTransport struct {
	after string
	at    []time.Time
}

func (r *retryAfterTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.at = append(r.at, time.Now())
	return &http.Response{StatusCode: 503, Header: http.Header{"Retry-After": {r.after}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
}

// A short Retry-After never shortens the pace, and a long one stops every
// later request until it passes.
func TestArxivRetryAfterKeepsThePace(t *testing.T) {
	a := testArxiv(t, nil)
	arxivPace = 300 * time.Millisecond
	short := &retryAfterTransport{after: "0"}
	a.Client = &http.Client{Transport: short}
	_, _ = a.get(context.Background(), http.MethodGet, "https://export.arxiv.org/api/query")
	for i := 1; i < len(short.at); i++ {
		if short.at[i].Sub(short.at[i-1]) < 250*time.Millisecond {
			t.Fatal("a retry broke the pace")
		}
	}
	long := &retryAfterTransport{after: "120"}
	a.Client = &http.Client{Transport: long}
	if _, err := a.Evaluate(context.Background(), "arxiv:1706.03762"); err == nil || len(long.at) != 1 {
		t.Fatal(len(long.at), err)
	}
	if _, err := a.get(context.Background(), http.MethodHead, "https://export.arxiv.org/pdf/1706.03762v1"); err == nil || len(long.at) != 1 || !strings.Contains(err.Error(), "not to be asked again") {
		t.Fatal("a request was sent inside a long Retry-After", len(long.at), err)
	}
}

// A versioned query answered with another version is refused.
func TestArxivVersionedQueryMustMatch(t *testing.T) {
	tr := &arxivTransport{feeds: map[string]string{"1706.03762": feed("1706.03762", 7, "T7"), "1706.03762v2": feed("1706.03762", 7, "T7")}, oai: oaiFor("1706.03762", ""), head: 1}
	if _, err := testArxiv(t, tr).Evaluate(context.Background(), "arxiv:1706.03762v2"); err == nil || !strings.Contains(err.Error(), "version 7") {
		t.Fatal(err)
	}
}

func TestArxivSharesOneConnection(t *testing.T) {
	a := NewArxiv()
	if n := a.Client.Transport.(fileTransport).base.(*http.Transport).MaxConnsPerHost; n != 1 {
		t.Fatal(n)
	}
	var lazy Arxiv
	if first := lazy.client(); first != lazy.client() {
		t.Fatal("a client was built per request")
	}
}
