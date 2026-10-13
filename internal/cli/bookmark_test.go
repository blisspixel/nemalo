package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/library"
)

func TestBookmarkCommandsKeepExplicitLocations(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := library.Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	first := holdCLIBook(t, ctx, root, "one.epub", cliEPUB("ALPHAWORD stays. BETAWORD follows."))
	second := holdCLIBook(t, ctx, root, "two.epub", cliEPUB("OTHERWORD separate."))
	journal := filepath.Join(root, ".nemalo", "bookmarks.jsonl")
	code, out, stderr := execute(t, []string{"content", "read", root, "--root", root, "--asset", first.AssetID, "--representation", "epub-text/2", "--json"}, nil)
	read := decodeCLI[contentJSON](t, out)
	if code != 0 || stderr != "" || !strings.Contains(read.Data.Text, "ALPHAWORD") || read.Data.RepresentationID == "" {
		t.Fatal(code, out, stderr)
	}
	if _, err := os.Stat(journal); !os.IsNotExist(err) {
		t.Fatal("reading created a bookmark journal", err)
	}
	code, out, stderr = execute(t, []string{"bookmark", "resume", root, "--asset", first.AssetID, "--json"}, nil)
	if code == 0 || !strings.Contains(out+stderr, "bookmark enable") {
		t.Fatal(code, out, stderr)
	}
	code, out, stderr = execute(t, []string{"bookmark", "enable", root}, nil)
	if code != 0 || !strings.Contains(out, "Bookmarks: enabled") || !strings.Contains(out, "Notes are not part of the book") {
		t.Fatal(code, out, stderr)
	}
	for _, args := range [][]string{
		{"bookmark", "enable", root, "--asset", first.AssetID},
		{"bookmark", "save", root},
		{"bookmark", "import", root, "--output", filepath.Join(t.TempDir(), "nope.json")},
		{"bookmark", "nope", root},
		{"bookmark", "note", root},
	} {
		if code, out, _ := execute(t, args, nil); code != 2 {
			t.Fatal(args, code, out)
		}
	}
	code, _, _ = execute(t, []string{"bookmark", "save", root, "--asset", first.AssetID, "--representation", "epub-text/2", "--representation-id", "sha256:" + strings.Repeat("ab", 32), "--unit", "0", "--text-offset", "0"}, nil)
	if code == 0 {
		t.Fatal("changed representation id was stored")
	}
	enabled := mustCLIRead(t, journal)
	saveArgs := []string{"bookmark", "save", root, "--asset", first.AssetID, "--representation", "epub-text/2", "--representation-id", read.Data.RepresentationID, "--unit", itoa(read.Data.Locator.Unit), "--text-offset", itoa(read.Data.Locator.Start), "--json"}
	code, out, stderr = execute(t, saveArgs, nil)
	saved := decodeCLI[bookmarkJSON](t, out)
	if code != 0 || stderr != "" || saved.Data.AssetID != first.AssetID || saved.Data.Offset != read.Data.Locator.Start {
		t.Fatal(code, out, stderr)
	}
	held := mustCLIRead(t, journal)
	code, _, stderr = execute(t, saveArgs, nil)
	if code != 0 || stderr != "" || !bytes.Equal(held, mustCLIRead(t, journal)) {
		t.Fatal("duplicate save appended", code, stderr)
	}
	if bytes.Equal(enabled, held) {
		t.Fatal("save did not record a location")
	}
	code, out, stderr = execute(t, []string{"bookmark", "note", root, "--asset", first.AssetID, "--text", "margin note, not the book", "--json"}, nil)
	if code != 0 || !strings.Contains(out, "margin note, not the book") {
		t.Fatal(code, out, stderr)
	}
	secondRead := contentRead(t, root, second.AssetID)
	code, out, stderr = execute(t, []string{"bookmark", "save", root, "--asset", second.AssetID, "--representation", "epub-text/2", "--representation-id", secondRead.Data.RepresentationID, "--unit", itoa(secondRead.Data.Locator.Unit), "--text-offset", itoa(secondRead.Data.Locator.Start)}, nil)
	if code != 0 {
		t.Fatal(code, out, stderr)
	}
	code, out, stderr = execute(t, []string{"bookmark", "list", root, "--json"}, nil)
	listed := decodeCLI[bookmarkStateJSON](t, out)
	if code != 0 || len(listed.Data.Bookmarks) != 2 {
		t.Fatal(code, out, stderr)
	}
	beforeRead := mustCLIRead(t, journal)
	if code, _, stderr = execute(t, []string{"content", "read", root, "--root", root, "--asset", first.AssetID, "--representation", "epub-text/2"}, nil); code != 0 || stderr != "" {
		t.Fatal(code, stderr)
	}
	if !bytes.Equal(beforeRead, mustCLIRead(t, journal)) {
		t.Fatal("reading moved a bookmark")
	}
	exportPath := filepath.Join(t.TempDir(), "bookmarks.json")
	code, out, stderr = execute(t, []string{"bookmark", "export", root, "--output", exportPath, "--json"}, nil)
	exported, err := os.ReadFile(exportPath)
	if code != 0 || err != nil || !bytes.Contains(exported, []byte(`"kind":"nemalo-bookmarks/1"`)) || !bytes.Contains(exported, []byte("margin note, not the book")) {
		t.Fatal(code, out, stderr, err)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err = os.WriteFile(bad, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	held = mustCLIRead(t, journal)
	if code, _, _ = execute(t, []string{"bookmark", "import", root, "--input", bad}, nil); code == 0 || !bytes.Equal(held, mustCLIRead(t, journal)) {
		t.Fatal("malformed import wrote", code)
	}
	code, out, stderr = execute(t, []string{"bookmark", "resume", root, "--asset", first.AssetID, "--json"}, nil)
	resumed := decodeCLI[resumeJSON](t, out)
	if code != 0 || resumed.Data.Status != "restored" || resumed.Data.Content == nil || !strings.Contains(resumed.Data.Content.Text, "ALPHAWORD") || strings.Contains(resumed.Data.Content.Text, "margin note") || resumed.Data.Bookmark.Note != "margin note, not the book" {
		t.Fatal(code, out, stderr)
	}
	code, out, stderr = execute(t, []string{"bookmark", "delete", root, "--asset", first.AssetID, "--json"}, nil)
	deleted := decodeCLI[bookmarkStateJSON](t, out)
	if code != 0 || len(deleted.Data.Bookmarks) != 1 || deleted.Data.Bookmarks[0].AssetID != second.AssetID {
		t.Fatal(code, out, stderr)
	}
	code, out, stderr = execute(t, []string{"bookmark", "import", root, "--input", exportPath}, nil)
	if code != 0 || !strings.Contains(out, first.AssetID) {
		t.Fatal(code, out, stderr)
	}
	stored := filepath.Join(root, filepath.FromSlash(first.Path))
	body, err := os.ReadFile(stored)
	if err != nil || len(body) < 2 {
		t.Fatal(err)
	}
	body[len(body)-1] ^= 0xff
	if err = os.WriteFile(stored, body, 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, stderr = execute(t, []string{"bookmark", "resume", root, "--asset", first.AssetID, "--json"}, nil)
	changed := decodeCLI[resumeJSON](t, out)
	if code == 0 || changed.Data.Status != "changed_source" || changed.Data.Content != nil || changed.Data.Bookmark.Note != "margin note, not the book" || strings.Contains(out, "ALPHAWORD") {
		t.Fatal(code, out, stderr)
	}
	code, out, stderr = execute(t, []string{"bookmark", "resume", root, "--asset", second.AssetID, "--json"}, nil)
	otherResume := decodeCLI[resumeJSON](t, out)
	if code != 0 || otherResume.Data.Status != "restored" || otherResume.Data.Content == nil || !strings.Contains(otherResume.Data.Content.Text, "OTHERWORD") {
		t.Fatal(code, out, stderr)
	}
	if code, _, stderr = execute(t, []string{"bookmark", "disable", root}, nil); code != 0 || stderr != "" {
		t.Fatal(code, stderr)
	}
	code, out, stderr = execute(t, []string{"bookmark", "resume", root, "--asset", second.AssetID, "--json"}, nil)
	paused := decodeCLI[resumeJSON](t, out)
	if code == 0 || paused.Data.Status != "not_enabled" || paused.Data.Content != nil {
		t.Fatal(code, out, stderr)
	}
	if code, _, _ = execute(t, []string{"bookmark", "save", root, "--asset", second.AssetID, "--representation", "epub-text/2", "--representation-id", secondRead.Data.RepresentationID, "--unit", "0", "--text-offset", "0"}, nil); code == 0 {
		t.Fatal("save while disabled wrote")
	}
	code, out, _ = execute(t, nil, nil)
	if code != 0 || !strings.Contains(out, "bookmark enable") || !strings.Contains(out, "MCP does not read or write") {
		t.Fatal(out)
	}
}

func contentRead(t *testing.T, root, asset string) envelope[contentJSON] {
	t.Helper()
	code, out, stderr := execute(t, []string{"content", "read", root, "--root", root, "--asset", asset, "--representation", "epub-text/2", "--json"}, nil)
	got := decodeCLI[contentJSON](t, out)
	if code != 0 || stderr != "" || got.Data.RepresentationID == "" || got.Data.Locator == nil {
		t.Fatal(code, out, stderr)
	}
	return got
}

func decodeCLI[T any](t *testing.T, out string) envelope[T] {
	t.Helper()
	var got envelope[T]
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err, out)
	}
	return got
}

type envelope[T any] struct {
	Data  T      `json:"data"`
	Error string `json:"error"`
}

type contentJSON struct {
	Text             string `json:"text"`
	RepresentationID string `json:"representation_id"`
	Locator          *struct {
		Unit  int `json:"unit"`
		Start int `json:"start"`
	} `json:"locator"`
}

type bookmarkJSON struct {
	AssetID string `json:"asset_id"`
	Offset  int    `json:"offset"`
	Note    string `json:"note"`
}

type bookmarkStateJSON struct {
	Bookmarks []bookmarkJSON `json:"bookmarks"`
}

type resumeJSON struct {
	Status   string       `json:"status"`
	Bookmark bookmarkJSON `json:"bookmark"`
	Content  *contentJSON `json:"content"`
}

func itoa(n int) string { return strconv.Itoa(n) }

func mustCLIRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func holdCLIBook(t *testing.T, ctx context.Context, root, name string, data []byte) library.ImportResult {
	t.Helper()
	src := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(src, data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := library.Import(ctx, library.ImportRequest{Library: root, Source: src, Apply: true})
	if err != nil || got.AssetID == "" || got.Path == "" {
		t.Fatal(got, err)
	}
	return got
}

func cliEPUB(body string) []byte {
	files := map[string]string{
		"META-INF/container.xml": `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`,
		"book.opf":               `<package xmlns="http://www.idpf.org/2007/opf"><metadata><title>Fixture</title><language>en</language></metadata><manifest><item id="u" href="unit.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="u"/></spine></package>`,
		"unit.xhtml":             "<html><body><p>" + body + "</p></body></html>",
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	f, err := z.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		panic(err)
	}
	if _, err = f.Write([]byte("application/epub+zip")); err != nil {
		panic(err)
	}
	for _, name := range []string{"META-INF/container.xml", "book.opf", "unit.xhtml"} {
		w, err := z.Create(name)
		if err != nil {
			panic(err)
		}
		if _, err = w.Write([]byte(files[name])); err != nil {
			panic(err)
		}
	}
	if err = z.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}
