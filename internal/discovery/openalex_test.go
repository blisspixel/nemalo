package discovery

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func openAlexProvider(t *testing.T, handler http.HandlerFunc) *OpenAlex {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	p := NewOpenAlex()
	p.endpoint, p.client = server.URL, server.Client()
	return p
}

func TestOpenAlexSearchContract(t *testing.T) {
	p := openAlexProvider(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("search") != "graphene & sensors" || q.Get("per_page") != "5" || q.Get("page") != "3" {
			t.Errorf("query not encoded: %s", r.URL)
		}
		if q.Get("select") != "id,display_name,authorships,language,doi,ids,publication_year,type,is_retracted" || q.Has("api_key") || q.Has("cursor") || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected request fields: %s", r.URL)
		}
		if !strings.Contains(r.UserAgent(), "nemalo/") {
			t.Error("unidentified client")
		}
		_, _ = io.WriteString(w, `{"meta":{"count":42},"results":[
			{"id":"https://openalex.org/W2741809807","display_name":"Graphene sensors","authorships":[{"author":{"display_name":"A. Author"}},{"author":{"display_name":" "}},{"author":{"display_name":"B. Author"}}],"language":"en","doi":"https://doi.org/10.1234/abc.5","ids":{"pmcid":"https://www.ncbi.nlm.nih.gov/pmc/articles/PMC1234567"},"publication_year":2021,"type":"article","is_retracted":true},
			{"id":"https://openalex.org/W1","display_name":"Sparse","authorships":null,"language":null,"doi":null,"ids":{},"publication_year":null,"type":null,"is_retracted":false},
			{"id":"https://openalex.org/W2","display_name":"Odd claims","doi":"10.1234/not-a-uri","ids":{"pmcid":"https://example.org/PMC1"},"publication_year":-5}
		]}`)
	})
	page, err := p.Search(context.Background(), " graphene & sensors ", 5, 10)
	if err != nil {
		t.Fatal(err)
	}
	if page.Source != "openalex" || page.Total != 42 || page.Offset != 10 || len(page.Results) != 3 {
		t.Fatalf("page %+v", page)
	}
	r := page.Results[0]
	if r.ID != "openalex:W2741809807" || r.LandingPage != "https://openalex.org/W2741809807" || r.Access != "discovery_only" ||
		strings.Join(r.Authors, "|") != "A. Author|B. Author" || strings.Join(r.Languages, "|") != "en" ||
		r.DOI != "10.1234/abc.5" || r.PMCID != "PMC1234567" || r.Year != 2021 || r.WorkType != "article" || !r.Retracted {
		t.Fatalf("first result %+v", r)
	}
	if s := page.Results[1]; s.Authors == nil || s.Languages == nil || len(s.Authors) != 0 || s.DOI != "" || s.PMCID != "" || s.Year != 0 || s.Retracted {
		t.Fatalf("absent claims %+v", s)
	}
	// Claims that are not in the documented URI form are dropped rather than
	// guessed at, and an impossible year is not shown.
	if o := page.Results[2]; o.DOI != "" || o.PMCID != "" || o.Year != 0 {
		t.Fatalf("odd claims kept %+v", o)
	}
}

func TestOpenAlexRefusesBadPagingBeforeRequesting(t *testing.T) {
	p := openAlexProvider(t, func(http.ResponseWriter, *http.Request) { t.Error("request sent") })
	for _, c := range []struct {
		query         string
		limit, offset int
	}{{"", 10, 0}, {strings.Repeat("x", 1001), 10, 0}, {"q", 0, 0}, {"q", 51, 0}, {"q", 10, -10}, {"q", 10, 5}, {"q", 50, 9960}, {"q", 10, 10000}} {
		if _, err := p.Search(context.Background(), c.query, c.limit, c.offset); err == nil {
			t.Errorf("accepted %+v", c)
		}
	}
	ok := openAlexProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "200" {
			t.Errorf("last page %s", r.URL)
		}
		_, _ = io.WriteString(w, `{"meta":{"count":0},"results":[]}`)
	})
	if page, err := ok.Search(context.Background(), "q", 50, 9950); err != nil || page.Results == nil {
		t.Fatalf("last reachable page: %+v %v", page, err)
	}
}

func TestOpenAlexMalformedResponses(t *testing.T) {
	for _, body := range []string{
		"not json", "{}", `{"results":[]}`, `{"meta":{},"results":[]}`, `{"meta":{"count":-1},"results":[]}`, `{"meta":{"count":0},"results":null}`,
		`{"meta":{"count":1},"results":[{"id":"https://openalex.org/A123","display_name":"x"}]}`,
		`{"meta":{"count":1},"results":[{"id":"https://openalex.org/W0","display_name":"x"}]}`,
		`{"meta":{"count":1},"results":[{"id":"https://evil.example/W1","display_name":"x"}]}`,
		`{"meta":{"count":1},"results":[{"id":"https://openalex.org/W1","display_name":"  "}]}`,
		`{"meta":{"count":2},"results":[{"id":"https://openalex.org/W1","display_name":"x"},{"id":"https://openalex.org/W2","display_name":"y"}]}`,
		strings.Repeat(" ", maxResponse+1),
	} {
		t.Run(body[:min(len(body), 30)], func(t *testing.T) {
			p := openAlexProvider(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) })
			if _, err := p.Search(context.Background(), "query", 1, 0); err == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
}

func TestOpenAlexUsesItsFixedHost(t *testing.T) {
	p := NewOpenAlex()
	if p.endpoint != "https://api.openalex.org/works" {
		t.Fatalf("endpoint %s", p.endpoint)
	}
	redirect := p.client.CheckRedirect
	other, _ := http.NewRequest(http.MethodGet, "https://openalex.org/works", nil)
	same, _ := http.NewRequest(http.MethodGet, "https://api.openalex.org/works?page=2", nil)
	if redirect(other, []*http.Request{same}) == nil || redirect(same, []*http.Request{same}) != nil {
		t.Fatal("redirect policy is not fixed to api.openalex.org")
	}
}

func TestOpenAlexEvaluateWork(t *testing.T) {
	p := openAlexProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/doi:10.7717/peerj.4375" || !strings.Contains(r.URL.Query().Get("select"), "locations") || r.URL.Query().Has("api_key") {
			t.Errorf("request %s", r.URL)
		}
		_, _ = io.WriteString(w, `{"id":"https://openalex.org/W2741809807","display_name":"The state of OA","authorships":[{"author":{"display_name":"H. Piwowar"}}],"language":"en","doi":"https://doi.org/10.7717/peerj.4375",
			"ids":{"pmcid":"https://www.ncbi.nlm.nih.gov/pmc/articles/5815332"},"publication_year":2018,"type":"article","is_retracted":true,
			"open_access":{"is_oa":true,"oa_status":"gold"},"locations_count":7,"locations":[
			{"is_oa":true,"landing_page_url":"https://doi.org/10.7717/peerj.4375","pdf_url":null,"source":{"display_name":"PeerJ","type":"journal"},"license":"cc-by","version":"publishedVersion"},
			{"is_oa":true,"landing_page_url":"https://www.ncbi.nlm.nih.gov/pmc/articles/5815332","source":{"display_name":"PubMed Central","type":"repository"},"license":"cc-by","version":"publishedVersion"},
			{"is_oa":true,"landing_page_url":"http://arxiv.org/abs/1706.03762","pdf_url":"https://arxiv.org/pdf/1706.03762v5","source":null},
			{"is_oa":true,"landing_page_url":"https://doi.org/10.48550/arXiv.1706.03762v2"},
			{"is_oa":false,"landing_page_url":"javascript:alert(1)","pdf_url":"https://user:pw@example.org/x.pdf"},
			{"is_oa":true,"landing_page_url":"https://arxiv.org/abs/9999.99999"},
			{"is_oa":true,"landing_page_url":"https://pmc.ncbi.nlm.nih.gov/articles/PMC7/"}
		]}`)
	})
	e, err := p.Evaluate(context.Background(), "doi:10.7717/peerj.4375")
	if err != nil {
		t.Fatal(err)
	}
	if !e.Complete || e.Source != "openalex" || e.Access != "discovery_only" || len(e.Files) != 0 || e.Title[0] != "The state of OA" || e.LandingPage != "https://openalex.org/W2741809807" ||
		e.Dates[0] != "2018" || e.MediaType[0] != "article" || e.MetadataSHA256 == "" {
		t.Fatalf("evaluation %+v", e)
	}
	if got := fmt.Sprint(e.Identifiers); got != "[{openalex openalex:W2741809807} {doi 10.7717/peerj.4375} {pmcid PMC5815332}]" {
		t.Fatalf("identifiers %s", got)
	}
	if !strings.Contains(e.Limitations[0], "retracted") {
		t.Fatalf("retraction not stated first: %q", e.Limitations)
	}
	oa := e.OpenAccess
	if oa == nil || !oa.Open || oa.Status != "gold" || oa.Count != 7 || len(oa.Locations) != 7 {
		t.Fatalf("open access %+v", oa)
	}
	want := []string{"", "pmc:PMC5815332", "arxiv:1706.03762", "arxiv:1706.03762v2", "", "", "pmc:PMC7"}
	for i, l := range oa.Locations {
		if l.Identifier != want[i] {
			t.Errorf("location %d identifier %q, want %q", i+1, l.Identifier, want[i])
		}
	}
	if l := oa.Locations[0]; l.Source != "PeerJ" || l.SourceType != "journal" || l.License != "cc-by" || l.Version != "publishedVersion" || l.PDFURL != "" {
		t.Fatalf("first location %+v", l)
	}
	// Unsafe URLs are dropped and counted, never shown.
	if l := oa.Locations[4]; l.LandingPage != "" || l.PDFURL != "" || !strings.Contains(strings.Join(e.Limitations, " "), "1 location URL(s)") {
		t.Fatalf("unsafe location kept: %+v %q", l, e.Limitations)
	}
}

func TestOpenAlexEvaluateReportsResolutionAndTruncation(t *testing.T) {
	p := openAlexProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/W2" {
			t.Errorf("path %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":"https://openalex.org/W3","display_name":"Merged","doi":null,"open_access":{"is_oa":false,"oa_status":"closed"},"locations_count":250,"locations":[{"is_oa":false}]}`)
	})
	e, err := p.Evaluate(context.Background(), "openalex:W2")
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(e.Limitations, " ")
	if e.Complete || !strings.Contains(all, "returned openalex:W3 for openalex:W2") || !strings.Contains(all, "lists 1 of 250 locations") {
		t.Fatalf("complete=%v %q", e.Complete, e.Limitations)
	}
	doi := openAlexProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"id":"https://openalex.org/W3","display_name":"Other","doi":"https://doi.org/10.5555/other","open_access":{"is_oa":false},"locations_count":0,"locations":[]}`)
	})
	e, err = doi.Evaluate(context.Background(), "doi:10.1234/X")
	if err != nil || !strings.Contains(strings.Join(e.Limitations, " "), "resolved DOI 10.1234/X to openalex:W3, which states DOI 10.5555/other") {
		t.Fatalf("%v %q", err, e.Limitations)
	}
	// The same DOI in another case is the same DOI.
	same := openAlexProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"id":"https://openalex.org/W3","display_name":"Same","doi":"https://doi.org/10.1234/x","open_access":{"is_oa":false},"locations_count":0,"locations":[]}`)
	})
	if e, err = same.Evaluate(context.Background(), "doi:10.1234/X"); err != nil || strings.Contains(strings.Join(e.Limitations, " "), "resolved DOI") {
		t.Fatalf("%v %q", err, e.Limitations)
	}
}

func TestOpenAlexEvaluateRefusals(t *testing.T) {
	p := openAlexProvider(t, func(http.ResponseWriter, *http.Request) { t.Error("request sent") })
	for _, id := range []string{"openalex:W0", "openalex:A1", "openalex:", "doi:", "doi:11.1234/x", "doi:10.12/x", "doi:10.1234/a b", "doi:10.1234/%2e%2e", "doi:10.1234/..", "doi:10.1234/a/./b", "doi:10.1234/x?y", "doi:10.1234/x#y", "W1", "archive:x"} {
		if _, err := p.Evaluate(context.Background(), id); err == nil {
			t.Errorf("accepted %q", id)
		}
	}
	for _, body := range []string{
		`not json`, `{}`,
		`{"id":"https://openalex.org/W1","display_name":"x","locations_count":0,"locations":[]}`,
		`{"id":"https://openalex.org/W1","display_name":"x","open_access":{},"locations":[]}`,
		`{"id":"https://openalex.org/W1","display_name":" ","open_access":{},"locations_count":0,"locations":[]}`,
		`{"id":"https://example.org/W1","display_name":"x","open_access":{},"locations_count":0,"locations":[]}`,
		`{"id":"https://openalex.org/W1","display_name":"x","open_access":{},"locations_count":0,"locations":[{}]}`,
		`{"id":"https://openalex.org/W1","display_name":"x","open_access":{},"locations_count":101,"locations":[` + strings.TrimSuffix(strings.Repeat(`{},`, 101), ",") + `]}`,
	} {
		bad := openAlexProvider(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) })
		if _, err := bad.Evaluate(context.Background(), "openalex:W1"); err == nil {
			t.Errorf("accepted %s", body[:min(len(body), 60)])
		}
	}
}
