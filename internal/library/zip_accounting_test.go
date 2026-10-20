package library

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/safeio"
)

func zipProofFixture(t testing.TB, extra []byte, comment string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, name := range []string{"one.epub", "two.epub"} {
		h := &zip.FileHeader{Name: name, Method: zip.Store, Extra: extra, Comment: comment}
		f, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte("synthetic " + name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipProofAccepts(data []byte) bool {
	ctx := context.Background()
	r := &zipProofReader{ctx: ctx, source: bytes.NewReader(data), remaining: zipProofBytes}
	z, err := assessment.OpenZIP(ctx, r, int64(len(data)), zipProofBytes, 8<<30, 10000)
	return err == nil && zipContiguous(r, int64(len(data)), z)
}

func TestZIPCleanupProofRejectsUnaccountedAndContradictoryRecords(t *testing.T) {
	base := zipProofFixture(t, nil, "")
	u16, u32 := binary.LittleEndian.Uint16, binary.LittleEndian.Uint32
	p16, p32 := binary.LittleEndian.PutUint16, binary.LittleEndian.PutUint32
	dir := int(u32(base[len(base)-6:]))
	next := dir + 46 + int(u16(base[dir+28:])) + int(u16(base[dir+30:]))
	z, err := zip.NewReader(bytes.NewReader(base), int64(len(base)))
	if err != nil || !zipProofAccepts(base) {
		t.Fatal("ordinary fixture did not qualify", err)
	}
	data, err := z.File[0].DataOffset()
	if err != nil {
		t.Fatal(err)
	}
	descriptor := int(data) + int(z.File[0].CompressedSize64)
	for name, mutate := range map[string]func([]byte) []byte{
		"directory_gap_before":   func(b []byte) []byte { return insertDirectoryBytes(b, dir, []byte("unheld")) },
		"directory_gap_between":  func(b []byte) []byte { return insertDirectoryBytes(b, next, []byte("unheld")) },
		"directory_gap_after":    func(b []byte) []byte { return insertDirectoryBytes(b, len(b)-22, []byte("unheld")) },
		"disk":                   func(b []byte) []byte { p16(b[len(b)-18:], 1); return b },
		"directory_disk":         func(b []byte) []byte { p16(b[len(b)-16:], 1); return b },
		"disk_count":             func(b []byte) []byte { p16(b[len(b)-14:], 1); return b },
		"total_count":            func(b []byte) []byte { p16(b[len(b)-12:], 1); return b },
		"directory_offset":       func(b []byte) []byte { p32(b[len(b)-6:], uint32(dir+1)); return b },
		"directory_size":         func(b []byte) []byte { p32(b[len(b)-10:], u32(b[len(b)-10:])+1); return b },
		"directory_zip64_size":   func(b []byte) []byte { p32(b[len(b)-10:], 0xffffffff); return b },
		"entry_disk":             func(b []byte) []byte { p16(b[dir+34:], 1); return b },
		"duplicate_local_offset": func(b []byte) []byte { p32(b[next+42:], 0); return b },
		"local_name":             func(b []byte) []byte { b[30] ^= 1; return b },
		"local_flags":            func(b []byte) []byte { p16(b[6:], u16(b[6:])^0x800); return b },
		"local_method":           func(b []byte) []byte { p16(b[8:], zip.Deflate); return b },
		"local_version":          func(b []byte) []byte { p16(b[4:], 45); return b },
		"unsupported_version": func(b []byte) []byte {
			p16(b[4:], 0)
			p16(b[dir+6:], 0)
			return b
		},
		"local_time":             func(b []byte) []byte { p16(b[10:], 1); return b },
		"local_crc":              func(b []byte) []byte { p32(b[14:], z.File[0].CRC32^1); return b },
		"local_size":             func(b []byte) []byte { p32(b[18:], 0xffffffff); return b },
		"local_name_length":      func(b []byte) []byte { p16(b[26:], 0xffff); return b },
		"local_extra_length":     func(b []byte) []byte { p16(b[28:], 0xffff); return b },
		"central_name_length":    func(b []byte) []byte { p16(b[dir+28:], 0xffff); return b },
		"central_extra_length":   func(b []byte) []byte { p16(b[dir+30:], 0xffff); return b },
		"central_comment_length": func(b []byte) []byte { p16(b[dir+32:], 0xffff); return b },
		"descriptor_crc":         func(b []byte) []byte { p32(b[descriptor+4:], z.File[0].CRC32^1); return b },
		"descriptor_packed_size": func(b []byte) []byte { p32(b[descriptor+8:], uint32(z.File[0].CompressedSize64)+1); return b },
		"descriptor_size":        func(b []byte) []byte { p32(b[descriptor+12:], uint32(z.File[0].UncompressedSize64)+1); return b },
		"reserved_flag":          func(b []byte) []byte { p16(b[6:], u16(b[6:])|0x4000); p16(b[dir+8:], u16(b[dir+8:])|0x4000); return b },
		"tail":                   func(b []byte) []byte { return append(b, []byte("unheld")...) },
		"truncated":              func(b []byte) []byte { return b[:len(b)-1] },
	} {
		t.Run(name, func(t *testing.T) {
			b := mutate(bytes.Clone(base))
			if zipProofAccepts(b) {
				t.Fatal("unaccounted or inconsistent ZIP qualified")
			}
		})
	}
	// The Go decoder ignores a descriptor's size fields. Its ability to return
	// the correct member bytes does not account for those contradictory fields.
	b := bytes.Clone(base)
	p32(b[descriptor+8:], uint32(z.File[0].CompressedSize64)+1)
	readable, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	f, err := readable.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(f)
	f.Close()
	if err != nil {
		t.Fatal("descriptor regression does not demonstrate decoder acceptance", err)
	}
}

func insertDirectoryBytes(b []byte, pos int, hidden []byte) []byte {
	dirSize := binary.LittleEndian.Uint32(b[len(b)-10:])
	out := append(append(append([]byte{}, b[:pos]...), hidden...), b[pos:]...)
	binary.LittleEndian.PutUint32(out[len(out)-10:], dirSize+uint32(len(hidden)))
	return out
}

func TestZIPCleanupProofKeepsCommentsAndUnknownMetadata(t *testing.T) {
	for name, data := range map[string][]byte{
		"member_comment":     zipProofFixture(t, nil, "a source annotation"),
		"unknown_extra":      zipProofFixture(t, []byte{0xfe, 0xca, 1, 0, 'x'}, ""),
		"truncated_extra":    zipProofFixture(t, []byte{0x55, 0x54, 5, 0, 1}, ""),
		"unframed_extra":     zipProofFixture(t, []byte{'x'}, ""),
		"duplicate_extra":    zipProofFixture(t, []byte{0x55, 0x54, 1, 0, 0, 0x55, 0x54, 1, 0, 0}, ""),
		"unix_attached_data": zipProofFixture(t, append([]byte{0x0d, 0, 13, 0}, make([]byte, 13)...), ""),
	} {
		t.Run(name, func(t *testing.T) {
			if zipProofAccepts(data) {
				t.Fatal("uncertain metadata qualified")
			}
		})
	}
	for _, local := range []bool{false, true} {
		if !zipMetadataExtra(nil, local) || !zipMetadataExtra([]byte{0x55, 0x54, 5, 0, 1, 0, 0, 0, 0}, local) {
			t.Fatal("ordinary timestamp metadata refused")
		}
		ntfs := make([]byte, 36)
		binary.LittleEndian.PutUint16(ntfs, 0xa)
		binary.LittleEndian.PutUint16(ntfs[2:], 32)
		binary.LittleEndian.PutUint16(ntfs[8:], 1)
		binary.LittleEndian.PutUint16(ntfs[10:], 24)
		if !zipMetadataExtra(ntfs, local) {
			t.Fatal("ordinary NTFS timestamps refused")
		}
		ntfs[8] = 2
		if zipMetadataExtra(ntfs, local) {
			t.Fatal("unknown NTFS attribute qualified")
		}
	}
}

func TestZIPCleanupProofAcceptsOrdinaryDescriptorAndDirectoryVariants(t *testing.T) {
	base := zipProofFixture(t, nil, "")
	z, err := zip.NewReader(bytes.NewReader(base), int64(len(base)))
	if err != nil {
		t.Fatal(err)
	}
	data, err := z.File[0].DataOffset()
	if err != nil {
		t.Fatal(err)
	}
	descriptor := int(data) + int(z.File[0].CompressedSize64)
	dir := int(binary.LittleEndian.Uint32(base[len(base)-6:]))
	next := dir + 46 + len(z.File[0].Name)
	// Remove only the first descriptor's optional signature. The later local
	// header and central-directory position shift by four, with sizes unchanged.
	unsigned := append(append([]byte{}, base[:descriptor]...), base[descriptor+4:]...)
	binary.LittleEndian.PutUint32(unsigned[len(unsigned)-6:], uint32(dir-4))
	binary.LittleEndian.PutUint32(unsigned[next-4+42:], binary.LittleEndian.Uint32(base[next+42:])-4)
	if !zipProofAccepts(unsigned) {
		t.Fatal("ordinary unsigned descriptor refused")
	}
	reordered := bytes.Clone(base)
	copy(reordered[dir:next], base[next:len(base)-22])
	copy(reordered[next:len(base)-22], base[dir:next])
	if !zipProofAccepts(reordered) {
		t.Fatal("directory order differing from local order refused")
	}
	var raw bytes.Buffer
	w := zip.NewWriter(&raw)
	body := []byte("ordinary stored bytes without a descriptor")
	h := &zip.FileHeader{Name: "book.epub", ReaderVersion: 20, Method: zip.Store, CRC32: crc32.ChecksumIEEE(body), CompressedSize64: uint64(len(body)), UncompressedSize64: uint64(len(body))}
	f, err := w.CreateRaw(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if !zipProofAccepts(raw.Bytes()) {
		t.Fatal("ordinary file without a descriptor refused")
	}
	for _, extra := range [][]byte{
		{0x55, 0x54, 5, 0, 1, 0, 0, 0, 0},
		append([]byte{0x0d, 0, 12, 0}, make([]byte, 12)...),
		append([]byte{0x55, 0x58, 8, 0}, make([]byte, 8)...),
	} {
		if !zipProofAccepts(zipProofFixture(t, extra, "")) {
			t.Fatal("ordinary timestamp/identity metadata refused", extra)
		}
	}
}

func TestZIPCleanupProofReadBudgetAndCancellation(t *testing.T) {
	data := zipProofFixture(t, nil, "")
	ctx, cancel := context.WithCancel(context.Background())
	r := &zipProofReader{ctx: ctx, source: bytes.NewReader(data), remaining: 8}
	p := make([]byte, 4)
	if _, err := r.ReadAt(p, 0); err != nil || r.remaining != 4 {
		t.Fatal(r.remaining, err)
	}
	if _, err := r.ReadAt(p, int64(len(data)-2)); !errors.Is(err, io.EOF) || r.remaining != 2 {
		t.Fatal("failed read work refunded", r.remaining, err)
	}
	if _, err := r.ReadAt(p, 0); !errors.Is(err, assessment.ErrLimit) || r.remaining != 2 {
		t.Fatal("allowance exceeded", r.remaining, err)
	}
	cancel()
	if _, err := r.ReadAt(p[:1], 0); !errors.Is(err, context.Canceled) || r.remaining != 2 {
		t.Fatal("cancellation ignored", r.remaining, err)
	}
	for _, allowance := range []int64{0, 4, int64(len(data))} {
		r := &zipProofReader{ctx: context.Background(), source: bytes.NewReader(data), remaining: allowance}
		z, err := assessment.OpenZIP(r.ctx, r, int64(len(data)), zipProofBytes, 8<<30, 10000)
		if err == nil && zipContiguous(r, int64(len(data)), z) {
			t.Fatal("decoder/proof reads did not share allowance", allowance)
		}
		if r.remaining < 0 {
			t.Fatal("negative allowance", r.remaining)
		}
	}
	active, stop := context.WithCancel(context.Background())
	defer stop()
	reader := &zipProofReader{ctx: active, source: cancelZIPRead{ReaderAt: bytes.NewReader(data), cancel: stop}, remaining: zipProofBytes}
	if n, err := reader.ReadAt(p, 0); n != len(p) || !errors.Is(err, context.Canceled) || reader.remaining != zipProofBytes-int64(n) {
		t.Fatal("active cancellation lost its read accounting", n, reader.remaining, err)
	}
}

type cancelZIPRead struct {
	io.ReaderAt
	cancel context.CancelFunc
}

func (r cancelZIPRead) ReadAt(p []byte, off int64) (int, error) {
	n, err := r.ReaderAt.ReadAt(p, off)
	r.cancel()
	return n, err
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
	// The ordinary clean archive still qualifies for purge and can be restored.
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

// legacyZIPCleanup constructs the exact manifest/hash/journal state that the
// earlier permissive proof could emit. Only temporary synthetic fixture data is
// rewritten. Production parsers and plan binding remain enabled in the test.
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
				// An old pending purge may have explicitly authorized this ZIP.
				// Recreate that durable intent and require the new writer to refuse
				// its removal while completing other independently held entries.
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

func FuzzZIPCleanupProofStaysBounded(f *testing.F) {
	f.Add([]byte("PK\x03\x04"))
	f.Add([]byte("PK\x05\x06"))
	valid := zipProofFixture(f, nil, "")
	f.Add(valid)
	f.Add(insertDirectoryBytes(valid, len(valid)-22, []byte("unheld")))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 65536 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		r := &zipProofReader{ctx: ctx, source: bytes.NewReader(data), remaining: 1 << 20}
		z, err := assessment.OpenZIP(ctx, r, int64(len(data)), 1<<20, 1<<20, 100)
		if err == nil {
			_ = zipContiguous(r, int64(len(data)), z)
		}
		if r.remaining < 0 {
			t.Fatal("proof exceeded its shared read allowance")
		}
	})
}
