package present

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/blisspixel/nemalo/internal/content"
)

// Content escapes each source line before terminal rendering. Newlines carry
// source structure; terminal controls remain inert, visible escape sequences.
func Content(r content.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Source access: %q\nAsset: %q\nRepresentation: %q\n", r.Status, r.AssetID, r.Representation)
	if r.Locator != nil {
		fmt.Fprintf(&b, "Unit %d | bytes %d..%d (%s)\n", r.Locator.Unit, r.Locator.Start, r.Locator.End, r.Locator.OffsetUnit)
	}
	if r.Coverage != nil {
		fmt.Fprintf(&b, "Coverage: %q\n", r.Coverage.Text)
		for _, id := range r.Coverage.KnownGaps {
			for _, p := range r.Parts {
				if p.ID == id {
					fmt.Fprintf(&b, "Gap %q: %q\n", id, p.Gap)
				}
			}
		}
		fmt.Fprintf(&b, "References outside range: %d\n", len(r.Coverage.ReferencedOutsideRange))
		for _, unknown := range r.Coverage.Unknown {
			fmt.Fprintf(&b, "Not established: %q\n", unknown)
		}
	}
	if r.Text != "" {
		b.WriteString("\n")
		for _, line := range strings.Split(r.Text, "\n") {
			quoted := strconv.Quote(line)
			b.WriteString(quoted[1 : len(quoted)-1])
			b.WriteByte('\n')
		}
	}
	if r.Continuation != "" {
		b.WriteString("\nNext range available. Explicit continuation required.\n")
	}
	b.WriteString("Delivery is not evidence of reading or understanding.\n")
	return b.String()
}
