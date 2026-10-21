package library

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/blisspixel/nemalo/internal/safeio"
	"github.com/blisspixel/nemalo/internal/staging"
	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
)

func cleanupRAR5SetRecord(kind, flags uint64, body, data []byte) []byte {
	h := cleanupRAR5Numbers(kind, flags)
	if flags&2 != 0 {
		h = append(h, cleanupRAR5Numbers(uint64(len(data)))...)
	}
	h = append(h, body...)
	h = append(cleanupRAR5Numbers(uint64(len(h))), h...)
	return append(append(binary.LittleEndian.AppendUint32(nil, crc32.ChecksumIEEE(h)), h...), data...)
}

func TestRAR5SetReadOnlyCoverageCannotAuthorizeCleanup(t *testing.T) {
	f := newRAR5CleanupFixture(t, nil)
	var book []byte
	for _, b := range snapshotTree(t, filepath.Join(f.root, "assets")) {
		book = b
	}
	if len(book) == 0 {
		t.Fatal("holding missing")
	}
	if err := os.Remove(filepath.Join(f.folder, filepath.FromSlash(f.names[0]))); err != nil {
		t.Fatal(err)
	}
	a := stagingtest.Archive{Kind: "-tRar5", Members: map[string][]byte{"book.epub": book}, Fields: map[string]string{"book.epub": "Split Before = -\nSplit After = -\nVolume Index = 0\n"}}
	var names []string
	var total, first int
	for i := range 3 {
		piece := book[i*len(book)/3 : (i+1)*len(book)/3]
		main := cleanupRAR5Numbers(1)
		if i > 0 {
			main = cleanupRAR5Numbers(3, uint64(i))
		}
		b := append([]byte("Rar!\x1a\x07\x01\x00"), cleanupRAR5SetRecord(1, 0, main, nil)...)
		flags := uint64(2)
		before, after := "-", "-"
		if i > 0 {
			flags |= 8
			before = "+"
		}
		if i < 2 {
			flags |= 16
			after = "+"
		}
		crc := crc32.ChecksumIEEE(book)
		if i < 2 {
			crc = crc32.ChecksumIEEE(piece)
		}
		fields := cleanupRAR5Numbers(4, uint64(len(book)), 0100644)
		fields = binary.LittleEndian.AppendUint32(fields, crc)
		fields = append(fields, cleanupRAR5Numbers(0, 1, uint64(len("book.epub")))...)
		fields = append(fields, "book.epub"...)
		b = append(b, cleanupRAR5SetRecord(2, flags, fields, piece)...)
		end := uint64(0)
		if i < 2 {
			end = 1
		}
		b = append(b, cleanupRAR5SetRecord(5, 0, cleanupRAR5Numbers(end), nil)...)
		name := fmt.Sprintf("books/set.part%d.rar", i+1)
		names = append(names, filepath.FromSlash(name))
		writeTree(t, f.folder, map[string][]byte{name: b})
		total += len(b)
		if i == 0 {
			first = len(b)
		}
		header := fmt.Sprintf("Type = Rar5\nPhysical Size = %d\nMultivolume = +\nVolume Index = %d\nVolumes = 1\n", len(b), i)
		if i < 2 {
			header += fmt.Sprintf("ERROR = Missing volume : archive.part%04d.rar\n", i+2)
		}
		if i > 0 {
			header += "ERRORS:\nHeaders Error\n"
		}
		listing := fmt.Sprintf("Path = book.epub\nSize = %d\nPacked Size = %d\nAttributes = -rw-r--r--\nCRC = %08X\nSplit Before = %s\nSplit After = %s\nVolume Index = %d\n\n", len(book), len(piece), crc, before, after, i)
		a.Volumes = append(a.Volumes, stagingtest.Volume{Name: fmt.Sprintf("archive.part%04d.rar", i+1), Body: b, Header: header, Listing: listing, Code: 2})
	}
	a.Header = fmt.Sprintf("Type = Rar5\nPhysical Size = %d\nTotal Physical Size = %d\nMultivolume = +\nVolume Index = 0\nVolumes = 3\n", first, total)
	stagingtest.Install(t, a)
	src, err := safeio.OpenRoot(f.folder)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := safeio.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	l := staging.Defaults()
	l.Coverage, l.RAR5SetCoverage = true, true
	x, err := staging.ExtractTree(context.Background(), src, names, dst, func(name string) bool { return name == "book.epub" }, l)
	if err != nil || !x.Whole || !x.Complete || x.RARSetVersion != 5 || x.RARVersion != 0 || x.RARContainers != 0 {
		t.Fatal("read-only prerequisite failed", x, err)
	}
	if got, err := dst.ReadFile(x.Members[0].Staged); err != nil || !bytes.Equal(got, book) {
		t.Fatal("staged holding differs", err)
	}
	before, held := snapshotTree(t, f.folder), snapshotTree(t, filepath.Join(f.root, "assets"))
	saved := staging.SevenZip
	staging.SevenZip = func() (string, error) { t.Fatal("RAR set cleanup reached the reader"); return "", nil }
	t.Cleanup(func() { staging.SevenZip = saved })
	p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
	if err != nil || p.Move != 0 {
		t.Fatal("read-only set capability granted write permission", p, err)
	}
	if applied, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest}); err != nil || applied.Moved != 0 {
		t.Fatal(applied, err)
	}
	if !reflect.DeepEqual(before, snapshotTree(t, f.folder)) || !reflect.DeepEqual(held, snapshotTree(t, filepath.Join(f.root, "assets"))) {
		t.Fatal("read-only set capability changed sources or held bytes")
	}
}
