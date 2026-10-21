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

func TestRAR4SetReadOnlyEvidenceStillPreservesAllHeldSources(t *testing.T) {
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
	a := stagingtest.Archive{Kind: "-tRar", Members: map[string][]byte{"book.epub": book}, Fields: map[string]string{"book.epub": "Split Before = -\nSplit After = -\nVolume Index = 0\n"}}
	var names []string
	var total, first int
	for i := range 3 {
		mainFlags := uint16(0x11)
		if i == 0 {
			mainFlags |= 0x100
		}
		b := append([]byte("Rar!\x1a\x07\x00"), cleanupRAR4Block(0x73, mainFlags, make([]byte, 6), nil)...)
		piece := book[i*len(book)/3 : (i+1)*len(book)/3]
		h := cleanupRAR4File("book.epub", piece)
		flags := uint16(0x8000)
		before, after := "-", "-"
		if i > 0 {
			flags |= 1
			before = "+"
		}
		if i < 2 {
			flags |= 2
			after = "+"
		}
		binary.LittleEndian.PutUint16(h[3:], flags)
		binary.LittleEndian.PutUint32(h[11:], uint32(len(book)))
		crc := crc32.ChecksumIEEE(book)
		if i < 2 {
			crc = crc32.ChecksumIEEE(piece)
		}
		binary.LittleEndian.PutUint32(h[16:], crc)
		binary.LittleEndian.PutUint16(h, uint16(crc32.ChecksumIEEE(h[2:int(binary.LittleEndian.Uint16(h[5:]))])))
		b = append(b, h...)
		endFlags := uint16(8)
		if i < 2 {
			endFlags |= 1
		}
		b = append(b, cleanupRAR4Block(0x7b, endFlags, binary.LittleEndian.AppendUint16(nil, uint16(i)), nil)...)
		name := fmt.Sprintf("books/set.part%d.rar", i+1)
		names = append(names, filepath.FromSlash(name))
		writeTree(t, f.folder, map[string][]byte{name: b})
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
		if i < 2 {
			header += fmt.Sprintf("ERROR = Missing volume : archive.part%04d.rar\n", i+2)
			code = 2
		}
		listing := fmt.Sprintf("Path = book.epub\nSize = %d\nPacked Size = %d\nAttributes = -rw-r--r--\nCRC = %08X\nSplit Before = %s\nSplit After = %s\nVolume Index = %d\n\n", len(book), len(piece), crc, before, after, i)
		a.Volumes = append(a.Volumes, stagingtest.Volume{Name: fmt.Sprintf("archive.part%04d.rar", i+1), Body: b, Header: header, Listing: listing, Code: code})
	}
	a.Header = fmt.Sprintf("Type = Rar\nPhysical Size = %d\nTotal Physical Size = %d\nMultivolume = +\nVolume Index = 0\nVolumes = 3\n", first, total)
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
	l.Coverage, l.RAR4Coverage, l.RAR4SetCoverage = true, true, true
	x, err := staging.ExtractTree(context.Background(), src, names, dst, func(n string) bool { return n == "book.epub" }, l)
	if err != nil || !x.Whole || !x.Complete || x.RARSetVersion != 4 || x.RARVersion != 0 || x.RARContainers != 0 || len(x.Members) != 1 {
		t.Fatal("read-only prerequisite failed", x, err)
	}
	if got, err := dst.ReadFile(x.Members[0].Staged); err != nil || !bytes.Equal(got, book) {
		t.Fatal("held bytes differ", err)
	}
	before, held := snapshotTree(t, f.folder), snapshotTree(t, filepath.Join(f.root, "assets"))
	p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
	if err != nil || p.Move != 0 || p.Keep != 3 {
		t.Fatal("read-only RAR4 facts borrowed write authority", p, err)
	}
	r, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest})
	if err != nil || r.Moved != 0 || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) || !reflect.DeepEqual(held, snapshotTree(t, filepath.Join(f.root, "assets"))) {
		t.Fatal("application changed retained sources or holdings", r, err)
	}
}
