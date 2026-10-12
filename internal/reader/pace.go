package reader

import (
	"sort"
	"time"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// Rates are nominal words per minute; pauses make the effective rate lower.
// Controlled studies found comprehension held near 250 to 350 words per
// minute and fell above that, so the default is 250 and higher rates are
// labeled as skimming. See decision 0025.
const (
	DefaultRate = 250
	MinRate     = 100
	MaxRate     = 1000
	RateStep    = 25
	SkimRate    = 350
)

// Dwell is how long one item stays on screen. A sentence end gets twice the
// base time, following evidence that pauses between sentences aid
// comprehension; the clause, paragraph, and long-word extensions are
// conventions of existing readers. Breaks take no time; an omission or unit
// heading stays long enough to notice.
func Dwell(it Item, rate int) time.Duration {
	base := time.Minute / time.Duration(min(max(rate, MinRate), MaxRate))
	switch it.Kind {
	case Break:
		return 0
	case Gap, Heading:
		return max(3*base, 750*time.Millisecond)
	}
	factor := 1.0
	if utf8.RuneCountInString(it.Text) > 8 {
		factor = 1.3
	}
	switch it.Pause {
	case Clause:
		factor *= 1.5
	case Sentence:
		factor *= 2
	case Paragraph:
		factor *= 2.5
	}
	return time.Duration(float64(base) * factor)
}

// Focus splits a word around the grapheme the eye is anchored to, so
// successive words keep that letter in one column. The position (about a
// third into the word) is a convention of existing speed readers; no study
// shows it beats a centered word.
func Focus(text string) (before, focus, after string) {
	g := uniseg.NewGraphemes(text)
	var parts []string
	for g.Next() {
		parts = append(parts, g.Str())
	}
	if len(parts) == 0 {
		return "", "", ""
	}
	n := len(parts)
	at := 4
	switch {
	case n == 1:
		at = 0
	case n <= 5:
		at = 1
	case n <= 9:
		at = 2
	case n <= 13:
		at = 3
	}
	for _, p := range parts[:at] {
		before += p
	}
	for _, p := range parts[at+1:] {
		after += p
	}
	return before, parts[at], after
}

// NextWord is the first item at or after i that is shown in speed mode, or
// len(Items). Breaks are skipped.
func (d *Doc) NextWord(i int) int {
	for i < len(d.Items) && d.Items[i].Kind == Break {
		i++
	}
	return i
}

// SentenceStart is the first item of the sentence holding item i: after a
// sentence end, a block break, an omission, or a heading.
func (d *Doc) SentenceStart(i int) int {
	i = min(i, len(d.Items))
	for i > 0 && !d.boundaryBefore(i) {
		i--
	}
	return d.NextWord(i)
}

func (d *Doc) boundaryBefore(i int) bool {
	prev := d.Items[i-1]
	cur := Item{}
	if i < len(d.Items) {
		cur = d.Items[i]
	}
	return prev.Kind == Gap || prev.Kind == Heading || cur.Kind == Gap || cur.Kind == Heading ||
		(prev.Kind == Break && !prev.Soft) || (prev.Kind == Word && prev.Pause >= Sentence)
}

// PreviousSentence moves back to the start of the sentence holding i, or to
// the one before when i already starts a sentence.
func (d *Doc) PreviousSentence(i int) int {
	start := d.SentenceStart(i)
	if start < i || i == 0 {
		return start
	}
	j := i - 1
	for j > 0 && d.Items[j].Kind == Break {
		j--
	}
	return d.SentenceStart(j)
}

// NextSentence moves to the start of the sentence after the one holding i,
// or len(Items) when it has not been delivered yet.
func (d *Doc) NextSentence(i int) int {
	for j := i + 1; j < len(d.Items); j++ {
		if d.boundaryBefore(j) {
			return d.NextWord(j)
		}
	}
	return len(d.Items)
}

// ItemFor is the first shown item at or after p.
func (d *Doc) ItemFor(p Position) int {
	return d.NextWord(sort.Search(len(d.Items), func(i int) bool { return !d.Items[i].Pos.Less(p) }))
}
