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
	"testing"

	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
)

func cleanupTreeRAR(version int, files ...nestedFile) ([]byte, stagingtest.Archive) {
	kind := "Rar"
	var b []byte
	if version == 4 {
		b = append([]byte("Rar!\x1a\x07\x00"), cleanupRAR4Block(0x73, 0, make([]byte, 6), nil)...)
	} else {
		kind = "Rar5"
		b = append([]byte("Rar!\x1a\x07\x01\x00"), cleanupRAR5Block(1, "", nil)...)
	}
	p := stagingtest.Archive{Kind: "-t" + kind, Members: map[string][]byte{}}
	for _, f := range files {
		if version == 4 {
			b = append(b, cleanupRAR4File(f.name, f.body)...)
		} else {
			b = append(b, cleanupRAR5Block(2, f.name, f.body)...)
		}
		p.Members[f.name] = f.body
	}
	if version == 4 {
		b = append(b, cleanupRAR4Block(0x7b, 0, nil, nil)...)
	} else {
		b = append(b, cleanupRAR5Block(5, "", nil)...)
	}
	p.Body, p.Header = b, fmt.Sprintf("Type = %s\nPhysical Size = %d\nMultivolume = -\nVolumes = 1\n", kind, len(b))
	return b, p
}

func newCleanupRARTreeFixture(t *testing.T, layout string, version, count int, build func([]byte) []byte) streamFixture {
	t.Helper()
	f := newCleanupTreeFixtureWithChild(t, layout, count, "inside.rar", func(book []byte) ([]byte, []stagingtest.Archive) {
		name, payload := "book.epub", book
		var profiles []stagingtest.Archive
		if build != nil {
			name, payload = "inner.zip", build(book)
			profiles = append(profiles, stagingtest.Archive{Kind: "-tzip", Body: payload})
		}
		data, p := cleanupTreeRAR(version, nestedFile{name, payload})
		return data, append(profiles, p)
	})
	f.archiveProof = "archive-tree/2"
	return f
}

func TestRARTreePreviewRequiresNewProofAndExactLeaves(t *testing.T) {
	for _, version := range []int{4, 5} {
		for _, layout := range []string{"zip", "7z", "zip_chunks", "zip_segments", "7z_chunks"} {
			t.Run(fmt.Sprintf("%d/%s", version, layout), func(t *testing.T) {
				f := newCleanupRARTreeFixture(t, layout, version, 3, nil)
				before := snapshotTree(t, f.folder)
				p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
				if err != nil || p.Move != 1 || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
					t.Fatal(p, err)
				}
				e := p.Entries[0]
				if e.ArchiveProof != "archive-tree/2" || len(e.ArchiveLeaves) != 2 || !supportedArchiveProof(e) || !reflect.DeepEqual(e.ArchiveLeaves[1].Chain, []string{"middle.7z", "inside.rar", "book.epub"}) {
					t.Fatal("RAR tree borrowed existing authority", e)
				}
				old := e
				old.ArchiveProof = "archive-tree/1"
				if supportedArchiveProof(old) {
					t.Fatal("existing proof silently broadened")
				}
				c := f.cleanup(t)
				q, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
				if err != nil || q.Purge != len(f.parts) || len(q.ArchiveGroups) != 1 || q.ArchiveGroups[0].Proof != "archive-tree/2" || q.ArchiveGroups[0].LeafDigest != archiveLeafDigest(e) {
					t.Fatal(q, err)
				}
				r, err := Restore(context.Background(), RestoreRequest{Library: f.root, CleanupID: c.CleanupID, Apply: true})
				if err != nil || r.Restore != len(f.parts) {
					t.Fatal(r, err)
				}
				f.assertParts(t, f.folder)
			})
		}
	}
}

func TestRARTreeRecoveryAcrossEveryOuterSetLayout(t *testing.T) {
	versions, layouts := rarTreeRecoveryVersions, rarTreeRecoveryLayouts
	selected := os.Getenv(rarRecoveryCaseEnv)
	// A filtered invocation keeps Go's exact subtest selection. Full suites run
	// independent groups in isolated processes, since fixtures change globals.
	if selected == "" && flag.Lookup("test.run").Value.String() == "" {
		runRARRecoveryProcesses(t, versions, layouts)
		return
	}
	matched := false
	for _, version := range versions {
		for _, layout := range layouts {
			if selected != "" && selected != fmt.Sprintf("%d/%s", version, layout) {
				continue
			}
			matched = true
			t.Run(fmt.Sprintf("%d/%s", version, layout), func(t *testing.T) {
				factory := func(t *testing.T, count int, build func([]byte) []byte) streamFixture {
					return newCleanupRARTreeFixture(t, layout, version, count, build)
				}
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
				} {
					t.Run(s.name, func(t *testing.T) { s.run(t, factory) })
				}
				t.Run("refusals", func(t *testing.T) {
					if layout == "7z_chunks" {
						// ZIP aliases use a second accepted ordinal width; 7z
						// requires exactly three digits, making it unrelated data.
						testArchiveCleanupRefusals(t, factory, []string{"notes", "member_comment", "directory_gap", "crc", "missing", "changed", "missing_tool"})
					} else {
						testZIPCleanupRefusesUnaccountedSources(t, factory)
					}
				})
			})
		}
	}
	if !matched {
		t.Fatalf("unknown RAR recovery case %q", selected)
	}
}

func TestRARTreeSingleArchiveCleanupRestoreAndPurge(t *testing.T) {
	for _, layout := range []string{"zip", "7z"} {
		t.Run(layout, func(t *testing.T) {
			factory := func(t *testing.T) streamFixture { return newCleanupRARTreeFixture(t, layout, 4, 1, nil) }
			testSingleArchiveCleanupAndRestoreInterruptions(t, factory)
			f := factory(t)
			c := f.cleanup(t)
			p, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
			if err != nil || p.Purge != 1 {
				t.Fatal(p, err)
			}
			done, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: p.Digest})
			if err != nil || done.Purged != 1 {
				t.Fatal(done, err)
			}
			if files := snapshotTree(t, filepath.Join(c.HoldingArea, "files")); len(files) != 0 {
				t.Fatal("purged bytes remain", files)
			}
		})
	}
}

func TestRARTreeEmptyChildRequiresMeasuredNewProof(t *testing.T) {
	for _, version := range []int{4, 5} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			f := newRAR4CleanupFixture(t, nil)
			var book []byte
			for _, b := range snapshotTree(t, filepath.Join(f.root, "assets")) {
				book = b
			}
			empty, profile := cleanupTreeRAR(version)
			body := nestedZIP(t, nestedFile{"direct.epub", book}, nestedFile{"empty.rar", empty})
			// Replace only this fixture's own source tree.
			if err := os.Remove(filepath.Join(f.folder, filepath.FromSlash(f.names[0]))); err != nil {
				t.Fatal(err)
			}
			writeTree(t, f.folder, map[string][]byte{"outer.zip": body})
			stagingtest.InstallMany(t, profile)
			before := snapshotTree(t, f.folder)
			p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
			if err != nil || p.Move != 1 || p.Entries[0].ArchiveProof != "archive-tree/2" || len(p.Entries[0].ArchiveLeaves) != 1 || !reflect.DeepEqual(p.Entries[0].ArchiveLeaves[0].Chain, []string{"direct.epub"}) || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
				t.Fatal("empty RAR evidence was inferred from leaves", p, err)
			}
			if !bytes.Equal(mustRead(t, filepath.Join(f.folder, "outer.zip")), body) {
				t.Fatal("preview changed source")
			}
		})
	}
}

func newRARootTreeFixture(t *testing.T, version int, build func([]byte) []byte) streamFixture {
	t.Helper()
	factory := newRAR4CleanupFixture
	if version == 5 {
		factory = newRAR5CleanupFixture
	}
	f := factory(t, nil)
	assets := snapshotTree(t, filepath.Join(f.root, "assets"))
	if len(assets) != 1 {
		t.Fatal("expected one held book")
	}
	var book []byte
	for _, data := range assets {
		book = data
	}
	inner := nestedZIP(t, nestedFile{"book.epub", book})
	if build != nil {
		inner = build(book)
	}
	outer, profile := cleanupTreeRAR(version, nestedFile{"inside.zip", inner})
	f.parts[0], f.archiveProof = outer, "archive-tree/2"
	writeTree(t, f.folder, map[string][]byte{f.names[0]: outer})
	stagingtest.InstallMany(t, profile, stagingtest.Archive{Kind: "-tzip", Body: inner})
	return f
}

func TestRARTreeRootRARRecoveryUsesTreeAuthority(t *testing.T) {
	for _, version := range []int{4, 5} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			factory := func(t *testing.T, build func([]byte) []byte) streamFixture {
				return newRARootTreeFixture(t, version, build)
			}
			t.Run("cleanup_restore_crashes", func(t *testing.T) {
				testSingleArchiveCleanupAndRestoreInterruptions(t, func(t *testing.T) streamFixture { return factory(t, nil) })
			})
			t.Run("fresh_purge", func(t *testing.T) { testStandaloneRARFreshPurgeRequiresCurrentEvidence(t, factory) })
			t.Run("durable_purge", func(t *testing.T) { testStandaloneRARPurgeRecoveryChecksFreshHoldingsAndRemainingBytes(t, factory) })
			t.Run("lifecycle", func(t *testing.T) {
				f := factory(t, nil)
				c := f.cleanup(t)
				p, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
				if err != nil || p.Purge != 1 || len(p.ArchiveGroups) != 1 || p.ArchiveGroups[0].Proof != "archive-tree/2" {
					t.Fatal(p, err)
				}
				r, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: p.Digest})
				if err != nil || r.Purged != 1 {
					t.Fatal(r, err)
				}
			})
		})
	}
}

func TestRARTreeUnknownAndMalformedPurgeBindingsRetainBytes(t *testing.T) {
	testRARTreePurgeBindings(t, func(t *testing.T) streamFixture { return newCleanupRARTreeFixture(t, "zip_chunks", 5, 3, nil) })
}

func testRARTreePurgeBindings(t *testing.T, fixture func(*testing.T) streamFixture, extra ...string) {
	for _, kind := range append([]string{"unknown", "unknown_both", "missing", "old_tree", "standalone", "leaf_digest", "index", "unsafe_chain"}, extra...) {
		t.Run(kind, func(t *testing.T) {
			f := fixture(t)
			c := f.cleanup(t)
			ctx := context.Background()
			p, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "unknown":
				p.ArchiveGroups[0].Proof = "archive-tree/future"
			case "unknown_both":
				p.ArchiveGroups[0].Proof = "archive-tree/future"
				rewriteTreeCleanup(t, f.root, c.CleanupID, func(m *cleanupManifest) { m.Entries[0].ArchiveProof = "archive-tree/future" })
			case "missing":
				p.ArchiveGroups = nil
			case "old_tree":
				p.ArchiveGroups[0].Proof = "archive-tree/1"
			case "standalone":
				p.ArchiveGroups[0].Proof = "rar5-frames/1"
			case "newer_tree":
				p.ArchiveGroups[0].Proof = archiveRARTreeProof
			case "rar4_standalone":
				p.ArchiveGroups[0].Proof = rar4FramesProof
			case "zip_stream":
				p.ArchiveGroups[0].Proof = zipStreamProof
			case "zip_segments":
				p.ArchiveGroups[0].Proof = zipSegmentsProof
			case "rar4_set":
				p.ArchiveGroups[0].Proof = rar4SetProof
			case "rar5_set":
				p.ArchiveGroups[0].Proof = rar5SetProof
			case "leaf_digest":
				p.ArchiveGroups[0].LeafDigest = ""
			case "index":
				p.ArchiveGroups[0].Indices = []int{0, 0, 2}
			case "unsafe_chain":
				rewriteTreeCleanup(t, f.root, c.CleanupID, func(m *cleanupManifest) {
					m.Entries[0].ArchiveLeaves[len(m.Entries[0].ArchiveLeaves)-1].Chain = []string{"../escape.rar", "book.epub"}
				})
			}
			digest := startRAR5TestPurge(t, c, p)
			before := snapshotTree(t, filepath.Join(c.HoldingArea, "files"))
			r, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: digest})
			if kind == "unknown" || kind == "unknown_both" || kind == "missing" {
				if err != nil || r.Purged != 0 {
					t.Fatal(r, err)
				}
			} else if !errors.Is(err, ErrStateReview) {
				t.Fatal("known malformed binding did not stop", r, err)
			}
			if !reflect.DeepEqual(before, snapshotTree(t, filepath.Join(c.HoldingArea, "files"))) {
				t.Fatal("unsupported binding removed bytes")
			}
			if kind == "unknown_both" {
				r, err := Restore(ctx, RestoreRequest{Library: f.root, CleanupID: c.CleanupID, Apply: true})
				if err != nil || r.Restore != 3 {
					t.Fatal("unknown evidence prevented raw restore", r, err)
				}
				f.assertParts(t, f.folder)
			}
		})
	}
}

func TestRARTreeIndependentGroupsValidateKnownShapesBeforeAnyRemoval(t *testing.T) {
	testRARTreeIndependentShapes(t, func(t *testing.T) streamFixture { return newCleanupRARTreeFixture(t, "zip", 4, 1, nil) })
}

func testRARTreeIndependentShapes(t *testing.T, fixture func(*testing.T) streamFixture, extra ...string) {
	for _, kind := range append([]string{"unknown_first", "old_tree_second", "zip_proof_second", "standalone_second", "missing_leaves_second"}, extra...) {
		t.Run(kind, func(t *testing.T) {
			f := fixture(t)
			var others []string
			for i, name := range f.names {
				other := "other/" + filepath.Base(name)
				others = append(others, other)
				writeTree(t, f.folder, map[string][]byte{other: f.parts[i]})
			}
			ctx := context.Background()
			p, err := Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder})
			if err != nil || p.Move != 2 {
				t.Fatal(p, err)
			}
			c, err := Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest})
			if err != nil {
				t.Fatal(err)
			}
			q, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
			if err != nil || q.Purge != 2*len(f.parts) || len(q.ArchiveGroups) != 2 {
				t.Fatal(q, err)
			}
			if kind == "unknown_first" {
				q.ArchiveGroups[0].Proof = "archive-tree/future"
			} else {
				rewriteTreeCleanup(t, f.root, c.CleanupID, func(m *cleanupManifest) {
					e := &m.Entries[1]
					switch kind {
					case "old_tree_second":
						e.ArchiveProof = "archive-tree/1"
					case "zip_proof_second":
						e.ArchiveProof = zipStreamProof
					case "standalone_second":
						e.ArchiveProof = "rar4-frames/1"
					case "newer_tree_second":
						e.ArchiveProof = archiveRARTreeProof
					case "rar5_standalone_second":
						e.ArchiveProof = rar5FramesProof
					case "rar4_set_second":
						e.ArchiveProof = rar4SetProof
					case "rar5_set_second":
						e.ArchiveProof = rar5SetProof
					case "missing_leaves_second":
						e.ArchiveLeaves = nil
					}
				})
			}
			digest := startRAR5TestPurge(t, c, q)
			before := snapshotTree(t, filepath.Join(c.HoldingArea, "files"))
			r, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: digest})
			if kind == "unknown_first" {
				if err != nil || r.Purged != len(f.parts) {
					t.Fatal(r, err)
				}
				f.assertParts(t, filepath.Join(c.HoldingArea, "files"))
				for _, other := range others {
					if _, err := os.Stat(filepath.Join(c.HoldingArea, "files", filepath.FromSlash(other))); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("independent supported group retained", err)
					}
				}
			} else if !errors.Is(err, ErrStateReview) || !reflect.DeepEqual(before, snapshotTree(t, filepath.Join(c.HoldingArea, "files"))) {
				t.Fatal("later malformed known proof permitted earlier removal", r, err)
			}
		})
	}
}

func TestRARTreeCleanupResumeRejectsUnsupportedProofsBeforeWrites(t *testing.T) {
	for _, version := range []int{4, 5} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			testTreeCleanupResumeBindings(t, func(t *testing.T) streamFixture { return newCleanupRARTreeFixture(t, "zip_chunks", version, 3, nil) }, "old_tree", "standalone")
		})
	}
}
