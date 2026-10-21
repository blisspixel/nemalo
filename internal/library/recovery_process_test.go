package library

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const rarRecoveryCaseEnv = "NEMALO_TEST_RAR_RECOVERY_CASE"

var recoveryProcessSlots = make(chan struct{}, 3)
var recoveryCoveragePublish sync.Mutex

// The same compiled test executable retains race/coverage instrumentation.
// Each covered child writes privately: concurrent Go metadata renames into a
// shared directory can fail on Windows. Merge counters only after it exits.
func recoveryProcess(ctx context.Context, root, selected string) ([]byte, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	args := []string{"-test.run=^" + regexp.QuoteMeta(root) + "$", "-test.count=1", "-test.v", "-test.timeout=4m"}
	env := append(os.Environ(), rarRecoveryCaseEnv+"="+selected)
	var private, parent string
	if testing.CoverMode() != "" {
		parent = os.Getenv("GOCOVERDIR")
		if parent == "" {
			return nil, errors.New("covered recovery subprocess requires Go test's counter directory")
		}
		private, err = os.MkdirTemp("", "nemalo-recovery-counters-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(private)
		for _, name := range []string{"counters", "merged"} {
			if err := os.Mkdir(filepath.Join(private, name), 0700); err != nil {
				return nil, err
			}
		}
		counters := filepath.Join(private, "counters")
		args = append(args, "-test.gocoverdir="+counters)
		env = append(env, "GOCOVERDIR="+counters)
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Env = env
	cmd.WaitDelay = time.Second
	out := &recoveryProcessOutput{}
	cmd.Stdout, cmd.Stderr = out, out
	err = cmd.Run()
	if err == nil && private != "" {
		err = mergeRecoveryCoverage(ctx, private, parent, out)
	}
	return out.bytes(), err
}

func mergeRecoveryCoverage(ctx context.Context, private, parent string, out *recoveryProcessOutput) error {
	cmd := exec.CommandContext(ctx, "go", "tool", "covdata", "merge", "-i=counters", "-o=merged")
	// Relative input names avoid covdata's comma-separated path ambiguity.
	cmd.Dir = private
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN="+runtime.Version(), "GOPROXY=off", "GOSUMDB=off")
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("merge recovery counters: %w", err)
	}
	return publishRecoveryCoverage(filepath.Join(private, "merged"), parent)
}

func publishRecoveryCoverage(merged, parent string) error {
	recoveryCoveragePublish.Lock()
	defer recoveryCoveragePublish.Unlock()
	entries, err := os.ReadDir(merged)
	if err != nil {
		return err
	}
	metadata, counters := false, false
	for _, entry := range entries {
		name := entry.Name()
		meta := strings.HasPrefix(name, "covmeta.")
		counter := strings.HasPrefix(name, "covcounters.")
		if !entry.Type().IsRegular() || (!meta && !counter) {
			return fmt.Errorf("unexpected recovery counter entry %q", name)
		}
		metadata = metadata || meta
		counters = counters || counter
	}
	if !metadata || !counters {
		return errors.New("recovery coverage requires metadata and counters")
	}
	for _, entry := range entries {
		name := entry.Name()
		data, err := os.ReadFile(filepath.Join(merged, name))
		if err != nil {
			return err
		}
		path := filepath.Join(parent, name)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if errors.Is(err, os.ErrExist) && strings.HasPrefix(name, "covmeta.") {
			existing, readErr := os.ReadFile(path)
			if readErr == nil && bytes.Equal(existing, data) {
				continue
			}
			return fmt.Errorf("recovery coverage metadata conflict %q: %v", name, readErr)
		}
		if err != nil {
			return fmt.Errorf("publish recovery coverage %q: %w", name, err)
		}
		_, writeErr := f.Write(data)
		closeErr := f.Close()
		if err := errors.Join(writeErr, closeErr); err != nil {
			return err
		}
	}
	return nil
}

type recoveryProcessOutput struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (o *recoveryProcessOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(p) > (1<<20)-o.buffer.Len() {
		return 0, errors.New("recovery subprocess output exceeded 1 MiB")
	}
	return o.buffer.Write(p)
}

func (o *recoveryProcessOutput) bytes() []byte {
	o.mu.Lock()
	defer o.mu.Unlock()
	return bytes.Clone(o.buffer.Bytes())
}

func runRARRecoveryProcesses(t *testing.T, versions []int, layouts []string) {
	t.Helper()
	var selections []string
	for _, version := range versions {
		for _, layout := range layouts {
			selections = append(selections, fmt.Sprintf("%d/%s", version, layout))
		}
	}
	runRecoveryProcesses(t, selections)
}

func runRecoveryProcesses(t *testing.T, selections []string) {
	t.Helper()
	root := t.Name()
	for _, selected := range selections {
		t.Run(selected, func(t *testing.T) {
			t.Parallel()
			recoveryProcessSlots <- struct{}{}
			defer func() { <-recoveryProcessSlots }()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
			defer cancel()
			out, err := recoveryProcess(ctx, root, selected)
			if err != nil {
				t.Fatalf("isolated recovery case %s: %v\n%s", selected, err, out)
			}
			if !bytes.Contains(out, []byte("=== RUN   "+root+"/"+selected+"\n")) || !bytes.Contains(out, []byte("--- PASS: "+root+"/"+selected+" (")) {
				t.Fatalf("recovery subprocess did not execute its selected group %s\n%s", selected, out)
			}
			t.Logf("isolated recovery result:\n%s", out)
		})
	}
}

func TestRecoverySubprocessRejectsUnknownCase(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	out, err := recoveryProcess(ctx, "TestRARTreeRecoveryAcrossEveryOuterSetLayout", "unknown/case")
	if err == nil || !bytes.Contains(out, []byte("unknown RAR recovery case")) {
		t.Fatalf("unexecuted child case reported success: %v\n%s", err, out)
	}
}

func TestRecoveryCoveragePublication(t *testing.T) {
	write := func(dir, name, data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("concurrent groups preserve every counter", func(t *testing.T) {
		parent := t.TempDir()
		var wg sync.WaitGroup
		for i := range 8 {
			merged := t.TempDir()
			write(merged, "covmeta.same", "same metadata")
			write(merged, fmt.Sprintf("covcounters.same.%d.1", i), "group counters")
			wg.Go(func() {
				if err := publishRecoveryCoverage(merged, parent); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		entries, err := os.ReadDir(parent)
		if err != nil || len(entries) != 9 {
			t.Fatalf("lost counter files: %v %v", entries, err)
		}
		for _, entry := range entries {
			got, err := os.ReadFile(filepath.Join(parent, entry.Name()))
			want := "group counters"
			if entry.Name() == "covmeta.same" {
				want = "same metadata"
			}
			if err != nil || string(got) != want {
				t.Fatalf("corrupt published coverage %q: %q %v", entry.Name(), got, err)
			}
		}
	})
	for _, kind := range []string{"missing metadata", "missing counters", "metadata conflict", "counter collision", "unexpected entry"} {
		t.Run(kind, func(t *testing.T) {
			merged, parent := t.TempDir(), t.TempDir()
			if kind != "missing metadata" {
				write(merged, "covmeta.same", "new metadata")
			}
			if kind != "missing counters" {
				write(merged, "covcounters.same.1.1", "new counters")
			}
			existing := ""
			switch kind {
			case "metadata conflict":
				existing = "covmeta.same"
			case "counter collision":
				existing = "covcounters.same.1.1"
			case "unexpected entry":
				write(merged, "unexpected", "not counters")
			}
			if existing != "" {
				write(parent, existing, "retained original")
			}
			if err := publishRecoveryCoverage(merged, parent); err == nil {
				t.Fatal("invalid coverage publication succeeded")
			}
			if existing != "" {
				got, err := os.ReadFile(filepath.Join(parent, existing))
				if err != nil || string(got) != "retained original" {
					t.Fatalf("existing coverage replaced: %q %v", got, err)
				}
			}
		})
	}
}

func TestRecoveryCoverageMergeRejectsMissingCounters(t *testing.T) {
	private, parent := t.TempDir(), t.TempDir()
	for _, name := range []string{"counters", "merged"} {
		if err := os.Mkdir(filepath.Join(private, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if err := mergeRecoveryCoverage(ctx, private, parent, &recoveryProcessOutput{}); err == nil {
		t.Fatal("missing child counters reported success")
	}
}
