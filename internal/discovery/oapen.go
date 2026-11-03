package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

const (
	// OAPENHost is the only host an OAPEN handle may use (decision 0109).
	OAPENHost = "library.oapen.org"
	// OAPENAccess is one read handle. It is not download permission.
	OAPENAccess = "oapen_handle"
	// OAPENOriginalAccess is one ORIGINAL bitstream offered for acquisition.
	OAPENOriginalAccess = "oapen_original"
)

// oapenPrefix is the only handle prefix this adapter reads. DOAB's
// 20.500.12854 is a different identifier and is refused.
const oapenPrefix = "20.500.12657"

var (
	oapenID   = regexp.MustCompile(`^oapen:20\.500\.12657/([0-9]{1,10})$`)
	oapenUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// SplitOAPENID accepts oapen:20.500.12657/N, where N is 1 to 10 digits.
func SplitOAPENID(id string) (string, bool) {
	m := oapenID.FindStringSubmatch(id)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// ValidOAPENID reports whether id is one explicit OAPEN Library handle.
func ValidOAPENID(id string) bool { _, ok := SplitOAPENID(id); return ok }

// OAPEN reads one explicit Library handle and its ORIGINAL bitstreams.
// It has no search. Metadata uses the shared metadata client; a bitstream
// download uses the checked file client and the same request pace.
type OAPEN struct {
	*metadataClient
	downloadClient *http.Client
}

// NewOAPEN returns a provider pinned to library.oapen.org.
func NewOAPEN() *OAPEN {
	p := &OAPEN{metadataClient: newMetadataClient(OAPENHost)}
	p.downloadClient = p.newFileClient()
	return p
}

// SetHTTPClient installs the metadata client. Tests use it so evaluate does not dial.
func (p *OAPEN) SetHTTPClient(client *http.Client) {
	if client != nil && p != nil && p.metadataClient != nil {
		p.client = client
	}
}

// SetDownloadClient installs the file client. Tests use it so a bitstream fetch does not dial.
func (p *OAPEN) SetDownloadClient(client *http.Client) {
	if client != nil && p != nil {
		p.downloadClient = client
	}
}

func (p *OAPEN) newFileClient() *http.Client {
	c := NewFileClient(func(host string) bool { return host == OAPENHost })
	// A redirect, including one that stays on this host, would name a
	// different object than the UUID just evaluated. Refuse it.
	c.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("OAPEN bitstream redirects are not supported")
	}
	return c
}

func (p *OAPEN) fileClient() *http.Client {
	if p != nil && p.downloadClient != nil {
		return p.downloadClient
	}
	return p.newFileClient()
}

func oapenMetadataURL(suffix string) string {
	u := url.URL{Scheme: "https", Host: OAPENHost, Path: "/rest/handle/" + oapenPrefix + "/" + suffix}
	q := url.Values{}
	q.Set("expand", "metadata,bitstreams")
	u.RawQuery = q.Encode()
	return u.String()
}

func oapenRetrieveURL(uuid string) string {
	return "https://" + OAPENHost + "/rest/bitstreams/" + uuid + "/retrieve"
}

// ValidOAPENOffer reports an ORIGINAL file acquire can select: the access
// mark, a canonical retrieve URL, a safe name, a positive declared size and
// an MD5. The budget and the supported type are checked by acquire.
func ValidOAPENOffer(file OfferedFile) bool {
	if file.Access != OAPENOriginalAccess || file.Bytes == nil || *file.Bytes < 1 || !validDigest(file.MD5, 16) || !oapenSafeName(file.Name) {
		return false
	}
	_, ok := oapenUUIDFromURL(file.DownloadURL)
	return ok
}

func oapenSafeName(name string) bool {
	return name != "." && name != ".." && fs.ValidPath(name) && !strings.Contains(name, "/") && !strings.ContainsAny(name, "\\:\x00\r\n") && len(name) <= 1000
}

func oapenSupportedName(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".epub", ".pdf", ".mp3", ".m4b", ".m4a":
		return true
	}
	return false
}

func oapenUUIDFromURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != OAPENHost || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Port() != "" {
		return "", false
	}
	rest, ok := strings.CutPrefix(u.Path, "/rest/bitstreams/")
	uuid, ok2 := strings.CutSuffix(rest, "/retrieve")
	if !ok || !ok2 || strings.Contains(uuid, "/") || !oapenUUID.MatchString(uuid) || raw != oapenRetrieveURL(uuid) {
		return "", false
	}
	return uuid, true
}

type oapenMeta struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

type oapenBitstream struct {
	UUID       string          `json:"uuid"`
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	BundleName string          `json:"bundleName"`
	MimeType   string          `json:"mimeType"`
	SizeBytes  json.RawMessage `json:"sizeBytes"`
	Retrieve   string          `json:"retrieveLink"`
	CheckSum   *struct {
		Value     string `json:"value"`
		Algorithm string `json:"checkSumAlgorithm"`
	} `json:"checkSum"`
	Code     json.RawMessage `json:"code"`
	Metadata []oapenMeta     `json:"metadata"`
}

// Evaluate reads one handle and lists ORIGINAL bitstreams. The suffix is the
// only caller-supplied path segment. File URLs are built from bitstream
// UUIDs on library.oapen.org; retrieve links, publisher URLs and other
// bundles are not requested.
func (p *OAPEN) Evaluate(ctx context.Context, id string) (Evaluation, error) {
	e := Evaluation{ID: id, Source: "oapen", Access: OAPENAccess, Files: []OfferedFile{}, Restrictions: map[string]string{}, Limitations: []string{
		"OAPEN metadata and bitstream declarations are untrusted evidence, not verification of rights, edition identity, safety, or completeness.",
		"Size and MD5 are source claims until a transfer measures the bytes. Only an ORIGINAL bitstream is listed.",
		"Bitstream code and dc.rights.uri are file declarations. They are separate from item rights and from an open access statement.",
		"Keyword search is not used. The handle prefix 20.500.12657 and host library.oapen.org are the only OAPEN destination.",
	}}
	suffix, ok := SplitOAPENID(id)
	if !ok {
		return e, errors.New("evaluate requires oapen:20.500.12657/N, where N is 1 to 10 digits")
	}
	e.LandingPage = "https://" + OAPENHost + "/handle/" + oapenPrefix + "/" + suffix
	e.MetadataURL = oapenMetadataURL(suffix)
	var raw json.RawMessage
	if err := p.getJSON(ctx, e.MetadataURL, &raw); err != nil {
		return e, err
	}
	e.MetadataSHA256 = digest(raw)
	var wire struct {
		Name       string          `json:"name"`
		Handle     string          `json:"handle"`
		Type       string          `json:"type"`
		Archived   json.RawMessage `json:"archived"`
		Withdrawn  json.RawMessage `json:"withdrawn"`
		Metadata   []oapenMeta     `json:"metadata"`
		Bitstreams json.RawMessage `json:"bitstreams"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil || wire.Type == "" && wire.Handle == "" && len(raw) > 0 && raw[0] == '[' {
		return e, errors.New("OAPEN returned item metadata that is not a JSON object")
	}
	if wire.Type != "item" || wire.Handle != oapenPrefix+"/"+suffix {
		return e, errors.New("OAPEN item type or handle does not match the request")
	}
	if len(bytesTrim(wire.Bitstreams)) == 0 || string(wire.Bitstreams) == "null" {
		return e, errors.New("OAPEN item did not include its bitstream list")
	}
	var streams []oapenBitstream
	if err := json.Unmarshal(wire.Bitstreams, &streams); err != nil {
		return e, errors.New("OAPEN bitstream list is not an array")
	}
	if len(streams) > 500 {
		return e, errors.New("OAPEN returned more than 500 bitstreams")
	}
	e.Title = oapenTexts(wire.Metadata, "dc.title", 500, 5)
	if len(e.Title) == 0 {
		if name, ok := bounded(wire.Name, 500); ok {
			e.Title = []string{name}
		}
	}
	if len(e.Title) == 0 {
		return e, errors.New("OAPEN item has no title")
	}
	e.Creators = append(oapenTexts(wire.Metadata, "dc.contributor.author", 500, 50), oapenTexts(wire.Metadata, "dc.contributor.editor", 500, 50)...)
	if langs := oapenTexts(wire.Metadata, "dc.language.iso", 100, 10); len(langs) > 0 {
		e.Languages = langs
	} else {
		e.Languages = oapenTexts(wire.Metadata, "dc.language", 100, 10)
	}
	e.MediaType = oapenTexts(wire.Metadata, "dc.type", 100, 10)
	e.Dates = oapenTexts(wire.Metadata, "dc.date.issued", 100, 5)
	e.Publishers = append(oapenTexts(wire.Metadata, "publisher.name", 500, 10), oapenTexts(wire.Metadata, "dc.publisher", 500, 10)...)
	e.Rights = oapenRights(wire.Metadata)
	e.LicenseURLs = oapenItemLicenses(wire.Metadata)
	e.Identifiers = oapenIdentifiers(wire.Metadata)
	// archived "true" means the item is in the repository. That is the
	// observed state of a book whose PDF was retrieved, so it does not
	// itself suppress a URL. withdrawn must be the string false. Any other
	// value, including a JSON boolean, suppresses file URLs.
	archived, withdrawn := oapenStringFlag(wire.Archived), oapenStringFlag(wire.Withdrawn)
	e.Restrictions["archived"] = archived
	e.Restrictions["withdrawn"] = withdrawn
	offer := archived == "true" && withdrawn == "false"
	if !offer {
		e.Limitations = append(e.Limitations, "File URLs are omitted unless withdrawn is the string false and archived is the string true.")
	}
	files, err := oapenFiles(streams, offer)
	if err != nil {
		return e, err
	}
	if len(files) == 0 {
		e.Limitations = append(e.Limitations, "This item has no ORIGINAL bitstream. Other bundles are not listed.")
	}
	e.Files = files
	e.CheckedAt = time.Now().UTC()
	e.Complete = true
	return e, nil
}

func oapenFiles(streams []oapenBitstream, offer bool) ([]OfferedFile, error) {
	files := []OfferedFile{}
	seenName := map[string]bool{}
	seenUUID := map[string]bool{}
	originals := 0
	for _, b := range streams {
		if b.BundleName != "ORIGINAL" {
			continue
		}
		originals++
		if originals > 50 {
			return nil, errors.New("OAPEN returned more than 50 ORIGINAL bitstreams")
		}
		name := b.Name
		if !oapenSafeName(name) || seenName[name] {
			return nil, errors.New("OAPEN returned an unsafe or duplicate ORIGINAL file name")
		}
		seenName[name] = true
		f := OfferedFile{Name: name, CandidateKind: offeredKind(name), Access: "not_offered", PrivateState: "not_declared", RestrictionState: "not_declared", RightsStatus: "not_declared", DRMStatus: "not_assessed", Findings: []string{}}
		if mime, ok := bounded(b.MimeType, 100); ok {
			if mime == "application/epub+zip" && !strings.EqualFold(path.Ext(name), ".epub") {
				f.Findings = append(f.Findings, "declared MIME application/epub+zip does not match the file name")
			} else {
				f.Format = mime
			}
		}
		if !offer {
			f.RestrictionState = "restricted_or_uncertain"
			f.Findings = append(f.Findings, "withdrawn must be the string false and archived must be the string true before a file URL is offered")
		}
		uuid := strings.ToLower(strings.TrimSpace(b.UUID))
		if !oapenUUID.MatchString(uuid) {
			f.Findings = append(f.Findings, "bitstream UUID is missing or not canonical")
		} else if seenUUID[uuid] {
			return nil, errors.New("OAPEN returned a repeated ORIGINAL bitstream UUID")
		} else {
			seenUUID[uuid] = true
		}
		if b.Type != "" && b.Type != "bitstream" {
			f.Findings = append(f.Findings, "bitstream type was not bitstream")
		}
		if n, ok := oapenSize(b.SizeBytes); ok {
			f.Bytes = &n
		} else {
			f.Findings = append(f.Findings, "declared size missing or not a positive integer")
		}
		if b.CheckSum != nil && b.CheckSum.Algorithm == "MD5" && validDigest(b.CheckSum.Value, 16) {
			f.MD5 = strings.ToLower(b.CheckSum.Value)
		} else if b.CheckSum != nil {
			f.Findings = append(f.Findings, "checksum is not an MD5")
		} else {
			f.Findings = append(f.Findings, "checksum is missing")
		}
		code, codeOK, uncertain := oapenCode(b.Code)
		if uncertain {
			f.Findings = append(f.Findings, "bitstream code was not a string")
		} else if codeOK && strings.TrimSpace(code) != "" {
			if cleaned, ok := bounded(code, 100); ok {
				f.RightsStatus = "declared code: " + cleaned
			} else {
				f.Findings = append(f.Findings, "bitstream code was not copied")
			}
		}
		f.Findings = append(f.Findings, oapenFileLicenses(b.Metadata)...)
		if len(b.Metadata) > 100 {
			f.Findings = append(f.Findings, "bitstream metadata exceeded 100 entries")
		}
		linkOK := oapenUUID.MatchString(uuid) && oapenRetrievePath(b.Retrieve, uuid)
		supported := oapenSupportedName(name)
		if !supported {
			f.Findings = append(f.Findings, "this ORIGINAL file is not an EPUB, PDF, MP3, M4B, or M4A, so it is not acquired")
		}
		blocked := !offer || !oapenUUID.MatchString(uuid) || seenFinding(f, "bitstream type was not bitstream") || f.Bytes == nil || f.MD5 == "" || uncertain || !linkOK || !supported || len(b.Metadata) > 100 || seenFinding(f, "declared MIME application/epub+zip does not match the file name") || seenFinding(f, "further bitstream dc.rights.uri values were not copied")
		if !linkOK && oapenUUID.MatchString(uuid) {
			f.Findings = append(f.Findings, "retrieve link does not match the bitstream UUID")
		}
		if !blocked {
			f.Access = OAPENOriginalAccess
			f.DownloadURL = oapenRetrieveURL(uuid)
		}
		files = append(files, f)
	}
	return files, nil
}

func seenFinding(f OfferedFile, text string) bool {
	for _, finding := range f.Findings {
		if finding == text {
			return true
		}
	}
	return false
}

func oapenRetrievePath(link, uuid string) bool {
	rest, ok := strings.CutPrefix(link, "/rest/bitstreams/")
	got, ok2 := strings.CutSuffix(rest, "/retrieve")
	return ok && ok2 && !strings.Contains(got, "/") && strings.EqualFold(got, uuid)
}

func oapenSize(raw json.RawMessage) (int64, bool) {
	s := strings.TrimSpace(string(raw))
	if s == "" || strings.Trim(s, "0123456789") != "" {
		return 0, false
	}
	var n int64
	if _, err := fmt.Sscan(s, &n); err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// oapenStringFlag classifies a DSpace flag. Only the JSON strings true and
// false are those values. Absence, null, and a JSON boolean are not.
func oapenStringFlag(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return "absent"
	}
	if len(s) < 2 || s[0] != '"' {
		return "not_a_string"
	}
	var v string
	if json.Unmarshal(raw, &v) != nil {
		return "not_a_string"
	}
	if v == "true" || v == "false" {
		return v
	}
	return "other"
}

func oapenCode(raw json.RawMessage) (string, bool, bool) {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return "", true, false
	}
	if s[0] != '"' {
		return "", false, true
	}
	var v string
	if json.Unmarshal(raw, &v) != nil {
		return "", false, true
	}
	return v, true, false
}

func oapenText(raw json.RawMessage) (string, bool) {
	s := strings.TrimSpace(string(raw))
	if s == "" || s[0] != '"' {
		return "", false
	}
	var v string
	if json.Unmarshal(raw, &v) != nil {
		return "", false
	}
	return v, true
}

func bounded(value string, limit int) (string, bool) {
	if strings.TrimSpace(value) == "" || len(value) > limit {
		return "", false
	}
	v := textField(value, limit)
	if v == "" || len(v) > limit {
		return "", false
	}
	return v, true
}

func oapenTexts(entries []oapenMeta, key string, limit, max int) []string {
	out := []string{}
	for _, e := range entries {
		if e.Key != key {
			continue
		}
		raw, ok := oapenText(e.Value)
		if !ok {
			continue
		}
		v, ok := bounded(raw, limit)
		if !ok || len(out) == max {
			continue
		}
		out = append(out, v)
	}
	return out
}

func oapenRights(entries []oapenMeta) []string {
	out := []string{}
	seen := map[string]bool{}
	add := func(v string) {
		if v == "" || seen[v] || len(out) == 20 {
			return
		}
		seen[v] = true
		out = append(out, v)
	}
	for _, e := range entries {
		raw, ok := oapenText(e.Value)
		if !ok {
			continue
		}
		v, ok := bounded(raw, 500)
		if !ok {
			continue
		}
		if e.Key == "dc.rights" || e.Key == "dc.rights.accessrights" || v == "info:eu-repo/semantics/openAccess" {
			add(v)
		}
	}
	return out
}

func oapenItemLicenses(entries []oapenMeta) []string {
	out := []string{}
	for _, e := range entries {
		if e.Key != "dc.rights.uri" || len(out) == 10 {
			continue
		}
		raw, ok := oapenText(e.Value)
		if !ok {
			continue
		}
		if v, ok := oapenLicenseURL(raw); ok {
			out = append(out, v)
		}
	}
	return out
}

func oapenLicenseURL(raw string) (string, bool) {
	v, ok := bounded(raw, 500)
	if !ok {
		return "", false
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", false
	}
	return v, true
}

func oapenFileLicenses(entries []oapenMeta) []string {
	out := []string{}
	n := 0
	for _, e := range entries {
		if e.Key != "dc.rights.uri" {
			continue
		}
		n++
		if n > 5 {
			return append(out, "further bitstream dc.rights.uri values were not copied")
		}
		raw, ok := oapenText(e.Value)
		if !ok {
			out = append(out, "dc.rights.uri was not a string")
			continue
		}
		if v, ok := oapenLicenseURL(raw); ok {
			out = append(out, "dc.rights.uri: "+v)
			continue
		}
		out = append(out, "dc.rights.uri was not an http(s) URL")
	}
	return out
}

func oapenIdentifiers(entries []oapenMeta) []DeclaredIdentifier {
	keys := []string{"oapen.identifier.doi", "oapen.identifier.isbn", "dc.identifier.doi", "dc.identifier.isbn"}
	var out []DeclaredIdentifier
	seen := map[DeclaredIdentifier]bool{}
	for _, key := range keys {
		taken := 0
		for _, e := range entries {
			if e.Key != key {
				continue
			}
			raw, ok := oapenText(e.Value)
			if !ok {
				continue
			}
			v, ok := bounded(raw, 300)
			if !ok {
				continue
			}
			d := DeclaredIdentifier{Field: key, Value: v}
			if seen[d] || taken == maxFieldIdentifiers || len(out) == MaxDeclaredIdentifiers {
				continue
			}
			seen[d] = true
			taken++
			out = append(out, d)
		}
	}
	return out
}

func bytesTrim(raw json.RawMessage) string { return strings.TrimSpace(string(raw)) }

// Download fetches one ORIGINAL bitstream whose URL evaluate built. It does
// not follow a retrieve link, a file name, or another host. Acquire passes
// the file from a fresh evaluation, which is what binds the UUID to that item.
func (p *OAPEN) Download(ctx context.Context, file OfferedFile) (*http.Response, error) {
	if !ValidOAPENOffer(file) {
		return nil, errors.New("download does not match an OAPEN ORIGINAL bitstream")
	}
	return requestFile(ctx, p.fileClient(), p.metadataClient, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, file.DownloadURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "nemalo/0.1 (+https://github.com/blisspixel/nemalo)")
		req.Header.Set("Accept-Encoding", "identity")
		return req, nil
	})
}
