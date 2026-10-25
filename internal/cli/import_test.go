package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/config"
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
	if code != 0 || !strings.Contains(out, "Preview only") || !strings.Contains(out, "Status: review") || !strings.Contains(out, "Title: Test") {
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

func TestCLIImportPlanBindsPacketDeclarations(t *testing.T) {
	state := t.TempDir()
	service := app.Service{Providers: map[string]app.Searcher{"archive": searcher{nil}, "openlibrary": searcher{nil}}, Scanner: fakeScanner("unavailable")}
	run := func(args []string) (int, string, string) {
		t.Helper()
		var out, stderr bytes.Buffer
		code := Execute(context.Background(), args, &out, &stderr, service, func(string) string { return "" }, func() (config.Paths, error) {
			return config.Paths{Config: filepath.Join(state, "missing.json"), State: state}, nil
		}, nil)
		return code, out.String(), stderr.String()
	}
	root := t.TempDir()
	packet := t.TempDir()
	if code, out, _ := run([]string{"library", "init", root}); code != 0 {
		t.Fatal(out)
	}
	source := writeCLIEPUB(t, packet)
	body, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(source, filepath.Join(packet, "content.epub")); err != nil {
		t.Fatal(err)
	}
	writeReceipt := func(id string) {
		t.Helper()
		b, err := json.Marshal(map[string]any{"schema_version": 1, "complete": true, "status": "untrusted_intake", "transfer": map[string]any{"bytes": len(body), "sha256": fmt.Sprintf("%x", sha256.Sum256(body))}, "intent": map[string]any{"request": map[string]string{"source_id": id, "source_file": "book.epub"}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(packet, "receipt.json"), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeReceipt("archive:before")
	code, out, _ := run([]string{"library", "import", root, packet, "--json"})
	var env struct {
		Data library.ImportResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil || code != 0 || env.Data.SourceEvidence == nil || env.Data.Holding == nil || env.Data.SourceEvidence.SourceID != "archive:before" {
		t.Fatal(out, err)
	}
	preview := env.Data
	writeReceipt("archive:after")
	env.Data = library.ImportResult{}
	code, out, _ = run([]string{"library", "import", root, packet, "--apply", "--plan", preview.Plan, "--json"})
	if err := json.Unmarshal([]byte(out), &env); err != nil || code != 1 || env.Data.Plan != "" || env.Data.Applied {
		t.Fatal(out, err)
	}
	for _, name := range []string{"operations.jsonl", "holdings.jsonl"} {
		if _, err := os.Stat(filepath.Join(root, ".nemalo", name)); !os.IsNotExist(err) {
			t.Fatal("refusal wrote", name, err)
		}
	}
	code, out, _ = run([]string{"library", "import", root, packet})
	if code != 0 || !strings.Contains(out, "Incoming source evidence (declarations):") || !strings.Contains(out, "Source ID: archive:after") || !strings.Contains(out, "Proposed holding sources: 1") {
		t.Fatal(code, out)
	}
	code, out, _ = run([]string{"library", "import", root, packet, "--json"})
	env.Data = library.ImportResult{}
	if err := json.Unmarshal([]byte(out), &env); err != nil || code != 0 {
		t.Fatal(out, err)
	}
	fresh := env.Data
	code, out, _ = run([]string{"library", "import", root, packet, "--apply", "--plan", fresh.Plan, "--json"})
	env.Data = library.ImportResult{}
	if err := json.Unmarshal([]byte(out), &env); err != nil || code != 0 || !env.Data.Applied || !reflect.DeepEqual(env.Data.Holding, fresh.Holding) {
		t.Fatal(out, err)
	}
}
