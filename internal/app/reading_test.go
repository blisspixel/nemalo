package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/pdftext/pdftest"
)

func TestTextUnitsTriesEachTextRepresentation(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := library.Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	hold := func(name string, data []byte) string {
		src := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(src, data, 0o600); err != nil {
			t.Fatal(err)
		}
		r, err := library.Import(ctx, library.ImportRequest{Library: root, Source: src, Apply: true})
		if err != nil {
			t.Fatal(err)
		}
		return r.AssetID
	}
	s := Service{}
	paper := hold("paper.pdf", pdftest.Pages("", "Text."))
	r, err := s.TextUnits(ctx, root, root, paper)
	if err != nil || r.Representation != "pdf-text/1" || len(r.Units) != 1 || r.Text != "" {
		t.Fatal(r.Representation, r.Units, err)
	}
	// Bytes neither representation reads report both failures.
	marker := hold("marker.pdf", []byte("%PDF-1.7\n%%EOF"))
	if _, err := s.TextUnits(ctx, root, root, marker); err == nil || !strings.Contains(err.Error(), "epub-text/2: ") || !strings.Contains(err.Error(), "pdf-text/1: ") {
		t.Fatal(err)
	}
	// A missing asset is not retried as another representation.
	if _, err := s.TextUnits(ctx, root, root, "sha256:"+strings.Repeat("0", 64)); err == nil || strings.Contains(err.Error(), "pdf-text/1") {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.TextUnits(cancelled, root, root, paper); err == nil || strings.Contains(err.Error(), "pdf-text/1") {
		t.Fatal("cancellation must stop at the first attempt", err)
	}
}
