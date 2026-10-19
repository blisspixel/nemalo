package content

import "strings"

// Break marks a line feed in delivered EPUB text that comes from document
// structure. Kind "block" ends a paragraph, heading, list item, or other
// block; "line" is an explicit line break (br) or a line of preformatted
// text. Every other line feed in the text is source whitespace, such as a
// hard-wrapped paragraph, and reads as a space. Offset is the UTF-8 byte of
// that line feed in the unit text. Breaks describe layout over unchanged
// text: they move no offset and are not part of a representation identity.
type Break struct {
	Offset int    `json:"offset"`
	Kind   string `json:"kind"`
}

// breakMarks holds two bits for each text byte: structural, and block. That
// bounds it to a quarter of the text it describes, however the text is marked.
type breakMarks []uint64

func (b *breakMarks) mark(offset int, block bool) {
	i := offset * 2
	for len(*b) <= i/64 {
		*b = append(*b, 0)
	}
	(*b)[i/64] |= 1 << (i % 64)
	if block {
		(*b)[i/64] |= 2 << (i % 64)
	}
}

func (b breakMarks) kind(offset int) string {
	i := offset * 2
	if i/64 >= len(b) || b[i/64]&(1<<(i%64)) == 0 {
		return ""
	}
	if b[i/64]&(2<<(i%64)) != 0 {
		return "block"
	}
	return "line"
}

// rangeBreaks lists the structural line feeds in text[start:end].
func rangeBreaks(text string, marks breakMarks, start, end int) []Break {
	out := []Break{}
	for at := start; at < end; {
		i := strings.IndexByte(text[at:end], '\n')
		if i < 0 {
			break
		}
		at += i
		if kind := marks.kind(at); kind != "" {
			out = append(out, Break{Offset: at, Kind: kind})
		}
		at++
	}
	return out
}
