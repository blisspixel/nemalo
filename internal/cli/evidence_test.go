package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/audio/audiotest"
	"github.com/blisspixel/nemalo/internal/config"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/decode/decodetest"
	"github.com/blisspixel/nemalo/internal/library"
)

func TestEvidenceCLISharedReadReplayAndIndependentDecodePermission(t *testing.T) {
	for _, decoded := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "decoded"}[decoded], func(t *testing.T) {
			ctx := context.Background()
			root, state := t.TempDir(), t.TempDir()
			if _, err := library.Initialize(ctx, root); err != nil {
				t.Fatal(err)
			}
			data := audiotest.MP4(4)
			fake := decodetest.NewPCM(data)
			source := writeCLIEPUB(t, t.TempDir())
			if decoded {
				source = filepath.Join(t.TempDir(), "track.m4b")
				if err := os.WriteFile(source, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			service := app.Service{Scanner: citationScanner{t}, PCMDecoder: fake.Decode}
			held, err := service.Import(ctx, library.ImportRequest{Library: root, Source: source, Apply: true})
			if err != nil {
				t.Fatal(err)
			}
			before, stateBefore := citationFiles(t, root), citationFiles(t, state)
			run := func(args ...string) (int, content.Result, string) {
				var out, stderr bytes.Buffer
				code := Execute(ctx, args, &out, &stderr, service, func(string) string { return "" }, func() (config.Paths, error) {
					return config.Paths{Config: filepath.Join(state, "none"), State: state}, nil
				}, nil)
				var env struct {
					Data  content.Result `json:"data"`
					Error string         `json:"error"`
				}
				if err := json.Unmarshal(out.Bytes(), &env); err != nil {
					t.Fatal(code, out.String(), stderr.String(), err)
				}
				return code, env.Data, env.Error
			}
			base := []string{"content", "read", root, "--root", root, "--asset", held.AssetID, "--max-bytes", "1000", "--json"}
			req := content.Request{SchemaVersion: 1, Catalog: root, Root: root, AssetID: held.AssetID, MaxBytes: 1000, Evidence: true}
			if decoded {
				base = append(base, "--representation", content.PCMRepresentation, "--decode")
				req.Representation, req.Decode = content.PCMRepresentation, true
			}
			code, first, message := run(append(append([]string{}, base...), "--evidence")...)
			if code != 0 || message != "" || first.Evidence == nil {
				t.Fatal(code, first, message)
			}
			direct, err := service.Content(ctx, req)
			wantJSON, wantErr := json.Marshal(direct)
			gotJSON, gotErr := json.Marshal(first)
			if err != nil || wantErr != nil || gotErr != nil || !bytes.Equal(wantJSON, gotJSON) {
				t.Fatal("CLI/service mismatch", first, direct, err)
			}
			code, replayed, message := run(append(append([]string{}, base...), "--evidence-reference", first.Evidence.Reference)...)
			if code != 0 || message != "" || !reflect.DeepEqual(replayed, first) {
				t.Fatal("CLI replay mismatch", code, replayed, message)
			}
			for _, action := range []string{"units", "source", "resource", "locate", "navigation", "pages"} {
				bad := append([]string{}, base...)
				bad[1] = action
				bad = append(bad, "--evidence")
				calls := fake.Calls.Load()
				code, _, _ := run(bad...)
				if code != 2 || fake.Calls.Load() != calls {
					t.Fatal("evidence accepted outside read", action, code)
				}
			}
			if decoded {
				bad := []string{}
				for _, arg := range base {
					if arg != "--decode" {
						bad = append(bad, arg)
					}
				}
				calls := fake.Calls.Load()
				code, result, _ := run(append(bad, "--evidence-reference", first.Evidence.Reference)...)
				if code != 2 || result.Evidence != nil || result.Resource != nil || fake.Calls.Load() != calls {
					t.Fatal("replay granted decode permission", code, result)
				}
			}
			code, failed, _ := run(append(append([]string{}, base...), "--max-bytes", "1001", "--evidence-reference", first.Evidence.Reference)...)
			if code != 1 || failed.Status != "stale_reference" || failed.Evidence != nil || failed.Resource != nil || failed.Text != "" || failed.Continuation != "" {
				t.Fatal("different bound leaked evidence", code, failed)
			}
			if !decoded {
				var out, stderr bytes.Buffer
				args := []string{"content", "read", root, "--root", root, "--asset", held.AssetID, "--evidence"}
				code := Execute(ctx, args, &out, &stderr, service, func(string) string { return "" }, func() (config.Paths, error) {
					return config.Paths{Config: filepath.Join(state, "none"), State: state}, nil
				}, nil)
				if code != 0 || !strings.Contains(out.String(), "Evidence reference: nemaloevidence1:") {
					t.Fatal(code, out.String(), stderr.String())
				}
			}
			if !reflect.DeepEqual(before, citationFiles(t, root)) || !reflect.DeepEqual(stateBefore, citationFiles(t, state)) {
				t.Fatal("evidence CLI changed library/user state")
			}
		})
	}
}
