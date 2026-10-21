package library

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/blisspixel/nemalo/internal/staging"
	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
)

func cleanupRAR4File(name string, book []byte) []byte {
	b := binary.LittleEndian.AppendUint32(nil, uint32(len(book)))
	b = binary.LittleEndian.AppendUint32(b, uint32(len(book)))
	b = append(b, 3)
	b = binary.LittleEndian.AppendUint32(b, crc32.ChecksumIEEE(book))
	b = binary.LittleEndian.AppendUint32(b, 0)
	b = append(b, 20, 0x30)
	b = binary.LittleEndian.AppendUint16(b, uint16(len(name)))
	b = binary.LittleEndian.AppendUint32(b, 0100644)
	b = append(b, name...)
	return cleanupRAR4Block(0x74, 0x8000, b, book)
}

func cleanupRAR4Archive(records ...[]byte) []byte {
	b := append([]byte("Rar!\x1a\x07\x00"), cleanupRAR4Block(0x73, 0, make([]byte, 6), nil)...)
	for _, record := range records {
		b = append(b, record...)
	}
	return append(b, cleanupRAR4Block(0x7b, 0, nil, nil)...)
}

func newRAR4CleanupFixture(t *testing.T, build func([]byte) []byte) streamFixture {
	t.Helper()
	ctx := context.Background()
	f := streamFixture{root: t.TempDir(), folder: t.TempDir(), archiveProof: "rar4-frames/1"}
	if _, err := Initialize(ctx, f.root); err != nil {
		t.Fatal(err)
	}
	source := writeEPUBParagraph(t, t.TempDir(), "Standalone RAR4 cleanup")
	if _, err := Import(ctx, ImportRequest{Library: f.root, Source: source, Apply: true, Scan: true, Scanner: cleanScan{}}); err != nil {
		t.Fatal(err)
	}
	book := mustRead(t, source)
	data := cleanupRAR4Archive(cleanupRAR4File("book.epub", book))
	if build != nil {
		data = build(book)
	}
	f.names, f.parts = []string{"books/one.rar"}, [][]byte{data}
	writeTree(t, f.folder, map[string][]byte{f.names[0]: data})
	stagingtest.Install(t, stagingtest.Archive{Kind: "-tRar", Body: data, Members: map[string][]byte{"book.epub": book}})
	return f
}

func TestStandaloneRAR4CleanupBindsExactLeafAndPurge(t *testing.T) {
	testStandaloneRARCleanupBindsExactLeafAndPurge(t, newRAR4CleanupFixture)
}

func TestStandaloneRAR4CleanupAndRestoreInterruptions(t *testing.T) {
	testSingleArchiveCleanupAndRestoreInterruptions(t, func(t *testing.T) streamFixture { return newRAR4CleanupFixture(t, nil) })
}

func TestStandaloneRAR4PendingPurgeValidatesBindingsBeforeRemoval(t *testing.T) {
	testStandaloneRARPendingPurgeValidatesBindingsBeforeRemoval(t, newRAR4CleanupFixture)
}

func TestStandaloneRAR4PurgeRecoveryChecksFreshHoldingsAndRemainingBytes(t *testing.T) {
	testStandaloneRARPurgeRecoveryChecksFreshHoldingsAndRemainingBytes(t, newRAR4CleanupFixture)
}

func TestStandaloneRAR4FreshPurgeRequiresCurrentEvidence(t *testing.T) {
	testStandaloneRARFreshPurgeRequiresCurrentEvidence(t, newRAR4CleanupFixture)
}

func TestStandaloneRAR4CleanupResumeRejectsInvalidProofBeforeWrites(t *testing.T) {
	testStandaloneRARCleanupResumeRejectsInvalidProofBeforeWrites(t, newRAR4CleanupFixture)
}

func TestStandaloneRAR4IndependentGroupsValidateBeforeAnyRemoval(t *testing.T) {
	testStandaloneRARIndependentGroupsValidateBeforeAnyRemoval(t, newRAR4CleanupFixture)
}

func TestStandaloneRAR4MetadataAndUnsupportedSources(t *testing.T) {
	for _, kind := range []string{"high_zero", "time", "both", "empty_directory", "subblock", "old_subblock", "tail", "unicode", "volume", "directory_data", "unknown_time", "high_nonzero"} {
		t.Run(kind, func(t *testing.T) {
			f := newRAR4CleanupFixture(t, func(book []byte) []byte {
				record := cleanupRAR4File("book.epub", book)
				header := bytes.Clone(record[:32])
				flags := uint16(0x8000)
				if kind == "high_zero" || kind == "both" || kind == "high_nonzero" {
					flags |= 0x100
					header = append(header, make([]byte, 8)...)
					if kind == "high_nonzero" {
						header[32] = 1
					}
				}
				header = append(header, "book.epub"...)
				if kind == "time" || kind == "both" || kind == "unknown_time" {
					flags |= 0x1000
					mask := uint16(0xb000)
					if kind == "unknown_time" {
						mask++
					}
					header = binary.LittleEndian.AppendUint16(header, mask)
					header = append(header, 0x7f, 0x96, 0x98)
				}
				if kind == "unicode" {
					flags |= 0x200
				}
				if kind == "volume" {
					flags |= 2
				}
				if kind == "directory_data" {
					flags |= 0xe0
					binary.LittleEndian.PutUint32(header[28:32], 0040755)
				}
				binary.LittleEndian.PutUint16(header[3:5], flags)
				binary.LittleEndian.PutUint16(header[5:7], uint16(len(header)))
				binary.LittleEndian.PutUint16(header, uint16(crc32.ChecksumIEEE(header[2:])))
				record = append(header, book...)
				records := [][]byte{record}
				if kind == "empty_directory" {
					dir := cleanupRAR4File("folder", nil)
					binary.LittleEndian.PutUint16(dir[3:5], 0x80e0)
					binary.LittleEndian.PutUint32(dir[28:32], 0040755)
					binary.LittleEndian.PutUint16(dir, uint16(crc32.ChecksumIEEE(dir[2:])))
					records = append(records, dir)
				}
				if kind == "subblock" || kind == "old_subblock" {
					block := byte(0x7a)
					if kind == "old_subblock" {
						block = 0x77
					}
					records = append(records, cleanupRAR4Block(block, 0, []byte("retained notes"), nil))
				}
				data := cleanupRAR4Archive(records...)
				if kind == "tail" {
					data = append(data, "retained tail"...)
				}
				return data
			})
			if kind == "empty_directory" {
				assets := snapshotTree(t, filepath.Join(f.root, "assets"))
				var book []byte
				for _, b := range assets {
					book = b
				}
				stagingtest.Install(t, stagingtest.Archive{Kind: "-tRar", Body: f.parts[0], Members: map[string][]byte{"book.epub": book, "folder": nil}, Attributes: map[string]string{"folder": "D drwxr-xr-x"}})
			}
			before := snapshotTree(t, f.folder)
			p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
			positive := kind == "high_zero" || kind == "time" || kind == "both" || kind == "empty_directory"
			want := 0
			if positive {
				want = 1
			}
			if err != nil || p.Move != want || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
				t.Fatal(p, err)
			}
			if positive && p.Entries[0].ArchiveProof != "rar4-frames/1" {
				t.Fatal("RAR4 borrowed another proof", p)
			}
		})
	}
}

func TestStandaloneRAR4RequiresEveryLeafHeld(t *testing.T) {
	for _, held := range []bool{false, true} {
		t.Run(fmt.Sprint(held), func(t *testing.T) {
			var book, second []byte
			f := newRAR4CleanupFixture(t, func(b []byte) []byte {
				book, second = b, b
				if !held {
					second = mustRead(t, writeEPUBParagraph(t, t.TempDir(), "Unheld RAR4 book"))
				}
				return cleanupRAR4Archive(cleanupRAR4File("book.epub", book), cleanupRAR4File("second.epub", second))
			})
			stagingtest.Install(t, stagingtest.Archive{Kind: "-tRar", Body: f.parts[0], Members: map[string][]byte{"book.epub": book, "second.epub": second}})
			p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
			want := 0
			if held {
				want = 1
			}
			if err != nil || p.Move != want || held && (len(p.Entries[0].ArchiveLeaves) != 2 || !supportedArchiveProof(p.Entries[0])) {
				t.Fatal(p, err)
			}
		})
	}
}

func TestStandaloneRAR4RestoreRequiresNoReaderAndNeverReplaces(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(fmt.Sprint(replacement), func(t *testing.T) {
			f := newRAR4CleanupFixture(t, nil)
			c := f.cleanup(t)
			saved := staging.SevenZip
			staging.SevenZip = func() (string, error) { t.Fatal("restore invoked archive reader"); return "", nil }
			t.Cleanup(func() { staging.SevenZip = saved })
			canary := []byte("unrelated replacement source")
			if replacement {
				writeTree(t, f.folder, map[string][]byte{f.names[0]: canary})
			}
			r, err := Restore(context.Background(), RestoreRequest{Library: f.root, CleanupID: c.CleanupID, Apply: true})
			if err != nil || r.Restore != map[bool]int{true: 0, false: 1}[replacement] {
				t.Fatal(r, err)
			}
			if replacement {
				if !bytes.Equal(mustRead(t, filepath.Join(f.folder, filepath.FromSlash(f.names[0]))), canary) {
					t.Fatal("restore replaced unrelated file")
				}
				f.assertParts(t, filepath.Join(c.HoldingArea, "files"))
			} else {
				f.assertParts(t, f.folder)
			}
		})
	}
}

func TestStandaloneRAR4PurgeInterruptions(t *testing.T) {
	for _, stage := range []string{"purge manifest", "purged"} {
		t.Run(stage, func(t *testing.T) {
			f := newRAR4CleanupFixture(t, nil)
			c := f.cleanup(t)
			ctx := context.Background()
			p, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
			if err != nil {
				t.Fatal(err)
			}
			crash := errors.New("RAR4 purge interruption")
			cleanupHook = func(s string) error {
				if s == stage {
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
			r, err := Purge(ctx, PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: p.Digest})
			if err != nil || r.Purged != 1 || stage == "purged" && !r.Resumed {
				t.Fatal(r, err)
			}
			if _, err := os.Stat(filepath.Join(c.HoldingArea, "files", filepath.FromSlash(f.names[0]))); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("purge did not remove checked source", err)
			}
		})
	}
}

func TestStandaloneRAR4ScopeRetainsVolumesAndNestedSources(t *testing.T) {
	for _, kind := range []string{"modern_set", "old_set", "nested_root", "child"} {
		t.Run(kind, func(t *testing.T) {
			var book []byte
			f := newRAR4CleanupFixture(t, func(b []byte) []byte {
				book = b
				return cleanupRAR4Archive(cleanupRAR4File("book.epub", b))
			})
			if err := os.Remove(filepath.Join(f.folder, filepath.FromSlash(f.names[0]))); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "modern_set", "old_set":
				first, second := "books/set.part1.rar", "books/set.part2.rar"
				if kind == "old_set" {
					first, second = "books/set.rar", "books/set.r00"
				}
				writeTree(t, f.folder, map[string][]byte{first: f.parts[0], second: []byte("synthetic continuation")})
				saved := staging.SevenZip
				staging.SevenZip = func() (string, error) { t.Fatal("unsupported set reached reader"); return "", nil }
				t.Cleanup(func() { staging.SevenZip = saved })
			case "nested_root":
				inner := nestedZIP(t, nestedFile{"book.epub", book})
				outer := cleanupRAR4Archive(cleanupRAR4File("inside.zip", inner))
				writeTree(t, f.folder, map[string][]byte{f.names[0]: outer})
				stagingtest.InstallMany(t, stagingtest.Archive{Kind: "-tRar", Body: outer, Members: map[string][]byte{"inside.zip": inner}}, stagingtest.Archive{Kind: "-tzip", Body: inner})
			case "child":
				outer := nestedZIP(t, nestedFile{"inside.rar", f.parts[0]})
				writeTree(t, f.folder, map[string][]byte{"books/outer.zip": outer})
				stagingtest.InstallMany(t, stagingtest.Archive{Kind: "-tRar", Body: f.parts[0], Members: map[string][]byte{"book.epub": book}})
			}
			before := snapshotTree(t, f.folder)
			p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
			if err != nil || p.Move != 0 || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
				t.Fatal("unsupported RAR4 source moved", p, err)
			}
		})
	}
}
