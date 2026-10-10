package assessment

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
)

var ErrLimit = errors.New("container inspection limit exceeded")

type Checks struct {
	Signature          string     `json:"signature"`
	ZIPCRC             string     `json:"zip_crc"`
	EPUBPackage        string     `json:"epub_package"`
	Conformance        string     `json:"conformance"`
	ContentReview      string     `json:"content_review"`
	Members            int        `json:"members,omitempty"`
	Warnings           []string   `json:"warnings,omitempty"`
	AccessRestrictions []string   `json:"access_restrictions,omitempty"`
	EPUB               *EPUBFacts `json:"epub,omitempty"`
}

type EPUBFacts struct {
	Titles                []string         `json:"titles"`
	Languages             []string         `json:"languages"`
	Identifiers           []EPUBIdentifier `json:"identifiers"`
	IdentifiersOmitted    int              `json:"identifiers_omitted,omitempty"`
	ReadingOrderDocuments int              `json:"reading_order_documents"`
	Images                int              `json:"images"`
	TextCharacters        int              `json:"non_whitespace_body_text_characters"`
	ExpandedBytes         uint64           `json:"expanded_bytes"`
	PageCountStatus       string           `json:"page_count_status"`
}

// EPUBIdentifier is one package identifier string. It is not a work, edition,
// recording, or track assignment.
type EPUBIdentifier struct {
	Value         string `json:"value"`
	Scheme        string `json:"scheme,omitempty"`
	PackageUnique bool   `json:"package_unique,omitempty"`
}

// Inspect performs bounded container checks without rendering or executing content.
// PDF/MP3 signatures are candidates, not parser, decoder, or malware validation.
func Inspect(r io.ReaderAt, size int64, format string) (Checks, error) {
	return InspectContext(context.Background(), r, size, format)
}

func InspectContext(ctx context.Context, r io.ReaderAt, size int64, format string) (Checks, error) {
	return inspectContext(ctx, r, size, format, nil, nil)
}

// EPUBDocument identifies an actual spine entry, not an inferred chapter. Data
// is a bounded member body valid only during the visitor call.
type EPUBDocument struct {
	Package string
	Path    string
	Media   string
	Linear  string
	Index   int
	Data    []byte
	Members map[string]EPUBMember
}

type EPUBMember struct {
	SHA256 string
	Bytes  uint64
	Media  string
}

// VisitEPUB shares container/package validation with assessment. The visitor
// must not publish results before the whole operation succeeds. No resources
// are fetched, rendered, executed, or extracted to disk.
func VisitEPUB(ctx context.Context, r io.ReaderAt, size int64, visit func(EPUBDocument) error) (Checks, error) {
	return inspectContext(ctx, r, size, "epub", visit, nil)
}

type memberRequest struct {
	name  string
	limit uint64
	data  []byte
}

// VisitEPUBResource performs the same complete bounded validation and returns
// only the selected validated local member. It never resolves URLs or paths.
func VisitEPUBResource(ctx context.Context, r io.ReaderAt, size int64, visit func(EPUBDocument) error, name string, limit uint64) (Checks, []byte, error) {
	if limit > 8<<20 {
		return Checks{}, nil, ErrLimit
	}
	request := &memberRequest{name: name, limit: limit}
	c, err := inspectContext(ctx, r, size, "epub", visit, request)
	if err != nil {
		return c, nil, err
	}
	return c, request.data, err
}

func inspectContext(ctx context.Context, r io.ReaderAt, size int64, format string, visit func(EPUBDocument) error, resource *memberRequest) (Checks, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	r = contextAt{ctx, r}
	c := Checks{Signature: "not_checked", ZIPCRC: "not_applicable", EPUBPackage: "not_applicable", Conformance: "not_checked", ContentReview: "not_checked"}
	if size <= 0 || size > 256<<20 {
		return c, errors.Join(ErrLimit, errors.New("invalid asset size"))
	}
	header := make([]byte, min(size, int64(8)))
	if _, err := r.ReadAt(header, 0); err != nil {
		return c, err
	}
	switch format {
	case "pdf":
		if !bytes.HasPrefix(header, []byte("%PDF-")) {
			return c, errors.New("PDF header missing")
		}
		tail := make([]byte, min(size, int64(2048)))
		if _, err := r.ReadAt(tail, size-int64(len(tail))); err != nil {
			return c, err
		}
		if !bytes.Contains(tail, []byte("%%EOF")) {
			return c, errors.New("PDF end marker missing")
		}
		c.Signature = "pdf_header_and_end_marker"
	case "mp3":
		if !bytes.HasPrefix(header, []byte("ID3")) && !(len(header) >= 2 && header[0] == 0xff && header[1]&0xe0 == 0xe0) {
			return c, errors.New("MP3 candidate signature missing")
		}
		c.Signature = "mp3_candidate_only"
	case "epub":
		if !bytes.HasPrefix(header, []byte("PK\x03\x04")) {
			return c, errors.New("ZIP signature missing")
		}
		c.Signature = "zip"
		if err := inspectEPUB(ctx, r, size, &c, visit, resource); err != nil {
			return c, err
		}
	default:
		return c, errors.New("unsupported assessment format")
	}
	return c, nil
}

func inspectEPUB(ctx context.Context, r io.ReaderAt, size int64, c *Checks, visit func(EPUBDocument) error, resource *memberRequest) error {
	// Bound directory parsing before zip.NewReader can allocate per-member state.
	metadata := &metadataReader{r: r, ctx: ctx, remaining: 2 << 20, workRemaining: 1 << 30, limited: true}
	z, err := zip.NewReader(metadata, size)
	metadata.limited = false
	if err != nil {
		return err
	}
	if len(z.File) == 0 || len(z.File) > 10000 {
		return errors.Join(ErrLimit, errors.New("EPUB member count outside limits"))
	}
	if err := compressedLayout(z.File, size); err != nil {
		return err
	}
	files := map[string]*zip.File{}
	members := map[string]EPUBMember{}
	type htmlMeasurement struct {
		characters int
		active     bool
		external   bool
	}
	measured := map[string]htmlMeasurement{}
	var expanded uint64
	for _, f := range z.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := strings.TrimSuffix(f.Name, "/")
		if !fs.ValidPath(name) || strings.ContainsAny(name, "\\:\x00") || f.Mode()&fs.ModeSymlink != 0 || (!f.FileInfo().IsDir() && !f.Mode().IsRegular()) || files[f.Name] != nil {
			return fmt.Errorf("unsafe or duplicate EPUB member %q", f.Name)
		}
		files[f.Name] = f
		if f.UncompressedSize64 > 32<<20 || expanded > 128<<20-f.UncompressedSize64 {
			return errors.Join(ErrLimit, errors.New("EPUB expanded size exceeds limit"))
		}
		expanded += f.UncompressedSize64
		body, err := f.Open()
		if err != nil {
			return err
		}
		h := sha256.New()
		var destination io.Writer = io.Discard
		if visit != nil {
			destination = h
		}
		n, err := io.Copy(destination, io.LimitReader(contextReader{ctx, body}, int64(f.UncompressedSize64)+1))
		closeErr := body.Close()
		if err != nil || closeErr != nil || n != int64(f.UncompressedSize64) {
			return fmt.Errorf("EPUB member failed CRC/length checks: %q", f.Name)
		}
		if visit != nil {
			members[f.Name] = EPUBMember{SHA256: hex.EncodeToString(h.Sum(nil)), Bytes: f.UncompressedSize64}
		}
	}
	c.Members, c.ZIPCRC = len(files), "passed"
	c.EPUB = &EPUBFacts{ExpandedBytes: expanded, PageCountStatus: "unknown_no_fixed_page_count", Titles: []string{}, Languages: []string{}, Identifiers: []EPUBIdentifier{}}
	if files["META-INF/encryption.xml"] != nil {
		c.Warnings = append(c.Warnings, "encryption declarations present; may include font obfuscation")
		c.AccessRestrictions = append(c.AccessRestrictions, "encryption_declarations")
	}
	mt, err := member(files, "mimetype", 128)
	if err != nil || string(mt) != "application/epub+zip" || z.File[0].Name != "mimetype" || z.File[0].Method != zip.Store {
		return errors.New("invalid EPUB mimetype placement, compression, or value")
	}
	container, err := member(files, "META-INF/container.xml", 1<<20)
	if err != nil {
		return err
	}
	var doc struct {
		XMLName xml.Name `xml:"urn:oasis:names:tc:opendocument:xmlns:container container"`
		Roots   []struct {
			Path  string `xml:"full-path,attr"`
			Media string `xml:"media-type,attr"`
		} `xml:"rootfiles>rootfile"`
	}
	if err := xml.Unmarshal(container, &doc); err != nil || len(doc.Roots) == 0 || len(doc.Roots) > 8 {
		return errors.New("invalid EPUB container document")
	}
	for _, entry := range doc.Roots {
		if !fs.ValidPath(entry.Path) || strings.ContainsAny(entry.Path, "\\:") || entry.Media != "application/oebps-package+xml" {
			return errors.New("invalid EPUB package path or media type")
		}
		data, err := member(files, entry.Path, 2<<20)
		if err != nil {
			return err
		}
		var pkg struct {
			XMLName     xml.Name        `xml:"http://www.idpf.org/2007/opf package"`
			UniqueID    string          `xml:"unique-identifier,attr"`
			Titles      []string        `xml:"metadata>title"`
			Languages   []string        `xml:"metadata>language"`
			Identifiers []opfIdentifier `xml:"metadata>identifier"`
			Items       []struct {
				ID         string `xml:"id,attr"`
				Href       string `xml:"href,attr"`
				Media      string `xml:"media-type,attr"`
				Properties string `xml:"properties,attr"`
			} `xml:"manifest>item"`
			Spine []struct {
				ID     string `xml:"idref,attr"`
				Linear string `xml:"linear,attr"`
			} `xml:"spine>itemref"`
		}
		if err := xml.Unmarshal(data, &pkg); err != nil || len(pkg.Items) == 0 || len(pkg.Spine) == 0 || len(pkg.Items) > 10000 || len(pkg.Spine) > 10000 {
			return errors.New("invalid EPUB package manifest or spine")
		}
		ids := map[string]bool{}
		text := map[string]int{}
		documents := map[string]EPUBDocument{}
		c.EPUB.Titles = append(c.EPUB.Titles, pkg.Titles...)
		c.EPUB.Languages = append(c.EPUB.Languages, pkg.Languages...)
		kept, omitted := collectIdentifiers(c.EPUB.Identifiers, pkg.UniqueID, pkg.Identifiers)
		c.EPUB.Identifiers = kept
		c.EPUB.IdentifiersOmitted += omitted
		if len(pkg.Titles) == 0 || len(pkg.Languages) == 0 {
			c.Warnings = append(c.Warnings, "title or language metadata missing")
		}
		for _, item := range pkg.Items {
			if item.ID == "" || ids[item.ID] || item.Href == "" {
				return errors.New("invalid EPUB manifest identity")
			}
			ids[item.ID] = true
			documents[item.ID] = EPUBDocument{Package: entry.Path, Path: item.Href, Media: item.Media}
			for _, property := range strings.Fields(item.Properties) {
				if property == "scripted" {
					c.Warnings = append(c.Warnings, "scripted manifest item declared")
					c.AccessRestrictions = append(c.AccessRestrictions, "scripted_manifest_item")
				}
			}
			// Remote resource declarations need content policy review; never fetch them.
			if strings.Contains(item.Href, ":") || strings.HasPrefix(item.Href, "//") {
				c.Warnings = append(c.Warnings, "remote manifest resource declared")
				continue
			}
			reference, err := url.Parse(item.Href)
			if err != nil || reference.RawQuery != "" || strings.HasPrefix(reference.Path, "/") || strings.ContainsAny(reference.Path, "\\:\x00") {
				return errors.New("invalid EPUB member reference")
			}
			name := path.Join(path.Dir(entry.Path), reference.Path)
			if !fs.ValidPath(name) || files[name] == nil {
				return fmt.Errorf("EPUB manifest references missing member %q", name)
			}
			documents[item.ID] = EPUBDocument{Package: entry.Path, Path: name, Media: item.Media}
			if visit != nil {
				m := members[name]
				m.Media = item.Media
				members[name] = m
			}
			if strings.HasPrefix(item.Media, "image/") {
				c.EPUB.Images++
			}
			if item.Media == "application/xhtml+xml" || item.Media == "text/html" {
				facts, ok := measured[name]
				if !ok {
					data, err := member(files, name, 32<<20)
					if err != nil {
						return err
					}
					n, active, external, err := htmlPolicyContext(ctx, data)
					if err != nil {
						return fmt.Errorf("EPUB document %q: %w", name, err)
					}
					facts = htmlMeasurement{n, active, external}
					measured[name] = facts
				}
				text[item.ID] = facts.characters
				if facts.active || facts.external {
					c.Warnings = append(c.Warnings, "active or externally referenced content in "+name)
				}
				if facts.active {
					c.AccessRestrictions = append(c.AccessRestrictions, "active_content")
				}
			}
		}
		seenSpine := map[string]bool{}
		for index, spine := range pkg.Spine {
			if !ids[spine.ID] {
				return errors.New("EPUB spine references missing manifest item")
			}
			if seenSpine[spine.ID] {
				c.Warnings = append(c.Warnings, "repeated reading-order reference")
				c.AccessRestrictions = append(c.AccessRestrictions, "repeated_reading_order")
				continue
			}
			seenSpine[spine.ID] = true
			if visit != nil {
				d := documents[spine.ID]
				d.Members = members
				d.Index, d.Linear = index, spine.Linear
				if d.Media == "application/xhtml+xml" || d.Media == "text/html" {
					d.Data, err = member(files, d.Path, 32<<20)
					if err != nil {
						return err
					}
				}
				if err := visit(d); err != nil {
					return err
				}
			}
			if n, ok := text[spine.ID]; ok {
				c.EPUB.ReadingOrderDocuments++
				c.EPUB.TextCharacters += n
			}
		}
	}
	if c.EPUB.TextCharacters == 0 {
		c.Warnings = append(c.Warnings, "no measured body text in HTML reading order; may be image-based or unsupported")
	}
	c.EPUBPackage = "container_manifest_spine_checked"
	c.ContentReview = "limited_token_indicators_only"
	if resource != nil {
		resource.data, err = member(files, resource.name, resource.limit)
		if err != nil {
			return err
		}
	}
	return nil
}

type opfIdentifier struct {
	ID       string `xml:"id,attr"`
	Scheme   string `xml:"scheme,attr"`
	SchemeNS string `xml:"http://www.idpf.org/2007/opf scheme,attr"`
	Value    string `xml:",chardata"`
}

func collectIdentifiers(existing []EPUBIdentifier, unique string, raw []opfIdentifier) ([]EPUBIdentifier, int) {
	omitted := 0
	unique = strings.TrimSpace(unique)
	if len(unique) > 200 || strings.ContainsAny(unique, "\x00\r\n") {
		unique = ""
	}
	for _, item := range raw {
		if len(existing) >= 32 {
			omitted++
			continue
		}
		scheme, ok := oneIdentifierScheme(item.Scheme, item.SchemeNS)
		value := strings.TrimSpace(item.Value)
		id := strings.TrimSpace(item.ID)
		if !ok || value == "" || len(value) > 1000 || strings.ContainsAny(value, "\x00\r\n") || len(id) > 200 || strings.ContainsAny(id, "\x00\r\n") {
			omitted++
			continue
		}
		existing = append(existing, EPUBIdentifier{Value: value, Scheme: scheme, PackageUnique: unique != "" && id == unique})
	}
	return existing, omitted
}

func oneIdentifierScheme(plain, namespaced string) (string, bool) {
	plain = strings.TrimSpace(plain)
	namespaced = strings.TrimSpace(namespaced)
	if len(plain) > 64 || len(namespaced) > 64 || strings.ContainsAny(plain, "\x00\r\n") || strings.ContainsAny(namespaced, "\x00\r\n") {
		return "", false
	}
	if plain != "" && namespaced != "" && !strings.EqualFold(plain, namespaced) {
		return "", false
	}
	if namespaced != "" {
		return namespaced, true
	}
	return plain, true
}

type metadataReader struct {
	r             io.ReaderAt
	ctx           context.Context
	remaining     int
	workRemaining int64
	limited       bool
}

func (r *metadataReader) ReadAt(p []byte, off int64) (int, error) {
	if r.ctx != nil {
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}
	}
	if int64(len(p)) > r.workRemaining {
		return 0, errors.Join(ErrLimit, errors.New("EPUB compressed read budget exceeded"))
	}
	r.workRemaining -= int64(len(p))
	if r.limited {
		if len(p) > r.remaining {
			return 0, errors.Join(ErrLimit, errors.New("EPUB ZIP metadata read budget exceeded"))
		}
		r.remaining -= len(p)
	}
	return r.r.ReadAt(p, off)
}

// Validate all compressed ranges before opening a decompressor. Central-directory
// order is not physical order, and signed offsets must be checked before casts.
func compressedLayout(files []*zip.File, size int64) error {
	type interval struct{ start, end int64 }
	ranges := make([]interval, 0, len(files))
	var compressed uint64
	for _, f := range files {
		offset, err := f.DataOffset()
		if err != nil {
			return err
		}
		if offset < 0 || offset > size || f.CompressedSize64 > uint64(size-offset) || f.CompressedSize64 > 256<<20 || compressed > 256<<20-f.CompressedSize64 {
			return errors.Join(ErrLimit, errors.New("EPUB compressed ranges exceed asset or work limits"))
		}
		compressed += f.CompressedSize64
		ranges = append(ranges, interval{offset, offset + int64(f.CompressedSize64)})
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].start < ranges[j].start })
	for i := 1; i < len(ranges); i++ {
		if ranges[i].start < ranges[i-1].end || ranges[i].start == ranges[i-1].start {
			return errors.New("EPUB compressed member ranges overlap")
		}
	}
	return nil
}

// These are positive indicators and text measurements, not a sanitizer or a DOM
// security verdict. No member is rendered and no referenced URL is fetched.
func htmlFacts(data []byte) (int, bool, error) {
	return htmlFactsContext(context.Background(), data)
}

func htmlFactsContext(ctx context.Context, data []byte) (int, bool, error) {
	n, active, external, err := htmlPolicyContext(ctx, data)
	return n, active || external, err
}

func htmlPolicyContext(ctx context.Context, data []byte) (int, bool, bool, error) {
	if !utf8.Valid(data) {
		return 0, false, false, errors.New("non-UTF-8 document is unsupported")
	}
	z := html.NewTokenizer(bytes.NewReader(data))
	z.SetMaxBuf(1 << 20)
	body, hidden, count, active := false, "", 0, false
	external := false
	for {
		if err := ctx.Err(); err != nil {
			return count, active, external, err
		}
		typ := z.Next()
		if typ == html.ErrorToken {
			if errors.Is(z.Err(), io.EOF) {
				return count, active, external, nil
			}
			return count, active, external, z.Err()
		}
		token := z.Token()
		if typ == html.StartTagToken || typ == html.SelfClosingTagToken {
			switch token.Data {
			case "body":
				body = true
			case "script", "iframe", "object", "embed":
				active = true
			}
			if token.Data == "script" || token.Data == "style" {
				hidden = token.Data
			}
			for _, a := range token.Attr {
				v := strings.ToLower(strings.TrimSpace(a.Val))
				if strings.HasPrefix(a.Key, "on") || ((a.Key == "href" || a.Key == "src" || a.Key == "data") && strings.HasPrefix(v, "javascript:")) {
					active = true
				}
				if (a.Key == "src" || a.Key == "data") && (strings.Contains(v, ":") || strings.HasPrefix(v, "//")) {
					external = true
				}
			}
		}
		if typ == html.EndTagToken {
			if token.Data == "body" {
				body = false
			}
			if token.Data == hidden {
				hidden = ""
			}
		}
		if typ == html.TextToken && body && hidden == "" {
			for _, r := range token.Data {
				if !unicode.IsSpace(r) {
					count++
				}
			}
		}
	}
}

func member(files map[string]*zip.File, name string, limit uint64) ([]byte, error) {
	f := files[name]
	if f == nil {
		return nil, fmt.Errorf("EPUB member missing or too large: %q", name)
	}
	if f.UncompressedSize64 > limit {
		return nil, errors.Join(ErrLimit, fmt.Errorf("EPUB member too large: %q", name))
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, int64(limit)+1))
}
