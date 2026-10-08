// Package discovery queries public bibliographic catalogs without acquiring content.
package discovery

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type Result struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Authors     []string `json:"authors"`
	Languages   []string `json:"languages"`
	LandingPage string   `json:"landing_page"`
	Access      string   `json:"access_type"`
}

type Page struct {
	Source  string   `json:"source"`
	Query   string   `json:"query"`
	Offset  int      `json:"offset"`
	Total   int      `json:"total"`
	Results []Result `json:"results"`
}

type OpenLibrary struct {
	*metadataClient
	endpoint string
}

func NewOpenLibrary() *OpenLibrary {
	return &OpenLibrary{metadataClient: newMetadataClient("openlibrary.org"), endpoint: "https://openlibrary.org/search.json"}
}
func (p *OpenLibrary) Search(ctx context.Context, query string, limit, offset int) (Page, error) {
	page := Page{Source: "openlibrary", Query: strings.TrimSpace(query), Offset: offset, Results: []Result{}}
	if page.Query == "" || len(page.Query) > 1000 || limit < 1 || limit > 50 || offset < 0 || offset > 10000 {
		return page, errors.New("search requires a query of 1-1000 bytes, limit 1-50, and offset 0-10000")
	}
	u, err := url.Parse(p.endpoint)
	if err != nil {
		return page, err
	}
	q := u.Query()
	q.Set("q", page.Query)
	q.Set("limit", fmt.Sprint(limit))
	q.Set("offset", fmt.Sprint(offset))
	q.Set("fields", "key,title,author_name,language")
	u.RawQuery = q.Encode()
	var wire struct {
		Found *int `json:"numFound"`
		Docs  []struct {
			Key       string   `json:"key"`
			Title     string   `json:"title"`
			Authors   []string `json:"author_name"`
			Languages []string `json:"language"`
		} `json:"docs"`
	}
	if err := p.getJSON(ctx, u.String(), &wire); err != nil {
		return page, fmt.Errorf("decode catalog: %w", err)
	}
	if wire.Docs == nil || wire.Found == nil || *wire.Found < 0 || len(wire.Docs) > limit {
		return page, errors.New("catalog returned an invalid result set")
	}
	page.Total = *wire.Found
	for _, doc := range wire.Docs {
		id := strings.TrimPrefix(doc.Key, "/works/")
		if id == doc.Key || !validWorkID(id) || strings.TrimSpace(doc.Title) == "" {
			return page, errors.New("catalog returned an invalid work identifier or title")
		}
		page.Results = append(page.Results, Result{ID: "openlibrary:" + id, Title: doc.Title, Authors: nonnil(doc.Authors), Languages: nonnil(doc.Languages), LandingPage: "https://openlibrary.org/works/" + id, Access: "discovery_only"})
	}
	return page, nil
}

func validWorkID(id string) bool {
	if len(id) < 4 || !strings.HasPrefix(id, "OL") || !strings.HasSuffix(id, "W") {
		return false
	}
	for _, c := range id[2 : len(id)-1] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func nonnil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
