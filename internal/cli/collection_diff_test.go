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
	"github.com/blisspixel/nemalo/internal/config"
	"github.com/blisspixel/nemalo/internal/library"
)

func TestCollectionDiffCommandSharesTheServiceAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	before, after := filepath.Join(dir, "before.json"), filepath.Join(dir, "after.json")
	inputs := map[string]string{
		before: `{"schema_version":1,"name":"Before\u001b[31m","items":[{"source":"archive:book","file":"book.epub","note":"Old"},{"source":"arxiv:1706.03762v1","file":"1706.03762v1.pdf"}]}`,
		after:  `{"schema_version":1,"name":"After","items":[{"source":"archive:book","file":"book.epub","note":"New"},{"source":"arxiv:1706.03762v2","file":"1706.03762v2.pdf"}]}`,
	}
	for path, body := range inputs {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	service := app.Service{Providers: map[string]app.Searcher{}, Scanner: fakeScanner("unavailable")}
	run := func(args ...string) (int, string, string) {
		t.Helper()
		var out, stderr bytes.Buffer
		code := Execute(context.Background(), args, &out, &stderr, service, func(string) string { return "" }, func() (config.Paths, error) {
			return config.Paths{Config: filepath.Join(dir, "missing-config"), State: filepath.Join(dir, "absent-state")}, nil
		}, nil)
		return code, out.String(), stderr.String()
	}
	want, err := service.CollectionDiff(context.Background(), before, after)
	if err != nil {
		t.Fatal(err)
	}
	code, out, stderr := run("collection", "diff", before, after, "--json")
	var env struct {
		SchemaVersion int                          `json:"schema_version"`
		Command       string                       `json:"command"`
		Data          library.CollectionDiffResult `json:"data"`
		Error         string                       `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil || code != 0 || stderr != "" || env.SchemaVersion != 1 || env.Command != "collection" || env.Error != "" || !reflect.DeepEqual(env.Data, want) {
		t.Fatal(code, out, stderr, err)
	}
	code, out, stderr = run("collection", "diff", before, after)
	if code != 0 || stderr != "" || strings.ContainsRune(out, '\x1b') || !strings.Contains(out, "Removed (before 2): arxiv:1706.03762v1") || !strings.Contains(out, "Added (after 2): arxiv:1706.03762v2") || !strings.Contains(out, "Note changed: archive:book book.epub") || !strings.Contains(out, want.Before.SHA256) || !strings.Contains(out, want.After.SHA256) {
		t.Fatal(code, out, stderr)
	}
	for _, args := range [][]string{
		{"collection", "diff"}, {"collection", "diff", before}, {"collection", "diff", before, after, "extra"},
		{"collection", "diff", before, after, "--apply"}, {"collection", "diff", before, after, "--output", "ignored"},
		{"collection", "diff", before, after, "--no-scan"}, {"collection", "diff", before, after, "--limit", "1"},
	} {
		if code, _, _ := run(args...); code != 2 {
			t.Fatal("invalid diff invocation accepted", args, code)
		}
	}
	code, out, stderr = run("collection", "diff", before, filepath.Join(dir, "missing"))
	if code != 1 || out != "" || !strings.Contains(stderr, "after collection") {
		t.Fatal("failed diff printed partial evidence", code, out, stderr)
	}
	code, out, stderr = run("collection", "diff", before, filepath.Join(dir, "missing"), "--json")
	if err := json.Unmarshal([]byte(out), &env); err != nil || code != 1 || stderr != "" || env.Error == "" || env.Data.Complete || env.Data.Before.SHA256 != "" || env.Data.After.SHA256 != "" {
		t.Fatal("JSON failure contract", code, out, stderr, err)
	}
	for path, body := range inputs {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != body {
			t.Fatal("manifest changed", path, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatal("diff wrote state", entries, err)
	}
}
