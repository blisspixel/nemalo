// Package present formats untrusted catalog and inventory data for both terminal interfaces.
package present

import (
	"fmt"
	"strings"

	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/textsafe"
)

func Search(page discovery.Page) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%q search %q: %d matches; %d shown from offset %d.\n", page.Source, page.Query, page.Total, len(page.Results), page.Offset)
	switch page.Order {
	case discovery.OrderRelevance:
		if page.Source == "gutenberg" {
			b.WriteString("Order: local catalog ranking. Title or exact text number, then author, then subject or bookshelf. Equal ranks use the lower text number. The Gutenberg website was not queried.\n")
		} else {
			b.WriteString("Order: provider relevance ranking; ties may move between requests.\n")
		}
	case discovery.OrderIdentifier:
		if page.Source == "gutenberg" {
			b.WriteString("Order: ascending text number, stable for paging. The Gutenberg website was not queried.\n")
		} else {
			b.WriteString("Order: ascending source identifier, stable for paging; not relevance.\n")
		}
	case discovery.OrderDownloads:
		b.WriteString("Order: declared download count, most first; popularity, not quality or relevance.\n")
	}
	if len(page.Results) == 0 {
		b.WriteString("No results in this page.\n")
	}
	for i, r := range page.Results {
		fmt.Fprintf(&b, "\n%d. %q\n   ID: %q\n   Authors: %q\n   Languages: %q\n", page.Offset+i+1, r.Title, r.ID, r.Authors, r.Languages)
		if claim := CatalogClaim(r); claim != "" {
			fmt.Fprintf(&b, "   Catalog: %s\n", claim)
		}
		fmt.Fprintf(&b, "   %q\n   Discovery metadata; download availability not resolved.\n", r.LandingPage)
	}
	return b.String()
}

// CatalogClaim is a catalog row's type and issue date as the catalog states
// them, for example "Sound, issued 2007-02-03", or "" when it states neither.
func CatalogClaim(r discovery.Result) string {
	claim := textsafe.Visible(r.MediaType)
	if r.Issued != "" {
		if claim != "" {
			claim += ", "
		}
		claim += "issued " + textsafe.Visible(r.Issued)
	}
	return claim
}

func Catalog(file discovery.CatalogFile) string {
	return fmt.Sprintf("Catalog written: %q\nBytes: %d\nSHA-256: %q\nSearch with: nemalo search QUERY --source gutenberg --catalog %q\nThis file is catalog metadata. It does not include ebook bytes or a rights statement.\n", file.Path, file.Bytes, file.SHA256, file.Path)
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
