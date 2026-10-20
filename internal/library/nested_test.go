package library

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/scanner"
	"github.com/blisspixel/nemalo/internal/staging"
	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
)

type nestedFile struct {
	name string
	body []byte
}

func nestedZIP(t *testing.T, files ...nestedFile) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, file := range files {
		out, err := w.CreateHeader(&zip.FileHeader{Name: file.name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := out.Write(file.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func nestedFolder(t *testing.T, kind string) (root, folder, outer string, leaf []byte) {
	t.Helper()
	root, folder = t.TempDir(), t.TempDir()
	if _, err := Initialize(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	direct := mustRead(t, writeEPUBParagraph(t, t.TempDir(), "Direct literal separator book"))
	leaf = mustRead(t, writeEPUBParagraph(t, t.TempDir(), "Nested book"))
	inner := nestedZIP(t, nestedFile{"book.epub", leaf}, nestedFile{"notes.txt", []byte("retain")})
	files := []nestedFile{{"inner.zip!/book.epub", direct}, {"inner.zip", inner}, {"notes.txt", []byte("outer notes")}}
	profiles := []stagingtest.Archive{{Body: inner, Kind: "-tzip"}}
	var body []byte
	switch kind {
	case "zip":
		outer, body = "outer.zip", nestedZIP(t, files...)
	case "rar":
		outer, body = "outer.rar", []byte("Rar!\x1a\x07\x01\x00 synthetic nested outer")
	case "7z":
		outer, body = "outer.7z", []byte("7z\xbc\xaf\x27\x1c synthetic nested outer")
	}
	if kind != "zip" {
		a := stagingtest.Archive{Body: body, Kind: "-t" + kind, Members: map[string][]byte{}}
		if kind == "rar" {
			a.Kind = "-tRar5"
		}
		for _, file := range files {
			a.Members[file.name] = file.body
		}
		profiles = append(profiles, a)
	}
	writeTree(t, folder, map[string][]byte{outer: body})
	stagingtest.InstallMany(t, profiles...)
	return root, folder, outer, leaf
}

func TestNestedIngestPreservesSourcesExactProvenanceAndRepeat(t *testing.T) {
	for _, kind := range []string{"zip", "rar", "7z"} {
		t.Run(kind, func(t *testing.T) {
			root, folder, outer, _ := nestedFolder(t, kind)
			before := snapshotTree(t, folder)
			initial := snapshotTree(t, root)
			var written map[string][]byte
			for run, apply := range []bool{false, true, true} {
				out, err := Ingest(context.Background(), IngestRequest{Library: root, Folder: folder, Apply: apply, Scan: true, Scanner: cleanScan{}})
				if err != nil || !out.Complete || out.Imported != 2 || out.Checked != 2 || out.Failed != 0 {
					t.Fatal(out, err)
				}
				selections := map[string]bool{}
				for _, item := range out.Items {
					if item.Import != nil {
						selections[item.Source] = true
						if run == 2 && !item.Import.AlreadyHeld {
							t.Fatal("repeat wrote a new fact", item)
						}
					}
				}
				if len(selections) != 2 || !selections[outer+"!/inner.zip!/book.epub"] || !selections[outer+`!//["inner.zip","book.epub"]`] {
					t.Fatal("direct and nested selections collided", selections)
				}
				if !reflect.DeepEqual(before, snapshotTree(t, folder)) || run == 0 && !reflect.DeepEqual(initial, snapshotTree(t, root)) {
					t.Fatal("source or preview wrote bytes")
				}
				if run == 1 {
					written = snapshotTree(t, root)
				}
				if run == 2 && !reflect.DeepEqual(written, snapshotTree(t, root)) {
					t.Fatal("repeat changed library bytes")
				}
			}
			page, err := FindManaged(context.Background(), root, "", "all", "all", 10, 0)
			if err != nil || page.Total != 2 {
				t.Fatal(page, err)
			}
			for _, h := range page.Holdings {
				if len(h.Sources) != 1 || h.Sources[0].Kind != "archive_member" {
					t.Fatal(h)
				}
			}
			if sameImportSource(filepath.Join(folder, outer)+"!/inner.zip!/book.epub", filepath.Join(folder, outer)+`!//["inner.zip","book.epub"]`) {
				t.Fatal("historical provenance was reinterpreted")
			}
			plan, err := Cleanup(context.Background(), CleanupRequest{Library: root, Folder: folder})
			if err != nil || len(plan.Entries) != 1 || plan.Entries[0].Disposition != "keep" || !reflect.DeepEqual(before, snapshotTree(t, folder)) {
				t.Fatal("nested extraction authorized cleanup", plan, err)
			}
		})
	}
}

func nestedIntent(t *testing.T, root, folder, outer string, body []byte) string {
	t.Helper()
	staged := filepath.Join(t.TempDir(), "lost.epub")
	if err := os.WriteFile(staged, body, 0o600); err != nil {
		t.Fatal(err)
	}
	label := filepath.Join(folder, outer) + `!//["inner.zip","book.epub"]`
	if _, err := importLibrary(context.Background(), ImportRequest{Library: root, Source: staged, Apply: true, Label: label}, func(stage string) error {
		if stage == "intent" {
			return errors.New("interrupted nested member")
		}
		return nil
	}); err == nil {
		t.Fatal("interruption ignored")
	}
	if err := os.Remove(staged); err != nil {
		t.Fatal(err)
	}
	return label
}

func TestNestedIngestResumesExactLeafBeforeOtherImports(t *testing.T) {
	root, folder, outer, body := nestedFolder(t, "zip")
	before := snapshotTree(t, folder)
	label := nestedIntent(t, root, folder, outer, body)
	out, err := Ingest(context.Background(), IngestRequest{Library: root, Folder: folder, Apply: true, Scanner: cleanScan{}})
	if err != nil || !out.Complete || out.Imported != 2 {
		t.Fatal(out, err)
	}
	var imported []IngestItem
	for _, item := range out.Items {
		if item.Import != nil {
			imported = append(imported, item)
		}
	}
	if len(imported) != 2 || !imported[0].Import.Resumed || imported[0].Import.Source != label || imported[1].Source != outer+"!/inner.zip!/book.epub" {
		t.Fatal(imported)
	}
	if pending, err := pendingSource(root); err != nil || pending != "" || !reflect.DeepEqual(before, snapshotTree(t, folder)) {
		t.Fatal(pending, err)
	}
}

func TestNestedIngestRecoveryRefusesChangedLeafBeforeOtherImports(t *testing.T) {
	root, folder, outer, _ := nestedFolder(t, "zip")
	old := mustRead(t, writeEPUBParagraph(t, t.TempDir(), "Earlier pending bytes"))
	label := nestedIntent(t, root, folder, outer, old)
	before, libraryBefore := snapshotTree(t, folder), snapshotTree(t, root)
	out, err := Ingest(context.Background(), IngestRequest{Library: root, Folder: folder, Apply: true, Scanner: cleanScan{}})
	if err == nil || out.Complete || out.Imported != 0 || out.Failed != 1 {
		t.Fatal(out, err)
	}
	var attempted []IngestItem
	for _, item := range out.Items {
		if item.Import != nil {
			attempted = append(attempted, item)
		}
	}
	if len(attempted) != 1 || attempted[0].Import.Source != label {
		t.Fatal("attempted another import before recovery", attempted)
	}
	if pending, err := pendingSource(root); err != nil || pending != label || !reflect.DeepEqual(before, snapshotTree(t, folder)) || !reflect.DeepEqual(libraryBefore, snapshotTree(t, root)) {
		t.Fatal(pending, err)
	}
}

type cancelNestedScan struct{ cancel context.CancelFunc }

func (s cancelNestedScan) Scan(context.Context, string) scanner.Result {
	s.cancel()
	return scanner.Result{Status: "incomplete"}
}

func TestNestedIngestCancellationRemovesPrivateTree(t *testing.T) {
	root, folder, _, _ := nestedFolder(t, "zip")
	temp := t.TempDir()
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, temp)
	}
	before := snapshotTree(t, folder)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out, err := Ingest(ctx, IngestRequest{Library: root, Folder: folder, Apply: true, Scan: true, Scanner: cancelNestedScan{cancel}})
	if !errors.Is(err, context.Canceled) || out.Complete || !reflect.DeepEqual(before, snapshotTree(t, folder)) {
		t.Fatal(out, err)
	}
	for _, pattern := range []string{"nemalo-ingest-*", "nemalo-nested-zip-*", "nemalo-archive-*"} {
		if left, _ := filepath.Glob(filepath.Join(temp, pattern)); len(left) != 0 {
			t.Fatal("private tree left behind", left)
		}
	}
}

func TestNestedMemberSelectionEscapesWithoutChangingNames(t *testing.T) {
	name := "sub/quote\"\u202ebook.epub"
	got := archiveMemberSelection(staging.Member{Name: name, Chain: []string{"inner.zip", name}})
	if !strings.Contains(got, `\u202e`) || !strings.Contains(got, `\"`) || strings.ContainsRune(got, '\u202e') {
		t.Fatal(got)
	}
	if direct := archiveMemberSelection(staging.Member{Name: "inner.zip!/book.epub", Chain: []string{"inner.zip!/book.epub"}}); direct != "inner.zip!/book.epub" {
		t.Fatal(direct)
	}
}

func TestNestedRecoveryRetainsIntentWhenLeafCannotBeLocated(t *testing.T) {
	for _, missingTool := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing child", true: "missing reader"}[missingTool], func(t *testing.T) {
			root, folder, outer, body := nestedFolder(t, "zip")
			label := nestedIntent(t, root, folder, outer, body)
			if missingTool {
				saved := staging.SevenZip
				staging.SevenZip = func() (string, error) { return "", staging.ErrNoArchiveTool }
				t.Cleanup(func() { staging.SevenZip = saved })
			} else {
				writeTree(t, folder, map[string][]byte{outer: nestedZIP(t, nestedFile{"other.epub", body})})
			}
			before, libraryBefore := snapshotTree(t, folder), snapshotTree(t, root)
			out, err := Ingest(context.Background(), IngestRequest{Library: root, Folder: folder, Apply: true, Scanner: cleanScan{}})
			if err == nil || out.Complete || out.Imported != 0 {
				t.Fatal(out, err)
			}
			for _, item := range out.Items {
				if item.Import != nil {
					t.Fatal("import before recovering the unavailable leaf", item)
				}
			}
			if pending, err := pendingSource(root); err != nil || pending != label || !reflect.DeepEqual(before, snapshotTree(t, folder)) || !reflect.DeepEqual(libraryBefore, snapshotTree(t, root)) {
				t.Fatal(pending, err)
			}
		})
	}
}

func TestNestedProvenanceLimitRefusesBeforePublication(t *testing.T) {
	ctx := context.Background()
	root, folder := t.TempDir(), t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	body := mustRead(t, writeEPUBParagraph(t, t.TempDir(), "A bounded provenance record"))
	inner := nestedZIP(t, nestedFile{"book.epub", body})
	// These bytes are metadata only. Generated staging names also work on
	// Windows, where this cannot be used as an ordinary filesystem name.
	name := strings.Repeat("\x01", 800) + ".zip"
	writeTree(t, folder, map[string][]byte{"outer.zip": nestedZIP(t, nestedFile{name, inner})})
	stagingtest.InstallMany(t, stagingtest.Archive{Body: inner, Kind: "-tzip"})
	before, libraryBefore := snapshotTree(t, folder), snapshotTree(t, root)
	out, err := Ingest(ctx, IngestRequest{Library: root, Folder: folder, Apply: true, Scanner: cleanScan{}})
	if err == nil || out.Complete || out.Imported != 0 || out.Failed != 1 {
		t.Fatal(out, err)
	}
	if !reflect.DeepEqual(before, snapshotTree(t, folder)) || !reflect.DeepEqual(libraryBefore, snapshotTree(t, root)) {
		t.Fatal("oversized provenance changed source or library state")
	}
}

func TestNestedCRCFailureKeepsIndependentImportsAndSources(t *testing.T) {
	ctx := context.Background()
	root, folder := t.TempDir(), t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	body := mustRead(t, writeEPUBParagraph(t, t.TempDir(), "A damaged nested member"))
	direct := mustRead(t, writeEPUBParagraph(t, t.TempDir(), "An independent good member"))
	inner := nestedZIP(t, nestedFile{"book.epub", body})
	z, err := zip.NewReader(bytes.NewReader(inner), int64(len(inner)))
	if err != nil {
		t.Fatal(err)
	}
	offset, err := z.File[0].DataOffset()
	if err != nil {
		t.Fatal(err)
	}
	inner[offset+int64(len(body)/2)] ^= 1
	writeTree(t, folder, map[string][]byte{"outer.zip": nestedZIP(t, nestedFile{"inner.zip", inner}, nestedFile{"direct.epub", direct})})
	stagingtest.InstallMany(t, stagingtest.Archive{Body: inner, Kind: "-tzip"})
	before := snapshotTree(t, folder)
	var written map[string][]byte
	for run := 0; run < 2; run++ {
		out, err := Ingest(ctx, IngestRequest{Library: root, Folder: folder, Apply: true, Scanner: cleanScan{}})
		if err == nil || out.Complete || out.Imported != 1 || out.Failed != 1 {
			t.Fatal(out, err)
		}
		found := false
		for _, item := range out.Items {
			if item.Source == `outer.zip!//["inner.zip","book.epub"]` && item.Outcome == "failed" && item.Import == nil {
				found = true
			}
		}
		if !found || !reflect.DeepEqual(before, snapshotTree(t, folder)) {
			t.Fatal(out)
		}
		if run == 0 {
			written = snapshotTree(t, root)
		} else if !reflect.DeepEqual(written, snapshotTree(t, root)) {
			t.Fatal("repeat changed library bytes after a member failure")
		}
	}
	page, err := FindManaged(ctx, root, "", "all", "all", 10, 0)
	if err != nil || page.Total != 1 {
		t.Fatal(page, err)
	}
}
