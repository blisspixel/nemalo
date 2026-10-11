package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/config"
)

func epubFile(t *testing.T, dir string) string {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, f := range []struct{ name, body string }{
		{"mimetype", "application/epub+zip"},
		{"META-INF/container.xml", `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`},
		{"book.opf", `<package xmlns="http://www.idpf.org/2007/opf"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Rescan</dc:title><dc:language>en</dc:language></metadata><manifest><item id="c" href="c.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="c"/></spine></package>`},
		{"c.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><body><p>Text</p></body></html>`},
	} {
		method := zip.Deflate
		if f.name == "mimetype" {
			method = zip.Store
		}
		w, err := z.CreateHeader(&zip.FileHeader{Name: f.name, Method: method})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(f.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "book.epub")
	if err := os.WriteFile(p, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLibraryRescanCommand(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	s := app.Service{Scanner: fakeScanner("no_detections_reported")}
	run := func(args ...string) (int, string) {
		var out, errOut bytes.Buffer
		code := Execute(context.Background(), args, &out, &errOut, s, func(string) string { return "" }, func() (config.Paths, error) {
			return config.Paths{Config: filepath.Join(state, "none"), State: state}, nil
		}, nil)
		return code, out.String() + errOut.String()
	}
	if code, out := run("library", "init", root); code != 0 {
		t.Fatal(code, out)
	}
	code, out := run("library", "import", root, epubFile(t, t.TempDir()), "--apply")
	if code != 0 || !strings.Contains(out, `"unscanned"`) || !strings.Contains(out, "Next: Add --scan") {
		t.Fatal("unscanned import must name the next step", code, out)
	}
	code, out = run("library", "rescan", root)
	if code != 0 || !strings.Contains(out, "Promoted to checked: 1") || !strings.Contains(out, "Preview only. 1 status change") {
		t.Fatal(code, out)
	}
	code, out = run("library", "rescan", root, "--apply", "--json")
	if code != 0 || !strings.Contains(out, `"applied":true`) || !strings.Contains(out, `"after":"checked"`) {
		t.Fatal(code, out)
	}
	if code, out := run("library", "audit", root); code != 0 {
		t.Fatal("rescanned holding is not checked", out)
	}
	for _, args := range [][]string{{"library", "rescan"}, {"library", "rescan", root, "extra"}, {"library", "rescan", root, "--scan"}, {"library", "rescan", root, "--root", root}} {
		if code, _ := run(args...); code != 2 {
			t.Fatal("invalid rescan usage accepted", args, code)
		}
	}
}
