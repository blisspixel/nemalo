package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/config"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/reader"
	"github.com/blisspixel/nemalo/internal/tui"
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
	// Human output is escaped readable text, never raw book control bytes, and
	// names the continuation explicitly when one exists.
	code, out, _ = execute(t, []string{"content", "read", catalog, "--root", root, "--asset", s.Catalog.Assets[0].ID}, nil)
	if code != 0 || !strings.Contains(out, "\nRead a little.") || !strings.Contains(out, "Representation: epub-text/2") || strings.ContainsAny(out, "\x1b\r") {
		t.Fatal(code, out)
	}
	code, out, _ = execute(t, []string{"content", "read", catalog, "--root", root, "--asset", s.Catalog.Assets[0].ID, "--max-bytes", "4"}, nil)
	if code != 0 || !strings.Contains(out, "Continue with: --cursor ") {
		t.Fatal(code, out)
	}
	code, out, _ = execute(t, []string{"content", "units", catalog, "--root", root, "--asset", s.Catalog.Assets[0].ID}, nil)
	if code != 0 || !strings.Contains(out, "Reading-order units: 1") || !strings.Contains(out, "supported plain text") {
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

func TestReadCLIOpensTheReaderAndPrintsWhereItClosed(t *testing.T) {
	asset := "sha256:" + strings.Repeat("a", 64)
	var got []tui.ReadOptions
	var stop *reader.Position
	var fail error
	saved := readTerminal
	readTerminal = func(_ context.Context, _ app.Service, _ io.Writer, o tui.ReadOptions) (*reader.Position, error) {
		got = append(got, o)
		return stop, fail
	}
	t.Cleanup(func() { readTerminal = saved })
	stop = &reader.Position{Unit: 3, Offset: 120}
	code, out, stderr := execute(t, []string{"read", "catalog.json", "--root", "books", "--asset", asset, "--speed", "--rate", "300", "--unit", "1", "--text-offset", "5"}, nil)
	if code != 0 || stderr != "" || !strings.Contains(out, "Closed at unit 3, byte 120. Nothing was recorded.") || !strings.Contains(out, "--unit 3 --text-offset 120") {
		t.Fatal(code, out, stderr)
	}
	if len(got) != 1 || got[0] != (tui.ReadOptions{Catalog: "catalog.json", Root: "books", Asset: asset, Start: reader.Position{Unit: 1, Offset: 5}, Speed: true, Rate: 300}) {
		t.Fatal(got)
	}
	stop = nil
	if code, out, _ := execute(t, []string{"read", "catalog.json", "--root", "books", "--asset", asset}, nil); code != 0 || out != "" || got[1].Rate != reader.DefaultRate || got[1].Speed {
		t.Fatal(code, out, got[1])
	}
	fail = errors.New("no terminal")
	if code, _, stderr := execute(t, []string{"read", "catalog.json", "--root", "books", "--asset", asset}, nil); code != 1 || !strings.Contains(stderr, "no terminal") {
		t.Fatal(code, stderr)
	}
	code, _, stderr = execute(t, []string{"read", "catalog.json", "--asset", asset}, nil)
	if code != 2 || !strings.Contains(stderr, "read needs --root") || !strings.Contains(stderr, "--root catalog.json") || strings.Contains(stderr, "has no --json") {
		t.Fatal(code, stderr)
	}
	code, _, stderr = execute(t, []string{"read", "catalog.json", "--root", "books"}, nil)
	if code != 2 || !strings.Contains(stderr, "read needs --asset") {
		t.Fatal(code, stderr)
	}
	code, out, stderr = execute(t, []string{"read", "catalog.json", "--root", "books", "--asset", asset, "--json"}, nil)
	if code != 2 || stderr != "" || !strings.Contains(out, "has no --json") || strings.Contains(out, "needs --root") {
		t.Fatal(code, out, stderr)
	}
	stdinTerminal = func() bool { return false }
	var plain, errOut bytes.Buffer
	if code := Execute(context.Background(), []string{"read", "catalog.json", "--root", "books", "--asset", asset}, &plain, &errOut, app.New(), func(string) string { return "" }, func() (config.Paths, error) {
		return config.Paths{Config: filepath.Join(t.TempDir(), "missing.json")}, nil
	}, nil); code != 2 || !strings.Contains(errOut.String(), "read requires an interactive terminal") || len(got) != 3 {
		t.Fatal(code, errOut.String(), len(got))
	}
	for _, args := range [][]string{
		{"read"},
		{"read", "catalog.json", "--asset", asset},
		{"read", "catalog.json", "--root", "books"},
		{"read", "catalog.json", "--root", "books", "--asset", asset, "--json"},
		{"read", "catalog.json", "--root", "books", "--asset", asset, "--rate", "99"},
		{"read", "catalog.json", "--root", "books", "--asset", asset, "--rate", "1001"},
		{"read", "catalog.json", "--root", "books", "--asset", "sha256:bad"},
		{"read", "catalog.json", "--root", "books", "--asset", asset, "--unit", "-1"},
		{"read", "catalog.json", "--root", "books", "--asset", asset, "--max-bytes", "10"},
		{"content", "read", "catalog.json", "--root", "books", "--asset", asset, "--speed"},
	} {
		if code, _, _ := execute(t, args, nil); code != 2 {
			t.Fatal(args, code)
		}
	}
	if len(got) != 3 {
		t.Fatal("invalid usage reached the terminal", got)
	}
}
