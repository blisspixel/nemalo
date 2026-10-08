// Package discovery queries public bibliographic catalogs without acquiring content.
package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const maxResponse = 2 << 20

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
	client   *http.Client
	endpoint string
	mu       sync.Mutex
	next     time.Time
}

func NewOpenLibrary() *OpenLibrary {
	return &OpenLibrary{endpoint: "https://openlibrary.org/search.json", client: &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 3 || req.URL.Scheme != "https" || req.URL.Host != "openlibrary.org" {
				return errors.New("catalog redirect outside approved source")
			}
			return nil
		},
	}}
}

func (p *OpenLibrary) Search(ctx context.Context, query string, limit, offset int) (Page, error) {
	page := Page{Source: "openlibrary", Query: strings.TrimSpace(query), Offset: offset, Results: []Result{}}
	if page.Query == "" || len(page.Query) > 1000 || limit < 1 || limit > 50 || offset < 0 || offset > 10000 {
		return page, errors.New("search requires a query of 1-1000 bytes, limit 1-50, and offset 0-10000")
	}
	if err := p.wait(ctx); err != nil {
		return page, err
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return page, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "nemalo/0.1 (+https://github.com/blisspixel/nemalo)")
	resp, err := p.client.Do(req)
	if err != nil {
		return page, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return page, fmt.Errorf("catalog returned HTTP %d; no automatic retry", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return page, err
	}
	if len(body) > maxResponse {
		return page, errors.New("catalog response exceeds 2 MiB")
	}
	var wire struct {
		Found *int `json:"numFound"`
		Docs  []struct {
			Key       string   `json:"key"`
			Title     string   `json:"title"`
			Authors   []string `json:"author_name"`
			Languages []string `json:"language"`
		} `json:"docs"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
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

func (p *OpenLibrary) wait(ctx context.Context) error {
	p.mu.Lock()
	start := maxTime(time.Now(), p.next)
	p.next = start.Add(time.Second)
	p.mu.Unlock()
	timer := time.NewTimer(time.Until(start))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
