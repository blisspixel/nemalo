package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

type Archive struct {
	*metadataClient
	endpoint       string
	downloadClient *http.Client
}

func NewArchive() *Archive {
	return &Archive{metadataClient: newMetadataClient("archive.org"), endpoint: "https://archive.org", downloadClient: NewFileClient(ArchiveDownloadHost)}
}

// Strings preserves source metadata without inventing language or rights mappings.
// Archive fields commonly alternate between a string and a list of strings.
type Strings []string

func (s *Strings) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*s = []string{}
		return nil
	}
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		*s = []string{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return errors.New("metadata must be a string or list of strings")
	}
	if len(many) > 1000 {
		return errors.New("too many metadata values")
	}
	*s = many
	return nil
}

func ValidArchiveID(id string) bool {
	id, ok := strings.CutPrefix(id, "archive:")
	if !ok || len(id) == 0 || len(id) > 100 {
		return false
	}
	for i, c := range id {
		alphanumeric := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !alphanumeric && (i == 0 || (c != '_' && c != '-' && c != '.')) {
			return false
		}
	}
	return true
}

func (p *Archive) Search(ctx context.Context, query string, limit, offset int) (Page, error) {
	page := Page{Source: "archive", Query: strings.TrimSpace(query), Offset: offset, Results: []Result{}}
	if page.Query == "" || len(page.Query) > 1000 || limit < 1 || limit > 50 || offset < 0 || offset > 10000 {
		return page, errors.New("search requires a query of 1-1000 bytes, limit 1-50, and offset 0-10000")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// Advanced search pages by page/rows. Its start parameter can echo an
	// offset without moving the documents. Unaligned offsets need two pages.
	base, skip := offset/limit*limit, offset%limit
	seen := map[string]bool{}
	for request := 0; request < 2; request++ {
		wire, err := p.searchPage(ctx, page.Query, limit, base)
		if err != nil {
			return page, err
		}
		if request > 0 && page.Total != *wire.Response.Found {
			return page, errors.New("Archive catalog changed between search pages; repeat the query")
		}
		page.Total = *wire.Response.Found
		for i, d := range wire.Response.Docs {
			id := "archive:" + d.Identifier
			if !ValidArchiveID(id) || seen[id] || strings.TrimSpace(d.Title) == "" || (d.MediaType != "texts" && d.MediaType != "audio") {
				return page, errors.New("Archive returned an invalid or duplicate item")
			}
			seen[id] = true
			if i < skip || len(page.Results) >= limit {
				continue
			}
			page.Results = append(page.Results, Result{ID: id, Title: d.Title, Authors: nonnil(d.Creator), Languages: nonnil(d.Language), LandingPage: "https://archive.org/details/" + d.Identifier, Access: "discovery_only"})
		}
		if len(page.Results) == limit || base+len(wire.Response.Docs) >= page.Total {
			return page, nil
		}
		base += limit
		skip = 0
	}
	return page, nil
}

type archiveSearchPage struct {
	Header struct {
		Status *int `json:"status"`
	} `json:"responseHeader"`
	Response struct {
		Found *int `json:"numFound"`
		Start *int `json:"start"`
		Docs  []struct {
			Identifier string  `json:"identifier"`
			Title      string  `json:"title"`
			Creator    Strings `json:"creator"`
			Language   Strings `json:"language"`
			MediaType  string  `json:"mediatype"`
		} `json:"docs"`
	} `json:"response"`
}

func (p *Archive) searchPage(ctx context.Context, query string, limit, base int) (archiveSearchPage, error) {
	q := url.Values{"q": {"(" + query + ") AND (mediatype:texts OR mediatype:audio)"}, "output": {"json"}, "rows": {strconv.Itoa(limit)}, "page": {strconv.Itoa(base/limit + 1)}, "sort[]": {"identifier asc"}, "fl[]": {"identifier", "title", "creator", "language", "mediatype"}}
	var wire archiveSearchPage
	if err := p.getJSON(ctx, p.endpoint+"/advancedsearch.php?"+q.Encode(), &wire); err != nil {
		return wire, err
	}
	if wire.Header.Status == nil || *wire.Header.Status != 0 || wire.Response.Found == nil || *wire.Response.Found < 0 || wire.Response.Start == nil || *wire.Response.Start != base || wire.Response.Docs == nil || len(wire.Response.Docs) > limit {
		return wire, errors.New("Archive returned an invalid search page")
	}
	if len(wire.Response.Docs) != min(limit, max(0, *wire.Response.Found-base)) {
		return wire, errors.New("Archive returned an inconsistent result count")
	}
	return wire, nil
}

type OfferedFile struct {
	Name             string   `json:"name"`
	Format           string   `json:"source_format"`
	CandidateKind    string   `json:"candidate_kind"`
	Bytes            *int64   `json:"declared_bytes,omitempty"`
	MD5              string   `json:"source_md5,omitempty"`
	SHA1             string   `json:"source_sha1,omitempty"`
	Access           string   `json:"access_status"`
	PrivateState     string   `json:"private_state"`
	RestrictionState string   `json:"restriction_state"`
	RightsStatus     string   `json:"rights_status"`
	DRMStatus        string   `json:"drm_status"`
	DownloadURL      string   `json:"download_url,omitempty"`
	Findings         []string `json:"findings"`
}

type Evaluation struct {
	Complete       bool              `json:"complete"`
	ID             string            `json:"id"`
	Source         string            `json:"source"`
	Title          []string          `json:"titles"`
	Creators       []string          `json:"creators"`
	Languages      []string          `json:"languages"`
	MediaType      []string          `json:"media_types"`
	Dates          []string          `json:"dates"`
	Publishers     []string          `json:"publishers"`
	Rights         []string          `json:"rights_statements"`
	LicenseURLs    []string          `json:"declared_license_urls"`
	LandingPage    string            `json:"landing_page"`
	MetadataURL    string            `json:"metadata_url"`
	CheckedAt      time.Time         `json:"checked_at"`
	MetadataSHA256 string            `json:"metadata_sha256"`
	Access         string            `json:"access_status"`
	Restrictions   map[string]string `json:"restriction_evidence"`
	Files          []OfferedFile     `json:"files"`
	Limitations    []string          `json:"limitations"`
}

type archiveFile struct {
	Name       string          `json:"name"`
	Format     string          `json:"format"`
	Size       json.RawMessage `json:"size"`
	MD5        string          `json:"md5"`
	SHA1       string          `json:"sha1"`
	Private    json.RawMessage `json:"private"`
	Restricted json.RawMessage `json:"access-restricted-item"`
}

func (p *Archive) Evaluate(ctx context.Context, id string) (Evaluation, error) {
	e := Evaluation{ID: id, Source: "archive", CheckedAt: time.Now().UTC(), Access: "unknown", Restrictions: map[string]string{}, Files: []OfferedFile{}, Limitations: []string{
		"Source metadata is untrusted evidence, not independent verification of rights, edition identity, quality, safety, or completeness.",
		"Public file candidates are not downloaded or probed. Availability, DRM, and file-level rights remain unverified.",
		"Item-level rights and license statements do not automatically apply to every file, translation, cover, or recording.",
		"Source MD5/SHA-1 values describe transport integrity only; no content SHA-256 or security scan has been performed.",
	}}
	if !ValidArchiveID(id) {
		return e, errors.New("evaluate requires archive:ITEM with a valid Archive identifier")
	}
	item := strings.TrimPrefix(id, "archive:")
	e.LandingPage = "https://archive.org/details/" + item
	e.MetadataURL = "https://archive.org/metadata/" + item + "?extended_err=1"
	var raw json.RawMessage
	if err := p.getJSON(ctx, p.endpoint+"/metadata/"+item+"?extended_err=1", &raw); err != nil {
		return e, err
	}
	e.MetadataSHA256 = digest(raw)
	var wire struct {
		Metadata   map[string]json.RawMessage `json:"metadata"`
		Files      *[]archiveFile             `json:"files"`
		Error      json.RawMessage            `json:"error"`
		Dark       json.RawMessage            `json:"is_dark"`
		Restricted json.RawMessage            `json:"is_restricted"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return e, fmt.Errorf("decode Archive item: %w", err)
	}
	if len(wire.Error) != 0 && string(wire.Error) != "null" {
		return e, errors.New("Archive reported an item metadata error")
	}
	if wire.Metadata == nil || wire.Files == nil || *wire.Files == nil || len(*wire.Files) > 5000 {
		return e, errors.New("Archive item metadata/files missing or outside limits")
	}
	fields := []struct {
		name string
		dest *[]string
	}{{"title", &e.Title}, {"creator", &e.Creators}, {"language", &e.Languages}, {"mediatype", &e.MediaType}, {"date", &e.Dates}, {"publisher", &e.Publishers}, {"rights", &e.Rights}, {"licenseurl", &e.LicenseURLs}}
	for _, field := range fields {
		v := Strings{}
		if b, ok := wire.Metadata[field.name]; ok {
			if err := json.Unmarshal(b, &v); err != nil {
				return e, fmt.Errorf("invalid Archive %s: %w", field.name, err)
			}
		}
		*field.dest = v
	}
	var returned string
	if err := json.Unmarshal(wire.Metadata["identifier"], &returned); err != nil || returned != item || len(e.Title) == 0 || strings.TrimSpace(e.Title[0]) == "" {
		return e, errors.New("Archive item identity/title does not match request")
	}
	itemRestricted := false
	for _, name := range []string{"access-restricted-item", "access-restricted", "is_lending_required", "lending___status"} {
		value := wire.Metadata[name]
		state := flagState(value)
		if name == "lending___status" && len(value) != 0 {
			state = "unknown"
		}
		e.Restrictions[name] = state
		itemRestricted = itemRestricted || state == "true" || state == "unknown"
	}
	for name, value := range map[string]json.RawMessage{"is_dark": wire.Dark, "is_restricted": wire.Restricted} {
		state := flagState(value)
		e.Restrictions[name] = state
		itemRestricted = itemRestricted || state == "true" || state == "unknown"
	}
	e.Access = "no_item_restriction_declared"
	if itemRestricted {
		e.Access = "restricted_or_uncertain"
	}
	seen := map[string]bool{}
	for _, f := range *wire.Files {
		if f.Name == "." || !fs.ValidPath(f.Name) || strings.ContainsAny(f.Name, "\\:\x00\r\n") || seen[f.Name] {
			return e, errors.New("Archive returned an unsafe or duplicate file name")
		}
		seen[f.Name] = true
		o := OfferedFile{Name: f.Name, Format: f.Format, CandidateKind: offeredKind(f.Name), Access: "public_file_candidate", RightsStatus: "not_resolved_at_file_level", DRMStatus: "not_assessed", Findings: []string{}}
		if bytes, ok := sourceSize(f.Size); ok {
			o.Bytes = &bytes
		} else {
			o.Findings = append(o.Findings, "declared size missing or invalid")
		}
		if validDigest(f.MD5, 16) {
			o.MD5 = strings.ToLower(f.MD5)
		} else if f.MD5 != "" {
			o.Findings = append(o.Findings, "invalid source MD5")
		}
		if validDigest(f.SHA1, 20) {
			o.SHA1 = strings.ToLower(f.SHA1)
		} else if f.SHA1 != "" {
			o.Findings = append(o.Findings, "invalid source SHA-1")
		}
		private, restricted := flagState(f.Private), flagState(f.Restricted)
		o.PrivateState, o.RestrictionState = private, restricted
		if itemRestricted || private == "true" || private == "unknown" || restricted == "true" || restricted == "unknown" {
			o.Access = "restricted_or_uncertain"
			o.Findings = append(o.Findings, "item/file restriction declared or unrecognized; no download URL offered")
		} else if strings.EqualFold(path.Ext(f.Name), ".acsm") || strings.EqualFold(path.Ext(f.Name), ".lcpl") {
			o.Access = "license_document_only"
			o.DRMStatus = "license_delivery_document"
			o.Findings = append(o.Findings, "license delivery document is not an unrestricted content file")
		} else {
			// Build from verified item identity and an escaped file path. Ignore
			// source-provided server, directory, and URL fields entirely.
			u := url.URL{Scheme: "https", Host: "archive.org", Path: "/download/" + item + "/" + f.Name}
			o.DownloadURL = u.String()
		}
		e.Files = append(e.Files, o)
	}
	e.Complete = true
	return e, nil
}

func flagState(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "not_declared"
	}
	s := strings.ToLower(strings.TrimSpace(string(raw)))
	if s == "true" || s == `"true"` || s == "1" || s == `"1"` {
		return "true"
	}
	if s == "false" || s == `"false"` || s == "0" || s == `"0"` {
		return "false"
	}
	return "unknown"
}

func sourceSize(raw json.RawMessage) (int64, bool) {
	s := string(raw)
	if strings.HasPrefix(s, `"`) {
		if err := json.Unmarshal(raw, &s); err != nil {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil && n >= 0
}

func offeredKind(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".epub":
		return "ebook_candidate"
	case ".pdf":
		return "pdf_candidate"
	case ".mp3", ".m4b", ".m4a", ".ogg", ".flac":
		return "audio_candidate"
	case ".zip", ".rar", ".7z", ".gz", ".tar":
		return "archive_candidate"
	}
	return "other"
}

func digest(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func validDigest(value string, bytes int) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == bytes
}
