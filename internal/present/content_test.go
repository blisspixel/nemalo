package present

import (
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/content"
)

func TestContentEscapesSourceWithoutFlatteningLines(t *testing.T) {
	r := content.Result{Status: "partial_range", AssetID: "asset", Representation: "epub-structure/1", Text: "日本語\n\nunsafe\x1b[31m\r", Continuation: "token", Locator: &content.Locator{Unit: 1, Start: 2, End: 4, OffsetUnit: "utf8_bytes"}, Coverage: &content.Coverage{Text: "known_extraction_gaps", KnownGaps: []string{"fig"}, Unknown: []string{"layout"}}, Parts: []content.Part{{ID: "fig", Gap: "image_pixels_not_text"}}}
	s := Content(r)
	if strings.ContainsAny(s, "\x1b\r") || !strings.Contains(s, "日本語\n\nunsafe\\x1b[31m\\r") || !strings.Contains(s, "image_pixels_not_text") || !strings.Contains(s, "Next range available") || !strings.Contains(s, "not evidence of reading") {
		t.Fatal(s)
	}
}

func TestContentMarksLocatedGapsWithoutAlteringSource(t *testing.T) {
	alt, empty, hostile := "The old sea-dog", "", "x\x1b[2Jy"
	r := content.Result{Status: "complete_range", Representation: "epub-text/2", Text: "CHAPTER I\nProse.\nEnd.\n",
		Locator:  &content.Locator{Unit: 0, Start: 100, End: 122, OffsetUnit: "utf8_bytes"},
		Coverage: &content.Coverage{Text: "known_extraction_gaps", KnownGaps: []string{"a", "b", "c", "d", "e"}},
		Gaps: []content.Gap{
			{Part: "a", Element: "img", Reason: "image_pixels_not_text", Offset: 110, Image: &content.GapImage{Alt: &alt}},
			{Part: "b", Element: "table", Reason: "table_relationships_not_extracted", Offset: 117},
			{Part: "c", Element: "img", Reason: "image_pixels_not_text", Offset: 122, Image: &content.GapImage{}},
			{Part: "d", Element: "img", Reason: "image_pixels_not_text", Offset: 122, Image: &content.GapImage{Alt: &empty}},
			{Part: "e", Element: "img", Reason: "image_pixels_not_text", Offset: 122, Image: &content.GapImage{Alt: &hostile}},
		}}
	s := Content(r)
	want := "CHAPTER I\n[image: The old sea-dog]Prose.\n[table omitted]End.\n[image, no alt text][image, empty alt text]" + `[image: x\x1b[2Jy]` + "\n"
	if !strings.Contains(s, want) || strings.Contains(s, "\x1b") || !strings.Contains(s, "5 located gap(s)") {
		t.Fatalf("%q", s)
	}
	if r.Text != "CHAPTER I\nProse.\nEnd.\n" {
		t.Fatal("presentation mutated source text")
	}
}

func TestContentUnitsResourcesAndContinuationCommand(t *testing.T) {
	units := Content(content.Result{Status: "units_available", Units: []content.Unit{{Index: 0, Extraction: "supported_text_with_gaps", TextBytes: 12, KnownGaps: 2, Linear: "yes", Member: "a.xhtml"}, {Index: 1, Extraction: "unsupported_document", Linear: "no", Member: "b.svg"}}, UnsupportedUnits: []int{1}})
	if !strings.Contains(units, "Reading-order units: 2") || !strings.Contains(units, "supported_text_with_gaps") || !strings.Contains(units, "Unsupported units [1]") {
		t.Fatal(units)
	}
	res := Content(content.Result{Status: "partial_range", Resource: &content.ResourceData{Member: "i.png", SHA256: "abc", TotalBytes: 10, Data: []byte("1234")}, Locator: &content.Locator{OffsetUnit: "resource_bytes"}})
	if !strings.Contains(res, "4 of 10 bytes") || strings.Contains(res, "1234") {
		t.Fatal(res)
	}
	cmd := ContentCommand(content.Result{Status: "partial_range", Text: "a", Continuation: "TOKEN", Locator: &content.Locator{}})
	if !strings.Contains(cmd, "Continue with: --cursor TOKEN") || !strings.Contains(cmd, "(utf8_bytes)") {
		t.Fatal(cmd)
	}
	if strings.Contains(ContentCommand(content.Result{Status: "complete_range", Text: "a", Locator: &content.Locator{}}), "--cursor") {
		t.Fatal("cursor hint without continuation")
	}
}
