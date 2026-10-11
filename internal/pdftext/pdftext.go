// Package pdftext reads the page tree, printed page labels, and content-stream
// text of an untrusted PDF without rendering it. Text is reported in content
// stream order; reading order across columns, layout, and semantic completeness
// are not reconstructed. Codes a font cannot map, images, and anything else
// left out of the text are reported as located gaps, never guessed.
package pdftext

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/digitorus/pdf"
)

// Extractor names these rules. A change to the output needs a new name.
const Extractor = "nemalo.pdf.text/1"

var (
	ErrNotPDF    = errors.New("not a PDF file")
	ErrEncrypted = errors.New("the PDF requires a password; its text was not extracted")
	ErrBudget    = errors.New("PDF exceeds an extraction budget")
	ErrMalformed = errors.New("malformed PDF")
)

// Gap locates something left out of a page's text. Offset is the UTF-8 byte
// position in the page text where it occurred; the omission is zero-width.
type Gap struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail,omitempty"`
	Offset int    `json:"offset"`
	Count  int    `json:"count,omitempty"`
}

// Page is one leaf of the page tree. Index is the zero-based physical page
// index. Label is the printed page label from the document's page labels, or
// empty when the document declares none for it.
type Page struct {
	Index int    `json:"index"`
	Label string `json:"label,omitempty"`
	Text  string `json:"-"`
	Gaps  []Gap  `json:"gaps"`
	// Images counts image XObjects and inline images drawn on the page.
	Images int `json:"images"`
}

// Document is the extraction of one PDF.
type Document struct {
	Version        string   `json:"version"`
	Pages          []Page   `json:"pages"`
	LabelsDeclared bool     `json:"labels_declared"`
	Findings       []string `json:"findings"`
}

// Limits bound one extraction. Defaults returns the production values.
type Limits struct {
	Pages            int
	StreamBytes      int64
	TotalStreamBytes int64
	TextBytes        int
	PageTextBytes    int
	// Operators bounds content-stream operators plus font code lookups.
	Operators   int
	FormDepth   int
	GapsPerPage int
}

func Defaults() Limits {
	return Limits{Pages: 10000, StreamBytes: 16 << 20, TotalStreamBytes: 256 << 20, TextBytes: 16 << 20, PageTextBytes: 8 << 20, Operators: 20_000_000, FormDepth: 8, GapsPerPage: 200}
}

// Extract reads data as a PDF. Panics from the object parser are recovered as
// ErrMalformed; budgets return ErrBudget; cancellation returns ctx.Err().
func Extract(ctx context.Context, data []byte, limits Limits) (doc Document, err error) {
	defer func() {
		if e := recover(); e != nil {
			doc, err = Document{}, fmt.Errorf("%w: %v", ErrMalformed, e)
		}
	}()
	r, src, err := open(ctx, data)
	if err != nil {
		return doc, err
	}
	doc = Document{Version: r.PDFVersion, Pages: []Page{}, Findings: []string{}}
	x := newExtractor(ctx, limits, src)
	root := r.Trailer().Key("Root")
	if root.Kind() != pdf.Dict {
		return doc, fmt.Errorf("%w: no document catalog", ErrMalformed)
	}
	leaves, err := x.pageTree(root.Key("Pages"))
	if err != nil {
		return doc, err
	}
	labels, declared, labelFindings := pageLabels(root.Key("PageLabels"), len(leaves))
	doc.LabelsDeclared = declared
	doc.Findings = append(doc.Findings, labelFindings...)
	doc.Findings = append(doc.Findings, x.findings...)
	for i, leaf := range leaves {
		if err := ctx.Err(); err != nil {
			return doc, err
		}
		p := Page{Index: i, Label: labels[i], Gaps: []Gap{}}
		if err := x.page(leaf, &p); err != nil {
			return doc, err
		}
		doc.Pages = append(doc.Pages, p)
	}
	if src.err != nil {
		return doc, ErrBudget
	}
	return doc, nil
}

// Facts are what the page tree and catalog establish without reading page
// content: the physical page count, whether page labels are declared, and
// findings about the tree.
type Facts struct {
	Version        string `json:"version"`
	Pages          int    `json:"pages"`
	LabelsDeclared bool   `json:"labels_declared"`
	FirstLabel     string `json:"first_label,omitempty"`
	LastLabel      string `json:"last_label,omitempty"`
	// Partial means the page tree walk skipped nodes, so Pages may undercount.
	Partial  bool     `json:"partial"`
	Findings []string `json:"findings"`
}

// Inspect walks the page tree and page labels only. It applies the same
// object-number guard, read budget, and panic recovery as Extract.
func Inspect(ctx context.Context, data []byte, limits Limits) (facts Facts, err error) {
	defer func() {
		if e := recover(); e != nil {
			facts, err = Facts{}, fmt.Errorf("%w: %v", ErrMalformed, e)
		}
	}()
	r, src, err := open(ctx, data)
	if err != nil {
		return facts, err
	}
	x := newExtractor(ctx, limits, src)
	root := r.Trailer().Key("Root")
	if root.Kind() != pdf.Dict {
		return facts, fmt.Errorf("%w: no document catalog", ErrMalformed)
	}
	leaves, err := x.pageTree(root.Key("Pages"))
	if err != nil {
		return facts, err
	}
	if len(leaves) == 0 {
		return facts, fmt.Errorf("%w: the page tree has no pages", ErrMalformed)
	}
	labels, declared, findings := pageLabels(root.Key("PageLabels"), len(leaves))
	facts = Facts{Version: r.PDFVersion, Pages: len(leaves), LabelsDeclared: declared, FirstLabel: labels[0], LastLabel: labels[len(labels)-1], Partial: x.partial, Findings: append(append([]string{}, x.findings...), findings...)}
	if src.err != nil {
		return Facts{}, ErrBudget
	}
	return facts, nil
}

// open applies the pre-parse guards and opens the object parser.
func open(ctx context.Context, data []byte) (*pdf.Reader, *budgetReader, error) {
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return nil, nil, ErrNotPDF
	}
	// The object parser sizes its cross-reference table from declared object
	// numbers, so a tiny file could otherwise make it allocate for millions.
	if !xrefBounded(data, min(int64(len(data))/2+1024, maxObjectNumber)) {
		return nil, nil, ErrBudget
	}
	src := &budgetReader{ctx: ctx, data: data, left: int64(len(data))*64 + 64<<20}
	r, err := pdf.NewReader(src, int64(len(data)))
	if err != nil {
		switch {
		case ctx.Err() != nil:
			return nil, nil, ctx.Err()
		case errors.Is(err, pdf.ErrInvalidPassword):
			return nil, nil, ErrEncrypted
		case errors.Is(err, errReadBudget):
			return nil, nil, ErrBudget
		}
		return nil, nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	return r, src, nil
}

var errReadBudget = errors.New("PDF read budget exhausted")

// budgetReader serves the in-memory file to the object parser, checking for
// cancellation and bounding total bytes read, since the parser has no context.
type budgetReader struct {
	ctx  context.Context
	data []byte
	left int64
	err  error
}

func (b *budgetReader) ReadAt(p []byte, off int64) (int, error) {
	if err := b.ctx.Err(); err != nil {
		b.err = err
		return 0, err
	}
	if b.left -= int64(len(p)); b.left < 0 {
		b.err = errReadBudget
		return 0, errReadBudget
	}
	if off < 0 || off >= int64(len(b.data)) {
		return 0, io.EOF
	}
	n := copy(p, b.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

type leaf struct {
	page      pdf.Value
	resources pdf.Value
}

type extractor struct {
	ctx       context.Context
	limits    Limits
	src       *budgetReader
	fonts     map[fontKey]*font
	operators int
	streamed  int64
	textBytes int
	findings  []string
	seen      map[string]bool
	partial   bool
	// limitErr stops interpretation after a budget or cancellation inside an
	// operator.
	limitErr error
}

// fontKey caches a font by its object and resource name: an inline font
// dictionary shares its container's object reference with its siblings.
type fontKey struct {
	ptr  pdf.Ptr
	name string
}

func newExtractor(ctx context.Context, limits Limits, src *budgetReader) *extractor {
	return &extractor{ctx: ctx, limits: limits, src: src, fonts: map[fontKey]*font{}, seen: map[string]bool{}}
}

// finding records a document finding once; at most 20 are kept.
func (x *extractor) finding(msg string) {
	if !x.seen[msg] && len(x.findings) < 20 {
		x.seen[msg] = true
		x.findings = append(x.findings, msg)
	}
}

// pageTree walks /Kids iteratively in document order with a visited set, so a
// cycle or a shared node cannot repeat pages. /Count is not trusted.
func (x *extractor) pageTree(root pdf.Value) ([]leaf, error) {
	type item struct {
		node      pdf.Value
		resources pdf.Value
		depth     int
	}
	if root.Kind() != pdf.Dict {
		return nil, fmt.Errorf("%w: no page tree", ErrMalformed)
	}
	leaves := []leaf{}
	visited := map[pdf.Ptr]bool{}
	stack := []item{{root, pdf.Value{}, 0}}
	nodes := 0
	for len(stack) > 0 {
		if err := x.ctx.Err(); err != nil {
			return nil, err
		}
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if nodes++; nodes > 2*x.limits.Pages+64 {
			return nil, ErrBudget
		}
		resources := it.resources
		if r := it.node.Key("Resources"); r.Kind() == pdf.Dict {
			resources = r
		}
		kids := it.node.Key("Kids")
		if it.node.Key("Type").Name() == "Page" || kids.Kind() != pdf.Array {
			if len(leaves) >= x.limits.Pages {
				return nil, ErrBudget
			}
			leaves = append(leaves, leaf{it.node, resources})
			continue
		}
		if it.depth >= 64 {
			x.partial = true
			x.finding("page tree deeper than 64 levels; deeper pages were not read")
			continue
		}
		visited[it.node.GetPtr()] = true
		for i := kids.Len() - 1; i >= 0; i-- {
			kid := kids.Index(i)
			if kid.Kind() != pdf.Dict {
				x.partial = true
				x.finding("a page tree entry is not a dictionary and was skipped")
				continue
			}
			// Kids are indirect references (ISO 32000-1, 7.7.3.2), so a kid
			// with an object already seen, including its own parent, is a
			// cycle or a repeat and is not read again.
			if p := kid.GetPtr(); visited[p] {
				x.partial = true
				x.finding("a page tree node is reachable twice or refers to itself; repeated pages were not read")
				continue
			} else {
				visited[p] = true
			}
			stack = append(stack, item{kid, resources, it.depth + 1})
		}
	}
	return leaves, nil
}

// stream decodes one stream within the per-stream and total budgets.
func (x *extractor) stream(v pdf.Value) ([]byte, error) {
	if v.Kind() != pdf.Stream {
		return nil, errors.New("not a stream")
	}
	rc := v.Reader()
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, x.limits.StreamBytes+1))
	if err != nil {
		if x.ctx.Err() != nil {
			return nil, x.ctx.Err()
		}
		return nil, err
	}
	if int64(len(data)) > x.limits.StreamBytes {
		return nil, ErrBudget
	}
	if x.streamed += int64(len(data)); x.streamed > x.limits.TotalStreamBytes {
		return nil, ErrBudget
	}
	return data, nil
}

// page interprets a page's content streams into p.
func (x *extractor) page(l leaf, p *Page) error {
	contents := l.page.Key("Contents")
	var streams []pdf.Value
	switch contents.Kind() {
	case pdf.Stream:
		streams = []pdf.Value{contents}
	case pdf.Array:
		for i := 0; i < contents.Len() && i < 10000; i++ {
			streams = append(streams, contents.Index(i))
		}
	case pdf.Null:
	default:
		p.Gaps = append(p.Gaps, Gap{Kind: "unreadable_content", Detail: "page contents are not a stream"})
	}
	var all []byte
	for _, s := range streams {
		data, err := x.stream(s)
		if errors.Is(err, ErrBudget) || x.ctx.Err() != nil {
			return firstErr(x.ctx.Err(), ErrBudget)
		}
		if err != nil {
			x.gap(p, Gap{Kind: "unreadable_content", Detail: "a content stream could not be decoded"}, 0)
			continue
		}
		// Streams of one page form one content stream, split at token boundaries.
		all = append(append(all, data...), '\n')
	}
	st := &textState{page: p}
	if err := x.interpret(all, l.resources, st, 0, map[pdf.Ptr]bool{}); err != nil {
		return err
	}
	p.Text = strings.TrimRight(string(st.out), " \n")
	if len(p.Text) > x.limits.PageTextBytes {
		return ErrBudget
	}
	for i := range p.Gaps {
		p.Gaps[i].Offset = min(p.Gaps[i].Offset, len(p.Text))
	}
	if strings.TrimSpace(p.Text) == "" && p.Images > 0 {
		p.Gaps = append(p.Gaps, Gap{Kind: "no_text_layer", Detail: "the page draws images and no extractable text; it may be scanned", Offset: 0})
	}
	if x.textBytes += len(p.Text); x.textBytes > x.limits.TextBytes {
		return ErrBudget
	}
	return nil
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// gap appends a gap, merging a repeat of the same kind and detail at the same
// offset and bounding the count per page.
func (x *extractor) gap(p *Page, g Gap, offset int) {
	g.Offset = offset
	if g.Count == 0 {
		g.Count = 1
	}
	if n := len(p.Gaps); n > 0 {
		last := &p.Gaps[n-1]
		if last.Kind == g.Kind && last.Detail == g.Detail && last.Offset == g.Offset {
			last.Count += g.Count
			return
		}
	}
	if len(p.Gaps) >= x.limits.GapsPerPage {
		last := &p.Gaps[len(p.Gaps)-1]
		if last.Kind != "more_gaps" {
			p.Gaps = append(p.Gaps, Gap{Kind: "more_gaps", Detail: "further omissions on this page are counted here", Offset: offset})
			last = &p.Gaps[len(p.Gaps)-1]
		}
		last.Count += g.Count
		return
	}
	p.Gaps = append(p.Gaps, g)
}

// textState accumulates page text in content-stream order.
type textState struct {
	page    *Page
	out     []byte
	floor   int // text before this offset is fixed: a gap was recorded there
	font    *font
	lineY   float64
	haveY   bool
	inText  bool
	fontStk []*font
}

// mark fixes the text so far and returns its length as a gap offset, so a
// later trim can never move or split text a gap points into.
func (s *textState) mark() int {
	s.floor = len(s.out)
	return s.floor
}

func (s *textState) newline() {
	n := len(s.out)
	if n == 0 || s.out[n-1] == '\n' {
		return
	}
	for n > s.floor && s.out[n-1] == ' ' {
		n--
	}
	s.out = append(s.out[:n], '\n')
}

func (s *textState) space() {
	if n := len(s.out); n > 0 && s.out[n-1] != ' ' && s.out[n-1] != '\n' {
		s.out = append(s.out, ' ')
	}
}

// moveTo starts a new line when the baseline changes, and a word break when
// only the horizontal position changes.
func (s *textState) moveTo(y float64, moved bool) {
	if s.haveY && math.Abs(y-s.lineY) > 0.01 {
		s.newline()
	} else if moved {
		s.space()
	}
	s.lineY, s.haveY = y, true
}

// interpret runs one content stream. Forms recurse within the depth budget,
// and a form already being drawn is not entered again.
func (x *extractor) interpret(data []byte, resources pdf.Value, st *textState, depth int, active map[pdf.Ptr]bool) error {
	l := &lexer{data: data}
	var operands []token
	arrayDepth := 0
	var array []token
	for {
		if x.operators++; x.operators > x.limits.Operators {
			return ErrBudget
		}
		if x.operators%4096 == 0 {
			if err := x.ctx.Err(); err != nil {
				return err
			}
		}
		t, err := l.next()
		if err != nil {
			// Skip one malformed token; text around it is still read.
			operands = operands[:0]
			continue
		}
		switch t.kind {
		case tokEOF:
			return nil
		case tokArrayStart:
			if arrayDepth++; arrayDepth == 1 {
				array = array[:0]
			}
			continue
		case tokArrayEnd:
			if arrayDepth > 0 {
				arrayDepth--
			}
			if arrayDepth == 0 {
				operands = append(operands, token{kind: tokArrayStart})
			}
			continue
		case tokDictStart, tokDictEnd:
			continue
		case tokOperator:
		default:
			if arrayDepth > 0 {
				if len(array) < 65536 {
					array = append(array, t)
				}
			} else if len(operands) < 64 {
				operands = append(operands, t)
			}
			continue
		}
		if arrayDepth > 0 {
			continue // operators are not valid inside arrays
		}
		x.operator(t.text, operands, array, resources, st, depth, active, l)
		if err := x.ctx.Err(); err != nil {
			return err
		}
		if x.limitErr != nil {
			return x.limitErr
		}
		operands, array = operands[:0], array[:0]
	}
}

func num(ops []token, i int) (float64, bool) {
	if i < 0 || i >= len(ops) || ops[i].kind != tokNumber {
		return 0, false
	}
	return ops[i].num, true
}

func lastString(ops []token) (string, bool) {
	if len(ops) == 0 || ops[len(ops)-1].kind != tokString {
		return "", false
	}
	return ops[len(ops)-1].text, true
}

func lastName(ops []token) string {
	for i := len(ops) - 1; i >= 0; i-- {
		if ops[i].kind == tokName {
			return ops[i].text
		}
	}
	return ""
}

func (x *extractor) operator(op string, ops, array []token, resources pdf.Value, st *textState, depth int, active map[pdf.Ptr]bool, l *lexer) {
	p := st.page
	switch op {
	case "BT":
		st.inText, st.haveY = true, false
		st.newline()
	case "ET":
		st.inText = false
	case "q":
		if len(st.fontStk) < 64 {
			st.fontStk = append(st.fontStk, st.font)
		}
	case "Q":
		if n := len(st.fontStk); n > 0 {
			st.font, st.fontStk = st.fontStk[n-1], st.fontStk[:n-1]
		}
	case "Tf":
		st.font = x.font(resources, lastName(ops))
	case "Td", "TD":
		tx, _ := num(ops, len(ops)-2)
		ty, _ := num(ops, len(ops)-1)
		st.moveTo(st.lineY+ty, tx != 0)
	case "Tm":
		if y, ok := num(ops, len(ops)-1); ok {
			st.moveTo(y, true)
		}
	case "T*":
		st.newline()
	case "Tj":
		if s, ok := lastString(ops); ok {
			x.show(st, s)
		}
	case "'", "\"":
		st.newline()
		if s, ok := lastString(ops); ok {
			x.show(st, s)
		}
	case "TJ":
		// The array must be the operator's own operand.
		if len(ops) == 0 || ops[len(ops)-1].kind != tokArrayStart {
			return
		}
		for _, item := range array {
			switch item.kind {
			case tokString:
				x.show(st, item.text)
			case tokNumber:
				// A negative adjustment beyond 0.12 em moves right by about a
				// word space; kerning between letters stays smaller. Justified
				// TeX lines set word spaces near -190.
				if item.num < -120 {
					st.space()
				}
			}
		}
	case "Do":
		x.draw(lastName(ops), resources, st, depth, active)
	case "BI":
		// Inline image: skip its dictionary to ID, then its data to EI.
		for {
			t, err := l.next()
			if err != nil || t.kind == tokEOF {
				break
			}
			if t.kind == tokOperator && t.text == "ID" {
				l.skipInlineImage()
				break
			}
		}
		p.Images++
		x.gap(p, Gap{Kind: "image", Detail: "inline image"}, st.mark())
	}
}

// show appends a shown string through the current font.
func (x *extractor) show(st *textState, s string) {
	p := st.page
	if st.font == nil {
		x.gap(p, Gap{Kind: "unmapped_text", Detail: "no font selected", Count: max(1, len(s))}, st.mark())
		return
	}
	// Code lookups count against the operator budget, and cancellation is
	// checked while a long string decodes.
	budget := x.limits.Operators - x.operators
	text, missing, cost, ok := st.font.decode(x.ctx, s, budget)
	if x.operators += cost; !ok {
		x.limitErr = firstErr(x.ctx.Err(), ErrBudget)
		return
	}
	st.out = append(st.out, text...)
	if missing > 0 {
		reason := st.font.reason
		if reason == "" {
			reason = "codes without a Unicode mapping"
		}
		x.gap(p, Gap{Kind: "unmapped_text", Detail: st.font.name + ": " + reason, Count: missing}, st.mark())
	}
	if x.textBytes+len(st.out) > x.limits.TextBytes || len(st.out) > x.limits.PageTextBytes {
		x.limitErr = ErrBudget
	}
}

// draw handles Do: an image XObject is a gap; a form XObject is interpreted
// with its own resources, within the depth budget and without re-entry.
func (x *extractor) draw(name string, resources pdf.Value, st *textState, depth int, active map[pdf.Ptr]bool) {
	p := st.page
	obj := resources.Key("XObject").Key(name)
	switch obj.Key("Subtype").Name() {
	case "Image":
		p.Images++
		x.gap(p, Gap{Kind: "image", Detail: name}, st.mark())
	case "Form":
		ptr := obj.GetPtr()
		if depth+1 > x.limits.FormDepth || active[ptr] {
			x.gap(p, Gap{Kind: "form_not_read", Detail: name + ": nested too deeply or drawn within itself"}, st.mark())
			return
		}
		data, err := x.stream(obj)
		if errors.Is(err, ErrBudget) {
			x.limitErr = ErrBudget
			return
		}
		if err != nil {
			x.gap(p, Gap{Kind: "unreadable_content", Detail: name + ": form could not be decoded"}, st.mark())
			return
		}
		formResources := resources
		if r := obj.Key("Resources"); r.Kind() == pdf.Dict {
			formResources = r
		}
		active[ptr] = true
		saved := st.font
		if err := x.interpret(data, formResources, st, depth+1, active); err != nil {
			x.limitErr = err
		}
		st.font = saved
		delete(active, ptr)
	default:
		if name != "" {
			x.gap(p, Gap{Kind: "unreadable_content", Detail: name + ": not an image or form"}, st.mark())
		}
	}
}

// font resolves a font resource once per object.
func (x *extractor) font(resources pdf.Value, name string) *font {
	v := resources.Key("Font").Key(name)
	if v.Kind() != pdf.Dict {
		return &font{name: name, kind: "simple", codeBytes: 1, reason: "font resource is missing"}
	}
	key := fontKey{v.GetPtr(), name}
	if f, ok := x.fonts[key]; ok {
		return f
	}
	f := &font{name: name, kind: "simple", codeBytes: 1}
	if base := v.Key("BaseFont").Name(); base != "" && utf8.ValidString(base) && len(base) <= 128 {
		f.name = base
	}
	subtype := v.Key("Subtype").Name()
	if tu := v.Key("ToUnicode"); tu.Kind() == pdf.Stream {
		if data, err := x.stream(tu); err == nil && len(data) <= maxCMapBytes {
			f.cmap = parseCMap(data)
		}
	}
	switch subtype {
	case "Type3":
		if f.cmap == nil {
			f.kind, f.reason = "type3", "Type3 glyph procedures have no Unicode mapping"
		}
	case "Type0":
		f.kind, f.codeBytes = "composite", 2
		enc := v.Key("Encoding").Name()
		if f.cmap == nil && (strings.HasSuffix(enc, "-UCS2-H") || strings.HasSuffix(enc, "-UTF16-H")) {
			f.ucs2 = true
		}
		if f.cmap == nil && !f.ucs2 {
			f.reason = "composite font without a ToUnicode map"
		}
	default:
		enc := v.Key("Encoding")
		base := enc
		if enc.Kind() == pdf.Dict {
			base = enc.Key("BaseEncoding")
		}
		symbolic := v.Key("FontDescriptor").Key("Flags").Int64()&4 != 0
		var table [256]rune
		switch base.Name() {
		case "WinAnsiEncoding":
			table = winAnsi
		case "MacRomanEncoding":
			table, f.reason = macRoman, "MacRoman codes above ASCII are not mapped"
		case "StandardEncoding":
			table, f.reason = standard, "StandardEncoding codes above ASCII are not mapped"
		default:
			if symbolic {
				for i := range table {
					table[i] = unmapped
				}
				f.reason = "symbolic font with a built-in encoding"
			} else {
				table, f.reason = standard, "font built-in encoding assumed to be StandardEncoding; codes above ASCII are not mapped"
			}
		}
		if enc.Kind() == pdf.Dict {
			diffs := enc.Key("Differences")
			code := -1
			for i := 0; i < diffs.Len() && i < 4096; i++ {
				item := diffs.Index(i)
				switch item.Kind() {
				case pdf.Integer:
					code = int(item.Int64())
				case pdf.Name:
					if code >= 0 && code < 256 {
						table[code] = glyphRune(item.Name())
					}
					code++
				}
			}
		}
		f.encoding = &table
	}
	x.fonts[key] = f
	return f
}

// pageLabels expands the /PageLabels number tree (ISO 32000-1, 12.4.2) into
// one label per page. Pages before the first range have no label.
func pageLabels(tree pdf.Value, pages int) ([]string, bool, []string) {
	labels := make([]string, pages)
	if tree.Kind() != pdf.Dict {
		return labels, false, nil
	}
	type rangeStart struct {
		index int
		dict  pdf.Value
	}
	var starts []rangeStart
	var findings []string
	type node struct {
		v     pdf.Value
		depth int
	}
	stack := []node{{tree, 0}}
	visited := map[pdf.Ptr]bool{}
	for len(stack) > 0 && len(starts) <= 10000 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if p := n.v.GetPtr(); p != (pdf.Ptr{}) {
			if visited[p] && n.depth > 0 {
				continue
			}
			visited[p] = true
		}
		nums := n.v.Key("Nums")
		for i := 0; i+1 < nums.Len() && len(starts) <= 10000; i += 2 {
			k := nums.Index(i)
			if k.Kind() != pdf.Integer || k.Int64() < 0 || k.Int64() >= int64(pages) {
				continue
			}
			starts = append(starts, rangeStart{int(k.Int64()), nums.Index(i + 1)})
		}
		kids := n.v.Key("Kids")
		if n.depth < 32 {
			for i := kids.Len() - 1; i >= 0; i-- {
				stack = append(stack, node{kids.Index(i), n.depth + 1})
			}
		} else if kids.Len() > 0 {
			findings = append(findings, "page label tree deeper than 32 levels; deeper labels were not read")
		}
	}
	if len(starts) == 0 {
		return labels, false, findings
	}
	sort.SliceStable(starts, func(i, j int) bool { return starts[i].index < starts[j].index })
	for i, s := range starts {
		end := pages
		if i+1 < len(starts) {
			end = starts[i+1].index
		}
		style := s.dict.Key("S").Name()
		prefix := s.dict.Key("P").Text()
		if len(prefix) > 256 || !utf8.ValidString(prefix) {
			prefix = ""
			findings = append(findings, "a page label prefix was invalid and omitted")
		}
		first := s.dict.Key("St").Int64()
		if s.dict.Key("St").Kind() != pdf.Integer || first < 1 {
			first = 1
		}
		for page := s.index; page < end; page++ {
			labels[page] = prefix + numberLabel(style, first+int64(page-s.index))
		}
	}
	return labels, true, findings
}

// numberLabel formats n in a page label numbering style. Without a style the
// label is the prefix alone.
func numberLabel(style string, n int64) string {
	switch style {
	case "D":
		return fmt.Sprint(n)
	case "R":
		return strings.ToUpper(roman(n))
	case "r":
		return roman(n)
	case "A":
		return strings.ToUpper(letters(n))
	case "a":
		return letters(n)
	}
	return ""
}

// roman writes lowercase Roman numerals. Values above 100000 fall back to
// decimal digits rather than producing an unbounded string.
func roman(n int64) string {
	if n < 1 || n > 100000 {
		return fmt.Sprint(n)
	}
	var b strings.Builder
	for _, step := range []struct {
		v int64
		s string
	}{{1000, "m"}, {900, "cm"}, {500, "d"}, {400, "cd"}, {100, "c"}, {90, "xc"}, {50, "l"}, {40, "xl"}, {10, "x"}, {9, "ix"}, {5, "v"}, {4, "iv"}, {1, "i"}} {
		for n >= step.v {
			b.WriteString(step.s)
			n -= step.v
		}
	}
	return b.String()
}

// letters writes a, b, ..., z, aa, bb, ..., zz, aaa, ... (ISO 32000-1, Table 159).
func letters(n int64) string {
	if n < 1 || n > 26*100 {
		return fmt.Sprint(n)
	}
	return strings.Repeat(string(rune('a'+(n-1)%26)), int((n-1)/26)+1)
}

// maxObjectNumber bounds the objects one PDF may declare.
const maxObjectNumber = 1_000_000

// xrefBounded follows the cross-reference chain the way the object parser
// does, from the final startxref through /Prev and /XRefStm, and checks every
// subsection, cross-reference stream /Index range, and /Size against limit
// before the parser sizes its table from them. Tokens are read with PDF rules,
// so comments, delimiters, and name escapes cannot hide a declaration. A chain
// it cannot follow is left to the parser, which reads only what is found here.
func xrefBounded(data []byte, limit int64) bool {
	eof := bytes.LastIndex(data, []byte("%%EOF"))
	if eof < 0 {
		return true
	}
	tail := data[max(0, eof-200):eof]
	at := bytes.LastIndex(tail, []byte("startxref"))
	if at < 0 {
		return true
	}
	l := &lexer{data: tail[at+len("startxref"):]}
	t, err := l.next()
	if err != nil || t.kind != tokNumber {
		return true
	}
	queue := []float64{t.num}
	visited := map[int64]bool{}
	for sections := 0; len(queue) > 0 && sections < 256; sections++ {
		off := int64(queue[0])
		queue = queue[1:]
		if off < 0 || off >= int64(len(data)) || visited[off] {
			continue
		}
		visited[off] = true
		l := &lexer{data: data[off:]}
		first, err := l.next()
		if err != nil {
			continue
		}
		var dict map[string][]float64
		switch {
		case first.kind == tokOperator && first.text == "xref":
			for {
				start, err1 := l.next()
				if err1 != nil || start.kind != tokNumber {
					if start.kind == tokOperator && start.text == "trailer" {
						dict = readDict(l)
					}
					break
				}
				count, err2 := l.next()
				if err2 != nil || count.kind != tokNumber {
					break
				}
				if count.num < 0 || start.num < 0 || start.num+count.num > float64(limit) {
					return false
				}
				// Each entry is an offset, a generation, and n or f.
				for e := int64(0); e < int64(count.num)*3; e++ {
					if t, err := l.next(); err != nil || t.kind == tokEOF {
						break
					}
				}
			}
		case first.kind == tokNumber:
			if gen, err := l.next(); err != nil || gen.kind != tokNumber {
				continue
			}
			if obj, err := l.next(); err != nil || obj.kind != tokOperator || obj.text != "obj" {
				continue
			}
			dict = readDict(l)
			if index, ok := dict["Index"]; ok {
				for i := 0; i+1 < len(index); i += 2 {
					if index[i] < 0 || index[i+1] < 0 || index[i]+index[i+1] > float64(limit) {
						return false
					}
				}
			}
		}
		if size, ok := dict["Size"]; ok && len(size) == 1 && size[0] > float64(limit) {
			return false
		}
		for _, key := range []string{"Prev", "XRefStm"} {
			if v, ok := dict[key]; ok && len(v) == 1 {
				queue = append(queue, v[0])
			}
		}
	}
	return true
}

// readDict reads one dictionary's top-level numeric and numeric-array values.
func readDict(l *lexer) map[string][]float64 {
	out := map[string][]float64{}
	t, err := l.next()
	if err != nil || t.kind != tokDictStart {
		return out
	}
	depth, key := 1, ""
	for steps := 0; depth > 0 && steps < 1<<20; steps++ {
		t, err := l.next()
		if err != nil {
			continue
		}
		switch t.kind {
		case tokEOF:
			return out
		case tokDictStart:
			depth++
		case tokDictEnd:
			depth--
		case tokName:
			if depth == 1 && key == "" {
				key = t.text
				continue
			}
		case tokNumber:
			if depth == 1 && key != "" {
				out[key] = []float64{t.num}
			}
		case tokArrayStart:
			if depth == 1 && key != "" {
				values := []float64{}
				for n := 0; n < 1<<20; n++ {
					v, err := l.next()
					if err != nil || v.kind == tokArrayEnd || v.kind == tokEOF {
						break
					}
					if v.kind == tokNumber {
						values = append(values, v.num)
					}
				}
				out[key] = values
			}
		}
		key = ""
	}
	return out
}
