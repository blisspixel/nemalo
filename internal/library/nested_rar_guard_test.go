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
	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
)

func TestNestedRARReaderCoverageCannotAuthorizeExistingCleanupProofs(t *testing.T) {
	for _, version := range []int{4, 5} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			fixture, kind := newRAR4CleanupFixture, "Rar"
			if version == 5 {
				fixture, kind = newRAR5CleanupFixture, "Rar5"
			}
			f := fixture(t, nil)
			var book []byte
			for _, data := range snapshotTree(t, filepath.Join(f.root, "assets")) {
				book = data
			}
			inner := f.parts[0]
			stagingtest.Install(t, stagingtest.Archive{Kind: "-t" + kind, Body: inner, Header: fmt.Sprintf("Type = %s\nPhysical Size = %d\nMultivolume = -\nVolumes = 1\n", kind, len(inner)), Members: map[string][]byte{"book.epub": book}})
			if err := os.Remove(filepath.Join(f.folder, filepath.FromSlash(f.names[0]))); err != nil {
				t.Fatal(err)
			}
			outer := nestedZIP(t, nestedFile{"child.rar", inner})
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
			limits := staging.Defaults()
			limits.Coverage, limits.RAR4Coverage, limits.NestedRARCoverage = true, true, true
			x, err := staging.ExtractTree(context.Background(), src, []string{"outer.zip"}, dst, func(name string) bool { return name == "book.epub" }, limits)
			if err != nil || !x.Whole || !x.Complete || x.RARVersion != 0 || len(x.Members) != 2 || x.Members[1].Disposition != "extracted" {
				t.Fatal("opted-in read-only coverage failed", x, err)
			}
			if got, err := dst.ReadFile(x.Members[1].Staged); err != nil || !bytes.Equal(got, book) {
				t.Fatal("nested bytes differ", err)
			}
			before := snapshotTree(t, f.folder)
			p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
			if err != nil || p.Move != 0 || p.Keep != 1 || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
				t.Fatal("read-only capability granted existing proof", p, err)
			}
			c, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest})
			if err != nil || c.Moved != 0 || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
				t.Fatal("application moved unsupported tree", c, err)
			}
		})
	}
}
