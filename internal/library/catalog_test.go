package library

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/blisspixel/nemalo/internal/inventory"
)

func put(t *testing.T, root, name, data string) string {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func fixture(t *testing.T) (string, Snapshot) {
	t.Helper()
	root := t.TempDir()
	put(t, root, "book.pdf", "%PDF-1.7\n%%EOF")
	put(t, root, "nested/copy.pdf", "%PDF-1.7\n%%EOF")
	put(t, root, "notes.txt", "Knowledge")
	s, err := Build(context.Background(), root, inventory.Defaults(), true)
	if err != nil {
		t.Fatal(err)
	}
	return root, s
}

func TestSnapshotIdentityMetadataAndDuplicates(t *testing.T) {
	root, s := fixture(t)
	wantRead := int64(3*len("%PDF-1.7\n%%EOF") + len("Knowledge"))
	if !s.Catalog.Complete || len(s.Catalog.Assets) != 2 || s.Catalog.ID == "" || s.BytesRead != wantRead {
		t.Fatal(s)
	}
	var duplicate Asset
	for _, a := range s.Catalog.Assets {
		if len(a.Locations) == 2 {
			duplicate = a
		}
	}
	if duplicate.Health == nil || duplicate.Health.DetectedFormat != "pdf" || duplicate.Health.Antivirus.Status != "not_scanned" || duplicate.Health.File != "book.pdf" {
		t.Fatal(duplicate)
	}
	file := filepath.Join(t.TempDir(), "catalog.json")
	if err := Save(context.Background(), file, &s); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(file)
	if err != nil || loaded.ID != s.Catalog.ID || s.Output != file {
		t.Fatal(loaded, err)
	}
	if err := Save(context.Background(), file, &s); err == nil {
		t.Fatal("overwrote catalog")
	}
	a, err := Verify(context.Background(), loaded, root, inventory.Defaults())
	if err != nil || !a.Complete || a.Unchanged != 3 || len(a.Findings) != 0 {
		t.Fatal(a, err)
	}
	// Root hints remain informational when catalogs move between OSes.
	loaded.RootHint = `C:\different\platform`
	if loaded.Validate() != nil {
		t.Fatal("platform-dependent catalog")
	}
	if _, err := Verify(context.Background(), loaded, "", inventory.Defaults()); err == nil {
		t.Fatal("catalog authorized implicit scan")
	}
	if got, _ := os.ReadFile(filepath.Join(root, "book.pdf")); string(got) != "%PDF-1.7\n%%EOF" {
		t.Fatal("source changed")
	}
}

func TestAuditChangedMissingAddedAndIncomplete(t *testing.T) {
	root, s := fixture(t)
	put(t, root, "book.pdf", "%PDF-1.8\n%%EOF")
	if err := os.Remove(filepath.Join(root, "nested", "copy.pdf")); err != nil {
		t.Fatal(err)
	}
	put(t, root, "new.epub", "new")
	a, err := Verify(context.Background(), s.Catalog, root, inventory.Defaults())
	if err == nil || !a.Complete || a.Unchanged != 1 {
		t.Fatal(a, err)
	}
	states := map[string]string{}
	for _, f := range a.Findings {
		states[f.Path] = f.Status
	}
	if states["book.pdf"] != "changed" || states["nested/copy.pdf"] != "missing" || states["new.epub"] != "added" {
		t.Fatal(states)
	}
	limits := inventory.Defaults()
	limits.Entries = 1
	a, err = Verify(context.Background(), s.Catalog, root, limits)
	if err == nil || a.Complete {
		t.Fatal("incomplete audit passed", a)
	}
	for _, f := range a.Findings {
		if f.Status == "missing" {
			t.Fatal("absence inferred from incomplete traversal")
		}
	}
	limits = inventory.Defaults()
	limits.TotalBytes = 1
	a, err = Verify(context.Background(), s.Catalog, root, limits)
	if err == nil || a.Complete {
		t.Fatal("hash budget ignored")
	}
	found := false
	for _, f := range a.Findings {
		found = found || f.Status == "unverified"
	}
	if !found {
		t.Fatal("unverified hashes hidden")
	}
	if _, err := Verify(context.Background(), Catalog{}, root, inventory.Defaults()); err == nil {
		t.Fatal("bad catalog audited")
	}
}

func TestStrictCatalogSchema(t *testing.T) {
	_, s := fixture(t)
	mutations := []func(*Catalog){
		func(c *Catalog) { c.SchemaVersion = 2 }, func(c *Catalog) { c.Complete = false }, func(c *Catalog) { c.ID = "snapshot:" + strings.Repeat("z", 32) },
		func(c *Catalog) { c.Assets[0].SHA256 = "bad" }, func(c *Catalog) { c.Assets[0].ID = "wrong" }, func(c *Catalog) { c.Assets[0].Bytes = -1 },
		func(c *Catalog) { c.Assets[0].Locations = nil }, func(c *Catalog) { c.Assets[0].Locations[0].Path = "../escape" }, func(c *Catalog) { c.Assets[0].Locations[0].Path = `folder\escape` },
		func(c *Catalog) { c.Assets[0].Locations[0].CandidateKind = "executable" }, func(c *Catalog) { c.Assets = append(c.Assets, c.Assets[0]) },
		func(c *Catalog) { c.Assets[1].Locations = append(c.Assets[1].Locations, c.Assets[0].Locations[0]) },
		func(c *Catalog) {
			c.Excluded = append(c.Excluded, inventory.Entry{Path: "bad", Kind: "other", Finding: "unknown"})
		},
		func(c *Catalog) {
			c.Excluded = append(c.Excluded, inventory.Entry{Path: c.Assets[0].Locations[0].Path, Kind: "link", Finding: "not_followed"})
		},
		func(c *Catalog) {
			for i := range c.Assets {
				if c.Assets[i].Health != nil {
					c.Assets[i].Health.SHA256 = strings.Repeat("0", 64)
				}
			}
		},
	}
	original, _ := json.Marshal(s.Catalog)
	for i, mutate := range mutations {
		var c Catalog
		if err := json.Unmarshal(original, &c); err != nil {
			t.Fatal(err)
		}
		mutate(&c)
		if c.Validate() == nil {
			t.Fatal("mutation accepted", i)
		}
	}
	for _, data := range []string{"null", "{}", string(original) + " {}", strings.Replace(string(original), `"schema_version":1`, `"extra":1,"schema_version":1`, 1)} {
		p := put(t, t.TempDir(), "catalog.json", data)
		if _, err := Load(p); err == nil {
			t.Fatal("invalid JSON accepted")
		}
	}
	p := put(t, t.TempDir(), "catalog.json", strings.Repeat(" ", maxCatalogBytes+1))
	if _, err := Load(p); err == nil {
		t.Fatal("large catalog accepted")
	}
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("directory catalog accepted")
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing catalog accepted")
	}
}

func TestSnapshotFailuresAndExclusivePublication(t *testing.T) {
	root, s := fixture(t)
	if err := Save(context.Background(), filepath.Join(root, "catalog.json"), &s); err == nil {
		t.Fatal("wrote into source root")
	}
	if err := Save(context.Background(), filepath.Join(t.TempDir(), "missing", "catalog.json"), &s); err == nil {
		t.Fatal("missing parent accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Build(ctx, root, inventory.Defaults(), true); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "cancelled.json")
	if err := Save(ctx, p, &s); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled output published")
	}
	if err := Save(context.Background(), p, &Snapshot{}); err == nil {
		t.Fatal("incomplete catalog saved")
	}
	limits := inventory.Defaults()
	limits.TotalBytes = 1
	partial, err := Build(context.Background(), root, limits, false)
	if err == nil || partial.Catalog.Complete {
		t.Fatal("incomplete snapshot passed")
	}
	limits = inventory.Defaults()
	limits.FileBytes = 256<<20 + 1
	if _, err := Build(context.Background(), root, limits, false); err == nil {
		t.Fatal("invalid limits accepted")
	}
	// Several writers cannot overwrite or expose partially encoded output.
	file := filepath.Join(t.TempDir(), "race.json")
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		wg.Go(func() { copy := s; results <- Save(context.Background(), file, &copy) })
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		}
	}
	if winners != 1 {
		t.Fatal("exclusive publication failed", winners)
	}
	if _, err := Load(file); err != nil {
		t.Fatal("partial publication", err)
	}
	files, err := os.ReadDir(filepath.Dir(file))
	if err != nil || len(files) != 1 {
		t.Fatal("pending files leaked", files, err)
	}
}

func TestHoldingsLiteralFilteringAndPaging(t *testing.T) {
	_, s := fixture(t)
	for _, q := range []string{"BOOK.PDF", "pdf_candidate", "sha256:"} {
		p, err := Find(s.Catalog, q, 1, 0)
		if err != nil || p.Total < 1 || len(p.Assets) != 1 {
			t.Fatal(p, err)
		}
	}
	p, err := Find(s.Catalog, "", 1, 1)
	if err != nil || p.Total != 2 || len(p.Assets) != 1 {
		t.Fatal(p, err)
	}
	p, err = Find(s.Catalog, "no match", 1, 0)
	if err != nil || p.Total != 0 || len(p.Assets) != 0 {
		t.Fatal(p, err)
	}
	for _, tc := range []struct{ limit, offset int }{{0, 0}, {101, 0}, {1, -1}, {1, 100001}} {
		if _, err := Find(s.Catalog, "", tc.limit, tc.offset); err == nil {
			t.Fatal("invalid paging")
		}
	}
	if _, err := Find(Catalog{}, "", 10, 0); err == nil {
		t.Fatal("bad catalog queried")
	}
	if _, err := Find(s.Catalog, strings.Repeat("x", 1001), 1, 0); err == nil {
		t.Fatal("long filter accepted")
	}
}

func TestEPUBMetadataAndUnhealthyFilesRemainEvidence(t *testing.T) {
	var data bytes.Buffer
	z := zip.NewWriter(&data)
	files := []struct{ name, body string }{
		{"mimetype", "application/epub+zip"},
		{"META-INF/container.xml", `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`},
		{"book.opf", `<package xmlns="http://www.idpf.org/2007/opf"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>La connaissance</dc:title><dc:language>fr</dc:language></metadata><manifest><item id="c" href="chapter.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="c"/></spine></package>`},
		{"chapter.xhtml", "<html><body>Bonjour</body></html>"},
	}
	for _, item := range files {
		w, err := z.CreateHeader(&zip.FileHeader{Name: item.name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(item.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	put(t, root, "a.bin", data.String())
	put(t, root, "b.epub", data.String())
	put(t, root, "broken.pdf", "MZnot a book")
	s, err := Build(context.Background(), root, inventory.Defaults(), true)
	if err != nil || len(s.Catalog.Assets) != 2 {
		t.Fatal(s, err)
	}
	for _, query := range []string{"CONNAISSANCE", "fr"} {
		p, err := Find(s.Catalog, query, 10, 0)
		if err != nil || p.Total != 1 || p.Assets[0].Health.Checks.EPUB.TextCharacters != 7 || len(p.Assets[0].Locations) != 2 {
			t.Fatal(p, err)
		}
	}
	p, err := Find(s.Catalog, "broken.pdf", 1, 0)
	if err != nil || p.Assets[0].Health.Status != "invalid_or_unsupported" {
		t.Fatal("baseline falsely certified bytes", p, err)
	}
	file := filepath.Join(t.TempDir(), "catalog.json")
	if err := Save(context.Background(), file, &s); err != nil {
		t.Fatal("unhealthy files should still be preservable evidence", err)
	}
}

func TestLinksExcludedAndPreserved(t *testing.T) {
	root := t.TempDir()
	outside := put(t, t.TempDir(), "outside.pdf", "outside")
	if err := os.Symlink(outside, filepath.Join(root, "link.pdf")); err != nil {
		t.Skip("symlink unavailable", err)
	}
	s, err := Build(context.Background(), root, inventory.Defaults(), true)
	if err != nil || len(s.Catalog.Assets) != 0 || len(s.Catalog.Excluded) != 1 {
		t.Fatal(s, err)
	}
	if _, err := Verify(context.Background(), s.Catalog, root, inventory.Defaults()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "link.pdf")); err != nil {
		t.Fatal(err)
	}
	put(t, root, "link.pdf", "different")
	a, err := Verify(context.Background(), s.Catalog, root, inventory.Defaults())
	if err == nil || a.Findings[0].Status != "excluded_location_changed" {
		t.Fatal(a, err)
	}
	link := filepath.Join(t.TempDir(), "source-alias")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if err := Save(context.Background(), filepath.Join(link, "catalog.json"), &s); err == nil {
		t.Fatal("source alias accepted as output")
	}
	if data, _ := os.ReadFile(outside); !bytes.Equal(data, []byte("outside")) {
		t.Fatal("external source changed")
	}
}
