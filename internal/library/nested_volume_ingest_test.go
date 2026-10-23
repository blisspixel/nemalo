package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/blisspixel/nemalo/internal/staging"
)

func TestNestedVolumeDefaultIntakePreservesPartsAndExactLeafProvenance(t *testing.T) {
	for _, inner := range nestedVolumeChildLayouts {
		t.Run(inner, func(t *testing.T) {
			f := newNestedVolumeCleanupFixture(t, "zip", inner, 3, nil)
			before, libraryBefore := snapshotTree(t, f.folder), snapshotTree(t, f.root)
			var applied map[string][]byte
			for run, apply := range []bool{false, true, true} {
				out, err := Ingest(context.Background(), IngestRequest{Library: f.root, Folder: f.folder, Apply: apply, Scan: true, Scanner: cleanScan{}})
				if err != nil || !out.Complete || out.Imported != 1 || out.Failed != 0 {
					t.Fatal("default child-set intake failed", out, err)
				}
				imports := 0
				for _, item := range out.Items {
					if item.Import == nil {
						continue
					}
					imports++
					if item.Kind != "archive_member" || len(item.Source) < len(f.names[0])+4 || item.Import.SourceEvidence.Kind != "archive_member" {
						t.Fatal("part escaped as a book", item)
					}
				}
				if imports != 1 || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
					t.Fatal("intake changed originals or published a continuation", out)
				}
				if run == 0 && !reflect.DeepEqual(libraryBefore, snapshotTree(t, f.root)) {
					t.Fatal("preview wrote state")
				}
				if run == 1 {
					applied = snapshotTree(t, f.root)
				}
				if run == 2 && !reflect.DeepEqual(applied, snapshotTree(t, f.root)) {
					t.Fatal("repeat intake wrote another fact")
				}
			}
			page, err := FindManaged(context.Background(), f.root, "", "all", "all", 10, 0)
			if err != nil || page.Total != 1 || len(page.Holdings) != 1 {
				t.Fatal(page, err)
			}
			first := map[string]string{"rar4": "set.part1.rar", "rar5": "set.part1.rar", "old4": "set.rar", "old5": "set.rar", "zip_chunks": "stream.zip.001", "zip_segments": "segments.z01", "7z_chunks": "flat.7z.001"}[inner]
			selection := archiveMemberSelection(staging.Member{Name: "book.epub", Chain: []string{"shelf/" + first, "book.epub"}})
			label := filepath.Join(f.folder, filepath.FromSlash(f.names[0])) + "!/" + selection
			found := false
			for _, s := range page.Holdings[0].Sources {
				if s.Kind == "archive_member" && s.Path == label {
					found = true
				}
			}
			if !found {
				t.Fatal("first-part provenance changed", page.Holdings[0].Sources, label)
			}
		})
	}
}

func TestNestedVolumePendingLeafResumesBeforeOtherImportsOrRefusesWithoutAdoption(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "exact", true: "changed"}[changed], func(t *testing.T) {
			f := newNestedVolumeCleanupFixture(t, "zip", "zip_chunks", 3, nil)
			var book []byte
			for _, b := range snapshotTree(t, filepath.Join(f.root, "assets")) {
				book = b
			}
			root := t.TempDir()
			if _, err := Initialize(context.Background(), root); err != nil {
				t.Fatal(err)
			}
			selection := archiveMemberSelection(staging.Member{Name: "book.epub", Chain: []string{"shelf/stream.zip.001", "book.epub"}})
			label := filepath.Join(f.folder, filepath.FromSlash(f.names[0])) + "!/" + selection
			pendingBook := book
			if changed {
				pendingBook = mustRead(t, writeEPUBParagraph(t, t.TempDir(), "An earlier pending child selection"))
			}
			staged := filepath.Join(t.TempDir(), "lost.epub")
			if err := os.WriteFile(staged, pendingBook, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := importLibrary(context.Background(), ImportRequest{Library: root, Source: staged, Apply: true, Label: label, Scanner: cleanScan{}}, func(stage string) error {
				if stage == "intent" {
					return errors.New("synthetic lost staging after intent")
				}
				return nil
			}); err == nil {
				t.Fatal("interruption absent")
			}
			if err := os.Remove(staged); err != nil {
				t.Fatal(err)
			}
			other := mustRead(t, writeEPUBParagraph(t, t.TempDir(), "A separate later import"))
			writeTree(t, f.folder, map[string][]byte{"aaa.epub": other})
			before, libraryBefore := snapshotTree(t, f.folder), snapshotTree(t, root)
			out, err := Ingest(context.Background(), IngestRequest{Library: root, Folder: f.folder, Apply: true, Scanner: cleanScan{}})
			var imported []IngestItem
			for _, item := range out.Items {
				if item.Import != nil {
					imported = append(imported, item)
				}
			}
			if changed {
				if err == nil || out.Complete || out.Imported != 0 || out.Failed != 1 || len(imported) != 1 || imported[0].Import.Source != label || !reflect.DeepEqual(libraryBefore, snapshotTree(t, root)) {
					t.Fatal("changed child was adopted or another import preceded recovery", out, err)
				}
				if pending, err := pendingSource(root); err != nil || pending != label {
					t.Fatal("pending selection changed", pending, err)
				}
			} else {
				if err != nil || !out.Complete || out.Imported != 2 || len(imported) != 2 || !imported[0].Import.Resumed || imported[0].Import.Source != label || imported[1].Source != "aaa.epub" {
					t.Fatal("exact pending selection did not run first", out, err)
				}
				if pending, err := pendingSource(root); err != nil || pending != "" {
					t.Fatal("successful pending selection remains", pending, err)
				}
			}
			if !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
				t.Fatal("recovery changed source archives")
			}
		})
	}
}

func TestNestedVolumeIncompleteIntakeKeepsPartsAndImportsOnlyIndependentLeaf(t *testing.T) {
	f := nestedVolumeChildFixture(t, "rar5", nil)
	var book []byte
	for _, b := range snapshotTree(t, filepath.Join(f.root, "assets")) {
		book = b
	}
	files := []nestedFile{{"shelf/" + filepath.Base(f.names[2]), f.parts[2]}, {"shelf/" + filepath.Base(f.names[0]), f.parts[0]}, {"direct.epub", book}}
	for _, name := range f.names {
		if err := os.Remove(filepath.Join(f.folder, filepath.FromSlash(name))); err != nil {
			t.Fatal(err)
		}
	}
	body := nestedZIP(t, files...)
	writeTree(t, f.folder, map[string][]byte{"outer.zip": body})
	saved := staging.SevenZip
	staging.SevenZip = func() (string, error) { t.Fatal("missing middle part reached reader"); return "", nil }
	t.Cleanup(func() { staging.SevenZip = saved })
	before := snapshotTree(t, f.folder)
	out, err := Ingest(context.Background(), IngestRequest{Library: f.root, Folder: f.folder, Apply: true, Scan: true, Scanner: cleanScan{}})
	if err == nil || out.Complete || out.Imported != 1 || out.Failed != 1 || len(out.Items) != 4 || out.Items[0].Kind != "archive" || out.Items[0].Outcome != "preserved" {
		t.Fatal("incomplete set skipped accounting or independent intake", out, err)
	}
	for _, item := range out.Items {
		if item.Import != nil && item.Source != "outer.zip!/direct.epub" {
			t.Fatal("continuation became a book", item)
		}
	}
	p, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder})
	if err != nil || p.Move != 0 || p.Keep != 1 {
		t.Fatal("partial intake granted cleanup", p, err)
	}
	r, err := Cleanup(context.Background(), CleanupRequest{Library: f.root, Folder: f.folder, Apply: p.Digest})
	if err != nil || r.Moved != 0 || !reflect.DeepEqual(before, snapshotTree(t, f.folder)) {
		t.Fatal("incomplete source changed", r, err)
	}
}
