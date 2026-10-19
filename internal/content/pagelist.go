package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/blisspixel/nemalo/internal/assessment"
)

// PageListExtractor is the epub-page-list/1 extractor. It lists an EPUB 3
// navigation document's page-list: the source's own page markers, such as
// "iv" or "42". They are assertions of this exact EPUB, not screen pages,
// spine units, chapter numbers, or a match to any print edition. Offsets
// are epub-text/2 coordinates. The table of contents stays epub-nav/1.
const PageListExtractor = "nemalo.epub.page-list/1"

// PageList is one declared page-list. Hidden reports the nav element's hidden
// attribute, common for page lists; it is still the source's declaration.
type PageList struct {
	Path    string      `json:"path"`
	Hidden  bool        `json:"hidden,omitempty"`
	Problem string      `json:"problem,omitempty"`
	Entries []PageEntry `json:"entries"`
}

// PageEntry is one declared page marker, in document order. Label is the
// declared text with whitespace collapsed; duplicate labels stay duplicate.
// Reference reads from this marker with epub-page-list/1 and is bound to its
// representation identity.
type PageEntry struct {
	Label     string `json:"label"`
	Href      string `json:"href,omitempty"`
	Order     int    `json:"order"`
	Status    string `json:"status"`
	Unit      *int   `json:"unit,omitempty"`
	Offset    *int   `json:"offset,omitempty"`
	Part      string `json:"part,omitempty"`
	Member    string `json:"member,omitempty"`
	Reference string `json:"reference,omitempty"`
}

// Pagination is what the package states about the source of its page
// markers. Status is declared or unknown; a declaration is not checked
// against any edition.
type Pagination struct {
	Status           string   `json:"status"`
	Sources          []string `json:"sources,omitempty"`
	PageBreakSources []string `json:"page_break_sources,omitempty"`
}

func getPageList(ctx context.Context, req Request, result Result) (Result, error) {
	result.Extractor, result.Representation = PageListExtractor, "epub-page-list/1"
	result.Limitations[1] = "Page labels are the EPUB's declared page markers. They are not screen pages, chapters, or proof of a match to a print edition."
	var saved cursor
	if req.Cursor != "" {
		c, err := parseCursor(req.Cursor)
		if err != nil {
			result.Status = "invalid_request"
			return result, err
		}
		if c.AssetID != req.AssetID || c.Extractor != PageListExtractor || c.Representation != "epub-page-list/1" || c.Member != "" || c.MemberSHA256 != "" || !strings.HasPrefix(c.Part, "page:") {
			result.Status = "stale_reference"
			return result, errors.New("reference belongs to another asset, extractor, or representation")
		}
		saved = c
	}
	documents, data, err := loadEPUBText(ctx, req, &result)
	if err != nil {
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
	view, err := viewTextV2(documents)
	if err != nil {
		result.Status = "budget_exceeded"
		return result, err
	}
	lists, err := parsePageLists(ctx, nav, documents)
	if err != nil {
		result.Status = documentStatus(ctx, err)
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return result, err
	}
	pagination := pageSource(nav, documents)
	encoded, err := json.Marshal(struct {
		Extractor  string
		Text       string
		PageLists  []PageList
		Pagination Pagination
	}{PageListExtractor, view.id, lists, pagination})
	if err != nil {
		return result, err
	}
	hash := sha256.Sum256(encoded)
	result.RepresentationID = "sha256:" + hex.EncodeToString(hash[:])
	if req.Cursor != "" && saved.RepresentationID != result.RepresentationID {
		result.Status = "stale_reference"
		return result, errors.New("the page list or text changed since this reference was issued")
	}
	for i := range lists {
		for j := range lists[i].Entries {
			e := &lists[i].Entries[j]
			if e.Status == "resolved" {
				e.Reference = encodeCursor(cursor{SchemaVersion: 1, AssetID: req.AssetID, Extractor: PageListExtractor, RepresentationID: result.RepresentationID, Unit: *e.Unit, Offset: *e.Offset, Representation: "epub-page-list/1", Part: "page:" + strconv.Itoa(e.Order)})
			}
		}
	}
	result.Units, result.UnsupportedUnits, result.Pagination = view.units, view.unsupported, &pagination
	if req.UnitsOnly {
		result.Status, result.PageLists = "units_available", lists
		if len(lists) == 0 {
			result.Status = "no_page_list"
		}
		return finishStructured(ctx, result)
	}
	var list *PageList
	for i := range lists {
		if lists[i].Problem == "" {
			list = &lists[i]
			break
		}
	}
	if list == nil {
		result.Status = "no_page_list"
		return result, errors.New("this EPUB declares no usable page list")
	}
	order := req.Unit
	switch {
	case req.Cursor != "":
		order, err = strconv.Atoi(strings.TrimPrefix(saved.Part, "page:"))
		if err != nil {
			result.Status = "invalid_request"
			return result, errors.New("invalid page reference")
		}
	case req.Label != "":
		var matches []PageEntry
		for _, e := range list.Entries {
			if e.Label == req.Label {
				matches = append(matches, e)
			}
		}
		switch len(matches) {
		case 0:
			result.Status = "no_matching_label"
			return result, fmt.Errorf("the page list has no label %q", req.Label)
		case 1:
			order = matches[0].Order
		default:
			// Several markers carry this label. Each is a candidate with its own
			// reference; none is chosen.
			result.Status = "ambiguous_label"
			result.PageLists = []PageList{{Path: list.Path, Hidden: list.Hidden, Entries: matches}}
			return finishStructured(ctx, result)
		}
	}
	if order < 0 || order >= len(list.Entries) {
		result.Status = "invalid_request"
		return result, errors.New("no page-list entry has that order")
	}
	entry := list.Entries[order]
	if entry.Status != "resolved" {
		result.Status = "unresolved_page"
		return result, fmt.Errorf("page %q cannot be read: %s", entry.Label, entry.Status)
	}
	unit, offset := *entry.Unit, *entry.Offset
	if req.Cursor != "" && (saved.Unit != unit || saved.Offset != offset) {
		result.Status = "stale_reference"
		return result, errors.New("the page marker moved since this reference was issued")
	}
	read, err := readTextV2Range(ctx, req, result, view, unit, offset)
	if err != nil {
		return read, err
	}
	read.Representation, read.Page = "epub-page-list/1", &entry
	// The page ends at the next declared marker only when that marker resolves
	// strictly after this one. Otherwise no end is claimed.
	if order+1 < len(list.Entries) {
		next := list.Entries[order+1]
		if next.Status == "resolved" && (*next.Unit > unit || (*next.Unit == unit && *next.Offset > offset)) {
			read.PageEnd = &Locator{SchemaVersion: 1, AssetID: req.AssetID, Extractor: TextExtractorV2, RepresentationID: view.id, Unit: *next.Unit, Start: *next.Offset, End: *next.Offset, Member: next.Member, OffsetUnit: "utf8_bytes"}
		}
	}
	if read.PageEnd == nil {
		read.Coverage.Unknown = append(read.Coverage.Unknown, "Where this page ends is not established: the next marker is absent, unresolved, or not after this one.")
	}
	return read, nil
}

// parsePageLists reads the page-list of every EPUB 3 navigation document of
// the reading package. NCX page lists are not read.
func parsePageLists(ctx context.Context, nav assessment.EPUBNavigation, documents []document) ([]PageList, error) {
	lists := []PageList{}
	packagePath := ""
	if len(documents) > 0 {
		packagePath = documents[0].unit.Package
	}
	for _, source := range nav.Sources {
		if source.Kind != "nav" || (packagePath != "" && source.Package != "" && source.Package != packagePath) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if source.Problem != "" {
			lists = append(lists, PageList{Path: source.Path, Problem: source.Problem, Entries: []PageEntry{}})
			continue
		}
		entries, hidden, navs, err := parseNavList(ctx, source.Data, "page-list")
		if err != nil {
			return nil, err
		}
		switch {
		case navs == 0:
			continue
		case navs > 1:
			lists = append(lists, PageList{Path: source.Path, Problem: "multiple_page_lists", Entries: []PageEntry{}})
			continue
		}
		list := PageList{Path: source.Path, Hidden: hidden, Entries: []PageEntry{}}
		for i := range entries {
			resolveNavEntry(&entries[i], source.Path, documents)
			e := entries[i]
			if e.Offset == nil {
				// A target member without its marker is not a location.
				e.Unit = nil
			}
			list.Entries = append(list.Entries, PageEntry{Label: e.Label, Href: e.Href, Order: e.Order, Status: e.Status, Unit: e.Unit, Offset: e.Offset, Part: e.Part, Member: e.Member})
		}
		lists = append(lists, list)
	}
	if len(lists) > 1 {
		// Two navigation documents are ambiguous; neither page list is chosen.
		for i := range lists {
			if lists[i].Problem == "" {
				lists[i].Problem = "multiple_nav_documents"
			}
			lists[i].Entries = []PageEntry{}
		}
	}
	return lists, nil
}

func pageSource(nav assessment.EPUBNavigation, documents []document) Pagination {
	p := Pagination{Status: "unknown"}
	packagePath := ""
	if len(documents) > 0 {
		packagePath = documents[0].unit.Package
	}
	for _, declared := range nav.Pagination {
		if packagePath != "" && declared.Package != packagePath {
			continue
		}
		p.Sources = append(p.Sources, declared.Sources...)
		p.PageBreakSources = append(p.PageBreakSources, declared.PageBreakSources...)
	}
	if len(p.Sources) > 0 || len(p.PageBreakSources) > 0 {
		p.Status = "declared"
	}
	return p
}
