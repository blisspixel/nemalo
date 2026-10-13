package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCatalog(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pg_catalog.csv")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const sampleCatalog = `Text#,Type,Issued,Title,Language,Authors,Subjects,LoCC,Bookshelves
11,Text,1865-11-26,Alice's Adventures in Wonderland,en,"Carroll, Lewis, 1832-1898",Fantasy,PZ,Children's Literature
12,Text,1813-01-28,Pride and Prejudice,en,"Austen, Jane, 1775-1817",Love stories,PR,Category: Novels
20,Text,2000-01-01,Austen Letters,en; fr,"Editor, Ed",Letters,PR,Essays
30,Text,1971-01-01,"Line one
Line two",en,Someone,Topic,E,Shelf
40,Text,2020-01-01,第一章,zh,作者,主题,PL,中文
1342,Text,1998-06-01,Pride and Prejudice,en,"Austen, Jane, 1775-1817; Smith, Ada",Love stories; England -- Fiction,PR,Novels
`

func TestGutenbergCatalogSearchRanksAndPages(t *testing.T) {
	g := &Gutenberg{Catalog: writeCatalog(t, sampleCatalog)}
	page, err := g.SearchOrdered(context.Background(), "austen", OrderRelevance, 10, 0)
	if err != nil || page.Total != 3 || page.Order != OrderRelevance || len(page.Results) != 3 {
		t.Fatal(page, err)
	}
	if page.Results[0].ID != "gutenberg:20" || page.Results[1].ID != "gutenberg:12" || page.Results[2].ID != "gutenberg:1342" {
		t.Fatal(page.Results)
	}
	if page.Results[2].Authors[0] != "Austen, Jane, 1775-1817" || page.Results[2].Authors[1] != "Smith, Ada" {
		t.Fatal(page.Results[2].Authors)
	}
	if page.Results[0].Access != "local_catalog" || page.Results[0].LandingPage != "https://www.gutenberg.org/ebooks/20" {
		t.Fatal(page.Results[0])
	}
	if len(page.Results[0].Languages) != 2 || page.Results[0].Languages[0] != "en" || page.Results[0].Languages[1] != "fr" {
		t.Fatal(page.Results[0].Languages)
	}
	byID, err := g.SearchOrdered(context.Background(), "austen", OrderIdentifier, 1, 1)
	if err != nil || byID.Total != 3 || len(byID.Results) != 1 || byID.Results[0].ID != "gutenberg:20" {
		t.Fatal(byID, err)
	}
	exact, err := g.Search(context.Background(), "1342", 10, 0)
	if err != nil || exact.Total != 1 || exact.Results[0].ID != "gutenberg:1342" {
		t.Fatal(exact, err)
	}
	named, err := g.Search(context.Background(), "gutenberg:1342", 10, 0)
	if err != nil || named.Total != 1 || named.Results[0].ID != "gutenberg:1342" {
		t.Fatal(named, err)
	}
	folded, err := g.Search(context.Background(), "Line Two", 10, 0)
	if err != nil || folded.Total != 1 || folded.Results[0].Title != "Line one Line two" {
		t.Fatal(folded, err)
	}
	unicode, err := g.Search(context.Background(), "第一章", 10, 0)
	if err != nil || unicode.Total != 1 || unicode.Results[0].Languages[0] != "zh" {
		t.Fatal(unicode, err)
	}
	subject, err := g.Search(context.Background(), "england", 10, 0)
	if err != nil || subject.Total != 1 || subject.Results[0].ID != "gutenberg:1342" {
		t.Fatal(subject, err)
	}
	none, err := g.Search(context.Background(), "missing-title", 10, 0)
	if err != nil || none.Total != 0 || len(none.Results) != 0 {
		t.Fatal(none, err)
	}
}

func TestGutenbergEvaluationReadsOneRowAndOffersNoFile(t *testing.T) {
	g := &Gutenberg{Catalog: writeCatalog(t, sampleCatalog)}
	got, err := g.Evaluate(context.Background(), "gutenberg:1342")
	if err != nil || !got.Complete || got.Access != "local_catalog" || len(got.Files) != 0 || len(got.Rights) != 0 {
		t.Fatal(got, err)
	}
	if got.Title[0] != "Pride and Prejudice" || len(got.Creators) != 2 || got.Dates[0] != "1998-06-01" {
		t.Fatal(got.Title, got.Creators, got.Dates)
	}
	if len(got.Subjects) != 2 || got.Subjects[1] != "England -- Fiction" || got.Classes[0] != "PR" || got.Bookshelves[0] != "Novels" {
		t.Fatal(got.Subjects, got.Classes, got.Bookshelves)
	}
	if got.LandingPage != "https://www.gutenberg.org/ebooks/1342" || len(got.MetadataSHA256) != 64 {
		t.Fatal(got.LandingPage, got.MetadataSHA256)
	}
	for _, limit := range got.Limitations {
		if strings.Contains(limit, "download URL") && strings.Contains(strings.Join(got.Limitations, "\n"), "website") {
			return
		}
	}
	t.Fatal(got.Limitations)
}

func TestGutenbergCatalogRefusesBrokenFiles(t *testing.T) {
	g := &Gutenberg{}
	if _, err := g.Search(context.Background(), "alice", 10, 0); err == nil || !strings.Contains(err.Error(), "does not query") {
		t.Fatal(err)
	}
	g.Catalog = "relative.csv"
	if _, err := g.Search(context.Background(), "alice", 10, 0); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatal(err)
	}
	dir := t.TempDir()
	g.Catalog = filepath.Join(dir, "missing.csv")
	if _, err := g.Evaluate(context.Background(), "gutenberg:11"); err == nil {
		t.Fatal("missing catalog evaluated")
	}
	g.Catalog = writeCatalog(t, "Text#,Type,Issued,Title,Language,Authors,Subjects,LoCC\n")
	if _, err := g.Search(context.Background(), "alice", 10, 0); err == nil || !strings.Contains(err.Error(), "header") {
		t.Fatal(err)
	}
	g.Catalog = writeCatalog(t, sampleCatalog+"11,Text,1865-11-26,Again,en,Someone,Topic,E,Shelf\n")
	if _, err := g.Search(context.Background(), "alice", 10, 0); err == nil || !strings.Contains(err.Error(), "repeated") {
		t.Fatal(err)
	}
	g.Catalog = writeCatalog(t, "Text#,Type,Issued,Title,Language,Authors,Subjects,LoCC,Bookshelves\n11,Text,not-a-date,Title,en,Someone,Topic,E,Shelf\n")
	if _, err := g.Search(context.Background(), "title", 10, 0); err == nil || !strings.Contains(err.Error(), "issued") {
		t.Fatal(err)
	}
	if _, err := g.Search(context.Background(), "", 10, 0); err == nil {
		t.Fatal("empty query")
	}
	if _, err := g.SearchOrdered(context.Background(), "alice", OrderDownloads, 10, 0); err == nil {
		t.Fatal("downloads order")
	}
	if ValidGutenbergID("gutenberg:01") || ValidGutenbergID("gutenberg:0") || ValidGutenbergID("archive:11") || !ValidGutenbergID("gutenberg:11") {
		t.Fatal("id rules")
	}
	previous := maxCatalogBytes
	maxCatalogBytes = 8
	t.Cleanup(func() { maxCatalogBytes = previous })
	g.Catalog = writeCatalog(t, sampleCatalog)
	if _, err := g.Search(context.Background(), "alice", 10, 0); err == nil || !strings.Contains(err.Error(), "64 MiB") {
		t.Fatal(err)
	}
}

func TestGutenbergFetchWritesACheckedCatalog(t *testing.T) {
	body := sampleCatalog
	sum := sha256.Sum256([]byte(body))
	var gotURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		if r.Header.Get("User-Agent") == "" {
			t.Error("missing user agent")
		}
		w.Header().Set("Content-Type", "text/csv")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	output := filepath.Join(t.TempDir(), "pg_catalog.csv")
	got, err := fetchCatalog(context.Background(), output, server.URL, server.Client())
	if err != nil || got.Path != output || got.Bytes != int64(len(body)) || got.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal(got, err)
	}
	if gotURL != "/" {
		t.Fatal(gotURL)
	}
	page, err := (&Gutenberg{Catalog: output}).Search(context.Background(), "alice", 10, 0)
	if err != nil || page.Total != 1 {
		t.Fatal(page, err)
	}
	if _, err := fetchCatalog(context.Background(), output, server.URL, server.Client()); err == nil {
		t.Fatal("existing catalog replaced")
	}
	if _, err := fetchCatalog(context.Background(), "relative.csv", server.URL, server.Client()); err == nil {
		t.Fatal("relative output accepted")
	}
	if _, err := fetchCatalog(context.Background(), filepath.Join(t.TempDir(), "missing", "pg.csv"), server.URL, server.Client()); err == nil {
		t.Fatal("missing parent accepted")
	}
	bad := filepath.Join(t.TempDir(), "bad.csv")
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not,a,catalog\n"))
	})
	if _, err := fetchCatalog(context.Background(), bad, server.URL, server.Client()); err == nil {
		t.Fatal("bad header stored")
	}
	if _, err := os.Lstat(bad); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("bad catalog left behind", err)
	}
	if CatalogURL != "https://www.gutenberg.org/cache/epub/feeds/pg_catalog.csv" {
		t.Fatal(CatalogURL)
	}
}
