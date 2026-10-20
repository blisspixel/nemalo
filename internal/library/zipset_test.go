package library

import (
	"archive/zip"
	"bytes"
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

func libraryZIPSet(t *testing.T, stream, corrupt bool) (root, folder, first string, members map[string][]byte) {
	t.Helper()
	root, folder = t.TempDir(), t.TempDir()
	if _, err := Initialize(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	member := mustRead(t, writeEPUBParagraph(t, t.TempDir(), "Split ZIP first book"))
	other := mustRead(t, writeEPUBParagraph(t, t.TempDir(), "Split ZIP second book"))
	members = map[string][]byte{"books/first.epub": member, "books/second.epub": other, "notes.txt": []byte("retain notes")}
	a := stagingtest.Archive{Kind: "-tzip", Members: members}
	if stream {
		var buf bytes.Buffer
		w := zip.NewWriter(&buf)
		for _, name := range []string{"books/first.epub", "books/second.epub", "notes.txt"} {
			out, err := w.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = out.Write(members[name]); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		body := buf.Bytes()
		first = "stream.zip.001"
		writeTree(t, folder, map[string][]byte{first: body[:3], "stream.zip.002": body[3:]})
		a.Header = fmt.Sprintf("Type = zip\nPhysical Size = %d\n", len(body))
		a.Volumes = []stagingtest.Volume{{Name: "archive.zip", Body: body}}
	} else {
		first = "parts.z01"
		a.Headers = map[string]string{}
		files := map[string][]byte{}
		for i, private := range []string{"archive.z01", "archive.z02", "archive.zip"} {
			body := make([]byte, 4096)
			copy(body, fmt.Sprintf("segment %d", i))
			if i == 0 {
				copy(body, "PK\x07\x08")
			}
			source := []string{first, "parts.z02", "parts.zip"}[i]
			files[source] = body
			a.Volumes = append(a.Volumes, stagingtest.Volume{Name: private, Body: body})
			a.Headers[private] = fmt.Sprintf("Type = zip\nPhysical Size = 4096\nTotal Physical Size = 12288\nEmbedded Stub Size = 4\nMultivolume = +\nVolume Index = %d\nVolumes = 3\n", i)
		}
		a.Fields = map[string]string{}
		for name := range members {
			a.Fields[name] = "Volume Index = 0\nOffset = 4\n"
		}
		if corrupt {
			bad := append([]byte{}, member...)
			bad[len(bad)/2] ^= 1
			a.Served = map[string][]byte{"books/first.epub": bad}
		}
		writeTree(t, folder, files)
	}
	stagingtest.Install(t, a)
	return root, folder, first, members
}

func TestSplitZIPIngestRetainsSourcesProvenanceAndRepeat(t *testing.T) {
	for _, stream := range []bool{true, false} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			root, folder, first, _ := libraryZIPSet(t, stream, false)
			before := snapshotTree(t, folder)
			var written map[string][]byte
			for run, apply := range []bool{false, true, true} {
				out, err := Ingest(context.Background(), IngestRequest{Library: root, Folder: folder, Apply: apply, Scan: true, Scanner: cleanScan{}})
				if err != nil || !out.Complete || out.Failed != 0 || out.Imported != 2 {
					t.Fatal(out, err)
				}
				items := map[string]IngestItem{}
				for _, item := range out.Items {
					items[item.Source] = item
					if run == 2 && item.Import != nil && !item.Import.AlreadyHeld {
						t.Fatal("repeat wrote a member", item)
					}
				}
				if items[first+"!/notes.txt"].Outcome != "not_imported" || items[first+"!/books/first.epub"].Import == nil {
					t.Fatal(items)
				}
				for name := range before {
					rel, _ := filepath.Rel(folder, name)
					if rel != first && items[filepath.ToSlash(rel)].Outcome != "archive_volume" {
						t.Fatal("unaccounted part", rel, items)
					}
				}
				if !reflect.DeepEqual(before, snapshotTree(t, folder)) {
					t.Fatal("source bytes changed")
				}
				if run == 1 {
					written = snapshotTree(t, root)
				}
				if run == 2 && !reflect.DeepEqual(written, snapshotTree(t, root)) {
					t.Fatal("repeat changed library bytes")
				}
			}
			page, err := FindManaged(context.Background(), root, first+"!/books/first.epub", "all", "all", 10, 0)
			if err != nil || page.Total != 1 || len(page.Holdings[0].Sources) != 1 || page.Holdings[0].Sources[0].Kind != "archive_member" {
				t.Fatal(page, err)
			}
			plan, err := Cleanup(context.Background(), CleanupRequest{Library: root, Folder: folder})
			if err != nil || len(plan.Entries) != len(before) {
				t.Fatal(plan, err)
			}
			for _, entry := range plan.Entries {
				if entry.Disposition != "keep" {
					t.Fatal(entry)
				}
			}
			wantReason := "ZIP byte coverage is not established"
			if stream {
				wantReason = "notes.txt is not held"
			}
			if !strings.Contains(entryFor(t, plan, first).Reason, wantReason) || !reflect.DeepEqual(before, snapshotTree(t, folder)) {
				t.Fatal(plan)
			}
		})
	}
}

func TestSplitZIPRecoveryResumesOriginalMemberLabel(t *testing.T) {
	for _, stream := range []bool{true, false} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			root, folder, first, bodies := libraryZIPSet(t, stream, false)
			before := snapshotTree(t, folder)
			staged := filepath.Join(t.TempDir(), "lost.epub")
			if err := os.WriteFile(staged, bodies["books/first.epub"], 0o600); err != nil {
				t.Fatal(err)
			}
			label := filepath.Join(folder, first) + "!/books/first.epub"
			if _, err := importLibrary(context.Background(), ImportRequest{Library: root, Source: staged, Apply: true, Label: label}, func(stage string) error {
				if stage == "intent" {
					return errors.New("interrupted member")
				}
				return nil
			}); err == nil {
				t.Fatal("interruption ignored")
			}
			if err := os.Remove(staged); err != nil {
				t.Fatal(err)
			}
			out, err := Ingest(context.Background(), IngestRequest{Library: root, Folder: folder, Apply: true, Scanner: cleanScan{}})
			if err != nil || !out.Complete {
				t.Fatal(out, err)
			}
			resumed := false
			for _, item := range out.Items {
				if item.Source == first+"!/books/first.epub" && item.Import != nil {
					resumed = item.Import.Resumed
				}
			}
			if !resumed || !reflect.DeepEqual(before, snapshotTree(t, folder)) {
				t.Fatal(out)
			}
			if pending, err := pendingSource(root); err != nil || pending != "" {
				t.Fatal(pending, err)
			}
		})
	}
}

func TestSplitZIPMemberFailureIsIncompleteWithVerifiedMembersRetained(t *testing.T) {
	root, folder, first, _ := libraryZIPSet(t, false, true)
	before := snapshotTree(t, folder)
	out, err := Ingest(context.Background(), IngestRequest{Library: root, Folder: folder, Apply: true, Scanner: cleanScan{}})
	if err == nil || out.Complete || out.Failed != 1 || out.Imported != 1 {
		t.Fatal(out, err)
	}
	items := map[string]IngestItem{}
	for _, item := range out.Items {
		items[item.Source] = item
	}
	if items[first+"!/books/first.epub"].Outcome != "failed" || items[first+"!/books/second.epub"].Import == nil {
		t.Fatal(items)
	}
	if !reflect.DeepEqual(before, snapshotTree(t, folder)) {
		t.Fatal("damaged sources changed")
	}
	page, err := FindManaged(context.Background(), root, "", "all", "all", 10, 0)
	if err != nil || page.Total != 1 {
		t.Fatal(page, err)
	}
}

func TestZIPGroupingAnchorsAndAmbiguity(t *testing.T) {
	for _, tc := range []struct {
		name     string
		paths    []string
		sets     int
		complete bool
		reason   string
	}{
		{"ordinary", []string{"a.zip"}, 0, false, ""},
		{"stream", []string{"a.zip.001", "a.ZIP.0002"}, 1, true, ""},
		{"segments", []string{"a.z01", "a.ZIP", "a.z02"}, 1, true, ""},
		{"missing terminal", []string{"a.z01", "a.z02"}, 2, false, "overlapping"},
		{"missing first", []string{"a.z02", "a.zip"}, 1, false, "missing"},
		{"missing middle", []string{"a.z01", "a.z03", "a.zip"}, 1, false, "missing"},
		{"duplicate number", []string{"a.z01", "a.z001", "a.zip"}, 1, false, "ambiguous"},
		{"terminal alias", []string{"a.z01", "a.zip", "a.ZIP"}, 1, false, "ambiguous"},
		{"RAR conflict", []string{"a.rar", "a.z01", "a.zip"}, 2, false, "overlapping"},
		{"old RAR anchor", []string{"a.rar", "a.r00", "a.z01"}, 1, false, "missing"},
		{"families distinct", []string{"a.z01", "a.zip", "a.zip.001", "a.zip.002", "a.7z.001", "a.7z.002"}, 3, true, ""},
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
}

func TestSplitZIPMissingToolPreservesEveryPart(t *testing.T) {
	for _, stream := range []bool{true, false} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			root, folder, first, _ := libraryZIPSet(t, stream, false)
			before := snapshotTree(t, folder)
			saved := staging.SevenZip
			staging.SevenZip = func() (string, error) { return "", staging.ErrNoArchiveTool }
			t.Cleanup(func() { staging.SevenZip = saved })
			out, err := Ingest(context.Background(), IngestRequest{Library: root, Folder: folder, Apply: true, Scanner: cleanScan{}})
			if err != nil || out.Imported != 0 {
				t.Fatal(out, err)
			}
			seen := false
			for _, item := range out.Items {
				if item.Source == first {
					seen = item.Outcome == "not_imported" && strings.Contains(item.Detail, "7-Zip")
				}
			}
			if !seen || !reflect.DeepEqual(before, snapshotTree(t, folder)) {
				t.Fatal("missing reader hidden or source changed", out)
			}
		})
	}
}

func TestIncompleteOrConflictingZIPNamesNeverReachAnArchiver(t *testing.T) {
	ctx := context.Background()
	root, folder := t.TempDir(), t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, name := range []string{"gap.z01", "gap.z03", "gap.zip", "no-first.z02", "no-first.zip", "no-last.z01", "no-last.z02", "both.rar", "both.z01", "both.zip", "stream.zip.001", "stream.zip.003"} {
		files[name] = []byte("retained uncertain part")
	}
	writeTree(t, folder, files)
	before := snapshotTree(t, folder)
	saved := staging.SevenZip
	staging.SevenZip = func() (string, error) { t.Fatal("incomplete or ambiguous names reached an archiver"); return "", nil }
	t.Cleanup(func() { staging.SevenZip = saved })
	out, err := Ingest(ctx, IngestRequest{Library: root, Folder: folder, Apply: true, Scanner: cleanScan{}})
	if err != nil || out.Imported != 0 || len(out.Items) != len(files) {
		t.Fatal(out, err)
	}
	for _, item := range out.Items {
		if item.Outcome != "not_imported" || item.Detail == "" {
			t.Fatal(item)
		}
	}
	plan, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	if err != nil || len(plan.Entries) != len(files) {
		t.Fatal(plan, err)
	}
	for _, entry := range plan.Entries {
		if entry.Disposition != "keep" {
			t.Fatal(entry)
		}
	}
	if !reflect.DeepEqual(before, snapshotTree(t, folder)) {
		t.Fatal("uncertain sources changed")
	}
}

func TestSplitZIPResumesLegacyTerminalPartProvenance(t *testing.T) {
	ctx := context.Background()
	root, folder, first, bodies := libraryZIPSet(t, false, false)
	before := snapshotTree(t, folder)
	staged := filepath.Join(t.TempDir(), "old-terminal-member.epub")
	if err := os.WriteFile(staged, bodies["books/second.epub"], 0o600); err != nil {
		t.Fatal(err)
	}
	label := filepath.Join(folder, "parts.zip") + "!/books/second.epub"
	if _, err := importLibrary(ctx, ImportRequest{Library: root, Source: staged, Apply: true, Label: label}, func(stage string) error {
		if stage == "intent" {
			return errors.New("earlier standalone terminal import interrupted")
		}
		return nil
	}); err == nil {
		t.Fatal("interruption ignored")
	}
	if err := os.Remove(staged); err != nil {
		t.Fatal(err)
	}
	out, err := Ingest(ctx, IngestRequest{Library: root, Folder: folder, Apply: true, Scanner: cleanScan{}})
	if err != nil || !out.Complete || out.Imported != 2 {
		t.Fatal(out, err)
	}
	var imported []IngestItem
	for _, item := range out.Items {
		if item.Import != nil {
			imported = append(imported, item)
		}
	}
	if len(imported) != 2 || imported[0].Source != "parts.zip!/books/second.epub" || !imported[0].Import.Resumed || imported[1].Source != first+"!/books/first.epub" {
		t.Fatal(imported)
	}
	page, err := FindManaged(ctx, root, "parts.zip!/books/second.epub", "all", "all", 10, 0)
	if err != nil || page.Total != 1 || len(page.Holdings[0].Sources) != 1 || page.Holdings[0].Sources[0].Path != label {
		t.Fatal("legacy provenance changed", page, err)
	}
	if pending, err := pendingSource(root); err != nil || pending != "" {
		t.Fatal(pending, err)
	}
	if !reflect.DeepEqual(before, snapshotTree(t, folder)) {
		t.Fatal("legacy recovery changed sources")
	}
}

func TestSplitZIPLegacyRecoveryRefusesUnavailableMemberBeforeOtherImports(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(fmt.Sprint(corrupt), func(t *testing.T) {
			ctx := context.Background()
			root, folder, _, bodies := libraryZIPSet(t, false, corrupt)
			before := snapshotTree(t, folder)
			name := "books/missing.epub"
			if corrupt {
				name = "books/first.epub"
			}
			staged := filepath.Join(t.TempDir(), "old-member.epub")
			if err := os.WriteFile(staged, bodies["books/first.epub"], 0o600); err != nil {
				t.Fatal(err)
			}
			label := filepath.Join(folder, "parts.zip") + "!/" + name
			if _, err := importLibrary(ctx, ImportRequest{Library: root, Source: staged, Apply: true, Label: label}, func(stage string) error {
				if stage == "intent" {
					return errors.New("interrupted terminal member")
				}
				return nil
			}); err == nil {
				t.Fatal("interruption ignored")
			}
			if err := os.Remove(staged); err != nil {
				t.Fatal(err)
			}
			out, err := Ingest(ctx, IngestRequest{Library: root, Folder: folder, Apply: true, Scanner: cleanScan{}})
			if err == nil || out.Imported != 0 {
				t.Fatal(out, err)
			}
			for _, item := range out.Items {
				if item.Import != nil {
					t.Fatal("another member imported before pending recovery", item)
				}
			}
			if pending, err := pendingSource(root); err != nil || pending != label {
				t.Fatal(pending, err)
			}
			if !reflect.DeepEqual(before, snapshotTree(t, folder)) {
				t.Fatal("refused recovery changed sources")
			}
		})
	}
}
