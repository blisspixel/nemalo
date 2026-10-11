package content

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

// An illustrated chapter like those in Project Gutenberg EPUBs (issue 5).
const illustrated = `<h1>CHAPTER I</h1><div class="fig"><img src="i1.png" alt="The old sea-dog"/></div>` +
	`<p>Squire Trelawney asked me to write it down.</p><table><tr><td>layout</td></tr></table>` +
	`<p>He was a tall, strong man.</p><p><img src="i2.png"/><img src="i3.png" alt=""/></p><p>End.</p>`

func textV2(t *testing.T, bodies []string, mutate func(map[string]string)) Request {
	t.Helper()
	req := fixture(t, bodies, mutate)
	req.Representation = "epub-text/2"
	return req
}

func TestTextV2ReadsIllustratedDocumentsWithLocatedGaps(t *testing.T) {
	req := textV2(t, []string{illustrated}, nil)
	req.MaxBytes = 65536
	r, err := Get(context.Background(), req)
	if err != nil || r.Status != "complete_range" || r.Representation != "epub-text/2" || r.Extractor != TextExtractorV2 || !r.EndOfSource {
		t.Fatal(r, err)
	}
	want := "CHAPTER I\nSquire Trelawney asked me to write it down.\nHe was a tall, strong man.\nEnd.\n"
	if r.Text != want {
		t.Fatalf("text %q", r.Text)
	}
	if len(r.Gaps) != 4 || r.Coverage == nil || r.Coverage.Text != "known_extraction_gaps" || len(r.Coverage.KnownGaps) != 4 {
		t.Fatal("gaps not disclosed", r.Gaps, r.Coverage)
	}
	first, table, absent, empty := r.Gaps[0], r.Gaps[1], r.Gaps[2], r.Gaps[3]
	if first.Element != "img" || first.Reason != "image_pixels_not_text" || first.Image == nil || first.Image.Alt == nil || *first.Image.Alt != "The old sea-dog" || first.Image.Source != "i1.png" {
		t.Fatal("image gap", first)
	}
	// Each gap's offset is the text position where the element occurred.
	if want[:first.Offset] != "CHAPTER I\n" || want[:table.Offset] != "CHAPTER I\nSquire Trelawney asked me to write it down.\n" || table.Element != "table" || table.Reason != "table_relationships_not_extracted" {
		t.Fatal("gap offsets", first.Offset, table)
	}
	// Absent alt text is null, not an empty string or a generated description.
	if absent.Image == nil || absent.Image.Alt != nil || empty.Image == nil || empty.Image.Alt == nil || *empty.Image.Alt != "" {
		t.Fatal("alt provenance", absent.Image, empty.Image)
	}
	if strings.Contains(r.Text, "layout") {
		t.Fatal("table text flattened into prose")
	}
	b, _ := json.Marshal(r.Gaps[2])
	if !strings.Contains(string(b), `"alt":null`) {
		t.Fatal("absent alt not serialized as null", string(b))
	}
}

func TestTextV2MatchesStructuredTextAndUsesItsPartIDs(t *testing.T) {
	req := textV2(t, []string{illustrated, "<p>Two</p>"}, nil)
	req.MaxBytes = 65536
	plain, err := Get(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	req.Representation = "epub-structure/1"
	structured, err := Get(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if plain.Text != structured.Text || plain.RepresentationID == structured.RepresentationID {
		t.Fatal("text/2 must share structure/1 text but not its representation identity")
	}
	for _, g := range plain.Gaps {
		found := false
		for _, p := range structured.Parts {
			found = found || (p.ID == g.Part && p.Gap == g.Reason && p.Start == g.Offset)
		}
		if !found {
			t.Fatal("gap does not identify a structured part", g)
		}
	}
}

func TestTextV2ChunksDiscloseEachGapOnce(t *testing.T) {
	req := textV2(t, []string{illustrated, `<img src="cover.png" alt="Cover"/>`, "<p>Après 日本語😀</p>"}, nil)
	req.MaxBytes = 7
	var text strings.Builder
	seen := map[string]int{}
	calls := 0
	for {
		r, err := Get(context.Background(), req)
		if err != nil || !utf8.ValidString(r.Text) || len(r.Text) > req.MaxBytes {
			t.Fatal(r, err)
		}
		again, _ := Get(context.Background(), req)
		b1, _ := json.Marshal(r)
		b2, _ := json.Marshal(again)
		if !bytes.Equal(b1, b2) {
			t.Fatal("retry is not deterministic")
		}
		text.WriteString(r.Text)
		for _, g := range r.Gaps {
			if g.Offset < r.Locator.Start || g.Offset > r.Locator.End {
				t.Fatal("gap outside delivered range", g, r.Locator)
			}
			seen[string(rune('0'+r.Locator.Unit))+g.Part]++
		}
		calls++
		if r.EndOfSource {
			if r.Continuation != "" {
				t.Fatal("continuation after end of source")
			}
			break
		}
		if calls > 200 || r.Continuation == "" {
			t.Fatal("no progress", r)
		}
		req.Cursor, req.Unit, req.Offset = r.Continuation, 0, 0
		req.Representation = "" // A continuation selects its own representation.
	}
	if len(seen) != 5 {
		t.Fatal("gap count across ranges", seen)
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatal("gap disclosed more than once", id, n)
		}
	}
	if !strings.Contains(text.String(), "He was a tall, strong man.\nEnd.\nAprès 日本語😀\n") {
		t.Fatalf("reassembled text %q", text.String())
	}
}

func TestTextV2ImageOnlyUnitIsEmptyTextWithGapsNotUnsupported(t *testing.T) {
	req := textV2(t, []string{`<div><img src="cover.png" alt="Cover"/></div>`, "<p>Next</p>"}, nil)
	req.UnitsOnly = true
	r, err := Get(context.Background(), req)
	if err != nil || len(r.UnsupportedUnits) != 0 || r.Units[0].Extraction != "supported_text_with_gaps" || r.Units[0].TextBytes != 0 || r.Units[0].KnownGaps != 1 || r.Units[1].Extraction != "supported_plain_text" {
		t.Fatal(r, err)
	}
	req.UnitsOnly = false
	r, err = Get(context.Background(), req)
	if err != nil || r.Status != "complete_range" || r.Text != "" || len(r.Gaps) != 1 || r.Coverage.Text != "known_extraction_gaps" || !r.EndOfUnit || r.EndOfSource || r.Continuation == "" {
		t.Fatal("image-only unit must disclose its omission and continue", r, err)
	}
}

func TestTextV2UnsupportedDocumentStaysExplicitPerUnit(t *testing.T) {
	req := textV2(t, []string{"<p>One</p>", "placeholder", "<p>Three</p>"}, func(files map[string]string) {
		// An SVG reading-order document cannot be represented as XHTML text.
		files["book.opf"] = strings.Replace(files["book.opf"], `href="unit1.xhtml" media-type="application/xhtml+xml"`, `href="unit1.xhtml" media-type="image/svg+xml"`, 1)
	})
	req.UnitsOnly = true
	r, err := Get(context.Background(), req)
	if err != nil || len(r.UnsupportedUnits) != 1 || r.UnsupportedUnits[0] != 1 || r.Units[1].Extraction != "unsupported_document" || r.Units[1].TextBytes != 0 {
		t.Fatal(r, err)
	}
	req.UnitsOnly, req.MaxBytes = false, 64
	r, err = Get(context.Background(), req)
	if err != nil || r.Text != "One\n" {
		t.Fatal(r, err)
	}
	// The continuation stops at the unsupported unit instead of skipping it.
	next := req
	next.Cursor, next.Representation = r.Continuation, ""
	r, err = Get(context.Background(), next)
	if err == nil || r.Status != "unsupported_extraction" || r.Text != "" || r.Continuation != "" {
		t.Fatal("continuation silently skipped an unsupported unit", r, err)
	}
	req.Unit = 2
	r, err = Get(context.Background(), req)
	if err != nil || r.Text != "Three\n" || !r.EndOfSource {
		t.Fatal(r, err)
	}
}

func TestTextV2RefusesActiveContainersAndMalformedXML(t *testing.T) {
	for _, tc := range []struct{ body, status string }{
		{"<script>window.x=1</script>", "unsupported_extraction"},
		{"<p>unclosed", "malformed_content"},
	} {
		req := textV2(t, []string{"<p>fine</p>", tc.body}, nil)
		r, err := Get(context.Background(), req)
		if err == nil || r.Status != tc.status || r.Text != "" || r.Units != nil || r.Gaps != nil {
			t.Fatal(tc.body, r, err)
		}
	}
}

func TestTextV2ToleratesMissingPackageMetadataUnlikeV1(t *testing.T) {
	strip := func(files map[string]string) {
		files["book.opf"] = strings.Replace(files["book.opf"], "<language>mul</language>", "", 1)
	}
	req := fixture(t, []string{"<p>Readable</p>"}, strip)
	if _, err := Get(context.Background(), req); err == nil {
		t.Fatal("text/1 contract changed: it refuses any assessment warning")
	}
	req.Representation, req.MaxBytes = "epub-text/2", 64
	r, err := Get(context.Background(), req)
	if err != nil || r.Text != "Readable\n" {
		t.Fatal(r, err)
	}
}

func TestRepresentationSelectionDefaultsAndContinuations(t *testing.T) {
	req := fixture(t, []string{"<p>One two three</p>", "<p>Four</p>"}, nil)
	req.Representation = ""
	r, err := Get(context.Background(), req)
	if err != nil || r.Representation != DefaultRepresentation || r.Extractor != TextExtractorV2 {
		t.Fatal("default representation", r, err)
	}
	// An existing epub-text/1 continuation keeps its extractor with no flag.
	req.Representation = "epub-text/1"
	v1, err := Get(context.Background(), req)
	if err != nil || v1.Continuation == "" {
		t.Fatal(v1, err)
	}
	req.Representation, req.Cursor = "", v1.Continuation
	r, err = Get(context.Background(), req)
	if err != nil || r.Extractor != Extractor || r.Representation != "epub-text/1" || r.Locator.Start != v1.Locator.End {
		t.Fatal("text/1 continuation moved to another extractor", r, err)
	}
	// Explicit mismatches are stale references, never remapped positions.
	for _, rep := range []string{"epub-text/2", "epub-structure/1"} {
		req.Representation = rep
		r, err = Get(context.Background(), req)
		if err == nil || r.Status != "stale_reference" || r.Text != "" {
			t.Fatal(rep, r, err)
		}
	}
	req.Representation, req.Cursor = "epub-text/2", ""
	v2, err := Get(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	req.Representation, req.Cursor = "epub-text/1", v2.Continuation
	if r, err = Get(context.Background(), req); err == nil || r.Status != "stale_reference" {
		t.Fatal("text/2 continuation accepted by text/1", r, err)
	}
	req.Representation, req.Cursor = "", "not-a-cursor"
	if r, err = Get(context.Background(), req); err == nil || r.Text != "" {
		t.Fatal("invalid continuation accepted", r, err)
	}
	for _, mutate := range []func(*Request){
		func(r *Request) { r.Part = "id:x" },
		func(r *Request) { r.Resource = true },
	} {
		bad := fixture(t, []string{"<p>One</p>"}, nil)
		bad.Representation = "epub-text/2"
		mutate(&bad)
		if r, err := Get(context.Background(), bad); err == nil || r.Text != "" {
			t.Fatal("part/resource accepted by plain text", r, err)
		}
	}
	if Available().Default != DefaultRepresentation || Available().Representations[0].ID != DefaultRepresentation {
		t.Fatal("capabilities do not report the default representation")
	}
}

func TestTextV2StaleAfterSourceChange(t *testing.T) {
	a := textV2(t, []string{"<p>Same words here</p>"}, nil)
	r, err := Get(context.Background(), a)
	if err != nil || r.Continuation == "" {
		t.Fatal(r, err)
	}
	b := textV2(t, []string{"<p>Same words here</p><img src=\"x.png\"/>"}, nil)
	c, err := parseCursor(r.Continuation)
	if err != nil {
		t.Fatal(err)
	}
	c.AssetID = b.AssetID
	b.Cursor = encodeCursor(c)
	if r, err := Get(context.Background(), b); err == nil || r.Status != "stale_reference" {
		t.Fatal("gap change did not invalidate the representation", r, err)
	}
}
