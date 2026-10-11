package pdftext

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/blisspixel/nemalo/internal/pdftext/pdftest"
)

var build, stream = pdftest.Build, pdftest.Stream

func flate(data string) string {
	var b bytes.Buffer
	w := zlib.NewWriter(&b)
	_, _ = w.Write([]byte(data))
	_ = w.Close()
	return stream("/Filter /FlateDecode", b.String())
}

const helvetica = "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>"

func extract(t *testing.T, data []byte) Document {
	t.Helper()
	doc, err := Extract(context.Background(), data, Defaults())
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func gapKinds(p Page) string {
	kinds := []string{}
	for _, g := range p.Gaps {
		kinds = append(kinds, g.Kind)
	}
	return strings.Join(kinds, ",")
}

func TestPagesLabelsAndTextInStreamOrder(t *testing.T) {
	data := build(
		"<< /Type /Catalog /Pages 2 0 R /PageLabels << /Nums [0 << /S /r >> 2 << /S /D /P (p-) /St 7 >>] >> >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R 5 0 R] /Count 3 /Resources << /Font << /F1 6 0 R >> >> >>",
		"<< /Type /Page /Parent 2 0 R /Contents 7 0 R >>",
		"<< /Type /Page /Parent 2 0 R /Contents 8 0 R >>",
		"<< /Type /Page /Parent 2 0 R /Contents 9 0 R >>",
		helvetica,
		stream("", "BT /F1 12 Tf 72 700 Td (Preface) Tj 0 -14 Td (caf\\351 \\223quoted\\224) Tj ET"),
		flate("BT /F1 12 Tf 72 700 Td [(Hello)-300(World)] TJ T* (Next line) Tj ET"),
		stream("", "BT /F1 12 Tf 1 0 0 1 72 700 Tm (Chapter) Tj 1 0 0 1 150 700 Tm (one) Tj ET"),
	)
	doc := extract(t, data)
	if !doc.LabelsDeclared || len(doc.Pages) != 3 {
		t.Fatal(doc)
	}
	for i, want := range []struct{ label, text string }{
		{"i", "Preface\ncafé “quoted”"},
		{"ii", "Hello World\nNext line"},
		{"p-7", "Chapter one"},
	} {
		p := doc.Pages[i]
		if p.Index != i || p.Label != want.label || p.Text != want.text || len(p.Gaps) != 0 {
			t.Fatalf("page %d: %q %q %v", i, p.Label, p.Text, p.Gaps)
		}
	}
}

func TestUnlabelledPagesAndLabelStyles(t *testing.T) {
	data := build(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R >>",
	)
	doc := extract(t, data)
	if doc.LabelsDeclared || doc.Pages[0].Label != "" || doc.Pages[0].Text != "" {
		t.Fatal(doc)
	}
	for _, c := range []struct {
		style string
		n     int64
		want  string
	}{{"R", 1994, "MCMXCIV"}, {"a", 1, "a"}, {"a", 27, "aa"}, {"A", 28, "BB"}, {"D", 0, "0"}, {"", 5, ""}, {"r", 200000, "200000"}} {
		if got := numberLabel(c.style, c.n); got != c.want {
			t.Fatal(c, got)
		}
	}
}

func TestImagesAndScannedPagesAreLocatedGaps(t *testing.T) {
	data := build(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /Resources << /XObject << /Im1 5 0 R >> >> /Contents 6 0 R >>",
		"<< /Type /Page /Parent 2 0 R /Resources << /XObject << /Im1 5 0 R >> /Font << /F1 7 0 R >> >> /Contents 8 0 R >>",
		stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8", "\x00"),
		stream("", "q 100 0 0 100 0 0 cm /Im1 Do Q BI /W 1 /H 1 /BPC 8 /CS /G ID \xff\x00EI\x01 EI Q"),
		helvetica,
		stream("", "BT /F1 12 Tf (Figure) Tj ET /Im1 Do BT /F1 12 Tf 0 -20 Td (Caption) Tj ET"),
	)
	doc := extract(t, data)
	scanned, figure := doc.Pages[0], doc.Pages[1]
	if scanned.Images != 2 || gapKinds(scanned) != "image,image,no_text_layer" || scanned.Gaps[1].Detail != "inline image" {
		t.Fatal("scanned page", scanned.Gaps, scanned.Text)
	}
	if figure.Text != "Figure\nCaption" || gapKinds(figure) != "image" || figure.Gaps[0].Offset != len("Figure") || figure.Gaps[0].Detail != "Im1" {
		t.Fatal("figure page", figure.Text, figure.Gaps)
	}
}

func TestFontsMapThroughToUnicodeAndReportUnmappedCodes(t *testing.T) {
	cmap := stream("", `/CIDInit /ProcSet findresource begin 12 dict begin begincmap
1 begincodespacerange <0000> <FFFF> endcodespacerange
2 beginbfchar <0001> <65E5> <0002> <672C> endbfchar
1 beginbfrange <0010> <0012> <0061> endbfrange
1 beginbfrange <0020> <0021> [<0058> <D835DC00>] endbfrange
endcmap end end`)
	data := build(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /Resources << /Font << /F1 4 0 R /F2 6 0 R /F3 7 0 R /F4 8 0 R /F5 9 0 R >> >> /Contents 10 0 R >>",
		"<< /Type /Font /Subtype /Type0 /BaseFont /Mincho /Encoding /Identity-H /ToUnicode 5 0 R >>",
		cmap,
		"<< /Type /Font /Subtype /Type0 /BaseFont /NoMap /Encoding /Identity-H >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /TeX /Encoding << /Differences [65 /fi /uni00E9 /bogusglyph /a.sc] >> >>",
		"<< /Type /Font /Subtype /Type3 /Encoding << /Differences [65 /square] >> >>",
		"<< /Type /Font /Subtype /Type0 /BaseFont /Song /Encoding /UniGB-UCS2-H >>",
		stream("", "BT /F1 1 Tf <000100020010001100120020 0021 0099> Tj T* /F2 1 Tf <00410042> Tj T* /F3 1 Tf (ABCD) Tj T* /F4 1 Tf (A) Tj T* /F5 1 Tf <4E2D6587> Tj ET"),
	)
	p := extract(t, data).Pages[0]
	if p.Text != "日本abcX𝐀\nﬁéa\n中文" {
		t.Fatalf("%q", p.Text)
	}
	want := []struct {
		detail string
		count  int
	}{{"Mincho", 1}, {"NoMap", 2}, {"TeX", 1}, {"F4", 1}}
	if len(p.Gaps) != len(want) {
		t.Fatal(p.Gaps)
	}
	for i, w := range want {
		if p.Gaps[i].Kind != "unmapped_text" || !strings.HasPrefix(p.Gaps[i].Detail, w.detail+": ") || p.Gaps[i].Count != w.count {
			t.Fatal(i, p.Gaps[i], w)
		}
	}
	if !strings.HasPrefix(p.Gaps[1].Detail, "NoMap: composite font without a ToUnicode map") || !strings.Contains(p.Gaps[3].Detail, "Type3") {
		t.Fatal("gap reasons", p.Gaps)
	}
}

func TestFormsRecurseOnceAndCyclesAreRefused(t *testing.T) {
	data := build(
		"<< /Type /Catalog /Pages 2 0 R /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 3 0 R 6 0 R] /Count 3 >>",
		"<< /Type /Page /Parent 2 0 R /Resources << /XObject << /Fm 4 0 R >> /Font << /F1 5 0 R >> >> /Contents 7 0 R >>",
		stream("/Type /XObject /Subtype /Form /BBox [0 0 1 1] /Resources << /XObject << /Fm 4 0 R >> /Font << /F1 5 0 R >> >>", "BT /F1 1 Tf (inside) Tj ET /Fm Do"),
		helvetica,
		"<< /Type /Pages /Kids [2 0 R] /Count 1 >>",
		stream("", "/Fm Do"),
	)
	doc := extract(t, data)
	if len(doc.Pages) != 1 || len(doc.Findings) == 0 {
		t.Fatal("cycle or repeated page not refused", len(doc.Pages), doc.Findings)
	}
	p := doc.Pages[0]
	if p.Text != "inside" || gapKinds(p) != "form_not_read" {
		t.Fatal(p.Text, p.Gaps)
	}
}

func TestRefusalsAndBudgets(t *testing.T) {
	ctx := context.Background()
	if _, err := Extract(ctx, []byte("PK\x03\x04"), Defaults()); !errors.Is(err, ErrNotPDF) {
		t.Fatal(err)
	}
	if _, err := Extract(ctx, []byte("%PDF-1.7\nnot really\n%%EOF"), Defaults()); !errors.Is(err, ErrMalformed) {
		t.Fatal(err)
	}
	ops := strings.Repeat("BT ET ", 1000)
	data := build(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /Contents 4 0 R >>",
		stream("", ops),
	)
	limits := Defaults()
	limits.Operators = 100
	if _, err := Extract(ctx, data, limits); !errors.Is(err, ErrBudget) {
		t.Fatal("operator budget", err)
	}
	limits = Defaults()
	limits.StreamBytes = 10
	if _, err := Extract(ctx, data, limits); !errors.Is(err, ErrBudget) {
		t.Fatal("stream budget", err)
	}
	limits = Defaults()
	limits.Pages = 0
	if _, err := Extract(ctx, data, limits); !errors.Is(err, ErrBudget) {
		t.Fatal("page budget", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Extract(cancelled, data, Defaults()); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation", err)
	}
}

func TestGapsPerPageAreBounded(t *testing.T) {
	var content strings.Builder
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&content, "/Im%d Do (x) Tj ", i%2)
	}
	data := build(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /Resources << /XObject << /Im0 4 0 R /Im1 4 0 R >> /Font << /F1 5 0 R >> >> /Contents 6 0 R >>",
		stream("/Subtype /Image /Width 1 /Height 1", "\x00"),
		helvetica,
		stream("", "BT /F1 1 Tf "+content.String()+" ET"),
	)
	limits := Defaults()
	limits.GapsPerPage = 10
	doc, err := Extract(context.Background(), data, limits)
	if err != nil {
		t.Fatal(err)
	}
	p := doc.Pages[0]
	last := p.Gaps[len(p.Gaps)-1]
	total := 0
	for _, g := range p.Gaps {
		if g.Kind == "image" {
			total += g.Count
		}
	}
	if len(p.Gaps) != 11 || last.Kind != "more_gaps" || total+last.Count != 50 {
		t.Fatal("gap bound", len(p.Gaps), last, total)
	}
}

func FuzzExtract(f *testing.F) {
	f.Add(build(
		"<< /Type /Catalog /Pages 2 0 R /PageLabels << /Nums [0 << /S /R >>] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		helvetica,
		stream("", "BT /F1 12 Tf (Hi) Tj [(a) -500 (b)] TJ ET"),
	))
	f.Add([]byte("%PDF-1.4\n1 0 obj << >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF"))
	f.Fuzz(func(t *testing.T, data []byte) {
		limits := Defaults()
		limits.Operators, limits.StreamBytes, limits.TotalStreamBytes = 100000, 1<<20, 4<<20
		doc, err := Extract(context.Background(), data, limits)
		if err != nil {
			return
		}
		for _, p := range doc.Pages {
			for _, g := range p.Gaps {
				if g.Offset < 0 || g.Offset > len(p.Text) {
					t.Fatal("gap offset outside the page text", g, len(p.Text))
				}
			}
		}
	})
}

func TestLexerEscapesAndInlineImages(t *testing.T) {
	l := &lexer{data: []byte(`(a\(b\)c\\\101\12) <48 65 6C6C6F7> /A#20B 12.5 -3 [ ] << >> % comment` + "\nTj")}
	want := []token{{kind: tokString, text: "a(b)c\\A\n"}, {kind: tokString, text: "Hello\x70"}, {kind: tokName, text: "A B"}, {kind: tokNumber, num: 12.5}, {kind: tokNumber, num: -3}, {kind: tokArrayStart}, {kind: tokArrayEnd}, {kind: tokDictStart}, {kind: tokDictEnd}, {kind: tokOperator, text: "Tj"}, {kind: tokEOF}}
	for i, w := range want {
		got, err := l.next()
		if err != nil || got != w {
			t.Fatal(i, got, err)
		}
	}
	bad := &lexer{data: []byte("(unterminated")}
	if _, err := bad.next(); err == nil {
		t.Fatal("unterminated string accepted")
	}
}

// Regressions from review: each case once cost quadratic time, wrong text, or
// an offset inside a character.
func TestReviewRegressions(t *testing.T) {
	ctx := context.Background()
	page := func(resources, content string) []byte {
		return build(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /Resources "+resources+" /Contents 4 0 R >>",
			stream("", content),
		)
	}
	fonts := "<< /Font << /F1 " + helvetica + " >> /XObject << /Im1 << /Subtype /Image /Width 1 /Height 1 >> >> >>"

	// Many line breaks on a long page stay linear.
	start := time.Now()
	doc := extract(t, page(fonts, "BT /F1 1 Tf "+strings.Repeat("(abcdefghij) Tj T* ", 80000)+"ET"))
	if time.Since(start) > 5*time.Second || len(doc.Pages[0].Text) != 80000*11-1 {
		t.Fatal("line breaks", time.Since(start), len(doc.Pages[0].Text))
	}

	// Inline font dictionaries do not share one cache entry.
	inline := "<< /Font << /F1 << /Subtype /Type3 >> /F2 " + helvetica + " >> >>"
	if p := extract(t, page(inline, "BT /F1 1 Tf (x) Tj /F2 1 Tf (Hello) Tj ET")).Pages[0]; p.Text != "Hello" {
		t.Fatalf("inline fonts %q", p.Text)
	}

	// A gap recorded before trailing spaces keeps a character boundary.
	p := extract(t, page(fonts, "BT /F1 1 Tf (a  ) Tj ET /Im1 Do BT /F1 1 Tf T* (\351) Tj ET")).Pages[0]
	if g := p.Gaps[0].Offset; g > len(p.Text) || !utf8.RuneStart(p.Text[g]) && g != len(p.Text) {
		t.Fatalf("gap offset %d splits %q", g, p.Text)
	}

	// TJ without its own array shows nothing.
	if p := extract(t, page(fonts, "BT /F1 1 Tf [(Hello)] TJ 0 -12 Td TJ ET")).Pages[0]; p.Text != "Hello" {
		t.Fatalf("TJ replay %q", p.Text)
	}

	// A huge ToUnicode map and a long string stay within budget and deadline.
	var cmap strings.Builder
	cmap.WriteString("1 begincodespacerange <0000> <FFFF> endcodespacerange\n")
	for i := 0; i < 60000; i++ {
		fmt.Fprintf(&cmap, "1 beginbfrange <%04X> <%04X> <0041> endbfrange\n", i, i)
	}
	big := build(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		stream("", "BT /F1 1 Tf <"+strings.Repeat("FFFF", 200000)+"> Tj ET"),
		"<< /Type /Font /Subtype /Type0 /Encoding /Identity-H /ToUnicode 6 0 R >>",
		stream("", cmap.String()),
	)
	deadline, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	start = time.Now()
	if _, err := Extract(deadline, big, Defaults()); err != nil && !errors.Is(err, ErrBudget) {
		t.Fatal(err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("ToUnicode lookups too slow", time.Since(start))
	}

	// A bfrange cannot carry its destination past the last byte.
	m := parseCMap([]byte("1 begincodespacerange <0000> <FFFF> endcodespacerange 1 beginbfrange <0000> <01FF> <00F0> endbfrange"))
	if text, ok, _ := m.lookup(0x000F, 2); !ok || text != "ÿ" {
		t.Fatal("in-range", text, ok)
	}
	if _, ok, _ := m.lookup(0x0010, 2); ok {
		t.Fatal("wrapped destination mapped")
	}

	// Repeated and self-referencing kids are refused once, with bounded findings.
	kids := strings.Repeat("3 0 R ", 300000)
	tree := build(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [2 0 R "+kids+"] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R >>",
	)
	facts, err := Inspect(ctx, tree, Defaults())
	if err != nil || facts.Pages != 1 || !facts.Partial || len(facts.Findings) != 1 {
		t.Fatal("repeated kids", facts.Pages, facts.Partial, len(facts.Findings), err)
	}
}
