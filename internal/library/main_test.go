package library

import (
	"bufio"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
)

const archiveFixtureEnv = "NEMALO_TEST_ARCHIVER"
const archiveFixtureHashEnv = "NEMALO_TEST_ARCHIVER_SHA256"
const archiveFixtureCountersEnv = "NEMALO_TEST_ARCHIVER_COUNTERS"

var archiveFixtureCounters string
var libraryTestCounters string

func TestMain(m *testing.M) {
	stagingtest.Main()
	os.Exit(runLibraryTests(m))
}

func runLibraryTests(m *testing.M) int {
	flag.Parse()
	// The recovery matrix launches thousands of synthetic reader processes.
	// Build the same handler without the rest of this test executable, retaining
	// real process boundaries and the parent's race/coverage instrumentation.
	// Recovery children reuse this parent-owned executable, never rebuild it.
	var dir string
	defer func() {
		if dir != "" {
			_ = os.RemoveAll(dir)
		}
	}()
	stagingtest.Executable = sync.OnceValues(func() (string, error) {
		if inherited := os.Getenv(archiveFixtureEnv); inherited != "" {
			if os.Getenv(rarRecoveryCaseEnv) == "" {
				return "", errors.New("inherited archiver outside a recovery subprocess")
			}
			if err := checkArchiveFixture(inherited, os.Getenv(archiveFixtureHashEnv)); err != nil {
				return "", err
			}
			return inherited, nil
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
		if mode := testing.CoverMode(); mode != "" {
			args = append(args, "-cover", "-covermode="+mode, "-coverpkg=github.com/blisspixel/nemalo/...")
		}
		args = append(args, "../staging/stagingtest/cmd/archiver")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", args...)
		cmd.Env = append(os.Environ(), "GOTOOLCHAIN="+runtime.Version(), "GOPROXY=off", "GOSUMDB=off")
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("build synthetic archiver: %w: %s", err, out)
		}
		return exe, nil
	})
	if testing.CoverMode() != "" {
		// Go test passes its own counter directory through -test.gocoverdir.
		// Descendant commands use GOCOVERDIR; retain those counters separately.
		libraryTestCounters = os.Getenv("GOCOVERDIR")
		archiveFixtureCounters = os.Getenv(archiveFixtureCountersEnv)
		if archiveFixtureCounters == "" {
			var err error
			archiveFixtureCounters, err = os.MkdirTemp("", "nemalo-reader-counters-")
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			defer os.RemoveAll(archiveFixtureCounters)
		}
		if err := os.Setenv("GOCOVERDIR", archiveFixtureCounters); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	if defaultRecoverySchedule() {
		var err error
		libraryRecoverySchedule, err = startRecoverySchedule()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer libraryRecoverySchedule.close()
	}
	code := m.Run()
	if code == 0 && libraryRecoverySchedule != nil {
		if err := libraryRecoverySchedule.complete(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	if code == 0 && testing.CoverMode() != "" && os.Getenv(rarRecoveryCaseEnv) == "" {
		if err := finishArchiveFixtureCoverage(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	return code
}

func archiveFixtureHash(exe string) (string, error) {
	f, err := os.Open(exe)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 32<<20 {
		return "", fmt.Errorf("invalid synthetic archiver file: %v", err)
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func checkArchiveFixture(exe, digest string) error {
	actual, err := archiveFixtureHash(exe)
	if err != nil || digest == "" || actual != digest {
		return fmt.Errorf("synthetic archiver identity mismatch: %v", err)
	}
	info, err := buildinfo.ReadFile(exe)
	if err != nil {
		return err
	}
	if info.Path != "github.com/blisspixel/nemalo/internal/staging/stagingtest/cmd/archiver" || info.GoVersion != runtime.Version() {
		return errors.New("synthetic archiver must use the same handler and toolchain")
	}
	race, covered := false, false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "-race":
			race = setting.Value == "true"
		case "-cover":
			covered = setting.Value == "true"
		}
	}
	if race != libraryTestRace || covered != (testing.CoverMode() != "") {
		return errors.New("synthetic archiver instrumentation mismatch")
	}
	return nil
}

// Go test's own report selects only its executable's metadata identity. Convert
// the separate reader counters with the stock tool, then append its text records
// to Go's profile, preserving the original records (including uncovered code).
func finishArchiveFixtureCoverage() error {
	entries, err := os.ReadDir(archiveFixtureCounters)
	if err != nil || len(entries) == 0 {
		return err // A filtered suite may never invoke the reader.
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	work, err := os.MkdirTemp("", "nemalo-reader-report-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	profile := filepath.Join(work, "reader.out")
	cmd := exec.CommandContext(ctx, "go", "tool", "covdata", "textfmt", "-i=.", "-o="+profile)
	cmd.Dir = archiveFixtureCounters
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN="+runtime.Version(), "GOPROXY=off", "GOSUMDB=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("convert synthetic reader coverage: %w: %s", err, out)
	}
	if output := flag.Lookup("test.coverprofile").Value.String(); output != "" {
		f, err := os.Open(profile)
		if err != nil {
			return err
		}
		defer f.Close()
		r := bufio.NewReader(f)
		mode, err := r.ReadString('\n')
		if err != nil || mode != "mode: "+testing.CoverMode()+"\n" {
			return errors.New("synthetic reader coverage mode mismatch")
		}
		out, err := os.OpenFile(output, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, r)
		if err := errors.Join(copyErr, out.Close()); err != nil {
			return err
		}
	}
	// Keep raw counters available to callers comparing full compiled suites.
	// A manually run test binary may request only a text profile and supply no
	// original counter directory. Never publish into its working directory.
	if libraryTestCounters == "" {
		return nil
	}
	merged := filepath.Join(work, "merged")
	if err := os.Mkdir(merged, 0700); err != nil {
		return err
	}
	return mergeRecoveryCounters(ctx, archiveFixtureCounters, merged, libraryTestCounters, &recoveryProcessOutput{})
}
