package library

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sync"
	"testing"
	"time"
)

const rarRecoveryCaseEnv = "NEMALO_TEST_RAR_RECOVERY_CASE"

var recoveryProcessSlots = make(chan struct{}, 3)

// The same compiled test executable retains race/coverage instrumentation.
// Go test supplies one counter directory; covered children join its counters
// without competing to write the parent's final text coverage profile.
func recoveryProcess(ctx context.Context, root, selected string) ([]byte, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	args := []string{"-test.run=^" + regexp.QuoteMeta(root) + "$", "-test.count=1", "-test.v", "-test.timeout=4m"}
	if testing.CoverMode() != "" {
		dir := os.Getenv("GOCOVERDIR")
		if dir == "" {
			return nil, errors.New("covered recovery subprocess requires Go test's counter directory")
		}
		args = append(args, "-test.gocoverdir="+dir)
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Env = append(os.Environ(), rarRecoveryCaseEnv+"="+selected)
	cmd.WaitDelay = time.Second
	out := &recoveryProcessOutput{}
	cmd.Stdout, cmd.Stderr = out, out
	err = cmd.Run()
	return out.bytes(), err
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
