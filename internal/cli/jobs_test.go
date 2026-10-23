package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/config"
	"github.com/blisspixel/nemalo/internal/jobs"
	"github.com/blisspixel/nemalo/internal/library"
)

func TestJobsSnapshotUsesSharedServiceWithoutOpeningWriterStore(t *testing.T) {
	dir := t.TempDir()
	libraryRoot := filepath.Join(dir, "library")
	if err := os.Mkdir(libraryRoot, 0700); err != nil {
		t.Fatal(err)
	}
	service := app.Service{}
	if _, err := library.Initialize(context.Background(), libraryRoot); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, "state")
	run := func(args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := Execute(context.Background(), args, &out, &errOut, service, func(string) string { return "" }, func() (config.Paths, error) {
			return config.Paths{Config: filepath.Join(dir, "absent.json"), State: state, Cache: filepath.Join(dir, "cache")}, nil
		}, func(context.Context, app.Service) error { return nil })
		return code, out.String(), errOut.String()
	}
	for _, args := range [][]string{{"jobs", "snapshot", libraryRoot, "--json"}, {"jobs", "snapshot", libraryRoot}} {
		code, out, stderr := run(args...)
		if code != 0 || stderr != "" || !strings.Contains(out, "observed") {
			t.Fatal(code, out, stderr)
		}
		if _, err := os.Stat(state); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("snapshot initialized state", err)
		}
	}
	for _, args := range [][]string{{"jobs", "snapshot"}, {"jobs", "snapshot", libraryRoot, "extra"}, {"jobs", "snapshot", libraryRoot, "--limit", "0"}, {"jobs", "snapshot", libraryRoot, "--limit", "101"}, {"jobs", "snapshot", libraryRoot, "--apply"}, {"jobs", "get"}, {"jobs", "list", "extra"}, {"jobs", "nonsense"}} {
		if code, out, stderr := run(args...); code != 2 {
			t.Fatal(args, code, out, stderr)
		}
		if _, err := os.Stat(state); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("invalid invocation opened writer store", args, err)
		}
	}
	// A genuinely running job supplies a durable context. Snapshot never uses
	// Get/List to probe ownership or reconcile it.
	store, err := jobs.Open(filepath.Join(state, "jobs"), 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	started := make(chan struct{})
	t.Cleanup(func() { close(release); store.Close(); _ = store.Wait(context.Background()) })
	binding, err := service.JobContext(context.Background(), libraryRoot)
	if err != nil {
		t.Fatal(err)
	}
	j, err := store.StartWithContext("library_rescan", map[string]string{"library": libraryRoot}, binding, func(context.Context, func(jobs.Progress)) (any, error) { close(started); <-release; return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	// Synchronize through the durable running record before comparing bytes.
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("job callback did not start")
	}
	before, err := os.ReadFile(filepath.Join(state, "jobs", strings.TrimPrefix(j.ID, "job:")+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	code, out, stderr := run("jobs", "snapshot", libraryRoot, "--limit", "1", "--json")
	if code != 0 || stderr != "" {
		t.Fatal(code, out, stderr)
	}
	var envelope struct {
		Data app.JobSnapshotResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	direct, err := service.JobSnapshot(context.Background(), app.JobSnapshotRequest{Library: libraryRoot, JobsDirectory: filepath.Join(state, "jobs"), Limit: 1})
	if err != nil || !reflect.DeepEqual(envelope.Data.Jobs, direct.Jobs) || len(direct.Jobs) != 1 || direct.Jobs[0].ID != j.ID || direct.Jobs[0].Owner != "not_observed" {
		t.Fatal(envelope, direct, err)
	}
	after, err := os.ReadFile(filepath.Join(state, "jobs", strings.TrimPrefix(j.ID, "job:")+".jsonl"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("passive CLI changed journal", err)
	}
	if err := os.WriteFile(filepath.Join(state, "jobs", strings.Repeat("f", 32)+".jsonl"), []byte("{bad}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if code, out, stderr := run("jobs", "snapshot", libraryRoot, "--json"); code != 1 || !strings.Contains(out, `"status":"incomplete"`) || stderr != "" {
		t.Fatal(code, out, stderr)
	}
}

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

func TestTerminalReceivesTheConfiguredPassiveJobsDirectoryWithoutCreatingIt(t *testing.T) {
	saved := stdinTerminal
	stdinTerminal = func() bool { return true }
	t.Cleanup(func() { stdinTerminal = saved })
	root := t.TempDir()
	state := filepath.Join(root, "state")
	for _, override := range []string{"", filepath.Join(root, "alternate-jobs")} {
		called := false
		code := Execute(context.Background(), []string{"tui"}, &bytes.Buffer{}, &bytes.Buffer{}, app.Service{JobsDirectory: override}, func(string) string { return "" }, func() (config.Paths, error) {
			return config.Paths{Config: filepath.Join(root, "missing.json"), State: state}, nil
		}, func(_ context.Context, s app.Service) error {
			called = true
			want := override
			if want == "" {
				want = filepath.Join(state, "jobs")
			}
			if s.JobsDirectory != want {
				t.Fatalf("terminal jobs path %q, want %q", s.JobsDirectory, want)
			}
			return nil
		})
		if code != 0 || !called {
			t.Fatal(code, called)
		}
		if _, err := os.Stat(state); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("terminal setup initialized state", err)
		}
		if override != "" {
			if _, err := os.Stat(override); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("terminal setup created overridden store", err)
			}
		}
	}
}
