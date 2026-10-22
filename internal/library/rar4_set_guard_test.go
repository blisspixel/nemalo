package library

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"

	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
)

func TestRAR4SetCoverageRequiresDistinctCleanupAuthority(t *testing.T) {
	testRARSetCoverageRequiresDistinctCleanupAuthority(t, 4, newRAR4SetCleanupFixture)
}

func newRAR4SetCleanupFixture(t *testing.T, count int, build func([]byte) []byte) streamFixture {
	if build == nil {
		return newRAR4SetCleanupWithChild(t, count, "book.epub", nil)
	}
	return newRAR4SetCleanupWithChild(t, count, "inside.zip", func(book []byte) ([]byte, []stagingtest.Archive) {
		body := build(book)
		return body, []stagingtest.Archive{{Kind: "-tzip", Body: body}}
	})
}

func newRAR4SetCleanupWithChild(t *testing.T, count int, name string, build func([]byte) ([]byte, []stagingtest.Archive), alter ...func(int, []byte) []byte) streamFixture {
	t.Helper()
	f := newRAR4CleanupFixture(t, nil)
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
	a := stagingtest.Archive{Kind: "-tRar", Members: map[string][]byte{name: book}, Fields: map[string]string{name: "Split Before = -\nSplit After = -\nVolume Index = 0\n"}}
	f.names, f.parts, f.archiveProof = nil, nil, rar4SetProof
	var total, first int
	for i := range count {
		mainFlags := uint16(0x11)
		if i == 0 {
			mainFlags |= 0x100
		}
		b := append([]byte("Rar!\x1a\x07\x00"), cleanupRAR4Block(0x73, mainFlags, make([]byte, 6), nil)...)
		piece := book[i*len(book)/count : (i+1)*len(book)/count]
		h := cleanupRAR4File(name, piece)
		flags := uint16(0x8000)
		before, after := "-", "-"
		if i > 0 {
			flags |= 1
			before = "+"
		}
		if i+1 < count {
			flags |= 2
			after = "+"
		}
		binary.LittleEndian.PutUint16(h[3:], flags)
		binary.LittleEndian.PutUint32(h[11:], uint32(len(book)))
		crc := crc32.ChecksumIEEE(book)
		if i+1 < count {
			crc = crc32.ChecksumIEEE(piece)
		}
		binary.LittleEndian.PutUint32(h[16:], crc)
		binary.LittleEndian.PutUint16(h, uint16(crc32.ChecksumIEEE(h[2:int(binary.LittleEndian.Uint16(h[5:]))])))
		b = append(b, h...)
		endFlags := uint16(8)
		if i+1 < count {
			endFlags |= 1
		}
		b = append(b, cleanupRAR4Block(0x7b, endFlags, binary.LittleEndian.AppendUint16(nil, uint16(i)), nil)...)
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
		header := fmt.Sprintf("Type = Rar\nPhysical Size = %d\nMultivolume = +\nVolume Index = %d\nVolumes = 1\nCharacteristics = Volume NewVolName", len(b), i)
		if i == 0 {
			header += " FirstVolume"
		}
		header += "\n"
		code := 0
		if i+1 < count {
			header += fmt.Sprintf("ERROR = Missing volume : archive.part%04d.rar\n", i+2)
			code = 2
		}
		listing := fmt.Sprintf("Path = %s\nSize = %d\nPacked Size = %d\nAttributes = -rw-r--r--\nCRC = %08X\nSplit Before = %s\nSplit After = %s\nVolume Index = %d\n\n", name, len(book), len(piece), crc, before, after, i)
		a.Volumes = append(a.Volumes, stagingtest.Volume{Name: fmt.Sprintf("archive.part%04d.rar", i+1), Body: b, Header: header, Listing: listing, Code: code})
	}
	a.Header = fmt.Sprintf("Type = Rar\nPhysical Size = %d\nTotal Physical Size = %d\nMultivolume = +\nVolume Index = 0\nVolumes = %d\n", first, total, count)
	f.archives = append(children, a)
	stagingtest.InstallMany(t, f.archives...)
	return f
}
