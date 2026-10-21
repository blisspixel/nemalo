package library

import (
	"bytes"
	"context"
	"errors"
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

func newRAR5CleanupFixture(t *testing.T, build func([]byte) []byte) streamFixture {
	t.Helper()
	ctx := context.Background()
	f := streamFixture{root: t.TempDir(), folder: t.TempDir(), archiveProof: "rar5-frames/1"}
	if _, err := Initialize(ctx, f.root); err != nil {
		t.Fatal(err)
	}
	source := writeEPUBParagraph(t, t.TempDir(), "Standalone RAR5 cleanup")
	if _, err := Import(ctx, ImportRequest{Library: f.root, Source: source, Apply: true, Scan: true, Scanner: cleanScan{}}); err != nil {
		t.Fatal(err)
	}
	book := mustRead(t, source)
	data := append([]byte("Rar!\x1a\x07\x01\x00"), cleanupRAR5Block(1, "", nil)...)
	data = append(data, cleanupRAR5Block(2, "book.epub", book)...)
	data = append(data, cleanupRAR5Block(5, "", nil)...)
	if build != nil {
		data = build(book)
	}
	f.names = []string{"books/one.rar"}
	f.parts = [][]byte{data}
	writeTree(t, f.folder, map[string][]byte{f.names[0]: data})
	stagingtest.Install(t, stagingtest.Archive{Kind: "-tRar5", Body: data, Members: map[string][]byte{"book.epub": book}})
	return f
}

func TestStandaloneRAR5CleanupBindsExactLeafAndPurge(t *testing.T) {
	testStandaloneRARCleanupBindsExactLeafAndPurge(t, newRAR5CleanupFixture)
}

func testStandaloneRARCleanupBindsExactLeafAndPurge(t *testing.T, fixture func(*testing.T, func([]byte) []byte) streamFixture) {
	f := fixture(t, nil)
	ctx := context.Background()
	before := snapshotTree(t, f.folder)
	p, err := Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder})
	if err != nil || p.Move != 1 || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
		t.Fatal(p, err)
	}
	e := p.Entries[0]
	if e.ArchiveProof != f.archiveProof || len(e.ArchiveLeaves) != 1 || !validArchiveLeaves(e) || !reflect.DeepEqual(e.ArchiveLeaves[0].Chain, []string{"book.epub"}) {
		t.Fatal(e)
	}
	changed := e
	changed.ArchiveLeaves = append([]CleanupArchiveLeaf{}, e.ArchiveLeaves...)
	changed.ArchiveLeaves[0].Chain = []string{"alias.epub"}
	if planDigest(f.folder, []CleanupEntry{changed}) == p.Digest {
		t.Fatal("exact selection was not bound")
	}
	c := f.cleanup(t)
	q, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
	if err != nil || q.Purge != 1 || len(q.ArchiveGroups) != 1 || q.ArchiveGroups[0].Proof != e.ArchiveProof || q.ArchiveGroups[0].LeafDigest != archiveLeafDigest(e) || !reflect.DeepEqual(q.ArchiveGroups[0].Indices, []int{0}) {
		t.Fatal(q, err)
	}
	r, err := Restore(ctx, RestoreRequest{Library: f.root, CleanupID: c.CleanupID, Apply: true})
	if err != nil || r.Restore != 1 {
		t.Fatal(r, err)
	}
	f.assertParts(t, f.folder)
	c = f.cleanup(t)
	q, err = Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
	if err != nil {
		t.Fatal(err)
	}
	done, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: q.Digest})
	if err != nil || done.Purged != 1 {
		t.Fatal(done, err)
	}
}

func TestStandaloneRAR5CleanupAndRestoreInterruptions(t *testing.T) {
	testSingleArchiveCleanupAndRestoreInterruptions(t, func(t *testing.T) streamFixture { return newRAR5CleanupFixture(t, nil) })
}

func TestStandaloneRAR5UnlistedMaterialPreservesSource(t *testing.T) {
	for _, kind := range []string{"service", "tail"} {
		t.Run(kind, func(t *testing.T) {
			f := newRAR5CleanupFixture(t, func(book []byte) []byte {
				if kind == "service" {
					return cleanupRAR5WithService(book)
				}
				data := append([]byte("Rar!\x1a\x07\x01\x00"), cleanupRAR5Block(1, "", nil)...)
				data = append(data, cleanupRAR5Block(2, "book.epub", book)...)
				data = append(data, cleanupRAR5Block(5, "", nil)...)
				return append(data, []byte("unheld tail")...)
			})
			before := snapshotTree(t, f.folder)
			p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
			if err != nil || p.Move != 0 || p.Keep != 1 || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
				t.Fatal(p, err)
			}
			for name, data := range before {
				if !bytes.Equal(mustRead(t, name), data) {
					t.Fatal(fmt.Sprint(filepath.Base(name)), "source changed")
				}
			}
		})
	}
}

func startRAR5TestPurge(t *testing.T, c CleanupResult, p PurgeResult) string {
	t.Helper()
	control, _, err := openControl(c.Library, true)
	if err != nil {
		t.Fatal(err)
	}
	defer control.close()
	records, err := readCleanup(control, c.LibraryID)
	if err != nil {
		t.Fatal(err)
	}
	store, err := safeio.OpenRoot(c.Library)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	digest := purgePlanDigest(c.CleanupID, p.Files, p.ArchiveGroups)
	pm := purgeManifest{SchemaVersion: 2, LibraryID: c.LibraryID, CleanupID: c.CleanupID, Digest: digest, Files: p.Files, ArchiveGroups: p.ArchiveGroups}
	if len(pm.ArchiveGroups) == 0 {
		pm.SchemaVersion = 1
	}
	rec := cleanupRecord{SchemaVersion: 1, Sequence: len(records) + 1, LibraryID: c.LibraryID, CleanupID: c.CleanupID, Event: "purge_started", Digest: digest, Count: len(p.Files), At: now()}
	rec.Manifest, err = writePurgeManifest(store, pm, rec.Sequence)
	if err != nil {
		t.Fatal(err)
	}
	if err := appendCanonical(context.Background(), control, cleanupName, maxCleanupJournal, rec); err != nil {
		t.Fatal(err)
	}
	return digest
}

func TestStandaloneRAR5PendingPurgeValidatesBindingsBeforeRemoval(t *testing.T) {
	testStandaloneRARPendingPurgeValidatesBindingsBeforeRemoval(t, newRAR5CleanupFixture)
}

func testStandaloneRARPendingPurgeValidatesBindingsBeforeRemoval(t *testing.T, fixture func(*testing.T, func([]byte) []byte) streamFixture) {
	for _, kind := range []string{"unknown", "unknown_both", "missing", "wrong_proof", "other_rar", "leaf_digest", "index", "nested_leaf", "changed_chain"} {
		t.Run(kind, func(t *testing.T) {
			f := fixture(t, nil)
			c := f.cleanup(t)
			ctx := context.Background()
			p, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "unknown":
				p.ArchiveGroups[0].Proof = f.archiveProof + "-future"
			case "unknown_both":
				p.ArchiveGroups[0].Proof = f.archiveProof + "-future"
				rewriteTreeCleanup(t, f.root, c.CleanupID, func(m *cleanupManifest) { m.Entries[0].ArchiveProof = f.archiveProof + "-future" })
			case "missing":
				p.ArchiveGroups = nil
			case "wrong_proof":
				p.ArchiveGroups[0].Proof = archiveTreeProof
			case "other_rar":
				p.ArchiveGroups[0].Proof = "rar4-frames/1"
				if f.archiveProof == "rar4-frames/1" {
					p.ArchiveGroups[0].Proof = "rar5-frames/1"
				}
			case "leaf_digest":
				p.ArchiveGroups[0].LeafDigest = ""
			case "index":
				p.ArchiveGroups[0].Indices = []int{1}
			case "nested_leaf":
				rewriteTreeCleanup(t, f.root, c.CleanupID, func(m *cleanupManifest) { m.Entries[0].ArchiveLeaves[0].Chain = []string{"inside.zip", "book.epub"} })
			case "changed_chain":
				rewriteTreeCleanup(t, f.root, c.CleanupID, func(m *cleanupManifest) { m.Entries[0].ArchiveLeaves[0].Chain = []string{"alias.epub"} })
			}
			digest := startRAR5TestPurge(t, c, p)
			before := snapshotTree(t, filepath.Join(c.HoldingArea, "files"))
			r, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: digest})
			if kind == "unknown" || kind == "unknown_both" || kind == "missing" {
				if err != nil || r.Purged != 0 {
					t.Fatal(r, err)
				}
			} else if !errors.Is(err, ErrStateReview) {
				t.Fatal("malformed known binding accepted", r, err)
			}
			if !reflect.DeepEqual(before, snapshotTree(t, filepath.Join(c.HoldingArea, "files"))) {
				t.Fatal("bad proof removed encoded bytes")
			}
			if kind == "unknown_both" {
				r, err := Restore(ctx, RestoreRequest{Library: f.root, CleanupID: c.CleanupID, Apply: true})
				if err != nil || r.Restore != 1 {
					t.Fatal("unknown proof prevented recorded-byte restore", r, err)
				}
				f.assertParts(t, f.folder)
			}
		})
	}
}

func TestStandaloneRAR5PurgeRecoveryChecksFreshHoldingsAndRemainingBytes(t *testing.T) {
	testStandaloneRARPurgeRecoveryChecksFreshHoldingsAndRemainingBytes(t, newRAR5CleanupFixture)
}

func testStandaloneRARPurgeRecoveryChecksFreshHoldingsAndRemainingBytes(t *testing.T, fixture func(*testing.T, func([]byte) []byte) streamFixture) {
	for _, kind := range []string{"valid", "source_changed", "holding_changed", "replacement_after_removal"} {
		t.Run(kind, func(t *testing.T) {
			f := fixture(t, nil)
			c := f.cleanup(t)
			ctx := context.Background()
			p, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
			if err != nil {
				t.Fatal(err)
			}
			held := filepath.Join(c.HoldingArea, "files", filepath.FromSlash(f.names[0]))
			digest := p.Digest
			replacement := []byte("replacement encoded source")
			if kind == "replacement_after_removal" {
				crash := errors.New("after source removal before entry journal")
				cleanupHook = func(stage string) error {
					if stage == "purged" {
						return crash
					}
					return nil
				}
				t.Cleanup(func() { cleanupHook = nil })
				_, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: p.Digest})
				cleanupHook = nil
				if !errors.Is(err, crash) {
					t.Fatal(err)
				}
				if err := os.WriteFile(held, replacement, 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				digest = startRAR5TestPurge(t, c, p)
				switch kind {
				case "source_changed":
					if err := os.WriteFile(held, replacement, 0600); err != nil {
						t.Fatal(err)
					}
				case "holding_changed":
					for name := range snapshotTree(t, filepath.Join(f.root, "assets")) {
						if err := os.Chmod(name, 0600); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(name, []byte("changed held book"), 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			saved := staging.SevenZip
			staging.SevenZip = func() (string, error) { t.Fatal("durable purge re-parsed original archive"); return "", nil }
			t.Cleanup(func() { staging.SevenZip = saved })
			r, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: digest})
			if err != nil || !r.Resumed || r.Purged != map[bool]int{true: 1, false: 0}[kind == "valid"] {
				t.Fatal(r, err)
			}
			if kind == "source_changed" || kind == "replacement_after_removal" {
				if !bytes.Equal(mustRead(t, held), replacement) {
					t.Fatal("replacement removed")
				}
			}
			if kind == "holding_changed" {
				f.assertParts(t, filepath.Join(c.HoldingArea, "files"))
			}
		})
	}
}

func TestStandaloneRAR5FreshPurgeRequiresCurrentEvidence(t *testing.T) {
	testStandaloneRARFreshPurgeRequiresCurrentEvidence(t, newRAR5CleanupFixture)
}

func testStandaloneRARFreshPurgeRequiresCurrentEvidence(t *testing.T, fixture func(*testing.T, func([]byte) []byte) streamFixture) {
	for _, kind := range []string{"missing_reader", "changed_source", "changed_holding", "other_rar_proof"} {
		t.Run(kind, func(t *testing.T) {
			f := fixture(t, nil)
			c := f.cleanup(t)
			switch kind {
			case "other_rar_proof":
				rewriteTreeCleanup(t, f.root, c.CleanupID, func(m *cleanupManifest) {
					m.Entries[0].ArchiveProof = "rar4-frames/1"
					if f.archiveProof == "rar4-frames/1" {
						m.Entries[0].ArchiveProof = "rar5-frames/1"
					}
				})
			case "missing_reader":
				saved := staging.SevenZip
				staging.SevenZip = func() (string, error) { return "", staging.ErrNoArchiveTool }
				t.Cleanup(func() { staging.SevenZip = saved })
			case "changed_source":
				if err := os.WriteFile(filepath.Join(c.HoldingArea, "files", filepath.FromSlash(f.names[0])), []byte("changed source"), 0600); err != nil {
					t.Fatal(err)
				}
			case "changed_holding":
				for name := range snapshotTree(t, filepath.Join(f.root, "assets")) {
					if err := os.Chmod(name, 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(name, []byte("changed book"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			before := snapshotTree(t, filepath.Join(c.HoldingArea, "files"))
			p, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
			if err != nil || p.Purge != 0 || len(p.ArchiveGroups) != 0 || !reflect.DeepEqual(before, snapshotTree(t, filepath.Join(c.HoldingArea, "files"))) {
				t.Fatal(p, err)
			}
		})
	}
}

func TestStandaloneRAR5CleanupResumeRejectsInvalidProofBeforeWrites(t *testing.T) {
	testStandaloneRARCleanupResumeRejectsInvalidProofBeforeWrites(t, newRAR5CleanupFixture)
}

func testStandaloneRARCleanupResumeRejectsInvalidProofBeforeWrites(t *testing.T, fixture func(*testing.T, func([]byte) []byte) streamFixture) {
	for _, kind := range []string{"unknown", "tree_proof", "missing_leaves", "nested_leaf", "wrong_source_type"} {
		t.Run(kind, func(t *testing.T) {
			f := fixture(t, nil)
			ctx := context.Background()
			p, err := Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder})
			if err != nil {
				t.Fatal(err)
			}
			crash := errors.New("RAR5 private copy interruption")
			cleanupHook = func(stage string) error {
				if stage == "partial" {
					return crash
				}
				return nil
			}
			t.Cleanup(func() { cleanupHook = nil })
			c, err := Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest})
			cleanupHook = nil
			if !errors.Is(err, crash) {
				t.Fatal(err)
			}
			digest := rewriteTreeCleanup(t, f.root, c.CleanupID, func(m *cleanupManifest) {
				e := &m.Entries[0]
				switch kind {
				case "unknown":
					e.ArchiveProof = f.archiveProof + "-future"
				case "tree_proof":
					e.ArchiveProof = archiveTreeProof
				case "missing_leaves":
					e.ArchiveLeaves = nil
				case "nested_leaf":
					e.ArchiveLeaves[0].Chain = []string{"inside.zip", "book.epub"}
				case "wrong_source_type":
					e.Path = "books/one.zip"
					e.Files[0].Path = e.Path
				}
			})
			before := snapshotTree(t, f.folder)
			held := snapshotTree(t, filepath.Join(c.HoldingArea, "files"))
			_, err = Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder, Apply: digest})
			if !errors.Is(err, ErrStateReview) || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) || !reflect.DeepEqual(held, snapshotTree(t, filepath.Join(c.HoldingArea, "files"))) {
				t.Fatal("invalid source proof changed bytes", err)
			}
		})
	}
}

func TestStandaloneRAR5IndependentGroupsValidateBeforeAnyRemoval(t *testing.T) {
	testStandaloneRARIndependentGroupsValidateBeforeAnyRemoval(t, newRAR5CleanupFixture)
}

func testStandaloneRARIndependentGroupsValidateBeforeAnyRemoval(t *testing.T, fixture func(*testing.T, func([]byte) []byte) streamFixture) {
	for _, kind := range []string{"unknown_first", "malformed_second"} {
		t.Run(kind, func(t *testing.T) {
			f := fixture(t, nil)
			ctx := context.Background()
			writeTree(t, f.folder, map[string][]byte{"other/one.rar": f.parts[0]})
			p, err := Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder})
			if err != nil || p.Move != 2 {
				t.Fatal(p, err)
			}
			c, err := Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest})
			if err != nil {
				t.Fatal(err)
			}
			q, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
			if err != nil || q.Purge != 2 || len(q.ArchiveGroups) != 2 {
				t.Fatal(q, err)
			}
			if kind == "unknown_first" {
				q.ArchiveGroups[0].Proof = f.archiveProof + "-future"
			} else {
				q.ArchiveGroups[1].LeafDigest = ""
			}
			digest := startRAR5TestPurge(t, c, q)
			before := snapshotTree(t, filepath.Join(c.HoldingArea, "files"))
			r, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: digest})
			if kind == "malformed_second" {
				if !errors.Is(err, ErrStateReview) || !reflect.DeepEqual(before, snapshotTree(t, filepath.Join(c.HoldingArea, "files"))) {
					t.Fatal("later malformed group allowed earlier removal", r, err)
				}
			} else {
				if err != nil || r.Purged != 1 {
					t.Fatal(r, err)
				}
				f.assertParts(t, filepath.Join(c.HoldingArea, "files"))
				if _, err := os.Stat(filepath.Join(c.HoldingArea, "files", "other", "one.rar")); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("independent valid group was not removed", err)
				}
			}
		})
	}
}

func TestStandaloneRAR5ScopeRetainsVolumesAndNestedRoots(t *testing.T) {
	for _, kind := range []string{"modern_set", "old_set", "nested_root", "rar4"} {
		t.Run(kind, func(t *testing.T) {
			var book []byte
			f := newRAR5CleanupFixture(t, func(b []byte) []byte {
				book = bytes.Clone(b)
				data := append([]byte("Rar!\x1a\x07\x01\x00"), cleanupRAR5Block(1, "", nil)...)
				data = append(data, cleanupRAR5Block(2, "book.epub", b)...)
				return append(data, cleanupRAR5Block(5, "", nil)...)
			})
			switch kind {
			case "modern_set", "old_set":
				first, second := "books/set.part1.rar", "books/set.part2.rar"
				if kind == "old_set" {
					first, second = "books/set.rar", "books/set.r00"
				}
				if err := os.Rename(filepath.Join(f.folder, filepath.FromSlash(f.names[0])), filepath.Join(f.folder, filepath.FromSlash(first))); err != nil {
					t.Fatal(err)
				}
				writeTree(t, f.folder, map[string][]byte{second: []byte("synthetic continuation")})
				saved := staging.SevenZip
				staging.SevenZip = func() (string, error) { t.Fatal("unsupported RAR set reached reader"); return "", nil }
				t.Cleanup(func() { staging.SevenZip = saved })
			case "nested_root":
				inner := nestedZIP(t, nestedFile{"book.epub", book})
				outer := append([]byte("Rar!\x1a\x07\x01\x00"), cleanupRAR5Block(1, "", nil)...)
				outer = append(outer, cleanupRAR5Block(2, "inside.zip", inner)...)
				outer = append(outer, cleanupRAR5Block(5, "", nil)...)
				writeTree(t, f.folder, map[string][]byte{f.names[0]: outer})
				stagingtest.InstallMany(t, stagingtest.Archive{Kind: "-tRar5", Body: outer, Members: map[string][]byte{"inside.zip": inner}}, stagingtest.Archive{Kind: "-tzip", Body: inner})
			case "rar4":
				writeTree(t, f.folder, map[string][]byte{f.names[0]: append([]byte("Rar!\x1a\x07\x00"), make([]byte, 64)...)})
			}
			before := snapshotTree(t, f.folder)
			p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
			if err != nil || p.Move != 0 || p.Keep != len(before) || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
				t.Fatal(p, err)
			}
			if kind == "nested_root" && !strings.Contains(p.Entries[0].Reason, "nested contents of a RAR root") {
				t.Fatal("nested root did not reach subset boundary", p)
			}
		})
	}
}

func TestStandaloneRAR5RequiresEveryLeafHeld(t *testing.T) {
	for _, held := range []bool{false, true} {
		t.Run(fmt.Sprint(held), func(t *testing.T) {
			f := newRAR5CleanupFixture(t, nil)
			assets := snapshotTree(t, filepath.Join(f.root, "assets"))
			if len(assets) != 1 {
				t.Fatal("expected one stored book", len(assets))
			}
			var book []byte
			for _, data := range assets {
				book = data
			}
			second := book
			if !held {
				second = mustRead(t, writeEPUBParagraph(t, t.TempDir(), "Unheld second RAR book"))
			}
			data := append([]byte("Rar!\x1a\x07\x01\x00"), cleanupRAR5Block(1, "", nil)...)
			data = append(data, cleanupRAR5Block(2, "book.epub", book)...)
			data = append(data, cleanupRAR5Block(2, "second.epub", second)...)
			data = append(data, cleanupRAR5Block(5, "", nil)...)
			writeTree(t, f.folder, map[string][]byte{f.names[0]: data})
			f.parts[0] = data
			stagingtest.Install(t, stagingtest.Archive{Kind: "-tRar5", Body: data, Members: map[string][]byte{"book.epub": book, "second.epub": second}})
			before := snapshotTree(t, f.folder)
			p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
			wantMove := 0
			if held {
				wantMove = 1
			}
			if err != nil || p.Move != wantMove || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
				t.Fatal(p, err)
			}
			if held {
				if len(p.Entries[0].ArchiveLeaves) != 2 || !supportedArchiveProof(p.Entries[0]) {
					t.Fatal(p)
				}
				c := f.cleanup(t)
				q, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
				if err != nil || q.Purge != 1 || len(q.ArchiveGroups) != 1 {
					t.Fatal(q, err)
				}
				done, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: q.Digest})
				if err != nil || done.Purged != 1 {
					t.Fatal(done, err)
				}
			} else if !strings.Contains(p.Entries[0].Reason, "not held") {
				t.Fatal("unheld leaf was not the refusal", p)
			}
		})
	}
}
