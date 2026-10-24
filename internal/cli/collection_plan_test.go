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
	"github.com/blisspixel/nemalo/internal/config"
	"github.com/blisspixel/nemalo/internal/library"
)

func TestCollectionAcquireCLIExposesAndRevalidatesPreviewPlan(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "library")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Initialize(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(dir, "collection.json")
	if err := os.WriteFile(manifest, []byte(`{"schema_version":1,"name":"Reviewed","items":[{"source":"archive:one","file":"one.pdf"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	intake := filepath.Join(dir, "intake")
	packet := filepath.Join(intake, library.CollectionItem{Source: "archive:one", File: "one.pdf"}.Packet())
	if err := os.MkdirAll(packet, 0700); err != nil {
		t.Fatal(err)
	}
	service := app.Service{Providers: map[string]app.Searcher{}, Scanner: fakeScanner("unavailable")}
	run := func(flags ...string) (int, string, string) {
		t.Helper()
		var out, stderr bytes.Buffer
		args := append([]string{"collection", "acquire", manifest, root, "--output", intake}, flags...)
		code := Execute(context.Background(), args, &out, &stderr, service, func(string) string { return "" }, func() (config.Paths, error) {
			return config.Paths{Config: filepath.Join(dir, "missing-config"), State: filepath.Join(dir, "missing-state")}, nil
		}, nil)
		return code, out.String(), stderr.String()
	}
	code, out, stderr := run("--json")
	var env struct {
		Data  app.CollectionAcquireResult `json:"data"`
		Error string                      `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil || code != 0 || stderr != "" || len(env.Data.PlanDigest) != 64 || env.Data.Applied {
		t.Fatal(code, out, stderr, err)
	}
	plan := env.Data.PlanDigest
	if code, out, stderr = run(); code != 0 || stderr != "" || !strings.Contains(out, plan) {
		t.Fatal("plain preview omitted binding", code, out, stderr)
	}
	if code, _, _ = run("--plan", plan); code != 2 {
		t.Fatal("plan alone became authority", code)
	}
	for _, flags := range [][]string{{"--plan", ""}, {"--apply", "--plan", ""}} {
		if code, _, _ = run(flags...); code != 2 {
			t.Fatal("empty explicit guard became unguarded apply", flags, code)
		}
	}
	if code, out, stderr = run("--apply", "--plan", "wrong", "--json"); code != 1 || stderr != "" || !strings.Contains(out, "preview changed") {
		t.Fatal("bad plan accepted", code, out, stderr)
	}
	if code, out, stderr = run("--apply", "--plan", plan, "--max-total-bytes", "1234", "--json"); code != 1 || stderr != "" || !strings.Contains(out, "preview changed") {
		t.Fatal("budget change accepted", code, out, stderr)
	}
	code, out, stderr = run("--apply", "--plan", plan, "--json")
	if err := json.Unmarshal([]byte(out), &env); err != nil || code != 1 || stderr != "" || env.Error != "" || !env.Data.Applied || env.Data.Complete || len(env.Data.Steps) != 1 || env.Data.Steps[0].Action != app.StepRetained || env.Data.PlanDigest != plan {
		t.Fatal("matching guarded result misreported", code, out, stderr, err)
	}
	entries, err := os.ReadDir(packet)
	if err != nil || len(entries) != 0 {
		t.Fatal("retained packet changed", entries, err)
	}
	for _, missing := range []string{"missing-config", "missing-state"} {
		if _, err := os.Stat(filepath.Join(dir, missing)); !os.IsNotExist(err) {
			t.Fatal("CLI initialized unrelated state", missing, err)
		}
	}
}
