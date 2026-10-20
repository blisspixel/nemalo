package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/blisspixel/nemalo/internal/safeio"
)

func cleanupRAR5Numbers(values ...uint64) []byte {
	var b []byte
	for _, value := range values {
		b = binary.AppendUvarint(b, value)
	}
	return b
}

func cleanupRAR5Block(kind uint64, name string, data []byte) []byte {
	h := cleanupRAR5Numbers(kind, 0)
	if kind == 1 || kind == 5 {
		h = append(h, 0)
	} else {
		h = cleanupRAR5Numbers(kind, 2, uint64(len(data)), 4, uint64(len(data)), 0100644)
		h = binary.LittleEndian.AppendUint32(h, crc32.ChecksumIEEE(data))
		h = append(h, cleanupRAR5Numbers(0, 1, uint64(len(name)))...)
		h = append(h, name...)
	}
	h = append(cleanupRAR5Numbers(uint64(len(h))), h...)
	return append(append(binary.LittleEndian.AppendUint32(nil, crc32.ChecksumIEEE(h)), h...), data...)
}

func cleanupRAR5WithService(book []byte) []byte {
	b := append([]byte("Rar!\x1a\x07\x01\x00"), cleanupRAR5Block(1, "", nil)...)
	b = append(b, cleanupRAR5Block(2, "one.epub", book)...)
	b = append(b, cleanupRAR5Block(2, "two.epub", book)...)
	b = append(b, cleanupRAR5Block(3, "ZZZ", []byte("unheld historical service material"))...)
	return append(b, cleanupRAR5Block(5, "", nil)...)
}

// Simulate a canonical older entry and its matching source identities, without
// asking current planning policy to approve the unaccounted container.
func legacyRARFixture(t *testing.T, pending bool, name string) (streamFixture, CleanupResult, []byte, string, string) {
	t.Helper()
	var book []byte
	f := newCleanupTreeFixture(t, "zip", 1, func(data []byte) []byte {
		book = append([]byte{}, data...)
		return nestedZIP(t, nestedFile{"book.epub", data})
	})
	ctx := context.Background()
	var c CleanupResult
	if pending {
		p, err := Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder})
		if err != nil {
			t.Fatal(err)
		}
		crash := errors.New("historical cleanup interruption")
		cleanupHook = func(stage string) error {
			if stage == "partial" {
				return crash
			}
			return nil
		}
		t.Cleanup(func() { cleanupHook = nil })
		c, err = Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest})
		cleanupHook = nil
		if !errors.Is(err, crash) {
			t.Fatal("interruption absent", err)
		}
	} else {
		c = f.cleanup(t)
	}
	parent := filepath.Join(c.HoldingArea, "files")
	if pending {
		parent = f.folder
	}
	old := filepath.Join(parent, filepath.FromSlash(f.names[0]))
	file := filepath.Join(parent, filepath.FromSlash(name))
	if err := os.Rename(old, file); err != nil {
		t.Fatal(err)
	}
	data := cleanupRAR5WithService(book)
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	digest := rewriteTreeCleanup(t, f.root, c.CleanupID, func(m *cleanupManifest) {
		e := &m.Entries[0]
		e.Path, e.ArchiveProof, e.ArchiveLeaves = name, "", nil
		e.Files = []CleanupFile{{Path: name, Bytes: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}}
	})
	return f, c, data, name, digest
}

func TestRARHistoricalCleanupCannotResumeWithoutCoverageProof(t *testing.T) {
	for _, name := range []string{"books/historical.rar", "books/historical.r00"} {
		t.Run(filepath.Base(name), func(t *testing.T) {
			f, c, _, _, digest := legacyRARFixture(t, true, name)
			beforeSource := snapshotTree(t, f.folder)
			beforeHeld := snapshotTree(t, filepath.Join(c.HoldingArea, "files"))
			_, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder, Apply: digest})
			if !errors.Is(err, ErrStateReview) {
				t.Fatal("unaccounted historical RAR cleanup resumed", err)
			}
			if !reflect.DeepEqual(beforeSource, snapshotTree(t, f.folder)) || !reflect.DeepEqual(beforeHeld, snapshotTree(t, filepath.Join(c.HoldingArea, "files"))) {
				t.Fatal("historical source or retained copies changed")
			}
		})
	}
}

func TestRARHistoricalFreshAndPendingPurgeRetainUnaccountedBytes(t *testing.T) {
	for _, scenario := range []struct {
		pending bool
		file    string
	}{
		{false, "books/historical.rar"}, {true, "books/historical.rar"},
		{false, "books/historical.r00"}, {true, "books/historical.r00"},
	} {
		pending := scenario.pending
		name := "fresh"
		if pending {
			name = "pending"
		}
		t.Run(filepath.Base(scenario.file)+"/"+name, func(t *testing.T) {
			f, c, data, relative, _ := legacyRARFixture(t, false, scenario.file)
			ctx := context.Background()
			assets := snapshotTree(t, filepath.Join(f.root, "assets"))
			p, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
			if err != nil {
				t.Fatal(err)
			}
			if !pending && (p.Purge != 0 || purgeDispositions(p)[relative] != "archive_not_accounted") {
				t.Fatal("historical RAR received fresh purge permission", p)
			}
			digest := p.Digest
			if pending {
				p.Files[0].Disposition = "purge"
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
				record := cleanupRecord{SchemaVersion: 1, Sequence: len(records) + 1, LibraryID: c.LibraryID, CleanupID: c.CleanupID, Event: "purge_started", Digest: digest, Count: len(p.Files), At: now()}
				pm := purgeManifest{SchemaVersion: 1, LibraryID: c.LibraryID, CleanupID: c.CleanupID, Digest: digest, Files: p.Files}
				record.Manifest, err = writePurgeManifest(store, pm, record.Sequence)
				if err == nil {
					err = appendCanonical(ctx, control, cleanupName, maxCleanupJournal, record)
				}
				store.Close()
				control.close()
				if err != nil {
					t.Fatal(err)
				}
			}
			r, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: digest})
			if err != nil || r.Purged != 0 || r.Resumed != pending || purgeDispositions(r)[relative] != "archive_not_accounted" {
				t.Fatal("unaccounted historical RAR was purged", r, err)
			}
			if !bytes.Equal(mustRead(t, filepath.Join(c.HoldingArea, "files", filepath.FromSlash(relative))), data) || !reflect.DeepEqual(assets, snapshotTree(t, filepath.Join(f.root, "assets"))) {
				t.Fatal("historical or stored bytes changed")
			}
			restored, err := Restore(ctx, RestoreRequest{Library: f.root, CleanupID: c.CleanupID, Apply: true})
			if err != nil || restored.Restore != 1 || !bytes.Equal(mustRead(t, filepath.Join(f.folder, filepath.FromSlash(relative))), data) {
				t.Fatal("recorded historical bytes cannot be restored", restored, err)
			}
		})
	}
}
