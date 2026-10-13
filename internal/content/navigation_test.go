package content

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/blisspixel/nemalo/internal/library"
)

func publishEPUB(t *testing.T, files map[string]string) Request {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	f, err := z.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write([]byte("application/epub+zip")); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(files[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "book.epub"), buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	hash := hex.EncodeToString(sum[:])
	catalog := library.Catalog{SchemaVersion: 1, ID: "snapshot:" + strings.Repeat("1", 32), CreatedAt: time.Unix(1, 0).UTC(), RootHint: "untrusted-unused-root", Complete: true, Assets: []library.Asset{{ID: "sha256:" + hash, SHA256: hash, Bytes: int64(buf.Len()), Locations: []library.Location{{Path: "book.epub", CandidateKind: "ebook_candidate"}}}}}
	raw, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return Request{SchemaVersion: 1, Catalog: path, Root: root, AssetID: catalog.Assets[0].ID, MaxBytes: 65536, Representation: "epub-nav/1"}
}

func containerXML() string {
	return `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`
}

func packageDoc(manifest, spineAttrs, spine string) string {
	return `<package xmlns="http://www.idpf.org/2007/opf"><metadata><title>Fixture</title><language>mul</language></metadata><manifest>` + manifest + `</manifest><spine` + spineAttrs + `>` + spine + `</spine></package>`
}

func xhtml(body string) string {
	return "<html><body>" + body + "</body></html>"
}

func navDocument(items string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><body><nav epub:type="toc"><ol>` + items + `</ol></nav></body></html>`
}

func ncxDocument(points string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><ncx xmlns="http://www.daisy.org/z3986/2005/ncx/"><navMap>` + points + `</navMap></ncx>`
}

func spineManifest(n int, media string) (manifest, spine string) {
	for i := 0; i < n; i++ {
		kind, name := "application/xhtml+xml", fmt.Sprintf("unit%d.xhtml", i)
		if media != "" && i == 1 {
			kind, name = media, "unit1.bin"
		}
		manifest += fmt.Sprintf(`<item id="u%d" href="%s" media-type="%s"/>`, i, name, kind)
		spine += fmt.Sprintf(`<itemref idref="u%d"/>`, i)
	}
	return manifest, spine
}

func mustGet(t *testing.T, req Request) Result {
	t.Helper()
	r, err := Get(context.Background(), req)
	if err != nil {
		t.Fatalf("status %s: %v", r.Status, err)
	}
	return r
}

func mustFail(t *testing.T, req Request, status string) Result {
	t.Helper()
	r, err := Get(context.Background(), req)
	if err == nil || r.Status != status || r.Text != "" || r.Continuation != "" || r.Locator != nil || len(r.Navigation) != 0 || r.RangeStatus != "" || r.RangeEnd != nil || r.EndOfUnit || r.EndOfSource {
		t.Fatalf("status %q err %v text %q continuation %t nav %d range %q", r.Status, err, r.Text, r.Continuation != "", len(r.Navigation), r.RangeStatus)
	}
	return r
}

func chapterFiles() map[string]string {
	manifest, spine := spineManifest(3, "")
	manifest += `<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>`
	return map[string]string{
		"META-INF/container.xml": containerXML(),
		"book.opf":               packageDoc(manifest, "", spine),
		"unit0.xhtml":            xhtml(`<h1 id="a">Alpha</h1><p>ALPHAWORD 日本語.</p><img src="pic.png" alt="A picture"/><p>AFTERIMAGE.</p><h2 id="a1">Also</h2><p>NESTEDWORD.</p><h1 id="b">Beta</h1><p>BETAWORD.</p>`),
		"unit1.xhtml":            xhtml(`<p>MIDDLEWORD across.</p>`),
		"unit2.xhtml":            xhtml(`<h1 id="c">Gamma</h1><p>GAMMAWORD.</p>`),
		"nav.xhtml":              navDocument(`<li><a href="unit0.xhtml#a">第一章  Alpha</a><ol><li><a href="unit0.xhtml#a1">第一章  Alpha</a></li></ol></li><li><a href="unit0.xhtml#b">Beta</a></li><li><a href="unit2.xhtml#c">  café` + "\u00a0tea  " + `</a></li>`),
	}
}

func readNav(t *testing.T, req Request, entry int) (string, []Result) {
	t.Helper()
	req.Unit, req.Offset, req.Cursor, req.UnitsOnly = entry, 0, "", false
	var got []Result
	var text strings.Builder
	for range 12 {
		r := mustGet(t, req)
		got = append(got, r)
		text.WriteString(r.Text)
		if r.Continuation == "" {
			return text.String(), got
		}
		req.Cursor, req.Unit, req.Offset, req.Navigation = r.Continuation, 0, 0, ""
	}
	t.Fatal("navigation read did not reach its declared end")
	return "", nil
}

func TestDeclaredNavigationSelectsChapters(t *testing.T) {
	req := publishEPUB(t, chapterFiles())
	listing := req
	listing.UnitsOnly = true
	listed := mustGet(t, listing)
	if listed.Status != "units_available" || listed.Representation != "epub-nav/1" || listed.Extractor != NavExtractor || len(listed.Units) != 3 || listed.Text != "" || listed.Locator != nil {
		t.Fatalf("listing status %s units %d", listed.Status, len(listed.Units))
	}
	if listed.Coverage == nil || listed.Coverage.Navigation != "nav-doc" || listed.Coverage.NavigationConflict || listed.Coverage.UnresolvedEntries != 0 {
		t.Fatalf("coverage %#v", listed.Coverage)
	}
	if len(listed.Navigation) != 1 || listed.Navigation[0].Evidence != "nav-doc" || listed.Navigation[0].Path != "nav.xhtml" || len(listed.Navigation[0].Entries) != 4 {
		t.Fatalf("navigation %#v", listed.Navigation)
	}
	entries := listed.Navigation[0].Entries
	if entries[0].Label != "第一章 Alpha" || entries[1].Label != "第一章 Alpha" || entries[0].Order == entries[1].Order || entries[2].Label != "Beta" || entries[3].Label != "café tea" {
		t.Fatalf("labels %#v", entries)
	}
	if entries[0].Parent != nil || entries[0].Level != 1 || entries[1].Parent == nil || *entries[1].Parent != 0 || entries[1].Level != 2 || entries[2].Level != 1 {
		t.Fatalf("hierarchy %#v %#v", entries[0], entries[1])
	}
	for i, e := range entries {
		if e.Status != "resolved" || e.Evidence != "nav-doc" || e.Range == nil || e.Unit == nil || e.Offset == nil {
			t.Fatalf("entry %d %#v", i, e)
		}
	}
	if entries[0].Range.Status != "bounded" || entries[2].Range.Status != "bounded" || entries[3].Range.Status != "end_of_source" || !entries[3].Range.EndAtSource {
		t.Fatalf("ranges %#v %#v %#v", entries[0].Range, entries[2].Range, entries[3].Range)
	}
	// The parent range includes its nested entry and stops at the next sibling,
	// which is still inside the same spine file.
	if entries[0].Range.EndUnit != *entries[2].Unit || entries[0].Range.EndOffset != *entries[2].Offset || *entries[0].Unit != *entries[2].Unit {
		t.Fatalf("alpha should end at beta inside one file: %#v %#v", entries[0].Range, entries[2])
	}
	// Beta starts in unit 0 and ends at Gamma in unit 2, so it covers unit 1.
	if *entries[2].Unit != 0 || entries[2].Range.EndUnit != 2 || *entries[3].Unit != 2 {
		t.Fatalf("beta span %#v gamma %#v", entries[2], entries[3])
	}

	alpha, pieces := readNav(t, req, 0)
	if len(pieces) != 1 || pieces[0].RangeStatus != "range_end" || pieces[0].Continuation != "" || pieces[0].EndOfUnit || pieces[0].EndOfSource || pieces[0].Status != "complete_range" {
		t.Fatalf("alpha delivery %#v end %t unit %t", pieces[0].RangeStatus, pieces[0].EndOfSource, pieces[0].EndOfUnit)
	}
	if !strings.Contains(alpha, "ALPHAWORD") || !strings.Contains(alpha, "日本語") || !strings.Contains(alpha, "NESTEDWORD") || strings.Contains(alpha, "BETAWORD") {
		t.Fatalf("alpha text %q", alpha)
	}
	if pieces[0].Locator == nil || pieces[0].Locator.Unit != 0 || pieces[0].Locator.Start != *entries[0].Offset || pieces[0].Locator.OffsetUnit != "utf8_bytes" || pieces[0].Locator.Extractor != NavExtractor {
		t.Fatalf("alpha locator %#v", pieces[0].Locator)
	}
	if pieces[0].RangeEnd == nil || pieces[0].RangeEnd.Unit != *entries[2].Unit || pieces[0].RangeEnd.Start != *entries[2].Offset {
		t.Fatalf("alpha range end %#v", pieces[0].RangeEnd)
	}
	if len(pieces[0].Navigation) != 1 || len(pieces[0].Navigation[0].Entries) != 1 || pieces[0].Navigation[0].Entries[0].Order != 0 {
		t.Fatalf("selected entry %#v", pieces[0].Navigation)
	}
	if len(pieces[0].Gaps) != 1 || pieces[0].Gaps[0].Element != "img" || pieces[0].Gaps[0].Reason != "image_pixels_not_text" || pieces[0].Gaps[0].Image == nil || pieces[0].Gaps[0].Image.Alt == nil || *pieces[0].Gaps[0].Image.Alt != "A picture" {
		t.Fatalf("gap %#v", pieces[0].Gaps)
	}
	again := mustGet(t, req)
	left, _ := json.Marshal(pieces[0])
	right, _ := json.Marshal(again)
	if !bytes.Equal(left, right) {
		t.Fatal("repeating a navigation read changed the result")
	}

	nested, _ := readNav(t, req, 1)
	if strings.Contains(nested, "ALPHAWORD") || !strings.Contains(nested, "NESTEDWORD") || strings.Contains(nested, "BETAWORD") {
		t.Fatalf("nested text %q", nested)
	}

	// A positive text offset is relative to the declared entry, whose start is
	// not byte zero of the spine file.
	beta := req
	beta.Unit = 2
	whole := mustGet(t, beta)
	if whole.Locator == nil || whole.Locator.Start == 0 || !strings.HasPrefix(whole.Text, "Beta") || !whole.EndOfUnit || whole.RangeStatus != "within_range" || strings.Contains(whole.Text, "MIDDLEWORD") || strings.Contains(whole.Text, "GAMMAWORD") {
		t.Fatalf("beta first unit %#v %q endunit %t range %s", whole.Locator, whole.Text, whole.EndOfUnit, whole.RangeStatus)
	}
	beta.Offset = 2
	shifted := mustGet(t, beta)
	if shifted.Locator == nil || shifted.Locator.Start != whole.Locator.Start+2 || shifted.Text != whole.Text[2:] {
		t.Fatalf("relative offset %#v %q", shifted.Locator, shifted.Text)
	}
	beta.Offset = 0
	beta.MaxBytes = 5
	var chunked strings.Builder
	for range 20 {
		r := mustGet(t, beta)
		if !utf8.ValidString(r.Text) || len(r.Text) > 5 {
			t.Fatalf("chunk %q", r.Text)
		}
		chunked.WriteString(r.Text)
		if r.EndOfUnit {
			break
		}
		if r.RangeStatus != "within_range" || r.Continuation == "" {
			t.Fatalf("truncated beta %s", r.RangeStatus)
		}
		beta.Cursor, beta.Unit, beta.Offset = r.Continuation, 0, 0
	}
	if chunked.String() != whole.Text {
		t.Fatalf("chunks %q whole %q", chunked.String(), whole.Text)
	}

	betaAll, betaParts := readNav(t, req, 2)
	if len(betaParts) != 2 || !betaParts[0].EndOfUnit || betaParts[0].RangeStatus != "within_range" || betaParts[0].EndOfSource || betaParts[1].RangeStatus != "range_end" || betaParts[1].EndOfSource || !betaParts[1].EndOfUnit {
		t.Fatalf("cross-file parts %d", len(betaParts))
	}
	if !strings.Contains(betaParts[0].Text, "BETAWORD") || strings.Contains(betaParts[0].Text, "MIDDLEWORD") || !strings.Contains(betaParts[1].Text, "MIDDLEWORD") || strings.Contains(betaAll, "GAMMAWORD") || strings.Contains(betaAll, "ALPHAWORD") {
		t.Fatalf("beta text %q | %q", betaParts[0].Text, betaParts[1].Text)
	}
	continued, err := parseCursor(betaParts[0].Continuation)
	if err != nil || continued.Part != "nav-doc:2" || continued.Unit != 1 || continued.Offset != 0 || continued.Representation != "epub-nav/1" {
		t.Fatalf("continuation %#v %v", continued, err)
	}

	gamma, gammaParts := readNav(t, req, 3)
	if len(gammaParts) != 1 || !strings.Contains(gamma, "GAMMAWORD") || strings.Contains(gamma, "MIDDLEWORD") || gammaParts[0].RangeStatus != "range_end" || !gammaParts[0].EndOfSource || !gammaParts[0].EndOfUnit {
		t.Fatalf("gamma %q %#v", gamma, gammaParts)
	}

	// The same bytes read as a spine document still contain every chapter.
	spine := req
	spine.Representation, spine.Unit = "epub-text/2", 0
	plain := mustGet(t, spine)
	if plain.RangeStatus != "" || len(plain.Navigation) != 0 || !strings.Contains(plain.Text, "ALPHAWORD") || !strings.Contains(plain.Text, "BETAWORD") {
		t.Fatalf("spine read %q %s", plain.Text, plain.RangeStatus)
	}
	bare := publishEPUB(t, func() map[string]string {
		files := chapterFiles()
		delete(files, "nav.xhtml")
		manifest, spine := spineManifest(3, "")
		files["book.opf"] = packageDoc(manifest, "", spine)
		return files
	}())
	bare.Representation = "epub-text/2"
	bare.UnitsOnly = true
	spine.UnitsOnly = true
	withNav := mustGet(t, spine)
	without := mustGet(t, bare)
	if withNav.RepresentationID == "" || withNav.RepresentationID != without.RepresentationID {
		t.Fatalf("nav document changed spine text identity %s %s", withNav.RepresentationID, without.RepresentationID)
	}
	structured := req
	structured.Representation = "epub-structure/1"
	parts := mustGet(t, structured)
	headings := 0
	for _, p := range parts.Parts {
		if p.Kind == "h1" || p.Kind == "h2" {
			t.Fatalf("part kind %s preserves a heading level the structure representation does not record", p.Kind)
		}
		if p.Kind == "heading" {
			headings++
		}
	}
	if headings < 3 {
		t.Fatalf("headings %d", headings)
	}

	past := req
	past.Unit, past.Offset = 2, 8000
	mustFail(t, past, "invalid_request")
	missing := req
	missing.Unit = 9
	mustFail(t, missing, "invalid_request")
	stale := req
	stale.Cursor = encodeCursor(cursor{SchemaVersion: 1, AssetID: req.AssetID, Extractor: NavExtractor, Representation: "epub-nav/1", RepresentationID: "sha256:" + strings.Repeat("ab", 32), Part: "nav-doc:0"})
	mustFail(t, stale, "stale_reference")
	changed := req
	changed.UnitsOnly = true
	path := filepath.Join(req.Root, "book.epub")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body[len(body)-1] ^= 1
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	mustFail(t, changed, "changed_source")
}

func TestNavigationChunkStaysInsideTheChapter(t *testing.T) {
	req := publishEPUB(t, chapterFiles())
	req.MaxBytes = 8
	text, parts := readNav(t, req, 0)
	if len(parts) < 2 || !strings.Contains(text, "日本語") || strings.Contains(text, "BETAWORD") {
		t.Fatalf("chunked alpha %q parts %d", text, len(parts))
	}
	for _, p := range parts[:len(parts)-1] {
		if p.RangeStatus != "within_range" || p.Continuation == "" {
			t.Fatalf("middle chunk %s", p.RangeStatus)
		}
	}
	if parts[len(parts)-1].RangeStatus != "range_end" || parts[len(parts)-1].Continuation != "" {
		t.Fatal("final chunk left the chapter open")
	}
}

func TestNavigationSourcesStayDistinct(t *testing.T) {
	manifest, spine := spineManifest(3, "")
	manifest += `<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/><item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>`
	nav := navDocument(`<li><a href="unit0.xhtml#a">NAV Alpha</a><ol><li><a href="unit0.xhtml#a1">NAV nested</a></li></ol></li><li><a href="unit0.xhtml#b">NAV Beta</a></li><li><a href="unit2.xhtml#c">NAV Gamma</a></li>`)
	ncx := ncxDocument(`<navPoint id="a" playOrder="4"><navLabel><text>NCX Alpha</text></navLabel><content src="unit0.xhtml#a"/><navPoint id="a1" playOrder="1"><navLabel><text>NCX nested</text></navLabel><content src="unit0.xhtml#a1"/></navPoint></navPoint><navPoint id="b" playOrder="2"><navLabel><text>NCX Beta</text></navLabel><content src="unit0.xhtml#b"/></navPoint><navPoint id="c" playOrder="3"><navLabel><text>NCX Gamma</text></navLabel><content src="unit2.xhtml#c"/></navPoint>`)
	files := chapterFiles()
	files["book.opf"] = packageDoc(manifest, ` toc="ncx"`, spine)
	files["nav.xhtml"] = nav
	files["toc.ncx"] = ncx
	req := publishEPUB(t, files)
	req.UnitsOnly = true
	listed := mustGet(t, req)
	if listed.Coverage == nil || listed.Coverage.Navigation != "nav-doc+ncx" || listed.Coverage.NavigationConflict || len(listed.Navigation) != 2 {
		t.Fatalf("coverage %#v docs %d", listed.Coverage, len(listed.Navigation))
	}
	if listed.Navigation[0].Evidence != "nav-doc" || listed.Navigation[1].Evidence != "ncx" || listed.Navigation[1].Entries[0].Label != "NCX Alpha" || listed.Navigation[1].Entries[0].PlayOrder == nil || *listed.Navigation[1].Entries[0].PlayOrder != 4 {
		t.Fatalf("sources were reordered or preferred %#v", listed.Navigation)
	}
	if listed.Navigation[1].Entries[1].Level != 2 || listed.Navigation[1].Entries[1].Parent == nil || *listed.Navigation[1].Entries[1].Parent != 0 {
		t.Fatalf("ncx nesting %#v", listed.Navigation[1].Entries[1])
	}
	choice := req
	choice.UnitsOnly = false
	mustFail(t, choice, "navigation_choice_required")
	choice.Navigation = "nav-doc"
	navText, _ := readNav(t, choice, 0)
	choice.Navigation = "ncx"
	ncxText, _ := readNav(t, choice, 0)
	if !strings.Contains(navText, "ALPHAWORD") || navText != ncxText {
		t.Fatalf("same target disagreed %q %q", navText, ncxText)
	}
	// A target disagreement is explicit and still requires the caller to choose.
	files["toc.ncx"] = ncxDocument(`<navPoint id="b" playOrder="1"><navLabel><text>NCX Beta</text></navLabel><content src="unit0.xhtml#b"/></navPoint>`)
	conflicted := publishEPUB(t, files)
	conflicted.UnitsOnly = true
	got := mustGet(t, conflicted)
	if got.Coverage == nil || !got.Coverage.NavigationConflict {
		t.Fatalf("conflict not recorded %#v", got.Coverage)
	}
	conflicted.UnitsOnly = false
	mustFail(t, conflicted, "navigation_choice_required")
	conflicted.Navigation = "ncx"
	beta, _ := readNav(t, conflicted, 0)
	if strings.Contains(beta, "ALPHAWORD") || !strings.Contains(beta, "BETAWORD") {
		t.Fatalf("chosen ncx %q", beta)
	}
}

func TestNavigationRefusesUnresolvedAndRemoteTargets(t *testing.T) {
	manifest, spine := spineManifest(1, "")
	manifest += `<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>`
	files := map[string]string{
		"META-INF/container.xml": containerXML(),
		"book.opf":               packageDoc(manifest, "", spine),
		"unit0.xhtml":            xhtml(`<h1 id="a">Alpha</h1><p>ALPHAWORD.</p><h1 id="b">Beta</h1><p>BETAWORD.</p>`),
		"nav.xhtml":              navDocument(`<li><a href="unit0.xhtml#a">Known</a></li><li><a href="unit0.xhtml#missing">Missing</a></li><li><a href="https://example.invalid/chapter">Away</a></li><li><a href="unit0.xhtml?x=1">Query</a></li><li><span>Bare</span></li>`),
	}
	req := publishEPUB(t, files)
	req.UnitsOnly = true
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	listed, err := Get(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]NavEntry{}
	for _, e := range listed.Navigation[0].Entries {
		got[e.Label] = e
	}
	if got["Known"].Range == nil || got["Known"].Range.Status != "unresolved" {
		t.Fatalf("known range should be unresolved because the next entry has no offset: %#v", got["Known"].Range)
	}
	if got["Missing"].Status != "unresolved_anchor" || got["Missing"].Unit == nil || got["Missing"].Offset != nil || got["Missing"].Range != nil {
		t.Fatalf("missing %#v", got["Missing"])
	}
	if got["Away"].Status != "external_not_fetched" || got["Query"].Status != "invalid_reference" || got["Bare"].Status != "no_href" {
		t.Fatalf("statuses %#v %#v %#v", got["Away"], got["Query"], got["Bare"])
	}
	req.UnitsOnly = false
	mustFail(t, req, "unresolved_navigation")
	remote := publishEPUB(t, map[string]string{
		"META-INF/container.xml": containerXML(),
		"book.opf":               packageDoc(manifest+`<item id="far" href="https://example.invalid/nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>`, "", spine),
		"unit0.xhtml":            xhtml(`<p id="a">Hi.</p>`),
		"nav.xhtml":              navDocument(`<li><a href="unit0.xhtml">Local</a></li>`),
	})
	remote.UnitsOnly = true
	// Two nav documents, one of them remote. The remote item is recorded and
	// not fetched; both are withheld rather than silently keeping the local one.
	remoteListed, err := Get(ctx, remote)
	if err != nil || len(remoteListed.Navigation) != 2 {
		t.Fatalf("%v %#v", err, remoteListed.Navigation)
	}
	problems := map[string]bool{}
	for _, doc := range remoteListed.Navigation {
		problems[doc.Problem] = true
		if doc.Problem == "" && len(doc.Entries) > 0 {
			t.Fatal("ambiguous navigation still exposed entries")
		}
	}
	if !problems["external_not_fetched"] || !problems["multiple_nav_documents"] {
		t.Fatalf("problems %#v", remoteListed.Navigation)
	}
}

func TestAbsentOversizedAndUnreadableNavigation(t *testing.T) {
	absent := fixture(t, []string{"<p>Plain spine.</p>"}, nil)
	absent.Representation = "epub-nav/1"
	absent.UnitsOnly = true
	listed := mustGet(t, absent)
	if listed.Coverage == nil || listed.Coverage.Navigation != "absent" || listed.Coverage.Text != "no_navigation" || len(listed.Navigation) != 0 || len(listed.Units) != 1 {
		t.Fatalf("absent %#v", listed.Coverage)
	}
	absent.UnitsOnly = false
	mustFail(t, absent, "no_navigation")

	manifest, spine := spineManifest(1, "")
	manifest += `<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>`
	label := publishEPUB(t, map[string]string{
		"META-INF/container.xml": containerXML(),
		"book.opf":               packageDoc(manifest, "", spine),
		"unit0.xhtml":            xhtml("<p>Short.</p>"),
		"nav.xhtml":              navDocument(`<li><a href="unit0.xhtml">` + strings.Repeat("A", 257) + `</a></li>`),
	})
	mustFail(t, label, "budget_exceeded")

	var items strings.Builder
	for i := range 1025 {
		fmt.Fprintf(&items, `<li><a href="unit0.xhtml">L%d</a></li>`, i)
	}
	many := publishEPUB(t, map[string]string{
		"META-INF/container.xml": containerXML(),
		"book.opf":               packageDoc(manifest, "", spine),
		"unit0.xhtml":            xhtml("<p>Short.</p>"),
		"nav.xhtml":              navDocument(items.String()),
	})
	mustFail(t, many, "budget_exceeded")

	nested := ""
	for range 65 {
		nested += `<li><a href="unit0.xhtml">L</a><ol>`
	}
	for range 65 {
		nested += `</ol></li>`
	}
	deep := publishEPUB(t, map[string]string{
		"META-INF/container.xml": containerXML(),
		"book.opf":               packageDoc(manifest, "", spine),
		"unit0.xhtml":            xhtml("<p>Short.</p>"),
		"nav.xhtml":              navDocument(nested),
	})
	mustFail(t, deep, "budget_exceeded")

	var huge strings.Builder
	huge.WriteString(`<?xml version="1.0" encoding="UTF-8"?><html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><body><nav epub:type="toc"><ol>`)
	for huge.Len() <= 2<<20 {
		huge.WriteString(`<li><a href="unit0.xhtml">A</a></li>`)
	}
	huge.WriteString(`</ol></nav></body></html>`)
	over := publishEPUB(t, map[string]string{
		"META-INF/container.xml": containerXML(),
		"book.opf":               packageDoc(manifest, "", spine),
		"unit0.xhtml":            xhtml("<p>Short.</p>"),
		"nav.xhtml":              huge.String(),
	})
	over.UnitsOnly = true
	overListed := mustGet(t, over)
	if len(overListed.Navigation) != 1 || overListed.Navigation[0].Problem != "over_budget" || len(overListed.Navigation[0].Entries) != 0 {
		t.Fatalf("over budget %#v", overListed.Navigation)
	}
	over.UnitsOnly = false
	mustFail(t, over, "no_navigation")

	backwards := publishEPUB(t, map[string]string{
		"META-INF/container.xml": containerXML(),
		"book.opf":               packageDoc(manifest, "", spine),
		"unit0.xhtml":            xhtml(`<h1 id="a">Alpha</h1><p>ALPHAWORD.</p><h1 id="b">Beta</h1>`),
		"nav.xhtml":              navDocument(`<li><a href="unit0.xhtml#b">Later</a></li><li><a href="unit0.xhtml#a">Earlier</a></li>`),
	})
	backwards.UnitsOnly = true
	order := mustGet(t, backwards)
	if order.Navigation[0].Entries[0].Range == nil || order.Navigation[0].Entries[0].Range.Status != "not_after_start" {
		t.Fatalf("order %#v", order.Navigation[0].Entries[0].Range)
	}
	backwards.UnitsOnly = false
	mustFail(t, backwards, "unresolved_navigation")

	pages := publishEPUB(t, map[string]string{
		"META-INF/container.xml": containerXML(),
		"book.opf":               packageDoc(manifest, "", spine),
		"unit0.xhtml":            xhtml(`<p id="a">Only.</p>`),
		"nav.xhtml":              `<?xml version="1.0" encoding="UTF-8"?><html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><body><nav epub:type="page-list"><ol><li><a href="unit0.xhtml">Page</a></li></ol></nav><nav epub:type="toc"><ol><li><a href="unit0.xhtml#a">Only</a></li></ol></nav></body></html>`,
	})
	pages.UnitsOnly = true
	pageList := mustGet(t, pages)
	if len(pageList.Navigation) != 1 || len(pageList.Navigation[0].Entries) != 1 || pageList.Navigation[0].Entries[0].Label != "Only" {
		t.Fatalf("page list leaked %#v", pageList.Navigation)
	}
}

func TestNCXOnlyAndUnsupportedUnitInsideARange(t *testing.T) {
	manifest, spine := spineManifest(1, "")
	manifest += `<item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>`
	points := `<navPoint id="a" playOrder="2"><navLabel><text>  First  label </text></navLabel><navLabel><text>Alternate</text></navLabel><content src="unit0.xhtml#a"/></navPoint><navPoint id="b" playOrder="1"><navLabel><text>Second</text></navLabel><content src="unit0.xhtml#b"/></navPoint>`
	req := publishEPUB(t, map[string]string{
		"META-INF/container.xml": containerXML(),
		"book.opf":               packageDoc(manifest, ` toc="ncx"`, spine),
		"unit0.xhtml":            xhtml(`<h1 id="a">Alpha</h1><p>ALPHAWORD.</p><h1 id="b">Beta</h1><p>BETAWORD.</p>`),
		"toc.ncx":                ncxDocument(points),
	})
	req.UnitsOnly = true
	listed := mustGet(t, req)
	if listed.Coverage == nil || listed.Coverage.Navigation != "ncx" || len(listed.Navigation) != 1 || listed.Navigation[0].Entries[0].Label != "First label" || listed.Navigation[0].Entries[1].Label != "Second" {
		t.Fatalf("ncx order %#v", listed.Navigation)
	}
	if len(listed.Navigation[0].Entries[0].AltLabels) != 1 || listed.Navigation[0].Entries[0].AltLabels[0] != "Alternate" || *listed.Navigation[0].Entries[0].PlayOrder != 2 {
		t.Fatalf("ncx evidence %#v", listed.Navigation[0].Entries[0])
	}
	req.UnitsOnly = false
	text, _ := readNav(t, req, 0)
	if !strings.Contains(text, "ALPHAWORD") || strings.Contains(text, "BETAWORD") {
		t.Fatalf("ncx chapter %q", text)
	}
	bad := req
	bad.Navigation = "nav-doc"
	mustFail(t, bad, "no_navigation")
	badOrder := publishEPUB(t, map[string]string{
		"META-INF/container.xml": containerXML(),
		"book.opf":               packageDoc(manifest, ` toc="ncx"`, spine),
		"unit0.xhtml":            xhtml(`<p id="a">Hi.</p>`),
		"toc.ncx":                ncxDocument(`<navPoint id="a" playOrder="01"><navLabel><text>No</text></navLabel><content src="unit0.xhtml"/></navPoint>`),
	})
	mustFail(t, badOrder, "budget_exceeded")

	crossed := map[string]string{
		"META-INF/container.xml": containerXML(),
		"book.opf":               packageDoc(manifest, "", spine),
		"unit0.xhtml":            xhtml(`<p id="a">ALPHAWORD.</p>`),
		"unit1.bin":              "not a document",
		"unit2.xhtml":            xhtml(`<p id="c">GAMMAWORD.</p>`),
		"nav.xhtml":              navDocument(`<li><a href="unit0.xhtml#a">Span</a></li><li><a href="unit2.xhtml#c">After</a></li>`),
	}
	// spineManifest writes unit1.xhtml into the manifest only when media is empty.
	// Rebuild the manifest explicitly so the binary member is the spine item.
	crossed["book.opf"] = packageDoc(`<item id="u0" href="unit0.xhtml" media-type="application/xhtml+xml"/><item id="u1" href="unit1.bin" media-type="application/octet-stream"/><item id="u2" href="unit2.xhtml" media-type="application/xhtml+xml"/><item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>`, "", `<itemref idref="u0"/><itemref idref="u1"/><itemref idref="u2"/>`)
	span := publishEPUB(t, crossed)
	first := mustGet(t, span)
	if first.RangeStatus != "within_range" || !first.EndOfUnit || first.Continuation == "" || strings.Contains(first.Text, "GAMMAWORD") {
		t.Fatalf("span start %#v %q", first.RangeStatus, first.Text)
	}
	span.Cursor, span.Unit = first.Continuation, 0
	blocked, err := Get(context.Background(), span)
	if err == nil || blocked.Status != "unsupported_extraction" || blocked.Text != "" || blocked.Continuation != "" || strings.Contains(blocked.Text, "GAMMAWORD") {
		t.Fatalf("unsupported unit was skipped: %s %v %q", blocked.Status, err, blocked.Text)
	}
}

func TestNavigationValidationAndFragmentForms(t *testing.T) {
	req := publishEPUB(t, map[string]string{
		"META-INF/container.xml": containerXML(),
		"book.opf":               packageDoc(`<item id="u0" href="unit0.xhtml" media-type="application/xhtml+xml"/><item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>`, "", `<itemref idref="u0"/>`),
		"unit0.xhtml":            xhtml(`<p>Intro.</p><p id="c 1">ALPHAWORD.</p><p id="tail">Tail.</p>`),
		"nav.xhtml":              navDocument(`<li><a href="unit0.xhtml">Whole</a></li><li><a href="unit0.xhtml#c%201">Spaced</a></li><li><a href="unit0.xhtml#tail">End</a></li>`),
	})
	req.UnitsOnly = true
	listed := mustGet(t, req)
	if listed.Navigation[0].Entries[0].Status != "resolved" || *listed.Navigation[0].Entries[0].Offset != 0 || listed.Navigation[0].Entries[1].Status != "resolved" || listed.Navigation[0].Entries[1].Part != "id:c 1" {
		t.Fatalf("fragments %#v", listed.Navigation[0].Entries)
	}
	text, _ := readNav(t, req, 1)
	if !strings.Contains(text, "ALPHAWORD") || strings.Contains(text, "Tail") {
		t.Fatalf("fragment range %q", text)
	}
	bad := req
	bad.Navigation = "toc"
	if err := bad.Validate(); err == nil {
		t.Fatal("accepted an unknown navigation source")
	}
	bad = req
	bad.Navigation = "nav-doc"
	bad.Representation = "epub-text/2"
	if err := bad.Validate(); err == nil {
		t.Fatal("accepted navigation on a spine representation")
	}
	bad = req
	bad.Navigation = "nav-doc"
	bad.Cursor = "abc"
	if err := bad.Validate(); err == nil {
		t.Fatal("accepted navigation combined with a cursor")
	}
}
