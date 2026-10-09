package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func richFixture(t *testing.T) Request {
	t.Helper()
	req := fixture(t, []string{
		`<h1 id="title">A poem</h1><div xmlns:epub="http://www.idpf.org/2007/ops" epub:type="stanza"><p>日本語<br/>e&#x301;<br/><br/>Second stanza</p></div><p>Passage <a id="r1" xmlns:epub="http://www.idpf.org/2007/ops" epub:type="noteref" href="unit1.xhtml#n1">1</a> ends. <a href="#absent">missing</a></p><figure id="fig"><img src="image.png" alt="Source alternative"/><figcaption id="cap">Source caption</figcaption></figure><figure><img src="image.png"/></figure><img src="https://example.invalid/never-fetch.png"/><table id="tab"><tr><th id="head" colspan="2">X</th></tr><tr><td headers="head" rowspan="2">X</td><td>X</td></tr></table><math xmlns="http://www.w3.org/1998/Math/MathML" id="eq"><mi>x</mi><mo>=</mo><mn>1</mn></math><p>After all gaps.</p>`,
		`<aside xmlns:epub="http://www.idpf.org/2007/ops" epub:type="footnote" id="n1"><p>Exact note text.</p><a href="unit0.xhtml#r1">Return</a></aside><p>Text outside note.</p>`,
	}, func(files map[string]string) {
		files["image.png"] = "exact resource bytes"
		files["book.opf"] = strings.Replace(files["book.opf"], "</manifest>", `<item id="img" href="image.png" media-type="image/png"/></manifest>`, 1)
		files["unit0.xhtml"] = strings.Replace(files["unit0.xhtml"], "<html>", `<html xml:lang="ja" dir="rtl">`, 1)
	})
	req.Representation, req.MaxBytes = "epub-structure/1", 65536
	return req
}

func TestStructuredRetrievalCyclesAndTerminalGap(t *testing.T) {
	ctx := context.Background()
	works := []Request{
		fixture(t, []string{`<p id="passage">` + strings.Repeat("日本語 e&#x301; ", 80) + `</p><img id="last" src="https://example.invalid/no.png"/>`}, nil),
		fixture(t, []string{`<pre>` + strings.Repeat("Line\n\nAnother line\n", 80) + `</pre>`}, nil),
	}
	calls := 0
	for _, base := range works {
		before, err := os.ReadFile(filepath.Join(base.Root, "book.epub"))
		if err != nil {
			t.Fatal(err)
		}
		base.Representation, base.MaxBytes = "epub-structure/1", 65536
		whole, err := Get(ctx, base)
		if err != nil {
			t.Fatal(err)
		}
		q := base
		q.MaxBytes = 7
		var text strings.Builder
		gaps := map[string]bool{}
		for {
			chunk, err := Get(ctx, q)
			if err != nil {
				t.Fatal(chunk, err)
			}
			retry, err := Get(ctx, q)
			a, _ := json.Marshal(chunk)
			b, _ := json.Marshal(retry)
			if err != nil || !bytes.Equal(a, b) || len(chunk.Text) > q.MaxBytes {
				t.Fatal("non-deterministic range", err)
			}
			calls += 2
			text.WriteString(chunk.Text)
			for _, id := range chunk.Coverage.KnownGaps {
				gaps[id] = true
			}
			if chunk.Continuation == "" {
				break
			}
			q.Cursor = chunk.Continuation
		}
		if text.String() != whole.Text {
			t.Fatal("continuation changed source representation")
		}
		if strings.Contains(whole.Text, "日本語") && !gaps["id:last"] {
			t.Fatal("terminal image gap disappeared")
		}
		after, err := os.ReadFile(filepath.Join(base.Root, "book.epub"))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("retrieval changed source", err)
		}
	}
	if calls <= 300 {
		t.Fatal("insufficient actual retrieval cycles", calls)
	}
	t.Logf("%d structured retrieval/retry calls", calls)
}

func TestStructuredSourceCoverageAndResourceBoundaries(t *testing.T) {
	q := fixture(t, []string{`<p id="p">Start <a href="#p">self</a> and more text.</p><img id="tail" src="../escape.png"/><span hidden="">not delivered</span>`}, nil)
	q.Representation, q.MaxBytes = "epub-structure/1", 12
	r, err := Get(context.Background(), q)
	if err != nil || len(r.Coverage.ReferencedOutsideRange) == 0 {
		t.Fatal("partially delivered reference target claimed in range", r, err)
	}
	q.MaxBytes = 65536
	r, err = Get(context.Background(), q)
	if err != nil || findPart(t, r.Parts, "id:tail").Image.Status != "invalid_reference" || strings.Contains(r.Text, "not delivered") {
		t.Fatal(r, err)
	}
	q.Representation = "epub-source/1"
	r, err = Get(context.Background(), q)
	if err != nil || len(r.Coverage.KnownGaps) != 0 || !strings.Contains(r.Text, `hidden=""`) {
		t.Fatal("original markup incorrectly reported omitted", r, err)
	}
	if referenceFits(strings.Repeat("\x01", 253), "", "") || referenceFits("part:1", strings.Repeat("x", 1800), strings.Repeat("0", 64)) {
		t.Fatal("oversized encoded reference accepted")
	}
	if _, _, status := localReference("a/b.xhtml", "%2e%2e/%2e%2e/outside"); status != "invalid_reference" {
		t.Fatal(status)
	}
	if _, _, status := localReference("a/b.xhtml", "image.png?query=1"); status != "invalid_reference" {
		t.Fatal(status)
	}
}

func TestStructuredMissingOversizedAndStaleResources(t *testing.T) {
	q := fixture(t, []string{`<img id="big" src="big.png"/><img id="missing" src="missing.png"/><p>Still available.</p>`}, func(files map[string]string) {
		files["big.png"] = strings.Repeat("x", (8<<20)+1)
		files["book.opf"] = strings.Replace(files["book.opf"], "</manifest>", `<item id="big" href="big.png" media-type="image/png"/></manifest>`, 1)
	})
	q.Representation, q.MaxBytes = "epub-structure/1", 65536
	r, err := Get(context.Background(), q)
	if err != nil {
		t.Fatal(r, err)
	}
	for id, want := range map[string]string{"id:big": "resource_budget_exceeded", "id:missing": "missing_member"} {
		image := findPart(t, r.Parts, id).Image
		if image == nil || image.Status != want || image.Reference != "" {
			t.Fatal(image)
		}
	}
	c := cursor{SchemaVersion: 1, AssetID: q.AssetID, Extractor: structuredExtractor, RepresentationID: "sha256:" + strings.Repeat("0", 64), Representation: q.Representation}
	q.Cursor = encodeCursor(c)
	r, err = Get(context.Background(), q)
	if err == nil || r.Status != "stale_reference" || r.Text != "" {
		t.Fatal(r, err)
	}
	q.Cursor = ""
	data, err := os.ReadFile(filepath.Join(q.Root, "book.epub"))
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 1
	if err = os.WriteFile(filepath.Join(q.Root, "book.epub"), data, 0600); err != nil {
		t.Fatal(err)
	}
	r, err = Get(context.Background(), q)
	if err == nil || r.Status != "changed_source" || r.Locator != nil {
		t.Fatal(r, err)
	}
}

func TestStructuredWholeDocumentAndNestedAnchorCycles(t *testing.T) {
	q := fixture(t, []string{`<p><a href="unit1.xhtml">next</a></p>`, `<aside id="note"><a id="back" href="unit0.xhtml">back</a></aside>`}, nil)
	q.Representation, q.MaxBytes = "epub-structure/1", 65536
	for unit := range 2 {
		q.Unit = unit
		r, err := Get(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, p := range r.Parts {
			if p.Link != nil {
				found = true
				if !p.Link.Cyclic {
					t.Fatal("document cycle undisclosed", p)
				}
			}
		}
		if !found {
			t.Fatal("link missing")
		}
	}
}

func TestStructuredSelectedEmptyPartAndNamespacedAttributes(t *testing.T) {
	q := fixture(t, []string{`<img id="one" src="one.png"/><img id="two" src="two.png"/><p xmlns:other="https://example.invalid/attributes" other:id="wrong" other:lang="de">Source</p>`}, func(files map[string]string) {
		files["unit0.xhtml"] = strings.Replace(files["unit0.xhtml"], "<html>", `<html xml:lang="ja" lang="fr">`, 1)
	})
	q.Representation, q.MaxBytes = "epub-structure/1", 65536
	r, err := Get(context.Background(), q)
	if err != nil || r.Units[0].Language != "ja" {
		t.Fatal(r, err)
	}
	for _, p := range r.Parts {
		if p.ID == "id:wrong" || p.Language != "ja" {
			t.Fatal("foreign attribute interpreted as XHTML", p)
		}
	}
	q.Part = "id:one"
	r, err = Get(context.Background(), q)
	if err != nil || len(r.Parts) != 1 || r.Parts[0].ID != "id:one" || len(r.Coverage.KnownGaps) != 1 || r.Continuation != "" {
		t.Fatal("neighboring zero-width parts delivered", r, err)
	}
}

func TestStructuredAccessRestrictionsAndPartBudget(t *testing.T) {
	for _, mutate := range []func(map[string]string){
		func(files map[string]string) { files["META-INF/encryption.xml"] = "<encryption/>" },
		func(files map[string]string) {
			files["book.opf"] = strings.Replace(files["book.opf"], `media-type="application/xhtml+xml"`, `media-type="application/xhtml+xml" properties="scripted"`, 1)
		},
		func(files map[string]string) {
			files["book.opf"] = strings.Replace(files["book.opf"], "</spine>", `<itemref idref="u0"/></spine>`, 1)
		},
	} {
		q := fixture(t, []string{"<p>Source</p>"}, mutate)
		q.Representation = "epub-structure/1"
		r, err := Get(context.Background(), q)
		if err == nil || r.Status != "unsupported_extraction" || r.Text != "" || r.Continuation != "" {
			t.Fatal(r, err)
		}
	}
	q := fixture(t, []string{strings.Repeat("<p>x</p>", 1025)}, nil)
	q.Representation = "epub-structure/1"
	r, err := Get(context.Background(), q)
	if err == nil || r.Status != "budget_exceeded" || r.Text != "" || r.Continuation != "" {
		t.Fatal(r, err)
	}
}

func FuzzStructuredExtraction(f *testing.F) {
	for _, seed := range []string{`<html><body><p id="p">日本語</p></body></html>`, `<body><table><tr><td>Cell</td></tr></table><img src="../no"/></body>`, `<body><math xmlns="http://www.w3.org/1998/Math/MathML"><mi>x</mi></math></body>`, `<body>broken`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 65536 {
			t.Skip()
		}
		d, err := extractDocument(context.Background(), []byte(input), 4096)
		if err != nil {
			return
		}
		if len(d.text) > 4096 || !utf8.ValidString(d.text) {
			t.Fatal("invalid successful text")
		}
		for _, p := range d.parts {
			if p.Start < 0 || p.End < p.Start || p.End > len(d.text) || p.SourceStart < 0 || p.SourceEnd < p.SourceStart || p.SourceEnd > len(input) {
				t.Fatal("invalid part range", p)
			}
		}
		again, err := extractDocument(context.Background(), []byte(input), 4096)
		a, _ := json.Marshal(d.parts)
		b, _ := json.Marshal(again.parts)
		if err != nil || d.text != again.text || !bytes.Equal(a, b) {
			t.Fatal("non-deterministic structure")
		}
	})
}

func findPart(t *testing.T, parts []Part, id string) Part {
	t.Helper()
	for _, p := range parts {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("missing source part %s", id)
	return Part{}
}

func TestStructuredSourcePartsNotesFiguresAndGaps(t *testing.T) {
	ctx := context.Background()
	req := richFixture(t)
	r, err := Get(ctx, req)
	if err != nil {
		t.Fatal(r, err)
	}
	if r.Units[0].Language != "ja" || r.Units[0].Direction != "rtl" || !strings.Contains(r.Text, "日本語\ne\u0301\n\nSecond stanza") || strings.Contains(r.Text, "Exact note") || strings.Contains(r.Text, "x=1") {
		t.Fatal("source structure flattened or omitted data masqueraded as text", r)
	}
	if r.Coverage == nil || r.Coverage.Text != "known_extraction_gaps" || len(r.Coverage.KnownGaps) < 4 || len(r.Coverage.ReferencedOutsideRange) == 0 {
		t.Fatal(r)
	}
	noteRef := findPart(t, r.Parts, "id:r1")
	if noteRef.Link == nil || noteRef.Link.Status != "resolved" || !noteRef.Link.Cyclic || noteRef.Link.TargetUnit != 1 || noteRef.Link.TargetPart != "id:n1" {
		t.Fatal(noteRef)
	}
	original, _ := json.Marshal(r)
	noteReq := req
	noteReq.Cursor = noteRef.Link.Reference
	note, err := Get(ctx, noteReq)
	if err != nil || !strings.Contains(note.Text, "Exact note text.") || strings.Contains(note.Text, "Text outside note") || note.Continuation != "" {
		t.Fatal(note, err)
	}
	returnLink := Part{}
	for _, p := range note.Parts {
		if p.Link != nil {
			returnLink = p
		}
	}
	if returnLink.Link == nil || returnLink.Link.Reference == "" || !returnLink.Link.Cyclic {
		t.Fatal(returnLink)
	}
	backReq := req
	backReq.Cursor = returnLink.Link.Reference
	back, err := Get(ctx, backReq)
	if err != nil || back.Text != "1" {
		t.Fatal("return lost exact source", back, err)
	}
	again, err := Get(ctx, req)
	encoded, _ := json.Marshal(again)
	if err != nil || !bytes.Equal(original, encoded) {
		t.Fatal("following notes changed original retrieval", err)
	}
	table := findPart(t, r.Parts, "id:tab")
	math := findPart(t, r.Parts, "id:eq")
	if table.Gap != "table_relationships_not_extracted" || math.Gap != "mathematical_layout_not_extracted" || table.SourceReference == "" || math.SourceReference == "" {
		t.Fatal(table, math)
	}
	for _, p := range []Part{table, math} {
		sourceReq := req
		sourceReq.Representation = "epub-source/1"
		sourceReq.Cursor = p.SourceReference
		source, err := Get(ctx, sourceReq)
		if err != nil || source.Locator.OffsetUnit != "member_utf8_bytes" || source.Locator.Start != p.SourceStart || source.Locator.End != p.SourceEnd || !strings.HasPrefix(source.Text, "<"+p.Kind) {
			t.Fatal(source, err)
		}
		if p.Kind == "table" && !strings.Contains(source.Text, `colspan="2"`) {
			t.Fatal("source spans lost", source.Text)
		}
		if p.Kind == "math" && !strings.Contains(source.Text, "<mi>x</mi>") {
			t.Fatal("source mathematics lost", source.Text)
		}
	}
	images := []Image{}
	for _, p := range r.Parts {
		if p.Image != nil {
			images = append(images, *p.Image)
		}
	}
	if len(images) != 3 || images[0].Alt == nil || *images[0].Alt != "Source alternative" || images[1].Alt != nil || images[2].Status != "external_not_fetched" || images[2].Reference != "" {
		t.Fatal(images)
	}
	caption := findPart(t, r.Parts, "id:cap")
	if caption.Kind != "figcaption" || caption.Parent != "id:fig" || r.Text[caption.Start:caption.End] != "Source caption" {
		t.Fatal(caption)
	}
	resourceReq := req
	resourceReq.Resource = true
	resourceReq.Cursor = images[0].Reference
	resourceReq.MaxBytes = 4
	var received []byte
	for {
		chunk, err := Get(ctx, resourceReq)
		if err != nil || chunk.Resource == nil || len(chunk.Resource.Data) > 4 || chunk.Text != "" || chunk.EndOfSource {
			t.Fatal(chunk, err)
		}
		received = append(received, chunk.Resource.Data...)
		if chunk.Continuation == "" {
			break
		}
		resourceReq.Cursor = chunk.Continuation
	}
	hash := sha256.Sum256(received)
	if string(received) != "exact resource bytes" || hex.EncodeToString(hash[:]) != images[0].SHA256 {
		t.Fatal("member byte identity lost")
	}
}

func TestStructuredReferenceFailuresAndBounds(t *testing.T) {
	req := richFixture(t)
	r, err := Get(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	image := Image{}
	for _, p := range r.Parts {
		if p.Image != nil && p.Image.Reference != "" {
			image = *p.Image
			break
		}
	}
	for _, mutate := range []func(*Request){
		func(q *Request) { q.Representation = "future/99" },
		func(q *Request) { q.Representation = "epub-text/1"; q.Cursor = r.Continuation },
		func(q *Request) { q.Part = "missing-part" },
		func(q *Request) { q.Cursor = image.Reference },
		func(q *Request) { q.Resource = true },
	} {
		q := req
		mutate(&q)
		failed, err := Get(context.Background(), q)
		if err == nil || failed.Text != "" || failed.Locator != nil || failed.Continuation != "" || failed.Resource != nil || len(failed.Parts) != 0 {
			t.Fatal(failed, err)
		}
	}
	c, err := parseCursor(image.Reference)
	if err != nil {
		t.Fatal(err)
	}
	c.MemberSHA256 = strings.Repeat("0", 64)
	req.Resource, req.Cursor = true, encodeCursor(c)
	failed, err := Get(context.Background(), req)
	if err == nil || failed.Status != "stale_reference" {
		t.Fatal(failed, err)
	}
	c.MemberSHA256 = image.SHA256
	for _, member := range []string{"../outside", "missing-member.png", "book.opf"} {
		c.Member = member
		req.Cursor = encodeCursor(c)
		failed, err = Get(context.Background(), req)
		if err == nil || failed.Status != "stale_reference" || failed.Resource != nil {
			t.Fatal("invalid reference mislabeled as malformed source", failed, err)
		}
	}
	for _, body := range []string{`<p id="x">One</p><p id="x">Two</p>`, `<p onclick="doThings()">Bad</p>`, `<script>Bad</script>`, `<body>nested</body>`, `<p>broken`} {
		q := fixture(t, []string{body}, nil)
		q.Representation = "epub-structure/1"
		if result, err := Get(context.Background(), q); err == nil || result.Text != "" {
			t.Fatal(body, result, err)
		}
	}
}

func TestStructuredRangeCoverageAndPartContinuation(t *testing.T) {
	req := richFixture(t)
	req.MaxBytes = 4
	r, err := Get(context.Background(), req)
	if err != nil || len(r.Text) > 4 || r.Coverage == nil || r.Coverage.Text != "bounded_selected_range" {
		t.Fatal(r, err)
	}
	req.Unit, req.Part = 1, "id:n1"
	var text strings.Builder
	for {
		r, err := Get(context.Background(), req)
		if err != nil {
			t.Fatal(r, err)
		}
		text.WriteString(r.Text)
		if r.Continuation == "" {
			break
		}
		req.Unit, req.Part, req.Cursor = 0, "", r.Continuation
	}
	if strings.Contains(text.String(), "outside note") || !strings.Contains(text.String(), "Exact note text.") {
		t.Fatal(text.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Get(ctx, req); err == nil {
		t.Fatal("cancellation ignored")
	}
}
