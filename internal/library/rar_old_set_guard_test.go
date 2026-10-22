package library

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/blisspixel/nemalo/internal/safeio"
	"github.com/blisspixel/nemalo/internal/staging"
	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
)

func TestOldRARSetCoverageRequiresDistinctCleanupAuthority(t *testing.T) {
	for _, version := range []int{4, 5} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			fixture := newOldRAR5SetFixture
			if version == 4 {
				fixture = newOldRAR4SetFixture
			}
			f := fixture(t, 3, nil)
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
			var names []string
			for _, name := range f.names {
				names = append(names, filepath.FromSlash(name))
			}
			l := staging.Defaults()
			l.Coverage, l.OldRARSetCoverage = true, true
			l.RAR4Coverage, l.RAR4SetCoverage, l.RAR5SetCoverage = version == 4, version == 4, version == 5
			x, err := staging.ExtractTree(context.Background(), src, names, dst, func(name string) bool { return name == "book.epub" }, l)
			if err != nil || !x.Whole || !x.Complete || x.OldRARSetVersion != version || x.RARSetVersion != 0 || x.RARVersion != 0 || x.RARContainers != 0 || len(x.Members) != 1 {
				t.Fatal("read-only proof prerequisite failed", x, err)
			}
			held := snapshotTree(t, filepath.Join(f.root, "assets"))
			got, err := dst.ReadFile(x.Members[0].Staged)
			if err != nil || len(held) != 1 {
				t.Fatal("holding prerequisite", len(held), err)
			}
			for _, b := range held {
				if !bytes.Equal(got, b) {
					t.Fatal("output is not the held leaf")
				}
			}
			before := snapshotTree(t, f.folder)
			p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
			if err != nil || p.Move != 1 || p.Keep != 0 || len(p.Entries) != 1 || p.Entries[0].ArchiveProof != f.proof() || !supportedArchiveProof(p.Entries[0]) || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
				t.Fatal("old set has no distinct complete authority", p, err)
			}
			for _, proof := range []string{rar4SetProof, rar5SetProof, rar4FramesProof, rar5FramesProof, archiveTreeProof, archiveRARTreeProof, "", "rar4-old-set/2", "rar5-old-set/2"} {
				e := p.Entries[0]
				e.ArchiveProof = proof
				if supportedArchiveProof(e) {
					t.Fatal("old layout borrowed another proof", proof)
				}
			}
			r, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest})
			if err != nil || r.Moved != 1 || !reflect.DeepEqual(held, snapshotTree(t, filepath.Join(f.root, "assets"))) {
				t.Fatal("cleanup or holding changed", r, err)
			}
			f.assertParts(t, filepath.Join(r.HoldingArea, "files"))
			restored, err := Restore(context.Background(), RestoreRequest{Library: f.root, CleanupID: r.CleanupID, Apply: true})
			if err != nil || restored.Restore != len(names) || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
				t.Fatal("raw restore differs", restored, err)
			}
		})
	}
}

func TestOldRAR4NamesCannotOverridePhysicalModernNamingFlags(t *testing.T) {
	f := newOldRAR4SetFixture(t, 3, nil)
	for i, b := range f.parts {
		h := b[7:20]
		binary.LittleEndian.PutUint16(h[3:], binary.LittleEndian.Uint16(h[3:])|0x10)
		binary.LittleEndian.PutUint16(h, uint16(crc32.ChecksumIEEE(h[2:])))
		writeTree(t, f.folder, map[string][]byte{f.names[i]: b})
	}
	// The injected reader still claims the exact old-name layout. Physical
	// framing must refuse before any extracted leaf can authorize cleanup.
	stagingtest.InstallMany(t, f.archives...)
	before := snapshotTree(t, f.folder)
	p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
	if err != nil || p.Move != 0 || p.Keep != len(f.names) || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
		t.Fatal("renaming granted incompatible RAR4 authority", p, err)
	}
	r, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest})
	if err != nil || r.Moved != 0 || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
		t.Fatal("incompatible physical flags moved", r, err)
	}
}
