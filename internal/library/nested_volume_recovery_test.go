package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/blisspixel/nemalo/internal/staging"
)

func TestNestedVolumeSingleOuterRecoveryUsesBoundWholeTreeEvidence(t *testing.T) {
	factory := func(t *testing.T) streamFixture { return newNestedVolumeCleanupFixture(t, "zip", "zip_chunks", 1, nil) }
	t.Run("cleanup and restore interruptions", func(t *testing.T) { testSingleArchiveCleanupAndRestoreInterruptions(t, factory) })
	t.Run("current holding and remaining original", func(t *testing.T) {
		testStandaloneRARPurgeRecoveryChecksFreshHoldingsAndRemainingBytes(t, func(t *testing.T, _ func([]byte) []byte) streamFixture { return factory(t) })
	})
	for _, stage := range []string{"purge manifest", "purged"} {
		t.Run(stage, func(t *testing.T) {
			f := factory(t)
			c := f.cleanup(t)
			q, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
			if err != nil {
				t.Fatal(err)
			}
			crash := errors.New("synthetic single-tree purge interruption")
			cleanupHook = func(at string) error {
				if at == stage {
					return crash
				}
				return nil
			}
			t.Cleanup(func() { cleanupHook = nil })
			if _, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: q.Digest}); !errors.Is(err, crash) {
				t.Fatal("interruption absent", err)
			}
			cleanupHook = nil
			if stage == "purged" {
				saved := staging.SevenZip
				staging.SevenZip = func() (string, error) { t.Fatal("durable single-tree purge reopened reader"); return "", nil }
				t.Cleanup(func() { staging.SevenZip = saved })
			}
			r, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: q.Digest})
			if err != nil || r.Purged != 1 || stage == "purged" && !r.Resumed {
				t.Fatal(r, err)
			}
			if _, err := os.Lstat(filepath.Join(c.HoldingArea, "files", filepath.FromSlash(f.names[0]))); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("original remains", err)
			}
		})
	}
}

func nestedVolumeRecoveryFixture(t *testing.T, outer string, count int, build func([]byte) []byte) streamFixture {
	inner := "old4"
	if outer == "rar5_set" {
		inner = "rar5"
	}
	return newNestedVolumeCleanupFixture(t, outer, inner, count, build)
}

func TestNestedVolumeTreeRecovery(t *testing.T) {
	layouts := []string{"zip_chunks", "rar5_set"}
	if runScheduledFixtures(t, layouts) {
		return
	}
	for _, outer := range layouts {
		t.Run(outer, func(t *testing.T) {
			factory := func(t *testing.T, count int, build func([]byte) []byte) streamFixture {
				return nestedVolumeRecoveryFixture(t, outer, count, build)
			}
			for _, scenario := range []struct {
				name string
				run  func(*testing.T, zipFixtureFactory)
			}{
				{"lifecycle", testZIPCleanupRestoreAndPurge},
				{"cleanup_crashes", testZIPCleanupRecoversEachPartWithoutAReader},
				{"purge_crashes", testZIPPurgeResumesEveryPartFromItsBoundProof},
				{"proof_bindings", testZIPPurgeRetainsUnsupportedOrMalformedEvidence},
				{"fresh_purge", testZIPFreshPurgeRequiresEveryPartAndCurrentHolding},
				{"independent_groups", testZIPPurgeBindsIndependentGroupsAtTheirActualIndices},
				{"stale_and_cancel", testZIPCancelledCleanupAndStalePlansPreserveSources},
				{"purge_retry", testZIPPurgeBeforeIntentRechecksAndResumesKeepChanges},
				{"refusals", testZIPCleanupRefusesUnaccountedSources},
			} {
				t.Run(scenario.name, func(t *testing.T) { scenario.run(t, factory) })
			}
		})
	}
}

func TestNestedVolumeRestoreAndImmutableKnownBindings(t *testing.T) {
	if runScheduledFixtures(t, []string{"zip_chunks", "rar5_set"}) {
		return
	}
	for _, outer := range []string{"zip_chunks", "rar5_set"} {
		t.Run(outer, func(t *testing.T) {
			factory := func(t *testing.T, count int, build func([]byte) []byte) streamFixture {
				return nestedVolumeRecoveryFixture(t, outer, count, build)
			}
			simple := func(t *testing.T) streamFixture { return factory(t, 3, nil) }
			t.Run("restore publication and journal", func(t *testing.T) { testRARSetRestorePublicationAndJournalInterruptions(t, factory) })
			t.Run("restore replacement and partial", func(t *testing.T) { testSetPartialRestoreKeepsReplacementAndEveryOriginalByte(t, factory) })
			t.Run("cleanup bindings", func(t *testing.T) {
				extras := []string{"standalone", "rar5_standalone"}
				// A /2 outer RAR set has an invalid historical shape. An old-name
				// child under ZIP has valid historical syntax; test its original
				// approved digest and fresh reproduction separately below.
				if outer == "rar5_set" {
					extras = append(extras, "newer_tree")
				}
				testTreeCleanupResumeBindings(t, simple, extras...)
			})
			t.Run("pending purge bindings", func(t *testing.T) { testRARTreePurgeBindings(t, simple, "newer_tree", "rar4_set", "rar5_set") })
			t.Run("independent malformed group", func(t *testing.T) {
				testRARTreeIndependentShapes(t, simple, "newer_tree_second", "rar4_set_second", "rar5_set_second")
			})
		})
	}
}

func TestNestedVolumeCleanupResumeKeepsItsOriginallyApprovedDigest(t *testing.T) {
	f := newNestedVolumeCleanupFixture(t, "zip_chunks", "old4", 3, nil)
	p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
	if err != nil {
		t.Fatal(err)
	}
	cleanupHook = func(stage string) error {
		if stage == "partial" {
			return errors.New("synthetic private-copy interruption")
		}
		return nil
	}
	t.Cleanup(func() { cleanupHook = nil })
	c, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest})
	if err == nil {
		t.Fatal("interruption absent")
	}
	cleanupHook = nil
	changed := rewriteTreeCleanup(t, f.root, c.CleanupID, func(m *cleanupManifest) {
		m.Entries[0].ArchiveProof = archiveRARTreeProof
		if !supportedArchiveProof(m.Entries[0]) {
			t.Fatal("historical syntax must be valid for this test")
		}
	})
	if changed == p.Digest {
		t.Fatal("proof change was absent from the plan digest")
	}
	before, held := snapshotTree(t, f.folder), snapshotTree(t, filepath.Join(c.HoldingArea, "files"))
	if _, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest}); err == nil {
		t.Fatal("altered intent reused original approval")
	}
	if !reflect.DeepEqual(before, snapshotTree(t, f.folder)) || !reflect.DeepEqual(held, snapshotTree(t, filepath.Join(c.HoldingArea, "files"))) {
		t.Fatal("changed intent removed or adopted bytes")
	}
}

func TestNestedVolumePartialPurgeCannotBorrowEarlierTreeAuthority(t *testing.T) {
	f := newNestedVolumeCleanupFixture(t, "zip_chunks", "old4", 3, nil)
	c := f.cleanup(t)
	q, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
	if err != nil {
		t.Fatal(err)
	}
	cleanupHook = func(stage string) error {
		if stage == "purged" {
			return errors.New("synthetic removal before journal")
		}
		return nil
	}
	t.Cleanup(func() { cleanupHook = nil })
	if _, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: q.Digest}); err == nil {
		t.Fatal("interruption absent")
	}
	cleanupHook = nil
	digest := changeStreamPurgeManifest(t, f.root, func(pm *purgeManifest) { pm.ArchiveGroups[0].Proof = archiveRARTreeProof })
	before := snapshotTree(t, filepath.Join(c.HoldingArea, "files"))
	if _, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: digest}); !errors.Is(err, ErrStateReview) {
		t.Fatal("earlier proof borrowed immutable new evidence", err)
	}
	if !reflect.DeepEqual(before, snapshotTree(t, filepath.Join(c.HoldingArea, "files"))) {
		t.Fatal("remaining original bytes changed")
	}
}
