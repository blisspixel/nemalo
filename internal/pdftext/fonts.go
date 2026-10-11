package pdftext

import (
	"context"
	"strconv"
	"strings"
	"unicode/utf16"
)

// A code that a font cannot map to Unicode is reported, never guessed.
const unmapped = rune(-1)

// winAnsi is WinAnsiEncoding (ISO 32000-1, Annex D): Windows code page 1252.
// Codes 0x81, 0x8D, 0x8F, 0x90, and 0x9D are undefined.
var winAnsi = func() [256]rune {
	var t [256]rune
	for i := range t {
		t[i] = rune(i)
	}
	for i := 0; i < 0x20; i++ {
		t[i] = unmapped
	}
	t[0x7F] = unmapped
	high := []rune{0x20AC, unmapped, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021, 0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, unmapped, 0x017D, unmapped,
		unmapped, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014, 0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, unmapped, 0x017E, 0x0178}
	copy(t[0x80:0xA0], high)
	return t
}()

// asciiBase maps the printable ASCII range, which StandardEncoding and
// MacRomanEncoding share except where noted. Their high ranges are not mapped
// here and are reported as unmapped codes.
var asciiBase = func() [256]rune {
	var t [256]rune
	for i := range t {
		t[i] = unmapped
	}
	for i := 0x20; i < 0x7F; i++ {
		t[i] = rune(i)
	}
	return t
}()

var standard = func() [256]rune {
	t := asciiBase
	t[0x27], t[0x60] = 0x2019, 0x2018 // quoteright, quoteleft
	return t
}()

var macRoman = asciiBase

// latin1Names are the glyph names of U+00A0..U+00FF in order.
var latin1Names = strings.Fields(`space exclamdown cent sterling currency yen brokenbar section dieresis copyright ordfeminine guillemotleft logicalnot hyphen registered macron
degree plusminus twosuperior threesuperior acute mu paragraph periodcentered cedilla onesuperior ordmasculine guillemotright onequarter onehalf threequarters questiondown
Agrave Aacute Acircumflex Atilde Adieresis Aring AE Ccedilla Egrave Eacute Ecircumflex Edieresis Igrave Iacute Icircumflex Idieresis
Eth Ntilde Ograve Oacute Ocircumflex Otilde Odieresis multiply Oslash Ugrave Uacute Ucircumflex Udieresis Yacute Thorn germandbls
agrave aacute acircumflex atilde adieresis aring ae ccedilla egrave eacute ecircumflex edieresis igrave iacute icircumflex idieresis
eth ntilde ograve oacute ocircumflex otilde odieresis divide oslash ugrave uacute ucircumflex udieresis yacute thorn ydieresis`)

// asciiNames are the glyph names of U+0020..U+007E in order, as in WinAnsi.
var asciiNames = strings.Fields(`space exclam quotedbl numbersign dollar percent ampersand quotesingle parenleft parenright asterisk plus comma hyphen period slash
zero one two three four five six seven eight nine colon semicolon less equal greater question
at A B C D E F G H I J K L M N O P Q R S T U V W X Y Z bracketleft backslash bracketright asciicircum underscore
grave a b c d e f g h i j k l m n o p q r s t u v w x y z braceleft bar braceright asciitilde`)

// glyphNames maps the glyph names this extractor recognizes in an encoding's
// Differences array. Any other name is an unmapped code. uniXXXX and uXXXX
// forms are decoded separately.
var glyphNames = func() map[string]rune {
	m := map[string]rune{}
	for i, n := range asciiNames {
		m[n] = rune(0x20 + i)
	}
	for i, n := range latin1Names {
		if _, ok := m[n]; !ok {
			m[n] = rune(0xA0 + i)
		}
	}
	for name, r := range map[string]rune{
		"nbspace": 0xA0, "sfthyphen": 0xAD, "middot": 0xB7,
		"Euro": 0x20AC, "quotesinglbase": 0x201A, "florin": 0x0192, "quotedblbase": 0x201E, "ellipsis": 0x2026,
		"dagger": 0x2020, "daggerdbl": 0x2021, "circumflex": 0x02C6, "perthousand": 0x2030, "Scaron": 0x0160,
		"guilsinglleft": 0x2039, "OE": 0x0152, "Zcaron": 0x017D, "quoteleft": 0x2018, "quoteright": 0x2019,
		"quotedblleft": 0x201C, "quotedblright": 0x201D, "bullet": 0x2022, "endash": 0x2013, "emdash": 0x2014,
		"tilde": 0x02DC, "trademark": 0x2122, "scaron": 0x0161, "guilsinglright": 0x203A, "oe": 0x0153,
		"zcaron": 0x017E, "Ydieresis": 0x0178, "fi": 0xFB01, "fl": 0xFB02, "ff": 0xFB00, "ffi": 0xFB03,
		"ffl": 0xFB04, "dotlessi": 0x0131, "Lslash": 0x0141, "lslash": 0x0142, "fraction": 0x2044,
		"minus": 0x2212, "grave": 0x60, "ring": 0x02DA, "caron": 0x02C7, "breve": 0x02D8, "dotaccent": 0x02D9,
		"ogonek": 0x02DB, "hungarumlaut": 0x02DD, "periodcentered": 0xB7, "quotesingle": 0x27,
	} {
		m[name] = r
	}
	return m
}()

// glyphRune resolves a glyph name, including uniXXXX and uXXXX[XX] forms and a
// .suffix variant of a known name.
func glyphRune(name string) rune {
	if r, ok := glyphNames[name]; ok {
		return r
	}
	if base, _, found := strings.Cut(name, "."); found && base != "" {
		return glyphRune(base)
	}
	if strings.HasPrefix(name, "uni") && len(name) == 7 {
		if v, err := strconv.ParseUint(name[3:], 16, 32); err == nil && validScalar(rune(v)) {
			return rune(v)
		}
	}
	if strings.HasPrefix(name, "u") && len(name) >= 5 && len(name) <= 7 {
		if v, err := strconv.ParseUint(name[1:], 16, 32); err == nil && validScalar(rune(v)) {
			return rune(v)
		}
	}
	return unmapped
}

func validScalar(r rune) bool {
	return r >= 0 && r <= 0x10FFFF && (r < 0xD800 || r > 0xDFFF)
}

// codespace is one CMap codespace range: codes of len bytes between lo and hi.
type codespace struct {
	len    int
	lo, hi uint32
}

// mapRange maps codes lo..hi of one length. dst is the UTF-16BE value for lo,
// whose last byte increases with the code; list gives each code its own value.
// seq orders all mappings, so a later one takes precedence.
type mapRange struct {
	len    int
	lo, hi uint32
	dst    []uint16
	list   [][]uint16
	seq    int
}

type single struct {
	dst []uint16
	seq int
}

// toUnicode is a parsed ToUnicode CMap (ISO 32000-1, 9.10.3). Single codes are
// in a map; ranges stay ranges, so a hostile range cannot expand into a huge
// table, and their number is bounded so a lookup stays cheap.
type toUnicode struct {
	spaces  []codespace
	singles map[uint64]single
	ranges  []mapRange
}

const (
	maxCMapBytes   = 1 << 20
	maxCMapEntries = 65536
	maxCMapRanges  = 4096
	maxCodespaces  = 256
)

// parseCMap reads codespacerange, bfchar, and bfrange sections. Anything it
// does not understand is skipped; a code it cannot map stays unmapped.
func parseCMap(data []byte) *toUnicode {
	m := &toUnicode{singles: map[uint64]single{}}
	l := &lexer{data: data}
	entries, seq := 0, 0
	for entries < maxCMapEntries {
		t, err := l.next()
		if err != nil {
			continue
		}
		if t.kind == tokEOF {
			break
		}
		if t.kind != tokOperator {
			continue
		}
		switch t.text {
		case "begincodespacerange":
			for entries < maxCMapEntries && len(m.spaces) < maxCodespaces {
				lo, ok1 := nextString(l)
				hi, ok2 := nextString(l)
				if !ok1 || !ok2 || len(lo) != len(hi) || len(lo) == 0 || len(lo) > 4 {
					break
				}
				m.spaces = append(m.spaces, codespace{len(lo), be(lo), be(hi)})
				entries++
			}
		case "beginbfchar":
			for entries < maxCMapEntries {
				src, ok1 := nextString(l)
				dst, ok2 := nextString(l)
				if !ok1 || !ok2 || len(src) == 0 || len(src) > 4 {
					break
				}
				seq++
				m.singles[uint64(len(src))<<32|uint64(be(src))] = single{units(dst), seq}
				entries++
			}
		case "beginbfrange":
			for entries < maxCMapEntries && len(m.ranges) < maxCMapRanges {
				lo, ok1 := nextString(l)
				hi, ok2 := nextString(l)
				if !ok1 || !ok2 || len(lo) != len(hi) || len(lo) == 0 || len(lo) > 4 || be(hi) < be(lo) {
					break
				}
				seq++
				r := mapRange{len: len(lo), lo: be(lo), hi: be(hi), seq: seq}
				t, err := l.next()
				if err != nil {
					break
				}
				switch t.kind {
				case tokString:
					r.dst = units(t.text)
				case tokArrayStart:
					for len(r.list) <= int(r.hi-r.lo) && entries < maxCMapEntries {
						item, err := l.next()
						if err != nil || item.kind != tokString {
							break
						}
						r.list = append(r.list, units(item.text))
						entries++
					}
				}
				m.ranges = append(m.ranges, r)
				entries++
			}
		}
	}
	if len(m.ranges) == 0 && len(m.singles) == 0 {
		return nil
	}
	return m
}

func nextString(l *lexer) (string, bool) {
	t, err := l.next()
	return t.text, err == nil && t.kind == tokString
}

func be(s string) uint32 {
	var v uint32
	for i := 0; i < len(s); i++ {
		v = v<<8 | uint32(s[i])
	}
	return v
}

func units(s string) []uint16 {
	out := make([]uint16, 0, len(s)/2)
	for i := 0; i+1 < len(s); i += 2 {
		out = append(out, uint16(s[i])<<8|uint16(s[i+1]))
	}
	return out
}

// codeLength returns how many bytes the next code takes, using the codespace
// ranges, or fallback when none match. It reports its cost.
func (m *toUnicode) codeLength(b string, fallback int) (int, int) {
	cost := 0
	for n := 1; n <= 4 && n <= len(b); n++ {
		c := be(b[:n])
		for _, s := range m.spaces {
			cost++
			if s.len == n && c >= s.lo && c <= s.hi {
				return n, cost
			}
		}
	}
	return min(fallback, len(b)), cost
}

// lookup returns the text for code and the work it took, honoring the order
// of mappings: a later bfchar or bfrange overrides an earlier one.
func (m *toUnicode) lookup(code uint32, n int) (string, bool, int) {
	best := -1
	var u []uint16
	if s, ok := m.singles[uint64(n)<<32|uint64(code)]; ok {
		best, u = s.seq, s.dst
	}
	cost := 1
	for i := len(m.ranges) - 1; i >= 0 && m.ranges[i].seq > best; i-- {
		cost++
		r := m.ranges[i]
		if r.len != n || code < r.lo || code > r.hi {
			continue
		}
		off := code - r.lo
		switch {
		case r.list != nil:
			if int(off) >= len(r.list) {
				return "", false, cost
			}
			u = r.list[off]
		case len(r.dst) > 0:
			// Only the last byte of the destination varies within a range.
			last := r.dst[len(r.dst)-1]
			if uint32(last&0xFF)+off > 0xFF {
				return "", false, cost
			}
			u = append(append([]uint16{}, r.dst[:len(r.dst)-1]...), last+uint16(off))
		default:
			return "", false, cost
		}
		break
	}
	if u == nil {
		return "", false, cost
	}
	text := string(utf16.Decode(u))
	if text == "" || strings.ContainsRune(text, 0xFFFD) {
		return "", false, cost
	}
	return text, true, cost
}

// font is the mapping used to turn shown strings into text.
type font struct {
	name      string
	kind      string // simple, composite, type3
	cmap      *toUnicode
	encoding  *[256]rune
	ucs2      bool // a predefined UCS-2 or UTF-16 CMap: codes are UTF-16BE
	codeBytes int
	reason    string // why codes without a mapping are unmapped
}

// decode returns the text of one shown string, the number of codes that could
// not be mapped, and the work done. It stops with ok false when the work would
// exceed budget or ctx is done.
func (f *font) decode(ctx context.Context, s string, budget int) (string, int, int, bool) {
	var b strings.Builder
	missing, cost := 0, 0
	switch {
	case f.kind == "type3":
		return "", len(s), 1, true
	case f.ucs2:
		u := units(s)
		for _, r := range utf16.Decode(u) {
			switch {
			case r == 0xFFFD:
				missing++
			case r == '\t' || r == ' ':
				b.WriteByte(' ')
			case r >= 0x20:
				b.WriteRune(r)
			default:
				missing++
			}
		}
		return b.String(), missing, len(u) + 1, len(u) <= budget
	}
	for i := 0; i < len(s); {
		if cost > budget {
			return "", 0, cost, false
		}
		if cost&4095 == 0 && ctx.Err() != nil {
			return "", 0, cost, false
		}
		n := f.codeBytes
		if f.cmap != nil {
			var c int
			n, c = f.cmap.codeLength(s[i:], f.codeBytes)
			cost += c
		}
		n = max(1, min(n, len(s)-i))
		code := be(s[i : i+n])
		i += n
		cost++
		if f.cmap != nil {
			text, ok, c := f.cmap.lookup(code, n)
			cost += c
			if ok {
				b.WriteString(clean(text))
				continue
			}
		}
		if f.encoding != nil && n == 1 {
			if r := f.encoding[code]; r != unmapped {
				b.WriteRune(r)
				continue
			}
		}
		missing++
	}
	return b.String(), missing, cost, cost <= budget
}

// clean replaces control characters from a mapping with spaces.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7F || r == 0xFFFD {
			return ' '
		}
		return r
	}, s)
}
