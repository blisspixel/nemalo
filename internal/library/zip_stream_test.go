package library

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

type streamFixture struct {
	root, folder string
	names        []string
	parts        [][]byte
	archiveProof string
	archives     []stagingtest.Archive
}

func streamZIPBytes(t *testing.T, book []byte, notes bool, comment string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.CreateHeader(&zip.FileHeader{Name: "book.epub", Method: zip.Store, Comment: comment})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(book); err != nil {
		t.Fatal(err)
	}
	if notes {
		f, err := w.CreateHeader(&zip.FileHeader{Name: "notes.txt", Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte("unheld notes")); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func newStreamFixture(t *testing.T, count int, build func([]byte) []byte) streamFixture {
	t.Helper()
	f := streamFixture{root: t.TempDir(), folder: t.TempDir()}
	ctx := context.Background()
	if _, err := Initialize(ctx, f.root); err != nil {
		t.Fatal(err)
	}
	bookPath := writeEPUBParagraph(t, t.TempDir(), "Recoverable ZIP byte chunks")
	book := mustRead(t, bookPath)
	if _, err := Import(ctx, ImportRequest{Library: f.root, Source: bookPath, Apply: true, Scan: true, Scanner: cleanScan{}}); err != nil {
		t.Fatal(err)
	}
	body := streamZIPBytes(t, book, false, "")
	if build != nil {
		body = build(book)
	}
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("books/stream.zip.%03d", i+1)
		part := bytes.Clone(body[i*len(body)/count : (i+1)*len(body)/count])
		f.names = append(f.names, name)
		f.parts = append(f.parts, part)
		writeTree(t, f.folder, map[string][]byte{name: part})
	}
	stagingtest.Install(t, stagingtest.Archive{Kind: "-tzip", Header: fmt.Sprintf("Type = zip\nPhysical Size = %d\n", len(body)), Volumes: []stagingtest.Volume{{Name: "archive.zip", Body: body}}})
	return f
}

func (f streamFixture) cleanup(t *testing.T) CleanupResult {
	t.Helper()
	ctx := context.Background()
	p, err := Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder})
	if err != nil || p.Move != 1 || p.Keep != 0 || len(p.Entries) != 1 || p.Entries[0].ArchiveProof != f.proof() || len(p.Entries[0].Files) != len(f.parts) {
		t.Fatal(p, err)
	}
	r, err := Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest})
	if err != nil || r.Moved != 1 || r.NotMoved != 0 {
		t.Fatal(r, err)
	}
	return r
}

func (f streamFixture) assertParts(t *testing.T, parent string) {
	t.Helper()
	for i, n := range f.names {
		if got := mustRead(t, filepath.Join(parent, filepath.FromSlash(n))); !bytes.Equal(got, f.parts[i]) {
			t.Fatal("part differs", n)
		}
	}
}

func testZIPCleanupRestoreAndPurge(t *testing.T, fixture zipFixtureFactory) {
	f := fixture(t, 3, nil)
	ctx := context.Background()
	before := snapshotTree(t, f.folder)
	p, err := Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder})
	if err != nil || p.Move != 1 || p.Applied || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
		t.Fatal(p, err)
	}
	c := f.cleanup(t)
	f.assertParts(t, filepath.Join(c.HoldingArea, "files"))
	if again, err := Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder}); err != nil || again.Move != 0 {
		t.Fatal(again, err)
	}
	r, err := Restore(ctx, RestoreRequest{Library: f.root, CleanupID: c.CleanupID, Apply: true})
	if err != nil || r.Restore != 3 {
		t.Fatal(r, err)
	}
	f.assertParts(t, f.folder)
	c = f.cleanup(t)
	stored := snapshotTree(t, filepath.Join(f.root, "assets"))
	preview, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
	if err != nil || preview.Purge != 3 || len(preview.ArchiveGroups) != 1 || preview.ArchiveGroups[0].Proof != f.proof() || !reflect.DeepEqual(preview.ArchiveGroups[0].Indices, []int{0, 1, 2}) {
		t.Fatal(preview, err)
	}
	if preview.Digest == purgeDigest(c.CleanupID, preview.Files) {
		t.Fatal("group evidence absent from plan binding")
	}
	done, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: preview.Digest})
	if err != nil || done.Purged != 3 || !done.Applied {
		t.Fatal(done, err)
	}
	if !reflect.DeepEqual(stored, snapshotTree(t, filepath.Join(f.root, "assets"))) {
		t.Fatal("stored assets changed")
	}
	r, err = Restore(ctx, RestoreRequest{Library: f.root, CleanupID: c.CleanupID, Apply: true})
	if err != nil || r.Purged != 3 || r.Restore != 0 {
		t.Fatal(r, err)
	}
}

func testZIPCleanupRefusesUnaccountedSources(t *testing.T, fixture zipFixtureFactory) {
	testArchiveCleanupRefusals(t, fixture, []string{"notes", "member_comment", "directory_gap", "crc", "missing", "changed", "alias", "missing_tool"})
}

func testArchiveCleanupRefusals(t *testing.T, fixture zipFixtureFactory, kinds []string) {
	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			build := func(book []byte) []byte {
				b := streamZIPBytes(t, book, kind == "notes", map[bool]string{true: "preserved annotation"}[kind == "member_comment"])
				if kind == "directory_gap" {
					b = insertDirectoryBytes(b, len(b)-22, []byte("unaccounted"))
				}
				if kind == "crc" {
					z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
					if err != nil {
						t.Fatal(err)
					}
					off, err := z.File[0].DataOffset()
					if err != nil {
						t.Fatal(err)
					}
					b[int(off)+1] ^= 1
				}
				return b
			}
			f := fixture(t, 3, build)
			switch kind {
			case "missing":
				if err := os.Remove(filepath.Join(f.folder, filepath.FromSlash(f.names[1]))); err != nil {
					t.Fatal(err)
				}
			case "changed":
				if err := os.WriteFile(filepath.Join(f.folder, filepath.FromSlash(f.names[1])), []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "alias":
				alias := "books/STREAM.ZIP.0001"
				if f.proof() == zipSegmentsProof {
					alias = "books/SEGMENTS.Z001"
				}
				if f.proof() == rar5SetProof || f.proof() == rar4SetProof {
					alias = "books/SET.part0001.RAR"
				}
				if f.proof() == archiveTreeProof || f.proof() == archiveRARTreeProof {
					base := strings.ToUpper(filepath.Base(f.names[0]))
					if isZIPChunk(f.names[0]) {
						base = strings.TrimSuffix(base, ".001") + ".0001"
					} else {
						base = strings.TrimSuffix(base, ".Z01") + ".Z001"
					}
					alias = filepath.ToSlash(filepath.Join(filepath.Dir(f.names[0]), base))
				}
				writeTree(t, f.folder, map[string][]byte{alias: f.parts[0]})
			case "missing_tool":
				saved := staging.SevenZip
				staging.SevenZip = func() (string, error) { return "", errors.New("unavailable") }
				t.Cleanup(func() { staging.SevenZip = saved })
			}
			before := snapshotTree(t, f.folder)
			p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
			if err != nil || p.Move != 0 || p.Keep != len(before) {
				t.Fatal(p, err)
			}
			if !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
				t.Fatal("refusal changed sources")
			}
			if _, err := os.Stat(filepath.Join(f.root, cleanupDir)); !os.IsNotExist(err) {
				t.Fatal("preview created recovery state", err)
			}
		})
	}
}

func testZIPCleanupRecoversEachPartWithoutAReader(t *testing.T, fixture zipFixtureFactory) {
	for _, stage := range []string{"manifest", "partial", "linked", "held"} {
		for stop := 1; stop <= 3; stop++ {
			if stage == "manifest" && stop != 1 {
				continue
			}
			t.Run(fmt.Sprintf("%s/%d", stage, stop), func(t *testing.T) {
				f := fixture(t, 3, nil)
				ctx := context.Background()
				p, err := Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder})
				if err != nil {
					t.Fatal(err)
				}
				crash := errors.New("synthetic per-part interruption")
				calls := 0
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
				_, err = Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest})
				cleanupHook = nil
				if !errors.Is(err, crash) {
					t.Fatal("interruption not reached", err)
				}
				if stage != "manifest" {
					saved := staging.SevenZip
					staging.SevenZip = func() (string, error) { t.Fatal("resume re-planned or reopened archive"); return "", nil }
					t.Cleanup(func() { staging.SevenZip = saved })
				}
				done, err := Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest})
				if err != nil || done.Resumed != (stage != "manifest") || done.Moved != 1 {
					t.Fatal(done, err)
				}
				f.assertParts(t, filepath.Join(done.HoldingArea, "files"))
				r, err := Restore(ctx, RestoreRequest{Library: f.root, CleanupID: done.CleanupID, Apply: true})
				if err != nil || r.Restore != 3 {
					t.Fatal(r, err)
				}
				f.assertParts(t, f.folder)
			})
		}
	}
}

func testZIPPurgeResumesEveryPartFromItsBoundProof(t *testing.T, fixture zipFixtureFactory) {
	for stop := 1; stop <= 3; stop++ {
		t.Run(fmt.Sprint(stop), func(t *testing.T) {
			f := fixture(t, 3, nil)
			c := f.cleanup(t)
			ctx := context.Background()
			p, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
			if err != nil || p.Purge != 3 {
				t.Fatal(p, err)
			}
			crash := errors.New("synthetic purge interruption")
			calls := 0
			cleanupHook = func(stage string) error {
				if stage == "purged" {
					calls++
					if calls == stop {
						return crash
					}
				}
				return nil
			}
			t.Cleanup(func() { cleanupHook = nil })
			_, err = Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: p.Digest})
			cleanupHook = nil
			if !errors.Is(err, crash) {
				t.Fatal(err)
			}
			saved := staging.SevenZip
			staging.SevenZip = func() (string, error) { t.Fatal("partial purge rejoined its missing set"); return "", nil }
			t.Cleanup(func() { staging.SevenZip = saved })
			done, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: p.Digest})
			if err != nil || !done.Resumed || done.Purged != 3 {
				t.Fatal(done, err)
			}
			for _, name := range f.names {
				if _, err := os.Stat(filepath.Join(c.HoldingArea, "files", filepath.FromSlash(name))); !os.IsNotExist(err) {
					t.Fatal("original part retained", name, err)
				}
			}
		})
	}
}

// Rebind a synthetic durable intent to test unsupported future evidence and
// corrupt membership. It still goes through production manifest validation.
func changeStreamPurgeManifest(t *testing.T, root string, alter func(*purgeManifest)) string {
	t.Helper()
	journalPath := filepath.Join(root, ".nemalo", cleanupName)
	lines := bytes.Split(bytes.TrimSuffix(mustRead(t, journalPath), []byte{'\n'}), []byte{'\n'})
	var rec cleanupRecord
	if err := json.Unmarshal(lines[len(lines)-1], &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Event != "purge_started" {
		t.Fatal("missing pending purge", rec)
	}
	file := filepath.Join(root, filepath.FromSlash(purgeManifestName(rec.CleanupID, rec.Sequence)))
	var pm purgeManifest
	if err := json.Unmarshal(mustRead(t, file), &pm); err != nil {
		t.Fatal(err)
	}
	alter(&pm)
	pm.Digest = purgePlanDigest(pm.CleanupID, pm.Files, pm.ArchiveGroups)
	data, err := json.Marshal(pm)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	rec.Manifest = hex.EncodeToString(sum[:])
	rec.Digest = pm.Digest
	lines[len(lines)-1], err = json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, append(bytes.Join(lines, []byte{'\n'}), '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	return pm.Digest
}

func testZIPPurgeRetainsUnsupportedOrMalformedEvidence(t *testing.T, fixture zipFixtureFactory) {
	for _, kind := range []string{"unknown", "missing", "wrong_proof", "duplicate", "wrong_hash", "wrong_holdings", "planned_outcome"} {
		t.Run(kind, func(t *testing.T) {
			f := fixture(t, 3, nil)
			c := f.cleanup(t)
			ctx := context.Background()
			p, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
			if err != nil {
				t.Fatal(err)
			}
			cleanupHook = func(stage string) error {
				if stage == "purged" {
					return errors.New("crash after removal before journal")
				}
				return nil
			}
			t.Cleanup(func() { cleanupHook = nil })
			if _, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: p.Digest}); err == nil {
				t.Fatal("no interruption")
			}
			cleanupHook = nil
			digest := changeStreamPurgeManifest(t, f.root, func(pm *purgeManifest) {
				switch kind {
				case "unknown":
					pm.ArchiveGroups[0].Proof = "zip-stream/future"
				case "wrong_proof":
					pm.ArchiveGroups[0].Proof = zipStreamProof
					if f.proof() == zipStreamProof {
						pm.ArchiveGroups[0].Proof = zipSegmentsProof
					}
				case "missing":
					pm.SchemaVersion = 1
					pm.ArchiveGroups = nil
				case "duplicate":
					pm.ArchiveGroups[0].Indices = []int{0, 0, 2}
				case "wrong_hash":
					pm.Files[1].SHA256 = strings.Repeat("0", 64)
				case "wrong_holdings":
					pm.Files[1].Holdings = []string{"sha256:" + strings.Repeat("0", 64)}
				case "planned_outcome":
					pm.Files[1].Outcome = "purged"
				}
			})
			before := snapshotTree(t, filepath.Join(c.HoldingArea, "files"))
			r, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: digest})
			if kind == "unknown" || kind == "missing" || (kind == "wrong_proof" && !leafBoundArchiveProof(f.proof())) {
				if err != nil || r.Purged != 0 {
					t.Fatal(r, err)
				}
			} else if !errors.Is(err, ErrStateReview) {
				t.Fatal("malformed proof not refused", r, err)
			}
			if !reflect.DeepEqual(before, snapshotTree(t, filepath.Join(c.HoldingArea, "files"))) {
				t.Fatal("unsupported evidence removed a remaining part")
			}
		})
	}
}

func testZIPFreshPurgeRequiresEveryPartAndCurrentHolding(t *testing.T, fixture zipFixtureFactory) {
	for _, kind := range []string{"missing", "changed", "holding", "missing_tool"} {
		t.Run(kind, func(t *testing.T) {
			f := fixture(t, 3, nil)
			c := f.cleanup(t)
			switch kind {
			case "missing":
				if err := os.Remove(filepath.Join(c.HoldingArea, "files", filepath.FromSlash(f.names[1]))); err != nil {
					t.Fatal(err)
				}
			case "changed":
				if err := os.WriteFile(filepath.Join(c.HoldingArea, "files", filepath.FromSlash(f.names[1])), []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "holding":
				for p := range snapshotTree(t, filepath.Join(f.root, "assets")) {
					if err := os.Chmod(p, 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(p, []byte("changed holding"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			case "missing_tool":
				saved := staging.SevenZip
				staging.SevenZip = func() (string, error) { return "", errors.New("unavailable") }
				t.Cleanup(func() { staging.SevenZip = saved })
			}
			before := snapshotTree(t, filepath.Join(c.HoldingArea, "files"))
			p, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
			if err != nil || p.Purge != 0 || len(p.ArchiveGroups) != 0 {
				t.Fatal(p, err)
			}
			if !reflect.DeepEqual(before, snapshotTree(t, filepath.Join(c.HoldingArea, "files"))) {
				t.Fatal("incomplete proof changed recovery data")
			}
		})
	}
}

func TestZIPStreamIdentityHashBudget(t *testing.T) {
	f := newStreamFixture(t, 2, nil)
	root, err := safeio.OpenRoot(f.folder)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	remaining := int64(len(f.parts[0]))
	if _, err := fileIdentityWithin(context.Background(), root, f.names[0], &remaining); err == nil || remaining != int64(len(f.parts[0])) {
		t.Fatal("insufficient budget was read", remaining, err)
	}
	remaining++
	if _, err := fileIdentityWithin(context.Background(), root, f.names[0], &remaining); err != nil || remaining != 1 {
		t.Fatal("hash work not counted", remaining, err)
	}
	inner, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &cancelStreamHash{Context: inner, cancel: cancel}
	remaining = int64(len(f.parts[0])) + 1
	if _, err := fileIdentityWithin(ctx, root, f.names[0], &remaining); !errors.Is(err, context.Canceled) || remaining != 1 {
		t.Fatal("failed hash work was refunded", remaining, err)
	}
}

type cancelStreamHash struct {
	context.Context
	cancel context.CancelFunc
	calls  int
}

func (c *cancelStreamHash) Err() error {
	c.calls++
	if c.calls > 1 {
		c.cancel()
	}
	return c.Context.Err()
}

func testZIPPurgeBindsIndependentGroupsAtTheirActualIndices(t *testing.T, fixture zipFixtureFactory) {
	f := fixture(t, 3, nil)
	for i, part := range f.parts {
		writeTree(t, f.folder, map[string][]byte{"other/" + filepath.Base(f.names[i]): part})
	}
	ctx := context.Background()
	p, err := Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder})
	if err != nil || p.Move != 2 {
		t.Fatal(p, err)
	}
	c, err := Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest})
	if err != nil || c.Moved != 2 {
		t.Fatal(c, err)
	}
	preview, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
	if err != nil || preview.Purge != 6 || len(preview.ArchiveGroups) != 2 || !reflect.DeepEqual(preview.ArchiveGroups[1].Indices, []int{3, 4, 5}) {
		t.Fatal(preview, err)
	}
	cleanupHook = func(stage string) error {
		if stage == "purged" {
			return errors.New("interrupt before first entry journal")
		}
		return nil
	}
	t.Cleanup(func() { cleanupHook = nil })
	if _, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: preview.Digest}); err == nil {
		t.Fatal("interruption not reached")
	}
	cleanupHook = nil
	digest := changeStreamPurgeManifest(t, f.root, func(pm *purgeManifest) { pm.ArchiveGroups[0].Proof = "zip-stream/future" })
	done, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: digest})
	if err != nil || done.Purged != 3 {
		t.Fatal(done, err)
	}
	for i := 1; i < 3; i++ {
		if got := mustRead(t, filepath.Join(c.HoldingArea, "files", filepath.FromSlash(f.names[i]))); !bytes.Equal(got, f.parts[i]) {
			t.Fatal("unsupported group changed")
		}
	}
	for i := 0; i < 3; i++ {
		if _, err := os.Stat(filepath.Join(c.HoldingArea, "files", "other", filepath.Base(f.names[i]))); !os.IsNotExist(err) {
			t.Fatal("independent supported group not completed", err)
		}
	}
}

func testZIPCancelledCleanupAndStalePlansPreserveSources(t *testing.T, fixture zipFixtureFactory) {
	f := fixture(t, 3, nil)
	p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
	if err != nil {
		t.Fatal(err)
	}
	changed := filepath.Join(f.folder, filepath.FromSlash(f.names[1]))
	if err := os.WriteFile(changed, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, f.folder)
	if _, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest}); err == nil {
		t.Fatal("stale set plan applied")
	}
	if !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
		t.Fatal("stale plan changed sources")
	}
	if err := os.WriteFile(changed, f.parts[1], 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cleanupHook = func(stage string) error {
		if stage == "partial" {
			cancel()
			return ctx.Err()
		}
		return nil
	}
	t.Cleanup(func() { cleanupHook = nil })
	_, err = Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest})
	cleanupHook = nil
	if !errors.Is(err, context.Canceled) {
		t.Fatal("active cancellation ignored", err)
	}
	f.assertParts(t, f.folder)
	done, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest})
	if err != nil || !done.Resumed || done.Moved != 1 {
		t.Fatal(done, err)
	}
	f.assertParts(t, filepath.Join(done.HoldingArea, "files"))
}

func testZIPPurgeBeforeIntentRechecksAndResumesKeepChanges(t *testing.T, fixture zipFixtureFactory) {
	t.Run("unreferenced_manifest", func(t *testing.T) {
		f := fixture(t, 3, nil)
		c := f.cleanup(t)
		ctx := context.Background()
		p, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
		if err != nil {
			t.Fatal(err)
		}
		crash := errors.New("before purge intent")
		cleanupHook = func(stage string) error {
			if stage == "purge manifest" {
				return crash
			}
			return nil
		}
		t.Cleanup(func() { cleanupHook = nil })
		_, err = Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: p.Digest})
		cleanupHook = nil
		if !errors.Is(err, crash) {
			t.Fatal(err)
		}
		saved := staging.SevenZip
		staging.SevenZip = func() (string, error) { return "", errors.New("reader unavailable") }
		t.Cleanup(func() { staging.SevenZip = saved })
		if _, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: p.Digest}); err == nil {
			t.Fatal("orphan manifest authorized purge")
		}
		f.assertParts(t, filepath.Join(c.HoldingArea, "files"))
		staging.SevenZip = saved
		done, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: p.Digest})
		if err != nil || done.Resumed || done.Purged != 3 {
			t.Fatal(done, err)
		}
	})
	for _, kind := range []string{"changed_part", "changed_holding", "replacement_after_journal"} {
		t.Run(kind, func(t *testing.T) {
			f := fixture(t, 3, nil)
			c := f.cleanup(t)
			ctx := context.Background()
			p, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			stop := 1
			if kind == "replacement_after_journal" {
				stop = 2
			}
			cleanupHook = func(stage string) error {
				if stage == "purged" {
					calls++
					if calls == stop {
						return errors.New("interruption")
					}
				}
				return nil
			}
			t.Cleanup(func() { cleanupHook = nil })
			if _, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: p.Digest}); err == nil {
				t.Fatal("interruption not reached")
			}
			cleanupHook = nil
			var changed string
			switch kind {
			case "changed_part":
				changed = filepath.Join(c.HoldingArea, "files", filepath.FromSlash(f.names[1]))
				if err := os.WriteFile(changed, []byte("changed part"), 0600); err != nil {
					t.Fatal(err)
				}
			case "changed_holding":
				for n := range snapshotTree(t, filepath.Join(f.root, "assets")) {
					changed = n
					if err := os.Chmod(n, 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(n, []byte("changed holding"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			case "replacement_after_journal":
				changed = filepath.Join(c.HoldingArea, "files", filepath.FromSlash(f.names[0]))
				if err := os.WriteFile(changed, []byte("unowned replacement"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			canary := mustRead(t, changed)
			done, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: p.Digest})
			if err != nil || !done.Resumed {
				t.Fatal(done, err)
			}
			if !bytes.Equal(mustRead(t, changed), canary) {
				t.Fatal("resume changed unrelated bytes")
			}
			if kind == "changed_part" && (done.Purged != 2 || purgeDispositions(done)[f.names[1]] != "held_copy_changed") {
				t.Fatal(done)
			}
			if kind == "changed_holding" {
				for _, n := range f.names[1:] {
					if _, err := os.Stat(filepath.Join(c.HoldingArea, "files", filepath.FromSlash(n))); err != nil {
						t.Fatal("holding no longer verifies but source removed", n, err)
					}
				}
			}
			if kind == "replacement_after_journal" && done.Purged != 3 {
				t.Fatal(done)
			}
		})
	}
}
