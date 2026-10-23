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
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/library"
)

func TestMP4PacketMCPMatchesServiceWithoutWriteOrNetworkPermission(t *testing.T) {
	scope := t.TempDir()
	root := filepath.Join(scope, "books")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	data := audiotest.MP4(4)
	path := filepath.Join(root, "track.m4b")
	if err := os.WriteFile(path, data, 0600); err != nil {
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
	service := app.Service{Scanner: noScanner{t}}
	session := connect(t, service, Options{Scopes: []string{scope}, Version: "test"})
	args := map[string]any{"catalog": catalog, "root": root, "asset": asset, "representation": "mp4-audio/2", "packet": 0, "max_bytes": 200}
	res, env := call(t, session, "content_read", args)
	if res.IsError || env["error"] != nil {
		t.Fatal(env)
	}
	zero := 0
	direct, err := service.Content(context.Background(), content.Request{SchemaVersion: 1, Catalog: catalog, Root: root, AssetID: asset, MaxBytes: 200, Representation: "mp4-audio/2", Packet: &zero})
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
		t.Fatal("MCP differs from shared service", string(want), string(got))
	}
	for _, tc := range []struct {
		name, tool string
		change     func(map[string]any)
	}{
		{"units", "content_units", func(map[string]any) {}},
		{"wrong_rep_zero", "content_read", func(a map[string]any) { a["representation"] = "mp4-audio/1" }},
		{"cursor_zero", "content_read", func(a map[string]any) { a["cursor"] = direct.Continuation }},
		{"outside_root", "content_read", func(a map[string]any) { a["root"] = t.TempDir() }},
		{"outside_catalog", "content_read", func(a map[string]any) { a["catalog"] = filepath.Join(t.TempDir(), "catalog.json") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copyArgs := make(map[string]any)
			for k, v := range args {
				copyArgs[k] = v
			}
			tc.change(copyArgs)
			res, env := call(t, session, tc.tool, copyArgs)
			if !res.IsError || env["error"] == nil {
				t.Fatal("invalid or unscoped request accepted", env)
			}
			if d, ok := env["data"].(map[string]any); ok && (d["mp4_packets"] != nil || d["resource"] != nil) {
				t.Fatal("failure exposed payload", env)
			}
		})
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, data) {
		t.Fatal("source changed", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatal("content changed source tree", entries, err)
	}
}
