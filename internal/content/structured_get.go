package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/blisspixel/nemalo/internal/assessment"
	"golang.org/x/net/html"
)

var errResourceReference = errors.New("resource reference does not match parent/member identity")

func localReference(member, reference string) (string, string, string) {
	u, err := url.Parse(reference)
	if err != nil {
		return "", "", "invalid_reference"
	}
	if u.Scheme != "" || u.Host != "" || u.User != nil || strings.HasPrefix(reference, "//") {
		return "", "", "external_not_fetched"
	}
	if u.RawQuery != "" || strings.HasPrefix(u.Path, "/") || strings.ContainsAny(u.Path, "\\:\x00") {
		return "", "", "invalid_reference"
	}
	name := member
	if u.Path != "" {
		name = path.Join(path.Dir(member), u.Path)
	}
	if !fs.ValidPath(name) {
		return "", "", "invalid_reference"
	}
	return name, u.Fragment, "local"
}

func resolveImages(d *document, members map[string]assessment.EPUBMember) {
	for i := range d.parts {
		image := d.parts[i].Image
		if image == nil {
			continue
		}
		name, fragment, status := localReference(d.unit.Member, image.Source)
		image.Status = status
		if image.Source == "" || fragment != "" {
			image.Status = "unsupported_resource_reference"
			continue
		}
		if status != "local" {
			continue
		}
		m, ok := members[name]
		if !ok {
			image.Status = "missing_member"
			continue
		}
		if !strings.HasPrefix(m.Media, "image/") {
			image.Status = "unsupported_resource_type"
			continue
		}
		if m.Bytes > 8<<20 {
			image.Status = "resource_budget_exceeded"
			continue
		}
		// Bound the member identity before hashing the representation. Issued
		// references must fit the same request budget on every round trip.
		if !referenceFits(d.parts[i].ID, name, m.SHA256) {
			image.Status = "reference_budget_exceeded"
			continue
		}
		image.Status, image.Member, image.SHA256, image.Bytes = "local_resource", name, m.SHA256, int(m.Bytes)
	}
}

func getStructured(ctx context.Context, req Request, result Result) (Result, error) {
	result.Extractor, result.Representation = structuredExtractor, req.Representation
	result.Limitations[0] = "Source-bound extraction or original member bytes, not rendered layout, a summary, or proof of reading."
	var c cursor
	var err error
	if req.Cursor != "" {
		c, err = parseCursor(req.Cursor)
		if err != nil {
			result.Status = "invalid_request"
			return result, err
		}
		if c.AssetID != req.AssetID || c.Extractor != structuredExtractor || c.Representation != req.Representation {
			result.Status = "stale_reference"
			return result, errors.New("reference belongs to another asset or representation")
		}
		req.Unit, req.Offset, req.Part = c.Unit, c.Offset, c.Part
	}
	if req.Resource && (req.Cursor == "" || c.Member == "") {
		result.Status = "invalid_request"
		return result, errors.New("resource read requires an issued local-resource cursor")
	}
	if !req.Resource && c.Member != "" {
		result.Status = "invalid_request"
		return result, errors.New("resource references require content resource")
	}
	data, err := resolveSource(ctx, req, &result)
	if err != nil {
		return result, err
	}
	if !bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		result.Status = "unsupported_extraction"
		return result, errUnsupported
	}
	documents := []document{}
	totalText, totalParts := 0, 0
	packagePath := ""
	visit := func(source assessment.EPUBDocument) error {
		if len(documents) >= maxUnits || len(source.Path) > 4096 || len(source.Package) > 4096 || len(source.Data) > maxText {
			return errBudget
		}
		if source.Index != len(documents) || (packagePath != "" && source.Package != packagePath) {
			return errUnsupported
		}
		packagePath = source.Package
		if source.Media != "application/xhtml+xml" {
			return errUnsupported
		}
		if source.Linear != "" && source.Linear != "yes" && source.Linear != "no" {
			return errUnsupported
		}
		d, err := extractDocument(ctx, source.Data, maxText-totalText)
		if err != nil {
			return err
		}
		totalText += len(d.text)
		totalParts += len(d.parts)
		if totalParts > 16384 {
			return fmt.Errorf("%w: more than 16384 parts in this EPUB", errBudget)
		}
		linear := source.Linear
		if linear == "" {
			linear = "yes"
		}
		d.unit.Index, d.unit.Kind, d.unit.Package, d.unit.Member, d.unit.Linear, d.unit.TextBytes, d.unit.Extraction = source.Index, "epub_reading_order_document", source.Package, source.Path, linear, len(d.text), "structured_text_with_coverage"
		d.unit.Parts = len(d.parts)
		for _, p := range d.parts {
			if p.Gap != "" {
				d.unit.KnownGaps++
			}
		}
		if req.Representation == "epub-source/1" && source.Index == req.Unit {
			d.raw = source.Data
		}
		resolveImages(&d, source.Members)
		if req.Resource && source.Index == req.Unit && !matchesResource(d, c) {
			return errResourceReference
		}
		documents = append(documents, d)
		return nil
	}
	var checks assessment.Checks
	var resource []byte
	if req.Resource {
		checks, resource, err = assessment.VisitEPUBResource(ctx, bytes.NewReader(data), int64(len(data)), visit, c.Member, 8<<20)
	} else {
		checks, err = assessment.VisitEPUB(ctx, bytes.NewReader(data), int64(len(data)), visit)
	}
	if err != nil || len(documents) == 0 || len(checks.AccessRestrictions) > 0 {
		result.Status = "malformed_content"
		if errors.Is(err, errUnsupported) || len(checks.AccessRestrictions) > 0 {
			result.Status = "unsupported_extraction"
		}
		if errors.Is(err, errBudget) || errors.Is(err, assessment.ErrLimit) || errors.Is(err, html.ErrBufferExceeded) {
			result.Status = "budget_exceeded"
		}
		if errors.Is(err, errResourceReference) {
			result.Status = "stale_reference"
		}
		if ctx.Err() != nil {
			result.Status, err = "cancelled", ctx.Err()
		}
		if err == nil {
			err = errors.New("container access restrictions require review")
		}
		return result, err
	}
	if err = resolveLinks(ctx, documents); err != nil {
		result.Status = "cancelled"
		return result, err
	}
	units, texts, parts := []Unit{}, []string{}, [][]Part{}
	for _, d := range documents {
		units = append(units, d.unit)
		texts = append(texts, d.text)
		parts = append(parts, d.parts)
	}
	encoded, err := json.Marshal(struct {
		Extractor string
		Units     []Unit
		Texts     []string
		Parts     [][]Part
	}{structuredExtractor, units, texts, parts})
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
		return result, errors.New("source representation changed")
	}
	bindReferences(documents, req.AssetID, result.RepresentationID)
	if req.UnitsOnly {
		result.Status = "units_available"
		return finishStructured(ctx, result)
	}
	if req.Unit >= len(documents) {
		result.Status = "invalid_request"
		return result, errors.New("reading-order unit is outside source")
	}
	d := documents[req.Unit]
	if req.Resource {
		return readStructuredResource(ctx, req, result, c, d, resource)
	}
	return readStructuredRange(ctx, req, result, d)
}

func readStructuredResource(ctx context.Context, req Request, result Result, c cursor, d document, resource []byte) (Result, error) {
	h := sha256.Sum256(resource)
	if !matchesResource(d, c) || hex.EncodeToString(h[:]) != c.MemberSHA256 {
		result.Status = "stale_reference"
		return result, errResourceReference
	}
	if req.Offset > len(resource) {
		result.Status = "invalid_request"
		return result, errors.New("resource offset outside source")
	}
	end := min(len(resource), req.Offset+req.MaxBytes)
	result.Resource = &ResourceData{Member: c.Member, SHA256: c.MemberSHA256, TotalBytes: len(resource), Encoding: "base64", Data: resource[req.Offset:end]}
	result.Locator = &Locator{SchemaVersion: 1, AssetID: req.AssetID, Extractor: structuredExtractor, RepresentationID: result.RepresentationID, Unit: req.Unit, Start: req.Offset, End: end, Member: c.Member, OffsetUnit: "resource_bytes"}
	result.Status = "complete_range"
	if end < len(resource) {
		result.Status = "partial_range"
		c.Offset = end
		result.Continuation = encodeCursor(c)
	}
	result.Coverage = &Coverage{Text: "not_applicable", KnownGaps: []string{}, ReferencedOutsideRange: []string{}, Unknown: []string{"Resource bytes were not rendered, decoded, or security-scanned."}}
	return finishStructured(ctx, result)
}

func matchesResource(d document, c cursor) bool {
	for _, p := range d.parts {
		if p.ID == c.Part && p.Image != nil && p.Image.Member == c.Member && p.Image.SHA256 == c.MemberSHA256 && p.Image.Status == "local_resource" {
			return true
		}
	}
	return false
}

func readStructuredRange(ctx context.Context, req Request, result Result, d document) (Result, error) {
	text := d.text
	offsetUnit := "utf8_bytes"
	if req.Representation == "epub-source/1" {
		text = string(d.raw)
		offsetUnit = "member_utf8_bytes"
	}
	begin, limit := 0, len(text)
	selectedSourceStart, selectedSourceEnd := 0, 0
	if req.Part != "" {
		found := false
		for _, p := range d.parts {
			if p.ID == req.Part {
				found = true
				selectedSourceStart, selectedSourceEnd = p.SourceStart, p.SourceEnd
				begin, limit = p.Start, p.End
				if req.Representation == "epub-source/1" {
					begin, limit = p.SourceStart, p.SourceEnd
				}
			}
		}
		if !found {
			result.Status = "missing_part"
			return result, errors.New("part not present in selected source unit")
		}
		if req.Cursor == "" && req.Offset == 0 {
			req.Offset = begin
		}
	}
	if req.Offset < begin || req.Offset > limit || (req.Offset < len(text) && !utf8.RuneStart(text[req.Offset])) {
		result.Status = "invalid_request"
		return result, errors.New("offset is outside selected part or splits UTF-8")
	}
	end := min(limit, req.Offset+req.MaxBytes)
	for end < len(text) && !utf8.RuneStart(text[end]) {
		end--
	}
	result.Text = text[req.Offset:end]
	result.Locator = &Locator{SchemaVersion: 1, AssetID: req.AssetID, Extractor: structuredExtractor, RepresentationID: result.RepresentationID, Unit: req.Unit, Start: req.Offset, End: end, Member: d.unit.Member, OffsetUnit: offsetUnit}
	result.Status = "complete_range"
	if end < limit {
		result.Status = "partial_range"
	}
	result.EndOfUnit = end == len(text)
	result.EndOfSource = result.EndOfUnit && req.Unit == len(result.Units)-1
	if end < limit || (!result.EndOfSource && req.Part == "") {
		next := cursor{SchemaVersion: 1, AssetID: req.AssetID, Extractor: structuredExtractor, RepresentationID: result.RepresentationID, Unit: req.Unit, Offset: end, Representation: req.Representation, Part: req.Part}
		if end == limit {
			next.Unit++
			next.Offset = 0
		}
		result.Continuation = encodeCursor(next)
	}
	result.Coverage = &Coverage{Text: "complete_selected_range", KnownGaps: []string{}, ReferencedOutsideRange: []string{}, Unknown: []string{"CSS visibility, rendered layout, accessibility quality, and semantic completeness are not established."}}
	if end < limit {
		result.Coverage.Text = "bounded_selected_range"
	}
	for _, p := range d.parts {
		if req.Part != "" && (p.SourceStart >= selectedSourceEnd || p.SourceEnd <= selectedSourceStart) {
			continue
		}
		start, stop := p.Start, p.End
		if req.Representation == "epub-source/1" {
			start, stop = p.SourceStart, p.SourceEnd
		}
		if (start < end && stop > req.Offset) || (start == stop && start >= req.Offset && (start < end || (start == end && end == limit))) {
			result.Parts = append(result.Parts, p)
			if p.Gap != "" && req.Representation != "epub-source/1" {
				result.Coverage.KnownGaps = append(result.Coverage.KnownGaps, p.ID)
				result.Coverage.Text = "known_extraction_gaps"
			}
			if p.Link != nil {
				target, parseErr := parseCursor(p.Link.Reference)
				targetStart, targetEnd := 0, len(text)
				if target.Part != "" {
					for _, candidate := range d.parts {
						if candidate.ID == target.Part {
							targetStart, targetEnd = candidate.Start, candidate.End
							if req.Representation == "epub-source/1" {
								targetStart, targetEnd = candidate.SourceStart, candidate.SourceEnd
							}
						}
					}
				}
				if parseErr != nil || target.Unit != req.Unit || targetStart < req.Offset || targetEnd > end {
					result.Coverage.ReferencedOutsideRange = append(result.Coverage.ReferencedOutsideRange, p.ID)
				}
			}
		}
	}
	return finishStructured(ctx, result)
}

func finishStructured(ctx context.Context, result Result) (Result, error) {
	if err := ctx.Err(); err != nil {
		result.Status = "cancelled"
		return result, err
	}
	b, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	if len(b) > 1<<20 {
		result.Status = "budget_exceeded"
		return result, errBudget
	}
	return result, nil
}
