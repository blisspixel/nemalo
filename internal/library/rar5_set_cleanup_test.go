package library

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/safeio"
	"github.com/blisspixel/nemalo/internal/staging"
	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
)

func TestRAR5SetRecovery(t *testing.T) {
	testRARSetRecovery(t, newRAR5SetCleanupFixture)
}

func testRARSetRecovery(t *testing.T, fixture zipFixtureFactory) {
	t.Helper()
	selected := os.Getenv(rarRecoveryCaseEnv)
	if selected == "" && flag.Lookup("test.run").Value.String() == "" {
		runRecoveryProcesses(t, []string{"direct"})
		return
	}
	if selected != "" && selected != "direct" {
		t.Fatalf("unknown RAR set recovery case %q", selected)
	}
	t.Run("direct", func(t *testing.T) {
		for _, s := range []struct {
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
			t.Run(s.name, func(t *testing.T) { s.run(t, fixture) })
		}
	})
}

func TestRAR5SetPartCountsAndNestedAuthority(t *testing.T) {
	testRARSetPartCountsAndNestedAuthority(t, 5)
}

func testRARSetPartCountsAndNestedAuthority(t *testing.T, version int) {
	t.Helper()
	fixture, withChild := newRAR5SetCleanupFixture, newRAR5SetCleanupWithChild
	if version == 4 {
		fixture, withChild = newRAR4SetCleanupFixture, newRAR4SetCleanupWithChild
	}
	testRARSetPartCountsWithFixtures(t, fixture, withChild)
}

func testRARSetPartCountsWithFixtures(t *testing.T, fixture zipFixtureFactory, withChild func(*testing.T, int, string, func([]byte) ([]byte, []stagingtest.Archive), ...func(int, []byte) []byte) streamFixture) {
	t.Helper()
	if runScheduledFixtures(t, []string{""}) {
		return
	}
	for _, count := range []int{2, 3, 7} {
		for _, child := range []int{0, 4, 5} {
			t.Run(fmt.Sprintf("%d/child_%d", count, child), func(t *testing.T) {
				var f streamFixture
				if child == 0 {
					f = fixture(t, count, nil)
				} else {
					f = withChild(t, count, "inside.rar", func(book []byte) ([]byte, []stagingtest.Archive) {
						body, profile := cleanupTreeRAR(child, nestedFile{"book.epub", book})
						return body, []stagingtest.Archive{profile}
					})
				}
				before := snapshotTree(t, f.folder)
				p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
				if err != nil || p.Move != 1 || len(p.Entries) != 1 || p.Entries[0].ArchiveProof != f.proof() || !supportedArchiveProof(p.Entries[0]) || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
					t.Fatal(p, err)
				}
				chain := []string{"book.epub"}
				if child != 0 {
					chain = []string{"inside.rar", "book.epub"}
				}
				if len(p.Entries[0].ArchiveLeaves) != 1 || !reflect.DeepEqual(p.Entries[0].ArchiveLeaves[0].Chain, chain) {
					t.Fatal("outer proof lost exact child selections", p)
				}
				c := f.cleanup(t)
				q, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
				if err != nil || q.Purge != count || len(q.ArchiveGroups) != 1 || q.ArchiveGroups[0].Proof != f.proof() || q.ArchiveGroups[0].LeafDigest != archiveLeafDigest(p.Entries[0]) {
					t.Fatal(q, err)
				}
				if r, err := Restore(context.Background(), RestoreRequest{Library: f.root, CleanupID: c.CleanupID, Apply: true}); err != nil || r.Restore != count {
					t.Fatal(r, err)
				}
				f.assertParts(t, f.folder)
				c = f.cleanup(t)
				q, err = Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
				if err != nil {
					t.Fatal(q, err)
				}
				if r, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: q.Digest}); err != nil || r.Purged != count {
					t.Fatal(r, err)
				}
				if len(snapshotTree(t, filepath.Join(c.HoldingArea, "files"))) != 0 {
					t.Fatal("purged source bytes remain")
				}
			})
		}
	}
}

func TestRAR5SetCleanupResumeRejectsUnsupportedBindings(t *testing.T) {
	testTreeCleanupResumeBindings(t, func(t *testing.T) streamFixture { return newRAR5SetCleanupFixture(t, 3, nil) }, "old_tree", "newer_tree", "standalone", "rar5_standalone")
}

func TestRAR5SetPendingPurgeRequiresExactAuthority(t *testing.T) {
	if runScheduledFixtures(t, fixtureGroups(t.Name())) {
		return
	}
	testRARTreePurgeBindings(t, func(t *testing.T) streamFixture { return newRAR5SetCleanupFixture(t, 3, nil) }, "newer_tree", "rar4_standalone", "zip_stream", "zip_segments", "rar4_set")
}

func TestRAR5SetIndependentKnownShapesBeforeAnyRemoval(t *testing.T) {
	if runScheduledFixtures(t, fixtureGroups(t.Name())) {
		return
	}
	testRARTreeIndependentShapes(t, func(t *testing.T) streamFixture { return newRAR5SetCleanupFixture(t, 3, nil) }, "newer_tree_second", "rar5_standalone_second", "rar4_set_second")
}

func TestRAR5SetPartialRestorePreservesEveryOriginalAndReplacement(t *testing.T) {
	testSetPartialRestoreKeepsReplacementAndEveryOriginalByte(t, newRAR5SetCleanupFixture)
}

func TestRAR5SetRestorePublicationAndJournalInterruptions(t *testing.T) {
	testRARSetRestorePublicationAndJournalInterruptions(t, newRAR5SetCleanupFixture)
}

func testRARSetRestorePublicationAndJournalInterruptions(t *testing.T, fixture zipFixtureFactory) {
	t.Helper()
	for _, stage := range []string{"linked", "restored"} {
		for stop := 1; stop <= 3; stop++ {
			t.Run(fmt.Sprintf("%s/%d", stage, stop), func(t *testing.T) {
				f := fixture(t, 3, nil)
				c := f.cleanup(t)
				crash, calls := errors.New("interrupted set restore"), 0
				cleanupHook = func(s string) error {
					if s == stage {
						calls++
						if calls == stop {
							return crash
						}
					}
					return nil
				}
				t.Cleanup(func() { cleanupHook = nil })
				if _, err := Restore(context.Background(), RestoreRequest{Library: f.root, CleanupID: c.CleanupID, Apply: true}); !errors.Is(err, crash) {
					t.Fatal("restore interruption not reached", err)
				}
				cleanupHook = nil
				saved := staging.SevenZip
				staging.SevenZip = func() (string, error) { t.Fatal("raw restore invoked reader"); return "", nil }
				t.Cleanup(func() { staging.SevenZip = saved })
				r, err := Restore(context.Background(), RestoreRequest{Library: f.root, CleanupID: c.CleanupID, Apply: true})
				if err != nil || !r.Resumed || r.Restore+r.Present != 3 {
					t.Fatal(r, err)
				}
				f.assertParts(t, f.folder)
				for name := range snapshotTree(t, f.folder) {
					if strings.Contains(name, ".nemalo-restore-") {
						t.Fatal("restore left a publication partial", name)
					}
				}
			})
		}
	}
}

func TestRAR5SetUnlistedPhysicalRecordsPreserveEveryVolume(t *testing.T) {
	testRARSetUnlistedPhysicalRecordsPreserveEveryVolume(t, 5)
}

func testRARSetUnlistedPhysicalRecordsPreserveEveryVolume(t *testing.T, version int) {
	t.Helper()
	fixture := newRAR5SetCleanupWithChild
	if version == 4 {
		fixture = newRAR4SetCleanupWithChild
	}
	for _, kind := range []string{"service", "tail", "header_crc"} {
		for index := range 3 {
			t.Run(fmt.Sprintf("%s/%d", kind, index), func(t *testing.T) {
				f := fixture(t, 3, "book.epub", nil, func(i int, b []byte) []byte {
					if i != index {
						return b
					}
					switch kind {
					case "service":
						if version == 4 {
							at := len(b) - 9
							out := append(bytes.Clone(b[:at]), cleanupRAR4Block(0x7a, 0x8000, make([]byte, 25), []byte("keep"))...)
							return append(out, b[at:]...)
						}
						end := uint64(0)
						if i < 2 {
							end = 1
						}
						at := len(b) - len(cleanupRAR5SetRecord(5, 0, cleanupRAR5Numbers(end), nil))
						out := append(bytes.Clone(b[:at]), cleanupRAR5SetRecord(3, 2, nil, []byte("keep"))...)
						return append(out, b[at:]...)
					case "tail":
						return append(b, []byte("unlisted tail")...)
					default:
						b[8] ^= 1
						return b
					}
				})
				before := snapshotTree(t, f.folder)
				// The injected ordinary reader deliberately omits these records.
				// Its successful extraction must not establish physical coverage.
				src, err := safeio.OpenRoot(f.folder)
				if err != nil {
					t.Fatal(err)
				}
				defer src.Close()
				dst, err := safeio.OpenRoot(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				defer dst.Close()
				var names []string
				for _, name := range f.names {
					names = append(names, filepath.FromSlash(name))
				}
				x, err := staging.ExtractSet(context.Background(), src, names, dst, supportedImport, staging.Defaults())
				if err != nil || !x.Complete || x.Whole {
					t.Fatal("reader-only prerequisite failed", x, err)
				}
				p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
				if err != nil || p.Move != 0 || p.Keep != 3 || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
					t.Fatal("unlisted records authorized removal", p, err)
				}
			})
		}
	}
}

func TestRAR5SetOldNamesCannotBorrowModernAuthority(t *testing.T) {
	testRARSetOldNamesCannotBorrowModernAuthority(t, newRAR5SetCleanupFixture)
}

func testRARSetOldNamesCannotBorrowModernAuthority(t *testing.T, fixture zipFixtureFactory) {
	t.Helper()
	f := fixture(t, 3, nil)
	for i, name := range f.names {
		old := "books/old.rar"
		if i > 0 {
			old = fmt.Sprintf("books/old.r%02d", i-1)
		}
		if err := os.Rename(filepath.Join(f.folder, filepath.FromSlash(name)), filepath.Join(f.folder, filepath.FromSlash(old))); err != nil {
			t.Fatal(err)
		}
	}
	// The old shape may now request its separate proof, but this reader's
	// modern-volume identity cannot establish the required old-name evidence.
	before := snapshotTree(t, f.folder)
	p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
	if err != nil || p.Move != 0 || p.Keep != 3 || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
		t.Fatal("old names borrowed new authority", p, err)
	}
}
