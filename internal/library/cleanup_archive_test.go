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
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/staging"
	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
)

// archiveCleanup is a library holding one book and a folder of archives
// whose only member is that book.
func archiveCleanup(t *testing.T, files map[string][]byte) (root, folder string, member []byte) {
	t.Helper()
	ctx := context.Background()
	root, folder = t.TempDir(), t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	book := writeEPUBParagraph(t, t.TempDir(), "Archived")
	if _, err := Import(ctx, ImportRequest{Library: root, Source: book, Apply: true}); err != nil {
		t.Fatal(err)
	}
	writeTree(t, folder, files)
	return root, folder, mustRead(t, book)
}

func entryFor(t *testing.T, plan CleanupResult, path string) CleanupEntry {
	t.Helper()
	for _, e := range plan.Entries {
		if e.Path == path {
			return e
		}
	}
	t.Fatalf("no entry for %s in %+v", path, plan.Entries)
	return CleanupEntry{}
}

// A 7z whose members are all held moves only when 7-Zip lists it whole: no
// tail, comment, or offset, a physical size equal to the file, and a header
// and packed streams that fill it. A RAR never moves.
func TestCleanupMovesAWholeHeld7zOnly(t *testing.T) {
	ctx := context.Background()
	// The stand-in's bytes need only be longer than the member they list.
	archive := append([]byte("7z\xbc\xaf\x27\x1c"), make([]byte, 64<<10)...)
	root, folder, member := archiveCleanup(t, map[string][]byte{"books.7z": archive, "books.rar": []byte("Rar!\x1a\x07\x01\x00 an archive")})
	n := len(archive)
	whole := fmt.Sprintf("Path = books.7z\nType = 7z\nPhysical Size = %d\nHeaders Size = %d\n", n, n-len(member))
	for name, header := range map[string]string{
		"tail":    fmt.Sprintf("Path = books.7z\nType = 7z\nWARNINGS:\nThere are data after the end of archive\nPhysical Size = %d\nTail Size = 4\n", n-4),
		"comment": whole + "Comment = notes\n",
		"short":   strings.Replace(whole, fmt.Sprintf("Physical Size = %d", n), fmt.Sprintf("Physical Size = %d", n-1), 1),
		"gap":     strings.Replace(whole, fmt.Sprintf("Headers Size = %d", n-len(member)), fmt.Sprintf("Headers Size = %d", n-len(member)-9), 1),
	} {
		stagingtest.Install(t, stagingtest.Archive{Kind: "-t7z", Header: header, Members: map[string][]byte{"book.epub": member}})
		plan, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
		if e := entryFor(t, plan, "books.7z"); err != nil || e.Disposition != "keep" || !strings.Contains(e.Reason, "beyond the archive") {
			t.Fatal(name, e, err)
		}
	}
	stagingtest.Install(t, stagingtest.Archive{Kind: "-t7z", Header: whole, Members: map[string][]byte{"book.epub": member, "other.txt": []byte("x")}})
	if plan, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder}); err != nil || entryFor(t, plan, "books.7z").Disposition != "keep" {
		t.Fatal("an archive with an unheld member would move", err)
	}
	stagingtest.Install(t, stagingtest.Archive{Kind: "-tRar5", Header: fmt.Sprintf("Path = books.rar\nType = Rar5\nPhysical Size = 26\nHeaders Size = %d\n", 26-len(member)), Members: map[string][]byte{"book.epub": member}})
	if plan, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder}); err != nil || !strings.Contains(entryFor(t, plan, "books.rar").Reason, "a RAR can hold") {
		t.Fatal("a RAR would move", err)
	}
	stagingtest.Install(t, stagingtest.Archive{Kind: "-t7z", Header: whole, Members: map[string][]byte{"book.epub": member}})
	plan, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	if e := entryFor(t, plan, "books.7z"); err != nil || e.Disposition != "move" || e.Kind != "archive" || len(e.Holdings) != 1 {
		t.Fatal(e, err)
	}
	applied, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest})
	if err != nil || applied.Moved != 1 {
		t.Fatal(applied, err)
	}
	if _, err := os.Stat(filepath.Join(folder, "books.7z")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the archive stayed")
	}
	saved := staging.SevenZip
	staging.SevenZip = func() (string, error) { return "", errors.New("missing") }
	defer func() { staging.SevenZip = saved }()
	other := t.TempDir()
	writeTree(t, other, map[string][]byte{"more.7z": []byte("7z\xbc\xaf\x27\x1c rest")})
	if plan, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: other}); err != nil || !strings.Contains(entryFor(t, plan, "more.7z").Reason, "not installed") {
		t.Fatal(plan, err)
	}
}

// A split 7z set moves as one entry holding every volume, only when 7-Zip
// lists the joined volumes whole; a set missing a volume keeps them all.
func TestCleanupMovesAVolumeSetAsOne(t *testing.T) {
	ctx := context.Background()
	v1, v2 := append([]byte("7z\xbc\xaf\x27\x1c"), make([]byte, 32<<10)...), make([]byte, 32<<10)
	root, folder, member := archiveCleanup(t, map[string][]byte{"set.7z.001": v1, "set.7z.002": v2, "gap.7z.001": v1, "gap.7z.003": v2})
	stagingtest.Install(t, stagingtest.Archive{Kind: "-t7z", Header: fmt.Sprintf("Path = archive.7z\nType = 7z\nPhysical Size = %d\nHeaders Size = %d\n", len(v1)+len(v2), len(v1)+len(v2)-len(member)), Members: map[string][]byte{"book.epub": member}})
	plan, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	if err != nil {
		t.Fatal(err)
	}
	set := entryFor(t, plan, "set.7z.001")
	if set.Disposition != "move" || len(set.Files) != 2 || set.Files[1].Path != "set.7z.002" {
		t.Fatalf("%+v", set)
	}
	for _, e := range plan.Entries {
		if e.Path == "set.7z.002" {
			t.Fatal("a moving set's other volume was planned on its own")
		}
	}
	if e := entryFor(t, plan, "gap.7z.003"); e.Disposition != "keep" || !strings.Contains(e.Reason, "missing volume") {
		t.Fatal(e)
	}
	applied, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest})
	if err != nil || applied.Moved != 1 {
		t.Fatal(applied, err)
	}
	for _, gone := range []string{"set.7z.001", "set.7z.002"} {
		if _, err := os.Stat(filepath.Join(folder, gone)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal(gone, "stayed")
		}
	}
	restored, err := Restore(ctx, RestoreRequest{Library: root, CleanupID: applied.CleanupID, Apply: true})
	if err != nil {
		t.Fatal(restored, err)
	}
	if !bytes.Equal(mustRead(t, filepath.Join(folder, "set.7z.002")), v2) {
		t.Fatal("the second volume was not restored")
	}
}

// A ZIP with bytes between its entries and its directory, or a directory
// entry that holds data, stays: those bytes were never checked.
func TestCleanupKeepsAZIPWithUncheckedBytes(t *testing.T) {
	ctx := context.Background()
	book := mustRead(t, writeEPUBParagraph(t, t.TempDir(), "Archived"))
	build := func(entries map[string][]byte) []byte {
		var buf bytes.Buffer
		z := zip.NewWriter(&buf)
		for name, body := range entries {
			w, err := z.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write(body); err != nil {
				t.Fatal(err)
			}
		}
		if err := z.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	clean := build(map[string][]byte{"book.epub": book})
	// Insert hidden bytes before the central directory and move its offset.
	n := len(clean)
	offset := int(binary.LittleEndian.Uint32(clean[n-6:]))
	hidden := []byte("hidden bytes nobody listed")
	gapped := append(append(append([]byte{}, clean[:offset]...), hidden...), clean[offset:]...)
	binary.LittleEndian.PutUint32(gapped[len(gapped)-6:], uint32(offset+len(hidden)))
	if _, err := zip.NewReader(bytes.NewReader(gapped), int64(len(gapped))); err != nil {
		t.Fatal("the crafted archive is not a readable ZIP", err)
	}
	// Go's ZIP reader accepts the requested entry count even if the declared
	// directory extends over arbitrary bytes before the end record.
	directoryGap := append(append(append([]byte{}, clean[:n-22]...), hidden...), clean[n-22:]...)
	dirSize := binary.LittleEndian.Uint32(clean[n-10:])
	binary.LittleEndian.PutUint32(directoryGap[len(directoryGap)-10:], dirSize+uint32(len(hidden)))
	if z, err := zip.NewReader(bytes.NewReader(directoryGap), int64(len(directoryGap))); err != nil || len(z.File) != 1 {
		t.Fatal("directory-gap fixture is not a readable one-member ZIP", err)
	}
	root, folder, _ := archiveCleanup(t, map[string][]byte{"clean.zip": clean, "gapped.zip": gapped, "directory-gap.zip": directoryGap, "dir.zip": bytes.ReplaceAll(build(map[string][]byte{"book.epub": book, "hiddenx": book}), []byte("hiddenx"), []byte("hidden/"))})
	plan, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	if err != nil {
		t.Fatal(err)
	}
	if e := entryFor(t, plan, "clean.zip"); e.Disposition != "move" {
		t.Fatal(e)
	}
	if e := entryFor(t, plan, "gapped.zip"); e.Disposition != "keep" || !strings.Contains(e.Reason, "outside its entries") {
		t.Fatal(e)
	}
	if e := entryFor(t, plan, "directory-gap.zip"); e.Disposition != "keep" || !strings.Contains(e.Reason, "outside its entries") {
		t.Fatal("unaccounted directory bytes authorized cleanup", e)
	}
	if e := entryFor(t, plan, "dir.zip"); e.Disposition != "keep" || !strings.Contains(e.Reason, "hidden/") {
		t.Fatal(e)
	}
}
