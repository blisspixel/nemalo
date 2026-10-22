package library

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
)

func TestRAR4SetRecovery(t *testing.T) {
	testRARSetRecovery(t, newRAR4SetCleanupFixture)
}

func TestRAR4SetPartCountsAndNestedAuthority(t *testing.T) {
	testRARSetPartCountsAndNestedAuthority(t, 4)
}

func TestRAR4SetCleanupResumeRejectsUnsupportedBindings(t *testing.T) {
	testTreeCleanupResumeBindings(t, func(t *testing.T) streamFixture { return newRAR4SetCleanupFixture(t, 3, nil) }, "old_tree", "newer_tree", "standalone", "rar5_standalone")
}

func TestRAR4SetPendingPurgeRequiresExactAuthority(t *testing.T) {
	testRARTreePurgeBindings(t, func(t *testing.T) streamFixture { return newRAR4SetCleanupFixture(t, 3, nil) }, "newer_tree", "rar4_standalone", "zip_stream", "zip_segments", "rar5_set")
}

func TestRAR4SetIndependentKnownShapesBeforeAnyRemoval(t *testing.T) {
	testRARTreeIndependentShapes(t, func(t *testing.T) streamFixture { return newRAR4SetCleanupFixture(t, 3, nil) }, "newer_tree_second", "rar5_standalone_second", "rar5_set_second")
}

func TestRAR4SetPartialRestorePreservesEveryOriginalAndReplacement(t *testing.T) {
	testSetPartialRestoreKeepsReplacementAndEveryOriginalByte(t, newRAR4SetCleanupFixture)
}

func TestRAR4SetRestorePublicationAndJournalInterruptions(t *testing.T) {
	testRARSetRestorePublicationAndJournalInterruptions(t, newRAR4SetCleanupFixture)
}

func TestRAR4SetUnlistedPhysicalRecordsPreserveEveryVolume(t *testing.T) {
	testRARSetUnlistedPhysicalRecordsPreserveEveryVolume(t, 4)
}

func TestRAR4SetOldNamesCannotBorrowModernAuthority(t *testing.T) {
	testRARSetOldNamesCannotBorrowModernAuthority(t, newRAR4SetCleanupFixture)
}

func TestRARSetFreshPurgeRejectsOppositeMeasuredVersion(t *testing.T) {
	for _, version := range []int{4, 5} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			fixture, opposite := newRAR4SetCleanupFixture, rar5SetProof
			if version == 5 {
				fixture, opposite = newRAR5SetCleanupFixture, rar4SetProof
			}
			f := fixture(t, 3, nil)
			c := f.cleanup(t)
			rewriteTreeCleanup(t, f.root, c.CleanupID, func(m *cleanupManifest) {
				m.Entries[0].ArchiveProof = opposite
				if !supportedArchiveProof(m.Entries[0]) {
					t.Fatal("test must reach physical format checking with a valid persisted shape")
				}
			})
			before := snapshotTree(t, filepath.Join(c.HoldingArea, "files"))
			ctx := context.Background()
			p, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
			if err != nil || p.Purge != 0 || len(p.ArchiveGroups) != 0 {
				t.Fatal("fresh purge accepted wrong measured format", p, err)
			}
			r, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: p.Digest})
			if err != nil || r.Purged != 0 || !reflect.DeepEqual(before, snapshotTree(t, filepath.Join(c.HoldingArea, "files"))) {
				t.Fatal("opposite version removed bytes", r, err)
			}
		})
	}
}

func TestRAR4SetZeroReservedFooterLifecycle(t *testing.T) {
	testZIPCleanupRestoreAndPurge(t, func(t *testing.T, count int, build func([]byte) []byte) streamFixture {
		if build != nil {
			t.Fatal("this lifecycle fixture requires direct leaves")
		}
		return newRAR4SetCleanupWithChild(t, count, "book.epub", nil, func(i int, b []byte) []byte {
			flags := uint16(8 | 4)
			if i+1 < count {
				flags |= 1
			}
			footer := binary.LittleEndian.AppendUint16(nil, uint16(i))
			footer = append(footer, make([]byte, 7)...)
			return append(bytes.Clone(b[:len(b)-9]), cleanupRAR4Block(0x7b, flags, footer, nil)...)
		})
	})
}

func TestRARSetMixedVersionsBindIndependentGroups(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		t.Run(fmt.Sprint(malformed), func(t *testing.T) {
			four, five := newRAR4SetCleanupFixture(t, 3, nil), newRAR5SetCleanupFixture(t, 3, nil)
			ctx := context.Background()
			for _, book := range snapshotTree(t, filepath.Join(five.root, "assets")) {
				source := filepath.Join(t.TempDir(), "second.epub")
				if err := os.WriteFile(source, book, 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := Import(ctx, ImportRequest{Library: four.root, Source: source, Apply: true, Scan: true, Scanner: cleanScan{}}); err != nil {
					t.Fatal(err)
				}
			}
			for i, name := range five.names {
				writeTree(t, four.folder, map[string][]byte{"other/" + filepath.Base(name): five.parts[i]})
			}
			stagingtest.InstallMany(t, append(four.archives, five.archives...)...)
			held := snapshotTree(t, filepath.Join(four.root, "assets"))
			p, err := Cleanup(ctx, CleanupRequest{Library: four.root, Folder: four.folder})
			if err != nil || p.Move != 2 || len(p.Entries) != 2 || p.Entries[0].ArchiveProof != rar4SetProof || p.Entries[1].ArchiveProof != rar5SetProof {
				t.Fatal("mixed sources lost measured versions", p, err)
			}
			c, err := Cleanup(ctx, CleanupRequest{Library: four.root, Folder: four.folder, Apply: p.Digest})
			if err != nil || c.Moved != 2 {
				t.Fatal(c, err)
			}
			q, err := Purge(ctx, PurgeRequest{Library: four.root, CleanupID: c.CleanupID})
			if err != nil || q.Purge != 6 || len(q.ArchiveGroups) != 2 {
				t.Fatal(q, err)
			}
			for i, proof := range []string{rar4SetProof, rar5SetProof} {
				g := q.ArchiveGroups[i]
				if g.Proof != proof || !reflect.DeepEqual(g.Indices, []int{i * 3, i*3 + 1, i*3 + 2}) || g.LeafDigest != archiveLeafDigest(p.Entries[i]) {
					t.Fatal("mixed purge lost exact proof, indices or leaves", q)
				}
			}
			before := snapshotTree(t, filepath.Join(c.HoldingArea, "files"))
			if malformed {
				q.ArchiveGroups[1].Proof = rar4SetProof
				q.Digest = startRAR5TestPurge(t, c, q)
			}
			r, err := Purge(ctx, PurgeRequest{Library: four.root, CleanupID: c.CleanupID, Apply: q.Digest})
			if malformed {
				if !errors.Is(err, ErrStateReview) || !reflect.DeepEqual(before, snapshotTree(t, filepath.Join(c.HoldingArea, "files"))) {
					t.Fatal("later version mismatch removed earlier independent bytes", r, err)
				}
			} else if err != nil || r.Purged != 6 || len(snapshotTree(t, filepath.Join(c.HoldingArea, "files"))) != 0 {
				t.Fatal("mixed valid groups failed purge", r, err)
			}
			if !reflect.DeepEqual(held, snapshotTree(t, filepath.Join(four.root, "assets"))) {
				t.Fatal("mixed cleanup changed held books")
			}
		})
	}
}
