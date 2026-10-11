package content

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/blisspixel/nemalo/internal/pdftext"
)

// getPDFText serves pdf-text/1: one unit per physical page (zero-based index)
// with its printed label, text in content-stream order, and located gaps for
// images, unmapped font codes, pages without a text layer, and unread forms.
func getPDFText(ctx context.Context, req Request, result Result) (Result, error) {
	result.Extractor, result.Representation = pdftext.Extractor, "pdf-text/1"
	result.Limitations = []string{
		"Text is in content-stream order. Reading order across columns, layout, and hyphenation are not reconstructed.",
		"Unit index is the zero-based physical page index; page_label is the printed label the document declares, if any.",
		"No OCR: a scanned page reports no_text_layer. Font codes without a Unicode mapping are located gaps, never guessed.",
		"Rights and edition metadata are not established by a byte-identity catalog; unknown.",
		"No antivirus scan, safety guarantee, reading history, or consumption acknowledgement.",
	}
	var c cursor
	var err error
	if req.Cursor != "" {
		c, err = parseCursor(req.Cursor)
		if err != nil {
			return result, err
		}
		if c.AssetID != req.AssetID || c.Extractor != pdftext.Extractor || c.Representation != "pdf-text/1" || c.Part != "" || c.Member != "" || c.MemberSHA256 != "" {
			result.Status = "stale_reference"
			return result, errors.New("continuation belongs to another asset, extractor, or representation")
		}
		req.Unit, req.Offset = c.Unit, c.Offset
	}
	data, err := resolveSource(ctx, req, &result)
	if err != nil {
		return result, err
	}
	doc, err := pdftext.Extract(ctx, data, pdftext.Defaults())
	if err != nil {
		switch {
		case ctx.Err() != nil:
			result.Status, err = "cancelled", ctx.Err()
		case errors.Is(err, pdftext.ErrNotPDF), errors.Is(err, pdftext.ErrEncrypted):
			result.Status = "unsupported_extraction"
		case errors.Is(err, pdftext.ErrBudget):
			result.Status = "budget_exceeded"
		default:
			result.Status = "malformed_content"
		}
		return result, err
	}
	units, texts, gaps := make([]Unit, len(doc.Pages)), make([]string, len(doc.Pages)), make([][]Gap, len(doc.Pages))
	for i, p := range doc.Pages {
		u := Unit{Index: p.Index, Kind: "pdf_page", Linear: "yes", TextBytes: len(p.Text), Extraction: "supported_text", PageLabel: p.Label, KnownGaps: len(p.Gaps)}
		for j, g := range p.Gaps {
			gaps[i] = append(gaps[i], Gap{Part: fmt.Sprintf("page-%d-gap-%d", p.Index, j), Element: g.Kind, Reason: g.Detail, Offset: g.Offset, Count: g.Count})
			if g.Kind == "no_text_layer" {
				u.Extraction = "no_text_layer"
			}
		}
		if u.Extraction == "supported_text" && len(p.Gaps) > 0 {
			u.Extraction = "supported_text_with_gaps"
		}
		units[i], texts[i] = u, p.Text
	}
	if len(units) == 0 {
		result.Status = "malformed_content"
		return result, errors.New("the PDF has no pages")
	}
	encoded, err := json.Marshal(struct {
		Extractor string
		Units     []Unit
		Texts     []string
		Gaps      [][]Gap
	}{pdftext.Extractor, units, texts, gaps})
	if err != nil {
		return result, err
	}
	hash := sha256.Sum256(encoded)
	result.RepresentationID = "sha256:" + hex.EncodeToString(hash[:])
	result.Units = units
	if req.Cursor != "" && c.RepresentationID != result.RepresentationID {
		result.Status = "stale_reference"
		return result, errors.New("continuation representation changed")
	}
	if len(doc.Findings) > 0 {
		result.Limitations = append(result.Limitations, "Document findings: "+strings.Join(doc.Findings, "; "))
	}
	if req.UnitsOnly {
		result.Status = "units_available"
		return finishStructured(ctx, result)
	}
	if req.Find != "" {
		return locate(ctx, req, result, units, texts)
	}
	if req.Unit >= len(texts) || req.Offset > len(texts[req.Unit]) || (req.Offset < len(texts[req.Unit]) && !utf8.RuneStart(texts[req.Unit][req.Offset])) {
		result.Status = "invalid_request"
		return result, errors.New("page or UTF-8 byte offset is outside the extracted source")
	}
	text := texts[req.Unit]
	end := min(len(text), req.Offset+req.MaxBytes)
	for end < len(text) && !utf8.RuneStart(text[end]) {
		end--
	}
	result.Text = text[req.Offset:end]
	result.Locator = &Locator{SchemaVersion: 1, AssetID: req.AssetID, Extractor: pdftext.Extractor, RepresentationID: result.RepresentationID, Unit: req.Unit, Start: req.Offset, End: end, OffsetUnit: "utf8_bytes", PageLabel: units[req.Unit].PageLabel}
	result.EndOfUnit, result.EndOfSource = end == len(text), end == len(text) && req.Unit == len(texts)-1
	result.Status = "complete_range"
	result.Coverage = &Coverage{Text: "complete_selected_range", KnownGaps: []string{}, ReferencedOutsideRange: []string{}, Unknown: []string{"Column reading order, layout, and text inside images are not established."}}
	if !result.EndOfUnit {
		result.Status = "partial_range"
		result.Coverage.Text = "bounded_selected_range"
	}
	// As for EPUB, a zero-width gap belongs to the range that starts at its
	// offset, or to the last range of the page, so each is disclosed once.
	result.Gaps = []Gap{}
	for _, g := range gaps[req.Unit] {
		if (g.Offset >= req.Offset && g.Offset < end) || (g.Offset == end && result.EndOfUnit) {
			result.Gaps = append(result.Gaps, g)
			result.Coverage.KnownGaps = append(result.Coverage.KnownGaps, g.Part)
			result.Coverage.Text = "known_extraction_gaps"
		}
	}
	if !result.EndOfSource {
		nextUnit, nextOffset := req.Unit, end
		if result.EndOfUnit {
			nextUnit, nextOffset = req.Unit+1, 0
		}
		result.Continuation = encodeCursor(cursor{SchemaVersion: 1, AssetID: req.AssetID, Extractor: pdftext.Extractor, RepresentationID: result.RepresentationID, Unit: nextUnit, Offset: nextOffset, Representation: "pdf-text/1"})
	}
	return finishStructured(ctx, result)
}

// Match is one exact occurrence of a requested text. Start and End are UTF-8
// byte offsets in the unit text, the same offsets a read locator uses, and
// Cursor reads from Start.
type Match struct {
	Unit      int    `json:"unit"`
	Start     int    `json:"start"`
	End       int    `json:"end"`
	PageLabel string `json:"page_label,omitempty"`
	Member    string `json:"member,omitempty"`
	Cursor    string `json:"cursor"`
}

const maxMatches = 100

// locate finds exact, case-sensitive occurrences of req.Find in the extracted
// text, bound to the same representation ID as reads, so a quotation can be
// cited by unit and offsets and read back exactly. Nothing is normalized.
func locate(ctx context.Context, req Request, result Result, units []Unit, texts []string) (Result, error) {
	result.Matches = []Match{}
	result.Status = "no_matches"
	for i, text := range texts {
		for at := 0; at <= len(text); {
			if err := ctx.Err(); err != nil {
				result.Status = "cancelled"
				return result, err
			}
			j := strings.Index(text[at:], req.Find)
			if j < 0 {
				break
			}
			start := at + j
			if len(result.Matches) == maxMatches {
				result.Status, result.Units = "matches_truncated", nil
				return finishStructured(ctx, result)
			}
			result.Matches = append(result.Matches, Match{Unit: i, Start: start, End: start + len(req.Find), PageLabel: units[i].PageLabel, Member: units[i].Member,
				Cursor: encodeCursor(cursor{SchemaVersion: 1, AssetID: req.AssetID, Extractor: result.Extractor, RepresentationID: result.RepresentationID, Unit: i, Offset: start, Representation: result.Representation})})
			result.Status = "matches_found"
			at = start + max(1, len(req.Find))
		}
	}
	result.Units = nil
	return finishStructured(ctx, result)
}
