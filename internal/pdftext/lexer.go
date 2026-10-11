package pdftext

import (
	"bytes"
	"errors"
	"strconv"
)

// tokenKind classifies one content-stream token (ISO 32000-1, 7.2 and 8.1).
type tokenKind int

const (
	tokEOF tokenKind = iota
	tokNumber
	tokString
	tokName
	tokOperator
	tokArrayStart
	tokArrayEnd
	tokDictStart
	tokDictEnd
)

type token struct {
	kind tokenKind
	num  float64
	text string // string bytes, name, or operator
}

var errSyntax = errors.New("content stream syntax")

// lexer reads tokens from decoded content-stream bytes. It never allocates more
// than the input it is given, and nesting is bounded by the caller.
type lexer struct {
	data []byte
	pos  int
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f' || c == 0
}

func isDelim(c byte) bool {
	return c == '(' || c == ')' || c == '<' || c == '>' || c == '[' || c == ']' || c == '{' || c == '}' || c == '/' || c == '%'
}

func (l *lexer) skipSpace() {
	for l.pos < len(l.data) {
		c := l.data[l.pos]
		if isSpace(c) {
			l.pos++
			continue
		}
		if c == '%' {
			for l.pos < len(l.data) && l.data[l.pos] != '\n' && l.data[l.pos] != '\r' {
				l.pos++
			}
			continue
		}
		return
	}
}

func (l *lexer) next() (token, error) {
	l.skipSpace()
	if l.pos >= len(l.data) {
		return token{kind: tokEOF}, nil
	}
	c := l.data[l.pos]
	switch {
	case c == '(':
		return l.literal()
	case c == '<' && l.pos+1 < len(l.data) && l.data[l.pos+1] == '<':
		l.pos += 2
		return token{kind: tokDictStart}, nil
	case c == '>' && l.pos+1 < len(l.data) && l.data[l.pos+1] == '>':
		l.pos += 2
		return token{kind: tokDictEnd}, nil
	case c == '<':
		return l.hex()
	case c == '[':
		l.pos++
		return token{kind: tokArrayStart}, nil
	case c == ']':
		l.pos++
		return token{kind: tokArrayEnd}, nil
	case c == '/':
		return l.name()
	case c == ')' || c == '>' || c == '{' || c == '}':
		l.pos++
		return token{}, errSyntax
	}
	start := l.pos
	for l.pos < len(l.data) && !isSpace(l.data[l.pos]) && !isDelim(l.data[l.pos]) {
		l.pos++
	}
	word := string(l.data[start:l.pos])
	if f, err := strconv.ParseFloat(word, 64); err == nil && (word[0] == '-' || word[0] == '+' || word[0] == '.' || (word[0] >= '0' && word[0] <= '9')) {
		return token{kind: tokNumber, num: f}, nil
	}
	return token{kind: tokOperator, text: word}, nil
}

func (l *lexer) literal() (token, error) {
	l.pos++ // (
	var out []byte
	depth := 1
	for l.pos < len(l.data) {
		c := l.data[l.pos]
		l.pos++
		switch c {
		case '(':
			depth++
			out = append(out, c)
		case ')':
			depth--
			if depth == 0 {
				return token{kind: tokString, text: string(out)}, nil
			}
			out = append(out, c)
		case '\\':
			if l.pos >= len(l.data) {
				return token{}, errSyntax
			}
			e := l.data[l.pos]
			l.pos++
			switch e {
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			case '\r':
				if l.pos < len(l.data) && l.data[l.pos] == '\n' {
					l.pos++
				}
			case '\n':
			default:
				if e >= '0' && e <= '7' {
					v := int(e - '0')
					for k := 0; k < 2 && l.pos < len(l.data) && l.data[l.pos] >= '0' && l.data[l.pos] <= '7'; k++ {
						v = v*8 + int(l.data[l.pos]-'0')
						l.pos++
					}
					out = append(out, byte(v))
				} else {
					out = append(out, e)
				}
			}
		default:
			out = append(out, c)
		}
	}
	return token{}, errSyntax
}

func hexValue(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

func (l *lexer) hex() (token, error) {
	l.pos++ // <
	var out []byte
	var high byte
	half := false
	for l.pos < len(l.data) {
		c := l.data[l.pos]
		l.pos++
		if c == '>' {
			if half {
				out = append(out, high<<4)
			}
			return token{kind: tokString, text: string(out)}, nil
		}
		if isSpace(c) {
			continue
		}
		v, ok := hexValue(c)
		if !ok {
			return token{}, errSyntax
		}
		if half {
			out = append(out, high<<4|v)
		} else {
			high = v
		}
		half = !half
	}
	return token{}, errSyntax
}

func (l *lexer) name() (token, error) {
	l.pos++ // /
	var out []byte
	for l.pos < len(l.data) && !isSpace(l.data[l.pos]) && !isDelim(l.data[l.pos]) {
		c := l.data[l.pos]
		if c == '#' && l.pos+2 < len(l.data) {
			if h, ok := hexValue(l.data[l.pos+1]); ok {
				if lo, ok := hexValue(l.data[l.pos+2]); ok {
					out = append(out, h<<4|lo)
					l.pos += 3
					continue
				}
			}
		}
		out = append(out, c)
		l.pos++
	}
	return token{kind: tokName, text: string(out)}, nil
}

// skipInlineImage moves past inline image data after an ID operator: the
// binary data ends at whitespace, EI, and whitespace or end of stream. It
// reports false when no end is found.
func (l *lexer) skipInlineImage() bool {
	if l.pos < len(l.data) && isSpace(l.data[l.pos]) {
		l.pos++
	}
	for {
		i := bytes.Index(l.data[l.pos:], []byte("EI"))
		if i < 0 {
			l.pos = len(l.data)
			return false
		}
		at := l.pos + i
		before := at == 0 || isSpace(l.data[at-1])
		after := at+2 >= len(l.data) || isSpace(l.data[at+2]) || isDelim(l.data[at+2])
		l.pos = at + 2
		if before && after {
			return true
		}
	}
}
