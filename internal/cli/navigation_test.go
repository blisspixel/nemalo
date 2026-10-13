package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/library"
)

func TestContentNavigationCLI(t *testing.T) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	stored, err := z.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = stored.Write([]byte("application/epub+zip")); err != nil {
		t.Fatal(err)
	}
	files := []struct{ name, data string }{
		{"META-INF/container.xml", `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`},
		{"book.opf", `<package xmlns="http://www.idpf.org/2007/opf"><metadata><title>Test</title><language>en</language></metadata><manifest><item id="c" href="chapter.xhtml" media-type="application/xhtml+xml"/><item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/></manifest><spine><itemref idref="c"/></spine></package>`},
		{"chapter.xhtml", `<html><body><h1 id="a">One</h1><p>ALPHAWORD.</p><h1 id="b">Two</h1><p>BETAWORD.</p></body></html>`},
		{"nav.xhtml", `<?xml version="1.0" encoding="UTF-8"?><html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><body><nav epub:type="toc"><ol><li><a href="chapter.xhtml#a">第一章</a></li><li><a href="chapter.xhtml#b">Second</a></li></ol></nav></body></html>`},
	}
	for _, file := range files {
		w, err := z.Create(file.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(file.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err = os.WriteFile(filepath.Join(root, "book.epub"), buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := library.Build(context.Background(), root, inventory.Defaults(), false)
	if err != nil {
		t.Fatal(err)
	}
	catalog := filepath.Join(t.TempDir(), "catalog.json")
	if err = library.Save(context.Background(), catalog, &s); err != nil {
		t.Fatal(err)
	}
	asset := s.Catalog.Assets[0].ID
	code, out, stderr := execute(t, []string{"content", "navigation", catalog, "--root", root, "--asset", asset}, nil)
	if code != 0 || stderr != "" || !strings.Contains(out, "第一章") || !strings.Contains(out, "Declared navigation:") || !strings.Contains(out, "nav-doc") {
		t.Fatal(code, out, stderr)
	}
	code, out, stderr = execute(t, []string{"content", "navigation", catalog, "--root", root, "--asset", asset, "--json"}, nil)
	if code != 0 || !strings.Contains(out, `"evidence":"nav-doc"`) || !strings.Contains(out, `"label":"第一章"`) || strings.Contains(out, "ALPHAWORD") {
		t.Fatal(code, out, stderr)
	}
	code, out, stderr = execute(t, []string{"content", "read", catalog, "--root", root, "--asset", asset, "--representation", "epub-nav/1", "--unit", "0", "--json"}, nil)
	if code != 0 || !strings.Contains(out, "ALPHAWORD") || strings.Contains(out, "BETAWORD") || !strings.Contains(out, `"range_status":"range_end"`) {
		t.Fatal(code, out, stderr)
	}
	code, out, _ = execute(t, []string{"content", "read", catalog, "--root", root, "--asset", asset, "--json"}, nil)
	if code != 0 || !strings.Contains(out, "ALPHAWORD") || !strings.Contains(out, "BETAWORD") {
		t.Fatal("spine read", code, out)
	}
	for _, args := range [][]string{
		{"content", "navigation", catalog, "--root", root, "--asset", asset, "--unit", "0"},
		{"content", "navigation", catalog, "--root", root, "--asset", asset, "--representation", "epub-text/2"},
		{"content", "units", catalog, "--root", root, "--asset", asset, "--navigation", "toc"},
	} {
		if code, out, _ := execute(t, args, nil); code != 2 {
			t.Fatal(args, code, out)
		}
	}
}
