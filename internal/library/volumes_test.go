package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/staging"
	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
)

func TestVolumeGroupingKeepsFamiliesAndAmbiguity(t *testing.T) {
	for _, tc := range []struct {
		name     string
		paths    []string
		sets     int
		complete bool
		reason   string
	}{
		{"plain", []string{"a.rar", "b.zip"}, 0, false, ""},
		{"old contiguous", []string{"a.r01", "a.rar", "a.r00"}, 1, true, ""},
		{"case variants", []string{"Book.RAR", "book.R00"}, 1, true, ""},
		{"old missing first", []string{"a.r00", "a.r01"}, 1, false, "missing"},
		{"old missing middle", []string{"a.rar", "a.r01"}, 1, false, "missing"},
		{"old alias", []string{"a.rar", "a.RAR", "a.r00"}, 1, false, "ambiguous"},
		{"numbered alias", []string{"a.part1.rar", "a.part01.rar", "a.part2.rar"}, 1, false, "ambiguous"},
		{"directory isolation", []string{"a/book.rar", "b/book.r00"}, 1, false, "missing"},
		{"families distinct", []string{"a.rar", "a.r00", "a.part1.rar", "a.part2.rar"}, 2, true, ""},
		{"part1 old first", []string{"a.part1.rar", "a.part1.r00"}, 1, true, ""},
		{"overlap", []string{"a.part1.rar", "a.part2.rar", "a.part1.r00"}, 2, false, "overlapping"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sets := volumeSets(tc.paths)
			if len(sets) != tc.sets {
				t.Fatal(sets)
			}
			for _, set := range sets {
				if set.complete != tc.complete || !strings.Contains(set.reason, tc.reason) {
					t.Fatal(set)
				}
			}
		})
	}
	paths := []string{"a.rar"}
	for n := 0; n < 900; n++ {
		paths = append(paths, fmt.Sprintf("a.%c%02d", 'r'+n/100, n%100))
	}
	if sets := volumeSets(paths); len(sets) != 1 || !sets[0].complete || len(sets[0].volumes) != 901 {
		t.Fatal("old letters did not roll over", sets)
	}
	paths = nil
	for n := 1; n <= 1001; n++ {
		paths = append(paths, fmt.Sprintf("a.part%d.rar", n))
	}
	if sets := volumeSets(paths); len(sets) != 1 || sets[0].complete || !strings.Contains(sets[0].reason, "limit") {
		t.Fatal("volume count limit", sets)
	}
}

func installOldRAR(t *testing.T, member []byte) []byte {
	t.Helper()
	body := []byte("Rar!\x1a\x07\x00 volume")
	stagingtest.Install(t, stagingtest.Archive{
		Kind: "-tRar", Header: "Type = Rar\nPhysical Size = 14\nTotal Physical Size = 28\nMultivolume = +\nVolume Index = 0\nVolumes = 2\n",
		Members: map[string][]byte{"books/held.epub": member, "notes.txt": []byte("preserve notes")},
		Volumes: []stagingtest.Volume{
			{Name: "archive.rar", Body: body, Code: 2, Header: "Type = Rar\nPhysical Size = 14\nCharacteristics = Volume FirstVolume\nMultivolume = +\nVolume Index = 0\nVolumes = 1\nERROR = Missing volume : archive.r00\n", Listing: fmt.Sprintf("Path = books/held.epub\nSize = %d\nSplit Before = -\nSplit After = +\n\n", len(member))},
			{Name: "archive.r00", Body: body, Header: "Type = Rar\nPhysical Size = 14\nCharacteristics = Volume\nMultivolume = +\nVolume Index = 1\nVolumes = 1\n", Listing: fmt.Sprintf("Path = books/held.epub\nSize = %d\nSplit Before = +\nSplit After = -\n\n", len(member))},
		},
	})
	return body
}

func TestOldRARIngestPreservesSourcesProvenanceAndRepeat(t *testing.T) {
	ctx := context.Background()
	root, folder := t.TempDir(), t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	member := mustRead(t, writeEPUBParagraph(t, t.TempDir(), "Old RAR member"))
	body := installOldRAR(t, member)
	writeTree(t, folder, map[string][]byte{"legacy.rar": body, "legacy.r00": body})
	before := snapshotTree(t, folder)
	for _, apply := range []bool{false, true, true} {
		out, err := Ingest(ctx, IngestRequest{Library: root, Folder: folder, Apply: apply, Scan: true, Scanner: cleanScan{}})
		if err != nil || out.Failed != 0 || !out.Complete {
			t.Fatal(out, err)
		}
		items := map[string]IngestItem{}
		for _, item := range out.Items {
			items[item.Source] = item
		}
		if items["legacy.r00"].Outcome != "archive_volume" || items["legacy.rar!/books/held.epub"].Outcome == "" || items["legacy.rar!/notes.txt"].Outcome != "not_imported" {
			t.Fatal(items)
		}
		if !reflect.DeepEqual(before, snapshotTree(t, folder)) {
			t.Fatal("sources changed")
		}
	}
	page, err := FindManaged(ctx, root, "legacy.rar!/books/held.epub", "all", "all", 10, 0)
	if err != nil || page.Total != 1 || len(page.Holdings[0].Sources) != 1 || page.Holdings[0].Sources[0].Kind != "archive_member" {
		t.Fatal(page, err)
	}
	plan, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"legacy.rar", "legacy.r00"} {
		if entry := entryFor(t, plan, name); entry.Disposition != "keep" {
			t.Fatal(entry)
		}
	}
	if !reflect.DeepEqual(before, snapshotTree(t, folder)) {
		t.Fatal("cleanup preview changed sources")
	}
}

func TestAmbiguousRARFamiliesAreAccountedForWithoutToolCalls(t *testing.T) {
	saved := staging.SevenZip
	staging.SevenZip = func() (string, error) { t.Fatal("ambiguous family reached the archiver"); return "", nil }
	t.Cleanup(func() { staging.SevenZip = saved })
	ctx := context.Background()
	root, folder := t.TempDir(), t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	writeTree(t, folder, map[string][]byte{"a.part1.rar": []byte("x"), "a.part2.rar": []byte("y"), "a.part1.r00": []byte("z")})
	before := snapshotTree(t, folder)
	out, err := Ingest(ctx, IngestRequest{Library: root, Folder: folder, Apply: true})
	if err != nil || len(out.Items) != 3 || out.Imported != 0 {
		t.Fatal(out, err)
	}
	for _, item := range out.Items {
		if item.Outcome != "not_imported" || !strings.Contains(item.Detail, "overlapping") {
			t.Fatal(item)
		}
	}
	plan, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	if err != nil || len(plan.Entries) != 3 {
		t.Fatal(plan, err)
	}
	for _, entry := range plan.Entries {
		if entry.Disposition != "keep" || !strings.Contains(entry.Reason, "overlapping") {
			t.Fatal(entry)
		}
	}
	if !reflect.DeepEqual(before, snapshotTree(t, folder)) {
		t.Fatal("sources changed")
	}
	for _, name := range []string{"a.part1.rar", "a.part2.rar", "a.part1.r00"} {
		if _, err := os.Stat(filepath.Join(folder, name)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOldRARSetResumesAnInterruptedMember(t *testing.T) {
	ctx := context.Background()
	root, folder := t.TempDir(), t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	staged := writeEPUBParagraph(t, t.TempDir(), "Recover old RAR")
	member := mustRead(t, staged)
	body := installOldRAR(t, member)
	writeTree(t, folder, map[string][]byte{"legacy.rar": body, "legacy.r00": body})
	before := snapshotTree(t, folder)
	label := filepath.Join(folder, "legacy.rar") + "!/books/held.epub"
	if _, err := importLibrary(ctx, ImportRequest{Library: root, Source: staged, Apply: true, Label: label}, func(stage string) error {
		if stage == "intent" {
			return errors.New("interrupted before member publication")
		}
		return nil
	}); err == nil {
		t.Fatal("interruption ignored")
	}
	if err := os.Remove(staged); err != nil {
		t.Fatal(err)
	}
	out, err := Ingest(ctx, IngestRequest{Library: root, Folder: folder, Apply: true, Scanner: cleanScan{}})
	if err != nil || !out.Complete {
		t.Fatal(out, err)
	}
	resumed := false
	for _, item := range out.Items {
		if item.Source == "legacy.rar!/books/held.epub" && item.Import != nil {
			resumed = item.Import.Resumed
		}
	}
	if !resumed || !reflect.DeepEqual(before, snapshotTree(t, folder)) {
		t.Fatal("member did not resume with preserved sources", out)
	}
	if pending, err := pendingSource(root); err != nil || pending != "" {
		t.Fatal(pending, err)
	}
}
