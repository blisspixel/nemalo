package discovery

import (
	"bufio"
	"container/heap"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/blisspixel/nemalo/internal/transfer"
)

// CatalogURL is the Project Gutenberg offline catalog. The website search
// pages are for people; this file is the permitted machine-readable catalog.
const CatalogURL = "https://www.gutenberg.org/cache/epub/feeds/pg_catalog.csv"

const catalogHeader = "Text#,Type,Issued,Title,Language,Authors,Subjects,LoCC,Bookshelves"

// maxCatalogBytes bounds a local catalog and a fetched copy. Tests may lower it.
var maxCatalogBytes int64 = 64 << 20

// Gutenberg searches and evaluates a local copy of the official catalog.
// Search and catalog evaluation do not contact a network. FileClient is used
// only by a bibliographic-record fetch and an EPUB download. Nil selects the
// pinned mirror client.
type Gutenberg struct {
	Catalog string
	// FileClient replaces the pinned mirror client. Tests set it.
	FileClient *http.Client
}

func NewGutenberg() *Gutenberg { return &Gutenberg{} }

func (g *Gutenberg) Search(ctx context.Context, query string, limit, offset int) (Page, error) {
	return g.SearchOrdered(ctx, query, OrderRelevance, limit, offset)
}

func (g *Gutenberg) SearchOrdered(ctx context.Context, query, order string, limit, offset int) (Page, error) {
	page := Page{Source: "gutenberg", Query: strings.TrimSpace(query), Order: order, Offset: offset, Results: []Result{}}
	if err := ctx.Err(); err != nil {
		return page, err
	}
	if page.Query == "" || len(page.Query) > 1000 || limit < 1 || limit > 50 || offset < 0 || offset > 10000 {
		return page, errors.New("search requires a query of 1-1000 bytes, limit 1-50, and offset 0-10000")
	}
	if order != OrderRelevance && order != OrderIdentifier {
		return page, errors.New("gutenberg search order must be relevance or identifier")
	}
	if strings.TrimSpace(g.Catalog) == "" {
		return page, errors.New("gutenberg search reads a local catalog; set gutenberg_catalog or pass --catalog. Nemalo does not query the Gutenberg website")
	}
	needle := strings.ToLower(page.Query)
	byID := order == OrderIdentifier
	kept := &hitHeap{byID: byID}
	need := offset + limit
	total := 0
	err := readCatalog(g.Catalog, func(rec catalogRecord) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		rank, ok := matchRank(needle, rec)
		if !ok {
			return nil
		}
		total++
		item := hit{rank: rank, num: rec.num, item: rec.result()}
		if kept.Len() < need {
			heap.Push(kept, item)
			return nil
		}
		if worseHit(item, kept.hits[0], byID) {
			return nil
		}
		heap.Pop(kept)
		heap.Push(kept, item)
		return nil
	})
	if err != nil {
		return page, err
	}
	page.Total = total
	if kept.Len() == 0 || offset >= kept.Len() {
		return page, nil
	}
	hits := append([]hit(nil), kept.hits...)
	sort.Slice(hits, func(i, j int) bool { return worseHit(hits[j], hits[i], byID) })
	end := offset + limit
	if end > len(hits) {
		end = len(hits)
	}
	for _, item := range hits[offset:end] {
		page.Results = append(page.Results, item.item)
	}
	return page, nil
}

func (g *Gutenberg) Evaluate(ctx context.Context, id string) (Evaluation, error) {
	e := Evaluation{ID: id, Source: "gutenberg", Access: "local_catalog", Restrictions: map[string]string{}, Files: []OfferedFile{}, Limitations: []string{
		"This row is from a local Project Gutenberg catalog file. Nemalo did not query the Gutenberg website.",
		"The catalog row does not list files, checksums, or a download URL, so nothing is offered for acquisition.",
		"The row does not state rights. Project Gutenberg's permissions are US-based and are not applied to the book by this record.",
		"Catalog metadata is not a check of the ebook bytes, a scan, or edition identity.",
	}}
	if !ValidGutenbergID(id) {
		return e, errors.New("evaluate requires gutenberg:NUMBER with a positive decimal text number")
	}
	if strings.TrimSpace(g.Catalog) == "" {
		return e, errors.New("gutenberg evaluation reads a local catalog; set gutenberg_catalog or pass --catalog. Nemalo does not query the Gutenberg website")
	}
	number, _ := strconv.Atoi(strings.TrimPrefix(id, "gutenberg:"))
	var found *catalogRecord
	err := readCatalog(g.Catalog, func(rec catalogRecord) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if rec.num == number {
			copy := rec
			found = &copy
			return errStopCatalog
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopCatalog) {
		return e, err
	}
	if found == nil {
		return e, fmt.Errorf("catalog has no record for %s", id)
	}
	e.Complete = true
	e.Title = []string{found.title}
	e.Creators = append([]string(nil), found.authors...)
	e.Languages = append([]string(nil), found.languages...)
	e.MediaType = []string{found.kind}
	if found.issued != "" {
		e.Dates = []string{found.issued}
	}
	e.Subjects = append([]string(nil), found.subjects...)
	e.Bookshelves = append([]string(nil), found.shelves...)
	e.Classes = append([]string(nil), found.classes...)
	e.LandingPage = "https://www.gutenberg.org/ebooks/" + strconv.Itoa(found.num)
	e.MetadataSHA256 = found.rowSHA256
	e.CheckedAt = time.Now().UTC()
	return e, nil
}

// ValidGutenbergID reports whether id is gutenberg: and a canonical positive
// decimal text number, without leading zeros, of at most 8 digits.
func ValidGutenbergID(id string) bool {
	number, ok := strings.CutPrefix(id, "gutenberg:")
	if !ok || number == "" || len(number) > 8 || number[0] == '0' {
		return false
	}
	for _, c := range number {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

type catalogRecord struct {
	num       int
	kind      string
	issued    string
	title     string
	authors   []string
	languages []string
	subjects  []string
	shelves   []string
	classes   []string
	rowSHA256 string
}

func (rec catalogRecord) result() Result {
	return Result{
		ID:          "gutenberg:" + strconv.Itoa(rec.num),
		Title:       rec.title,
		Authors:     append([]string(nil), rec.authors...),
		Languages:   append([]string(nil), rec.languages...),
		LandingPage: "https://www.gutenberg.org/ebooks/" + strconv.Itoa(rec.num),
		Access:      "local_catalog",
		MediaType:   rec.kind,
		Issued:      rec.issued,
	}
}

func matchRank(needle string, rec catalogRecord) (int, bool) {
	id := strconv.Itoa(rec.num)
	if needle == id || needle == "gutenberg:"+id || strings.Contains(strings.ToLower(rec.title), needle) {
		return 0, true
	}
	for _, author := range rec.authors {
		if strings.Contains(strings.ToLower(author), needle) {
			return 1, true
		}
	}
	for _, field := range [][]string{rec.subjects, rec.shelves} {
		for _, value := range field {
			if strings.Contains(strings.ToLower(value), needle) {
				return 2, true
			}
		}
	}
	return 0, false
}

var errStopCatalog = errors.New("catalog scan stopped")

func readCatalog(path string, visit func(catalogRecord) error) error {
	f, err := openCatalog(path)
	if err != nil {
		return err
	}
	defer f.Close()
	reader := csv.NewReader(bufio.NewReader(f))
	reader.FieldsPerRecord = 9
	reader.ReuseRecord = false
	header, err := reader.Read()
	if err != nil {
		return fmt.Errorf("read gutenberg catalog header: %w", err)
	}
	if strings.Join(header, ",") != catalogHeader {
		return errors.New("gutenberg catalog header is not the official pg_catalog.csv header")
	}
	seen := map[int]struct{}{}
	for row := 2; ; row++ {
		fields, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("gutenberg catalog row %d: %w", row, err)
		}
		rec, err := parseCatalogRecord(fields)
		if err != nil {
			return fmt.Errorf("gutenberg catalog row %d: %w", row, err)
		}
		if _, ok := seen[rec.num]; ok {
			return fmt.Errorf("gutenberg catalog text number %d is repeated", rec.num)
		}
		seen[rec.num] = struct{}{}
		if err := visit(rec); err != nil {
			if errors.Is(err, errStopCatalog) {
				return errStopCatalog
			}
			return err
		}
	}
}

func parseCatalogRecord(fields []string) (catalogRecord, error) {
	var rec catalogRecord
	for _, field := range fields {
		if strings.ContainsRune(field, 0) || !utf8.ValidString(field) || len(field) > 16384 {
			return rec, errors.New("field is not valid UTF-8, contains NUL, or exceeds 16384 bytes")
		}
	}
	number := fields[0]
	if !ValidGutenbergID("gutenberg:" + number) {
		return rec, errors.New("text number is not a canonical positive decimal")
	}
	rec.num, _ = strconv.Atoi(number)
	rec.kind = strings.TrimSpace(fields[1])
	if rec.kind == "" || len(rec.kind) > 40 || strings.ContainsAny(rec.kind, "\r\n") {
		return rec, errors.New("type is missing or not a single catalog token")
	}
	rec.issued = strings.TrimSpace(fields[2])
	if rec.issued != "" && !validIssued(rec.issued) {
		return rec, errors.New("issued date is not empty or YYYY-MM-DD")
	}
	rec.title = collapseSpaces(fields[3])
	if rec.title == "" {
		return rec, errors.New("title is empty")
	}
	rec.languages = splitLanguages(fields[4])
	rec.authors = splitList(fields[5])
	rec.subjects = splitList(fields[6])
	rec.classes = splitList(fields[7])
	rec.shelves = splitList(fields[8])
	var buf strings.Builder
	w := csv.NewWriter(&buf)
	if err := w.Write(fields); err != nil {
		return rec, err
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return rec, err
	}
	sum := sha256.Sum256([]byte(buf.String()))
	rec.rowSHA256 = hex.EncodeToString(sum[:])
	return rec, nil
}

func validIssued(value string) bool {
	if len(value) != 10 || value[4] != '-' || value[7] != '-' {
		return false
	}
	for i, c := range value {
		if i == 4 || i == 7 {
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func collapseSpaces(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

// splitList separates authors, subjects, Library of Congress classes, and
// bookshelves. The official catalog uses semicolons, including inside names
// that themselves contain commas.
func splitList(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return []string{}
	}
	parts := strings.Split(value, ";")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = collapseSpaces(part)
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return []string{}
	}
	return out
}

// splitLanguages separates short ASCII language tags. The official catalog
// uses semicolons ("en; fr"). A comma is accepted for the same shape. Any
// other field stays one language string.
func splitLanguages(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return []string{}
	}
	sep := ""
	switch {
	case strings.Contains(value, ";"):
		sep = ";"
	case strings.Contains(value, ","):
		sep = ","
	default:
		return []string{value}
	}
	parts := strings.Split(value, sep)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || len(part) > 16 || strings.ContainsFunc(part, func(r rune) bool { return r <= ' ' || r > 127 }) {
			return []string{collapseSpaces(value)}
		}
		out = append(out, part)
	}
	if len(out) == 0 {
		return []string{}
	}
	return out
}

func openCatalog(path string) (*os.File, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("gutenberg catalog path must be absolute")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("open gutenberg catalog: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("gutenberg catalog must be a regular file")
	}
	if info.Size() < 1 || info.Size() > maxCatalogBytes {
		return nil, errors.New("gutenberg catalog must be a non-empty file no larger than 64 MiB")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open gutenberg catalog: %w", err)
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() < 1 || st.Size() > maxCatalogBytes {
		f.Close()
		return nil, errors.New("gutenberg catalog must be a regular file no larger than 64 MiB")
	}
	return f, nil
}

// CatalogFile is one fetched official catalog.
type CatalogFile struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
	// Continuations counts requests that continued the file after a
	// dropped connection, or started it again when the source changed.
	Continuations int `json:"continuations,omitempty"`
}

// FetchCatalog downloads the official CSV to a new file. It is the explicit
// network step. Search and evaluation never call it.
func FetchCatalog(ctx context.Context, output string) (CatalogFile, error) {
	return fetchCatalog(ctx, output, CatalogURL, NewFileClient(func(host string) bool { return host == "www.gutenberg.org" }))
}

func fetchCatalog(ctx context.Context, output, rawURL string, client *http.Client) (CatalogFile, error) {
	var result CatalogFile
	if !filepath.IsAbs(output) {
		return result, errors.New("catalog output must be an absolute path")
	}
	if _, err := os.Lstat(output); err == nil {
		return result, errors.New("catalog output already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	parent, err := os.Stat(filepath.Dir(output))
	if err != nil || !parent.IsDir() {
		return result, errors.New("catalog output directory must exist")
	}
	f, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return result, err
	}
	remove := true
	defer func() {
		f.Close()
		if remove {
			os.Remove(output)
		}
	}()
	n, sum, continued, err := downloadCatalog(ctx, client, rawURL, f)
	if err != nil {
		return result, err
	}
	if err := f.Sync(); err != nil {
		return result, err
	}
	if err := f.Close(); err != nil {
		return result, err
	}
	if err := readCatalog(output, func(catalogRecord) error { return nil }); err != nil {
		return result, err
	}
	remove = false
	result.Path, result.Bytes, result.SHA256, result.Continuations = output, n, hex.EncodeToString(sum), continued
	return result, nil
}

// catalogAttempts bounds one catalog fetch: the first request and up to three
// more after a dropped connection.
const catalogAttempts = 4

// catalogPause waits longer before each further request; tests shorten it.
var catalogPause = func(attempt int) time.Duration { return time.Duration(attempt) * 2 * time.Second }

// downloadCatalog writes the catalog to f. A connection that drops is
// continued from the bytes already written, with a Range request conditional
// on the first response's strong validator, and accepted only as HTTP 206 with
// exactly the missing bytes of the same length. A source that answers with
// the whole file instead has changed, so the file is started again. Without a
// declared length or a strong validator, nothing is continued. The result is
// the whole file's length and SHA-256; it must equal the declared length.
func downloadCatalog(ctx context.Context, client *http.Client, rawURL string, f *os.File) (n int64, sum []byte, continued int, err error) {
	hasher := sha256.New()
	total, validator := int64(-1), ""
	for attempt := 1; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return n, nil, continued, err
		}
		req.Header.Set("Accept", "text/csv")
		req.Header.Set("User-Agent", "nemalo/0.1 (+https://github.com/blisspixel/nemalo)")
		if n > 0 {
			req.Header.Set("Range", "bytes="+strconv.FormatInt(n, 10)+"-")
			req.Header.Set("If-Range", validator)
		}
		resp, err := client.Do(req)
		if err != nil {
			if retryCatalog(ctx, err, n, total, validator, attempt) {
				continued++
				continue
			}
			return n, nil, continued, catalogStopped(n, total, err)
		}
		switch {
		case resp.StatusCode == http.StatusOK && n > 0:
			// The whole file again: it changed since the first response, or
			// the source does not continue. Start over.
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				resp.Body.Close()
				return 0, nil, continued, err
			}
			if err := f.Truncate(0); err != nil {
				resp.Body.Close()
				return 0, nil, continued, err
			}
			hasher.Reset()
			n = 0
			fallthrough
		case resp.StatusCode == http.StatusOK:
			total, validator = resp.ContentLength, catalogValidator(resp)
		case resp.StatusCode == http.StatusPartialContent && n > 0:
			want := "bytes " + strconv.FormatInt(n, 10) + "-" + strconv.FormatInt(total-1, 10) + "/" + strconv.FormatInt(total, 10)
			if strings.TrimSpace(resp.Header.Get("Content-Range")) != want || (resp.ContentLength >= 0 && resp.ContentLength != total-n) {
				resp.Body.Close()
				return n, nil, continued, errors.New("the catalog source continued with a different range than " + want + "; nothing was kept")
			}
		default:
			resp.Body.Close()
			return n, nil, continued, fmt.Errorf("catalog returned HTTP %d", resp.StatusCode)
		}
		if total > maxCatalogBytes {
			resp.Body.Close()
			return n, nil, continued, errors.New("gutenberg catalog is larger than 64 MiB")
		}
		written, copyErr := io.Copy(io.MultiWriter(f, hasher), io.LimitReader(resp.Body, maxCatalogBytes+1-n))
		resp.Body.Close()
		n += written
		if n > maxCatalogBytes {
			return n, nil, continued, errors.New("gutenberg catalog must be a non-empty file no larger than 64 MiB")
		}
		if copyErr == nil {
			if n < 1 || (total >= 0 && n != total) {
				return n, nil, continued, catalogStopped(n, total, io.ErrUnexpectedEOF)
			}
			return n, hasher.Sum(nil), continued, nil
		}
		if !retryCatalog(ctx, copyErr, n, total, validator, attempt) {
			return n, nil, continued, catalogStopped(n, total, copyErr)
		}
		continued++
	}
}

// retryCatalog waits and reports whether a stopped fetch may be tried again:
// a transport failure, attempts left, and, once bytes are kept, a declared
// length and a strong validator to continue them by.
func retryCatalog(ctx context.Context, err error, n, total int64, validator string, attempt int) bool {
	if !transfer.Transient(err) || ctx.Err() != nil || attempt >= catalogAttempts {
		return false
	}
	if n > 0 && (total < 1 || validator == "") {
		return false
	}
	select {
	case <-ctx.Done():
		return false
	case <-time.After(catalogPause(attempt)):
		return true
	}
}

func catalogStopped(n, total int64, err error) error {
	if total > 0 {
		return fmt.Errorf("the catalog download stopped after %d of %d bytes, and nothing was kept; try again: %w", n, total, err)
	}
	return fmt.Errorf("the catalog download stopped after %d bytes, and nothing was kept; try again: %w", n, err)
}

// catalogValidator is the response's strong validator for If-Range: a strong
// ETag, or a Last-Modified date at least a second before the response Date,
// which RFC 9110 section 8.8.2.2 treats as strong. Otherwise "".
func catalogValidator(resp *http.Response) string {
	if etag := resp.Header.Get("ETag"); StrongETag(etag) {
		return etag
	}
	modified, date := resp.Header.Get("Last-Modified"), resp.Header.Get("Date")
	lm, err1 := http.ParseTime(modified)
	d, err2 := http.ParseTime(date)
	if err1 == nil && err2 == nil && d.Sub(lm) >= time.Second && len(modified) <= 64 {
		return modified
	}
	return ""
}

type hit struct {
	rank int
	num  int
	item Result
}

type hitHeap struct {
	byID bool
	hits []hit
}

func (h hitHeap) Len() int { return len(h.hits) }
func (h hitHeap) Less(i, j int) bool {
	return worseHit(h.hits[i], h.hits[j], h.byID)
}
func (h hitHeap) Swap(i, j int) { h.hits[i], h.hits[j] = h.hits[j], h.hits[i] }
func (h *hitHeap) Push(value any) {
	h.hits = append(h.hits, value.(hit))
}
func (h *hitHeap) Pop() any {
	last := h.hits[len(h.hits)-1]
	h.hits = h.hits[:len(h.hits)-1]
	return last
}

func worseHit(a, b hit, byID bool) bool {
	if byID {
		return a.num > b.num
	}
	if a.rank != b.rank {
		return a.rank > b.rank
	}
	return a.num > b.num
}
