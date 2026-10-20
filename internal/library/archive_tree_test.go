package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/staging"
	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
	"github.com/blisspixel/nemalo/internal/staging/zipfixture"
)

func cleanupTreeSeven(label string, files ...nestedFile) (body []byte, profile stagingtest.Archive) {
	packed := 0
	profile = stagingtest.Archive{Kind: "-t7z", Members: map[string][]byte{}}
	for _, f := range files {
		profile.Members[f.name] = f.body
		packed += len(f.body)
	}
	body = append([]byte("7z\xbc\xaf\x27\x1c"), make([]byte, packed+128)...)
	copy(body[8:], label)
	profile.Body = body
	profile.Header = fmt.Sprintf("Type = 7z\nPhysical Size = %d\nHeaders Size = %d\n", len(body), len(body)-packed)
	return body, profile
}

func newCleanupTreeFixture(t *testing.T, layout string, count int, build func([]byte) []byte) streamFixture {
	t.Helper()
	f := streamFixture{root: t.TempDir(), folder: t.TempDir(), archiveProof: archiveTreeProof}
	if _, err := Initialize(context.Background(), f.root); err != nil {
		t.Fatal(err)
	}
	bookPath := writeEPUBParagraph(t, t.TempDir(), "Nested cleanup proof")
	book := mustRead(t, bookPath)
	if _, err := Import(context.Background(), ImportRequest{Library: f.root, Source: bookPath, Apply: true, Scan: true, Scanner: cleanScan{}}); err != nil {
		t.Fatal(err)
	}
	inner := nestedZIP(t, nestedFile{"book.epub", book})
	if build != nil {
		inner = build(book)
	}
	middle, middleProfile := cleanupTreeSeven("middle", nestedFile{"inside.zip", inner})
	outerFiles := []nestedFile{{"direct.epub", book}, {"middle.7z", middle}}
	outer := nestedZIP(t, outerFiles...)
	profiles := []stagingtest.Archive{{Body: inner, Kind: "-tzip"}, middleProfile}
	rootProfile := stagingtest.Archive{Body: outer, Kind: "-tzip", Header: fmt.Sprintf("Type = zip\nPhysical Size = %d\n", len(outer))}
	if layout == "7z" || layout == "7z_chunks" {
		outer, rootProfile = cleanupTreeSeven("outer", outerFiles...)
	}
	var parts [][]byte
	switch layout {
	case "zip", "7z":
		parts = [][]byte{outer}
	case "zip_chunks", "7z_chunks":
		for i := 0; i < count; i++ {
			parts = append(parts, bytes.Clone(outer[i*len(outer)/count:(i+1)*len(outer)/count]))
		}
		rootProfile.Volumes = []stagingtest.Volume{{Name: "archive" + map[bool]string{true: ".7z", false: ".zip"}[layout == "7z_chunks"], Body: outer}}
	case "zip_segments":
		s := zipfixture.Split(t, outer, 100, 200)
		parts = s.Parts
		rootProfile.Headers = map[string]string{}
		rootProfile.Fields = map[string]string{}
		rootProfile.Members = map[string][]byte{}
		for _, f := range outerFiles {
			rootProfile.Members[f.name] = f.body
		}
		for _, c := range s.Central {
			name := string(s.Raw[c+46 : c+46+int(binary.LittleEndian.Uint16(s.Raw[c+28:]))])
			rootProfile.Fields[name] = fmt.Sprintf("Volume Index = %d\nOffset = %d\n", binary.LittleEndian.Uint16(s.Raw[c+34:]), binary.LittleEndian.Uint32(s.Raw[c+42:]))
		}
		for i, part := range parts {
			name := fmt.Sprintf("archive.z%02d", i+1)
			if i+1 == len(parts) {
				name = "archive.zip"
			}
			rootProfile.Volumes = append(rootProfile.Volumes, stagingtest.Volume{Name: name, Body: part})
			rootProfile.Headers[name] = fmt.Sprintf("Type = zip\nPhysical Size = %d\nTotal Physical Size = %d\nEmbedded Stub Size = 4\nMultivolume = +\nVolume Index = %d\nVolumes = %d\n", len(part), len(s.Raw), i, len(parts))
		}
	default:
		t.Fatal("unknown fixture layout", layout)
	}
	for i, part := range parts {
		var name string
		switch layout {
		case "zip", "7z":
			name = "books/tree." + layout
		case "zip_chunks":
			name = fmt.Sprintf("books/tree.zip.%03d", i+1)
		case "7z_chunks":
			name = fmt.Sprintf("books/tree.7z.%03d", i+1)
		case "zip_segments":
			name = fmt.Sprintf("books/tree.z%02d", i+1)
			if i+1 == len(parts) {
				name = "books/tree.zip"
			}
		}
		f.names, f.parts = append(f.names, name), append(f.parts, part)
		writeTree(t, f.folder, map[string][]byte{name: part})
	}
	if layout == "zip_segments" {
		for _, part := range parts {
			p := rootProfile
			p.Body = part
			profiles = append(profiles, p)
		}
	} else {
		profiles = append(profiles, rootProfile)
	}
	stagingtest.InstallMany(t, profiles...)
	return f
}

func TestArchiveTreeCleanupAndPurgeRecovery(t *testing.T) {
	for _, layout := range []string{"zip_chunks", "zip_segments", "7z_chunks"} {
		t.Run(layout, func(t *testing.T) {
			factory := func(t *testing.T, count int, build func([]byte) []byte) streamFixture {
				return newCleanupTreeFixture(t, layout, count, build)
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
			} {
				t.Run(scenario.name, func(t *testing.T) { scenario.run(t, factory) })
			}
			if layout != "7z_chunks" {
				t.Run("refused_sources", func(t *testing.T) { testZIPCleanupRefusesUnaccountedSources(t, factory) })
			}
		})
	}
}

func TestArchiveTreeSingleFilePreviewBindsExactLeaves(t *testing.T) {
	for _, layout := range []string{"zip", "7z"} {
		t.Run(layout, func(t *testing.T) {
			f := newCleanupTreeFixture(t, layout, 1, nil)
			before := snapshotTree(t, f.folder)
			p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
			if err != nil || p.Move != 1 || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
				t.Fatal(p, err)
			}
			e := p.Entries[0]
			if e.ArchiveProof != archiveTreeProof || !validArchiveLeaves(e) || len(e.ArchiveLeaves) != 2 || !reflect.DeepEqual(e.ArchiveLeaves[1].Chain, []string{"middle.7z", "inside.zip", "book.epub"}) {
				t.Fatal(e)
			}
			digest := p.Digest
			e.ArchiveLeaves[1].Chain[0] = "other.7z"
			p.Entries[0] = e
			if planDigest(f.folder, p.Entries) == digest {
				t.Fatal("nested selections missing from preview binding")
			}
			c := f.cleanup(t)
			q, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
			if err != nil || q.Purge != 1 || len(q.ArchiveGroups) != 1 || q.ArchiveGroups[0].LeafDigest == "" {
				t.Fatal(q, err)
			}
			r, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: q.Digest})
			if err != nil || r.Purged != 1 {
				t.Fatal(r, err)
			}
		})
	}
}

func TestArchiveTreeLeafDigestCannotChangeUnderPurgeIntent(t *testing.T) {
	for _, kind := range []string{"missing_digest", "changed_digest", "changed_cleanup_leaf"} {
		t.Run(kind, func(t *testing.T) {
			f := newCleanupTreeFixture(t, "zip_chunks", 3, nil)
			c := f.cleanup(t)
			p, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
			if err != nil {
				t.Fatal(err)
			}
			cleanupHook = func(s string) error {
				if s == "purged" {
					return errors.New("interrupted")
				}
				return nil
			}
			t.Cleanup(func() { cleanupHook = nil })
			if _, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: p.Digest}); err == nil {
				t.Fatal("interruption absent")
			}
			cleanupHook = nil
			digest := p.Digest
			if kind == "changed_cleanup_leaf" {
				rewriteTreeCleanup(t, f.root, c.CleanupID, func(m *cleanupManifest) { m.Entries[0].ArchiveLeaves[1].Chain[0] = "other.7z" })
			} else {
				digest = changeStreamPurgeManifest(t, f.root, func(pm *purgeManifest) {
					pm.ArchiveGroups[0].LeafDigest = ""
					if kind == "changed_digest" {
						pm.ArchiveGroups[0].LeafDigest = strings.Repeat("0", 64)
					}
				})
			}
			before := snapshotTree(t, filepath.Join(c.HoldingArea, "files"))
			if _, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: digest}); !errors.Is(err, ErrStateReview) || !strings.Contains(err.Error(), "invalid archive purge binding") {
				t.Fatal("changed evidence authorized remaining parts", err)
			}
			if !reflect.DeepEqual(before, snapshotTree(t, filepath.Join(c.HoldingArea, "files"))) {
				t.Fatal("remaining bytes changed")
			}
		})
	}
}

func rewriteTreeCleanup(t *testing.T, root, id string, alter func(*cleanupManifest)) string {
	t.Helper()
	journal := filepath.Join(root, ".nemalo", cleanupName)
	lines := bytes.Split(bytes.TrimSuffix(mustRead(t, journal), []byte{'\n'}), []byte{'\n'})
	for i, line := range lines {
		var rec cleanupRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatal(err)
		}
		if rec.Event != "cleanup_started" || rec.CleanupID != id {
			continue
		}
		file := filepath.Join(root, filepath.FromSlash(holdingSlash(id)), "manifest.json")
		var m cleanupManifest
		if err := json.Unmarshal(mustRead(t, file), &m); err != nil {
			t.Fatal(err)
		}
		alter(&m)
		m.Digest = planDigest(m.Folder, m.Entries)
		rec.Digest = m.Digest
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, data, 0600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		rec.Manifest = hex.EncodeToString(sum[:])
		lines[i], err = json.Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(journal, append(bytes.Join(lines, []byte{'\n'}), '\n'), 0600); err != nil {
			t.Fatal(err)
		}
		return m.Digest
	}
	t.Fatal("cleanup intent absent")
	return ""
}

func TestArchiveTreeCleanupResumeRejectsUnsupportedBindingsBeforeWrites(t *testing.T) {
	for _, kind := range []string{"future", "missing_leaves", "wrong_hash", "wrong_chain", "wrong_layout"} {
		t.Run(kind, func(t *testing.T) {
			f := newCleanupTreeFixture(t, "zip_chunks", 3, nil)
			p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
			if err != nil {
				t.Fatal(err)
			}
			crash := errors.New("after first private copy")
			cleanupHook = func(stage string) error {
				if stage == "partial" {
					return crash
				}
				return nil
			}
			t.Cleanup(func() { cleanupHook = nil })
			c, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest})
			cleanupHook = nil
			if !errors.Is(err, crash) {
				t.Fatal(err)
			}
			digest := rewriteTreeCleanup(t, f.root, c.CleanupID, func(m *cleanupManifest) {
				e := &m.Entries[0]
				switch kind {
				case "future":
					e.ArchiveProof = "archive-tree/future"
				case "missing_leaves":
					e.ArchiveLeaves = nil
				case "wrong_hash":
					e.ArchiveLeaves[0].SHA256 = strings.Repeat("0", 64)
				case "wrong_chain":
					e.ArchiveLeaves[0].Chain = []string{"../outside.epub"}
				case "wrong_layout":
					e.Files[2].Path = "books/other.zip.003"
				}
			})
			beforeSource, beforeHolding := snapshotTree(t, f.folder), snapshotTree(t, filepath.Join(c.HoldingArea, "files"))
			if _, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder, Apply: digest}); !errors.Is(err, ErrStateReview) {
				t.Fatal("unsupported cleanup resumed", err)
			}
			if !reflect.DeepEqual(beforeSource, snapshotTree(t, f.folder)) || !reflect.DeepEqual(beforeHolding, snapshotTree(t, filepath.Join(c.HoldingArea, "files"))) {
				t.Fatal("unsupported proof changed bytes")
			}
		})
	}
}

func TestArchiveTreeUnknownProofStillRestoresRecordedSourceBytes(t *testing.T) {
	f := newCleanupTreeFixture(t, "zip_chunks", 3, nil)
	c := f.cleanup(t)
	rewriteTreeCleanup(t, f.root, c.CleanupID, func(m *cleanupManifest) { m.Entries[0].ArchiveProof = "archive-tree/future" })
	saved := staging.SevenZip
	staging.SevenZip = func() (string, error) { t.Fatal("restore reopened unrecognized archive"); return "", nil }
	t.Cleanup(func() { staging.SevenZip = saved })
	r, err := Restore(context.Background(), RestoreRequest{Library: f.root, CleanupID: c.CleanupID, Apply: true})
	if err != nil || r.Restore != 3 {
		t.Fatal(r, err)
	}
	f.assertParts(t, f.folder)
}

func TestArchiveTreeSingleFileCleanupAndRestoreInterruptions(t *testing.T) {
	testSingleArchiveCleanupAndRestoreInterruptions(t, func(t *testing.T) streamFixture {
		return newCleanupTreeFixture(t, "zip", 1, nil)
	})
}

func testSingleArchiveCleanupAndRestoreInterruptions(t *testing.T, fixture func(*testing.T) streamFixture) {
	for _, stage := range []string{"partial", "linked", "held", "restored"} {
		t.Run(stage, func(t *testing.T) {
			f := fixture(t)
			p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
			if err != nil {
				t.Fatal(err)
			}
			crash := errors.New("single-file tree interruption")
			cleanupHook = func(s string) error {
				if s == stage {
					return crash
				}
				return nil
			}
			t.Cleanup(func() { cleanupHook = nil })
			c, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest})
			if stage != "restored" {
				cleanupHook = nil
				if !errors.Is(err, crash) {
					t.Fatal(err)
				}
				c, err = Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest})
				if err != nil || !c.Resumed || c.Moved != 1 {
					t.Fatal(c, err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			r, err := Restore(context.Background(), RestoreRequest{Library: f.root, CleanupID: c.CleanupID, Apply: true})
			cleanupHook = nil
			if stage == "restored" {
				if !errors.Is(err, crash) {
					t.Fatal(err)
				}
				r, err = Restore(context.Background(), RestoreRequest{Library: f.root, CleanupID: c.CleanupID, Apply: true})
				if !r.Resumed {
					t.Fatal(r)
				}
			}
			if err != nil || r.Restore+r.Present != 1 {
				t.Fatal(r, err)
			}
			f.assertParts(t, f.folder)
		})
	}
}

func TestArchiveTreeRefusalLocatesTheExactChild(t *testing.T) {
	f := newCleanupTreeFixture(t, "zip", 1, func(book []byte) []byte {
		b := nestedZIP(t, nestedFile{"book.epub", book})
		return insertDirectoryBytes(b, len(b)-22, []byte("unheld metadata"))
	})
	p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
	if err != nil || p.Move != 0 || !strings.Contains(p.Entries[0].Reason, `["middle.7z","inside.zip"]`) {
		t.Fatal(p, err)
	}
}

func TestArchiveCleanupBindsSnapshotToThePlannedSourceBytes(t *testing.T) {
	_, folder, heldBook := archiveCleanup(t, nil)
	// A valid different byte asset with identical lengths: change one declared
	// timestamp in both local and central headers, retaining the book payload.
	unheldBook := bytes.Clone(heldBook)
	binary.LittleEndian.PutUint16(unheldBook[10:], 1)
	dir := binary.LittleEndian.Uint32(unheldBook[len(unheldBook)-6:])
	binary.LittleEndian.PutUint16(unheldBook[int(dir)+12:], 1)
	original := nestedZIP(t, nestedFile{"book.epub", unheldBook})
	substitute := nestedZIP(t, nestedFile{"book.epub", heldBook})
	if len(original) != len(substitute) || bytes.Equal(original, substitute) {
		t.Fatal("race fixture requires distinct same-size archives")
	}
	writeTree(t, folder, map[string][]byte{"original.zip": original})
	src, err := os.OpenRoot(folder)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	sum := sha256.Sum256(heldBook)
	heldHash := hex.EncodeToString(sum[:])
	held := func(hash string) (Holding, bool, error) {
		return Holding{ID: "sha256:" + heldHash, SHA256: heldHash}, hash == heldHash, nil
	}
	calls := 0
	remaining := int64(maxPlanHashBytes)
	hashFile := func(name string) (CleanupFile, error) {
		calls++
		if calls > 1 {
			if err := src.WriteFile(name, original, 0600); err != nil {
				t.Fatal(err)
			}
		}
		f, err := fileIdentityWithin(context.Background(), src, name, &remaining)
		if calls == 1 {
			if err := src.WriteFile(name, substitute, 0600); err != nil {
				t.Fatal(err)
			}
		}
		return f, err
	}
	e := CleanupEntry{Path: "original.zip", Kind: "archive"}
	if err := planArchive(context.Background(), src, e.Path, nil, held, hashFile, &remaining, &e); err != nil {
		t.Fatal(err)
	}
	if e.Disposition == "move" {
		t.Fatal("different extracted bytes authorized the original unheld archive", e)
	}
	if remaining != maxPlanHashBytes-2*int64(len(original)) {
		t.Fatal("failed snapshot hash work was refunded", remaining)
	}
}

func TestArchiveTreeEveryOuterLayoutBindsTheActualSnapshot(t *testing.T) {
	for _, layout := range []string{"zip", "7z", "zip_chunks", "zip_segments", "7z_chunks"} {
		t.Run(layout, func(t *testing.T) {
			f := newCleanupTreeFixture(t, layout, 3, nil)
			page, err := FindManaged(context.Background(), f.root, "", "all", "all", 10, 0)
			if err != nil || len(page.Holdings) != 1 {
				t.Fatal(page, err)
			}
			h := page.Holdings[0]
			held := func(sum string) (Holding, bool, error) { return h, sum == h.SHA256, nil }
			substitute := f.parts[0]
			original := bytes.Clone(substitute)
			original[len(original)/2] ^= 1
			src, err := os.OpenRoot(f.folder)
			if err != nil {
				t.Fatal(err)
			}
			defer src.Close()
			if err := src.WriteFile(filepath.FromSlash(f.names[0]), original, 0600); err != nil {
				t.Fatal(err)
			}
			remaining := int64(maxPlanHashBytes)
			calls := map[string]int{}
			hashFile := func(name string) (CleanupFile, error) {
				calls[name]++
				if name == f.names[0] && calls[name] > 1 {
					if err := src.WriteFile(filepath.FromSlash(name), original, 0600); err != nil {
						t.Fatal(err)
					}
				}
				identity, err := fileIdentityWithin(context.Background(), src, name, &remaining)
				if name == f.names[0] && calls[name] == 1 {
					if err := src.WriteFile(filepath.FromSlash(name), substitute, 0600); err != nil {
						t.Fatal(err)
					}
				}
				return identity, err
			}
			volumes := f.names
			if len(f.parts) == 1 {
				volumes = nil
			}
			e := CleanupEntry{Path: f.names[0], Kind: "archive"}
			if err := planArchive(context.Background(), src, e.Path, volumes, held, hashFile, &remaining, &e); err != nil {
				t.Fatal(err)
			}
			if e.Disposition == "move" || !strings.Contains(e.Reason, "private snapshot") {
				t.Fatal("unchecked original would move", e)
			}
			expected := int64(len(f.parts[0]))
			for _, part := range f.parts {
				expected += int64(len(part))
			}
			if maxPlanHashBytes-remaining != expected {
				t.Fatal("source hash work not charged", remaining, expected)
			}
		})
	}
}

func TestArchiveTreeProofRejectsMalformedLeafBindings(t *testing.T) {
	f := newCleanupTreeFixture(t, "zip", 1, nil)
	p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"empty", "hash", "holding", "duplicate", "chain", "rar", "bytes"} {
		t.Run(kind, func(t *testing.T) {
			data, _ := json.Marshal(p.Entries[0])
			var e CleanupEntry
			if err := json.Unmarshal(data, &e); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "empty":
				e.ArchiveLeaves = nil
			case "hash":
				e.ArchiveLeaves[0].SHA256 = strings.Repeat("0", 64)
			case "holding":
				e.ArchiveLeaves[0].Holding = "sha256:" + strings.Repeat("0", 64)
			case "duplicate":
				e.ArchiveLeaves[1] = e.ArchiveLeaves[0]
			case "chain":
				e.ArchiveLeaves[1].Chain = []string{"../escape.zip", "book.epub"}
			case "rar":
				e.ArchiveLeaves[1].Chain = []string{"unproven.rar", "book.epub"}
			case "bytes":
				e.ArchiveLeaves[0].Bytes = 0
			}
			if supportedArchiveProof(e) {
				t.Fatal("malformed leaf proof accepted", kind)
			}
		})
	}
}

func TestArchiveTree7zOuterRetainsUnheldInnerMaterial(t *testing.T) {
	for _, kind := range []string{"notes", "gap", "missing_tool"} {
		t.Run(kind, func(t *testing.T) {
			f := newCleanupTreeFixture(t, "7z_chunks", 3, func(book []byte) []byte {
				inner := nestedZIP(t, nestedFile{"book.epub", book})
				if kind == "notes" {
					inner = nestedZIP(t, nestedFile{"book.epub", book}, nestedFile{"notes.txt", []byte("retained notes")})
				}
				if kind == "gap" {
					inner = insertDirectoryBytes(inner, len(inner)-22, []byte("retained metadata"))
				}
				return inner
			})
			if kind == "missing_tool" {
				saved := staging.SevenZip
				staging.SevenZip = func() (string, error) { return "", errors.New("absent") }
				t.Cleanup(func() { staging.SevenZip = saved })
			}
			before := snapshotTree(t, f.folder)
			p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
			if err != nil || p.Move != 0 || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
				t.Fatal(p, err)
			}
		})
	}
}
