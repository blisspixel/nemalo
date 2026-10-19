// Package content provides stateless, offline, source-bound text retrieval.
package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/audio"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/safeio"
	"golang.org/x/net/html"
)

const Extractor = "nemalo.epub.text/1"
const maxInput = 32 << 20
const maxManagedInput = 256 << 20
const maxText = 8 << 20
const maxUnits = 512

// maxRequestUnit bounds a requested unit: an EPUB reading-order document or a
// zero-based PDF physical page index.
const maxRequestUnit = 10000

var errBudget = errors.New("content retrieval budget exceeded")
var errUnsupported = errors.New("unsupported EPUB extraction; no excerpt returned")

type Request struct {
	SchemaVersion  int
	Catalog        string
	Root           string
	AssetID        string
	Unit           int
	Offset         int
	MaxBytes       int
	Cursor         string
	UnitsOnly      bool
	Representation string
	Part           string
	Resource       bool
	// Sample is the encoded sample at which an mpeg-audio/1 read starts.
	Sample int64
	// Find locates every exact occurrence of this text in a text
	// representation instead of reading a range.
	Find string
	// Navigation selects epub-nav/1 evidence when a book declares more than
	// one: nav-doc or ncx. It is required for a read in that case.
	Navigation string
	// Label selects an epub-page-list/1 marker by its exact declared label.
	// Several markers with that label are returned as candidates.
	Label string
}

func (r Request) Validate() error {
	if r.SchemaVersion != 1 || r.Catalog == "" || r.Root == "" || len(r.Catalog) > 4096 || len(r.Root) > 4096 || len(r.AssetID) != 71 || !strings.HasPrefix(r.AssetID, "sha256:") {
		return errors.New("content requires a catalog, explicit root, and sha256: asset ID")
	}
	b, err := hex.DecodeString(r.AssetID[7:])
	if err != nil || len(b) != 32 || hex.EncodeToString(b) != r.AssetID[7:] {
		return errors.New("invalid asset ID")
	}
	if r.Unit < 0 || r.Unit >= maxRequestUnit || r.Offset < 0 || r.Offset > maxText || r.MaxBytes < 4 || r.MaxBytes > 65536 || len(r.Cursor) > 2048 {
		return errors.New("content requires unit 0-9999, offset 0-8388608, max-bytes 4-65536, and cursor at most 2048 bytes")
	}
	if r.Sample < 0 || r.Sample > 1<<40 || (r.Sample != 0 && r.Representation != "mpeg-audio/1") {
		return errors.New("sample applies only to mpeg-audio/1 and must be 0 through 2^40")
	}
	if len(r.Find) > 1000 || !utf8.ValidString(r.Find) || (r.Find != "" && (r.Cursor != "" || r.UnitsOnly || r.Part != "" || r.Resource || (r.Representation != "epub-text/2" && r.Representation != "pdf-text/1" && r.Representation != ""))) {
		return errors.New("find takes valid UTF-8 text of at most 1000 bytes and applies to epub-text/2 or pdf-text/1 without a cursor, units, or parts")
	}
	if r.Cursor != "" && (r.Unit != 0 || r.Offset != 0 || r.UnitsOnly || r.Sample != 0) {
		return errors.New("cursor cannot be combined with unit, offset, or units listing")
	}
	if len(r.Representation) > 80 || len(r.Part) > 256 || (r.Cursor != "" && r.Part != "") || (r.UnitsOnly && (r.Part != "" || r.Resource)) {
		return errors.New("invalid representation, part, or cursor combination")
	}
	if (r.Resource && r.Representation != "epub-structure/1") || (r.UnitsOnly && r.Representation == "epub-source/1") {
		return errors.New("operation is unavailable for the selected representation; inspect content capabilities")
	}
	if r.Navigation != "" && (r.Navigation != "nav-doc" && r.Navigation != "ncx" || r.Representation != "epub-nav/1" || r.Cursor != "") {
		return errors.New("navigation source must be nav-doc or ncx, applies only to an epub-nav/1 request, and cannot be combined with a cursor")
	}
	if r.Label != "" && (len(r.Label) > maxNavLabel || !utf8.ValidString(r.Label) || r.Representation != "epub-page-list/1" || r.Cursor != "" || r.UnitsOnly || r.Unit != 0) {
		return errors.New("label is valid UTF-8 of at most 256 bytes and applies only to an epub-page-list/1 read, without a cursor or unit")
	}
	return nil
}

type Unit struct {
	Index      int    `json:"index"`
	Kind       string `json:"kind"`
	Package    string `json:"package"`
	Member     string `json:"member"`
	Linear     string `json:"linear"`
	TextBytes  int    `json:"text_bytes"`
	Extraction string `json:"extraction"`
	Language   string `json:"language,omitempty"`
	Direction  string `json:"direction,omitempty"`
	Parts      int    `json:"parts,omitempty"`
	KnownGaps  int    `json:"known_gaps,omitempty"`
	// PageLabel is a PDF page's printed label; Index is then the zero-based
	// physical page index.
	PageLabel string `json:"page_label,omitempty"`
}

// Locator offsets are zero-based and half-open in OffsetUnit. An omitted
// OffsetUnit retains the original UTF-8 extracted-text contract. No offset
// refers to ZIP positions or inferred rendered pages.
type Locator struct {
	SchemaVersion    int    `json:"schema_version"`
	AssetID          string `json:"asset_id"`
	Extractor        string `json:"extractor"`
	RepresentationID string `json:"representation_id"`
	Unit             int    `json:"unit"`
	Start            int    `json:"start"`
	End              int    `json:"end"`
	Member           string `json:"member,omitempty"`
	OffsetUnit       string `json:"offset_unit,omitempty"`
	PageLabel        string `json:"page_label,omitempty"`
}

type Result struct {
	SchemaVersion    int           `json:"schema_version"`
	Status           string        `json:"status"`
	AssetID          string        `json:"asset_id"`
	CatalogID        string        `json:"catalog_id,omitempty"`
	SourcePath       string        `json:"source_path,omitempty"`
	Extractor        string        `json:"extractor"`
	RepresentationID string        `json:"representation_id,omitempty"`
	Units            []Unit        `json:"units,omitempty"`
	Text             string        `json:"text,omitempty"`
	Locator          *Locator      `json:"locator,omitempty"`
	Continuation     string        `json:"continuation,omitempty"`
	EndOfUnit        bool          `json:"end_of_unit"`
	EndOfSource      bool          `json:"end_of_source"`
	UnsupportedUnits []int         `json:"unsupported_units,omitempty"`
	Limitations      []string      `json:"limitations"`
	Representation   string        `json:"representation,omitempty"`
	Parts            []Part        `json:"parts,omitempty"`
	Coverage         *Coverage     `json:"coverage,omitempty"`
	Resource         *ResourceData `json:"resource,omitempty"`
	Gaps             []Gap         `json:"gaps,omitempty"`
	// Breaks are the structural line feeds of an epub-text/2, epub-nav/1, or
	// epub-structure/1 range; other line feeds in Text are source whitespace.
	Breaks     []Break      `json:"breaks,omitempty"`
	Audio      *audio.Track `json:"audio,omitempty"`
	AudioRange *audio.Range `json:"audio_range,omitempty"`
	// MP4 is the mp4-audio/1 sample table summary and declared chapters.
	MP4     *audio.MP4 `json:"mp4,omitempty"`
	Matches []Match    `json:"matches,omitempty"`
	// Navigation is the source-declared table of contents for epub-nav/1.
	// A read carries the one selected entry. Spine units stay in Units.
	Navigation []NavDocument `json:"navigation,omitempty"`
	// RangeStatus is within_range or range_end for an epub-nav/1 read.
	// A spine boundary is EndOfUnit; it does not by itself end the declared range.
	RangeStatus string   `json:"range_status,omitempty"`
	RangeEnd    *Locator `json:"range_end,omitempty"`
	// PageLists, Pagination, Page, and PageEnd are epub-page-list/1 results:
	// the declared page lists, their stated source, the selected marker, and
	// the next marker when it is established to follow.
	PageLists  []PageList  `json:"page_lists,omitempty"`
	Pagination *Pagination `json:"pagination,omitempty"`
	Page       *PageEntry  `json:"page,omitempty"`
	PageEnd    *Locator    `json:"page_end,omitempty"`
}

type cursor struct {
	SchemaVersion    int    `json:"schema_version"`
	AssetID          string `json:"asset_id"`
	Extractor        string `json:"extractor"`
	RepresentationID string `json:"representation_id"`
	Unit             int    `json:"unit"`
	Offset           int    `json:"offset"`
	Representation   string `json:"representation,omitempty"`
	Part             string `json:"part,omitempty"`
	Member           string `json:"member,omitempty"`
	MemberSHA256     string `json:"member_sha256,omitempty"`
}

func parseCursor(token string) (cursor, error) {
	var c cursor
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return c, errors.New("invalid continuation encoding")
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	canonical, err := json.Marshal(c)
	if err != nil || !bytes.Equal(b, canonical) || base64.RawURLEncoding.EncodeToString(b) != token || c.SchemaVersion != 1 || c.Unit < 0 || c.Unit >= maxRequestUnit || c.Offset < 0 || c.Offset > maxText {
		return c, errors.New("invalid continuation schema or fields")
	}
	return c, nil
}

func encodeCursor(c cursor) string {
	b, _ := json.Marshal(c) // This fixed scalar schema has no failing marshaler.
	return base64.RawURLEncoding.EncodeToString(b)
}

// Get performs no writes, network calls, rendering, or consumption bookkeeping.
// Every call revalidates the bytes. An error returns no text or continuation.
func Get(ctx context.Context, req Request) (result Result, err error) {
	result = Result{SchemaVersion: 1, Status: "invalid_request", Extractor: Extractor, Limitations: []string{
		"Plain source text, not rendered layout, a summary, or proof of reading.",
		"Reading-order documents are not necessarily chapters. No page counts inferred.",
		"CSS visibility, typography, images, and semantic completeness are not reconstructed.",
		"Rights and edition metadata are not established by a byte-identity catalog; unknown.",
		"No antivirus scan, safety guarantee, reading history, or consumption acknowledgement.",
	}}
	// Failed or cancelled retrieval must not leak a usable-looking partial result.
	defer func() {
		if err != nil {
			err = diagnosticError{err}
			result.Text, result.Continuation, result.Locator = "", "", nil
			result.Units = nil
			result.Parts, result.Coverage, result.Resource, result.Gaps = nil, nil, nil, nil
			result.Audio, result.AudioRange, result.Matches, result.MP4 = nil, nil, nil, nil
			result.Navigation, result.RangeEnd = nil, nil
			result.PageLists, result.Pagination, result.Page, result.PageEnd = nil, nil, nil, nil
			result.RangeStatus = ""
			result.EndOfUnit, result.EndOfSource = false, false
		}
	}()
	if err = req.Validate(); err != nil {
		return result, err
	}
	result.AssetID = req.AssetID
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err = ctx.Err(); err != nil {
		result.Status = "cancelled"
		return result, err
	}
	if req.Representation == "" {
		req.Representation = DefaultRepresentation
		if req.Cursor != "" {
			// A continuation keeps the representation it was issued for, so an
			// existing epub-text/1 reference never moves to another extractor.
			c, parseErr := parseCursor(req.Cursor)
			if parseErr != nil {
				return result, parseErr
			}
			req.Representation = c.Representation
			if req.Representation == "" {
				req.Representation = "epub-text/1"
			}
		}
	}
	switch req.Representation {
	case "epub-text/1":
	case "epub-text/2":
		if req.Part != "" || req.Resource {
			return result, errors.New("part/resource access requires a structured EPUB representation")
		}
		return getTextV2(ctx, req, result)
	case "epub-structure/1", "epub-source/1":
		return getStructured(ctx, req, result)
	case "mpeg-audio/1":
		return getAudio(ctx, req, result)
	case "mp4-audio/1":
		return getMP4(ctx, req, result)
	case "pdf-text/1":
		if req.Part != "" || req.Resource {
			return result, errors.New("part/resource access is unavailable for pdf-text/1")
		}
		return getPDFText(ctx, req, result)
	case "epub-page-list/1":
		if req.Part != "" || req.Resource || req.Find != "" {
			return result, errors.New("part, resource, and find are unavailable for epub-page-list/1")
		}
		return getPageList(ctx, req, result)
	case "epub-nav/1":
		if req.Part != "" || req.Resource || req.Find != "" || req.Sample != 0 {
			return result, errors.New("epub-nav/1 reads a declared entry by --unit, or continues with its cursor")
		}
		return getNavigation(ctx, req, result)
	default:
		result.Status = "unsupported_representation"
		return result, errors.New("unsupported content representation; inspect content capabilities")
	}
	result.Representation = "epub-text/1"
	if req.Part != "" || req.Resource {
		return result, errors.New("part/resource access requires a structured EPUB representation")
	}
	var continuation cursor
	if req.Cursor != "" {
		continuation, err = parseCursor(req.Cursor)
		if err != nil {
			return result, err
		}
		if continuation.AssetID != req.AssetID || continuation.Extractor != Extractor || continuation.Part != "" || continuation.Member != "" || continuation.MemberSHA256 != "" || (continuation.Representation != "" && continuation.Representation != "epub-text/1") {
			result.Status = "stale_reference"
			return result, errors.New("continuation belongs to another asset or extractor")
		}
		req.Unit, req.Offset = continuation.Unit, continuation.Offset
	}
	data, err := resolveSource(ctx, req, &result)
	if err != nil {
		return result, err
	}
	if !bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		result.Status = "unsupported_extraction"
		return result, errUnsupported
	}
	texts := []string{}
	units := []Unit{}
	total := 0
	packagePath := ""
	checks, err := assessment.VisitEPUB(ctx, bytes.NewReader(data), int64(len(data)), func(d assessment.EPUBDocument) error {
		if len(units) >= maxUnits {
			return errBudget
		}
		if len(d.Package) > 4096 || len(d.Path) > 4096 {
			return errBudget
		}
		if d.Index != len(units) {
			return errUnsupported
		}
		if packagePath != "" && packagePath != d.Package {
			return errUnsupported
		}
		packagePath = d.Package
		if d.Linear != "" && d.Linear != "yes" && d.Linear != "no" {
			return errUnsupported
		}
		text, extractErr := "", error(errUnsupported)
		if d.Media == "application/xhtml+xml" {
			text, extractErr = extractText(ctx, d.Data, maxText-total)
		}
		extraction := "supported_plain_text"
		if errors.Is(extractErr, errUnsupported) {
			extraction = "unsupported_document"
			result.UnsupportedUnits = append(result.UnsupportedUnits, d.Index)
		} else if extractErr != nil {
			return extractErr
		}
		total += len(text)
		linear := d.Linear
		if linear == "" {
			linear = "yes"
		}
		units = append(units, Unit{Index: d.Index, Kind: "epub_reading_order_document", Package: d.Package, Member: d.Path, Linear: linear, TextBytes: len(text), Extraction: extraction})
		texts = append(texts, text)
		return nil
	})
	if err != nil || len(checks.Warnings) != 0 || len(units) == 0 {
		result.Status = "malformed_content"
		if errors.Is(err, errUnsupported) || (err == nil && len(checks.Warnings) != 0) {
			result.Status = "unsupported_extraction"
		}
		if errors.Is(err, errBudget) || errors.Is(err, assessment.ErrLimit) || errors.Is(err, html.ErrBufferExceeded) {
			result.Status = "budget_exceeded"
		}
		if ctx.Err() != nil {
			result.Status, err = "cancelled", ctx.Err()
		}
		if err == nil {
			err = fmt.Errorf("extraction requires review: %q", checks.Warnings)
		}
		return result, err
	}
	representation, err := json.Marshal(struct {
		Extractor string
		Units     []Unit
		Texts     []string
	}{Extractor, units, texts})
	if err != nil {
		return result, err
	}
	hash := sha256.Sum256(representation)
	result.RepresentationID = "sha256:" + hex.EncodeToString(hash[:])
	result.Units = units
	if req.Cursor != "" && continuation.RepresentationID != result.RepresentationID {
		result.Status = "stale_reference"
		return result, errors.New("continuation representation changed")
	}
	if req.UnitsOnly {
		result.Status = "units_available"
	} else {
		if req.Unit >= len(texts) || req.Offset > len(texts[req.Unit]) || (req.Offset < len(texts[req.Unit]) && !utf8.RuneStart(texts[req.Unit][req.Offset])) {
			result.Status = "invalid_request"
			return result, errors.New("unit or UTF-8 byte offset is outside the extracted source")
		}
		if units[req.Unit].Extraction != "supported_plain_text" {
			result.Status = "unsupported_extraction"
			return result, errUnsupported
		}
		text := texts[req.Unit]
		end := min(len(text), req.Offset+req.MaxBytes)
		for end < len(text) && !utf8.RuneStart(text[end]) {
			end--
		}
		result.Text = text[req.Offset:end]
		result.Locator = &Locator{SchemaVersion: 1, AssetID: req.AssetID, Extractor: Extractor, RepresentationID: result.RepresentationID, Unit: req.Unit, Start: req.Offset, End: end}
		result.EndOfUnit, result.EndOfSource = end == len(text), end == len(text) && req.Unit == len(texts)-1
		result.Status = "complete_range"
		if !result.EndOfUnit {
			result.Status = "partial_range"
		}
		if !result.EndOfSource {
			nextUnit, nextOffset := req.Unit, end
			if result.EndOfUnit {
				nextUnit, nextOffset = req.Unit+1, 0
			}
			result.Continuation = encodeCursor(cursor{SchemaVersion: 1, AssetID: req.AssetID, Extractor: Extractor, RepresentationID: result.RepresentationID, Unit: nextUnit, Offset: nextOffset})
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	if len(encoded) > 1<<20 {
		result.Status = "budget_exceeded"
		return result, errBudget
	}
	if err = ctx.Err(); err != nil {
		result.Status = "cancelled"
		return result, err
	}
	return result, nil
}

func resolveSource(ctx context.Context, req Request, result *Result) ([]byte, error) {
	directory, asset, err := findAsset(ctx, req, result)
	if err != nil {
		return nil, err
	}
	data, status, err := readAsset(ctx, directory, result.SourcePath, asset)
	if err != nil {
		result.Status = status
	}
	return data, err
}

// resolveFile is resolveSource for a parser that reads at offsets: use reads
// the opened file, and the whole file is then hashed and its identity
// rechecked, so a change while it was read is refused. Nothing is loaded into
// memory, so an asset may be as large as its format allows.
func resolveFile(ctx context.Context, req Request, result *Result, use func(io.ReaderAt, int64) error) error {
	directory, asset, err := findAsset(ctx, req, result)
	if err != nil {
		return err
	}
	status, err := useAsset(ctx, directory, result.SourcePath, asset, use)
	if err != nil && status != "" {
		result.Status = status
	}
	return err
}

// findAsset finds the requested asset and the directory its path is relative to:
// the explicit root for a snapshot catalog, or the library for a holding.
func findAsset(ctx context.Context, req Request, result *Result) (string, library.Asset, error) {
	result.Status = "unavailable"
	info, err := os.Lstat(req.Catalog)
	if err != nil {
		return "", library.Asset{}, err
	}
	if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		return findManaged(ctx, req, result)
	}
	catalog, err := library.Load(req.Catalog)
	if err != nil {
		return "", library.Asset{}, err
	}
	result.CatalogID = catalog.ID
	for _, asset := range catalog.Assets {
		if asset.ID != req.AssetID {
			continue
		}
		if len(asset.Locations[0].Path) > 4096 {
			result.Status = "budget_exceeded"
			return "", library.Asset{}, errBudget
		}
		// The selected catalog location is deterministic; hints and duplicate
		// locations never authorize fallback filesystem searches.
		result.SourcePath = asset.Locations[0].Path
		return req.Root, asset, nil
	}
	result.Status = "missing_asset"
	return "", library.Asset{}, errors.New("asset not present in catalog")
}

func findManaged(ctx context.Context, req Request, result *Result) (string, library.Asset, error) {
	catalog, err := filepath.Abs(req.Catalog)
	if err != nil {
		return "", library.Asset{}, err
	}
	root, err := filepath.Abs(req.Root)
	if err != nil {
		return "", library.Asset{}, err
	}
	if catalog != root {
		return "", library.Asset{}, errors.New("managed content requires the library directory as both catalog and root")
	}
	asset, libraryID, err := library.ManagedAsset(ctx, catalog, req.AssetID)
	if err != nil {
		if strings.Contains(err.Error(), "not present") {
			result.Status = "missing_asset"
		}
		return "", library.Asset{}, err
	}
	result.CatalogID = libraryID
	result.SourcePath = asset.Locations[0].Path
	return catalog, asset, nil
}

func sourceBudget(asset library.Asset) int64 {
	if len(asset.Locations) == 1 && asset.Locations[0].CandidateKind == "managed_holding" {
		return maxManagedInput
	}
	return maxInput
}

func readAsset(ctx context.Context, directory, name string, asset library.Asset) ([]byte, string, error) {
	if asset.Bytes <= 0 || asset.Bytes > sourceBudget(asset) {
		return nil, "budget_exceeded", errBudget
	}
	root, err := safeio.OpenResolvedRoot(directory)
	if err != nil {
		return nil, "unavailable", err
	}
	defer root.Close()
	before, err := root.Lstat(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "missing_asset", err
		}
		return nil, "unavailable", err
	}
	if !before.Mode().IsRegular() {
		return nil, "unsupported_extraction", errors.New("source must be a regular non-symlink file")
	}
	if before.Size() != asset.Bytes {
		return nil, "changed_source", errors.New("source size differs from asset")
	}
	f, err := safeio.OpenRegular(root, name, before, os.O_RDONLY)
	if err != nil {
		return nil, "unavailable", err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, "unavailable", err
	}
	if !os.SameFile(before, opened) {
		return nil, "changed_source", errors.New("source replaced while opening")
	}
	b, err := io.ReadAll(io.LimitReader(contextReader{ctx, f}, asset.Bytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, "cancelled", ctx.Err()
		}
		return nil, "unavailable", err
	}
	current, err := root.Lstat(name)
	if err != nil {
		return nil, "changed_source", err
	}
	after, err := f.Stat()
	if err != nil {
		return nil, "unavailable", err
	}
	hash := sha256.Sum256(b)
	if int64(len(b)) != asset.Bytes || !os.SameFile(before, current) || !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() || hex.EncodeToString(hash[:]) != asset.SHA256 {
		return nil, "changed_source", errors.New("source bytes or identity changed")
	}
	return b, "", nil
}

// useAsset opens an asset as readAsset does, lets use read it at offsets, and
// then hashes the whole file and rechecks its identity. A file replaced or
// changed while use read it is refused, and use's result is not kept.
func useAsset(ctx context.Context, directory, name string, asset library.Asset, use func(io.ReaderAt, int64) error) (string, error) {
	if asset.Bytes <= 0 || asset.Bytes > assessment.MaxMP4Bytes {
		return "budget_exceeded", errBudget
	}
	root, err := safeio.OpenResolvedRoot(directory)
	if err != nil {
		return "unavailable", err
	}
	defer root.Close()
	before, err := root.Lstat(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "missing_asset", err
		}
		return "unavailable", err
	}
	if !before.Mode().IsRegular() {
		return "unsupported_extraction", errors.New("source must be a regular non-symlink file")
	}
	if before.Size() != asset.Bytes {
		return "changed_source", errors.New("source size differs from asset")
	}
	f, err := safeio.OpenRegular(root, name, before, os.O_RDONLY)
	if err != nil {
		return "unavailable", err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return "unavailable", err
	}
	if !os.SameFile(before, opened) {
		return "changed_source", errors.New("source replaced while opening")
	}
	if err := use(f, asset.Bytes); err != nil {
		// A failed read of a file that changed meanwhile is the change.
		current, statErr := root.Lstat(name)
		after, afterErr := f.Stat()
		if statErr != nil || afterErr != nil || !os.SameFile(before, current) || !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
			return "changed_source", errors.Join(err, errors.New("source changed while it was read"))
		}
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(contextReader{ctx, io.NewSectionReader(f, 0, asset.Bytes+1)}, asset.Bytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return "cancelled", ctx.Err()
		}
		return "unavailable", err
	}
	current, err := root.Lstat(name)
	if err != nil {
		return "changed_source", err
	}
	after, err := f.Stat()
	if err != nil {
		return "unavailable", err
	}
	if n != asset.Bytes || !os.SameFile(before, current) || !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() || hex.EncodeToString(h.Sum(nil)) != asset.SHA256 {
		return "changed_source", errors.New("source bytes or identity changed")
	}
	return "", nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

// Keep untrusted parser/path diagnostics bounded while preserving errors.Is.
type diagnosticError struct{ error }

func (e diagnosticError) Error() string {
	s := e.error.Error()
	if len(s) <= 2048 {
		return s
	}
	end := 2048
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end] + "... (diagnostic truncated)"
}
func (e diagnosticError) Unwrap() error { return e.error }

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
