package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"unicode/utf8"
)

// TextExtractorV2 produces exactly the structured extractor's text with located
// gap descriptors instead of parts. Documents the extractor cannot represent
// become explicit unsupported units rather than failing the whole source.
const TextExtractorV2 = "nemalo.epub.text/2"

// DefaultRepresentation is used when a request names no representation and
// carries no continuation. A continuation always selects its own representation.
const DefaultRepresentation = "epub-text/2"

// Gap locates content omitted from the plain text. Offset is the UTF-8 byte
// position in the unit text where the omitted element occurred; the omission is
// zero-width in the text. Part is the epub-structure/1 part ID of the element,
// usable with --representation epub-structure/1 or epub-source/1 to inspect it.
type Gap struct {
	Part    string    `json:"part"`
	Element string    `json:"element"`
	Reason  string    `json:"reason"`
	Offset  int       `json:"offset"`
	Image   *GapImage `json:"image,omitempty"`
	// Count is how many omissions of this kind one PDF gap stands for.
	Count int `json:"count,omitempty"`
}

// GapImage keeps source alt text exactly. A null Alt means the source declared
// none, which is distinct from an empty declared alternative.
type GapImage struct {
	Source string  `json:"source"`
	Alt    *string `json:"alt"`
}

func documentGaps(d document) []Gap {
	gaps := []Gap{}
	for i, p := range d.parts {
		if p.Gap == "" {
			continue
		}
		g := Gap{Part: p.ID, Element: d.elements[i], Reason: p.Gap, Offset: p.Start}
		if p.Image != nil {
			g.Image = &GapImage{Source: p.Image.Source, Alt: p.Image.Alt}
		}
		gaps = append(gaps, g)
	}
	return gaps
}

func getTextV2(ctx context.Context, req Request, result Result) (Result, error) {
	result.Extractor, result.Representation = TextExtractorV2, "epub-text/2"
	result.Limitations[2] = "Images, tables, mathematics, embedded media, and hidden content are omitted and located in gaps; CSS and semantic completeness are not reconstructed."
	var c cursor
	var err error
	if req.Cursor != "" {
		c, err = parseCursor(req.Cursor)
		if err != nil {
			return result, err
		}
		if c.AssetID != req.AssetID || c.Extractor != TextExtractorV2 || c.Representation != "epub-text/2" || c.Part != "" || c.Member != "" || c.MemberSHA256 != "" {
			result.Status = "stale_reference"
			return result, errors.New("continuation belongs to another asset, extractor, or representation")
		}
		req.Unit, req.Offset = c.Unit, c.Offset
	}
	documents, _, err := loadEPUBText(ctx, req, &result)
	if err != nil {
		return result, err
	}
	view, err := viewTextV2(documents)
	if err != nil {
		result.Status = "budget_exceeded"
		return result, err
	}
	result.UnsupportedUnits = view.unsupported
	result.RepresentationID = view.id
	result.Units = view.units
	if req.Cursor != "" && c.RepresentationID != result.RepresentationID {
		result.Status = "stale_reference"
		return result, errors.New("continuation representation changed")
	}
	if req.UnitsOnly {
		result.Status = "units_available"
		return finishStructured(ctx, result)
	}
	if req.Find != "" {
		return locate(ctx, req, result, view.units, view.texts)
	}
	return readTextV2Range(ctx, req, result, view, req.Unit, req.Offset)
}

// loadEPUBText resolves the source and loads every reading-order document
// tolerantly, as epub-text/2 and the representations over its text do. It
// returns the verified source bytes too.
func loadEPUBText(ctx context.Context, req Request, result *Result) ([]document, []byte, error) {
	data, err := resolveSource(ctx, req, result)
	if err != nil {
		return nil, nil, err
	}
	if bytes.HasPrefix(data, []byte("%PDF-")) {
		result.Status = "unsupported_extraction"
		return nil, nil, errors.New("this asset is a PDF; request representation pdf-text/1")
	}
	if !bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		result.Status = "unsupported_extraction"
		return nil, nil, errUnsupported
	}
	documents, _, err := loadDocuments(ctx, req, cursor{}, data, true)
	if err != nil {
		result.Status = documentStatus(ctx, err)
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return nil, nil, err
	}
	return documents, data, nil
}

// textV2View is the epub-text/2 view of loaded documents. Its id is hashed
// exactly as epub-text/2 representation identities always have been.
type textV2View struct {
	units       []Unit
	texts       []string
	gaps        [][]Gap
	unsupported []int
	documents   []document
	id          string
}

func viewTextV2(documents []document) (textV2View, error) {
	v := textV2View{units: []Unit{}, texts: []string{}, gaps: [][]Gap{}, documents: documents}
	for _, d := range documents {
		u := d.unit
		// Parts belong to the structured representation; text/2 reports gaps.
		u.Parts = 0
		if u.Extraction != "unsupported_document" {
			u.Extraction = "supported_plain_text"
			if u.KnownGaps > 0 {
				u.Extraction = "supported_text_with_gaps"
			}
		} else {
			v.unsupported = append(v.unsupported, u.Index)
		}
		v.units = append(v.units, u)
		v.texts = append(v.texts, d.text)
		v.gaps = append(v.gaps, documentGaps(d))
	}
	encoded, err := json.Marshal(struct {
		Extractor string
		Units     []Unit
		Texts     []string
		Gaps      [][]Gap
	}{TextExtractorV2, v.units, v.texts, v.gaps})
	if err != nil {
		return v, err
	}
	if len(encoded) > 32<<20 {
		return v, errBudget
	}
	hash := sha256.Sum256(encoded)
	v.id = "sha256:" + hex.EncodeToString(hash[:])
	return v, nil
}

// readTextV2Range reads epub-text/2 text from unit and offset, with its gaps,
// breaks, and an epub-text/2 continuation. The locator is always in
// epub-text/2 coordinates and identity.
func readTextV2Range(ctx context.Context, req Request, result Result, v textV2View, unit, offset int) (Result, error) {
	texts, units, gaps := v.texts, v.units, v.gaps
	if unit >= len(texts) || offset > len(texts[unit]) || (offset < len(texts[unit]) && !utf8.RuneStart(texts[unit][offset])) {
		result.Status = "invalid_request"
		return result, errors.New("unit or UTF-8 byte offset is outside the extracted source")
	}
	if units[unit].Extraction == "unsupported_document" {
		// A continuation stops here. Skipping to a later unit needs an explicit request.
		result.Status = "unsupported_extraction"
		return result, errUnsupported
	}
	text := texts[unit]
	end := min(len(text), offset+req.MaxBytes)
	for end < len(text) && !utf8.RuneStart(text[end]) {
		end--
	}
	result.Text = text[offset:end]
	result.Locator = &Locator{SchemaVersion: 1, AssetID: req.AssetID, Extractor: TextExtractorV2, RepresentationID: v.id, Unit: unit, Start: offset, End: end, Member: units[unit].Member, OffsetUnit: "utf8_bytes"}
	result.EndOfUnit, result.EndOfSource = end == len(text), end == len(text) && unit == len(texts)-1
	result.Status = "complete_range"
	result.Breaks = rangeBreaks(text, v.documents[unit].breaks, offset, end)
	result.Coverage = &Coverage{Text: "complete_selected_range", KnownGaps: []string{}, ReferencedOutsideRange: []string{}, Unknown: []string{"CSS visibility, rendered layout, accessibility quality, and semantic completeness are not established."}}
	if !result.EndOfUnit {
		result.Status = "partial_range"
		result.Coverage.Text = "bounded_selected_range"
	}
	// A zero-width gap belongs to the range that starts at its offset, or to the
	// final range of the unit, so consecutive ranges disclose each gap exactly once.
	result.Gaps = []Gap{}
	for _, g := range gaps[unit] {
		if (g.Offset >= offset && g.Offset < end) || (g.Offset == end && result.EndOfUnit) {
			result.Gaps = append(result.Gaps, g)
			result.Coverage.KnownGaps = append(result.Coverage.KnownGaps, g.Part)
			result.Coverage.Text = "known_extraction_gaps"
		}
	}
	if !result.EndOfSource {
		nextUnit, nextOffset := unit, end
		if result.EndOfUnit {
			nextUnit, nextOffset = unit+1, 0
		}
		result.Continuation = encodeCursor(cursor{SchemaVersion: 1, AssetID: req.AssetID, Extractor: TextExtractorV2, RepresentationID: v.id, Unit: nextUnit, Offset: nextOffset, Representation: "epub-text/2"})
	}
	return finishStructured(ctx, result)
}
