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
	for range 2 {
		raw, merged := t.TempDir(), t.TempDir()
		cmd := exec.CommandContext(t.Context(), exe)
		cmd.Env = append(os.Environ(), "GOCOVERDIR="+raw)
		var exit *exec.ExitError
		if err := cmd.Run(); !errors.As(err, &exit) || exit.ExitCode() != 7 {
			t.Fatal("unconfigured reader invocation", err)
		}
		cmd = exec.CommandContext(t.Context(), "go", "tool", "covdata", "merge", "-i=.", "-o="+merged)
		cmd.Dir = raw
		cmd.Env = append(os.Environ(), "GOTOOLCHAIN="+runtime.Version(), "GOPROXY=off", "GOSUMDB=off")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("merge actual reader counters: %v\n%s", err, out)
		}
		entries, err := os.ReadDir(merged)
		if err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, entry := range entries {
			name := entry.Name()
			if !recoveryMergedCounterName.MatchString(name) {
				continue
			}
			// Force the same merged name for both actual reader invocations,
			// reproducing a coarse Windows clock independently of this OS.
			coarse := name[:strings.LastIndexByte(name, '.')+1] + "1"
			if err := os.Rename(filepath.Join(merged, name), filepath.Join(merged, coarse)); err != nil {
				t.Fatal(err)
			}
			found++
		}
		if found != 1 {
			t.Fatalf("expected one actual merged reader counter, got %d", found)
		}
		if err := nameRecoveryCounters(merged, 1); err != nil {
			t.Fatal(err)
		}
		if err := publishRecoveryCoverage(merged, counters); err != nil {
			t.Fatal(err)
		}
	}
	profile := filepath.Join(t.TempDir(), "reader.out")
	cmd := exec.CommandContext(t.Context(), "go", "tool", "covdata", "textfmt", "-i=.", "-o="+profile)
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
	foundBoth := false
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && strings.Contains(fields[0], "staging/stagingtest/cmd/archiver/main.go:") && fields[2] == "2" {
			foundBoth = true
		}
	}
	if !foundBoth {
		t.Fatal("stock Go report did not retain both actual reader invocations")
	}
	if err := publishRecoveryCoverage(counters, archiveFixtureCounters); err != nil {
		t.Fatal(err)
	}
}
