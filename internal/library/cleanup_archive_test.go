package library

import (
	"context"
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

// A RAR whose members are all held moves only when 7-Zip lists it whole:
// no tail, comment, or offset, and a physical size equal to the file.
func TestCleanupMovesAWholeHeldRAROnly(t *testing.T) {
	ctx := context.Background()
	rar := []byte("Rar!\x1a\x07\x01\x00 an archive")
	root, folder, member := archiveCleanup(t, map[string][]byte{"books.rar": rar})
	whole := fmt.Sprintf("Path = books.rar\nType = Rar5\nPhysical Size = %d\nMultivolume = -\nVolumes = 1\n", len(rar))
	for name, header := range map[string]string{
		"tail":    fmt.Sprintf("Path = books.rar\nType = Rar5\nWARNINGS:\nThere are data after the end of archive\nPhysical Size = %d\nTail Size = 4\n", len(rar)-4),
		"comment": whole + "Comment = notes\n",
		"short":   strings.Replace(whole, fmt.Sprint(len(rar)), fmt.Sprint(len(rar)-1), 1),
	} {
		stagingtest.Install(t, stagingtest.Archive{Kind: "-tRar5", Header: header, Members: map[string][]byte{"book.epub": member}})
		plan, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
		if e := entryFor(t, plan, "books.rar"); err != nil || e.Disposition != "keep" || !strings.Contains(e.Reason, "beyond the archive") {
			t.Fatal(name, e, err)
		}
	}
	stagingtest.Install(t, stagingtest.Archive{Kind: "-tRar5", Header: whole, Members: map[string][]byte{"book.epub": member, "other.txt": []byte("x")}})
	if plan, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder}); err != nil || entryFor(t, plan, "books.rar").Disposition != "keep" {
		t.Fatal("an archive with an unheld member would move", err)
	}
	stagingtest.Install(t, stagingtest.Archive{Kind: "-tRar5", Header: whole, Members: map[string][]byte{"book.epub": member}})
	plan, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	if e := entryFor(t, plan, "books.rar"); err != nil || e.Disposition != "move" || e.Kind != "archive" || len(e.Holdings) != 1 {
		t.Fatal(e, err)
	}
	applied, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder, Apply: plan.Digest})
	if err != nil || applied.Moved != 1 {
		t.Fatal(applied, err)
	}
	if _, err := os.Stat(filepath.Join(folder, "books.rar")); !errors.Is(err, os.ErrNotExist) {
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
	v1, v2 := []byte("7z\xbc\xaf\x27\x1c first"), []byte(" second")
	root, folder, member := archiveCleanup(t, map[string][]byte{"set.7z.001": v1, "set.7z.002": v2, "gap.7z.001": v1, "gap.7z.003": v2})
	stagingtest.Install(t, stagingtest.Archive{Kind: "-t7z", Header: fmt.Sprintf("Path = archive.7z\nType = 7z\nPhysical Size = %d\n", len(v1)+len(v2)), Members: map[string][]byte{"book.epub": member}})
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
	if string(mustRead(t, filepath.Join(folder, "set.7z.002"))) != string(v2) {
		t.Fatal("the second volume was not restored")
	}
}
