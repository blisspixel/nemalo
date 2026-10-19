package content

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// pageFiles is a two-file book with a toc and a hidden page-list: labels iv
// and 1 in the first file, an empty page-break marker, a duplicate label, a
// cross-file target, a missing fragment, and a backward target.
func pageFiles(pageList string) map[string]string {
	manifest, spine := spineManifest(2, "")
	manifest += `<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>`
	pkg := `<package xmlns="http://www.idpf.org/2007/opf"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Pages</dc:title><dc:language>en</dc:language>` +
		`<dc:source id="print">urn:isbn:9780306406157</dc:source><meta refines="#print" property="source-of">pagination</meta><meta property="a11y:pageBreakSource">urn:isbn:9780306406157</meta>` +
		`</metadata><manifest>` + manifest + `</manifest><spine>` + spine + `</spine></package>`
	return map[string]string{
		"META-INF/container.xml": containerXML(),
		"book.opf":               pkg,
		"unit0.xhtml":            xhtml(`<h1 id="c1">One</h1><p><span id="piv"/>Preface text. <span epub:type="pagebreak" id="p1" title="1"></span>Page one text.</p><p id="p2">Page two text.</p>`),
		"unit1.xhtml":            xhtml(`<p id="p3">Page three text.</p>`),
		"nav.xhtml": `<?xml version="1.0" encoding="UTF-8"?><html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><body>` +
			`<nav epub:type="toc"><ol><li><a href="unit0.xhtml#c1">One</a></li></ol></nav>` + pageList + `</body></html>`,
	}
}

const pageListNav = `<nav epub:type="page-list" hidden=""><ol>` +
	`<li><a href="unit0.xhtml#piv">iv</a></li>` +
	`<li><a href="unit0.xhtml#p1">1</a></li>` +
	`<li><a href="unit0.xhtml#p2">2</a></li>` +
	`<li><a href="unit1.xhtml#p3">3</a></li>` +
	`<li><a href="unit0.xhtml#p2">2</a></li>` +
	`<li><a href="unit1.xhtml#gone">4</a></li>` +
	`</ol></nav>`

func pageRequest(t *testing.T, files map[string]string) Request {
	t.Helper()
	req := publishEPUB(t, files)
	req.Representation = "epub-page-list/1"
	return req
}

func TestPageListListsDeclaredMarkersExactly(t *testing.T) {
	req := pageRequest(t, pageFiles(pageListNav))
	req.UnitsOnly = true
	r := mustGet(t, req)
	if r.Status != "units_available" || r.Extractor != PageListExtractor || len(r.PageLists) != 1 {
		t.Fatal(r.Status, r.PageLists)
	}
	list := r.PageLists[0]
	if !list.Hidden || list.Path != "nav.xhtml" || len(list.Entries) != 6 {
		t.Fatalf("%+v", list)
	}
	text := mustGet(t, Request{SchemaVersion: 1, Catalog: req.Catalog, Root: req.Root, AssetID: req.AssetID, Representation: "epub-text/2", MaxBytes: 65536})
	var got []string
	for _, e := range list.Entries {
		where := e.Status
		if e.Unit != nil {
			where = fmt.Sprintf("%s@%d:%d", e.Status, *e.Unit, *e.Offset)
		}
		got = append(got, e.Label+"="+where)
		if (e.Status == "resolved") != (e.Reference != "") {
			t.Fatal("reference without a resolved target, or none for one", e)
		}
	}
	// Offsets are epub-text/2 coordinates: each marker lands on its text.
	at := func(label string, order int) string {
		e := list.Entries[order]
		if *e.Unit != 0 {
			return ""
		}
		return text.Text[*e.Offset:]
	}
	if !strings.HasPrefix(at("iv", 0), "Preface") || !strings.HasPrefix(at("1", 1), "Page one") || !strings.HasPrefix(at("2", 2), "Page two") {
		t.Fatal("markers do not land on their text", got)
	}
	want := []string{"iv=resolved@0:", "1=resolved@0:", "2=resolved@0:", "3=resolved@1:0", "2=resolved@0:", "4=unresolved_anchor"}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) {
			t.Fatalf("entries\n got %v\nwant %v", got, want)
		}
	}
	if r.Pagination == nil || r.Pagination.Status != "declared" || len(r.Pagination.Sources) != 1 || r.Pagination.Sources[0] != "urn:isbn:9780306406157" || len(r.Pagination.PageBreakSources) != 1 {
		t.Fatalf("pagination %+v", r.Pagination)
	}
	// The table of contents is unchanged by a page list: same entries, same
	// epub-nav/1 identity as the book without one.
	withList := mustGet(t, Request{SchemaVersion: 1, Catalog: req.Catalog, Root: req.Root, AssetID: req.AssetID, Representation: "epub-nav/1", UnitsOnly: true, MaxBytes: 4096})
	plain := publishEPUB(t, pageFiles(""))
	plain.UnitsOnly = true
	without := mustGet(t, plain)
	if withList.RepresentationID != without.RepresentationID || len(withList.Navigation[0].Entries) != 1 || withList.Navigation[0].Entries[0].Label != "One" {
		t.Fatal("a page list changed the table of contents", withList.Navigation)
	}
}

func TestPageListReadsFromOneMarker(t *testing.T) {
	req := pageRequest(t, pageFiles(pageListNav))
	listing := req
	listing.UnitsOnly = true
	list := mustGet(t, listing).PageLists[0]
	// By exact label, with the next marker as the established end.
	byLabel := req
	byLabel.Label = "1"
	r := mustGet(t, byLabel)
	if r.Representation != "epub-page-list/1" || r.Page == nil || r.Page.Label != "1" || !strings.HasPrefix(r.Text, "Page one") || r.Locator.Extractor != TextExtractorV2 {
		t.Fatalf("%s %+v %q", r.Status, r.Page, r.Text)
	}
	if r.PageEnd == nil || r.PageEnd.Unit != 0 || r.PageEnd.Start != *list.Entries[2].Offset {
		t.Fatal("page end", r.PageEnd)
	}
	// The continuation reads on in epub-text/2, past the marker's page.
	next := mustGet(t, Request{SchemaVersion: 1, Catalog: req.Catalog, Root: req.Root, AssetID: req.AssetID, Cursor: r.Continuation, MaxBytes: 4096})
	if next.Representation != "epub-text/2" || !strings.Contains(next.Text, "Page three") {
		t.Fatal(next.Representation, next.Text)
	}
	// A duplicate label returns every candidate, with its own reference.
	dup := req
	dup.Label = "2"
	r = mustGet(t, dup)
	if r.Status != "ambiguous_label" || r.Text != "" || len(r.PageLists) != 1 || len(r.PageLists[0].Entries) != 2 || r.PageLists[0].Entries[0].Reference == r.PageLists[0].Entries[1].Reference {
		t.Fatalf("%s %+v", r.Status, r.PageLists)
	}
	byRef := mustGet(t, Request{SchemaVersion: 1, Catalog: req.Catalog, Root: req.Root, AssetID: req.AssetID, Cursor: r.PageLists[0].Entries[1].Reference, MaxBytes: 4096})
	if byRef.Page.Order != 4 || !strings.HasPrefix(byRef.Text, "Page two") {
		t.Fatal(byRef.Page, byRef.Text)
	}
	// A marker followed by an earlier target claims no end.
	cross := req
	cross.Unit = 3
	r = mustGet(t, cross)
	if !strings.HasPrefix(r.Text, "Page three") || r.PageEnd != nil || !strings.Contains(strings.Join(r.Coverage.Unknown, " "), "not established") {
		t.Fatal("backward next marker", r.PageEnd, r.Coverage.Unknown)
	}
	for _, bad := range []struct {
		label  string
		unit   int
		status string
	}{{"9", 0, "no_matching_label"}, {"", 5, "unresolved_page"}, {"", 6, "invalid_request"}} {
		q := req
		q.Label, q.Unit = bad.label, bad.unit
		if r := mustFail(t, q, bad.status); r.Text != "" || r.PageLists != nil {
			t.Fatal("failure leaked a result", bad)
		}
	}
	// A changed page list makes an issued reference stale; it is not remapped.
	changed := pageFiles(strings.Replace(pageListNav, ">1<", ">one<", 1))
	moved := publishEPUB(t, changed)
	stale := Request{SchemaVersion: 1, Catalog: moved.Catalog, Root: moved.Root, AssetID: moved.AssetID, Cursor: list.Entries[1].Reference, MaxBytes: 4096}
	mustFail(t, stale, "stale_reference")
}

func TestPageListAbsentHiddenAndOverBudget(t *testing.T) {
	absent := pageRequest(t, pageFiles(""))
	absent.UnitsOnly = true
	r := mustGet(t, absent)
	if r.Status != "no_page_list" || len(r.PageLists) != 0 || r.Pagination.Status != "declared" {
		t.Fatal(r.Status, r.PageLists)
	}
	read := absent
	read.UnitsOnly = false
	mustFail(t, read, "no_page_list")
	two := pageRequest(t, pageFiles(pageListNav+pageListNav))
	two.UnitsOnly = true
	if r := mustGet(t, two); r.PageLists[0].Problem != "multiple_page_lists" || len(r.PageLists[0].Entries) != 0 {
		t.Fatal("two page lists", r.PageLists)
	}
	var many strings.Builder
	for i := 0; i <= maxNavEntries; i++ {
		fmt.Fprintf(&many, `<li><a href="unit0.xhtml#p1">%d</a></li>`, i)
	}
	over := pageRequest(t, pageFiles(`<nav epub:type="page-list"><ol>`+many.String()+`</ol></nav>`))
	over.UnitsOnly = true
	mustFail(t, over, "budget_exceeded")
	// Without package declarations the pagination source is unknown.
	files := pageFiles(pageListNav)
	files["book.opf"] = packageDoc(`<item id="u0" href="unit0.xhtml" media-type="application/xhtml+xml"/><item id="u1" href="unit1.xhtml" media-type="application/xhtml+xml"/><item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>`, "", `<itemref idref="u0"/><itemref idref="u1"/>`)
	unknown := pageRequest(t, files)
	unknown.UnitsOnly = true
	if r := mustGet(t, unknown); r.Pagination.Status != "unknown" || r.Pagination.Sources != nil {
		t.Fatal(r.Pagination)
	}
	if _, err := Get(context.Background(), Request{SchemaVersion: 1, Catalog: unknown.Catalog, Root: unknown.Root, AssetID: unknown.AssetID, Representation: "epub-text/2", Label: "1", MaxBytes: 4096}); err == nil {
		t.Fatal("a label applied to another representation")
	}
}
