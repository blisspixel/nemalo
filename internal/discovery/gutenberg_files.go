package discovery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// GutenbergMirrorHost is the only host for a bibliographic record and an EPUB.
	GutenbergMirrorHost = "gutenberg.pglaf.org"
	// GutenbergMirrorAccess is an evaluation built from that one record.
	GutenbergMirrorAccess = "mirror_rdf"
	// GutenbergEPUBAccess is one offered EPUB. The record has no checksum.
	GutenbergEPUBAccess = "public_file_no_source_checksum"
	// GutenbergListedAccess is a format the record names and this slice does not download.
	GutenbergListedAccess = "listed_not_acquired"
)

// maxRDFBytes bounds one bibliographic record. A text's record is tens of
// kilobytes; a larger body is refused rather than parsed.
const maxRDFBytes = 1 << 20

const maxRDFTokens = 100000

func (g *Gutenberg) mirrorClient() *http.Client {
	if g != nil && g.FileClient != nil {
		return g.FileClient
	}
	return NewFileClient(func(host string) bool { return host == GutenbergMirrorHost })
}

// EvaluateFiles fetches one bibliographic record and lists its files.
// It does not download an ebook. Catalog evaluation does not call it.
func (g *Gutenberg) EvaluateFiles(ctx context.Context, id string) (Evaluation, error) {
	e := Evaluation{
		ID: id, Source: "gutenberg", Access: GutenbergMirrorAccess, Restrictions: map[string]string{}, Files: []OfferedFile{},
		Limitations: []string{
			"This record is one bibliographic file from gutenberg.pglaf.org. Reading it does not query the Gutenberg website and does not transfer an ebook.",
			"The record provides no checksum. A declared extent is not a hash and may differ from the response length.",
			"The rights sentence is the record's statement. It is not a file-level rights decision.",
			"Formats other than the pinned EPUB cache objects are listed and are not acquired.",
			"Generated summaries in the record are not stored.",
		},
	}
	if !ValidGutenbergID(id) {
		return e, errors.New("evaluate requires gutenberg:NUMBER with a positive decimal text number")
	}
	if err := ctx.Err(); err != nil {
		return e, err
	}
	number := strings.TrimPrefix(id, "gutenberg:")
	raw := gutenbergRDFURL(number)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return e, err
	}
	req.Header.Set("User-Agent", "nemalo/0.1 (+https://github.com/blisspixel/nemalo)")
	req.Header.Set("Accept", "application/rdf+xml, application/xml, text/xml")
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := g.mirrorClient().Do(req)
	if err != nil {
		return e, fmt.Errorf("gutenberg bibliographic record was not available from %s: %w", GutenbergMirrorHost, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return e, fmt.Errorf("gutenberg bibliographic record returned HTTP %d from %s", resp.StatusCode, GutenbergMirrorHost)
	}
	if resp.ContentLength > maxRDFBytes {
		return e, errors.New("gutenberg bibliographic record exceeds 1 MiB")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRDFBytes+1))
	if err != nil {
		return e, err
	}
	if len(body) == 0 || len(body) > maxRDFBytes {
		return e, errors.New("gutenberg bibliographic record exceeds 1 MiB")
	}
	parsed, err := parseGutenbergRDF(number, body)
	if err != nil {
		return e, err
	}
	e.Complete = true
	e.Title = parsed.titles
	e.Creators = parsed.creators
	e.Languages = parsed.languages
	e.Publishers = parsed.publishers
	e.Rights = parsed.rights
	e.LicenseURLs = parsed.licenses
	e.Dates = parsed.dates
	e.Files = parsed.files
	e.LandingPage = "https://www.gutenberg.org/ebooks/" + number
	e.MetadataURL = raw
	sum := sha256.Sum256(body)
	e.MetadataSHA256 = hex.EncodeToString(sum[:])
	e.CheckedAt = time.Now().UTC()
	if len(e.Title) == 0 {
		e.Limitations = append(e.Limitations, "The record states no title.")
	}
	if len(e.Rights) == 0 {
		e.Limitations = append(e.Limitations, "The record states no rights sentence.")
	}
	if len(e.Files) == 0 {
		e.Limitations = append(e.Limitations, "The record lists no files.")
	}
	return e, nil
}

// Download opens the pinned cache object for one offered EPUB.
func (g *Gutenberg) Download(ctx context.Context, file OfferedFile) (*http.Response, error) {
	return g.DownloadFrom(ctx, file, 0, "")
}

// DownloadFrom requests the EPUB from offset onward when validator is a strong
// ETag. A changed file that answers 200 is refused by the caller.
func (g *Gutenberg) DownloadFrom(ctx context.Context, file OfferedFile, offset int64, validator string) (*http.Response, error) {
	if offset < 0 || (offset > 0 && !StrongETag(validator)) {
		return nil, errors.New("a resumed download needs a positive offset and a strong validator")
	}
	if file.Access != GutenbergEPUBAccess {
		return nil, errors.New("file is not a Gutenberg EPUB offer")
	}
	number, kind, ok := splitGutenbergEPUB(file.Name)
	want, wantOK := gutenbergEPUBURL(number, kind)
	if !ok || !wantOK || file.DownloadURL != want {
		return nil, errors.New("download URL does not match the pinned Gutenberg EPUB")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, want, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "nemalo/0.1 (+https://github.com/blisspixel/nemalo)")
	req.Header.Set("Accept", "application/epub+zip, application/octet-stream")
	req.Header.Set("Accept-Encoding", "identity")
	if offset > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
		req.Header.Set("If-Range", validator)
	}
	return g.mirrorClient().Do(req)
}

// Files is the acquisition view of a Gutenberg catalog provider. Evaluate
// fetches one bibliographic record. Download fetches one offered EPUB.
type Files struct{ *Gutenberg }

// Evaluate implements acquisition.Provider by fetching the bibliographic record.
func (f Files) Evaluate(ctx context.Context, id string) (Evaluation, error) {
	if f.Gutenberg == nil {
		return Evaluation{}, errors.New("gutenberg acquisition is unavailable")
	}
	return f.EvaluateFiles(ctx, id)
}

// GutenbergEPUBName reports whether name is N.epub.noimages, N.epub.images, or
// N.epub3.images for the text number in id.
func GutenbergEPUBName(id, name string) bool {
	number, kind, ok := splitGutenbergEPUB(name)
	if !ok || id != "gutenberg:"+number {
		return false
	}
	_, ok = gutenbergEPUBObjects[kind]
	return ok
}

func gutenbergRDFURL(number string) string {
	return "https://" + GutenbergMirrorHost + "/cache/epub/" + number + "/pg" + number + ".rdf"
}

func gutenbergEPUBURL(number, kind string) (string, bool) {
	pattern, ok := gutenbergEPUBObjects[kind]
	if !ok {
		return "", false
	}
	return "https://" + GutenbergMirrorHost + "/cache/epub/" + number + "/" + fmt.Sprintf(pattern, number), true
}

// gutenbergEPUBObjects maps a bibliographic KIND to the generated cache filename.
// The patterns are Project Gutenberg's generated-file names, not a guess that
// every EPUB is pgN.epub.
var gutenbergEPUBObjects = map[string]string{
	"epub.noimages": "pg%s.epub",
	"epub.images":   "pg%s-images.epub",
	"epub3.images":  "pg%s-images-3.epub",
}

func splitGutenbergEPUB(name string) (number, kind string, ok bool) {
	for _, kind := range []string{"epub.noimages", "epub.images", "epub3.images"} {
		suffix := "." + kind
		if strings.HasSuffix(name, suffix) {
			number = strings.TrimSuffix(name, suffix)
			if ValidGutenbergID("gutenberg:" + number) {
				return number, kind, true
			}
		}
	}
	return "", "", false
}

type rdfParsed struct {
	titles, creators, languages, publishers, rights, licenses, dates []string
	files                                                            []OfferedFile
}

type rdfFile struct {
	about  string
	extent string
	media  []string
}

func parseGutenbergRDF(number string, body []byte) (rdfParsed, error) {
	var parsed rdfParsed
	if bytes.Contains(bytes.ToLower(body), []byte("<!doctype")) || bytes.Contains(bytes.ToLower(body), []byte("<!entity")) {
		return parsed, errors.New("bibliographic record must not declare a document type")
	}
	dec := xml.NewDecoder(bytes.NewReader(body))
	dec.CharsetReader = func(string, io.Reader) (io.Reader, error) {
		return nil, errors.New("bibliographic record encoding must be utf-8")
	}
	var stack []string
	var text strings.Builder
	var base string
	var ebookAbout string
	var sawEbook bool
	var current *rdfFile
	tokens := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return parsed, errors.New("bibliographic record is truncated or not XML")
		}
		tokens++
		if tokens > maxRDFTokens {
			return parsed, errors.New("bibliographic record is too complex")
		}
		switch t := tok.(type) {
		case xml.Directive:
			return parsed, errors.New("bibliographic record must not declare a document type")
		case xml.StartElement:
			stack = append(stack, t.Name.Local)
			text.Reset()
			switch t.Name.Local {
			case "RDF":
				if base == "" {
					base = attrLocal(t, "base")
				}
			case "ebook":
				if sawEbook {
					return parsed, errors.New("bibliographic record has more than one ebook")
				}
				sawEbook = true
				ebookAbout = attrLocal(t, "about")
				if !ebookAboutMatches(ebookAbout, number) {
					return parsed, fmt.Errorf("bibliographic record is not text %s", number)
				}
			case "file":
				if current != nil || !stackHas(stack[:len(stack)-1], "ebook") {
					return parsed, errors.New("bibliographic record has a file outside the ebook")
				}
				current = &rdfFile{about: attrLocal(t, "about")}
			case "license":
				if len(stack) >= 2 && stack[len(stack)-2] == "ebook" {
					if license := resolveLicense(base, attrLocal(t, "resource")); license != "" {
						parsed.licenses = appendLimited(parsed.licenses, license, 8)
					}
				}
			}
		case xml.CharData:
			if text.Len() < 16384 {
				text.Write(t)
			}
		case xml.EndElement:
			if len(stack) == 0 {
				return parsed, errors.New("bibliographic record is truncated or not XML")
			}
			value := cleanRDFText(text.String())
			local := stack[len(stack)-1]
			parent := ""
			if len(stack) >= 2 {
				parent = stack[len(stack)-2]
			}
			switch local {
			case "title":
				if parent == "ebook" {
					parsed.titles = appendLimited(parsed.titles, value, 32)
				}
			case "name":
				if stackHas(stack, "creator") && stackHas(stack, "ebook") && current == nil {
					parsed.creators = appendLimited(parsed.creators, value, 64)
				}
			case "publisher":
				if parent == "ebook" {
					parsed.publishers = appendLimited(parsed.publishers, value, 8)
				}
			case "rights":
				if parent == "ebook" {
					parsed.rights = appendLimited(parsed.rights, value, 8)
				}
			case "issued":
				if parent == "ebook" {
					parsed.dates = appendLimited(parsed.dates, value, 8)
				}
			case "value":
				if current != nil && stackHas(stack, "format") {
					if value != "" && len(current.media) < 8 {
						current.media = append(current.media, value)
					}
				} else if current == nil && stackHas(stack, "language") && stackHas(stack, "ebook") {
					parsed.languages = appendLimited(parsed.languages, value, 32)
				}
			case "extent":
				if current != nil && parent == "file" {
					if current.extent != "" {
						return parsed, errors.New("bibliographic record repeats a file extent")
					}
					current.extent = value
				}
			case "file":
				if current == nil {
					return parsed, errors.New("bibliographic record is truncated or not XML")
				}
				file, err := offerFromRDF(number, *current)
				if err != nil {
					return parsed, err
				}
				for _, existing := range parsed.files {
					if existing.Name == file.Name {
						return parsed, errors.New("bibliographic record repeats a file name")
					}
				}
				if len(parsed.files) >= 64 {
					return parsed, errors.New("bibliographic record lists too many files")
				}
				parsed.files = append(parsed.files, file)
				current = nil
			}
			stack = stack[:len(stack)-1]
			text.Reset()
		}
	}
	if !sawEbook || len(stack) != 0 || current != nil {
		return parsed, errors.New("bibliographic record is truncated or not XML")
	}
	if !ebookAboutMatches(ebookAbout, number) {
		return parsed, fmt.Errorf("bibliographic record is not text %s", number)
	}
	if parsed.titles == nil {
		parsed.titles = []string{}
	}
	if parsed.creators == nil {
		parsed.creators = []string{}
	}
	if parsed.languages == nil {
		parsed.languages = []string{}
	}
	if parsed.files == nil {
		parsed.files = []OfferedFile{}
	}
	return parsed, nil
}

func offerFromRDF(number string, file rdfFile) (OfferedFile, error) {
	name, kind, listed := fileIdentity(number, file.about)
	offer := OfferedFile{
		Name: name, Access: GutenbergListedAccess, RightsStatus: "not_resolved_at_file_level",
		DRMStatus: "not_assessed", Findings: []string{"This format is listed by the bibliographic record and is not acquired."},
	}
	if len(file.media) > 0 {
		offer.Format = file.media[0]
	}
	if extent, ok := positiveExtent(file.extent); ok {
		offer.Bytes = &extent
	}
	if !listed {
		offer.Findings = []string{"This file URL is not a Project Gutenberg ebook name for this text, so it is not acquired."}
		return offer, nil
	}
	if kind == "" {
		offer.Findings = []string{"This file names a different text number or an unsupported name, so it is not acquired."}
		return offer, nil
	}
	_, mapped := gutenbergEPUBObjects[kind]
	epubType := false
	for _, media := range file.media {
		if media == "application/epub+zip" {
			epubType = true
		}
	}
	if !mapped || !epubType || offer.Bytes == nil {
		if mapped && !epubType {
			offer.Findings = []string{"The record does not give this file the media type application/epub+zip, so it is not acquired."}
		} else if mapped && offer.Bytes == nil {
			offer.Findings = []string{"This EPUB has no positive declared extent, so it is not acquired."}
		} else if strings.Contains(kind, "epub") {
			offer.Findings = []string{"This EPUB name has no pinned cache object, so it is not acquired."}
		}
		return offer, nil
	}
	download, ok := gutenbergEPUBURL(number, kind)
	if !ok {
		return offer, errors.New("bibliographic record names an EPUB this client cannot place")
	}
	offer.Format = "application/epub+zip"
	offer.CandidateKind = "epub"
	offer.Access = GutenbergEPUBAccess
	offer.DownloadURL = download
	offer.Findings = []string{
		"The bibliographic record provides no checksum.",
		"Declared extent is not required to equal the response length.",
	}
	return offer, nil
}

// fileIdentity returns a safe display name. kind is set only for /ebooks/N.KIND
// of this text number. listed is false when the URL is not on gutenberg.org.
func fileIdentity(number, about string) (name, kind string, listed bool) {
	u, err := url.Parse(strings.TrimSpace(about))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "listed-file", "", false
	}
	if u.Host != "www.gutenberg.org" && u.Host != "gutenberg.org" {
		return "listed-file", "", false
	}
	segment := u.Path
	if i := strings.LastIndex(segment, "/"); i >= 0 {
		segment = segment[i+1:]
	}
	if !safeListedName(segment) {
		segment = "listed-file"
	}
	prefix := "/ebooks/" + number + "."
	if strings.HasPrefix(u.Path, prefix) {
		kind = strings.TrimPrefix(u.Path, prefix)
		if !safeKind(kind) {
			return segment, "", true
		}
		return number + "." + kind, kind, true
	}
	if strings.HasPrefix(u.Path, "/ebooks/") {
		return segment, "", true
	}
	return segment, "", true
}

func safeListedName(name string) bool {
	if name == "" || len(name) > 80 {
		return false
	}
	for i, c := range name {
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-'
		if !ok || (i == 0 && c == '.') {
			return false
		}
	}
	return !strings.Contains(name, "..")
}

func safeKind(kind string) bool {
	if kind == "" || len(kind) > 40 {
		return false
	}
	for _, c := range kind {
		ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' || c == '-'
		if !ok {
			return false
		}
	}
	return !strings.Contains(kind, "..")
}

func positiveExtent(value string) (int64, bool) {
	if value == "" || len(value) > 18 || value[0] == '0' {
		return 0, false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(value, 10, 64)
	return n, err == nil && n > 0
}

func ebookAboutMatches(about, number string) bool {
	about = strings.TrimSpace(about)
	if about == "ebooks/"+number {
		return true
	}
	u, err := url.Parse(about)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	if u.Host != "www.gutenberg.org" && u.Host != "gutenberg.org" {
		return false
	}
	return u.Path == "/ebooks/"+number
}

func resolveLicense(base, resource string) string {
	resource = strings.TrimSpace(resource)
	if resource == "" || len(resource) > 200 || strings.ContainsAny(resource, " \t\r\n") {
		return ""
	}
	if strings.Contains(resource, "://") {
		u, err := url.Parse(resource)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			return ""
		}
		return u.String()
	}
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "http://www.gutenberg.org" && resource == "license" {
		return base + "/license"
	}
	return ""
}

func attrLocal(el xml.StartElement, local string) string {
	for _, attr := range el.Attr {
		if attr.Name.Local == local {
			return attr.Value
		}
	}
	return ""
}

func stackHas(stack []string, local string) bool {
	for _, name := range stack {
		if name == local {
			return true
		}
	}
	return false
}

func appendLimited(values []string, value string, limit int) []string {
	if value == "" || len(values) >= limit {
		return values
	}
	return append(values, value)
}

func cleanRDFText(value string) string {
	value = strings.TrimSpace(strings.Join(strings.Fields(value), " "))
	if value == "" || len(value) > 16384 || strings.ContainsRune(value, '\x00') || !utf8OK(value) {
		return ""
	}
	return value
}

func utf8OK(value string) bool {
	return strings.ToValidUTF8(value, "") == value
}
