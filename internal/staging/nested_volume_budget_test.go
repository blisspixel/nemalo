package staging

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type nestedBudgetFixture struct {
	body                    []byte
	first, second           []member
	firstWhole, secondWhole []byte
	secondState             string
}

func TestNestedVolumeJoinChargesCopiedBytesAndCannotRetryAnExhaustedBudget(t *testing.T) {
	first, second := []byte("first generated part"), []byte("second generated part")
	src, dst, _ := roots(t, first)
	if err := src.WriteFile("second", second, 0600); err != nil {
		t.Fatal(err)
	}
	names := []string{"a.zip", "second"}
	infos := make([]fs.FileInfo, 2)
	for i, name := range names {
		info, err := src.Lstat(name)
		if err != nil {
			t.Fatal(err)
		}
		infos[i] = info
	}
	l := nestedAllSetLimits()
	l.child, l.snapshotReads, l.tree = true, true, newTreeBudget(l)
	l.tree.reads.remaining = int64(len(first)) + 1
	err := joinVolumes(context.Background(), src, names, infos, dst, "joined.7z", int64(len(first)+len(second)), l)
	if !errors.Is(err, errTreeBudget) || l.tree.reads.left() != 0 {
		t.Fatal("child join bypassed its shared read allowance", err, l.tree.reads.left())
	}
	if got, err := dst.ReadFile("joined.7z"); err != nil || !bytes.Equal(got, first) {
		t.Fatal("test did not charge a successful first copy", string(got), err)
	}
	err = joinVolumes(context.Background(), src, names, infos, dst, "retry.7z", int64(len(first)+len(second)), l)
	if !errors.Is(err, errTreeBudget) || l.tree.reads.left() != 0 {
		t.Fatal("failed join reset the read allowance", err)
	}
	for i, want := range [][]byte{first, second} {
		if got, err := src.ReadFile(names[i]); err != nil || !bytes.Equal(got, want) {
			t.Fatal("join changed its source", names[i], err)
		}
	}
}

func nestedBudgetData(t *testing.T) nestedBudgetFixture {
	t.Helper()
	four, _, fourWhole := nestedRARSetFixture(t, 4, true)
	fourParts, fourState := zipMembersForNestedTest(t, four), os.Getenv("NEMALO_FAKE_7Z")
	five, _, fiveWhole := nestedRARSetFixture(t, 5, false)
	fiveParts, fiveState := zipMembersForNestedTest(t, five), os.Getenv("NEMALO_FAKE_7Z")
	registry := treeProfiles(t)
	registerNestedFixtureParts(t, registry, fourState, fourParts)
	registerNestedFixtureParts(t, registry, fiveState, fiveParts)
	entries := append(append([]member{}, fourParts...), fiveParts...)
	return nestedBudgetFixture{treeZIP(t, entries...), fourParts, fiveParts, fourWhole, fiveWhole, fiveState}
}

func nestedProofWork(t *testing.T, f nestedBudgetFixture) int64 {
	t.Helper()
	r := &coverageReaderAt{ctx: context.Background(), source: bytes.NewReader(f.body), remaining: archiveProofBytes}
	if ok, why := ZIPAccounted(context.Background(), r, int64(len(f.body))); !ok {
		t.Fatal(why)
	}
	spent := archiveProofBytes - r.remaining
	for _, part := range f.first {
		r = &coverageReaderAt{ctx: context.Background(), source: bytes.NewReader(part.body), remaining: archiveProofBytes}
		if _, err := rar4VolumeAccounted(context.Background(), r, int64(len(part.body)), true); err != nil {
			t.Fatal(err)
		}
		spent += archiveProofBytes - r.remaining
	}
	return spent
}

func TestNestedVolumeGroupsSpendOneSharedBudgetIncludingLaterFailures(t *testing.T) {
	for _, kind := range []string{"members", "output", "proof", "reads", "processes", "diagnostics"} {
		t.Run(kind, func(t *testing.T) {
			f := nestedBudgetData(t)
			src, dst := treeRoots(t, "outer.zip", f.body)
			l := nestedAllSetLimits()
			l.tree = newTreeBudget(l)
			switch kind {
			case "members":
				l.tree.members = 7 // Six outer parts and the first leaf, not a reset per group.
			case "output":
				var partWork int64
				for _, m := range append(append([]member{}, f.first...), f.second...) {
					partWork += int64(len(m.body)) + 1
				}
				l.tree.bytes = partWork + int64(len(f.firstWhole)+1+len(f.secondWhole)) // No second detection byte.
			case "proof":
				l.tree.proofs.remaining = nestedProofWork(t, f)
			case "processes":
				l.tree.processes = 5 // Three isolated probes, one assembled listing and one output.
			case "reads", "diagnostics":
				saved, calls := SevenZip, 0
				SevenZip = func() (string, error) {
					calls++
					if calls == 2 {
						if kind == "reads" {
							l.tree.reads = allowance{}
						} else {
							l.tree.diagnostics = allowance{}
						}
					}
					return saved()
				}
				t.Cleanup(func() { SevenZip = saved })
			}
			x, err := extractTree(context.Background(), src, []string{"outer.zip"}, dst, books, l)
			if err != nil || x.Complete {
				t.Fatal("later work escaped the shared allowance", x, err)
			}
			covered := 1
			if kind == "output" {
				covered = 2 // Framing remains distinct from failed output integrity.
			} else if x.Whole {
				t.Fatal("incomplete framing was reported whole", x)
			}
			if x.CoveredVolumeSets != covered {
				t.Fatal("set facts were reset or inferred from output", x)
			}
			leaves := 0
			for _, m := range x.Members {
				if m.Disposition == "extracted" {
					leaves++
					got, err := dst.ReadFile(m.Staged)
					if err != nil || !bytes.Equal(got, f.firstWhole) {
						t.Fatal("the proved first group changed", m, err)
					}
				}
			}
			if leaves != 1 {
				t.Fatal("later failure skipped or repeated earlier output", x)
			}
			if kind == "proof" && l.tree.proofs.left() != 0 || kind == "reads" && l.tree.reads.left() != 0 || kind == "diagnostics" && l.tree.diagnostics.left() != 0 {
				t.Fatal("failed work refunded an exhausted allowance")
			}
		})
	}
}

func TestNestedVolumeActiveCancellationRetainsSourcesAndEarlierFacts(t *testing.T) {
	f := nestedBudgetData(t)
	if err := os.WriteFile(filepath.Join(f.secondState, "wait"), []byte("active probe"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.secondState, "trace-enabled"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	src, dst := treeRoots(t, "outer.zip", f.body)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	started := make(chan time.Time, 1)
	go func() {
		for ctx.Err() == nil {
			if info, err := os.Stat(filepath.Join(f.secondState, "trace")); err == nil && info.Size() > 0 {
				started <- time.Now()
				cancel()
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	x, err := ExtractTree(ctx, src, []string{"outer.zip"}, dst, books, nestedAllSetLimits())
	if !errors.Is(err, context.Canceled) || x.Complete || x.Whole || x.CoveredVolumeSets != 1 {
		t.Fatal("active cancellation was not retained as incomplete", x, err)
	}
	select {
	case at := <-started:
		if time.Since(at) > 5*time.Second {
			t.Fatal("cancellation did not stop the active reader promptly")
		}
	default:
		t.Fatal("test did not reach the active second-group reader")
	}
	if got, err := src.ReadFile("outer.zip"); err != nil || !bytes.Equal(got, f.body) {
		t.Fatal("cancelled source changed", err)
	}
}
