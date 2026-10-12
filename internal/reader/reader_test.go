package reader

import (
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/blisspixel/nemalo/internal/content"
)

// ranges delivers unit texts as content would: rune-aligned ranges of at most
// size bytes, each gap disclosed once in the range that holds its offset.
func ranges(texts []string, gaps [][]content.Gap, size int) []content.Result {
	var out []content.Result
	for u, text := range texts {
		for start := 0; ; {
			end := min(len(text), start+size)
			for end < len(text) && !utf8.RuneStart(text[end]) {
				end--
			}
			if end == start && start < len(text) {
				// Content never splits a character; a range holds at least one.
				_, n := utf8.DecodeRuneInString(text[start:])
				end = start + n
			}
			r := content.Result{Text: text[start:end], Locator: &content.Locator{Unit: u, Start: start, End: end, OffsetUnit: "utf8_bytes"}, EndOfUnit: end == len(text)}
			r.EndOfSource = r.EndOfUnit && u == len(texts)-1
			if !r.EndOfSource {
				r.Continuation = "next"
			}
			for _, g := range gaps[u] {
				if (g.Offset >= start && g.Offset < end) || (g.Offset == end && r.EndOfUnit) {
					r.Gaps = append(r.Gaps, g)
				}
			}
			out = append(out, r)
			start = end
			if r.EndOfUnit {
				break
			}
		}
	}
	return out
}

func deliver(t *testing.T, representation string, units []content.Unit, results []content.Result) *Doc {
	t.Helper()
	d := New(representation, units)
	for _, r := range results {
		if err := d.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func kinds(d *Doc) string {
	var b strings.Builder
	for _, it := range d.Items {
		switch it.Kind {
		case Word:
			if it.Join {
				b.WriteString("+")
			} else {
				b.WriteString(" ")
			}
			b.WriteString(it.Text)
		case Break:
			if it.Soft {
				b.WriteString(" /")
			} else {
				b.WriteString(" //")
			}
		case Gap:
			b.WriteString(" " + it.Text)
		case Heading:
			b.WriteString(" #" + it.Text)
		}
	}
	return strings.TrimSpace(b.String())
}

var alt = "The old sea-dog"

func illustrated() ([]string, [][]content.Gap) {
	texts := []string{"CHAPTER I\nSquire Trelawney asked me, to write it down.\nHe was a tall, strong man.\nEnd.\n", "Second \"unit\" here!"}
	gaps := [][]content.Gap{{
		{Element: "img", Reason: "image_pixels_not_text", Offset: 10, Image: &content.GapImage{Source: "i1.png", Alt: &alt}},
		{Element: "table", Reason: "table_relationships_not_extracted", Offset: 55},
		{Element: "img", Offset: 82, Image: &content.GapImage{Source: "i2.png"}},
		{Element: "img", Offset: 87, Image: &content.GapImage{Source: "i3.png", Alt: new(string)}},
	}, nil}
	return texts, gaps
}

func TestEveryRangeSizeGivesTheSameItems(t *testing.T) {
	texts, gaps := illustrated()
	texts = append(texts, "日本語の文章です。とても長いので分割されます、そうでしょう。 next")
	gaps = append(gaps, nil)
	whole := deliver(t, "epub-text/2", make([]content.Unit, 3), ranges(texts, gaps, 1<<20))
	want := `#Document 1 of 3 CHAPTER I // [image: "The old sea-dog"] Squire Trelawney asked me, to write it down. // [omitted table: table relationships not extracted] He was a tall, strong man. // [image without a text alternative] End. [image marked decorative] #Document 2 of 3 Second "unit" here! #Document 3 of 3 日本語の文章+です。とても+長いので分割+されます、そ+うでしょう。 next`
	if got := kinds(whole); got != want {
		t.Fatalf("items\n got %s\nwant %s", got, want)
	}
	for size := 1; size < 100; size++ {
		d := deliver(t, "epub-text/2", make([]content.Unit, 3), ranges(texts, gaps, size))
		if !reflect.DeepEqual(d.Items, whole.Items) || !d.End || d.Next != "" || d.Bytes != whole.Bytes {
			t.Fatalf("range size %d changed the session:\n%s", size, kinds(d))
		}
	}
	// Every position is a real word start in its unit.
	for _, it := range whole.Items {
		if it.Kind == Word && !it.Join {
			text := texts[it.Pos.Unit]
			if it.Pos.Offset >= len(text) || !strings.HasPrefix(text[it.Pos.Offset:], it.Text[:1]) {
				t.Fatalf("position %v does not start %q", it.Pos, it.Text)
			}
		}
	}
	for i := 1; i < len(whole.Items); i++ {
		if whole.Items[i].Pos.Less(whole.Items[i-1].Pos) {
			t.Fatalf("positions go backward at %d: %v then %v", i, whole.Items[i-1], whole.Items[i])
		}
	}
}

func TestPausesFollowPunctuation(t *testing.T) {
	d := deliver(t, "epub-text/2", nil, ranges([]string{"One, two; three. \"Four?\" five)\nsix"}, [][]content.Gap{nil}, 1<<20))
	var got []Pause
	for _, it := range d.Items {
		if it.Kind == Word {
			got = append(got, it.Pause)
		}
	}
	if want := []Pause{Clause, Clause, Sentence, Sentence, Paragraph, Paragraph}; !reflect.DeepEqual(got, want) {
		t.Fatal(got, want)
	}
}

func TestRefusesRangesThatSkipOrRepeat(t *testing.T) {
	results := ranges([]string{"alpha beta gamma", "delta"}, [][]content.Gap{nil, nil}, 6)
	d := New("epub-text/2", nil)
	if err := d.Append(results[0]); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []content.Result{results[0], results[2], results[3], {Text: "x"}, {Text: "x", Locator: &content.Locator{Unit: 0, Start: 6, End: 7}}, {Text: "xy", Locator: &content.Locator{Unit: 0, Start: 6, End: 7, OffsetUnit: "utf8_bytes"}}} {
		before := len(d.Items)
		if err := d.Append(bad); err == nil || len(d.Items) != before {
			t.Fatalf("accepted %+v", bad)
		}
	}
	if d.Expect() != (Position{0, 6}) {
		t.Fatal(d.Expect())
	}
}

func TestStartingMidUnitSaysSo(t *testing.T) {
	d := New("epub-text/2", make([]content.Unit, 2))
	r := content.Result{Text: "tail end", Locator: &content.Locator{Unit: 1, Start: 40, End: 48, OffsetUnit: "utf8_bytes"}, EndOfUnit: true, EndOfSource: true}
	if err := d.Append(r); err != nil {
		t.Fatal(err)
	}
	if got := kinds(d); got != "#Document 2 of 2, from byte 40 tail end" {
		t.Fatal(got)
	}
	if d.Items[1].Pos != (Position{1, 40}) {
		t.Fatal(d.Items[1].Pos)
	}
}

func TestSourceControlsAreVisibleNotSent(t *testing.T) {
	d := deliver(t, "epub-text/2", nil, ranges([]string{"red\x1b[31mtext \u202eevil bell\a"}, [][]content.Gap{nil}, 1<<20))
	for _, it := range d.Items {
		if strings.ContainsAny(it.Text, "\x1b\a\u202e") {
			t.Fatalf("control reached display text: %q", it.Text)
		}
	}
	if !strings.Contains(kinds(d), `red\u001b[31mtext`) {
		t.Fatal(kinds(d))
	}
	long := strings.Repeat("x", 500)
	g := content.Gap{Element: "img", Offset: 0, Image: &content.GapImage{Alt: &long}}
	if got := describe(g); Width(got) > maxGapText+20 || !strings.HasSuffix(got, `..."]`) {
		t.Fatal(got)
	}
	if got := describe(content.Gap{Element: "font", Reason: "no_unicode_map", Count: 3}); got != "[omitted font: no unicode map, 3 times]" {
		t.Fatal(got)
	}
}

func TestPDFKeepsLinesAndLabelsPages(t *testing.T) {
	units := []content.Unit{{Index: 0, PageLabel: "iv"}, {Index: 1, PageLabel: "2"}}
	d := deliver(t, "pdf-text/1", units, ranges([]string{"First line of\nthe page.\n\nNew block", "Two"}, [][]content.Gap{nil, nil}, 7))
	if got := kinds(d); got != "#Page 1 of 2 (printed iv) First line of / the page. // New block #Page 2 of 2 Two" {
		t.Fatal(got)
	}
	if d.Items[3].Pause != None {
		t.Fatal("a PDF line end must not pause like a paragraph", d.Items[3])
	}
	var l Layout
	l.Update(d, 80)
	var rows []string
	for _, line := range l.Lines {
		rows = append(rows, l.Text(line))
	}
	if got := strings.Join(rows, "|"); got != "Page 1 of 2 (printed iv)||First line of|the page.||New block||Page 2 of 2||Two" {
		t.Fatal(got)
	}
}

func TestLayoutFitsWidthAndExtendsIncrementally(t *testing.T) {
	texts, gaps := illustrated()
	texts[1] = strings.Repeat("Wide 漢字かな交じり文 words wrap here. ", 40) + strings.Repeat("無", 90)
	for _, width := range []int{1, 24, 31, 66} {
		whole := deliver(t, "epub-text/2", nil, ranges(texts, gaps, 1<<20))
		var fresh Layout
		fresh.Update(whole, width)
		for _, line := range fresh.Lines {
			if line.Hang {
				t.Fatal("EPUB prose must not hang", line)
			}
			if Width(fresh.Text(line)) > max(width, maxWord) {
				t.Fatalf("width %d: row of %d cells: %q", width, Width(fresh.Text(line)), fresh.Text(line))
			}
		}
		for _, size := range []int{3, 17, 64} {
			d := New("epub-text/2", nil)
			var l Layout
			for _, r := range ranges(texts, gaps, size) {
				if err := d.Append(r); err != nil {
					t.Fatal(err)
				}
				l.Update(d, width)
			}
			if !reflect.DeepEqual(l.Lines, fresh.Lines) {
				t.Fatalf("width %d size %d: incremental layout differs", width, size)
			}
		}
		// A width change lays everything out again.
		var l Layout
		l.Update(whole, 200)
		l.Update(whole, width)
		if !reflect.DeepEqual(l.Lines, fresh.Lines) {
			t.Fatal("width change kept stale rows")
		}
	}
}

func TestPDFContinuationRowsHang(t *testing.T) {
	line := "This typeset source line is longer than the measure and must wrap twice over here."
	d := deliver(t, "pdf-text/1", nil, ranges([]string{line + "\nShort line."}, [][]content.Gap{nil}, 1<<20))
	var l Layout
	l.Update(d, 30)
	var rows []string
	for _, row := range l.Lines {
		text := l.Text(row)
		if row.Hang {
			text = "  " + text
		}
		if Width(text) > 30 {
			t.Fatalf("row of %d cells: %q", Width(text), text)
		}
		rows = append(rows, text)
	}
	want := "Page 1||This typeset source line is|  longer than the measure and|  must wrap twice over here.|Short line."
	if got := strings.Join(rows, "|"); got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestLineForKeepsHeadingsWithText(t *testing.T) {
	texts, gaps := illustrated()
	d := deliver(t, "epub-text/2", nil, ranges(texts, gaps, 1<<20))
	var l Layout
	l.Update(d, 40)
	if i := l.LineFor(Position{1, 0}); l.Lines[i].Kind != Heading && !(l.Lines[i].Kind == Break && l.Lines[i+1].Kind == Heading) {
		t.Fatal("unit start does not show its heading", i, l.Lines[i])
	}
	// The omission located with the paragraph opens the row it starts.
	if i := l.LineFor(Position{0, 30}); l.Lines[i].Kind != Gap || !strings.Contains(l.Text(l.Lines[i+1]), "Trelawney") {
		t.Fatal(l.Text(l.Lines[i]))
	}
	if l.LineFor(Position{0, 0}) != 0 || l.Text(l.Lines[l.LineFor(Position{9, 0})]) != "Document 2" {
		t.Fatal("bounds")
	}
	var empty Layout
	if empty.LineFor(Position{}) != 0 {
		t.Fatal("empty layout")
	}
}

func TestDwellAndFocus(t *testing.T) {
	base := time.Minute / 250
	cases := []struct {
		it   Item
		want time.Duration
	}{
		{Item{Text: "word"}, base},
		{Item{Text: "word", Pause: Clause}, base * 3 / 2},
		{Item{Text: "word", Pause: Sentence}, 2 * base},
		{Item{Text: "word", Pause: Paragraph}, base * 5 / 2},
		{Item{Text: "remarkable"}, base * 13 / 10},
		{Item{Kind: Break}, 0},
		{Item{Kind: Gap}, 750 * time.Millisecond},
		{Item{Kind: Heading}, 750 * time.Millisecond},
	}
	for _, c := range cases {
		if got := Dwell(c.it, 250); got != c.want {
			t.Fatalf("%+v: %v, want %v", c.it, got, c.want)
		}
	}
	if Dwell(Item{Text: "a"}, 5) != time.Minute/MinRate || Dwell(Item{Text: "a"}, 1e6) != time.Minute/MaxRate {
		t.Fatal("rate not clamped")
	}
	for word, want := range map[string][3]string{"": {}, "a": {"", "a", ""}, "word": {"w", "o", "rd"}, "reading": {"re", "a", "ding"}, "comprehension": {"com", "p", "rehension"}, "incomprehensibility": {"inco", "m", "prehensibility"}, "éte": {"é", "t", "e"}} {
		b, f, a := Focus(word)
		if [3]string{b, f, a} != want {
			t.Fatalf("%q: %q %q %q", word, b, f, a)
		}
	}
}

func TestSentenceNavigation(t *testing.T) {
	texts, gaps := illustrated()
	d := deliver(t, "epub-text/2", nil, ranges(texts, gaps, 1<<20))
	text := func(i int) string {
		if i >= len(d.Items) {
			return "<end>"
		}
		return d.Items[i].Text
	}
	asked := d.ItemFor(Position{0, 33})
	if text(asked) != "me," {
		t.Fatal(text(asked))
	}
	if got := text(d.SentenceStart(asked)); got != "Squire" {
		t.Fatal(got)
	}
	start := d.SentenceStart(asked)
	if got := text(d.PreviousSentence(asked)); got != "Squire" {
		t.Fatal(got)
	}
	if got := d.Items[d.PreviousSentence(start)]; got.Kind != Gap {
		t.Fatal("previous sentence of the first after a gap is the gap", got)
	}
	if got := text(d.NextSentence(asked)); !strings.HasPrefix(got, "[omitted table") {
		t.Fatal(got)
	}
	if d.PreviousSentence(0) != 0 || d.NextSentence(len(d.Items)-1) != len(d.Items) {
		t.Fatal("ends")
	}
	if d.ItemFor(Position{5, 0}) != len(d.Items) || d.Items[d.ItemFor(Position{0, 0})].Kind != Heading {
		t.Fatal("ItemFor bounds")
	}
}

func TestFullStopsAtTheBudget(t *testing.T) {
	d := New("epub-text/2", nil)
	if d.Full() {
		t.Fatal("empty is full")
	}
	d.Bytes = MaxText
	if !d.Full() {
		t.Fatal("budget not reported")
	}
}

func TestStopAndFinishPlaceEverythingDelivered(t *testing.T) {
	g := content.Gap{Element: "img", Offset: 8, Image: &content.GapImage{Source: "a.png", Alt: &alt}}
	first := content.Result{Text: "one two\nthr", Locator: &content.Locator{Start: 0, End: 11, OffsetUnit: "utf8_bytes"}, Gaps: []content.Gap{g}, Continuation: "next"}
	// A failed continuation shows the cut word and the omission it disclosed.
	d := New("epub-text/2", nil)
	d.StartAt(Position{})
	if err := d.Append(first); err != nil {
		t.Fatal(err)
	}
	if got := kinds(d); got != "#Document 1 one two" {
		t.Fatal(got)
	}
	d.Finish()
	d.Finish()
	if got := kinds(d); got != `#Document 1 one two // [image: "The old sea-dog"] thr` {
		t.Fatal(got)
	}
	if err := d.Append(content.Result{Text: "ee", Locator: &content.Locator{Start: 11, End: 13, OffsetUnit: "utf8_bytes"}}); err == nil {
		t.Fatal("a finished session accepted more text")
	}
	// A session at its limit leaves the cut word for the next session, which
	// starts at it, and places only omissions before it.
	d = New("epub-text/2", nil)
	if err := d.Append(first); err != nil {
		t.Fatal(err)
	}
	if at := d.Stop(); at != (Position{0, 8}) || d.Stop() != at {
		t.Fatal(at)
	}
	if got := kinds(d); got != "#Document 1 one two" {
		t.Fatal("an omission at the resume position belongs to the next session", got)
	}
	g.Offset = 4
	d = New("epub-text/2", nil)
	if err := d.Append(content.Result{Text: "one two thr", Locator: &content.Locator{Start: 0, End: 11, OffsetUnit: "utf8_bytes"}, Gaps: []content.Gap{g}}); err != nil {
		t.Fatal(err)
	}
	if d.Stop() != (Position{0, 8}) || !strings.Contains(kinds(d), "[image") {
		t.Fatal(kinds(d))
	}
	// A session opened at one place refuses a range for another.
	d = New("epub-text/2", nil)
	d.StartAt(Position{2, 0})
	if err := d.Append(first); err == nil || len(d.Items) != 0 {
		t.Fatal("range for another place accepted")
	}
}

func TestSourceLabelsAndReasonsStayOnOneRow(t *testing.T) {
	d := New("pdf-text/1", []content.Unit{{PageLabel: "x\r\n\ty\x1b"}})
	if got := d.Label(0); got != `Page 1 of 1 (printed x y\u001b)` {
		t.Fatalf("%q", got)
	}
	if got := describe(content.Gap{Element: "unmapped_text", Reason: "font F_1\r\nX"}); got != "[omitted unmapped_text: font F_1 X]" {
		t.Fatalf("free text must be kept, on one row: %q", got)
	}
	if got := describe(content.Gap{Element: "img", Image: &content.GapImage{Alt: func() *string { s := "a\rb"; return &s }()}}); got != `[image: "a b"]` {
		t.Fatalf("%q", got)
	}
}

func TestHangingRowsNeverExceedTheWidth(t *testing.T) {
	long := strings.Repeat("y", 23)
	d := deliver(t, "pdf-text/1", nil, ranges([]string{"a " + long + " b " + strings.Repeat("z", 24) + " c"}, [][]content.Gap{nil}, 1<<20))
	var l Layout
	l.Update(d, 24)
	for _, row := range l.Lines {
		w := Width(l.Text(row))
		if row.Hang {
			w += Indent
		}
		if w > 24 {
			t.Fatalf("row of %d cells: %q", w, l.Text(row))
		}
	}
}
