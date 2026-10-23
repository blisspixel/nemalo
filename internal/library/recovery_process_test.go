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
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
)

const rarRecoveryCaseEnv = "NEMALO_TEST_RAR_RECOVERY_CASE"

var recoveryProcessSlots = make(chan struct{}, 3)
var recoveryCoveragePublish sync.Mutex
var recoveryCounterTimestamp atomic.Int64
var recoveryMergedCounterName = regexp.MustCompile(`^covcounters\.[0-9a-f]{32}\.0\.[0-9]+$`)

type recoveryProcessSettings struct {
	environment                      []string
	directory, scratch, goExecutable string
	testCounters, readerCounters     string
}

func captureRecoveryProcessSettings() (recoveryProcessSettings, error) {
	directory, err := os.Getwd()
	if err != nil {
		return recoveryProcessSettings{}, err
	}
	goExecutable, err := exec.LookPath("go")
	if err != nil {
		return recoveryProcessSettings{}, err
	}
	return recoveryProcessSettings{environment: os.Environ(), directory: directory, scratch: os.TempDir(), goExecutable: goExecutable, testCounters: libraryTestCounters, readerCounters: archiveFixtureCounters}, nil
}

// The same compiled test executable retains race/coverage instrumentation.
// Each covered child writes privately: concurrent Go metadata renames into a
// shared directory can fail on Windows. Merge counters only after it exits.
func recoveryProcess(ctx context.Context, root, selected, reader, digest string) ([]byte, error) {
	settings, err := captureRecoveryProcessSettings()
	if err != nil {
		return nil, err
	}
	return recoveryProcessWithSettings(ctx, root, selected, reader, digest, settings)
}

func recoveryRunPattern(root, selected string) (string, error) {
	pattern := "^" + regexp.QuoteMeta(root) + "$"
	if strings.HasPrefix(selected, fixtureCasePrefix) {
		group := strings.TrimPrefix(selected, fixtureCasePrefix)
		if !slices.Contains(fixtureGroups(root), group) {
			return "", errors.New("unknown isolated fixture group")
		}
		if group != "" {
			pattern += "/^" + regexp.QuoteMeta(group) + "$"
		}
	}
	return pattern, nil
}

func recoveryProcessWithSettings(ctx context.Context, root, selected, reader, digest string, settings recoveryProcessSettings) (output []byte, resultErr error) {
	pattern, err := recoveryRunPattern(root, selected)
	if err != nil {
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	args := []string{"-test.run=" + pattern, "-test.count=1", "-test.v", "-test.timeout=4m"}
	env := append(append([]string(nil), settings.environment...), rarRecoveryCaseEnv+"="+selected)
	if reader != "" {
		env = append(env, archiveFixtureEnv+"="+reader, archiveFixtureHashEnv+"="+digest)
	}
	var private, parent string
	if testing.CoverMode() != "" {
		parent = settings.testCounters
		if parent == "" {
			return nil, errors.New("covered recovery subprocess requires Go test's counter directory")
		}
		private, err = os.MkdirTemp(settings.scratch, "nemalo-recovery-counters-")
		if err != nil {
			return nil, err
		}
		defer func() {
			if cleanupErr := removeCoverageScratch(private); cleanupErr != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("remove private recovery counters: %w", cleanupErr))
			}
		}()
		for _, name := range []string{"counters", "merged", "reader-counters", "reader-merged"} {
			if err := os.Mkdir(filepath.Join(private, name), 0700); err != nil {
				return nil, err
			}
		}
		counters := filepath.Join(private, "counters")
		args = append(args, "-test.gocoverdir="+counters)
		env = append(env, "GOCOVERDIR="+counters)
		env = append(env, archiveFixtureCountersEnv+"="+filepath.Join(private, "reader-counters"))
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Env = env
	cmd.Dir = settings.directory
	cmd.WaitDelay = time.Second
	out := &recoveryProcessOutput{}
	cmd.Stdout, cmd.Stderr = out, out
	err = cmd.Run()
	if err == nil && private != "" {
		err = mergeRecoveryCountersUsing(ctx, filepath.Join(private, "counters"), filepath.Join(private, "merged"), parent, out, settings)
		if err == nil {
			err = mergeRecoveryCountersUsing(ctx, filepath.Join(private, "reader-counters"), filepath.Join(private, "reader-merged"), settings.readerCounters, out, settings)
		}
	}
	return out.bytes(), err
}

func mergeRecoveryCoverage(ctx context.Context, private, parent string, out *recoveryProcessOutput) error {
	return mergeRecoveryCounters(ctx, filepath.Join(private, "counters"), filepath.Join(private, "merged"), parent, out)
}

func mergeRecoveryCounters(ctx context.Context, counters, merged, parent string, out *recoveryProcessOutput) error {
	settings, err := captureRecoveryProcessSettings()
	if err != nil {
		return err
	}
	return mergeRecoveryCountersUsing(ctx, counters, merged, parent, out, settings)
}

func mergeRecoveryCountersUsing(ctx context.Context, counters, merged, parent string, out *recoveryProcessOutput, settings recoveryProcessSettings) error {
	cmd := exec.CommandContext(ctx, settings.goExecutable, "tool", "covdata", "merge", "-i=.", "-o="+merged)
	// Relative input names avoid covdata's comma-separated path ambiguity.
	cmd.Dir = counters
	cmd.Env = append(append([]string(nil), settings.environment...), "GOTOOLCHAIN="+runtime.Version(), "GOPROXY=off", "GOSUMDB=off")
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("merge recovery counters: %w", err)
	}
	if err := nameRecoveryCounters(merged, time.Now().UnixNano()); err != nil {
		return err
	}
	return publishRecoveryCoverage(merged, parent)
}

// Stock covdata merge uses PID zero and wall time, so independent merges can
// choose the same name on a coarse clock. Assign unique increasing timestamps
// within this publisher before copying. Keep the metadata hash, PID and bytes;
// Go's own tools still read and combine every counter. Publication stays strict.
func nameRecoveryCounters(merged string, now int64) error {
	entries, err := os.ReadDir(merged)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "covcounters.") {
			continue
		}
		if !entry.Type().IsRegular() || !recoveryMergedCounterName.MatchString(name) {
			return fmt.Errorf("unexpected merged counter name %q", name)
		}
		var stamp int64
		for {
			previous := recoveryCounterTimestamp.Load()
			stamp = max(now, previous+1)
			if recoveryCounterTimestamp.CompareAndSwap(previous, stamp) {
				break
			}
		}
		next := name[:strings.LastIndexByte(name, '.')+1] + strconv.FormatInt(stamp, 10)
		if next != name {
			if err := os.Rename(filepath.Join(merged, name), filepath.Join(merged, next)); err != nil {
				return err
			}
		}
	}
	return nil
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
	// Queue the roots together; parallel children alone still make each
	// sequential root wait before the next recovery matrix can start.
	t.Parallel()
	if libraryRecoverySchedule != nil {
		libraryRecoverySchedule.serialFinished()
		root := t.Name()
		for _, selected := range selections {
			t.Run(selected, func(t *testing.T) {
				t.Parallel()
				out, err := libraryRecoverySchedule.collect(root, selected)
				checkRecoveryResult(t, root, selected, out, err)
			})
		}
		return
	}
	reader, err := stagingtest.Executable()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := archiveFixtureHash(reader)
	if err != nil {
		t.Fatal(err)
	}
	root := t.Name()
	for _, selected := range selections {
		t.Run(selected, func(t *testing.T) {
			t.Parallel()
			recoveryProcessSlots <- struct{}{}
			defer func() { <-recoveryProcessSlots }()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
			defer cancel()
			out, err := recoveryProcess(ctx, root, selected, reader, digest)
			checkRecoveryResult(t, root, selected, out, err)
		})
	}
}

func checkRecoveryResult(t *testing.T, root, selected string, out []byte, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("isolated recovery case %s: %v\n%s", selected, err, out)
	}
	name := root
	if selected != "" {
		name += "/" + selected
	}
	if !bytes.Contains(out, []byte("=== RUN   "+name+"\n")) || !bytes.Contains(out, []byte("--- PASS: "+name+" (")) {
		t.Fatalf("recovery subprocess did not execute its selected group %s\n%s", selected, out)
	}
	t.Logf("isolated recovery result:\n%s", out)
}

func TestFixtureProcessSelectionKeepsExactTestGroups(t *testing.T) {
	for _, tc := range []struct{ root, selected, want string }{
		{"TestArchiveTreeCleanupAndPurgeRecovery", "fixture:zip_chunks", "^TestArchiveTreeCleanupAndPurgeRecovery$/^zip_chunks$"},
		{"TestSplitZIPCleanupAndPurgeRecovery", "fixture:segments", "^TestSplitZIPCleanupAndPurgeRecovery$/^segments$"},
		{"TestRAR4SetPartCountsAndNestedAuthority", "fixture:", "^TestRAR4SetPartCountsAndNestedAuthority$"},
		{"TestNestedVolumeRestoreAndImmutableKnownBindings", "fixture:zip_chunks", "^TestNestedVolumeRestoreAndImmutableKnownBindings$/^zip_chunks$"},
		{"TestNestedVolumeRestoreAndImmutableKnownBindings", "fixture:rar5_set", "^TestNestedVolumeRestoreAndImmutableKnownBindings$/^rar5_set$"},
		{"TestOldRARSetExactRecoveryBindings", "fixture:4", "^TestOldRARSetExactRecoveryBindings$/^4$"},
		{"TestOldRARSetExactRecoveryBindings", "fixture:5", "^TestOldRARSetExactRecoveryBindings$/^5$"},
		{"TestNestedVolumeCleanupBindsAllOuterAndChildSelectionLayouts", "fixture:", "^TestNestedVolumeCleanupBindsAllOuterAndChildSelectionLayouts$"},
		{"TestFlatSevenVersionedSetRecovery", "fixture:", "^TestFlatSevenVersionedSetRecovery$"},
		{"TestOldRARSetExactRecoveryBindings", "fixture:6", ""},
		{"TestNestedVolumeRestoreAndImmutableKnownBindings", "fixture:zip_chunks/other", ""},
		{"TestRAR5SetRecovery", "direct", "^TestRAR5SetRecovery$"},
		{"TestRARTreeRecoveryAcrossEveryOuterSetLayout", "4/zip_chunks", "^TestRARTreeRecoveryAcrossEveryOuterSetLayout$"},
		{"Unknown", "fixture:", ""},
		{"TestArchiveTreeCleanupAndPurgeRecovery", "fixture:", ""},
		{"TestSplitZIPCleanupAndPurgeRecovery", "fixture:segments/other", ""},
		{"TestSplitZIPCleanupAndPurgeRecovery", "fixture:.*", ""},
	} {
		got, err := recoveryRunPattern(tc.root, tc.selected)
		if got != tc.want || (err != nil) != (tc.want == "") {
			t.Fatal(tc, got, err)
		}
	}
}

func TestRecoverySubprocessRejectsUnknownCase(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	settings, err := captureRecoveryProcessSettings()
	if err != nil {
		t.Fatal(err)
	}
	settings.scratch = t.TempDir()
	// Background workers must not inherit another serial fixture's temporary
	// reader, PATH, working directory or scratch-directory environment.
	hidden := t.TempDir()
	t.Setenv("NEMALO_STAGINGTEST_7Z", hidden)
	t.Setenv("PATH", hidden)
	for _, name := range []string{"TMPDIR", "TEMP", "TMP"} {
		t.Setenv(name, hidden)
	}
	t.Chdir(hidden)
	out, err := recoveryProcessWithSettings(ctx, "TestRARTreeRecoveryAcrossEveryOuterSetLayout", "unknown/case", "", "", settings)
	if err == nil || !bytes.Contains(out, []byte("unknown RAR recovery case")) {
		t.Fatalf("unexecuted child case reported success: %v\n%s", err, out)
	}
	entries, err := os.ReadDir(hidden)
	if err != nil || len(entries) != 0 {
		t.Fatal("child used a later fixture's scratch directory", entries, err)
	}
}

func TestRecoveryCoveragePublication(t *testing.T) {
	write := func(dir, name, data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("colliding merged timestamps preserve every counter", func(t *testing.T) {
		parent := t.TempDir()
		var wg sync.WaitGroup
		for i := range 8 {
			merged := t.TempDir()
			write(merged, "covmeta."+strings.Repeat("a", 32), "same metadata")
			write(merged, "covcounters."+strings.Repeat("a", 32)+".0.1", strconv.Itoa(i))
			wg.Go(func() {
				// Force the same original name and the same clock for all groups.
				if err := nameRecoveryCounters(merged, 1); err != nil {
					t.Error(err)
					return
				}
				if err := publishRecoveryCoverage(merged, parent); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		entries, err := os.ReadDir(parent)
		if err != nil || len(entries) != 9 {
			t.Fatalf("colliding groups lost counters: %v %v", entries, err)
		}
		seen := make(map[string]bool)
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), "covcounters.") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(parent, entry.Name()))
			if err != nil || !recoveryMergedCounterName.MatchString(entry.Name()) || seen[string(data)] {
				t.Fatalf("counter replaced or malformed: %s %q %v", entry.Name(), data, err)
			}
			seen[string(data)] = true
		}
		for i := range 8 {
			if !seen[strconv.Itoa(i)] {
				t.Fatalf("lost group %d", i)
			}
		}
		bad := t.TempDir()
		write(bad, "covcounters.invalid.0.1", "retain malformed evidence")
		if err := nameRecoveryCounters(bad, 1); err == nil {
			t.Fatal("malformed merged name accepted")
		}
		data, err := os.ReadFile(filepath.Join(bad, "covcounters.invalid.0.1"))
		if err != nil || string(data) != "retain malformed evidence" {
			t.Fatal("malformed input changed", string(data), err)
		}
	})
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
