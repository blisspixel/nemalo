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
