package discovery

import (
	"context"
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
