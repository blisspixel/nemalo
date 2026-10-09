// Package textsafe encodes untrusted display text without changing source data.
package textsafe

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf16"
)

// Visible preserves prose whitespace for the caller's layout policy. Other
// control and format characters become visible Unicode escapes.
func Visible(s string) string { return escape(s) }

func escape(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t') || unicode.Is(unicode.Cf, r) {
			if r > 0xffff {
				a, c := utf16.EncodeRune(r)
				fmt.Fprintf(&b, "\\u%04x\\u%04x", a, c)
			} else {
				fmt.Fprintf(&b, "\\u%04x", r)
			}
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// MarshalJSON emits parse-equivalent JSON, including object keys. Escaping is
// applied after JSON serialization so literal backslash sequences stay literal.
func MarshalJSON(value any, indent bool) ([]byte, error) {
	var data []byte
	var err error
	if indent {
		data, err = json.MarshalIndent(value, "", "  ")
	} else {
		data, err = json.Marshal(value)
	}
	if err != nil {
		return nil, err
	}
	return []byte(escape(string(data))), nil
}

func WriteJSON(w io.Writer, value any, indent bool) error {
	data, err := MarshalJSON(value, indent)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	n, err := w.Write(data)
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}
	return err
}
