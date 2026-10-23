package library

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
)

func TestNestedVolumeInterleavedGroupsRequireEveryHeldLeaf(t *testing.T) {
	for _, holdSecond := range []bool{false, true} {
		t.Run(map[bool]string{false: "unheld second group", true: "all groups held"}[holdSecond], func(t *testing.T) {
			a, b := nestedVolumeChildFixture(t, "old4", nil), nestedVolumeChildFixture(t, "zip_chunks", nil)
			var first, second []byte
			for _, data := range snapshotTree(t, filepath.Join(a.root, "assets")) {
				first = data
			}
			for _, data := range snapshotTree(t, filepath.Join(b.root, "assets")) {
				second = data
			}
			if holdSecond {
				source := filepath.Join(t.TempDir(), "second.epub")
				if err := os.WriteFile(source, second, 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := Import(context.Background(), ImportRequest{Library: a.root, Source: source, Apply: true, Scan: true, Scanner: cleanScan{}}); err != nil {
					t.Fatal(err)
				}
			}
			part := func(prefix string, f streamFixture, i int) nestedFile {
				return nestedFile{prefix + "/" + filepath.Base(f.names[i]), f.parts[i]}
			}
			files := []nestedFile{part("a", a, 2), part("b", b, 1), part("a", a, 0), {"direct.epub", first}, part("b", b, 2), part("a", a, 1), part("b", b, 0)}
			for _, name := range a.names {
				if err := os.Remove(filepath.Join(a.folder, filepath.FromSlash(name))); err != nil {
					t.Fatal(err)
				}
			}
			body := nestedZIP(t, files...)
			writeTree(t, a.folder, map[string][]byte{"outer.zip": body})
			stagingtest.InstallMany(t, append(a.archives, b.archives...)...)
			before, assets := snapshotTree(t, a.folder), snapshotTree(t, filepath.Join(a.root, "assets"))
			p, err := Cleanup(context.Background(), CleanupRequest{Library: a.root, Folder: a.folder})
			if err != nil {
				t.Fatal(err)
			}
			if !holdSecond {
				if p.Move != 0 || p.Keep != 1 {
					t.Fatal("one held group authorized an unheld peer", p)
				}
				r, err := Cleanup(context.Background(), CleanupRequest{Library: a.root, Folder: a.folder, Apply: p.Digest})
				if err != nil || r.Moved != 0 || !reflect.DeepEqual(before, snapshotTree(t, a.folder)) {
					t.Fatal("unheld group source changed", r, err)
				}
				return
			}
			if p.Move != 1 || p.Entries[0].ArchiveProof != archiveVolumeTreeProof || len(p.Entries[0].ArchiveLeaves) != 3 || !reflect.DeepEqual(before, snapshotTree(t, a.folder)) {
				t.Fatal("complete groups lost their distinct selections", p)
			}
			want := [][]string{{"a/set.rar", "book.epub"}, {"b/stream.zip.001", "book.epub"}, {"direct.epub"}}
			for i, leaf := range p.Entries[0].ArchiveLeaves {
				if !reflect.DeepEqual(leaf.Chain, want[i]) {
					t.Fatal("interleaved source chain changed", leaf, want[i])
				}
			}
			c, err := Cleanup(context.Background(), CleanupRequest{Library: a.root, Folder: a.folder, Apply: p.Digest})
			if err != nil || c.Moved != 1 {
				t.Fatal(c, err)
			}
			q, err := Purge(context.Background(), PurgeRequest{Library: a.root, CleanupID: c.CleanupID})
			if err != nil || q.Purge != 1 || len(q.ArchiveGroups) != 1 || q.ArchiveGroups[0].LeafDigest != archiveLeafDigest(p.Entries[0]) {
				t.Fatal("fresh proof lost a group or repeated holding", q, err)
			}
			r, err := Purge(context.Background(), PurgeRequest{Library: a.root, CleanupID: c.CleanupID, Apply: q.Digest})
			if err != nil || r.Purged != 1 || !reflect.DeepEqual(assets, snapshotTree(t, filepath.Join(a.root, "assets"))) {
				t.Fatal("group purge changed stored bytes", r, err)
			}
		})
	}
}
