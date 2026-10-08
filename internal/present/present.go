// Package present formats untrusted catalog and inventory data for both terminal interfaces.
package present

import (
	"fmt"
	"strings"

	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/inventory"
)

func Search(page discovery.Page) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Open Library search %q: %d matching works; %d shown from offset %d.\n", page.Query, page.Total, len(page.Results), page.Offset)
	if len(page.Results) == 0 {
		b.WriteString("No results in this page.\n")
	}
	for i, r := range page.Results {
		fmt.Fprintf(&b, "\n%d. %q\n   ID: %q\n   Authors: %q\n   Languages: %q\n   %q\n   Discovery metadata; download availability not resolved.\n", page.Offset+i+1, r.Title, r.ID, r.Authors, r.Languages, r.LandingPage)
	}
	return b.String()
}

func Inventory(r inventory.Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Folder: %q\nComplete: %t | Entries: %d | Bytes read: %d\nSecurity: not scanned. Formats below are filename candidates.\n", r.Root, r.Complete, len(r.Entries), r.BytesRead)
	for _, e := range r.Entries {
		fmt.Fprintf(&b, "\n%-18q %q (%d bytes)\n", e.Kind, e.Path, e.Bytes)
		if e.SHA256 != "" {
			fmt.Fprintf(&b, "  SHA-256: %q\n", e.SHA256)
		}
		if e.Finding != "" {
			fmt.Fprintf(&b, "  Finding: %q\n", e.Finding)
		}
	}
	return b.String()
}
