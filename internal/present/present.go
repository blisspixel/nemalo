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
	fmt.Fprintf(&b, "%q search %q: %d matches; %d shown from offset %d.\n", page.Source, page.Query, page.Total, len(page.Results), page.Offset)
	switch page.Order {
	case discovery.OrderRelevance:
		b.WriteString("Order: provider relevance ranking; ties may move between requests.\n")
	case discovery.OrderIdentifier:
		b.WriteString("Order: ascending source identifier, stable for paging; not relevance.\n")
	case discovery.OrderDownloads:
		b.WriteString("Order: declared download count, most first; popularity, not quality or relevance.\n")
	}
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
