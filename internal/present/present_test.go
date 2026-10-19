package present

import (
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/inventory"
)

func TestSearchEscapesTerminalControls(t *testing.T) {
	page := discovery.Page{Total: 1, Results: []discovery.Result{{Title: "Title\x1b[31m", ID: "openlibrary:OL1W", Authors: []string{"Name\x00"}, LandingPage: "https://openlibrary.org/works/OL1W"}}}
	text := Search(page)
	if strings.ContainsAny(text, "\x1b\x00") || !strings.Contains(text, "download availability is not resolved") {
		t.Fatal(text)
	}
	if !strings.Contains(Search(discovery.Page{}), "No results") {
		t.Fatal("empty results hidden")
	}
}

func TestInventoryFindings(t *testing.T) {
	text := Inventory(inventory.Report{Root: "/books", Entries: []inventory.Entry{{Path: "bad\x1b.epub", Kind: "ebook_candidate", SHA256: "abc", Finding: "limited\x00"}}})
	if strings.ContainsAny(text, "\x1b\x00") || !strings.Contains(text, "not scanned") || !strings.Contains(text, "SHA-256: abc") {
		t.Fatal(text)
	}
}

func TestSearchShowsCatalogTypeAndIssued(t *testing.T) {
	page := discovery.Page{Source: "gutenberg", Query: "pride", Order: discovery.OrderRelevance, Total: 2, Results: []discovery.Result{
		{ID: "gutenberg:1342", Title: "Pride and Prejudice", MediaType: "Text", Issued: "1998-06-01"},
		{ID: "gutenberg:20687", Title: "Pride and Prejudice", MediaType: "Sound\x1b[2J"},
		{ID: "archive:x", Title: "Other"},
	}}
	out := Search(page)
	if !strings.Contains(out, "   Catalog: Text, issued 1998-06-01\n") || !strings.Contains(out, `   Catalog: Sound\u001b[2J`+"\n") || strings.Count(out, "Catalog:") != 2 {
		t.Fatal(out)
	}
	if CatalogClaim(discovery.Result{Issued: "2007-02-03"}) != "issued 2007-02-03" || CatalogClaim(discovery.Result{}) != "" {
		t.Fatal("claim forms")
	}
}
