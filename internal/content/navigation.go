package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/blisspixel/nemalo/internal/assessment"
)

// NavExtractor is the epub-nav/1 extractor. Offsets are the epub-text/2 UTF-8
// byte offsets of the same documents; the representation id also covers the
// declared navigation, so a changed table of contents does not keep an old cursor.
const NavExtractor = "nemalo.epub.nav/1"

const (
	maxNavEntries = 1024
	maxNavLabel   = 256
	maxNavDepth   = 64
	maxNavHref    = 2048
)

// NavDocument is one declared navigation source. Evidence is nav-doc or ncx.
type NavDocument struct {
	Evidence string     `json:"evidence"`
	Path     string     `json:"path,omitempty"`
	Problem  string     `json:"problem,omitempty"`
	Entries  []NavEntry `json:"entries"`
}

// NavEntry is one declared contents entry, in source order. Label is the
// declared text with whitespace collapsed; it is never generated. Level is the
// 1-based list or navPoint depth. Parent is the order of the enclosing entry.
type NavEntry struct {
	Label     string    `json:"label"`
	Href      string    `json:"href,omitempty"`
	Order     int       `json:"order"`
	Parent    *int      `json:"parent,omitempty"`
	Level     int       `json:"level"`
	Evidence  string    `json:"evidence"`
	Status    string    `json:"status"`
	Unit      *int      `json:"unit,omitempty"`
	Offset    *int      `json:"offset,omitempty"`
	Part      string    `json:"part,omitempty"`
	Member    string    `json:"member,omitempty"`
	PlayOrder *int      `json:"play_order,omitempty"`
	AltLabels []string  `json:"alt_labels,omitempty"`
	Range     *NavRange `json:"range,omitempty"`
}

// NavRange is the half-open source span of one entry: from its target to the
// next entry that is not nested inside it, or to the end of the source.
// Status is bounded, end_of_source, unresolved, or not_after_start.
type NavRange struct {
	StartUnit   int    `json:"start_unit"`
	StartOffset int    `json:"start_offset"`
	EndUnit     int    `json:"end_unit"`
	EndOffset   int    `json:"end_offset"`
	EndAtSource bool   `json:"end_at_source,omitempty"`
	Status      string `json:"status"`
}

func getNavigation(ctx context.Context, req Request, result Result) (Result, error) {
	result.Extractor, result.Representation = NavExtractor, "epub-nav/1"
	result.Limitations[1] = "Declared navigation entries are not spine documents. A truncated result or a spine boundary is not the end of a declared range."
	var saved cursor
	if req.Cursor != "" {
		c, err := parseCursor(req.Cursor)
		if err != nil {
			result.Status = "invalid_request"
			return result, err
		}
		if c.AssetID != req.AssetID || c.Extractor != NavExtractor || c.Representation != "epub-nav/1" || c.Member != "" || c.MemberSHA256 != "" {
			result.Status = "stale_reference"
			return result, errors.New("continuation belongs to another asset, extractor, or representation")
		}
		saved = c
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
	nav, _, err := assessment.ReadEPUBNavigation(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		result.Status = documentStatus(ctx, err)
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return result, err
	}
	parsed, err := parseNavigation(ctx, nav, documents)
	if err != nil {
		result.Status = documentStatus(ctx, err)
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return result, err
	}
	units, texts, gaps := navigationText(documents)
	encoded, err := json.Marshal(struct {
		Extractor  string
		Units      []Unit
		Texts      []string
		Gaps       [][]Gap
		Navigation []NavDocument
	}{NavExtractor, units, texts, gaps, parsed})
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
	if req.Cursor != "" && saved.RepresentationID != result.RepresentationID {
		result.Status = "stale_reference"
		return result, errors.New("continuation representation changed")
	}
	result.Navigation = parsed
	result.Coverage = navigationCoverage(parsed)
	if req.UnitsOnly {
		result.Status = "units_available"
		return finishStructured(ctx, result)
	}
	evidence, order, absUnit, absOffset, err := selectNavEntry(req, saved, parsed)
	if err != nil {
		result.Status = navStatus(err)
		return result, err
	}
	entry, doc := findNavEntry(parsed, evidence, order)
	if entry == nil {
		for _, d := range parsed {
			if d.Evidence == evidence && d.Problem != "" {
				result.Status = "no_navigation"
				return result, fmt.Errorf("navigation %s: %s", evidence, d.Problem)
			}
		}
		result.Status = "invalid_request"
		return result, errNavEntry
	}
	if entry.Status != "resolved" || entry.Range == nil || (entry.Range.Status != "bounded" && entry.Range.Status != "end_of_source") {
		result.Status = "unresolved_navigation"
		return result, fmt.Errorf("navigation entry %d cannot be read: %s", order, entry.Status)
	}
	if absUnit < 0 {
		absUnit = entry.Range.StartUnit
		absOffset = entry.Range.StartOffset + req.Offset
		if req.Offset < 0 || absOffset < entry.Range.StartOffset {
			result.Status = "invalid_request"
			return result, errors.New("text offset is outside the declared entry")
		}
	}
	return readNavRange(ctx, req, result, *entry, doc, texts, gaps, documents, absUnit, absOffset)
}

func navigationText(documents []document) ([]Unit, []string, [][]Gap) {
	units, texts, gaps := []Unit{}, []string{}, [][]Gap{}
	for _, d := range documents {
		u := d.unit
		u.Parts = 0
		if u.Extraction != "unsupported_document" {
			u.Extraction = "supported_plain_text"
			if u.KnownGaps > 0 {
				u.Extraction = "supported_text_with_gaps"
			}
		}
		units = append(units, u)
		texts = append(texts, d.text)
		gaps = append(gaps, documentGaps(d))
	}
	return units, texts, gaps
}

func navigationCoverage(docs []NavDocument) *Coverage {
	c := &Coverage{Text: "no_navigation", KnownGaps: []string{}, ReferencedOutsideRange: []string{}, Unknown: []string{"Navigation labels and targets are source declarations, not inferred chapters or a completeness check."}}
	hasNav, hasNCX := false, false
	unresolved := 0
	for _, doc := range docs {
		switch doc.Evidence {
		case "nav-doc":
			hasNav = true
		case "ncx":
			hasNCX = true
		}
		if doc.Problem != "" {
			c.Unknown = append(c.Unknown, doc.Evidence+": "+doc.Problem)
		}
		for _, e := range doc.Entries {
			if e.Status != "resolved" {
				unresolved++
			}
		}
	}
	switch {
	case hasNav && hasNCX:
		c.Navigation = "nav-doc+ncx"
		c.Text = "navigation_listed"
	case hasNav:
		c.Navigation = "nav-doc"
		c.Text = "navigation_listed"
	case hasNCX:
		c.Navigation = "ncx"
		c.Text = "navigation_listed"
	default:
		c.Navigation = "absent"
	}
	if hasNav && hasNCX && navConflicts(docs) {
		c.NavigationConflict = true
		c.Unknown = append(c.Unknown, "NAV and NCX disagree on hierarchy or resolved targets. Choose one with --navigation.")
	}
	c.UnresolvedEntries = unresolved
	return c
}

func navConflicts(docs []NavDocument) bool {
	var a, b *NavDocument
	for i := range docs {
		switch docs[i].Evidence {
		case "nav-doc":
			if a == nil {
				a = &docs[i]
			}
		case "ncx":
			if b == nil {
				b = &docs[i]
			}
		}
	}
	if a == nil || b == nil {
		return false
	}
	if a.Problem != "" || b.Problem != "" || len(a.Entries) != len(b.Entries) {
		return true
	}
	for i := range a.Entries {
		if !sameNavTarget(a.Entries[i], b.Entries[i]) {
			return true
		}
	}
	return false
}

func sameNavTarget(a, b NavEntry) bool {
	if a.Level != b.Level || a.Status != b.Status || (a.Parent == nil) != (b.Parent == nil) {
		return false
	}
	if a.Parent != nil && *a.Parent != *b.Parent {
		return false
	}
	if (a.Unit == nil) != (b.Unit == nil) || (a.Offset == nil) != (b.Offset == nil) {
		return false
	}
	if a.Unit != nil && (*a.Unit != *b.Unit || *a.Offset != *b.Offset) {
		return false
	}
	return true
}

var (
	errNavChoice  = errors.New("both a navigation document and an NCX are present; choose --navigation nav-doc or --navigation ncx")
	errNavMissing = errors.New("this EPUB has no readable navigation document or NCX")
	errNavEntry   = errors.New("navigation entry is outside the declared list")
	errNavCursor  = errors.New("continuation is outside the declared entry")
)

func navStatus(err error) string {
	switch {
	case errors.Is(err, errNavChoice):
		return "navigation_choice_required"
	case errors.Is(err, errNavMissing):
		return "no_navigation"
	case errors.Is(err, errNavCursor):
		return "stale_reference"
	default:
		return "invalid_request"
	}
}

func selectNavEntry(req Request, saved cursor, docs []NavDocument) (evidence string, order, unit, offset int, err error) {
	unit, offset = -1, -1
	if req.Cursor != "" {
		evidence, order, err = parseNavPart(saved.Part)
		if err != nil {
			return "", 0, 0, 0, err
		}
		return evidence, order, saved.Unit, saved.Offset, nil
	}
	present := map[string]bool{}
	for _, doc := range docs {
		if doc.Evidence == "nav-doc" || doc.Evidence == "ncx" {
			present[doc.Evidence] = true
		}
	}
	switch {
	case present["nav-doc"] && present["ncx"]:
		if req.Navigation == "" {
			return "", 0, 0, 0, errNavChoice
		}
		evidence = req.Navigation
	case present["nav-doc"]:
		evidence = "nav-doc"
	case present["ncx"]:
		evidence = "ncx"
	default:
		return "", 0, 0, 0, errNavMissing
	}
	if req.Navigation != "" {
		evidence = req.Navigation
	}
	if !present[evidence] {
		return "", 0, 0, 0, errNavMissing
	}
	return evidence, req.Unit, -1, -1, nil
}

func parseNavPart(part string) (string, int, error) {
	evidence, rest, ok := strings.Cut(part, ":")
	if !ok || (evidence != "nav-doc" && evidence != "ncx") {
		return "", 0, errors.New("navigation continuation is missing its entry")
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 0 || strconv.Itoa(n) != rest {
		return "", 0, errors.New("navigation continuation entry is invalid")
	}
	return evidence, n, nil
}

func findNavEntry(docs []NavDocument, evidence string, order int) (*NavEntry, NavDocument) {
	for _, doc := range docs {
		if doc.Evidence != evidence || doc.Problem != "" {
			continue
		}
		if order >= 0 && order < len(doc.Entries) && doc.Entries[order].Order == order {
			return &doc.Entries[order], doc
		}
	}
	return nil, NavDocument{}
}

func readNavRange(ctx context.Context, req Request, result Result, entry NavEntry, doc NavDocument, texts []string, gaps [][]Gap, documents []document, unit, offset int) (Result, error) {
	if err := ctx.Err(); err != nil {
		result.Status = "cancelled"
		return result, err
	}
	span := entry.Range
	if unit < span.StartUnit || unit > span.EndUnit || unit >= len(texts) || unit < 0 {
		result.Status = "invalid_request"
		return result, errNavCursor
	}
	if !navContains(span, unit, offset) && !(unit == span.EndUnit && offset == span.EndOffset) {
		result.Status = "invalid_request"
		return result, errNavCursor
	}
	if result.Units[unit].Extraction == "unsupported_document" {
		result.Status = "unsupported_extraction"
		return result, errUnsupported
	}
	text := texts[unit]
	limit := len(text)
	if unit == span.EndUnit {
		if span.EndOffset < 0 || span.EndOffset > len(text) {
			result.Status = "invalid_request"
			return result, errors.New("declared range end is outside the source text")
		}
		limit = span.EndOffset
	}
	if offset < 0 || offset > limit || (offset < len(text) && !utf8.RuneStart(text[offset])) {
		result.Status = "invalid_request"
		return result, errors.New("text offset is outside the declared entry or splits UTF-8")
	}
	end := min(limit, offset+req.MaxBytes)
	for end > offset && end < len(text) && !utf8.RuneStart(text[end]) {
		end--
	}
	if end < offset {
		end = offset
	}
	result.Text = text[offset:end]
	result.Locator = &Locator{SchemaVersion: 1, AssetID: req.AssetID, Extractor: NavExtractor, RepresentationID: result.RepresentationID, Unit: unit, Start: offset, End: end, Member: result.Units[unit].Member, OffsetUnit: "utf8_bytes"}
	result.RangeEnd = &Locator{SchemaVersion: 1, AssetID: req.AssetID, Extractor: NavExtractor, RepresentationID: result.RepresentationID, Unit: span.EndUnit, Start: span.EndOffset, End: span.EndOffset, Member: memberAt(result.Units, span.EndUnit), OffsetUnit: "utf8_bytes"}
	result.EndOfUnit = end == len(text)
	reached := end == limit && (unit == span.EndUnit && limit == span.EndOffset || !navContains(span, nextNavUnit(unit, end, text), nextNavOffset(unit, end, text)))
	if end < limit {
		reached = false
	}
	if reached {
		result.RangeStatus = "range_end"
		result.Status = "complete_range"
	} else {
		result.RangeStatus = "within_range"
		result.Status = "partial_range"
	}
	result.EndOfSource = result.RangeStatus == "range_end" && span.EndAtSource && result.EndOfUnit && unit == len(texts)-1
	result.Navigation = []NavDocument{{Evidence: doc.Evidence, Path: doc.Path, Entries: []NavEntry{entry}}}
	result.Breaks = rangeBreaks(text, documents[unit].breaks, offset, end)
	result.Gaps = []Gap{}
	result.Coverage.Text = "bounded_selected_range"
	result.Coverage.KnownGaps = []string{}
	for _, g := range gaps[unit] {
		if (g.Offset >= offset && g.Offset < end) || (g.Offset == end && result.EndOfUnit) {
			result.Gaps = append(result.Gaps, g)
			result.Coverage.KnownGaps = append(result.Coverage.KnownGaps, g.Part)
			result.Coverage.Text = "known_extraction_gaps"
		}
	}
	if result.RangeStatus != "range_end" {
		nextUnit, nextOffset := unit, end
		if end == len(text) {
			nextUnit, nextOffset = unit+1, 0
		}
		if navContains(span, nextUnit, nextOffset) {
			token := encodeCursor(cursor{SchemaVersion: 1, AssetID: req.AssetID, Extractor: NavExtractor, RepresentationID: result.RepresentationID, Unit: nextUnit, Offset: nextOffset, Representation: "epub-nav/1", Part: fmt.Sprintf("%s:%d", entry.Evidence, entry.Order)})
			if len(token) > 2048 {
				result.Status = "budget_exceeded"
				return result, errBudget
			}
			result.Continuation = token
		} else {
			result.RangeStatus = "range_end"
			result.Status = "complete_range"
			result.EndOfSource = span.EndAtSource && result.EndOfUnit && unit == len(texts)-1
		}
	}
	return finishStructured(ctx, result)
}

// NavigationCursor is the continuation that reads epub-nav/1 at an absolute
// spine position inside one declared entry. It does not open the book. The
// cursor is the only reference that keeps the entry identity with that position.
func NavigationCursor(assetID, representationID, evidence string, order, unit, offset int) (string, error) {
	if !validContentID(assetID) || !validContentID(representationID) {
		return "", errors.New("navigation cursor requires sha256 asset and representation ids")
	}
	if (evidence != "nav-doc" && evidence != "ncx") || order < 0 || order >= maxNavEntries || unit < 0 || unit >= maxRequestUnit || offset < 0 || offset > maxText {
		return "", errors.New("navigation cursor entry or position is outside the retrieval bounds")
	}
	token := encodeCursor(cursor{SchemaVersion: 1, AssetID: assetID, Extractor: NavExtractor, RepresentationID: representationID, Unit: unit, Offset: offset, Representation: "epub-nav/1", Part: fmt.Sprintf("%s:%d", evidence, order)})
	if len(token) > 2048 {
		return "", errBudget
	}
	return token, nil
}

func validContentID(id string) bool {
	if len(id) != 71 || !strings.HasPrefix(id, "sha256:") {
		return false
	}
	b, err := hex.DecodeString(id[7:])
	return err == nil && len(b) == 32 && hex.EncodeToString(b) == id[7:]
}

func memberAt(units []Unit, unit int) string {
	if unit >= 0 && unit < len(units) {
		return units[unit].Member
	}
	return ""
}

func nextNavUnit(unit, end int, text string) int {
	if end == len(text) {
		return unit + 1
	}
	return unit
}

func nextNavOffset(unit, end int, text string) int {
	if end == len(text) {
		return 0
	}
	return end
}

func navContains(span *NavRange, unit, offset int) bool {
	if span == nil {
		return false
	}
	start := unit > span.StartUnit || (unit == span.StartUnit && offset >= span.StartOffset)
	end := unit < span.EndUnit || (unit == span.EndUnit && offset < span.EndOffset)
	return start && end
}

func parseNavigation(ctx context.Context, nav assessment.EPUBNavigation, documents []document) ([]NavDocument, error) {
	docs := []NavDocument{}
	packagePath := ""
	if len(documents) > 0 {
		packagePath = documents[0].unit.Package
	}
	for _, source := range nav.Sources {
		if packagePath != "" && source.Package != "" && source.Package != packagePath {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		doc := NavDocument{Evidence: source.Kind, Path: source.Path, Entries: []NavEntry{}}
		if source.Kind != "nav" && source.Kind != "ncx" {
			doc.Evidence = source.Kind
			doc.Problem = source.Problem
			if doc.Problem == "" {
				doc.Problem = "unsupported_navigation_source"
			}
			docs = append(docs, doc)
			continue
		}
		if source.Kind == "nav" {
			doc.Evidence = "nav-doc"
		}
		if source.Problem != "" {
			doc.Problem = source.Problem
			docs = append(docs, doc)
			continue
		}
		var entries []NavEntry
		var err error
		if doc.Evidence == "nav-doc" {
			entries, err = parseNavDocument(ctx, source.Data)
		} else {
			entries, err = parseNCX(ctx, source.Data)
		}
		if err != nil {
			return nil, err
		}
		for i := range entries {
			entries[i].Evidence = doc.Evidence
			resolveNavEntry(&entries[i], source.Path, documents)
		}
		assignNavRanges(entries, documents)
		doc.Entries = entries
		docs = append(docs, doc)
	}
	// Two documents of one kind are ambiguous. Neither is preferred, and a
	// partial hierarchy is not returned as if it were the book's contents.
	// A remote or unreadable document still counts; it does not leave the
	// other document silently selected.
	navDocs, ncxDocs := 0, 0
	for _, doc := range docs {
		switch doc.Evidence {
		case "nav-doc":
			navDocs++
		case "ncx":
			ncxDocs++
		}
	}
	if navDocs > 1 || ncxDocs > 1 {
		for i := range docs {
			if navDocs > 1 && docs[i].Evidence == "nav-doc" {
				if docs[i].Problem == "" {
					docs[i].Problem = "multiple_nav_documents"
				}
				docs[i].Entries = nil
			}
			if ncxDocs > 1 && docs[i].Evidence == "ncx" {
				if docs[i].Problem == "" {
					docs[i].Problem = "multiple_ncx_documents"
				}
				docs[i].Entries = nil
			}
		}
	}
	return docs, nil
}

func resolveNavEntry(e *NavEntry, navPath string, documents []document) {
	if e.Href == "" {
		e.Status = "no_href"
		return
	}
	name, fragment, status := localReference(navPath, e.Href)
	if status != "local" {
		e.Status = status
		return
	}
	unit := -1
	for i, d := range documents {
		if d.unit.Member == name {
			unit = i
			break
		}
	}
	if unit < 0 {
		e.Status = "unresolved_target"
		return
	}
	u := unit
	e.Unit = &u
	e.Member = name
	if fragment == "" {
		zero := 0
		e.Offset = &zero
		e.Status = "resolved"
		return
	}
	decoded, err := url.PathUnescape(fragment)
	if err != nil || strings.ContainsAny(decoded, "\x00") {
		e.Status = "invalid_reference"
		return
	}
	want := "id:" + decoded
	for _, p := range documents[unit].parts {
		if p.ID == want {
			off := p.Start
			e.Offset = &off
			e.Part = p.ID
			e.Status = "resolved"
			return
		}
	}
	e.Status = "unresolved_anchor"
}

func assignNavRanges(entries []NavEntry, documents []document) {
	for i := range entries {
		if entries[i].Status != "resolved" || entries[i].Unit == nil || entries[i].Offset == nil {
			continue
		}
		startU, startO := *entries[i].Unit, *entries[i].Offset
		unresolved := false
		var endU, endO int
		found := false
		for j := i + 1; j < len(entries); j++ {
			if navDescendant(entries, i, j) {
				continue
			}
			if entries[j].Status != "resolved" || entries[j].Unit == nil || entries[j].Offset == nil {
				unresolved = true
				break
			}
			endU, endO = *entries[j].Unit, *entries[j].Offset
			found = true
			break
		}
		if unresolved {
			entries[i].Range = &NavRange{StartUnit: startU, StartOffset: startO, Status: "unresolved"}
			continue
		}
		if !found {
			if len(documents) == 0 {
				entries[i].Range = &NavRange{StartUnit: startU, StartOffset: startO, Status: "unresolved"}
				continue
			}
			last := len(documents) - 1
			entries[i].Range = &NavRange{StartUnit: startU, StartOffset: startO, EndUnit: last, EndOffset: len(documents[last].text), EndAtSource: true, Status: "end_of_source"}
			continue
		}
		if endU < startU || (endU == startU && endO <= startO) {
			entries[i].Range = &NavRange{StartUnit: startU, StartOffset: startO, EndUnit: endU, EndOffset: endO, Status: "not_after_start"}
			continue
		}
		entries[i].Range = &NavRange{StartUnit: startU, StartOffset: startO, EndUnit: endU, EndOffset: endO, Status: "bounded"}
	}
}

func navDescendant(entries []NavEntry, ancestor, i int) bool {
	seen := map[int]bool{}
	for entries[i].Parent != nil {
		p := *entries[i].Parent
		if p == ancestor {
			return true
		}
		if p < 0 || p >= len(entries) || seen[p] {
			return false
		}
		seen[p] = true
		i = p
	}
	return false
}

func parseNavDocument(ctx context.Context, data []byte) ([]NavEntry, error) {
	entries, _, _, err := parseNavList(ctx, data, "toc")
	return entries, err
}

// parseNavList reads the entries of the nav element of one epub:type, toc or
// page-list. More than one toc fails, as epub-nav/1 always has; for a page
// list the caller is told how many there were. hidden reports a hidden
// attribute on the nav element.
func parseNavList(ctx context.Context, data []byte, kind string) (entries []NavEntry, hidden bool, navs int, err error) {
	if len(data) > 2<<20 || !utf8.Valid(data) {
		return nil, false, 0, errBudget
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.CharsetReader = func(string, io.Reader) (io.Reader, error) {
		return nil, errors.New("navigation document must be UTF-8")
	}
	type frame struct {
		local     string
		inTOC     bool
		li        bool
		label     bool
		labelText strings.Builder
	}
	stack := []frame{}
	liEntries := []int{}
	entries = []NavEntry{}
	inTOC := false
	for {
		if err := ctx.Err(); err != nil {
			return nil, false, 0, err
		}
		token, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return entries, hidden, navs, nil
		}
		if err != nil {
			return nil, false, 0, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			if len(stack) >= 256 {
				return nil, false, 0, errBudget
			}
			f := frame{local: t.Name.Local}
			if len(stack) > 0 {
				f.inTOC = stack[len(stack)-1].inTOC
			}
			if t.Name.Local == "nav" && navType(t, kind) {
				navs++
				if navs > 1 && kind == "toc" {
					return nil, false, 0, fmt.Errorf("%w: more than one toc nav", errBudget)
				}
				if navs > 1 {
					return nil, false, navs, nil
				}
				hidden = hasAttr(t, "hidden")
				f.inTOC = true
			}
			inTOC = f.inTOC
			if inTOC && t.Name.Local == "li" {
				if len(liEntries) >= maxNavDepth {
					return nil, false, 0, fmt.Errorf("%w: navigation deeper than %d", errBudget, maxNavDepth)
				}
				liEntries = append(liEntries, -1)
				f.li = true
			}
			labelAlready := false
			for _, open := range stack {
				if open.label {
					labelAlready = true
				}
			}
			if inTOC && (t.Name.Local == "a" || t.Name.Local == "span") && len(liEntries) > 0 && liEntries[len(liEntries)-1] < 0 && !labelAlready {
				if len(entries) >= maxNavEntries {
					return nil, false, 0, fmt.Errorf("%w: more than %d navigation entries", errBudget, maxNavEntries)
				}
				href := ""
				if t.Name.Local == "a" {
					href = attr(t, "href")
				}
				if len(href) > maxNavHref {
					return nil, false, 0, fmt.Errorf("%w: navigation href exceeds %d bytes", errBudget, maxNavHref)
				}
				parent := navParent(liEntries)
				level := len(liEntries)
				entries = append(entries, NavEntry{Href: href, Order: len(entries), Parent: parent, Level: level, Status: "unresolved"})
				liEntries[len(liEntries)-1] = len(entries) - 1
				f.label = true
			}
			stack = append(stack, f)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, false, 0, errUnsupported
			}
			f := stack[len(stack)-1]
			if f.label {
				label := collapseLabel(stack[len(stack)-1].labelText.String())
				if len(label) > maxNavLabel {
					return nil, false, 0, fmt.Errorf("%w: navigation label exceeds %d bytes", errBudget, maxNavLabel)
				}
				if li := len(liEntries) - 1; li >= 0 && liEntries[li] >= 0 {
					entries[liEntries[li]].Label = label
				}
			}
			if f.li && len(liEntries) > 0 {
				liEntries = liEntries[:len(liEntries)-1]
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i].label {
					stack[i].labelText.WriteString(string(t))
					break
				}
			}
		}
	}
}

// navType reports a nav element of one epub:type in the EPUB namespace.
func navType(t xml.StartElement, kind string) bool {
	for _, a := range t.Attr {
		if a.Name.Local == "type" && a.Name.Space == "http://www.idpf.org/2007/ops" {
			for _, v := range strings.Fields(a.Value) {
				if v == kind {
					return true
				}
			}
		}
	}
	return false
}

func navParent(liEntries []int) *int {
	for i := len(liEntries) - 2; i >= 0; i-- {
		if liEntries[i] >= 0 {
			p := liEntries[i]
			return &p
		}
	}
	return nil
}

func collapseLabel(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func parseNCX(ctx context.Context, data []byte) ([]NavEntry, error) {
	if len(data) > 2<<20 || !utf8.Valid(data) {
		return nil, errBudget
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.CharsetReader = func(string, io.Reader) (io.Reader, error) {
		return nil, errors.New("NCX must be UTF-8")
	}
	type frame struct {
		local   string
		entry   int
		inLabel bool
		inText  bool
		text    strings.Builder
	}
	stack := []frame{}
	points := []int{}
	entries := []NavEntry{}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		token, err := dec.Token()
		if errors.Is(err, io.EOF) {
			for i := range entries {
				if entries[i].Label == "" && entries[i].Status == "" {
					entries[i].Status = "unresolved"
				}
			}
			return entries, nil
		}
		if err != nil {
			return nil, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			if len(stack) >= 256 {
				return nil, errBudget
			}
			f := frame{local: t.Name.Local, entry: -1}
			if len(stack) > 0 {
				f.inLabel = stack[len(stack)-1].inLabel
				f.entry = stack[len(stack)-1].entry
			}
			if t.Name.Local == "navPoint" {
				if len(points) >= maxNavDepth {
					return nil, fmt.Errorf("%w: navigation deeper than %d", errBudget, maxNavDepth)
				}
				if len(entries) >= maxNavEntries {
					return nil, fmt.Errorf("%w: more than %d navigation entries", errBudget, maxNavEntries)
				}
				parent := navParent(append(points, -1))
				entry := NavEntry{Order: len(entries), Parent: parent, Level: len(points) + 1, Status: "unresolved"}
				if raw := attr(t, "playOrder"); raw != "" {
					n, conv := strconv.Atoi(raw)
					if conv != nil || n < 0 || strconv.Itoa(n) != raw {
						return nil, fmt.Errorf("%w: NCX playOrder is not a canonical non-negative integer", errBudget)
					}
					entry.PlayOrder = &n
				}
				entries = append(entries, entry)
				f.entry = len(entries) - 1
				points = append(points, f.entry)
			}
			if t.Name.Local == "navLabel" {
				f.inLabel = true
			}
			if t.Name.Local == "text" && f.inLabel {
				f.inText = true
			}
			if t.Name.Local == "content" && f.entry >= 0 && entries[f.entry].Href == "" {
				href := attr(t, "src")
				if len(href) > maxNavHref {
					return nil, fmt.Errorf("%w: navigation href exceeds %d bytes", errBudget, maxNavHref)
				}
				entries[f.entry].Href = href
			}
			stack = append(stack, f)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, errUnsupported
			}
			f := stack[len(stack)-1]
			if f.local == "text" && f.inText && f.entry >= 0 {
				label := collapseLabel(stack[len(stack)-1].text.String())
				if len(label) > maxNavLabel {
					return nil, fmt.Errorf("%w: navigation label exceeds %d bytes", errBudget, maxNavLabel)
				}
				if entries[f.entry].Label == "" {
					entries[f.entry].Label = label
				} else if label != "" {
					entries[f.entry].AltLabels = append(entries[f.entry].AltLabels, label)
				}
			}
			if t.Name.Local == "navPoint" && len(points) > 0 {
				points = points[:len(points)-1]
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i].local == "text" && stack[i].inText {
					stack[i].text.WriteString(string(t))
					break
				}
			}
		}
	}
}
