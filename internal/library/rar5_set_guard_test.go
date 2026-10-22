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

func newRAR5SetCleanupFixture(t *testing.T, count int, build func([]byte) []byte) streamFixture {
	if build == nil {
		return newRAR5SetCleanupWithChild(t, count, "book.epub", nil)
	}
	return newRAR5SetCleanupWithChild(t, count, "inside.zip", func(book []byte) ([]byte, []stagingtest.Archive) {
		body := build(book)
		return body, []stagingtest.Archive{{Kind: "-tzip", Body: body}}
	})
}

func newRAR5SetCleanupWithChild(t *testing.T, count int, name string, build func([]byte) ([]byte, []stagingtest.Archive), alter ...func(int, []byte) []byte) streamFixture {
	t.Helper()
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
	var children []stagingtest.Archive
	if build != nil {
		book, children = build(book)
	}
	a := stagingtest.Archive{Kind: "-tRar5", Members: map[string][]byte{name: book}, Fields: map[string]string{name: "Split Before = -\nSplit After = -\nVolume Index = 0\n"}}
	f.names, f.parts, f.archiveProof = nil, nil, rar5SetProof
	var total, first int
	for i := range count {
		piece := book[i*len(book)/count : (i+1)*len(book)/count]
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
		if i+1 < count {
			flags |= 16
			after = "+"
		}
		crc := crc32.ChecksumIEEE(book)
		if i+1 < count {
			crc = crc32.ChecksumIEEE(piece)
		}
		fields := cleanupRAR5Numbers(4, uint64(len(book)), 0100644)
		fields = binary.LittleEndian.AppendUint32(fields, crc)
		fields = append(fields, cleanupRAR5Numbers(0, 1, uint64(len(name)))...)
		fields = append(fields, name...)
		b = append(b, cleanupRAR5SetRecord(2, flags, fields, piece)...)
		end := uint64(0)
		if i+1 < count {
			end = 1
		}
		b = append(b, cleanupRAR5SetRecord(5, 0, cleanupRAR5Numbers(end), nil)...)
		for _, change := range alter {
			b = change(i, b)
		}
		partName := fmt.Sprintf("books/set.part%d.rar", i+1)
		f.names, f.parts = append(f.names, partName), append(f.parts, b)
		writeTree(t, f.folder, map[string][]byte{partName: b})
		total += len(b)
		if i == 0 {
			first = len(b)
		}
		header := fmt.Sprintf("Type = Rar5\nPhysical Size = %d\nMultivolume = +\nVolume Index = %d\nVolumes = 1\n", len(b), i)
		if i+1 < count {
			header += fmt.Sprintf("ERROR = Missing volume : archive.part%04d.rar\n", i+2)
		}
		if i > 0 {
			header += "ERRORS:\nHeaders Error\n"
		}
		listing := fmt.Sprintf("Path = %s\nSize = %d\nPacked Size = %d\nAttributes = -rw-r--r--\nCRC = %08X\nSplit Before = %s\nSplit After = %s\nVolume Index = %d\n\n", name, len(book), len(piece), crc, before, after, i)
		a.Volumes = append(a.Volumes, stagingtest.Volume{Name: fmt.Sprintf("archive.part%04d.rar", i+1), Body: b, Header: header, Listing: listing, Code: 2})
	}
	a.Header = fmt.Sprintf("Type = Rar5\nPhysical Size = %d\nTotal Physical Size = %d\nMultivolume = +\nVolume Index = 0\nVolumes = %d\n", first, total, count)
	f.archives = append(children, a)
	stagingtest.InstallMany(t, f.archives...)
	return f
}

func TestRAR5SetCoverageRequiresDistinctCleanupAuthority(t *testing.T) {
	testRARSetCoverageRequiresDistinctCleanupAuthority(t, 5, newRAR5SetCleanupFixture)
}

func testRARSetCoverageRequiresDistinctCleanupAuthority(t *testing.T, version int, fixture zipFixtureFactory) {
	t.Helper()
	f := fixture(t, 3, nil)
	var book []byte
	for _, b := range snapshotTree(t, filepath.Join(f.root, "assets")) {
		book = b
	}
	var names []string
	for _, name := range f.names {
		names = append(names, filepath.FromSlash(name))
	}
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
	l.Coverage = true
	l.RAR5SetCoverage = version == 5
	l.RAR4Coverage, l.RAR4SetCoverage = version == 4, version == 4
	x, err := staging.ExtractTree(context.Background(), src, names, dst, func(name string) bool { return name == "book.epub" }, l)
	if err != nil || !x.Whole || !x.Complete || x.RARSetVersion != version || x.RARVersion != 0 || x.RARContainers != 0 || len(x.Members) != 1 {
		t.Fatal("read-only prerequisite failed", x, err)
	}
	if got, err := dst.ReadFile(x.Members[0].Staged); err != nil || !bytes.Equal(got, book) {
		t.Fatal("staged holding differs", err)
	}
	before, held := snapshotTree(t, f.folder), snapshotTree(t, filepath.Join(f.root, "assets"))
	p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
	if err != nil || p.Move != 1 || len(p.Entries) != 1 || p.Entries[0].ArchiveProof != f.proof() || !supportedArchiveProof(p.Entries[0]) {
		t.Fatal("set has no distinct complete authority", p, err)
	}
	for _, proof := range []string{archiveTreeProof, archiveRARTreeProof, rar4FramesProof, rar5FramesProof, zipStreamProof, zipSegmentsProof, "", fmt.Sprintf("rar%d-set/2", version)} {
		e := p.Entries[0]
		e.ArchiveProof = proof
		if supportedArchiveProof(e) {
			t.Fatal("RAR outer set borrowed older or unknown authority", proof, e)
		}
	}
	for _, kind := range []string{"single", "ordinal", "parent", "family", "zero_bytes", "part_limit", "total_limit", "wrong_kind", "first_path"} {
		e := p.Entries[0]
		e.Files = append([]CleanupFile{}, e.Files...)
		switch kind {
		case "single":
			e.Files = e.Files[:1]
		case "ordinal":
			e.Files[1].Path = "books/set.part3.rar"
		case "parent":
			e.Files[1].Path = "Books/set.part2.rar"
		case "family":
			e.Files[1].Path = "books/other.part2.rar"
		case "zero_bytes":
			e.Files[1].Bytes = 0
		case "part_limit":
			e.Files[1].Bytes = (256 << 20) + 1
		case "total_limit":
			e.Files = nil
			for i := range 17 {
				e.Files = append(e.Files, CleanupFile{Path: fmt.Sprintf("books/set.part%d.rar", i+1), Bytes: 256 << 20})
			}
		case "wrong_kind":
			e.Kind = "file"
		case "first_path":
			e.Path = "books/other.part1.rar"
		}
		if supportedArchiveProof(e) {
			t.Fatal("malformed set authority accepted", kind, e)
		}
	}
	if !reflect.DeepEqual(before, snapshotTree(t, f.folder)) || !reflect.DeepEqual(held, snapshotTree(t, filepath.Join(f.root, "assets"))) {
		t.Fatal("preview changed sources or held bytes")
	}
}
