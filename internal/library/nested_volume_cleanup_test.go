package library

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var nestedVolumeOuterLayouts = []string{"zip", "7z", "rar4", "rar5", "zip_chunks", "zip_segments", "7z_chunks", "rar4_set", "rar5_set", "old4_set", "old5_set"}
var nestedVolumeChildLayouts = []string{"rar4", "rar5", "old4", "old5", "zip_chunks", "zip_segments", "7z_chunks"}

func TestNestedVolumeCleanupBindsAllOuterAndChildSelectionLayouts(t *testing.T) {
	var cases [][2]string
	for _, outer := range nestedVolumeOuterLayouts {
		cases = append(cases, [2]string{outer, "old5"})
	}
	for _, inner := range nestedVolumeChildLayouts {
		if inner != "old5" {
			cases = append(cases, [2]string{"zip", inner})
		}
	}
	for _, tc := range cases {
		t.Run(tc[0]+"/"+tc[1], func(t *testing.T) {
			f := newNestedVolumeCleanupFixture(t, tc[0], tc[1], 3, nil)
			before, assets := snapshotTree(t, f.folder), snapshotTree(t, filepath.Join(f.root, "assets"))
			p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
			if err != nil || p.Move != 1 || p.Keep != 0 || len(p.Entries) != 1 || p.Entries[0].ArchiveProof != archiveVolumeTreeProof || !supportedArchiveProof(p.Entries[0]) || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
				t.Fatal("new authority or preview identity differs", p, err)
			}
			first := map[string]string{"rar4": "set.part1.rar", "rar5": "set.part1.rar", "old4": "set.rar", "old5": "set.rar", "zip_chunks": "stream.zip.001", "zip_segments": "segments.z01", "7z_chunks": "flat.7z.001"}[tc[1]]
			chain := []string{"shelf/" + first, "book.epub"}
			if strings.HasSuffix(tc[0], "_set") {
				chain = append([]string{"inner.zip"}, chain...)
			}
			if len(p.Entries[0].ArchiveLeaves) != 1 || !reflect.DeepEqual(p.Entries[0].ArchiveLeaves[0].Chain, chain) {
				t.Fatal("exact first-member chain changed", p.Entries[0])
			}
			c := f.cleanup(t)
			q, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
			if err != nil || q.Purge != len(f.parts) || len(q.ArchiveGroups) != 1 || q.ArchiveGroups[0].Proof != archiveVolumeTreeProof || q.ArchiveGroups[0].LeafDigest != archiveLeafDigest(p.Entries[0]) {
				t.Fatal("fresh tree proof lost its sources/leaves", q, err)
			}
			r, err := Restore(context.Background(), RestoreRequest{Library: f.root, CleanupID: c.CleanupID, Apply: true})
			if err != nil || r.Restore != len(f.parts) || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
				t.Fatal("raw source restore changed bytes", r, err)
			}
			c = f.cleanup(t)
			q, err = Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
			if err != nil {
				t.Fatal(err)
			}
			done, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: q.Digest})
			if err != nil || done.Purged != len(f.parts) || len(snapshotTree(t, filepath.Join(c.HoldingArea, "files"))) != 0 || !reflect.DeepEqual(assets, snapshotTree(t, filepath.Join(f.root, "assets"))) {
				t.Fatal("fresh purge changed holdings or retained originals", done, err)
			}
		})
	}
}

func TestNestedVolumeFreshPurgeReproducesNewAuthorityEvenWithHistoricalRARSyntax(t *testing.T) {
	for _, inner := range []string{"old4", "zip_chunks"} {
		for _, proof := range []string{archiveRARTreeProof, archiveTreeProof, rar4SetProof, "archive-tree/future", ""} {
			t.Run(inner+"/"+proof, func(t *testing.T) {
				f := newNestedVolumeCleanupFixture(t, "zip_chunks", inner, 3, nil)
				c := f.cleanup(t)
				rewriteTreeCleanup(t, f.root, c.CleanupID, func(m *cleanupManifest) {
					m.Entries[0].ArchiveProof = proof
					if inner == "old4" && proof == archiveRARTreeProof && !supportedArchiveProof(m.Entries[0]) {
						t.Fatal("ambiguous RAR-name test must reach fresh physical reproduction")
					}
				})
				before := snapshotTree(t, filepath.Join(c.HoldingArea, "files"))
				q, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID})
				if err != nil || q.Purge != 0 || len(q.ArchiveGroups) != 0 {
					t.Fatal("earlier/future proof borrowed new traversal", q, err)
				}
				r, err := Purge(context.Background(), PurgeRequest{Library: f.root, CleanupID: c.CleanupID, Apply: q.Digest})
				if err != nil || r.Purged != 0 || !reflect.DeepEqual(before, snapshotTree(t, filepath.Join(c.HoldingArea, "files"))) {
					t.Fatal("unreproduced proof removed bytes", r, err)
				}
			})
		}
	}
}
