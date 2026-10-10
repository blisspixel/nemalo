package content

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/library"
)

func TestManagedLibraryReadRequiresTheSameRoot(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := library.Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	source := writeContentEPUB(t, t.TempDir())
	imported, err := library.Import(ctx, library.ImportRequest{Library: root, Source: source, Apply: true})
	if err != nil || imported.AssetID == "" {
		t.Fatal(imported, err)
	}
	got, err := Get(ctx, Request{SchemaVersion: 1, Catalog: root, Root: root, AssetID: imported.AssetID, MaxBytes: 4096})
	if err != nil || !strings.Contains(got.Text, "Hello") || got.CatalogID != imported.LibraryID || got.Status != "complete_range" {
		t.Fatal(got, err)
	}
	other := t.TempDir()
	if _, err := Get(ctx, Request{SchemaVersion: 1, Catalog: root, Root: other, AssetID: imported.AssetID, MaxBytes: 4096}); err == nil || !strings.Contains(err.Error(), "both catalog and root") {
		t.Fatal(err)
	}
	if _, err := Get(ctx, Request{SchemaVersion: 1, Catalog: root, Root: root, AssetID: "sha256:" + strings.Repeat("ab", 32), MaxBytes: 4096}); err == nil || !strings.Contains(err.Error(), "not present") {
		t.Fatal(err)
	}
}

func writeContentEPUB(t *testing.T, dir string) string {
	t.Helper()
	files := map[string]string{
		"mimetype":               "application/epub+zip",
		"META-INF/container.xml": `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`,
		"book.opf":               `<package xmlns="http://www.idpf.org/2007/opf"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Test</dc:title><dc:language>ja</dc:language></metadata><manifest><item id="c" href="chapter.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="c"/></spine></package>`,
		"chapter.xhtml":          `<html xmlns="http://www.w3.org/1999/xhtml"><body><p>Hello</p></body></html>`,
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	header, err := z.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := header.Write([]byte(files["mimetype"])); err != nil {
		t.Fatal(err)
	}
	delete(files, "mimetype")
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(files[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "book.epub")
	if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
