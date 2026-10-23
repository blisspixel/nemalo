package library

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
)

func TestNestedVolumeEmptyChildBesideDirectHoldingRequiresMeasuredNewProof(t *testing.T) {
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
	// The existing ZIP proof accounts for a known empty directory, while a
	// zero-entry ZIP is outside that proof. No parser subset expands here.
	empty := nestedZIP(t, nestedFile{"empty/", nil})
	body := nestedZIP(t, nestedFile{"direct.epub", book}, nestedFile{"empty.zip.002", empty[len(empty)/2:]}, nestedFile{"empty.zip.001", empty[:len(empty)/2]})
	f.names, f.parts, f.archiveProof = []string{"outer.zip"}, [][]byte{body}, archiveVolumeTreeProof
	writeTree(t, f.folder, map[string][]byte{"outer.zip": body})
	stagingtest.InstallMany(t, stagingtest.Archive{Body: empty, Kind: "-tzip", Header: fmt.Sprintf("Type = zip\nPhysical Size = %d\n", len(empty))})
	before := snapshotTree(t, f.folder)
	p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
	if err != nil || p.Move != 1 || p.Entries[0].ArchiveProof != archiveVolumeTreeProof || len(p.Entries[0].ArchiveLeaves) != 1 || !reflect.DeepEqual(p.Entries[0].ArchiveLeaves[0].Chain, []string{"direct.epub"}) || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
		t.Fatal("empty set authority was inferred from leaf names", p, err)
	}
	c := f.cleanup(t)
	q, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
	if err != nil || q.Purge != 1 || q.ArchiveGroups[0].Proof != archiveVolumeTreeProof {
		t.Fatal("fresh proof lost the covered empty set", q, err)
	}
}
