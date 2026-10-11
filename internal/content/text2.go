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
	data, err := resolveSource(ctx, req, &result)
	if err != nil {
		return result, err
	}
	if bytes.HasPrefix(data, []byte("%PDF-")) {
		result.Status = "unsupported_extraction"
		return result, errors.New("this asset is a PDF; request representation pdf-text/1")
	}
	if !bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		result.Status = "unsupported_extraction"
		return result, errUnsupported
	}
	documents, _, err := loadDocuments(ctx, req, cursor{}, data, true)
	if err != nil {
		result.Status = documentStatus(ctx, err)
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return result, err
	}
	units, texts, gaps := []Unit{}, []string{}, [][]Gap{}
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
			result.UnsupportedUnits = append(result.UnsupportedUnits, u.Index)
		}
		units = append(units, u)
		texts = append(texts, d.text)
		gaps = append(gaps, documentGaps(d))
	}
	encoded, err := json.Marshal(struct {
		Extractor string
		Units     []Unit
		Texts     []string
		Gaps      [][]Gap
	}{TextExtractorV2, units, texts, gaps})
	if err != nil {
		return result, err
	}
	if len(encoded) > 32<<20 {
		result.Status = "budget_exceeded"
		return result, errBudget
	}
	hash := sha256.Sum256(encoded)
	result.RepresentationID = "sha256:" + hex.EncodeToString(hash[:])
	result.Units = units
	if req.Cursor != "" && c.RepresentationID != result.RepresentationID {
		result.Status = "stale_reference"
		return result, errors.New("continuation representation changed")
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
		return result, errors.New("unit or UTF-8 byte offset is outside the extracted source")
	}
	if units[req.Unit].Extraction == "unsupported_document" {
		// A continuation stops here. Skipping to a later unit needs an explicit request.
		result.Status = "unsupported_extraction"
		return result, errUnsupported
	}
	text := texts[req.Unit]
	end := min(len(text), req.Offset+req.MaxBytes)
	for end < len(text) && !utf8.RuneStart(text[end]) {
		end--
	}
	result.Text = text[req.Offset:end]
	result.Locator = &Locator{SchemaVersion: 1, AssetID: req.AssetID, Extractor: TextExtractorV2, RepresentationID: result.RepresentationID, Unit: req.Unit, Start: req.Offset, End: end, Member: units[req.Unit].Member, OffsetUnit: "utf8_bytes"}
	result.EndOfUnit, result.EndOfSource = end == len(text), end == len(text) && req.Unit == len(texts)-1
	result.Status = "complete_range"
	result.Coverage = &Coverage{Text: "complete_selected_range", KnownGaps: []string{}, ReferencedOutsideRange: []string{}, Unknown: []string{"CSS visibility, rendered layout, accessibility quality, and semantic completeness are not established."}}
	if !result.EndOfUnit {
		result.Status = "partial_range"
		result.Coverage.Text = "bounded_selected_range"
	}
	// A zero-width gap belongs to the range that starts at its offset, or to the
	// final range of the unit, so consecutive ranges disclose each gap exactly once.
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
		result.Continuation = encodeCursor(cursor{SchemaVersion: 1, AssetID: req.AssetID, Extractor: TextExtractorV2, RepresentationID: result.RepresentationID, Unit: nextUnit, Offset: nextOffset, Representation: "epub-text/2"})
	}
	return finishStructured(ctx, result)
}
