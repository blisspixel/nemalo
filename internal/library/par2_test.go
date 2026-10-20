package library

import (
	"context"
	"reflect"
	"testing"

	"github.com/blisspixel/nemalo/internal/inventory/inventorytest"
)

func TestPAR2SourcesAndArchiveMembersStayPreserved(t *testing.T) {
	ctx := context.Background()
	root, folder := t.TempDir(), t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	book := mustRead(t, writeEPUBParagraph(t, t.TempDir(), "A book beside recovery material"))
	recovery := inventorytest.File(inventorytest.Main(1, 4096, 1, 2), inventorytest.Description(1, 2, []byte("book.epub"), uint64(len(book))), inventorytest.Creator(1, "synthetic"))
	writeTree(t, folder, map[string][]byte{
		"book.epub": book, "book.par2": recovery, "same-bytes.par2": book,
		"bundle.zip": nestedZIP(t, nestedFile{"book.epub", book}, nestedFile{"book.par2", recovery}),
	})
	before := snapshotTree(t, folder)
	out, err := Ingest(ctx, IngestRequest{Library: root, Folder: folder, Apply: true, Scan: true, Scanner: cleanScan{}})
	if err != nil || !out.Complete || out.Imported != 2 || out.Failed != 0 {
		t.Fatal(out, err)
	}
	want := map[string]bool{"book.par2": false, "same-bytes.par2": false, "bundle.zip!/book.par2": false}
	for _, item := range out.Items {
		if _, recovery := want[item.Source]; recovery {
			if item.Import != nil || item.Outcome != "not_imported" || item.Detail == "" {
				t.Fatal("recovery data was imported or unaccounted", item)
			}
			want[item.Source] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatal("recovery entry disappeared", name, out)
		}
	}
	page, err := FindManaged(ctx, root, "", "all", "all", 10, 0)
	if err != nil || page.Total != 1 || len(page.Holdings[0].Sources) != 2 {
		t.Fatal(page, err)
	}
	plan, err := Cleanup(ctx, CleanupRequest{Library: root, Folder: folder})
	if err != nil {
		t.Fatal(plan, err)
	}
	for _, name := range []string{"book.par2", "same-bytes.par2", "bundle.zip"} {
		if entry := entryFor(t, plan, name); entry.Disposition != "keep" || entry.Reason == "" {
			t.Fatal("recovery material authorized cleanup", entry)
		}
	}
	if !reflect.DeepEqual(before, snapshotTree(t, folder)) {
		t.Fatal("recovery source bytes changed")
	}
}
