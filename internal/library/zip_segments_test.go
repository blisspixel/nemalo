package library

import (
	"archive/zip"
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
	"github.com/blisspixel/nemalo/internal/staging/zipfixture"
)

type zipFixtureFactory func(*testing.T, int, func([]byte) []byte) streamFixture

func TestZIPSegmentManifestFamilyUsesPortablePaths(t *testing.T) {
	e := CleanupEntry{Kind: "archive", Path: "nested/books/parts.z01", ArchiveProof: zipSegmentsProof,
		Files: []CleanupFile{{Path: "nested/books/parts.z01", Bytes: 100}, {Path: "nested/books/parts.z02", Bytes: 100}, {Path: "nested/books/parts.zip", Bytes: 100}}}
	parts, ok := zipSetParts(e)
	if !ok || !supportedZIPSet(e) || !reflect.DeepEqual(parts, []string{"parts.z01", "parts.z02", "parts.zip"}) {
		t.Fatal("portable manifest disagrees with native family keys", parts, ok)
	}
	e.Files[2].Path = "nested/other/parts.zip"
	if _, ok := zipSetParts(e); ok {
		t.Fatal("a different exact parent was accepted")
	}
}

func (f streamFixture) proof() string {
	if f.archiveProof != "" {
		return f.archiveProof
	}
	return zipProofFor(CleanupEntry{Path: f.names[0]})
}

func newSegmentFixture(t *testing.T, count int, build func([]byte) []byte) streamFixture {
	t.Helper()
	f := streamFixture{root: t.TempDir(), folder: t.TempDir()}
	if _, err := Initialize(context.Background(), f.root); err != nil {
		t.Fatal(err)
	}
	bookPath := writeEPUBParagraph(t, t.TempDir(), "Recoverable disk-segmented ZIP")
	book := mustRead(t, bookPath)
	if _, err := Import(context.Background(), ImportRequest{Library: f.root, Source: bookPath, Apply: true, Scan: true, Scanner: cleanScan{}}); err != nil {
		t.Fatal(err)
	}
	body := streamZIPBytes(t, book, false, "")
	if build != nil {
		body = build(book)
	}
	z, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	data, err := z.File[0].DataOffset()
	if err != nil {
		t.Fatal(err)
	}
	var cuts []int
	for i := 1; i < count; i++ {
		cuts = append(cuts, 4+int(data)+len(book)*i/count)
	}
	s := zipfixture.Split(t, body, cuts...)
	a := stagingtest.Archive{Kind: "-tzip", Members: map[string][]byte{}, Served: map[string][]byte{}, Headers: map[string]string{}, Fields: map[string]string{}}
	for i, c := range s.Central {
		name := z.File[i].Name
		off, err := z.File[i].DataOffset()
		if err != nil {
			t.Fatal(err)
		}
		actual := body[int(off) : int(off)+int(z.File[i].CompressedSize64)]
		// All fixtures use stored payloads. Listed CRC binds to the original
		// central record; Served also models corrupt bytes, rather than hiding
		// them behind a successful synthetic reader response.
		if z.File[i].Method != zip.Store {
			t.Fatal("segment fixture needs stored members")
		}
		a.Members[name] = bytes.Clone(actual)
		if name == "book.epub" {
			a.Members[name] = book
			a.Served[name] = actual
		}
		a.Fields[name] = fmt.Sprintf("Volume Index = %d\nOffset = %d\n", binary.LittleEndian.Uint16(s.Raw[c+34:]), binary.LittleEndian.Uint32(s.Raw[c+42:]))
	}
	for i, part := range s.Parts {
		private := fmt.Sprintf("archive.z%02d", i+1)
		name := fmt.Sprintf("books/segments.z%02d", i+1)
		if i+1 == count {
			private, name = "archive.zip", "books/segments.zip"
		}
		f.names, f.parts = append(f.names, name), append(f.parts, part)
		writeTree(t, f.folder, map[string][]byte{name: part})
		a.Volumes = append(a.Volumes, stagingtest.Volume{Name: private, Body: part})
		a.Headers[private] = fmt.Sprintf("Type = zip\nPhysical Size = %d\nTotal Physical Size = %d\nEmbedded Stub Size = 4\nMultivolume = +\nVolume Index = %d\nVolumes = %d\n", len(part), len(s.Raw), i, count)
	}
	stagingtest.Install(t, a)
	return f
}

// Both layouts use precisely the same acceptance scenarios and assertions.
// The segmented fixture exercises the terminal .zip through every crash window.
func TestSplitZIPCleanupAndPurgeRecovery(t *testing.T) {
	for _, layout := range []struct {
		name    string
		fixture zipFixtureFactory
	}{{"stream", newStreamFixture}, {"segments", newSegmentFixture}} {
		t.Run(layout.name, func(t *testing.T) {
			for _, scenario := range []struct {
				name string
				run  func(*testing.T, zipFixtureFactory)
			}{
				{"lifecycle", testZIPCleanupRestoreAndPurge},
				{"refused_sources", testZIPCleanupRefusesUnaccountedSources},
				{"cleanup_crash_windows", testZIPCleanupRecoversEachPartWithoutAReader},
				{"purge_crash_windows", testZIPPurgeResumesEveryPartFromItsBoundProof},
				{"proof_bindings", testZIPPurgeRetainsUnsupportedOrMalformedEvidence},
				{"fresh_purge", testZIPFreshPurgeRequiresEveryPartAndCurrentHolding},
				{"independent_groups", testZIPPurgeBindsIndependentGroupsAtTheirActualIndices},
				{"stale_and_cancelled", testZIPCancelledCleanupAndStalePlansPreserveSources},
				{"purge_retry", testZIPPurgeBeforeIntentRechecksAndResumesKeepChanges},
			} {
				t.Run(scenario.name, func(t *testing.T) { scenario.run(t, layout.fixture) })
			}
		})
	}
}

func TestZIPSegmentsCannotUseAStreamPurgeProof(t *testing.T) {
	f := newSegmentFixture(t, 3, nil)
	c := f.cleanup(t)
	p, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
	if err != nil {
		t.Fatal(err)
	}
	cleanupHook = func(stage string) error {
		if stage == "purged" {
			return errors.New("interruption")
		}
		return nil
	}
	t.Cleanup(func() { cleanupHook = nil })
	if _, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: p.Digest}); err == nil {
		t.Fatal("interruption absent")
	}
	cleanupHook = nil
	digest := changeStreamPurgeManifest(t, f.root, func(pm *purgeManifest) { pm.ArchiveGroups[0].Proof = zipStreamProof })
	before := snapshotTree(t, filepath.Join(c.HoldingArea, "files"))
	r, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: digest})
	if err != nil || r.Purged != 0 || !reflect.DeepEqual(before, snapshotTree(t, filepath.Join(c.HoldingArea, "files"))) {
		t.Fatal("stream evidence authorized a segment", r, err)
	}
	if purgeDispositions(r)[f.names[2]] != "archive_not_accounted" {
		t.Fatal("terminal segment was treated as an ordinary ZIP", r)
	}
}

func TestZIPSegmentsPartialRestoreKeepsReplacementAndEveryOriginalByte(t *testing.T) {
	for stop := 1; stop <= 3; stop++ {
		t.Run(fmt.Sprint(stop), func(t *testing.T) {
			f := newSegmentFixture(t, 3, nil)
			c := f.cleanup(t)
			ctx := context.Background()
			crash := errors.New("restore interrupted before completion record")
			calls := 0
			cleanupHook = func(stage string) error {
				if stage == "restored" {
					calls++
					if calls == stop {
						return crash
					}
				}
				return nil
			}
			t.Cleanup(func() { cleanupHook = nil })
			if _, err := Restore(ctx, RestoreRequest{Library: f.root, CleanupID: c.CleanupID, Apply: true}); !errors.Is(err, crash) {
				t.Fatal(err)
			}
			cleanupHook = nil
			r, err := Restore(ctx, RestoreRequest{Library: f.root, CleanupID: c.CleanupID, Apply: true})
			if err != nil || !r.Resumed || !r.Applied || r.Restore+r.Present != 3 {
				t.Fatal(r, err)
			}
			f.assertParts(t, f.folder)
			c = f.cleanup(t)
			changed := filepath.Join(f.folder, filepath.FromSlash(f.names[2]))
			if err := os.WriteFile(changed, []byte("unowned terminal replacement"), 0600); err != nil {
				t.Fatal(err)
			}
			r, err = Restore(ctx, RestoreRequest{Library: f.root, CleanupID: c.CleanupID, Apply: true})
			if err != nil || r.Conflicts != 1 || r.Restore != 2 {
				t.Fatal(r, err)
			}
			if string(mustRead(t, changed)) != "unowned terminal replacement" || !bytes.Equal(mustRead(t, filepath.Join(c.HoldingArea, "files", filepath.FromSlash(f.names[2]))), f.parts[2]) {
				t.Fatal("restore replaced or lost original terminal bytes")
			}
			if err := os.Remove(changed); err != nil {
				t.Fatal(err)
			}
			r, err = Restore(ctx, RestoreRequest{Library: f.root, CleanupID: c.CleanupID, Apply: true})
			if err != nil || r.Restore != 1 || r.Present != 2 {
				t.Fatal(r, err)
			}
			f.assertParts(t, f.folder)
		})
	}
}
