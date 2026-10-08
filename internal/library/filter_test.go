package library

import (
	"context"
	"testing"

	"github.com/blisspixel/nemalo/internal/inventory"
)

func TestFilenameFormatFiltersExcludeReceiptsWithoutCertifyingContent(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{"book.EPUB": "invalid epub bytes", "book.epub.receipt.json": "{}", "paper.pdf": "invalid pdf bytes", "track.mp3": "invalid audio bytes", "notes.txt": "notes"} {
		put(t, root, name, body)
	}
	s, err := Build(context.Background(), root, inventory.Defaults(), false)
	if err != nil {
		t.Fatal(err)
	}
	for format, count := range map[string]int{"all": 5, "epub": 1, "pdf": 1, "mp3": 1} {
		p, err := FindFormat(s.Catalog, "", format, 100, 0)
		if err != nil || p.Total != count || p.Format != format {
			t.Fatal(format, p, err)
		}
		for _, asset := range p.Assets {
			if asset.Health != nil {
				t.Fatal("filename filter manufactured validation")
			}
		}
	}
	if p, err := FindFormat(s.Catalog, "receipt", "epub", 10, 0); err != nil || p.Total != 0 {
		t.Fatal("receipt mistaken for EPUB", p, err)
	}
	for _, format := range []string{"", "EPUB", "zip", "exe"} {
		if _, err := FindFormat(s.Catalog, "", format, 10, 0); err == nil {
			t.Fatal("unknown format accepted", format)
		}
	}
}
