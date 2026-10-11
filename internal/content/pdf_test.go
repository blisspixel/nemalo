package content

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/library"
)

// pdfFixture writes a three-page PDF: roman-labelled front matter, a body page
// with an image between two paragraphs, and a scanned page with no text layer.
func pdfFixture(t *testing.T) []byte {
	t.Helper()
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R /PageLabels << /Nums [0 << /S /r >> 1 << /S /D >>] >> >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R 5 0 R] /Count 3 /Resources << /Font << /F1 6 0 R >> /XObject << /Im1 7 0 R >> >> >>",
		"<< /Type /Page /Parent 2 0 R /Contents 8 0 R >>",
		"<< /Type /Page /Parent 2 0 R /Contents 9 0 R >>",
		"<< /Type /Page /Parent 2 0 R /Contents 10 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /Length 1 >>\nstream\n\x00\nendstream",
	}
	for _, content := range []string{
		"BT /F1 12 Tf 72 700 Td (Preface to the reader.) Tj ET",
		"BT /F1 12 Tf 72 700 Td (The first finding is stated here.) Tj ET /Im1 Do BT /F1 12 Tf 72 400 Td (Figure 1 shows the result.) Tj ET",
		"q 600 0 0 800 0 0 cm /Im1 Do Q",
	} {
		objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := []int{}
	for i, body := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}

func importPDF(t *testing.T, data []byte) (string, string) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	if _, err := library.Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "paper.pdf")
	if err := os.WriteFile(source, data, 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := library.Import(ctx, library.ImportRequest{Library: root, Source: source, Apply: true})
	if err != nil {
		t.Fatal(r, err)
	}
	return root, r.AssetID
}

func TestPDFPagesKeepPhysicalIndexLabelsAndLocatedGaps(t *testing.T) {
	ctx := context.Background()
	root, asset := importPDF(t, pdfFixture(t))
	base := Request{SchemaVersion: 1, Catalog: root, Root: root, AssetID: asset, MaxBytes: 4096, Representation: "pdf-text/1"}
	units := base
	units.UnitsOnly = true
	got, err := Get(ctx, units)
	if err != nil || got.Status != "units_available" || len(got.Units) != 3 {
		t.Fatal(got, err)
	}
	for i, want := range []struct{ label, extraction string }{{"i", "supported_text"}, {"1", "supported_text_with_gaps"}, {"2", "no_text_layer"}} {
		u := got.Units[i]
		if u.Index != i || u.Kind != "pdf_page" || u.PageLabel != want.label || u.Extraction != want.extraction {
			t.Fatal(i, u)
		}
	}
	// Read the body page in small ranges: the image gap is disclosed once, at
	// its offset, and the continuation crosses to the scanned page.
	read := base
	read.Unit, read.MaxBytes = 1, 20
	seen, text := 0, ""
	for step := 0; step < 20; step++ {
		r, err := Get(ctx, read)
		if err != nil {
			t.Fatal(r, err)
		}
		if r.Locator.Unit != 1 {
			if r.Locator.Unit != 2 || r.Locator.PageLabel != "2" || r.Text != "" || len(r.Gaps) != 2 || r.Gaps[1].Element != "no_text_layer" || !r.EndOfSource {
				t.Fatal("scanned page", r)
			}
			break
		}
		if r.Locator.PageLabel != "1" {
			t.Fatal("label", r.Locator)
		}
		text += r.Text
		for _, g := range r.Gaps {
			if g.Element == "image" && g.Reason == "Im1" && g.Offset == len("The first finding is stated here.") {
				seen++
			}
		}
		read = base
		read.Cursor, read.Representation = r.Continuation, ""
	}
	if text != "The first finding is stated here.\nFigure 1 shows the result." || seen != 1 {
		t.Fatalf("%q %d", text, seen)
	}
	// A pdf-text continuation cannot address the EPUB default representation.
	if r, err := Get(ctx, base); err != nil || r.Continuation == "" {
		t.Fatal(r, err)
	} else {
		wrong := base
		wrong.Cursor, wrong.Representation = r.Continuation, "epub-text/2"
		if r, err := Get(ctx, wrong); err == nil || r.Status != "stale_reference" {
			t.Fatal("cross-representation cursor accepted", r, err)
		}
	}
	plain := base
	plain.Representation = ""
	if r, err := Get(ctx, plain); err == nil || r.Status != "unsupported_extraction" || !strings.Contains(err.Error(), "pdf-text/1") {
		t.Fatal("default text representation did not point to pdf-text/1", r, err)
	}
}

func TestLocateQuotationsByPageAndOffset(t *testing.T) {
	ctx := context.Background()
	root, asset := importPDF(t, pdfFixture(t))
	find := Request{SchemaVersion: 1, Catalog: root, Root: root, AssetID: asset, MaxBytes: 4096, Representation: "pdf-text/1", Find: "result"}
	got, err := Get(ctx, find)
	if err != nil || got.Status != "matches_found" || len(got.Matches) != 1 {
		t.Fatal(got, err)
	}
	m := got.Matches[0]
	if m.Unit != 1 || m.PageLabel != "1" || m.End-m.Start != len("result") {
		t.Fatal(m)
	}
	// The match cursor reads the quotation back exactly.
	back := Request{SchemaVersion: 1, Catalog: root, Root: root, AssetID: asset, MaxBytes: len("result"), Cursor: m.Cursor}
	r, err := Get(ctx, back)
	if err != nil || r.Text != "result" || r.Locator.Start != m.Start || r.Locator.RepresentationID != got.RepresentationID {
		t.Fatal(r, err)
	}
	find.Find = "absent phrase"
	if got, err := Get(ctx, find); err != nil || got.Status != "no_matches" || len(got.Matches) != 0 {
		t.Fatal(got, err)
	}
	// EPUB text locates the same way.
	epubRoot := t.TempDir()
	if _, err := library.Initialize(ctx, epubRoot); err != nil {
		t.Fatal(err)
	}
	imported, err := library.Import(ctx, library.ImportRequest{Library: epubRoot, Source: writeContentEPUB(t, t.TempDir()), Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	e, err := Get(ctx, Request{SchemaVersion: 1, Catalog: epubRoot, Root: epubRoot, AssetID: imported.AssetID, MaxBytes: 4096, Find: "ell"})
	if err != nil || len(e.Matches) != 1 || e.Matches[0].Member != "chapter.xhtml" || e.Matches[0].Start != 1 {
		t.Fatal(e, err)
	}
	for _, bad := range []Request{
		{Find: "x", Cursor: m.Cursor},
		{Find: "x", Representation: "epub-structure/1"},
		{Find: strings.Repeat("x", 1001)},
		{Find: "\xff"},
	} {
		bad.SchemaVersion, bad.Catalog, bad.Root, bad.AssetID, bad.MaxBytes = 1, root, root, asset, 4096
		if err := bad.Validate(); err == nil {
			t.Fatal("invalid find accepted", bad.Find, bad.Representation)
		}
	}
}

func TestLocateStopsAtTheMatchBudget(t *testing.T) {
	req := Request{AssetID: "sha256:" + strings.Repeat("0", 64), Find: "ab"}
	got, err := locate(context.Background(), req, Result{Extractor: "x", Representation: "pdf-text/1", Units: []Unit{{Index: 0}}}, []Unit{{Index: 0, PageLabel: "1"}}, []string{strings.Repeat("ab ", 200)})
	if err != nil || got.Status != "matches_truncated" || len(got.Matches) != maxMatches || got.Matches[99].Start != 99*3 || got.Units != nil {
		t.Fatal(got.Status, len(got.Matches), err)
	}
}

func TestMalformedAndHostilePDFsFailExplicitly(t *testing.T) {
	ctx := context.Background()
	for name, data := range map[string][]byte{
		"truncated": pdfFixture(t)[:200],
		"no pages":  []byte("%PDF-1.7\n1 0 obj\n<< /Type /Catalog >>\nendobj\nxref\n0 2\n0000000000 65535 f \n0000000009 00000 n \ntrailer\n<< /Size 2 /Root 1 0 R >>\nstartxref\n47\n%%EOF\n"),
	} {
		root, asset := importPDF(t, data)
		r, err := Get(ctx, Request{SchemaVersion: 1, Catalog: root, Root: root, AssetID: asset, MaxBytes: 4096, Representation: "pdf-text/1"})
		if err == nil || r.Status != "malformed_content" || r.Text != "" {
			t.Fatal(name, r.Status, err)
		}
	}
}
