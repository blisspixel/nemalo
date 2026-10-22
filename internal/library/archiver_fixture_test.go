package library

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
)

func TestArchiveFixtureRetainsToolchainAndInstrumentation(t *testing.T) {
	exe, err := stagingtest.Executable()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := archiveFixtureHash(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkArchiveFixture(exe, digest); err != nil {
		t.Fatal(err)
	}
	if err := checkArchiveFixture(exe, "wrong hash"); err == nil {
		t.Fatal("inherited executable accepted without its parent's byte identity")
	}
	if testing.CoverMode() == "" {
		return
	}
	// Exercise actual counter emission, not just the build flag. No fixture is
	// configured, so the command must refuse the invocation with exit code 7.
	counters := t.TempDir()
	cmd := exec.CommandContext(t.Context(), exe)
	cmd.Env = append(os.Environ(), "GOCOVERDIR="+counters)
	var exit *exec.ExitError
	if err := cmd.Run(); !errors.As(err, &exit) || exit.ExitCode() != 7 {
		t.Fatal("unconfigured reader invocation", err)
	}
	profile := filepath.Join(t.TempDir(), "reader.out")
	cmd = exec.CommandContext(t.Context(), "go", "tool", "covdata", "textfmt", "-i=.", "-o="+profile)
	cmd.Dir = counters
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN="+runtime.Version(), "GOPROXY=off", "GOSUMDB=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("reader counters cannot be converted: %v\n%s", err, out)
	}
	data, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"staging/stagingtest/stagingtest.go:", "staging/stagingtest/cmd/archiver/main.go:"} {
		found := false
		for line := range strings.SplitSeq(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 3 && strings.Contains(fields[0], source) && fields[2] != "0" {
				found = true
			}
		}
		if !found {
			t.Fatalf("reader profile lost executed source %s", source)
		}
	}
	if err := publishRecoveryCoverage(counters, archiveFixtureCounters); err != nil {
		t.Fatal(err)
	}
}
