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
	if code != 0 || !strings.Contains(out, "unscanned") || !strings.Contains(out, "Next: Add --scan") {
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

func TestLibraryListManagedHoldings(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	s := app.Service{Scanner: fakeScanner("no_detections_reported")}
	run := func(args ...string) (int, string) {
		var out, errOut bytes.Buffer
		code := Execute(context.Background(), args, &out, &errOut, s, func(string) string { return "" }, func() (config.Paths, error) {
			return config.Paths{Config: filepath.Join(state, "none"), State: state}, nil
		}, nil)
		return code, out.String() + errOut.String()
	}
	run("library", "init", root)
	run("library", "import", root, epubFile(t, t.TempDir()), "--apply", "--scan")
	code, out := run("library", "list", root, "--status", "checked")
	if code != 0 || !strings.Contains(out, "Matching holdings: 1") || !strings.Contains(out, "\nRescan\n") {
		t.Fatal(code, out)
	}
	code, out = run("library", "list", root, "--status", "review", "--json")
	if code != 0 || !strings.Contains(out, `"total_holdings":0`) {
		t.Fatal(code, out)
	}
	catalog := filepath.Join(t.TempDir(), "catalog.json")
	if code, out := run("library", "snapshot", t.TempDir(), "--output", catalog); code != 0 {
		t.Fatal(out)
	}
	for _, args := range [][]string{{"library", "list", catalog, "--status", "checked"}, {"library", "list", root, "--status", "pending"}} {
		if code, _ := run(args...); code == 0 {
			t.Fatal("invalid status filter accepted", args)
		}
	}
}

func TestLibraryOpenAndExportCommands(t *testing.T) {
	root, state, dest := t.TempDir(), t.TempDir(), t.TempDir()
	opened := ""
	s := app.Service{Scanner: fakeScanner("no_detections_reported"), Launch: func(_ context.Context, _, file string) error {
		opened = file
		return nil
	}}
	run := func(args ...string) (int, string) {
		var out, errOut bytes.Buffer
		code := Execute(context.Background(), args, &out, &errOut, s, func(string) string { return "" }, func() (config.Paths, error) {
			return config.Paths{Config: filepath.Join(state, "none"), State: state}, nil
		}, nil)
		return code, out.String() + errOut.String()
	}
	run("library", "init", root)
	_, out := run("library", "import", root, epubFile(t, t.TempDir()), "--apply", "--json")
	asset := out[strings.Index(out, `"asset_id":"`)+12:]
	asset = asset[:strings.Index(asset, `"`)]
	code, out := run("library", "open", root, "--asset", asset)
	if code != 0 || !strings.HasPrefix(opened, filepath.Join(state, "handoff")) || !strings.Contains(out, "Copy state: read-only") {
		t.Fatal(code, out, opened)
	}
	if code, out := run("library", "export", root, "--asset", asset, "--output", dest, "--json"); code != 0 || !strings.Contains(out, `"read_only":false`) {
		t.Fatal(code, out)
	}
	if code, _ := run("library", "export", root, "--asset", asset, "--output", dest); code != 1 {
		t.Fatal("export overwrote an existing copy")
	}
	for _, args := range [][]string{{"library", "open", root}, {"library", "export", root, "--asset", asset}, {"library", "open", root, "--asset", asset, "--output", dest}, {"library", "export", root, "--asset", asset, "--output", dest, "--app", "x"}} {
		if code, _ := run(args...); code != 2 {
			t.Fatal("invalid open/export usage accepted", args, code)
		}
	}
}

func TestLibraryIngestCommand(t *testing.T) {
	root, state, folder := t.TempDir(), t.TempDir(), t.TempDir()
	epubFile(t, folder)
	if err := os.WriteFile(filepath.Join(folder, "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := app.Service{Scanner: fakeScanner("no_detections_reported")}
	run := func(args ...string) (int, string) {
		var out, errOut bytes.Buffer
		code := Execute(context.Background(), args, &out, &errOut, s, func(string) string { return "" }, func() (config.Paths, error) {
			return config.Paths{Config: filepath.Join(state, "none"), State: state}, nil
		}, nil)
		return code, out.String() + errOut.String()
	}
	run("library", "init", root)
	code, out := run("library", "ingest", root, folder)
	if code != 0 || !strings.Contains(out, "Preview only") || !strings.Contains(out, "Imported: 1") {
		t.Fatal(code, out)
	}
	code, out = run("library", "ingest", root, folder, "--apply", "--scan", "--json")
	if code != 0 || !strings.Contains(out, `"checked":1`) || !strings.Contains(out, `"not_imported":1`) {
		t.Fatal(code, out)
	}
	for _, args := range [][]string{{"library", "ingest", root}, {"library", "ingest", root, folder, "--asset", "x"}} {
		if code, _ := run(args...); code != 2 {
			t.Fatal("invalid ingest usage accepted", args)
		}
	}
}
