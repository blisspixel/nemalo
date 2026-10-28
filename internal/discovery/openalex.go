package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// OpenAlexHost is the only host OpenAlex search reaches (decision 0106).
const OpenAlexHost = "api.openalex.org"

// openAlexWindow is the deepest result basic paging reaches.
const openAlexWindow = 10000

var (
	openAlexWork = regexp.MustCompile(`^https://openalex\.org/(W[1-9][0-9]{0,11})$`)
	openAlexDOI  = regexp.MustCompile(`^https://doi\.org/(10\.[0-9]{4,9}/\S{1,300})$`)
	// OpenAlex writes most PMCIDs without the PMC prefix, as observed on
	// 2026-10-28; both forms name the same accession.
	openAlexPMC     = regexp.MustCompile(`^https://(?:www\.ncbi\.nlm\.nih\.gov/pmc|pmc\.ncbi\.nlm\.nih\.gov)/articles/(?:PMC)?([1-9][0-9]{0,9})/?$`)
	openAlexArxiv   = regexp.MustCompile(`^(?:https?://arxiv\.org/(?:abs|pdf)/|https://doi\.org/10\.48550/(?i:arxiv)\.)(\S{1,64}?)(?:\.pdf)?$`)
	openAlexDOIName = regexp.MustCompile(`(?i)^10\.[0-9]{4,9}/[-._;()/:a-z0-9]{1,300}$`)
)

// openAlexLocations is the most locations OpenAlex lists for one work.
const openAlexLocations = 100

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
			r.PMCID = "PMC" + c[1]
		}
		if w.Year > 0 && w.Year < 10000 {
			r.Year = w.Year
		}
		page.Results = append(page.Results, r)
	}
	return page, nil
}

// ValidOpenAlexWork reports openalex:W followed by digits.
func ValidOpenAlexWork(id string) bool {
	rest, ok := strings.CutPrefix(id, "openalex:")
	return ok && openAlexWork.MatchString("https://openalex.org/"+rest)
}

// ValidDOI reports doi:10.PREFIX/SUFFIX within the character set Crossref
// recommends matching, without dot path segments. Rarer DOIs outside it are
// refused, not escaped.
func ValidDOI(id string) bool {
	rest, ok := strings.CutPrefix(id, "doi:")
	if !ok || !openAlexDOIName.MatchString(rest) {
		return false
	}
	// A dot segment could be normalized out of the request path.
	for _, segment := range strings.Split(rest, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

// OpenLocation is one place OpenAlex states a work can be found. Every field
// is OpenAlex's claim; Nemalo opens none of these URLs.
type OpenLocation struct {
	Open        bool   `json:"open"`
	Source      string `json:"source,omitempty"`
	SourceType  string `json:"source_type,omitempty"`
	LandingPage string `json:"landing_page,omitempty"`
	PDFURL      string `json:"pdf_url,omitempty"`
	License     string `json:"license,omitempty"`
	Version     string `json:"version,omitempty"`
	// Identifier is an explicit Nemalo identifier the location's URL states,
	// such as pmc:PMC123 or arxiv:2101.00001v2, which can then be evaluated.
	Identifier string `json:"identifier,omitempty"`
}

// OpenAccess is OpenAlex's open access summary for one work.
type OpenAccess struct {
	Open      bool           `json:"open"`
	Status    string         `json:"status,omitempty"`
	Count     int            `json:"location_count"`
	Locations []OpenLocation `json:"locations"`
}

// Evaluate reads one work by openalex:W or doi: identifier. A single work
// costs nothing against the keyless budget. Nothing is downloaded, and no
// location is a file selection or a rights statement.
func (p *OpenAlex) Evaluate(ctx context.Context, id string) (Evaluation, error) {
	e := Evaluation{ID: id, Source: "openalex", CheckedAt: time.Now().UTC(), Access: "discovery_only", Title: []string{}, Creators: []string{}, Languages: []string{}, MediaType: []string{}, Dates: []string{},
		Publishers: []string{}, Rights: []string{}, LicenseURLs: []string{}, Restrictions: map[string]string{}, Files: []OfferedFile{}, Limitations: []string{
			"OpenAlex metadata is untrusted evidence, not verification of rights, edition identity, or completeness.",
			"Locations are OpenAlex's claims. Nemalo opened none of them; only an explicit pmc: or arxiv: identifier can be evaluated and acquired.",
			"A location's license and version describe that copy only, as OpenAlex states them.",
		}}
	var path string
	switch {
	case ValidOpenAlexWork(id):
		path = "/" + strings.TrimPrefix(id, "openalex:")
	case ValidDOI(id):
		path = "/doi:" + strings.TrimPrefix(id, "doi:")
	default:
		return e, errors.New("evaluate requires openalex:W followed by digits, or doi:10.PREFIX/SUFFIX")
	}
	u, err := url.Parse(p.endpoint)
	if err != nil {
		return e, err
	}
	u.Path += path
	q := url.Values{}
	q.Set("select", "id,display_name,authorships,language,doi,ids,publication_year,type,is_retracted,open_access,locations_count,locations")
	u.RawQuery = q.Encode()
	e.MetadataURL = u.String()
	var raw json.RawMessage
	if err := p.getJSON(ctx, e.MetadataURL, &raw); err != nil {
		return e, fmt.Errorf("evaluate OpenAlex work: %w", err)
	}
	e.MetadataSHA256 = digest(raw)
	var wire struct {
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
		Year       int    `json:"publication_year"`
		Type       string `json:"type"`
		Retracted  bool   `json:"is_retracted"`
		OpenAccess *struct {
			Open   bool   `json:"is_oa"`
			Status string `json:"oa_status"`
		} `json:"open_access"`
		Count     *int `json:"locations_count"`
		Locations []struct {
			Open        bool   `json:"is_oa"`
			LandingPage string `json:"landing_page_url"`
			PDFURL      string `json:"pdf_url"`
			Source      *struct {
				Name string `json:"display_name"`
				Type string `json:"type"`
			} `json:"source"`
			License string `json:"license"`
			Version string `json:"version"`
		} `json:"locations"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return e, fmt.Errorf("decode OpenAlex work: %w", err)
	}
	m := openAlexWork.FindStringSubmatch(wire.ID)
	if m == nil || strings.TrimSpace(wire.Title) == "" || wire.OpenAccess == nil || wire.Count == nil || *wire.Count < len(wire.Locations) || len(wire.Locations) > openAlexLocations {
		return e, errors.New("OpenAlex returned an invalid work record")
	}
	work := "openalex:" + m[1]
	e.Title = []string{wire.Title}
	e.LandingPage = "https://openalex.org/" + m[1]
	for _, a := range wire.Authorships {
		if name := strings.TrimSpace(a.Author.Name); name != "" {
			e.Creators = append(e.Creators, name)
		}
	}
	if wire.Language != "" {
		e.Languages = []string{wire.Language}
	}
	if wire.Type != "" {
		e.MediaType = []string{wire.Type}
	}
	if wire.Year > 0 && wire.Year < 10000 {
		e.Dates = []string{fmt.Sprint(wire.Year)}
	}
	e.Identifiers = []DeclaredIdentifier{{Field: "openalex", Value: work}}
	doi := ""
	if d := openAlexDOI.FindStringSubmatch(wire.DOI); d != nil {
		doi = d[1]
		e.Identifiers = append(e.Identifiers, DeclaredIdentifier{Field: "doi", Value: doi})
	}
	if c := openAlexPMC.FindStringSubmatch(wire.IDs.PMCID); c != nil {
		e.Identifiers = append(e.Identifiers, DeclaredIdentifier{Field: "pmcid", Value: "PMC" + c[1]})
	}
	var notes []string
	if ValidOpenAlexWork(id) && id != work {
		notes = append(notes, "OpenAlex returned "+work+" for "+id+", which it does when a record was merged.")
	}
	if requested, ok := strings.CutPrefix(id, "doi:"); ok && !strings.EqualFold(requested, doi) {
		stated := "states no DOI"
		if doi != "" {
			stated = "states DOI " + doi
		}
		notes = append(notes, "OpenAlex resolved DOI "+requested+" to "+work+", which "+stated+". Check that it is the intended work.")
	}
	if wire.Retracted {
		notes = append(notes, "OpenAlex states this work is retracted.")
	}
	oa := &OpenAccess{Open: wire.OpenAccess.Open, Status: wire.OpenAccess.Status, Count: *wire.Count, Locations: []OpenLocation{}}
	dropped := 0
	for _, l := range wire.Locations {
		loc := OpenLocation{Open: l.Open, License: claim(l.License, 64), Version: claim(l.Version, 64)}
		if l.Source != nil {
			loc.Source, loc.SourceType = claim(l.Source.Name, 300), claim(l.Source.Type, 64)
		}
		loc.LandingPage, loc.PDFURL = locationURL(l.LandingPage), locationURL(l.PDFURL)
		if (l.LandingPage != "" && loc.LandingPage == "") || (l.PDFURL != "" && loc.PDFURL == "") {
			dropped++
		}
		loc.Identifier = locationIdentifier(loc.LandingPage)
		if loc.Identifier == "" {
			loc.Identifier = locationIdentifier(loc.PDFURL)
		}
		oa.Locations = append(oa.Locations, loc)
	}
	e.OpenAccess = oa
	if dropped > 0 {
		notes = append(notes, fmt.Sprintf("%d location URL(s) were not absolute http or https URLs and are not shown.", dropped))
	}
	e.Complete = *wire.Count == len(wire.Locations)
	if !e.Complete {
		notes = append(notes, fmt.Sprintf("OpenAlex lists %d of %d locations.", len(wire.Locations), *wire.Count))
	}
	e.Limitations = append(notes, e.Limitations...)
	return e, nil
}

// claim keeps one short provider string, or nothing if it is too long.
func claim(s string, limit int) string {
	if s = strings.TrimSpace(s); len(s) > limit {
		return ""
	}
	return s
}

// locationURL keeps an absolute http or https URL of at most 2048 bytes.
func locationURL(raw string) string {
	if raw == "" || len(raw) > 2048 || strings.ContainsFunc(raw, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return ""
	}
	return raw
}

// locationIdentifier names the explicit pmc: or arxiv: identifier a location
// URL states, or nothing.
func locationIdentifier(raw string) string {
	if m := openAlexPMC.FindStringSubmatch(raw); m != nil {
		return "pmc:PMC" + m[1]
	}
	if m := openAlexArxiv.FindStringSubmatch(raw); m != nil && ValidArxivID("arxiv:"+m[1]) {
		return "arxiv:" + m[1]
	}
	return ""
}
