// Package reader lays out delivered source text for a person: pages sized to
// a terminal and one word at a time. It is presentation over content results.
// It reads no files, stores nothing, and records no reading progress; every
// position it reports is a source position the caller may show or discard.
package reader

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/textsafe"
	"github.com/rivo/uniseg"
)

// MaxText bounds the source text one session holds: more than a very long
// novel, in at most about 40 MiB of memory. Past it the reader stops
// continuing and says so; an explicit jump starts a new session.
const MaxText = 4 << 20

const (
	// A word wider than maxWord cells (often unspaced CJK text) is shown in
	// chunks of at most chunkWidth cells.
	maxWord    = 24
	chunkWidth = 12
	maxGapText = 200
)

type Kind uint8

const (
	Word Kind = iota
	// Break is a source line break. A soft break keeps a PDF line; a block
	// break separates EPUB blocks.
	Break
	Gap
	Heading
)

// Pause is the boundary after a word.
type Pause uint8

const (
	None Pause = iota
	Clause
	Sentence
	Paragraph
)

// Position is a reading-order unit and a UTF-8 byte offset in its extracted
// text, the same coordinates as a content locator.
type Position struct{ Unit, Offset int }

func (p Position) Less(q Position) bool {
	return p.Unit < q.Unit || p.Unit == q.Unit && p.Offset < q.Offset
}

// Item is one displayable piece. Text is safe to write to a terminal: control
// and format characters from the source are shown as visible escapes.
type Item struct {
	Text  string
	Pos   Position
	Kind  Kind
	Pause Pause
	// Join continues the previous word without a space.
	Join bool
	Soft bool
}

// Doc is the contiguous text delivered in one session, in reading order.
type Doc struct {
	Items []Item
	// Next is the continuation for the text after the last delivered range;
	// empty at the end of the source or after a failed range.
	Next  string
	End   bool
	Bytes int

	pdf      bool
	units    []content.Unit
	anchored bool
	anchor   Position
	stopped  bool
	started  bool
	expect   Position
	tail     string
	tailPos  Position
	breaks   int
	gaps     []content.Gap
	unitHas  bool
	lastWord int
}

// New starts an empty session over units from a units listing of the same
// representation. PDF pages keep the extractor's line breaks; no paragraphs
// are inferred.
func New(representation string, units []content.Unit) *Doc {
	return &Doc{pdf: representation == "pdf-text/1", units: units, lastWord: -1}
}

// StartAt requires the first delivered range to begin at p, so a range
// requested for another place or session is refused.
func (d *Doc) StartAt(p Position) { d.anchored, d.anchor = true, p }

// Stop ends delivery for a new session to continue: omissions located before
// the resume position are placed, and the resume position is returned. A
// word the last range cut off is left for the new session, which starts at it.
func (d *Doc) Stop() Position {
	resume := d.expect
	if d.tail != "" {
		resume = d.tailPos
	}
	if !d.stopped {
		d.stopped = true
		d.flushGaps(resume.Unit, resume.Offset-1)
	}
	return resume
}

// Finish ends delivery because nothing more can be read here: the text the
// last range ended with and every omission it disclosed are placed, so all
// delivered text is shown even when continuation failed.
func (d *Doc) Finish() {
	if d.stopped {
		return
	}
	d.stopped = true
	if d.tail != "" {
		d.word(d.tailPos.Unit, d.tail, d.tailPos.Offset)
		d.tail = ""
	}
	d.flushGaps(d.expect.Unit, int(^uint(0)>>1))
}

// Full reports that the session holds MaxText bytes and continues no further.
func (d *Doc) Full() bool { return d.Bytes >= MaxText }

// Expect is the position the next delivered range must start at.
func (d *Doc) Expect() Position { return d.expect }

// Label names a unit for a person: a PDF physical page, with its printed
// label when that differs, or an EPUB reading-order document.
func (d *Doc) Label(unit int) string {
	total := ""
	if len(d.units) > 0 {
		total = " of " + strconv.Itoa(len(d.units))
	}
	if d.pdf {
		label := "Page " + strconv.Itoa(unit+1) + total
		if unit < len(d.units) && d.units[unit].PageLabel != "" && d.units[unit].PageLabel != strconv.Itoa(unit+1) {
			label += " (printed " + oneLine(d.units[unit].PageLabel) + ")"
		}
		return label
	}
	return "Document " + strconv.Itoa(unit+1) + total
}

// Append adds the next delivered range. A range that does not continue
// exactly where the session ends is refused, so nothing is silently skipped
// or repeated.
func (d *Doc) Append(r content.Result) error {
	if r.Locator == nil || r.Locator.OffsetUnit != "utf8_bytes" || r.Locator.End-r.Locator.Start != len(r.Text) {
		return errors.New("reader requires a text range located in UTF-8 bytes")
	}
	at := Position{r.Locator.Unit, r.Locator.Start}
	if d.stopped {
		return errors.New("this session has stopped; a new session continues the source")
	}
	if !d.started && d.anchored && at != d.anchor {
		return fmt.Errorf("range starts at unit %d byte %d, not where the session was opened (unit %d byte %d)", at.Unit, at.Offset, d.anchor.Unit, d.anchor.Offset)
	}
	if d.started && at != d.expect {
		return fmt.Errorf("range starts at unit %d byte %d, not where the session continues (unit %d byte %d)", at.Unit, at.Offset, d.expect.Unit, d.expect.Offset)
	}
	if !d.started || at.Offset == 0 {
		heading := d.Label(at.Unit)
		if at.Offset > 0 {
			heading += ", from byte " + strconv.Itoa(at.Offset)
		}
		d.add(Item{Kind: Heading, Text: heading, Pos: at})
		d.unitHas, d.breaks = false, 0
	}
	d.started = true
	d.gaps = append(d.gaps, r.Gaps...)
	text, base := r.Text, at.Offset
	if d.tail != "" {
		text, base = d.tail+r.Text, d.tailPos.Offset
	}
	d.tail = ""
	d.scan(at.Unit, text, base, r.EndOfUnit)
	d.Bytes += len(r.Text)
	d.expect = Position{at.Unit, r.Locator.End}
	if r.EndOfUnit {
		d.flushGaps(at.Unit, int(^uint(0)>>1))
		if d.lastWord >= 0 && d.Items[d.lastWord].Pos.Unit == at.Unit {
			d.Items[d.lastWord].Pause = Paragraph
		}
		d.expect = Position{at.Unit + 1, 0}
	}
	d.Next, d.End = r.Continuation, r.EndOfSource
	return nil
}

func (d *Doc) add(it Item) {
	if it.Kind == Word {
		d.lastWord = len(d.Items)
	}
	d.Items = append(d.Items, it)
}

func (d *Doc) scan(unit int, text string, base int, endOfUnit bool) {
	start := -1
	for i, r := range text {
		if unicode.IsSpace(r) {
			if start >= 0 {
				d.word(unit, text[start:i], base+start)
				start = -1
			}
			if r == '\n' {
				d.breaks++
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		if endOfUnit {
			d.word(unit, text[start:], base+start)
		} else {
			// The range may end inside a word; it is completed by the next one.
			d.tail, d.tailPos = text[start:], Position{unit, base + start}
		}
	}
}

func (d *Doc) word(unit int, raw string, offset int) {
	if d.breaks > 0 && d.unitHas {
		block := !d.pdf || d.breaks > 1
		if block && d.lastWord >= 0 && d.Items[d.lastWord].Pos.Unit == unit {
			d.Items[d.lastWord].Pause = Paragraph
		}
		at := offset
		for _, g := range d.gaps {
			at = min(at, max(g.Offset, d.Items[len(d.Items)-1].Pos.Offset))
		}
		d.add(Item{Kind: Break, Soft: !block, Pos: Position{unit, at}})
	}
	d.breaks = 0
	d.flushGaps(unit, offset)
	d.unitHas = true
	pause := classify(raw)
	display := textsafe.Visible(raw)
	if uniseg.StringWidth(display) <= maxWord {
		d.add(Item{Kind: Word, Text: display, Pos: Position{unit, offset}, Pause: pause})
		return
	}
	chunks := split(display, chunkWidth)
	for i, c := range chunks {
		it := Item{Kind: Word, Text: c, Pos: Position{unit, offset}, Join: i > 0}
		if i == len(chunks)-1 {
			it.Pause = pause
		}
		d.add(it)
	}
}

// flushGaps places every pending gap located before offset.
func (d *Doc) flushGaps(unit, offset int) {
	kept := d.gaps[:0]
	for _, g := range d.gaps {
		if g.Offset <= offset {
			d.add(Item{Kind: Gap, Text: describe(g), Pos: Position{unit, g.Offset}})
			d.unitHas = true
			continue
		}
		kept = append(kept, g)
	}
	d.gaps = kept
}

func describe(g content.Gap) string {
	var s string
	switch {
	case g.Image != nil && g.Image.Alt == nil:
		s = "[image without a text alternative]"
	case g.Image != nil && *g.Image.Alt == "":
		s = "[image marked decorative]"
	case g.Image != nil:
		alt := *g.Image.Alt
		if utf8.RuneCountInString(alt) > maxGapText {
			alt = string([]rune(alt)[:maxGapText]) + "..."
		}
		s = "[image: \"" + strings.Join(strings.Fields(alt), " ") + "\"]"
	default:
		reason := g.Reason
		if strings.Trim(reason, "abcdefghijklmnopqrstuvwxyz_") == "" {
			// A reason code reads as words; free text, such as a PDF font
			// name, is kept exactly.
			reason = strings.ReplaceAll(reason, "_", " ")
		}
		s = "[omitted " + g.Element + ": " + reason
		if g.Count > 1 {
			s += ", " + strconv.Itoa(g.Count) + " times"
		}
		s += "]"
	}
	return oneLine(s)
}

// oneLine makes source text safe for one terminal row: control and format
// characters become visible escapes, and line breaks, tabs, and runs of
// spaces become single spaces.
func oneLine(s string) string {
	return strings.Join(strings.Fields(textsafe.Visible(s)), " ")
}

// classify reads the boundary after a word from its final punctuation,
// looking past closing quotes and brackets.
func classify(raw string) Pause {
	s := strings.TrimRightFunc(raw, func(r rune) bool {
		return strings.ContainsRune("\"')]}’”»」』）", r)
	})
	r, _ := utf8.DecodeLastRuneInString(s)
	switch {
	case strings.ContainsRune(".!?…。！？", r):
		return Sentence
	case strings.ContainsRune(",;:，、；：", r):
		return Clause
	}
	return None
}

// split breaks display text into grapheme-aligned chunks of at most width cells.
func split(s string, width int) []string {
	var out []string
	var b strings.Builder
	used := 0
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		w := g.Width()
		if used+w > width && b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
			used = 0
		}
		b.WriteString(g.Str())
		used += w
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

// Width is the terminal cell width of display text.
func Width(s string) int { return uniseg.StringWidth(s) }
