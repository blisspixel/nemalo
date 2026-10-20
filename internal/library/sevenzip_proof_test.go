package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/blisspixel/nemalo/internal/safeio"
	"github.com/blisspixel/nemalo/internal/staging"
	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
)

func newFlatSevenFixture(t *testing.T, count int, build func([]byte) []byte) streamFixture {
	t.Helper()
	if build != nil {
		t.Fatal("flat 7z fixture requires its own reader profile for changed members")
	}
	f := streamFixture{root: t.TempDir(), folder: t.TempDir(), archiveProof: archiveTreeProof}
	ctx := context.Background()
	if _, err := Initialize(ctx, f.root); err != nil {
		t.Fatal(err)
	}
	source := writeEPUBParagraph(t, t.TempDir(), "Versioned 7z proof")
	if _, err := Import(ctx, ImportRequest{Library: f.root, Source: source, Apply: true, Scan: true, Scanner: cleanScan{}}); err != nil {
		t.Fatal(err)
	}
	body, profile := cleanupTreeSeven("flat", nestedFile{"book.epub", mustRead(t, source)})
	for i := 0; i < count; i++ {
		name := "books/flat.7z"
		if count > 1 {
			name = fmt.Sprintf("books/flat.7z.%03d", i+1)
		}
		part := bytes.Clone(body[i*len(body)/count : (i+1)*len(body)/count])
		f.names = append(f.names, name)
		f.parts = append(f.parts, part)
		writeTree(t, f.folder, map[string][]byte{name: part})
	}
	if count > 1 {
		profile.Volumes = []stagingtest.Volume{{Name: "archive.7z", Body: body}}
	}
	stagingtest.Install(t, profile)
	return f
}

// Recreate an immutable older cleanup receipt containing a gap in its encoded
// bytes. These tests never ask current planning to grant those bytes authority.
func legacySevenFixture(t *testing.T, count int, pending bool) (streamFixture, CleanupResult, string) {
	t.Helper()
	f := newFlatSevenFixture(t, count, nil)
	p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
	if err != nil || p.Move != 1 {
		t.Fatal(p, err)
	}
	crash := errors.New("historical 7z interruption")
	if pending {
		cleanupHook = func(stage string) error {
			if stage == "partial" {
				return crash
			}
			return nil
		}
		t.Cleanup(func() { cleanupHook = nil })
	}
	c, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest})
	cleanupHook = nil
	if pending && !errors.Is(err, crash) || !pending && err != nil {
		t.Fatal(c, err)
	}
	parent := filepath.Join(c.HoldingArea, "files")
	if pending {
		parent = f.folder
	}
	// The old header/physical-size-only policy did not account for such material.
	f.parts[len(f.parts)-1] = append(f.parts[len(f.parts)-1], []byte("unheld encoded historical material")...)
	for i, name := range f.names {
		if err := os.WriteFile(filepath.Join(parent, filepath.FromSlash(name)), f.parts[i], 0600); err != nil {
			t.Fatal(err)
		}
	}
	digest := rewriteTreeCleanup(t, f.root, c.CleanupID, func(m *cleanupManifest) {
		e := &m.Entries[0]
		e.ArchiveProof = ""
		e.ArchiveLeaves = nil
		for i := range e.Files {
			sum := sha256.Sum256(f.parts[i])
			e.Files[i].Bytes = int64(len(f.parts[i]))
			e.Files[i].SHA256 = hex.EncodeToString(sum[:])
		}
	})
	saved := staging.SevenZip
	staging.SevenZip = func() (string, error) { return "", staging.ErrNoArchiveTool }
	t.Cleanup(func() { staging.SevenZip = saved })
	return f, c, digest
}

func TestHistoricalSevenCleanupRequiresVersionedProofBeforeWrites(t *testing.T) {
	for _, count := range []int{1, 3} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f, c, digest := legacySevenFixture(t, count, true)
			beforeSource := snapshotTree(t, f.folder)
			beforeHeld := snapshotTree(t, filepath.Join(c.HoldingArea, "files"))
			_, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder, Apply: digest})
			if !errors.Is(err, ErrStateReview) {
				t.Fatal("unversioned 7z cleanup resumed", err)
			}
			if !reflect.DeepEqual(beforeSource, snapshotTree(t, f.folder)) || !reflect.DeepEqual(beforeHeld, snapshotTree(t, filepath.Join(c.HoldingArea, "files"))) {
				t.Fatal("unversioned recovery changed bytes")
			}
		})
	}
}

func TestHistoricalSevenPurgePreservesEveryPartAndRestore(t *testing.T) {
	for _, count := range []int{1, 3} {
		for _, mode := range []int{0, 1, 2} {
			if count == 1 && mode == 2 {
				continue
			}
			pending := mode != 0
			t.Run(fmt.Sprintf("%d/pending=%d", count, mode), func(t *testing.T) {
				f, c, _ := legacySevenFixture(t, count, false)
				ctx := context.Background()
				assets := snapshotTree(t, filepath.Join(f.root, "assets"))
				p, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
				if err != nil {
					t.Fatal(err)
				}
				if !pending && p.Purge != 0 {
					t.Fatal("unversioned 7z received purge permission", p)
				}
				digest := p.Digest
				if pending {
					for i := range p.Files {
						p.Files[i].Disposition = "purge"
					}
					digest = purgeDigest(c.CleanupID, p.Files)
					control, _, err := openControl(f.root, true)
					if err != nil {
						t.Fatal(err)
					}
					records, err := readCleanup(control, c.LibraryID)
					if err != nil {
						control.close()
						t.Fatal(err)
					}
					store, err := safeio.OpenRoot(f.root)
					if err != nil {
						control.close()
						t.Fatal(err)
					}
					rec := cleanupRecord{SchemaVersion: 1, Sequence: len(records) + 1, LibraryID: c.LibraryID, CleanupID: c.CleanupID, Event: "purge_started", Digest: digest, Count: len(p.Files), At: now()}
					pm := purgeManifest{SchemaVersion: 1, LibraryID: c.LibraryID, CleanupID: c.CleanupID, Digest: digest, Files: p.Files}
					rec.Manifest, err = writePurgeManifest(store, pm, rec.Sequence)
					if err == nil {
						err = appendCanonical(ctx, control, cleanupName, maxCleanupJournal, rec)
					}
					if err == nil && mode == 2 {
						err = os.Remove(filepath.Join(c.HoldingArea, "files", filepath.FromSlash(f.names[0])))
						if err == nil {
							err = appendCanonical(ctx, control, cleanupName, maxCleanupJournal, cleanupRecord{SchemaVersion: 1, Sequence: rec.Sequence + 1, LibraryID: c.LibraryID, CleanupID: c.CleanupID, Event: "purge_entry", Index: 0, Outcome: "purged", At: now()})
						}
					}
					store.Close()
					control.close()
					if err != nil {
						t.Fatal(err)
					}
				}
				r, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: digest})
				alreadyPurged := 0
				if mode == 2 {
					alreadyPurged = 1
				}
				if err != nil || r.Purged != alreadyPurged || r.Resumed != pending {
					t.Fatal("unversioned encoded bytes removed", r, err)
				}
				for _, file := range r.Files {
					if mode == 2 && file.Index == 0 && file.Outcome == "purged" {
						continue
					}
					if file.Disposition != "archive_not_accounted" && file.Outcome != "archive_not_accounted" {
						t.Fatal(file)
					}
				}
				if mode == 2 {
					if _, err := os.Stat(filepath.Join(c.HoldingArea, "files", filepath.FromSlash(f.names[0]))); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("completed removal adopted a replacement", err)
					}
					f.names, f.parts = f.names[1:], f.parts[1:]
				}
				f.assertParts(t, filepath.Join(c.HoldingArea, "files"))
				if !reflect.DeepEqual(assets, snapshotTree(t, filepath.Join(f.root, "assets"))) {
					t.Fatal("stored bytes changed")
				}
				restored, err := Restore(ctx, RestoreRequest{Library: f.root, CleanupID: c.CleanupID, Apply: true})
				if err != nil || restored.Restore != count-alreadyPurged {
					t.Fatal(restored, err)
				}
				f.assertParts(t, f.folder)
			})
		}
	}
}

func TestFlatSevenVersionedSetRecovery(t *testing.T) {
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
	} {
		t.Run(scenario.name, func(t *testing.T) { scenario.run(t, newFlatSevenFixture) })
	}
}

func TestFlatSevenSingleProofLifecycle(t *testing.T) {
	f := newFlatSevenFixture(t, 1, nil)
	ctx := context.Background()
	p, err := Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder})
	if err != nil || p.Move != 1 || p.Entries[0].ArchiveProof != archiveTreeProof || !validArchiveLeaves(p.Entries[0]) || len(p.Entries[0].ArchiveLeaves) != 1 || !reflect.DeepEqual(p.Entries[0].ArchiveLeaves[0].Chain, []string{"book.epub"}) {
		t.Fatal(p, err)
	}
	c := f.cleanup(t)
	q, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
	if err != nil || q.Purge != 1 || len(q.ArchiveGroups) != 1 || q.ArchiveGroups[0].Proof != archiveTreeProof || q.ArchiveGroups[0].LeafDigest != archiveLeafDigest(p.Entries[0]) || !reflect.DeepEqual(q.ArchiveGroups[0].Indices, []int{0}) {
		t.Fatal(q, err)
	}
	crash := errors.New("single 7z purge interruption")
	cleanupHook = func(stage string) error {
		if stage == "purged" {
			return crash
		}
		return nil
	}
	t.Cleanup(func() { cleanupHook = nil })
	if _, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: q.Digest}); !errors.Is(err, crash) {
		t.Fatal(err)
	}
	cleanupHook = nil
	saved := staging.SevenZip
	staging.SevenZip = func() (string, error) { t.Fatal("partial purge re-extracted the source"); return "", nil }
	t.Cleanup(func() { staging.SevenZip = saved })
	r, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: q.Digest})
	if err != nil || !r.Resumed || r.Purged != 1 {
		t.Fatal(r, err)
	}
}

func TestFlatSevenSingleCleanupAndRestoreInterruptions(t *testing.T) {
	testSingleArchiveCleanupAndRestoreInterruptions(t, func(t *testing.T) streamFixture {
		return newFlatSevenFixture(t, 1, nil)
	})
}

func TestFlatSevenFreshPurgeRepeatsCompleteAccounting(t *testing.T) {
	for _, count := range []int{1, 3} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f := newFlatSevenFixture(t, count, nil)
			c := f.cleanup(t)
			body := bytes.Join(f.parts, nil)
			assets := snapshotTree(t, filepath.Join(f.root, "assets"))
			if len(assets) != 1 {
				t.Fatal("expected exactly one stored book", len(assets))
			}
			var book []byte
			for _, data := range assets {
				book = data
			}
			gap := fmt.Sprintf("Type = 7z\nPhysical Size = %d\nHeaders Size = %d\n", len(body), len(body)-len(book)-9)
			// Same member output and physical size; a gap in header-plus-packed coverage
			// invalidates fresh authorization even though every recorded byte still hashes.
			stagingtest.Install(t, stagingtest.Archive{Kind: "-t7z", Body: body, Header: gap, Members: map[string][]byte{"book.epub": book}, Volumes: []stagingtest.Volume{{Name: "archive.7z", Body: body}}})
			p, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
			if err != nil || p.Purge != 0 || len(p.ArchiveGroups) != 0 {
				t.Fatal(p, err)
			}
			f.assertParts(t, filepath.Join(c.HoldingArea, "files"))
		})
	}
}
