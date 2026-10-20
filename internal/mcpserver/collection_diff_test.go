package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/blisspixel/nemalo/internal/app"
)

func TestCollectionDiffThroughScopedReadOnlyMCP(t *testing.T) {
	// Scopes resolve parent aliases, including macOS /var and Windows 8.3 names.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	before, after := filepath.Join(dir, "before.json"), filepath.Join(dir, "after.json")
	old := `{"schema_version":1,"name":"Before","items":[{"source":"archive:demo","file":"book.epub","note":"Old"}]}`
	next := `{"schema_version":1,"name":"After","items":[{"source":"archive:demo","file":"book.epub","note":"New"},{"source":"gutenberg:84","file":"84.epub.noimages"}]}`
	for path, body := range map[string]string{before: old, after: next} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	service := app.Service{Providers: map[string]app.Searcher{"openlibrary": catalog{&calls}}, Scanner: noScanner{t}}
	session := connect(t, service, Options{Scopes: []string{dir}, Version: "test"})
	list, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range list.Tools {
		if tool.Name == "collection_diff" {
			a := tool.Annotations
			// DestructiveHint is defined only for tools that may write.
			if a == nil || !a.ReadOnlyHint || !a.IdempotentHint || a.OpenWorldHint == nil || *a.OpenWorldHint {
				t.Fatal("diff annotations", a)
			}
		}
	}
	res, env := call(t, session, "collection_diff", map[string]any{"before": before, "after": after})
	if res.IsError || env["error"] != nil || env["tool"] != "collection_diff" {
		t.Fatal(env)
	}
	direct, err := service.CollectionDiff(context.Background(), before, after)
	if err != nil || !direct.Complete || !direct.Changed || len(direct.Added) != 1 || len(direct.NotesChanged) != 1 {
		t.Fatal(direct, err)
	}
	want, _ := json.Marshal(direct)
	got, _ := json.Marshal(env["data"])
	var a, b any
	_ = json.Unmarshal(want, &a)
	_ = json.Unmarshal(got, &b)
	if !jsonEqual(a, b) {
		t.Fatalf("MCP diff differs from shared service\n%s\n%s", want, got)
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range []map[string]any{
		{"before": outside, "after": after}, {"before": before, "after": outside},
		{"before": before, "after": "relative.json"},
	} {
		res, env := call(t, session, "collection_diff", args)
		if !res.IsError || env["error"] == nil || env["data"] != nil {
			t.Fatal("scope refusal exposed partial data", env)
		}
	}
	// Separate explicit scopes are allowed; there is no implicit library root.
	twoScopes := connect(t, service, Options{Scopes: []string{dir, filepath.Dir(outside)}, Version: "test"})
	res, env = call(t, twoScopes, "collection_diff", map[string]any{"before": before, "after": outside})
	if res.IsError || env["data"].(map[string]any)["complete"] != true || env["data"].(map[string]any)["changed"] != false {
		t.Fatal("explicit independent scopes refused", env)
	}
	bad := filepath.Join(dir, "invalid.json")
	if err := os.WriteFile(bad, []byte(`{"schema_version":2}`), 0600); err != nil {
		t.Fatal(err)
	}
	res, env = call(t, session, "collection_diff", map[string]any{"before": before, "after": bad})
	if !res.IsError || env["error"] == nil || env["data"].(map[string]any)["complete"] != false || env["data"].(map[string]any)["before"].(map[string]any)["sha256"] != "" {
		t.Fatal("invalid input returned partial diff", env)
	}
	t.Run("file links stay scoped", func(t *testing.T) {
		insideLink, escape := filepath.Join(dir, "alias.json"), filepath.Join(dir, "escape.json")
		if err := os.Symlink(before, insideLink); err != nil {
			t.Skip("file links unavailable", err)
		}
		if err := os.Symlink(outside, escape); err != nil {
			t.Fatal(err)
		}
		res, env := call(t, session, "collection_diff", map[string]any{"before": insideLink, "after": after})
		if res.IsError || env["data"].(map[string]any)["before"].(map[string]any)["path"] != before {
			t.Fatal("in-scope explicit alias refused", env)
		}
		for _, args := range []map[string]any{{"before": escape, "after": after}, {"before": before, "after": escape}} {
			if res, env := call(t, session, "collection_diff", args); !res.IsError || env["data"] != nil {
				t.Fatal("link escaped configured scopes", env)
			}
		}
	})
	if calls != 0 {
		t.Fatal("comparison contacted a provider", calls)
	}
	for path, body := range map[string]string{before: old, after: next, outside: old} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != body {
			t.Fatal("comparison changed an input", path, err)
		}
	}
}
