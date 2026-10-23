package library

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
)

func TestOldRAR4SetRecovery(t *testing.T) {
	testRARSetRecovery(t, newOldRAR4SetFixture)
}

func TestOldRARSetExactRecoveryBindings(t *testing.T) {
	if runScheduledFixtures(t, []string{"4", "5"}) {
		return
	}
	for _, version := range []int{4, 5} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			factory, opposite := newOldRAR4SetFixture, "rar5_old_set"
			if version == 5 {
				factory, opposite = newOldRAR5SetFixture, "rar4_old_set"
			}
			fixture := func(t *testing.T) streamFixture { return factory(t, 3, nil) }
			t.Run("cleanup", func(t *testing.T) {
				testTreeCleanupResumeBindings(t, fixture, "old_tree", "newer_tree", "standalone", "rar5_standalone")
			})
			t.Run("pending purge", func(t *testing.T) {
				testRARTreePurgeBindings(t, fixture, "newer_tree", "rar4_set", "rar5_set", opposite)
			})
			t.Run("independent groups", func(t *testing.T) {
				testRARTreeIndependentShapes(t, fixture, "newer_tree_second", "rar4_set_second", "rar5_set_second", opposite+"_second")
			})
		})
	}
}

func TestOldRARSetFreshPurgeRejectsOtherFormatAndLayout(t *testing.T) {
	if runScheduledFixtures(t, fixtureGroups(t.Name())) {
		return
	}
	for _, version := range []int{4, 5} {
		for _, substitute := range []string{rar4OldSetProof, rar5OldSetProof, rar4SetProof, rar5SetProof, archiveRARTreeProof, "rar-old-set/future", ""} {
			if substitute == fmt.Sprintf("rar%d-old-set/1", version) {
				continue
			}
			t.Run(fmt.Sprintf("%d/%s", version, substitute), func(t *testing.T) {
				fixture := newOldRAR4SetFixture
				if version == 5 {
					fixture = newOldRAR5SetFixture
				}
				f := fixture(t, 3, nil)
				c := f.cleanup(t)
				rewriteTreeCleanup(t, f.root, c.CleanupID, func(m *cleanupManifest) { m.Entries[0].ArchiveProof = substitute })
				before := snapshotTree(t, filepath.Join(c.HoldingArea, "files"))
				p, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
				if err != nil || p.Purge != 0 || len(p.ArchiveGroups) != 0 {
					t.Fatal("fresh purge borrowed another proof", p, err)
				}
				r, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: p.Digest})
				if err != nil || r.Purged != 0 || !reflect.DeepEqual(before, snapshotTree(t, filepath.Join(c.HoldingArea, "files"))) {
					t.Fatal("wrong proof removed bytes", r, err)
				}
			})
		}
	}
}

func TestOldRAR5SetRecovery(t *testing.T) {
	testRARSetRecovery(t, newOldRAR5SetFixture)
}

func TestOldRAR4SetPartCountsAndNestedAuthority(t *testing.T) {
	testRARSetPartCountsWithFixtures(t, newOldRAR4SetFixture, func(t *testing.T, count int, name string, build func([]byte) ([]byte, []stagingtest.Archive), alter ...func(int, []byte) []byte) streamFixture {
		return makeOldRARSet(t, newRAR4SetCleanupWithChild(t, count, name, build, alter...), 4)
	})
}

func TestOldRAR5SetPartCountsAndNestedAuthority(t *testing.T) {
	testRARSetPartCountsWithFixtures(t, newOldRAR5SetFixture, func(t *testing.T, count int, name string, build func([]byte) ([]byte, []stagingtest.Archive), alter ...func(int, []byte) []byte) streamFixture {
		return makeOldRARSet(t, newRAR5SetCleanupWithChild(t, count, name, build, alter...), 5)
	})
}

func TestOldRARSetRestoreInterruptionsAndReplacements(t *testing.T) {
	if runScheduledFixtures(t, fixtureGroups(t.Name())) {
		return
	}
	for _, fixture := range []struct {
		name string
		make zipFixtureFactory
	}{{"4", newOldRAR4SetFixture}, {"5", newOldRAR5SetFixture}} {
		t.Run(fixture.name, func(t *testing.T) {
			t.Run("publication and journal", func(t *testing.T) { testRARSetRestorePublicationAndJournalInterruptions(t, fixture.make) })
			t.Run("replacement and partial", func(t *testing.T) { testSetPartialRestoreKeepsReplacementAndEveryOriginalByte(t, fixture.make) })
		})
	}
}
