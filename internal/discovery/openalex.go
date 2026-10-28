package discovery

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// OpenAlexHost is the only host OpenAlex search reaches (decision 0106).
const OpenAlexHost = "api.openalex.org"

// openAlexWindow is the deepest result basic paging reaches.
const openAlexWindow = 10000

var (
	openAlexWork = regexp.MustCompile(`^https://openalex\.org/(W[1-9][0-9]{0,11})$`)
	openAlexDOI  = regexp.MustCompile(`^https://doi\.org/(10\.[0-9]{4,9}/\S{1,300})$`)
	openAlexPMC  = regexp.MustCompile(`^https://www\.ncbi\.nlm\.nih\.gov/pmc/articles/(PMC[1-9][0-9]{0,9})/?$`)
)

// OpenAlex searches scholarly works without a key, so every search is metered
// against the keyless daily budget. Results are discovery metadata only.
type OpenAlex struct {
	*metadataClient
	endpoint string
}

func NewOpenAlex() *OpenAlex {
	return &OpenAlex{metadataClient: newMetadataClient(OpenAlexHost), endpoint: "https://" + OpenAlexHost + "/works"}
}

// SetHTTPClient installs a metadata client. Tests use it so a call does not dial.
func (p *OpenAlex) SetHTTPClient(client *http.Client) {
	if client != nil {
		p.client = client
	}
}

// Search requests one page of works in OpenAlex relevance order. The offset
// must be a multiple of the limit, since OpenAlex pages by page number, and
// the page must lie within the first 10,000 results.
func (p *OpenAlex) Search(ctx context.Context, query string, limit, offset int) (Page, error) {
	page := Page{Source: "openalex", Query: strings.TrimSpace(query), Order: OrderRelevance, Offset: offset, Results: []Result{}}
	if page.Query == "" || len(page.Query) > 1000 || limit < 1 || limit > 50 || offset < 0 || offset%limit != 0 || offset+limit > openAlexWindow {
		return page, errors.New("OpenAlex search requires a query of 1-1000 bytes, limit 1-50, and an offset that is a multiple of the limit within the first 10000 results")
	}
	u, err := url.Parse(p.endpoint)
	if err != nil {
		return page, err
	}
	q := u.Query()
	q.Set("search", page.Query)
	q.Set("per_page", fmt.Sprint(limit))
	q.Set("page", fmt.Sprint(offset/limit+1))
	q.Set("select", "id,display_name,authorships,language,doi,ids,publication_year,type,is_retracted")
	u.RawQuery = q.Encode()
	var wire struct {
		Meta *struct {
			Count *int `json:"count"`
		} `json:"meta"`
		Results []struct {
			ID          string `json:"id"`
			Title       string `json:"display_name"`
			Authorships []struct {
				Author struct {
					Name string `json:"display_name"`
				} `json:"author"`
			} `json:"authorships"`
			Language string `json:"language"`
			DOI      string `json:"doi"`
			IDs      struct {
				PMCID string `json:"pmcid"`
			} `json:"ids"`
			Year      int    `json:"publication_year"`
			Type      string `json:"type"`
			Retracted bool   `json:"is_retracted"`
		} `json:"results"`
	}
	if err := p.getJSON(ctx, u.String(), &wire); err != nil {
		return page, fmt.Errorf("decode OpenAlex works: %w", err)
	}
	if wire.Meta == nil || wire.Meta.Count == nil || *wire.Meta.Count < 0 || wire.Results == nil || len(wire.Results) > limit {
		return page, errors.New("OpenAlex returned an invalid result set")
	}
	page.Total = *wire.Meta.Count
	for _, w := range wire.Results {
		m := openAlexWork.FindStringSubmatch(w.ID)
		if m == nil || strings.TrimSpace(w.Title) == "" {
			return page, errors.New("OpenAlex returned an invalid work identifier or title")
		}
		r := Result{ID: "openalex:" + m[1], Title: w.Title, Authors: []string{}, Languages: []string{}, LandingPage: "https://openalex.org/" + m[1], Access: "discovery_only", WorkType: w.Type, Retracted: w.Retracted}
		for _, a := range w.Authorships {
			if name := strings.TrimSpace(a.Author.Name); name != "" {
				r.Authors = append(r.Authors, name)
			}
		}
		if w.Language != "" {
			r.Languages = append(r.Languages, w.Language)
		}
		if d := openAlexDOI.FindStringSubmatch(w.DOI); d != nil {
			r.DOI = d[1]
		}
		if c := openAlexPMC.FindStringSubmatch(w.IDs.PMCID); c != nil {
			r.PMCID = c[1]
		}
		if w.Year > 0 && w.Year < 10000 {
			r.Year = w.Year
		}
		page.Results = append(page.Results, r)
	}
	return page, nil
}
