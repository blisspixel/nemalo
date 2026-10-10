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
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/safeio"
	"golang.org/x/net/html"
)

const Extractor = "nemalo.epub.text/1"
const maxInput = 32 << 20
const maxManagedInput = 256 << 20
const maxText = 8 << 20
const maxUnits = 512

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
}

func (r Request) Validate() error {
	if r.SchemaVersion != 1 || r.Catalog == "" || r.Root == "" || len(r.Catalog) > 4096 || len(r.Root) > 4096 || len(r.AssetID) != 71 || !strings.HasPrefix(r.AssetID, "sha256:") {
		return errors.New("content requires a catalog, explicit root, and sha256: asset ID")
	}
	b, err := hex.DecodeString(r.AssetID[7:])
	if err != nil || len(b) != 32 || hex.EncodeToString(b) != r.AssetID[7:] {
		return errors.New("invalid asset ID")
	}
	if r.Unit < 0 || r.Unit >= maxUnits || r.Offset < 0 || r.Offset > maxText || r.MaxBytes < 4 || r.MaxBytes > 65536 || len(r.Cursor) > 2048 {
		return errors.New("content requires unit 0-511, offset 0-8388608, max-bytes 4-65536, and cursor at most 2048 bytes")
	}
	if r.Cursor != "" && (r.Unit != 0 || r.Offset != 0 || r.UnitsOnly) {
		return errors.New("cursor cannot be combined with unit, offset, or units listing")
	}
	if len(r.Representation) > 80 || len(r.Part) > 256 || (r.Cursor != "" && r.Part != "") || (r.UnitsOnly && (r.Part != "" || r.Resource)) {
		return errors.New("invalid representation, part, or cursor combination")
	}
	if (r.Resource && r.Representation != "epub-structure/1") || (r.UnitsOnly && r.Representation == "epub-source/1") {
		return errors.New("operation is unavailable for the selected representation; inspect content capabilities")
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
	if err != nil || !bytes.Equal(b, canonical) || base64.RawURLEncoding.EncodeToString(b) != token || c.SchemaVersion != 1 || c.Unit < 0 || c.Unit >= maxUnits || c.Offset < 0 || c.Offset > maxText {
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
			result.Parts, result.Coverage, result.Resource = nil, nil, nil
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
	if req.Representation != "" && req.Representation != "epub-text/1" {
		if req.Representation != "epub-structure/1" && req.Representation != "epub-source/1" {
			result.Status = "unsupported_representation"
			return result, errors.New("unsupported content representation; inspect content capabilities")
		}
		return getStructured(ctx, req, result)
	}
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
	result.Status = "unavailable"
	info, err := os.Lstat(req.Catalog)
	if err != nil {
		return nil, err
	}
	if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		return resolveManaged(ctx, req, result)
	}
	catalog, err := library.Load(req.Catalog)
	if err != nil {
		return nil, err
	}
	result.CatalogID = catalog.ID
	for _, asset := range catalog.Assets {
		if asset.ID != req.AssetID {
			continue
		}
		if len(asset.Locations[0].Path) > 4096 {
			result.Status = "budget_exceeded"
			return nil, errBudget
		}
		// The selected catalog location is deterministic; hints and duplicate
		// locations never authorize fallback filesystem searches.
		result.SourcePath = asset.Locations[0].Path
		data, status, err := readAsset(ctx, req.Root, result.SourcePath, asset)
		if err != nil {
			result.Status = status
		}
		return data, err
	}
	result.Status = "missing_asset"
	return nil, errors.New("asset not present in catalog")
}

func resolveManaged(ctx context.Context, req Request, result *Result) ([]byte, error) {
	catalog, err := filepath.Abs(req.Catalog)
	if err != nil {
		return nil, err
	}
	root, err := filepath.Abs(req.Root)
	if err != nil {
		return nil, err
	}
	if catalog != root {
		return nil, errors.New("managed content requires the library directory as both catalog and root")
	}
	asset, libraryID, err := library.ManagedAsset(ctx, catalog, req.AssetID)
	if err != nil {
		if strings.Contains(err.Error(), "not present") {
			result.Status = "missing_asset"
		}
		return nil, err
	}
	result.CatalogID = libraryID
	result.SourcePath = asset.Locations[0].Path
	data, status, err := readAsset(ctx, catalog, result.SourcePath, asset)
	if err != nil {
		result.Status = status
	}
	return data, err
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
