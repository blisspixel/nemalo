package library

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/blisspixel/nemalo/internal/safeio"
	"github.com/blisspixel/nemalo/internal/staging"
)

func TestNestedVolumeReadOnlyEvidenceCannotAuthorizeExistingCleanup(t *testing.T) {
	for _, version := range []int{4, 5} {
		for _, old := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/%t", version, old), func(t *testing.T) {
				fixture := newRAR5SetCleanupFixture
				if version == 4 {
					fixture = newRAR4SetCleanupFixture
				}
				f := fixture(t, 3, nil)
				if old {
					f = makeOldRARSet(t, f, version)
				}
				var members []nestedFile
				for i, name := range f.names {
					members = append(members, nestedFile{name: "shelf/" + filepath.Base(name), body: f.parts[i]})
					if err := os.Remove(filepath.Join(f.folder, filepath.FromSlash(name))); err != nil {
						t.Fatal(err)
					}
				}
				outer := nestedZIP(t, members...)
				writeTree(t, f.folder, map[string][]byte{"outer.zip": outer})
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
				l.Coverage, l.RAR4Coverage, l.RAR4SetCoverage, l.RAR5SetCoverage, l.OldRARSetCoverage, l.NestedVolumeSets = true, true, true, true, true, true
				x, err := staging.ExtractTree(context.Background(), src, []string{"outer.zip"}, dst, supportedImport, l)
				if err != nil || !x.Complete || !x.Whole || x.CoveredVolumeSets != 1 || x.RARVersion != 0 || x.RARSetVersion != 0 || x.OldRARSetVersion != 0 {
					t.Fatal("read-only prerequisite failed", x, err)
				}
				held := snapshotTree(t, filepath.Join(f.root, "assets"))
				var leaves int
				for _, m := range x.Members {
					if m.Disposition != "extracted" {
						continue
					}
					leaves++
					got, err := dst.ReadFile(m.Staged)
					if err != nil || len(held) != 1 {
						t.Fatal("holding prerequisite failed", err)
					}
					for _, b := range held {
						if !bytes.Equal(got, b) {
							t.Fatal("read-only output is not the held leaf")
						}
					}
				}
				if leaves != 1 {
					t.Fatal("missing exact held leaf", x)
				}
				before := snapshotTree(t, f.folder)
				p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
				if err != nil || p.Move != 0 || p.Keep != 1 {
					t.Fatal("nested evidence borrowed an existing writer", p, err)
				}
				r, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest})
				if err != nil || r.Moved != 0 || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) || !reflect.DeepEqual(held, snapshotTree(t, filepath.Join(f.root, "assets"))) {
					t.Fatal("read-only nested evidence changed source or holding", r, err)
				}
			})
		}
	}
}
