package library

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/blisspixel/nemalo/internal/safeio"
	"github.com/blisspixel/nemalo/internal/staging"
	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
)

func cleanupRAR4Block(kind byte, flags uint16, body, data []byte) []byte {
	h := []byte{0, 0, kind}
	h = binary.LittleEndian.AppendUint16(h, flags)
	h = binary.LittleEndian.AppendUint16(h, uint16(7+len(body)))
	h = append(h, body...)
	binary.LittleEndian.PutUint16(h, uint16(crc32.ChecksumIEEE(h[2:])))
	return append(h, data...)
}

func TestRAR4ReaderCapabilityCannotAuthorizeRAR5CleanupProof(t *testing.T) {
	f := newRAR5CleanupFixture(t, nil)
	assets := snapshotTree(t, filepath.Join(f.root, "assets"))
	if len(assets) != 1 {
		t.Fatal("expected one stored book", len(assets))
	}
	var book []byte
	for _, data := range assets {
		book = data
	}
	b := binary.LittleEndian.AppendUint32(nil, uint32(len(book)))
	b = binary.LittleEndian.AppendUint32(b, uint32(len(book)))
	b = append(b, 3)
	b = binary.LittleEndian.AppendUint32(b, crc32.ChecksumIEEE(book))
	b = binary.LittleEndian.AppendUint32(b, 0)
	b = append(b, 20, 0x30)
	b = binary.LittleEndian.AppendUint16(b, uint16(len("book.epub")))
	b = binary.LittleEndian.AppendUint32(b, 0100644)
	b = append(b, "book.epub"...)
	data := append([]byte("Rar!\x1a\x07\x00"), cleanupRAR4Block(0x73, 0, make([]byte, 6), nil)...)
	data = append(data, cleanupRAR4Block(0x74, 0x8000, b, book)...)
	data = append(data, cleanupRAR4Block(0x7b, 0, nil, nil)...)
	writeTree(t, f.folder, map[string][]byte{f.names[0]: data})
	stagingtest.Install(t, stagingtest.Archive{Kind: "-tRar", Body: data, Members: map[string][]byte{"book.epub": book}})
	ctx := context.Background()
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
	limits := staging.Defaults()
	limits.Coverage = true
	limits.RAR4Coverage = true
	x, err := staging.ExtractTree(ctx, src, []string{filepath.FromSlash(f.names[0])}, dst, func(name string) bool { return name == "book.epub" }, limits)
	if err != nil || !x.Whole || !x.Complete || len(x.Members) != 1 || x.Members[0].Disposition != "extracted" {
		t.Fatal("opted-in RAR4 reader evidence failed", x, err)
	}
	if got, err := dst.ReadFile(x.Members[0].Staged); err != nil || !bytes.Equal(got, book) {
		t.Fatal("staged book differs", err)
	}
	before := snapshotTree(t, f.folder)
	p, err := Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder})
	if err != nil || p.Move != 0 || p.Keep != 1 || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
		t.Fatal("RAR4 capability granted wrong cleanup proof", p, err)
	}
	applied, err := Cleanup(ctx, CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest})
	if err != nil || applied.Moved != 0 || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
		t.Fatal("unsupported RAR4 source moved", applied, err)
	}
}
