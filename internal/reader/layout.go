package reader

import (
	"sort"
	"strings"
)

// Line is one laid-out row. A Word row shows the Doc's items From to To;
// Break is a blank row between blocks; a Gap or Heading row shows Row, one
// wrapped row of that item's text. Hang marks a row that continues a wrapped
// PDF source line; it is shown indented by Indent cells, so the source's own
// line breaks stay visible.
type Line struct {
	Kind     Kind
	From, To int
	Row      string
	Pos      Position
	Hang     bool
}

// Indent is the hanging indent of a continued PDF source line.
const Indent = 2

// Layout is a Doc wrapped to a width. It is extended as the Doc grows; only
// the open block after the last hard boundary is laid out again. Rows refer to
// the Doc's items rather than copying them.
type Layout struct {
	Width     int
	Lines     []Line
	doc       *Doc
	doneItems int
	doneLines int
}

// Words are the items a Word row shows.
func (l *Layout) Words(line Line) []Item {
	if line.Kind != Word {
		return nil
	}
	return l.doc.Items[line.From:line.To]
}

// Text is a row's display text: words joined by single spaces, or none
// within a chunked word, without the hanging indent.
func (l *Layout) Text(line Line) string {
	if line.Kind != Word {
		return line.Row
	}
	var b strings.Builder
	for i, it := range l.Words(line) {
		if i > 0 && !it.Join {
			b.WriteByte(' ')
		}
		b.WriteString(it.Text)
	}
	return b.String()
}

// Update lays out items added since the last call, or everything again when
// the width changed.
func (l *Layout) Update(d *Doc, width int) {
	width = max(width, maxWord)
	if width != l.Width || d != l.doc {
		*l = Layout{Width: width, doc: d}
	}
	l.Lines = l.Lines[:l.doneLines]
	from, to := -1, -1
	used, hang := 0, false
	push := func() {
		if from >= 0 {
			l.Lines = append(l.Lines, Line{Kind: Word, From: from, To: to, Pos: d.Items[from].Pos, Hang: hang})
		}
		from, used, hang = -1, 0, false
	}
	blank := func(at Position) {
		if n := len(l.Lines); n > 0 && l.Lines[n-1].Kind != Break {
			l.Lines = append(l.Lines, Line{Kind: Break, Pos: at})
		}
	}
	for i := l.doneItems; i < len(d.Items); i++ {
		it := d.Items[i]
		switch it.Kind {
		case Word:
			w, sep := Width(it.Text), 1
			if it.Join || from < 0 {
				sep = 0
			}
			if from >= 0 && used+sep+w > width {
				push()
				sep = 0
				// A continued PDF line hangs when the word still fits.
				if d.pdf && Indent+w <= width {
					used, hang = Indent, true
				}
			}
			if from < 0 {
				from = i
			}
			to = i + 1
			used += sep + w
			continue
		case Break:
			push()
			if !it.Soft {
				blank(it.Pos)
			}
		case Gap:
			push()
			for _, row := range wrap(it.Text, width) {
				l.Lines = append(l.Lines, Line{Kind: Gap, Row: row, Pos: it.Pos})
			}
		case Heading:
			push()
			blank(it.Pos)
			for _, row := range wrap(it.Text, width) {
				l.Lines = append(l.Lines, Line{Kind: Heading, Row: row, Pos: it.Pos})
			}
			l.Lines = append(l.Lines, Line{Kind: Break, Pos: it.Pos})
		}
		l.doneItems, l.doneLines = i+1, len(l.Lines)
	}
	push()
}

// LineFor is the row showing p: the last row starting at or before it, moved
// up over rows that start at the same place, so a heading or omission stays
// with its text.
func (l *Layout) LineFor(p Position) int {
	i := l.RowOf(p)
	for i > 0 && l.Lines[i-1].Pos == l.Lines[i].Pos {
		i--
	}
	// A page does not open on a blank row.
	for i+1 < len(l.Lines) && l.Lines[i].Kind == Break && l.Lines[i+1].Pos == l.Lines[i].Pos {
		i++
	}
	return i
}

// RowOf is the last row starting at or before p, or 0.
func (l *Layout) RowOf(p Position) int {
	return max(0, sort.Search(len(l.Lines), func(i int) bool { return p.Less(l.Lines[i].Pos) })-1)
}

// wrap breaks display text at spaces into rows of at most width cells,
// chunking any longer word.
func wrap(s string, width int) []string {
	var rows []string
	row := ""
	for _, w := range strings.Fields(s) {
		for _, piece := range split(w, width) {
			switch {
			case row == "":
				row = piece
			case Width(row)+1+Width(piece) <= width:
				row += " " + piece
			default:
				rows = append(rows, row)
				row = piece
			}
		}
	}
	if row != "" || len(rows) == 0 {
		rows = append(rows, row)
	}
	return rows
}
