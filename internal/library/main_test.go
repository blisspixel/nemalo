package library

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
)

func TestMain(m *testing.M) {
	stagingtest.Main()
	os.Exit(runLibraryTests(m))
}

func runLibraryTests(m *testing.M) int {
	// The recovery matrix launches thousands of synthetic reader processes.
	// Build the same handler without the rest of this test executable, retaining
	// real process boundaries and the parent's race instrumentation. Build lazily
	// so lock/FIFO subprocess tests do not compile an unused reader of their own.
	var dir string
	defer func() {
		if dir != "" {
			_ = os.RemoveAll(dir)
		}
	}()
	stagingtest.Executable = sync.OnceValues(func() (string, error) {
		// Keep Go test's own instrumented executable and counter identity in
		// coverage runs. The hosted timeout concerns the separate race suite.
		if testing.CoverMode() != "" {
			return os.Executable()
		}
		var err error
		dir, err = os.MkdirTemp("", "nemalo-test-archiver-")
		if err != nil {
			return "", err
		}
		exe := filepath.Join(dir, "archiver.exe")
		args := []string{"build", "-o", exe}
		if libraryTestRace {
			args = append(args, "-race")
		}
		args = append(args, "../staging/stagingtest/cmd/archiver")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", args...)
		cmd.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off")
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("build synthetic archiver: %w: %s", err, out)
		}
		return exe, nil
	})
	return m.Run()
}
