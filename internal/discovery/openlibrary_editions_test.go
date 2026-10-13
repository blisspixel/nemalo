package discovery

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestEditionListUsesTheWorkEditionsPage(t *testing.T) {
	calls := 0
	p := provider(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/works/OL45804W/editions.json" || r.URL.Query().Get("limit") != "4" || r.URL.Query().Get("offset") != "0" {
			t.Errorf("edition page: %s", r.URL)
		}
		if r.URL.Query().Has("ia") || r.URL.Query().Has("availability") || strings.Contains(r.URL.Path, "search") {
			t.Errorf("work-level Archive field requested: %s", r.URL)
		}
		_, _ = io.WriteString(w, `{
			"links":{"work":"/works/OL45804W","next":"/works/OL45804W/editions.json?offset=4"},
			"size":4,
			"entries":[
				{"key":"/books/OL7353617M","title":"Fantastic Mr. Fox","type":{"key":"/type/edition"},"publishers":["Puffin"],"publish_date":"1988","languages":[{"key":"/languages/eng"}],"works":[{"key":"/works/OL45804W"}],"ocaid":"demoitem","isbn_13":["9780140328721"]},
				{"key":"/books/OL2M","title":"Paper only","works":[{"key":"/works/OL45804W"}]},
				{"key":"/books/OL9M","title":"Other work","works":[{"key":"/works/OL1W"}]},
				{"key":"/books/OL8M","title":"Many ids","works":[{"key":"/works/OL45804W"}],"ocaid":["one","two"]}
			]
		}`)
	})
	page, err := p.ListEditions(context.Background(), "openlibrary:OL45804W", 4, 0)
	if err != nil || !page.Complete || page.Access != OpenLibraryEditionList || page.EditionCount != 4 || len(page.Editions) != 2 || calls != 1 {
		t.Fatal(page, err, calls)
	}
	if page.Editions[0].ID != "openlibrary:OL7353617M" || page.Editions[0].ArchiveID != "archive:demoitem" || page.Editions[0].Title != "Fantastic Mr. Fox" || page.Editions[0].Languages[0] != "eng" || page.Editions[0].ISBNs[0] != "9780140328721" {
		t.Fatal(page.Editions[0])
	}
	if page.Editions[1].ArchiveID != "" || page.Editions[1].ID != "openlibrary:OL2M" {
		t.Fatal(page.Editions[1])
	}
	if page.LandingPage != "https://openlibrary.org/works/OL45804W" || !strings.Contains(strings.Join(page.Limitations, " "), "were omitted") {
		t.Fatal(page.LandingPage, page.Limitations)
	}
	if strings.Contains(page.MetadataURL, "next") {
		t.Fatal("followed the provider next link", page.MetadataURL)
	}
}

func TestEditionListRejectsAShortPage(t *testing.T) {
	p := provider(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{
			"links":{"work":"/works/OL45804W"},
			"size":10,
			"entries":[{"key":"/books/OL2M","type":{"key":"/type/edition"},"works":[{"key":"/works/OL45804W"}]}]
		}`)
	})
	if _, err := p.ListEditions(context.Background(), "openlibrary:OL45804W", 4, 0); err == nil {
		t.Fatal("short edition page accepted")
	}
}

func TestEditionRecordKeepsOCAIDAndReportsOtherSourceRecords(t *testing.T) {
	p := provider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/books/OL7353617M.json" {
			t.Errorf("edition: %s", r.URL)
		}
		_, _ = io.WriteString(w, `{"key":"/books/OL7353617M","title":"Fantastic Mr. Fox","type":{"key":"/type/edition"},"languages":[{"key":"/languages/eng"}],"works":[{"key":"/works/OL45804W"}],"ocaid":"demoitem","source_records":["ia:otheritem","ia:demoitem","marc:not-an-item"]}`)
	})
	record, err := p.ReadEdition(context.Background(), "openlibrary:OL7353617M")
	if err != nil || record.ArchiveID != "archive:demoitem" || len(record.OtherArchiveIDs) != 1 || record.OtherArchiveIDs[0] != "archive:otheritem" || record.MetadataSHA256 == "" {
		t.Fatal(record, err)
	}
}

func TestEditionRecordRejectsAnAmbiguousArchiveIdentifier(t *testing.T) {
	for _, body := range []string{
		`{"key":"/books/OL8M","ocaid":["one","two"]}`,
		`{"key":"/books/OL8M","ocaid":"../secret"}`,
		`{"key":"/books/OL9M","ocaid":"demoitem"}`,
		`{"key":"/books/OL8M","ocaid":"demoitem","source_records":{"ia":"demoitem"}}`,
	} {
		p := provider(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) })
		if _, err := p.ReadEdition(context.Background(), "openlibrary:OL8M"); err == nil {
			t.Fatal("ambiguous edition accepted", body)
		}
	}
	p := NewOpenLibrary()
	if _, err := p.ListEditions(context.Background(), "openlibrary:OL1M", 1, 0); err == nil {
		t.Fatal("edition id accepted as a work")
	}
	if _, err := p.ReadEdition(context.Background(), "openlibrary:OL1W"); err == nil {
		t.Fatal("work id accepted as an edition")
	}
	if !ValidOpenLibraryWork("openlibrary:OL45804W") || ValidOpenLibraryWork("openlibrary:OL45804M") || !ValidOpenLibraryEdition("openlibrary:OL7353617M") || ValidOpenLibraryEdition("openlibrary:OL7353617W") {
		t.Fatal("identifier classes overlap")
	}
}

func TestEditionListDoesNotFollowARedirectOffHost(t *testing.T) {
	p := NewOpenLibrary()
	req, err := http.NewRequest(http.MethodGet, "https://archive.org/metadata/demo", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.client.CheckRedirect(req, nil); err == nil {
		t.Fatal("Open Library client followed an Archive host")
	}
}
