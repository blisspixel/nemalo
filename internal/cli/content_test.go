package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/library"
)

func TestContentCLIContract(t *testing.T) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	f, err := z.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write([]byte("application/epub+zip")); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct{ name, data string }{
		{"META-INF/container.xml", `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`},
		{"book.opf", `<package xmlns="http://www.idpf.org/2007/opf"><metadata><title>Test</title><language>en</language></metadata><manifest><item id="c" href="chapter.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="c"/></spine></package>`},
		{"chapter.xhtml", "<html><body><p>Read a little. 日本語</p></body></html>"},
	} {
		f, err := z.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write([]byte(entry.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "book.epub"), buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := library.Build(context.Background(), root, inventory.Defaults(), false)
	if err != nil {
		t.Fatal(err)
	}
	catalog := filepath.Join(t.TempDir(), "catalog.json")
	if err := library.Save(context.Background(), catalog, &s); err != nil {
		t.Fatal(err)
	}
	base := []string{"content", "read", catalog, "--root", root, "--asset", s.Catalog.Assets[0].ID, "--json", "--max-bytes", "4"}
	code, out, stderr := execute(t, base, nil)
	var envelope struct {
		SchemaVersion int            `json:"schema_version"`
		Command       string         `json:"command"`
		Data          content.Result `json:"data"`
		Error         string         `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || code != 0 || stderr != "" || envelope.Command != "content" || envelope.SchemaVersion != 1 || envelope.Data.Text != "Read" || envelope.Data.Continuation == "" {
		t.Fatal(code, out, stderr, err)
	}
	code, out, _ = execute(t, append(append([]string{}, base...), "--cursor", envelope.Data.Continuation), nil)
	if code != 0 || !strings.Contains(out, `"start":4`) {
		t.Fatal(code, out)
	}
	listing := []string{"content", "units", catalog, "--root", root, "--asset", s.Catalog.Assets[0].ID, "--json"}
	code, out, _ = execute(t, listing, nil)
	if code != 0 || !strings.Contains(out, "units_available") || !strings.Contains(out, "epub_reading_order_document") || strings.Contains(out, `"text":`) {
		t.Fatal(code, out)
	}
	for _, args := range [][]string{
		{"content"}, {"content", "read", catalog}, {"content", "bad", catalog},
		append(append([]string{}, base...), "--scan"),
		append(append([]string{}, base...), "--max-bytes", "3"),
		append(append([]string{}, base...), "--cursor", "token", "--unit", "0"),
		append(append([]string{}, listing...), "--unit", "0"),
	} {
		if code, out, _ := execute(t, args, nil); code != 2 {
			t.Fatal(args, code, out)
		}
	}
	code, out, _ = execute(t, append(append([]string{}, base...), "--cursor", "invalid"), nil)
	if code != 1 || !strings.Contains(out, `"status":"invalid_request"`) || strings.Contains(out, `"text":`) {
		t.Fatal(code, out)
	}
	// Human output retains JSON escaping rather than emitting book control bytes.
	code, out, _ = execute(t, []string{"content", "read", catalog, "--root", root, "--asset", s.Catalog.Assets[0].ID}, nil)
	if code != 0 || !strings.Contains(out, `"text": "Read a little.`) {
		t.Fatal(code, out)
	}
	code, out, _ = execute(t, []string{"content", "capabilities", "--json"}, nil)
	if code != 0 || !strings.Contains(out, "epub-structure/1") || !strings.Contains(out, "epub-source/1") {
		t.Fatal(code, out)
	}
	rich := append(append([]string{}, base...), "--representation", "epub-structure/1")
	code, out, _ = execute(t, rich, nil)
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || code != 0 || len(envelope.Data.Parts) == 0 || envelope.Data.Coverage == nil {
		t.Fatal(code, out, err)
	}
	source := []string{"content", "source", catalog, "--root", root, "--asset", s.Catalog.Assets[0].ID, "--cursor", envelope.Data.Parts[0].SourceReference, "--json"}
	code, out, _ = execute(t, source, nil)
	if code != 0 || !strings.Contains(out, "member_utf8_bytes") || !strings.Contains(out, `\u003cp\u003eRead`) {
		t.Fatal(code, out)
	}
	code, out, _ = execute(t, []string{"content", "resource", catalog, "--root", root, "--asset", s.Catalog.Assets[0].ID, "--json"}, nil)
	if code != 1 || !strings.Contains(out, "invalid_request") {
		t.Fatal(code, out)
	}
}
