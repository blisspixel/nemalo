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
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/safeio"
	"github.com/blisspixel/nemalo/internal/staging"
	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
)

func TestOldRARReadOnlyCoverageCannotAuthorizeCleanup(t *testing.T) {
	for _, version := range []int{4, 5} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			fixture := newRAR5SetCleanupFixture
			if version == 4 {
				fixture = newRAR4SetCleanupFixture
			}
			f := fixture(t, 3, nil)
			a := &f.archives[len(f.archives)-1]
			for i, name := range f.names {
				if err := os.Remove(filepath.Join(f.folder, filepath.FromSlash(name))); err != nil {
					t.Fatal(err)
				}
				private, original := "archive.rar", "books/set.rar"
				if i > 0 {
					private, original = fmt.Sprintf("archive.r%02d", i-1), fmt.Sprintf("books/set.r%02d", i-1)
				}
				if version == 4 {
					h := f.parts[i][7:20]
					binary.LittleEndian.PutUint16(h[3:], binary.LittleEndian.Uint16(h[3:])&^0x10)
					binary.LittleEndian.PutUint16(h, uint16(crc32.ChecksumIEEE(h[2:])))
				}
				f.names[i] = original
				writeTree(t, f.folder, map[string][]byte{original: f.parts[i]})
				a.Volumes[i].Name, a.Volumes[i].Body = private, f.parts[i]
				header := strings.ReplaceAll(a.Volumes[i].Header, " NewVolName", "")
				if i < 2 {
					header = strings.ReplaceAll(header, fmt.Sprintf("archive.part%04d.rar", i+2), fmt.Sprintf("archive.r%02d", i))
				}
				a.Volumes[i].Header = header
			}
			stagingtest.InstallMany(t, f.archives...)
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
			if err != nil || p.Move != 0 || p.Keep != len(names) {
				t.Fatal("old evidence supplied cleanup authority", p, err)
			}
			r, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest})
			if err != nil || r.Moved != 0 || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) || !reflect.DeepEqual(held, snapshotTree(t, filepath.Join(f.root, "assets"))) {
				t.Fatal("source or holding changed", r, err)
			}
		})
	}
}
