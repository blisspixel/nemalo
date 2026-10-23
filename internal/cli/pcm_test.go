package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/audio/audiotest"
	"github.com/blisspixel/nemalo/internal/config"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/decode/decodetest"
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/library"
)

func TestPCMCLIExplicitPermissionAndSharedDecodedContract(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	data := audiotest.MP4(4)
	path := filepath.Join(root, "track.m4b")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := library.Build(context.Background(), root, inventory.Defaults(), false)
	if err != nil {
		t.Fatal(err)
	}
	catalog := filepath.Join(t.TempDir(), "catalog.json")
	if err := library.Save(context.Background(), catalog, &snapshot); err != nil {
		t.Fatal(err)
	}
	asset := snapshot.Catalog.Assets[0].ID
	fake := decodetest.NewPCM(data)
	service := app.Service{PCMDecoder: fake.Decode}
	run := func(args []string) (int, string, string) {
		var out, stderr bytes.Buffer
		code := Execute(context.Background(), args, &out, &stderr, service, func(string) string { return "" }, func() (config.Paths, error) {
			return config.Paths{Config: filepath.Join(state, "none"), State: state}, nil
		}, nil)
		return code, out.String(), stderr.String()
	}
	common := []string{catalog, "--root", root, "--asset", asset, "--representation", "mp4-pcm/1", "--json"}
	args := append([]string{"content", "read"}, common...)
	args = append(args, "--decode", "--pcm-sample", "0", "--max-bytes", "1044")
	code, out, stderr := run(args)
	var envelope struct {
		Data  content.Result `json:"data"`
		Error string         `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || code != 0 || stderr != "" || envelope.Data.MP4PCM == nil {
		t.Fatal(code, out, stderr, err)
	}
	first := envelope.Data
	zero := int64(0)
	direct, err := service.Content(context.Background(), content.Request{SchemaVersion: 1, Catalog: catalog, Root: root, AssetID: asset, Representation: content.PCMRepresentation, Decode: true, PCMSample: &zero, MaxBytes: 1044})
	want, _ := json.Marshal(direct)
	got, _ := json.Marshal(first)
	if err != nil || !bytes.Equal(want, got) {
		t.Fatal("CLI diverged from service", err, string(want), string(got))
	}
	for _, bad := range [][]string{
		append(append([]string{"content", "read"}, common...), "--pcm-sample", "0"),
		append(append([]string{"content", "units"}, common...), "--decode", "--pcm-sample", "0"),
		append(append([]string{"content", "read"}, common...), "--decode", "--cursor", first.Continuation, "--pcm-sample", "0"),
		append(append([]string{"content", "read"}, common...), "--decode", "--representation", "mp4-audio/2"),
		{"content", "read", catalog, "--root", root, "--asset", asset, "--cursor", first.Continuation, "--json"},
	} {
		before := fake.Calls.Load()
		if code, out, _ := run(bad); code != 2 || fake.Calls.Load() != before {
			t.Fatal("invalid decoder request executed", bad, code, out)
		}
	}
	continuation := []string{"content", "read", catalog, "--root", root, "--asset", asset, "--decode", "--cursor", first.Continuation, "--max-bytes", "1044", "--json"}
	code, out, _ = run(continuation)
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || code != 0 || envelope.Data.MP4PCM.Range.StartSample != "500" {
		t.Fatal("explicit continuation", code, out, err)
	}
	code, out, _ = run(append(append([]string{"content", "units"}, common...), "--decode"))
	if code != 0 || !strings.Contains(out, `"total_samples":"1500"`) || strings.Contains(out, `"resource":`) {
		t.Fatal("units", code, out)
	}
	human := []string{"content", "read", catalog, "--root", root, "--asset", asset, "--decode", "--representation", "mp4-pcm/1"}
	if code, out, _ := run(human); code != 0 || !strings.Contains(out, "Decoded output: mono s16le") || strings.Contains(out, "Reading-order units") {
		t.Fatal(code, out)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, after) {
		t.Fatal("source changed", err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Fatal("content initialized library state", entries)
	}
}
