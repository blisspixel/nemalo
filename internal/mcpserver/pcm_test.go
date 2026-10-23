package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/audio/audiotest"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/decode/decodetest"
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/library"
)

func TestPCMToolsRequireIndependentOperatorPermissionAndPerCallSelection(t *testing.T) {
	scope := t.TempDir()
	root := filepath.Join(scope, "books")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	data := audiotest.MP4(4)
	source := filepath.Join(root, "track.m4b")
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := library.Build(context.Background(), root, inventory.Defaults(), false)
	if err != nil {
		t.Fatal(err)
	}
	catalog := filepath.Join(scope, "catalog.json")
	if err := library.Save(context.Background(), catalog, &snapshot); err != nil {
		t.Fatal(err)
	}
	asset := snapshot.Catalog.Assets[0].ID
	fake := decodetest.NewPCM(data)
	service := app.Service{Scanner: noScanner{t}, PCMDecoder: fake.Decode}
	for _, options := range []Options{{Scopes: []string{scope}}, {Scopes: []string{scope}, Network: true}, {Scopes: []string{scope}, Write: true}} {
		s := connect(t, service, options)
		for tool, err := range s.Tools(context.Background(), nil) {
			if err != nil {
				t.Fatal(err)
			}
			if tool.Name == "content_decode_units" || tool.Name == "content_decode_read" {
				t.Fatal("write/network implied decoding permission")
			}
		}
	}
	session := connect(t, service, Options{Scopes: []string{scope}, Decode: true})
	tools := map[string]bool{}
	for tool, err := range session.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		tools[tool.Name] = true
	}
	if !tools["content_decode_read"] || !tools["content_decode_units"] {
		t.Fatal("explicit tools absent", tools)
	}
	args := map[string]any{"catalog": catalog, "root": root, "asset": asset, "representation": "mp4-pcm/1", "pcm_sample": 0, "max_bytes": 1044}
	res, env := call(t, session, "content_decode_read", args)
	if res.IsError || env["error"] != nil {
		t.Fatal(env)
	}
	zero := int64(0)
	direct, err := service.Content(context.Background(), content.Request{SchemaVersion: 1, Catalog: catalog, Root: root, AssetID: asset, Representation: content.PCMRepresentation, Decode: true, PCMSample: &zero, MaxBytes: 1044})
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(direct)
	got, _ := json.Marshal(env["data"])
	var a, b any
	if err := json.Unmarshal(want, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got, &b); err != nil {
		t.Fatal(err)
	}
	if !jsonEqual(a, b) {
		t.Fatal("decoded MCP differs from service", string(want), string(got))
	}
	before := fake.Calls.Load()
	// Operator permission does not make ordinary content tools invoke a decoder.
	for _, tool := range []string{"content_read", "content_units"} {
		res, env := call(t, session, tool, args)
		if !res.IsError || env["error"] == nil || fake.Calls.Load() != before {
			t.Fatal("ordinary tool gained decoder authority", tool, env)
		}
	}
	for _, tc := range []struct {
		name, tool string
		change     func(map[string]any)
	}{
		{"units_zero", "content_decode_units", func(map[string]any) {}},
		{"cursor_zero", "content_decode_read", func(a map[string]any) { a["cursor"] = direct.Continuation }},
		{"outside_root", "content_decode_read", func(a map[string]any) { a["root"] = t.TempDir() }},
		{"outside_catalog", "content_decode_read", func(a map[string]any) { a["catalog"] = filepath.Join(t.TempDir(), "catalog.json") }},
		{"wrong_rep", "content_decode_read", func(a map[string]any) { a["representation"] = "mp4-audio/2" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clone := map[string]any{}
			for k, v := range args {
				clone[k] = v
			}
			tc.change(clone)
			res, env := call(t, session, tc.tool, clone)
			if !res.IsError || env["error"] == nil || fake.Calls.Load() != before {
				t.Fatal("invalid or unscoped request decoded", env)
			}
		})
	}
	res, env = call(t, session, "content_decode_units", map[string]any{"catalog": catalog, "root": root, "asset": asset})
	if res.IsError || env["data"].(map[string]any)["status"] != "units_available" {
		t.Fatal(env)
	}
	res, env = call(t, session, "content_decode_read", map[string]any{"catalog": catalog, "root": root, "asset": asset, "cursor": direct.Continuation, "max_bytes": 1044})
	if res.IsError || env["data"].(map[string]any)["mp4_pcm"].(map[string]any)["range"].(map[string]any)["start_sample"] != "500" {
		t.Fatal(env)
	}
	after, err := os.ReadFile(source)
	if err != nil || !bytes.Equal(data, after) {
		t.Fatal("source changed", err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Fatal("decoder tools changed library state", entries)
	}
}
