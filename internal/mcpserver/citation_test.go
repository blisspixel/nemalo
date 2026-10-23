package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/library"
)

func citationInventory(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			files[path] = "directory"
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[path] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestCitationMCPIsScopedReadOnlyAndMatchesService(t *testing.T) {
	ctx := context.Background()
	scope, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(scope, "library")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	service := app.Service{Scanner: noScanner{t}}
	imported, err := service.Import(ctx, library.ImportRequest{Library: root, Source: epub(t, t.TempDir(), "Synthetic text."), Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(imported.Path))); err != nil {
		t.Fatal(err)
	}
	before := citationInventory(t, scope)
	session := connect(t, service, Options{Scopes: []string{scope}})
	found := false
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		if tool.Name == "library_citation" {
			found = tool.Annotations != nil && tool.Annotations.ReadOnlyHint
		}
	}
	if !found {
		t.Fatal("citation read-only tool absent")
	}
	res, env := call(t, session, "library_citation", map[string]any{"library": root, "asset": imported.AssetID})
	if res.IsError || env["error"] != nil {
		t.Fatal(env)
	}
	direct, err := service.Citation(ctx, root, imported.AssetID)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(direct)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(want, &value); err != nil || !jsonEqual(value, env["data"]) {
		t.Fatal("MCP differs from service", env, err)
	}
	for _, args := range []map[string]any{
		{"library": t.TempDir(), "asset": imported.AssetID}, {"library": root, "asset": "sha256:" + strings.Repeat("0", 64)}, {"library": root, "asset": "sha256:bad"},
	} {
		res, env := call(t, session, "library_citation", args)
		if !res.IsError || env["error"] == nil {
			t.Fatal("invalid/unscoped citation accepted", env)
		}
	}
	if !reflect.DeepEqual(before, citationInventory(t, scope)) {
		t.Fatal("citation MCP changed state")
	}
}
