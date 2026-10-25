package mcpserver

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/scanner"
)

// noScanner fails the test if any tool reaches an installed scanner.
type noScanner struct{ t *testing.T }

func (n noScanner) Scan(context.Context, string) scanner.Result {
	n.t.Fatal("an MCP tool invoked a scanner")
	return scanner.Result{}
}

type catalog struct{ calls *int }

func (c catalog) Search(_ context.Context, q string, _, _ int) (discovery.Page, error) {
	*c.calls++
	return discovery.Page{Query: q, Order: "relevance", Results: []discovery.Result{}}, nil
}

func epub(t *testing.T, dir, body string) string {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, f := range []struct{ name, body string }{
		{"mimetype", "application/epub+zip"},
		{"META-INF/container.xml", `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`},
		{"book.opf", `<package xmlns="http://www.idpf.org/2007/opf"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Tool</dc:title><dc:language>en</dc:language></metadata><manifest><item id="c" href="c.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="c"/></spine></package>`},
		{"c.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><body>` + body + `</body></html>`},
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

// managedLibrary returns an initialized library holding one EPUB, and its asset.
func managedLibrary(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	if _, err := library.Initialize(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	r, err := library.Import(context.Background(), library.ImportRequest{Library: root, Source: epub(t, t.TempDir(), `<p>Before</p><img src="i.png" alt="A map"/><p>After the picture.</p>`), Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	return root, r.AssetID
}

func connect(t *testing.T, service app.Service, opts Options) *mcp.ClientSession {
	t.Helper()
	server, err := New(service, opts)
	if err != nil {
		t.Fatal(err)
	}
	st, ct := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = server.Run(ctx, st) }()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func call(t *testing.T, s *mcp.ClientSession, tool string, args any) (*mcp.CallToolResult, map[string]any) {
	t.Helper()
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatal(tool, err)
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var env map[string]any
	if err := json.Unmarshal(b, &env); err != nil {
		t.Fatal(tool, err, string(b))
	}
	return res, env
}

func toolNames(t *testing.T, s *mcp.ClientSession) []string {
	t.Helper()
	list, err := s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, tool := range list.Tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Fatal("tool not declared read-only", tool.Name)
		}
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names
}

func TestToolsAreReadOnlyAndNetworkIsExplicit(t *testing.T) {
	calls := 0
	service := app.Service{Providers: map[string]app.Searcher{"openlibrary": catalog{&calls}}, Scanner: noScanner{t}}
	offline := toolNames(t, connect(t, service, Options{Scopes: []string{t.TempDir()}, Version: "test"}))
	want := []string{"collection_diff", "collection_status", "content_capabilities", "content_locate", "content_navigation", "content_pages", "content_read", "content_resource", "content_source", "content_units", "file_check", "library_audit", "library_check", "library_citation", "library_list", "library_recordings", "library_related", "library_status", "nemalo_capabilities"}
	if !slices.Equal(offline, want) {
		t.Fatal("offline tools", offline)
	}
	online := connect(t, service, Options{Scopes: []string{t.TempDir()}, Network: true, Version: "test"})
	if names := toolNames(t, online); !slices.Contains(names, "catalog_search") || !slices.Contains(names, "catalog_evaluate") {
		t.Fatal("network tools missing with --allow-network", names)
	}
	res, env := call(t, online, "catalog_search", map[string]any{"query": "Verne"})
	if res.IsError || calls != 1 || env["tool"] != "catalog_search" {
		t.Fatal(res, env)
	}
	_, env = call(t, online, "nemalo_capabilities", map[string]any{})
	data := env["data"].(map[string]any)
	if data["read_only"] != true || data["network"] != true || len(data["tools"].([]any)) != 21 {
		t.Fatal("capabilities", data)
	}
}

func TestRecordingByteIdentitiesThroughReadOnlyMCP(t *testing.T) {
	ctx := context.Background()
	// MCP scopes resolve parent aliases, including macOS /var -> /private/var.
	// Give the direct service the same path so the entire result is comparable.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := library.Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	service := app.Service{Scanner: noScanner{t}}
	body := []byte("ID3 synthetic track")
	for _, item := range []string{"first", "mirror"} {
		packet := t.TempDir()
		receipt := fmt.Sprintf(`{"schema_version":1,"complete":true,"status":"untrusted_intake","antivirus":"not_scanned","transfer":{"bytes":%d,"sha256":"%x"},"intent":{"request":{"source_id":"archive:%s","source_file":"track.mp3","recording":{"source_format":"MP3","position":1,"tracks":1}}}}`, len(body), sha256.Sum256(body), item)
		for name, data := range map[string][]byte{"content.mp3": body, "receipt.json": []byte(receipt)} {
			if err := os.WriteFile(filepath.Join(packet, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := service.Import(ctx, library.ImportRequest{Library: root, Source: packet, Apply: true}); err != nil {
			t.Fatal(err)
		}
	}
	direct, err := service.Recordings(ctx, root, "")
	if err != nil || len(direct.ByteIdentities) != 1 || len(direct.ByteIdentities[0].Recordings) != 2 {
		t.Fatal(direct, err)
	}
	declaration := library.NarrationDeclaration{Schema: library.NarrationDeclarationSchema, NarrationID: "user-narration:explicit-reading", Recordings: []library.NarrationSelection{}}
	for _, ref := range direct.ByteIdentities[0].Recordings {
		declaration.Recordings = append(declaration.Recordings, library.NarrationSelection{SourceID: ref.SourceID, Format: ref.Format, Kind: ref.Kind, ByteIdentity: direct.ByteIdentities[0].ID})
	}
	declarationFile := filepath.Join(t.TempDir(), "declaration.json")
	declarationBytes, err := json.Marshal(declaration)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(declarationFile, declarationBytes, 0600); err != nil {
		t.Fatal(err)
	}
	preview, err := service.DeclareNarration(ctx, library.NarrationRequest{Library: root, File: declarationFile})
	if err != nil {
		t.Fatal(err)
	}
	if applied, err := service.DeclareNarration(ctx, library.NarrationRequest{Library: root, File: declarationFile, Apply: true, Plan: preview.Plan}); err != nil || !applied.Applied {
		t.Fatal(applied, err)
	}
	direct, err = service.Recordings(ctx, root, "")
	if err != nil || len(direct.UserNarrations) != 1 || len(direct.Declarations) != 1 || direct.Declarations[0].Evidence != library.UserNarrationEvidence {
		t.Fatal(direct, err)
	}
	session := connect(t, service, Options{Scopes: []string{root}, Version: "test"})
	res, env := call(t, session, "library_recordings", map[string]any{"library": root})
	if res.IsError || env["error"] != nil {
		t.Fatal(env)
	}
	want, _ := json.Marshal(direct)
	got, _ := json.Marshal(env["data"])
	var a, b any
	_ = json.Unmarshal(want, &a)
	_ = json.Unmarshal(got, &b)
	if !jsonEqual(a, b) {
		t.Fatalf("MCP recording identities differ from the service\n%s\n%s", want, got)
	}
	res, _ = call(t, session, "library_recordings", map[string]any{"library": t.TempDir()})
	if !res.IsError {
		t.Fatal("recording listing escaped the configured scope")
	}
}

func TestContentThroughMCPMatchesTheService(t *testing.T) {
	root, asset := managedLibrary(t)
	service := app.Service{Scanner: noScanner{t}}
	s := connect(t, service, Options{Scopes: []string{root}, Version: "test"})
	res, env := call(t, s, "content_read", map[string]any{"catalog": root, "root": root, "asset": asset, "max_bytes": 65536})
	if res.IsError || env["error"] != nil {
		t.Fatal(env)
	}
	direct, err := service.Content(context.Background(), content.Request{SchemaVersion: 1, Catalog: root, Root: root, AssetID: asset, MaxBytes: 65536})
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(direct)
	got, _ := json.Marshal(env["data"])
	var a, b any
	_ = json.Unmarshal(want, &a)
	_ = json.Unmarshal(got, &b)
	if !jsonEqual(a, b) {
		t.Fatalf("MCP result differs from the shared service\n%s\n%s", want, got)
	}
	if direct.Text != "Before\nAfter the picture.\n" || len(direct.Gaps) != 1 {
		t.Fatal(direct.Text, direct.Gaps)
	}
	_, env = call(t, s, "content_navigation", map[string]any{"catalog": root, "root": root, "asset": asset})
	navDirect, err := service.Content(context.Background(), content.Request{SchemaVersion: 1, Catalog: root, Root: root, AssetID: asset, MaxBytes: 4096, Representation: "epub-nav/1", UnitsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	want, _ = json.Marshal(navDirect)
	got, _ = json.Marshal(env["data"])
	_ = json.Unmarshal(want, &a)
	_ = json.Unmarshal(got, &b)
	if env["data"].(map[string]any)["status"] != "units_available" || !jsonEqual(a, b) {
		t.Fatal("navigation through MCP", env)
	}
	_, env = call(t, s, "content_units", map[string]any{"catalog": root, "root": root, "asset": asset})
	if env["data"].(map[string]any)["status"] != "units_available" {
		t.Fatal(env)
	}
	// A failed read is a tool error that still carries the stable status.
	res, env = call(t, s, "content_read", map[string]any{"catalog": root, "root": root, "asset": asset, "cursor": "not-a-cursor"})
	if !res.IsError || env["error"] == nil || env["data"].(map[string]any)["status"] != "invalid_request" || env["data"].(map[string]any)["text"] != nil {
		t.Fatal("failure facts lost", env)
	}
	_, env = call(t, s, "content_locate", map[string]any{"catalog": root, "root": root, "asset": asset, "text": "picture"})
	matches, _ := env["data"].(map[string]any)["matches"].([]any)
	if env["data"].(map[string]any)["status"] != "matches_found" || len(matches) != 1 {
		t.Fatal("locate through MCP", env)
	}
	if res, _ := call(t, s, "content_locate", map[string]any{"catalog": root, "root": root, "asset": asset}); !res.IsError {
		t.Fatal("locate without text accepted")
	}
	_, env = call(t, s, "library_list", map[string]any{"catalog": root, "status": "review"})
	if env["data"].(map[string]any)["total_holdings"] != float64(1) {
		t.Fatal("managed holdings through MCP", env)
	}
	_, env = call(t, s, "library_status", map[string]any{"library": root})
	if env["data"].(map[string]any)["status"] != "initialized" {
		t.Fatal(env)
	}
	res, env = call(t, s, "library_audit", map[string]any{"catalog": root})
	if env["data"].(map[string]any)["assets"] != float64(1) {
		t.Fatal(res, env)
	}
	before, _ := os.ReadFile(filepath.Join(root, ".nemalo", "holdings.jsonl"))
	res, env = call(t, s, "file_check", map[string]any{"file": filepath.Join(root, filepath.FromSlash("assets/sha256/"+asset[7:9]+"/"+asset[7:]+".epub"))})
	if res.IsError || env["data"].(map[string]any)["antivirus"].(map[string]any)["status"] != "not_scanned" {
		t.Fatal("check", env)
	}
	if after, _ := os.ReadFile(filepath.Join(root, ".nemalo", "holdings.jsonl")); !bytes.Equal(before, after) {
		t.Fatal("a read-only tool changed library state")
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

func TestPathsMustResolveInsideScopes(t *testing.T) {
	root, asset := managedLibrary(t)
	inside := t.TempDir()
	s := connect(t, app.Service{Scanner: noScanner{t}}, Options{Scopes: []string{inside}, Version: "test"})
	for _, args := range []map[string]any{
		{"catalog": root, "root": root, "asset": asset},
		{"catalog": "relative/catalog.json", "root": inside, "asset": asset},
		{"catalog": filepath.Join(inside, "missing.json"), "root": inside, "asset": asset},
		{"catalog": inside + string(filepath.Separator) + ".." + string(filepath.Separator) + filepath.Base(root), "root": inside, "asset": asset},
	} {
		res, env := call(t, s, "content_read", args)
		if !res.IsError || env["error"] == nil {
			t.Fatal("path outside scope accepted", args, env)
		}
	}
	link := filepath.Join(inside, "escape")
	if err := os.Symlink(root, link); err == nil {
		res, env := call(t, s, "library_status", map[string]any{"library": link})
		if !res.IsError || !strings.Contains(env["error"].(string), "not an accessible path inside the configured scopes") {
			t.Fatal("a link out of scope was followed", env)
		}
	}
}

// Refusals must not reveal whether a path outside the scopes exists, and paths
// outside every scope, such as UNC paths, are refused before any filesystem or
// network access (lexical containment comes first).
func TestScopeRefusalsAreUniformAndLexicalFirst(t *testing.T) {
	inside, outside := t.TempDir(), t.TempDir()
	s, err := newScope([]string{inside})
	if err != nil {
		t.Fatal(err)
	}
	existing, missing := filepath.Join(outside, "present"), filepath.Join(outside, "absent")
	if err := os.WriteFile(existing, nil, 0600); err != nil {
		t.Fatal(err)
	}
	_, errExisting := s.path(existing)
	_, errMissing := s.path(missing)
	if errExisting == nil || errMissing == nil || strings.ReplaceAll(errExisting.Error(), strconv.Quote(existing), "P") != strings.ReplaceAll(errMissing.Error(), strconv.Quote(missing), "P") {
		t.Fatal("refusal reveals existence", errExisting, errMissing)
	}
	for _, p := range []string{`\unreachable.invalid\share\a`, "//unreachable.invalid/share/a", `\?\C:\Windows`, `\.\C:\Windows`} {
		if filepath.IsAbs(p) {
			if _, err := s.path(p); err == nil {
				t.Fatal("network or device path accepted", p)
			}
		}
	}
	if _, err := s.path(filepath.Join(inside, "missing")); err == nil {
		t.Fatal("missing path inside the scope accepted")
	}
	f := filepath.Join(inside, "f")
	if err := os.WriteFile(f, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := s.path(f); err != nil || !strings.HasSuffix(got, "f") {
		t.Fatal(got, err)
	}
}

func TestServerConfiguration(t *testing.T) {
	if _, err := New(app.Service{}, Options{}); err == nil {
		t.Fatal("server without scope or network accepted")
	}
	for _, scopes := range [][]string{{"relative"}, {filepath.Join(t.TempDir(), "missing")}} {
		if _, err := New(app.Service{}, Options{Scopes: scopes}); err == nil {
			t.Fatal("invalid scope accepted", scopes)
		}
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(app.Service{}, Options{Scopes: []string{file}}); err == nil {
		t.Fatal("file scope accepted")
	}
	dir := t.TempDir()
	s, err := newScope([]string{dir, dir})
	if err != nil || len(s.roots) != 1 {
		t.Fatal("duplicate scopes", s, err)
	}
	if _, err := s.path(strings.Repeat("a", 5000)); err == nil {
		t.Fatal("oversized path accepted")
	}
	if got := boundDiagnostic(strings.Repeat("é", 2000)); len(got) > 2100 || !strings.HasSuffix(got, "(diagnostic truncated)") {
		t.Fatal("diagnostic not bounded")
	}
	if _, err := contentRequest(s, contentInput{Catalog: dir, Root: dir, Asset: "bad"}, "read"); err == nil {
		t.Fatal("invalid asset accepted")
	}
}

// pagedLibrary holds one EPUB whose navigation declares a page list.
func pagedLibrary(t *testing.T) (string, string) {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, f := range []struct{ name, body string }{
		{"mimetype", "application/epub+zip"},
		{"META-INF/container.xml", `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`},
		{"book.opf", `<package xmlns="http://www.idpf.org/2007/opf"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Paged</dc:title><dc:language>en</dc:language></metadata><manifest><item id="c" href="c.xhtml" media-type="application/xhtml+xml"/><item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/></manifest><spine><itemref idref="c"/></spine></package>`},
		{"c.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><body><p id="pi">Roman page.</p><p id="p1">First page.</p></body></html>`},
		{"nav.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><body><nav epub:type="toc"><ol><li><a href="c.xhtml">Text</a></li></ol></nav><nav epub:type="page-list" hidden=""><ol><li><a href="c.xhtml#pi">i</a></li><li><a href="c.xhtml#p1">1</a></li><li><a href="c.xhtml#p1">1</a></li></ol></nav></body></html>`},
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
	source := filepath.Join(t.TempDir(), "paged.epub")
	if err := os.WriteFile(source, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := library.Initialize(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	r, err := library.Import(context.Background(), library.ImportRequest{Library: root, Source: source, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	return root, r.AssetID
}

func TestPageListThroughMCPMatchesTheService(t *testing.T) {
	root, asset := pagedLibrary(t)
	service := app.Service{Scanner: noScanner{t}}
	s := connect(t, service, Options{Scopes: []string{root}, Version: "test"})
	_, env := call(t, s, "content_pages", map[string]any{"catalog": root, "root": root, "asset": asset})
	direct, err := service.Content(context.Background(), content.Request{SchemaVersion: 1, Catalog: root, Root: root, AssetID: asset, MaxBytes: 4096, Representation: "epub-page-list/1", UnitsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(direct)
	got, _ := json.Marshal(env["data"])
	var a, b any
	_ = json.Unmarshal(want, &a)
	_ = json.Unmarshal(got, &b)
	if direct.Status != "units_available" || len(direct.PageLists) != 1 || len(direct.PageLists[0].Entries) != 3 || !jsonEqual(a, b) {
		t.Fatalf("page list through MCP\n%s\n%s", want, got)
	}
	_, env = call(t, s, "content_read", map[string]any{"catalog": root, "root": root, "asset": asset, "representation": "epub-page-list/1", "label": "i"})
	data := env["data"].(map[string]any)
	if data["text"] == nil || !strings.HasPrefix(data["text"].(string), "Roman page.") || data["page"].(map[string]any)["label"] != "i" {
		t.Fatal("label read through MCP", env)
	}
	_, env = call(t, s, "content_read", map[string]any{"catalog": root, "root": root, "asset": asset, "representation": "epub-page-list/1", "label": "1"})
	if env["data"].(map[string]any)["status"] != "ambiguous_label" {
		t.Fatal("duplicate label through MCP", env)
	}
}

// Import tools exist only with Write. An apply stores exactly the plan a
// preview returned; a missing, forged, or stale plan writes nothing, and the
// source is never changed.
func TestImportToolsApplyOnlyAPreviewedPlan(t *testing.T) {
	root := t.TempDir()
	if _, err := library.Initialize(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	intake := t.TempDir()
	source := epub(t, intake, "<p>An imported page.</p>")
	before, _ := os.ReadFile(source)
	service := app.Service{Scanner: noScanner{t}}
	plain := connect(t, service, Options{Scopes: []string{root, intake}, Version: "test"})
	if names := toolNames(t, plain); slices.Contains(names, "library_import_apply") {
		t.Fatal("write tools without Write", names)
	}
	s := connect(t, service, Options{Scopes: []string{root, intake}, Write: true, Version: "test"})
	list, err := s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range list.Tools {
		destructive := tool.Annotations.DestructiveHint
		if tool.Name == "library_import_apply" && (tool.Annotations.ReadOnlyHint || destructive == nil || *destructive) {
			t.Fatal("apply annotations", tool.Annotations)
		}
	}
	_, env := call(t, s, "nemalo_capabilities", map[string]any{})
	if env["data"].(map[string]any)["read_only"] != false {
		t.Fatal("a write server reported read_only")
	}
	ops := filepath.Join(root, ".nemalo", "operations.jsonl")
	for name, args := range map[string]map[string]any{
		"no plan":       {"library": root, "source": source},
		"forged plan":   {"library": root, "source": source, "plan": strings.Repeat("a", 64)},
		"outside scope": {"library": root, "source": epub(t, t.TempDir(), "<p>x</p>"), "plan": strings.Repeat("a", 64)},
	} {
		if res, _ := call(t, s, "library_import_apply", args); !res.IsError {
			t.Fatal(name, "applied")
		}
		if _, err := os.Stat(ops); !os.IsNotExist(err) {
			t.Fatal(name, "a refused apply wrote an operation")
		}
	}
	if res, _ := call(t, s, "library_import_preview", map[string]any{"library": root, "source": source, "plan": strings.Repeat("a", 64)}); !res.IsError {
		t.Fatal("a preview accepted a plan")
	}
	res, env := call(t, s, "library_import_preview", map[string]any{"library": root, "source": source})
	preview := env["data"].(map[string]any)
	if res.IsError || preview["applied"] == true || len(preview["plan"].(string)) != 64 {
		t.Fatal(env)
	}
	res, env = call(t, s, "library_import_apply", map[string]any{"library": root, "source": source, "plan": preview["plan"]})
	done := env["data"].(map[string]any)
	if res.IsError || done["applied"] != true || done["asset_id"] != preview["asset_id"] {
		t.Fatal(env)
	}
	if after, _ := os.ReadFile(source); !bytes.Equal(after, before) {
		t.Fatal("the source changed")
	}
	held, err := library.FindManaged(context.Background(), root, "", "all", "all", 10, 0)
	if err != nil || held.Total != 1 {
		t.Fatal(held, err)
	}
}

func TestMCPImportRefusesChangedPacketProvenance(t *testing.T) {
	ctx := context.Background()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	intake, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := library.Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	source := epub(t, intake, "<p>Metadata-bound import.</p>")
	body, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(source, filepath.Join(intake, "content.epub")); err != nil {
		t.Fatal(err)
	}
	writeReceipt := func(id string) {
		t.Helper()
		b, err := json.Marshal(map[string]any{"schema_version": 1, "complete": true, "status": "untrusted_intake", "transfer": map[string]any{"bytes": len(body), "sha256": fmt.Sprintf("%x", sha256.Sum256(body))}, "intent": map[string]any{"request": map[string]string{"source_id": id, "source_file": "book.epub"}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(intake, "receipt.json"), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeReceipt("archive:before")
	service := app.Service{Scanner: noScanner{t}}
	s := connect(t, service, Options{Scopes: []string{root, intake}, Write: true, Version: "test"})
	res, env := call(t, s, "library_import_preview", map[string]any{"library": root, "source": intake})
	if res.IsError {
		t.Fatal(env)
	}
	preview := env["data"].(map[string]any)
	if preview["source_evidence"].(map[string]any)["source_id"] != "archive:before" || preview["holding"] == nil {
		t.Fatal(env)
	}
	writeReceipt("archive:after")
	res, env = call(t, s, "library_import_apply", map[string]any{"library": root, "source": intake, "plan": preview["plan"]})
	if !res.IsError {
		t.Fatal("stale metadata applied", env)
	}
	refused := env["data"].(map[string]any)
	if refused["plan"] != nil || refused["holding"] != nil {
		t.Fatal("refusal offered unreviewed proposal", env)
	}
	for _, name := range []string{"operations.jsonl", "holdings.jsonl"} {
		if _, err := os.Stat(filepath.Join(root, ".nemalo", name)); !os.IsNotExist(err) {
			t.Fatal("refusal wrote", name, err)
		}
	}
	res, env = call(t, s, "library_import_preview", map[string]any{"library": root, "source": intake})
	if res.IsError {
		t.Fatal(env)
	}
	fresh := env["data"].(map[string]any)
	direct, err := service.Import(ctx, library.ImportRequest{Library: root, Source: intake})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(direct)
	if err != nil {
		t.Fatal(err)
	}
	var want any
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal(err)
	}
	if !jsonEqual(want, fresh) {
		t.Fatal("MCP differs from shared service", want, fresh)
	}
	res, env = call(t, s, "library_import_apply", map[string]any{"library": root, "source": intake, "plan": fresh["plan"]})
	if res.IsError || !jsonEqual(env["data"].(map[string]any)["holding"], fresh["holding"]) {
		t.Fatal(env)
	}
}
