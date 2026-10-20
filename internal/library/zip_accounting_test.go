package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"github.com/blisspixel/nemalo/internal/safeio"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func insertDirectoryBytes(b []byte, pos int, hidden []byte) []byte {
	dirSize := binary.LittleEndian.Uint32(b[len(b)-10:])
	out := append(append(append([]byte{}, b[:pos]...), hidden...), b[pos:]...)
	binary.LittleEndian.PutUint32(out[len(out)-10:], dirSize+uint32(len(hidden)))
	return out
}

func TestZIPCleanupRefusalPreservesSourcesAndOrdinaryRecovery(t *testing.T) {
	ctx := context.Background()
	root, folder := cleanupFixture(t)
	clean := mustRead(t, filepath.Join(folder, "held.zip"))
	gapped := insertDirectoryBytes(clean, len(clean)-22, []byte("unheld source material"))
	bad := filepath.Join(folder, "gapped.zip")
	if err := os.WriteFile(bad, gapped, 0600); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, folder)
	p, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	if err != nil || entryFor(t, p, "gapped.zip").Disposition != "keep" {
		t.Fatal(p, err)
	}
	if !reflect.DeepEqual(before, snapshotTree(t, folder)) {
		t.Fatal("preview changed sources")
	}
	cleanup := CleanupResult{}
	cleanup, err = Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: p.Digest})
	if err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, bad); !bytes.Equal(got, gapped) {
		t.Fatal("refused archive moved or changed")
	}

	preview, err := Purge(ctx, PurgeRequest{Library: root, CleanupID: cleanup.CleanupID})
	if err != nil || purgeDispositions(preview)["held.zip"] != "purge" {
		t.Fatal(preview, err)
	}
	if r, err := Restore(ctx, RestoreRequest{Library: root, CleanupID: cleanup.CleanupID, Apply: true}); err != nil || r.Restore != 5 {
		t.Fatal(r, err)
	}
	if !reflect.DeepEqual(before, snapshotTree(t, folder)) {
		t.Fatal("ordinary recovery or refused archive changed")
	}
}

func legacyZIPCleanup(t *testing.T) (string, CleanupResult, []byte) {
	t.Helper()
	root, cleanup := cleanedFixture(t)
	held := filepath.Join(cleanup.HoldingArea, "files", "held.zip")
	clean := mustRead(t, held)
	gapped := insertDirectoryBytes(clean, len(clean)-22, []byte("unheld historical source material"))
	if err := os.WriteFile(held, gapped, 0600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(cleanup.HoldingArea, "manifest.json")
	var m cleanupManifest
	if err := json.Unmarshal(mustRead(t, manifestPath), &m); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(gapped)
	found := false
	for i := range m.Entries {
		for j := range m.Entries[i].Files {
			f := &m.Entries[i].Files[j]
			if f.Path == "held.zip" {
				f.Bytes, f.SHA256 = int64(len(gapped)), hex.EncodeToString(sum[:])
				found = true
			}
		}
	}
	if !found {
		t.Fatal("missing legacy ZIP entry")
	}
	m.Digest = planDigest(m.Folder, m.Entries)
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	manifestHash := sha256.Sum256(data)
	journalPath := filepath.Join(root, ".nemalo", cleanupName)
	var journal bytes.Buffer
	for _, line := range bytes.Split(bytes.TrimSuffix(mustRead(t, journalPath), []byte{'\n'}), []byte{'\n'}) {
		var record cleanupRecord
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatal(err)
		}
		if record.CleanupID == cleanup.CleanupID && record.Event == "cleanup_started" {
			record.Digest, record.Manifest = m.Digest, hex.EncodeToString(manifestHash[:])
		}
		encoded, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		journal.Write(encoded)
		journal.WriteByte('\n')
	}
	if err := os.WriteFile(journalPath, journal.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return root, cleanup, gapped
}

func TestZIPCleanupProofProtectsHistoricalAndPendingPurges(t *testing.T) {
	for _, pending := range []bool{false, true} {
		name := "new_preview"
		if pending {
			name = "old_pending_manifest"
		}
		t.Run(name, func(t *testing.T) {
			root, cleanup, gapped := legacyZIPCleanup(t)
			ctx := context.Background()
			stored := snapshotTree(t, filepath.Join(root, "assets"))
			preview, err := Purge(ctx, PurgeRequest{Library: root, CleanupID: cleanup.CleanupID})
			if err != nil || preview.Purge != 2 || purgeDispositions(preview)["held.zip"] != "archive_not_accounted" {
				t.Fatal("historical ZIP was trusted", preview, err)
			}
			digest := preview.Digest
			if pending {

				for i := range preview.Files {
					if preview.Files[i].Path == "held.zip" {
						preview.Files[i].Disposition = "purge"
					}
				}
				digest = purgeDigest(cleanup.CleanupID, preview.Files)
				control, _, err := openControl(root, true)
				if err != nil {
					t.Fatal(err)
				}
				records, err := readCleanup(control, cleanup.LibraryID)
				if err != nil {
					control.close()
					t.Fatal(err)
				}
				store, err := safeio.OpenRoot(root)
				if err != nil {
					control.close()
					t.Fatal(err)
				}
				record := cleanupRecord{SchemaVersion: 1, Sequence: len(records) + 1, LibraryID: cleanup.LibraryID, CleanupID: cleanup.CleanupID, Event: "purge_started", Digest: digest, Count: len(preview.Files), At: now()}
				pm := purgeManifest{SchemaVersion: 1, LibraryID: cleanup.LibraryID, CleanupID: cleanup.CleanupID, Digest: digest, Files: preview.Files}
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
			applied, err := Purge(ctx, PurgeRequest{Library: root, CleanupID: cleanup.CleanupID, Apply: digest})
			if err != nil || applied.Purged != 2 || applied.Resumed != pending || purgeDispositions(applied)["held.zip"] != "archive_not_accounted" {
				t.Fatal("unsafe historical purge", applied, err)
			}
			if got := mustRead(t, filepath.Join(cleanup.HoldingArea, "files", "held.zip")); !bytes.Equal(got, gapped) {
				t.Fatal("historical unaccounted bytes lost")
			}
			if !reflect.DeepEqual(stored, snapshotTree(t, filepath.Join(root, "assets"))) {
				t.Fatal("stored assets changed")
			}
			restored, err := Restore(ctx, RestoreRequest{Library: root, CleanupID: cleanup.CleanupID, Apply: true})
			if err != nil || restored.Restore != 3 || restored.Purged != 2 {
				t.Fatal(restored, err)
			}
			if got := mustRead(t, filepath.Join(cleanup.Folder, "held.zip")); !bytes.Equal(got, gapped) {
				t.Fatal("full historical archive was not restored")
			}
		})
	}
}
