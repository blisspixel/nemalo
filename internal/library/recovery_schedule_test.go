package library

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
)

var libraryRecoverySchedule *recoverySchedule
var rarTreeRecoveryVersions = []int{4, 5}
var rarTreeRecoveryLayouts = []string{"zip_chunks", "zip_segments", "7z_chunks"}

const fixtureCasePrefix = "fixture:"

// These fixed groups keep the original fixture bodies and Go test selections.
// They share the recovery workers rather than adding another concurrency pool.
var fixtureSchedule = []struct {
	root   string
	groups []string
}{
	{"TestArchiveTreeCleanupAndPurgeRecovery", []string{"zip_chunks", "zip_segments", "7z_chunks"}},
	{"TestSplitZIPCleanupAndPurgeRecovery", []string{"stream", "segments"}},
	{"TestRAR4SetPartCountsAndNestedAuthority", []string{""}},
	{"TestRAR5SetPartCountsAndNestedAuthority", []string{""}},
	{"TestOldRAR4SetPartCountsAndNestedAuthority", []string{""}},
	{"TestOldRAR5SetPartCountsAndNestedAuthority", []string{""}},
	{"TestNestedVolumeTreeRecovery", []string{"zip_chunks", "rar5_set"}},
	{"TestNestedVolumeRestoreAndImmutableKnownBindings", []string{"zip_chunks", "rar5_set"}},
	{"TestOldRARSetExactRecoveryBindings", []string{"4", "5"}},
	{"TestNestedVolumeCleanupBindsAllOuterAndChildSelectionLayouts", nestedVolumeCleanupGroups()},
	{"TestFlatSevenVersionedSetRecovery", []string{""}},
	{"TestOldRARSetFreshPurgeRejectsOtherFormatAndLayout", []string{""}},
	{"TestOldRARSetRestoreInterruptionsAndReplacements", []string{""}},
	{"TestRAR4SetPendingPurgeRequiresExactAuthority", []string{""}},
	{"TestRAR4SetIndependentKnownShapesBeforeAnyRemoval", []string{""}},
	{"TestRAR5SetPendingPurgeRequiresExactAuthority", []string{""}},
	{"TestRAR5SetIndependentKnownShapesBeforeAnyRemoval", []string{""}},
	{"TestRARTreePreviewRequiresNewProofAndExactLeaves", []string{""}},
	{"TestRARTreeSingleArchiveCleanupRestoreAndPurge", []string{""}},
	{"TestRARTreeRootRARRecoveryUsesTreeAuthority", []string{""}},
	{"TestRARTreeUnknownAndMalformedPurgeBindingsRetainBytes", []string{""}},
	{"TestRARTreeIndependentGroupsValidateKnownShapesBeforeAnyRemoval", []string{""}},
	{"TestRARTreeCleanupResumeRejectsUnsupportedProofsBeforeWrites", []string{""}},
}

// These names are the original independent lifecycle leaves. Go prints their
// complete names, not a separate result for an implicit slash-created parent.
func nestedVolumeCleanupGroups() []string {
	var groups []string
	for _, outer := range nestedVolumeOuterLayouts {
		groups = append(groups, outer+"/old5")
	}
	for _, inner := range nestedVolumeChildLayouts {
		if inner != "old5" {
			groups = append(groups, "zip/"+inner)
		}
	}
	return groups
}

func fixtureGroups(root string) []string {
	for _, item := range fixtureSchedule {
		if item.root == root {
			return item.groups
		}
	}
	return nil
}

func runScheduledFixtures(t *testing.T, groups []string) bool {
	t.Helper()
	root := t.Name()
	if !slices.Equal(groups, fixtureGroups(root)) {
		t.Fatal("fixture schedule does not include exactly the original groups")
	}
	if selected := os.Getenv(rarRecoveryCaseEnv); selected != "" {
		if !strings.HasPrefix(selected, fixtureCasePrefix) || !slices.Contains(groups, strings.TrimPrefix(selected, fixtureCasePrefix)) {
			t.Fatal("unknown isolated fixture group", selected)
		}
		return false
	}
	if libraryRecoverySchedule == nil {
		return false // Filtered, repeated and constrained runs keep ordinary execution.
	}
	t.Parallel()
	libraryRecoverySchedule.serialFinished()
	collect := func(t *testing.T, group string) {
		t.Helper()
		out, err := libraryRecoverySchedule.collect(root, fixtureCasePrefix+group)
		checkRecoveryResult(t, root, group, out, err)
	}
	for _, group := range groups {
		if group == "" {
			collect(t, group)
		} else {
			t.Run(group, func(t *testing.T) {
				t.Parallel()
				collect(t, group)
			})
		}
	}
	return true
}

type recoveryCase struct{ root, selected string }
type recoveryCaseResult struct {
	output       []byte
	err          error
	began, ended time.Time
}
type scheduledRecoveryCase struct {
	done     chan recoveryCaseResult
	settings recoveryProcessSettings
	claimed  bool
}
type recoverySchedule struct {
	cancel             context.CancelFunc
	workspace          string
	jobs               map[recoveryCase]*scheduledRecoveryCase
	wait               sync.WaitGroup
	mu                 sync.Mutex
	serial             sync.Once
	started, serialEnd time.Time
}

// Repeated, filtered, listed, CPU-matrix and fuzz invocations retain the ordinary
// test runner. Never reuse a scheduled result for a second test execution.
func defaultRecoverySchedule() bool {
	for name, want := range map[string]string{
		"test.run": "", "test.skip": "", "test.list": "", "test.count": "1",
		"test.cpu": "", "test.fuzz": "", "test.bench": "", "test.short": "false",
		"test.fuzzworker": "false",
	} {
		f := flag.Lookup(name)
		if f == nil || f.Value.String() != want {
			return false
		}
	}
	parallel, err := strconv.Atoi(flag.Lookup("test.parallel").Value.String())
	if err != nil || parallel < 3 {
		return false
	}
	return true
}

func startRecoverySchedule() (*recoverySchedule, error) {
	settings, err := captureRecoveryProcessSettings()
	if err != nil {
		return nil, err
	}
	if testing.CoverMode() != "" && settings.testCounters == "" {
		return nil, errors.New("covered recovery subprocess requires Go test's counter directory")
	}
	reader, err := stagingtest.Executable()
	if err != nil {
		return nil, err
	}
	digest, err := archiveFixtureHash(reader)
	if err != nil {
		return nil, err
	}
	workspace, err := os.MkdirTemp(settings.scratch, "nemalo-recovery-schedule-")
	if err != nil {
		return nil, err
	}
	limit, err := time.ParseDuration(flag.Lookup("test.timeout").Value.String())
	if err != nil {
		_ = os.RemoveAll(workspace)
		return nil, err
	}
	// Start the same overall timeout before any scheduled test work, rather
	// than giving prestarted children time outside M.Run's package alarm.
	var ctx context.Context
	var cancel context.CancelFunc
	if limit > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), limit)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	s := &recoverySchedule{cancel: cancel, workspace: workspace, jobs: make(map[recoveryCase]*scheduledRecoveryCase), started: time.Now()}
	cases := []recoveryCase{{"TestRAR4SetRecovery", "direct"}, {"TestRAR5SetRecovery", "direct"}, {"TestOldRAR4SetRecovery", "direct"}, {"TestOldRAR5SetRecovery", "direct"}}
	for _, version := range rarTreeRecoveryVersions {
		for _, layout := range rarTreeRecoveryLayouts {
			cases = append(cases, recoveryCase{"TestRARTreeRecoveryAcrossEveryOuterSetLayout", fmt.Sprintf("%d/%s", version, layout)})
		}
	}
	for _, item := range fixtureSchedule {
		for _, group := range item.groups {
			cases = append(cases, recoveryCase{item.root, fixtureCasePrefix + group})
		}
	}
	// Larger nested/tree groups go first. Stable ties retain their declared
	// order; goroutine/token contention must not choose the dispatch order.
	slices.SortStableFunc(cases, func(a, b recoveryCase) int {
		return recoveryWorkClass(a) - recoveryWorkClass(b)
	})
	for i, key := range cases {
		if s.jobs[key] != nil {
			cancel()
			_ = os.RemoveAll(workspace)
			return nil, errors.New("duplicate isolated test group")
		}
		job := &scheduledRecoveryCase{done: make(chan recoveryCaseResult, 1), settings: settings}
		job.settings.scratch = workspace
		if testing.CoverMode() != "" {
			job.settings.testCounters = filepath.Join(workspace, fmt.Sprintf("tests-%d", i))
			job.settings.readerCounters = filepath.Join(workspace, fmt.Sprintf("readers-%d", i))
			for _, dir := range []string{job.settings.testCounters, job.settings.readerCounters} {
				if err := os.Mkdir(dir, 0700); err != nil {
					cancel()
					_ = os.RemoveAll(workspace)
					return nil, err
				}
			}
		}
		s.jobs[key] = job
	}
	// Reserve one lane for ordinary serial fixtures and their control helpers.
	// Two isolated groups can overlap them; all three lanes become available
	// when Go releases the parallel roots after the serial phase.
	recoveryProcessSlots <- struct{}{}
	s.startWorkers(ctx, cases, recoveryProcessSlots, func(key recoveryCase) recoveryCaseResult {
		child, stop := context.WithTimeout(ctx, 5*time.Minute)
		defer stop()
		began := time.Now()
		out, err := recoveryProcessWithSettings(child, key.root, key.selected, reader, digest, s.jobs[key].settings)
		return recoveryCaseResult{output: out, err: err, began: began, ended: time.Now()}
	})
	return s, nil
}

func recoveryWorkClass(key recoveryCase) int {
	switch key.root {
	case "TestNestedVolumeRestoreAndImmutableKnownBindings", "TestNestedVolumeTreeRecovery":
		return 0
	case "TestRARTreeRecoveryAcrossEveryOuterSetLayout":
		return 1
	case "TestArchiveTreeCleanupAndPurgeRecovery":
		return 2
	default:
		return 3
	}
}

// Acquire a lane before taking the next queued item. A reserved ordinary lane
// must not privately strand a large item while later jobs pass it. Cancellation
// reports every unstarted selection, so collectors cannot wait forever.
func (s *recoverySchedule) startWorkers(ctx context.Context, cases []recoveryCase, slots chan struct{}, run func(recoveryCase) recoveryCaseResult) {
	pending := make(chan recoveryCase, len(cases))
	for _, key := range cases {
		pending <- key
	}
	close(pending)
	for range cap(slots) {
		s.wait.Go(func() {
			for {
				select {
				case slots <- struct{}{}:
				case <-ctx.Done():
					for key := range pending {
						s.jobs[key].done <- recoveryCaseResult{err: ctx.Err()}
					}
					return
				}
				key, ok := <-pending
				if !ok {
					<-slots
					return
				}
				result := recoveryCaseResult{err: ctx.Err()}
				if result.err == nil {
					result = run(key)
				}
				s.jobs[key].done <- result
				<-slots
			}
		})
	}
}

func (s *recoverySchedule) serialFinished() {
	s.serial.Do(func() {
		s.mu.Lock()
		s.serialEnd = time.Now()
		s.mu.Unlock()
		<-recoveryProcessSlots
	})
}

func (s *recoverySchedule) claim(key recoveryCase) (*scheduledRecoveryCase, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.jobs[key]
	if job == nil || job.claimed {
		return nil, fmt.Errorf("unregistered or repeated recovery case %s/%s", key.root, key.selected)
	}
	job.claimed = true
	return job, nil
}

func (s *recoverySchedule) collect(root, selected string) ([]byte, error) {
	job, err := s.claim(recoveryCase{root, selected})
	if err != nil {
		return nil, err
	}
	r := <-job.done
	if r.err == nil && testing.CoverMode() != "" {
		// Parent fixtures have finished. Only now publish privately accumulated
		// counters beside the parent's reader counters, avoiding metadata races.
		r.err = publishRecoveryCoverage(job.settings.testCounters, libraryTestCounters)
		if r.err == nil {
			r.err = publishRecoveryCoverage(job.settings.readerCounters, archiveFixtureCounters)
		}
	}
	if !r.began.IsZero() {
		s.mu.Lock()
		serialEnd := s.serialEnd
		s.mu.Unlock()
		timing := fmt.Appendf(nil, "scheduled recovery start=%s serial_end=%s duration=%s\n", r.began.Sub(s.started), serialEnd.Sub(s.started), r.ended.Sub(r.began))
		r.output = append(timing, r.output...)
	}
	return r.output, r.err
}

func (s *recoverySchedule) complete() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, job := range s.jobs {
		if !job.claimed {
			return fmt.Errorf("recovery case was not collected: %s/%s", key.root, key.selected)
		}
	}
	return nil
}

func (s *recoverySchedule) close(published bool) error {
	s.cancel()
	s.serialFinished()
	s.wait.Wait()
	return finishCoverageScratch(s.workspace, published)
}

func TestRecoveryScheduleRequiresExactConsumption(t *testing.T) {
	a, b := recoveryCase{"root", "a"}, recoveryCase{"root", "b"}
	s := &recoverySchedule{jobs: map[recoveryCase]*scheduledRecoveryCase{a: {}, b: {}}}
	if s.complete() == nil {
		t.Fatal("unexecuted schedule accepted")
	}
	if _, err := s.claim(a); err != nil {
		t.Fatal(err)
	}
	if _, err := s.claim(a); err == nil {
		t.Fatal("reused recovery result accepted")
	}
	if _, err := s.claim(recoveryCase{"unknown", "case"}); err == nil {
		t.Fatal("unknown scheduled case accepted")
	}
	if s.complete() == nil {
		t.Fatal("partly unexecuted schedule accepted")
	}
	if _, err := s.claim(b); err != nil || s.complete() != nil {
		t.Fatal("complete schedule refused", err)
	}
}
