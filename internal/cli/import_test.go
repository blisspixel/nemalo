package cli

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/library"
)

func TestLibraryImportPreviewApplyAuditAndRead(t *testing.T) {
	libraryRoot := t.TempDir()
	source := writeCLIEPUB(t, t.TempDir())
	if code, _, _ := execute(t, []string{"library", "import", libraryRoot, source, "--apply"}, nil); code != 1 {
		t.Fatal("uninitialized import accepted", code)
	}
	if _, err := os.Stat(filepath.Join(libraryRoot, ".nemalo")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("import created control state")
	}
	if code, _, _ := execute(t, []string{"library", "init", libraryRoot}, nil); code != 0 {
		t.Fatal(code)
	}
	code, out, _ := execute(t, []string{"library", "import", libraryRoot, source}, nil)
	if code != 0 || !strings.Contains(out, "Preview only") || !strings.Contains(out, `Status: "review"`) || !strings.Contains(out, `Title: "Test"`) {
		t.Fatal(code, out)
	}
	if _, err := os.Stat(filepath.Join(libraryRoot, ".nemalo", "operations.jsonl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preview wrote", err)
	}
	code, out, _ = execute(t, []string{"library", "import", libraryRoot, source, "--apply", "--json"}, nil)
	if code != 0 || !strings.Contains(out, `"applied":true`) || !strings.Contains(out, `"status":"review"`) {
		t.Fatal(code, out)
	}
	var envelope struct {
		Data library.ImportResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || envelope.Data.AssetID == "" {
		t.Fatal(envelope, err)
	}
	code, out, _ = execute(t, []string{"content", "read", libraryRoot, "--root", libraryRoot, "--asset", envelope.Data.AssetID}, nil)
	if code != 0 || !strings.Contains(out, "Hello") {
		t.Fatal(code, out)
	}
	code, out, _ = execute(t, []string{"library", "audit", libraryRoot}, nil)
	if code != 1 || !strings.Contains(out, `Review:`) || !strings.Contains(out, "unscanned") {
		t.Fatal(code, out)
	}
	for _, args := range [][]string{
		{"library", "import", libraryRoot},
		{"library", "import", libraryRoot, source, "--output", "x"},
		{"library", "audit", libraryRoot, "--root", t.TempDir()},
		{"library", "audit", libraryRoot, "--max-entries", "1"},
	} {
		if code, _, _ := execute(t, args, nil); code != 2 {
			t.Fatal("invalid import or managed audit accepted", args, code)
		}
	}
}

func writeCLIEPUB(t *testing.T, dir string) string {
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
