package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/config"
	"github.com/blisspixel/nemalo/internal/jobs"
)

// jobs list, get, and cancel read the same journals MCP servers write.
func TestJobsCommandsReadTheStateJournals(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	run := func(args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := Execute(context.Background(), args, &out, &errOut, app.Service{}, func(string) string { return "" }, func() (config.Paths, error) {
			return config.Paths{Config: filepath.Join(dir, "missing.json"), State: state, Cache: filepath.Join(dir, "cache")}, nil
		}, func(context.Context, app.Service) error { return nil })
		return code, out.String(), errOut.String()
	}
	if code, out, _ := run("jobs", "list"); code != 0 || !strings.Contains(out, "Jobs: 0") {
		t.Fatal(code, out)
	}
	store, err := jobs.Open(filepath.Join(state, "jobs"), 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	j, err := store.Start("acquire", map[string]string{"output": "/x"}, func(context.Context, func(jobs.Progress)) (any, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Wait(context.Background())
	store.Close()
	if code, out, _ := run("jobs", "list"); code != 0 || !strings.Contains(out, j.ID) || !strings.Contains(out, "succeeded") {
		t.Fatalf("%d %q", code, out)
	}
	if code, out, _ := run("jobs", "get", j.ID); code != 0 || !strings.Contains(out, "Status: succeeded") || strings.Contains(out, "Run by") {
		t.Fatal(code, out)
	}
	if code, _, errOut := run("jobs", "cancel", j.ID); code != 1 || !strings.Contains(errOut, "already finished") {
		t.Fatal(code, errOut)
	}
	if code, _, _ := run("jobs", "get"); code != 2 {
		t.Fatal("jobs get without an ID", code)
	}
}
