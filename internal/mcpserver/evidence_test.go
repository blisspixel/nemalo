package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/audio/audiotest"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/decode/decodetest"
	"github.com/blisspixel/nemalo/internal/library"
)

func TestEvidenceMCPSharedSchemaReplayScopeAndDecoderBoundaries(t *testing.T) {
	for _, decoded := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "decoded"}[decoded], func(t *testing.T) {
			ctx := context.Background()
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := library.Initialize(ctx, root); err != nil {
				t.Fatal(err)
			}
			data := audiotest.MP4(4)
			fake := decodetest.NewPCM(data)
			source := epub(t, t.TempDir(), "<p>Café. Source evidence with a bounded continuation.</p>")
			if decoded {
				source = filepath.Join(t.TempDir(), "track.m4b")
				if err := os.WriteFile(source, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			service := app.Service{Scanner: noScanner{t}, PCMDecoder: fake.Decode}
			held, err := service.Import(ctx, library.ImportRequest{Library: root, Source: source, Apply: true})
			if err != nil {
				t.Fatal(err)
			}
			session := connect(t, service, Options{Scopes: []string{root}, Decode: decoded})
			before := citationInventory(t, root)
			args := map[string]any{"catalog": root, "root": root, "asset": held.AssetID, "max_bytes": 1000, "evidence": true}
			req := content.Request{SchemaVersion: 1, Catalog: root, Root: root, AssetID: held.AssetID, MaxBytes: 1000, Evidence: true}
			tool := "content_read"
			if decoded {
				tool = "content_decode_read"
				args["representation"] = content.PCMRepresentation
				req.Representation, req.Decode = content.PCMRepresentation, true
			}
			res, env := call(t, session, tool, args)
			if res.IsError || env["error"] != nil {
				t.Fatal(env)
			}
			direct, err := service.Content(ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(direct)
			if err != nil {
				t.Fatal(err)
			}
			var want any
			if err := json.Unmarshal(encoded, &want); err != nil {
				t.Fatal(err)
			}
			if !jsonEqual(want, env["data"]) {
				t.Fatal("MCP/service mismatch", env)
			}
			delete(args, "evidence")
			args["evidence_reference"] = direct.Evidence.Reference
			res, env = call(t, session, tool, args)
			if res.IsError || !jsonEqual(want, env["data"]) {
				t.Fatal("MCP replay mismatch", env)
			}
			for _, operation := range []string{"content_units", "content_navigation", "content_pages", "content_source", "content_resource"} {
				calls := fake.Calls.Load()
				res, env := call(t, session, operation, args)
				if !res.IsError || env["error"] == nil || fake.Calls.Load() != calls {
					t.Fatal("evidence accepted outside read", operation, env)
				}
			}
			if decoded {
				calls := fake.Calls.Load()
				res, env := call(t, session, "content_read", args)
				if !res.IsError || fake.Calls.Load() != calls {
					t.Fatal("ordinary tool gained decoder authority", env)
				}
				res, env = call(t, session, "content_decode_units", args)
				if !res.IsError || fake.Calls.Load() != calls {
					t.Fatal("units accepted evidence", env)
				}
			}
			args["root"] = t.TempDir()
			calls := fake.Calls.Load()
			res, env = call(t, session, tool, args)
			if !res.IsError || env["error"] == nil || fake.Calls.Load() != calls {
				t.Fatal("reference bypassed scope", env)
			}
			args["root"], args["max_bytes"] = root, 1001
			res, env = call(t, session, tool, args)
			if !res.IsError {
				t.Fatal("different bounds accepted", env)
			}
			body, err := json.Marshal(env["data"])
			if err != nil {
				t.Fatal(err)
			}
			var failed content.Result
			if err := json.Unmarshal(body, &failed); err != nil {
				t.Fatal(err)
			}
			if failed.Status != "stale_reference" || failed.Evidence != nil || failed.Text != "" || failed.Resource != nil || failed.Continuation != "" {
				t.Fatal("failure leaked evidence", env)
			}
			if !reflect.DeepEqual(before, citationInventory(t, root)) {
				t.Fatal("MCP evidence wrote state")
			}
		})
	}
}
