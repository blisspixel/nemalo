// Package present formats untrusted catalog and inventory data for both terminal interfaces.
package present

import (
	"fmt"
	"strings"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/inventory"
)

func Search(page discovery.Page) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s search for %s: %d matches; %d shown from offset %d.\n", sourceName(page.Source), Line(page.Query), page.Total, len(page.Results), page.Offset)
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
		fmt.Fprintf(&b, "\n%d. %s\n   ID: %s\n", page.Offset+i+1, Line(r.Title), Line(r.ID))
		if v := list(r.Authors); v != "" {
			fmt.Fprintf(&b, "   Author: %s\n", v)
		}
		if v := list(r.Languages); v != "" {
			fmt.Fprintf(&b, "   Language: %s\n", v)
		}
		if claim := CatalogClaim(r); claim != "" {
			fmt.Fprintf(&b, "   Catalog: %s\n", claim)
		}
		if v := Line(r.LandingPage); v != "" {
			fmt.Fprintf(&b, "   Page: %s\n", v)
		}
	}
	if len(page.Results) > 0 {
		b.WriteString("Discovery metadata only; download availability is not resolved.\n")
	}
	return b.String()
}

// MultiSearch shows a search of every source, each in its own section and
// order, with a source that failed or is not configured saying why.
func MultiSearch(m app.MultiPage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Search of every source for %s, up to %d results each. Each source keeps its own order; results are not ranked against each other.\n", Line(m.Query), m.Limit)
	for _, p := range m.Sources {
		b.WriteString("\n")
		switch {
		case p.Status == "complete" && p.Page != nil:
			b.WriteString(Search(*p.Page))
		case p.Status == "not_configured":
			fmt.Fprintf(&b, "%s: not configured. %s\n", sourceName(p.Source), Line(p.Detail))
		default:
			fmt.Fprintf(&b, "%s: search failed: %s\n", sourceName(p.Source), Line(p.Detail))
		}
	}
	return b.String()
}

// CatalogClaim is a catalog row's type and issue date as the catalog states
// them, for example "Sound, issued 2007-02-03", or "" when it states neither.
func CatalogClaim(r discovery.Result) string {
	claim := Line(r.MediaType)
	if issued := Line(r.Issued); issued != "" {
		if claim != "" {
			claim += ", "
		}
		claim += "issued " + issued
	}
	return claim
}

// sourceName is a provider's display name.
func sourceName(source string) string {
	switch source {
	case "openlibrary":
		return "Open Library"
	case "archive":
		return "Internet Archive"
	case "gutenberg":
		return "Project Gutenberg"
	}
	return Line(source)
}

func Catalog(file discovery.CatalogFile) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Catalog written: %s\nBytes: %d\nSHA-256: %s\n", Exact(file.Path), file.Bytes, Line(file.SHA256))
	if file.Continuations > 0 {
		fmt.Fprintf(&b, "Continued %d time(s) after a dropped connection; the whole file was checked.\n", file.Continuations)
	}
	if c := command("nemalo", "search", "QUERY", "--source", "gutenberg", "--catalog", file.Path); c != "" {
		fmt.Fprintf(&b, "Next: %s\n", c)
	}
	b.WriteString("This file is catalog metadata. It does not include ebook bytes or a rights statement.\n")
	return b.String()
}

func Inventory(r inventory.Report) string {
	var b strings.Builder
	field(&b, "Folder", Exact(r.Root))
	fmt.Fprintf(&b, "Inventory: %s | Entries: %d | Bytes read: %d\nSecurity: not scanned. Formats below are filename candidates.\n", map[bool]string{true: "complete", false: "incomplete"}[r.Complete], len(r.Entries), r.BytesRead)
	for _, e := range r.Entries {
		fmt.Fprintf(&b, "\n%-16s %s (%d bytes)\n", words(e.Kind), Exact(e.Path), e.Bytes)
		if e.SHA256 != "" {
			fmt.Fprintf(&b, "  SHA-256: %s\n", Line(e.SHA256))
		}
		if e.Finding != "" {
			fmt.Fprintf(&b, "  Finding: %s\n", words(e.Finding))
		}
	}
	return b.String()
}
