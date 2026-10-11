package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFindManagedFiltersLatestHoldings(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := FindManaged(ctx, root, "", "all", "all", 10, 0); err == nil {
		t.Fatal("uninitialized library listed")
	}
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	empty, err := FindManaged(ctx, root, "", "all", "all", 10, 0)
	if err != nil || empty.Total != 0 || empty.Holdings == nil || empty.LibraryID == "" {
		t.Fatal(empty, err)
	}
	books := importUnscanned(t, root, "Alpha", "Beta", "Gamma")
	pdf := filepath.Join(t.TempDir(), "paper.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF-1.7\n%%EOF"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Import(ctx, ImportRequest{Library: root, Source: pdf, Apply: true}); err != nil {
		t.Fatal(err)
	}
	// Scanning one book appends a newer holding line; the list shows the latest.
	if _, err := Import(ctx, ImportRequest{Library: root, Source: books[1].Source, Apply: true, Scan: true, Scanner: cleanScan{}}); err != nil {
		t.Fatal(err)
	}
	all, err := FindManaged(ctx, root, "", "all", "all", 10, 0)
	if err != nil || all.Total != 4 || len(all.Holdings) != 4 || all.Holdings[0].ID != books[0].AssetID {
		t.Fatal("listing order or count", all, err)
	}
	for _, tc := range []struct {
		query, format, status string
		want                  int
	}{
		{"", "epub", "all", 3},
		{"", "pdf", "all", 1},
		{"", "all", "checked", 1},
		{"", "epub", "review", 2},
		{"UNSCANNED", "all", "all", 2},
		{books[2].SHA256[:12], "all", "all", 1},
		{"paper.pdf", "all", "all", 1},
		{"no such title", "all", "all", 0},
	} {
		p, err := FindManaged(ctx, root, tc.query, tc.format, tc.status, 10, 0)
		if err != nil || p.Total != tc.want || len(p.Holdings) != tc.want {
			t.Fatal(tc, p.Total, err)
		}
	}
	page, err := FindManaged(ctx, root, "", "all", "all", 2, 1)
	if err != nil || page.Total != 4 || len(page.Holdings) != 2 || page.Holdings[0].ID != books[1].AssetID || page.Holdings[0].Status != "checked" {
		t.Fatal("paging", page, err)
	}
	for _, bad := range []struct {
		format, status string
		limit, offset  int
	}{{"exe", "all", 10, 0}, {"all", "pending", 10, 0}, {"all", "all", 0, 0}, {"all", "all", 101, 0}, {"all", "all", 10, -1}} {
		if _, err := FindManaged(ctx, root, "", bad.format, bad.status, bad.limit, bad.offset); err == nil {
			t.Fatal("invalid filter accepted", bad)
		}
	}
}
