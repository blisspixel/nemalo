package content

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

// extractText v1 retains decoded XHTML body text and source whitespace,
// normalizes CRLF/CR to LF, and adds explicit LF boundaries for common blocks/br.
// XML tokenization preserves CDATA and namespace-qualified XHTML text. No CSS,
// custom entities, rendering, remote resources, or Unicode normalization apply.
func extractText(ctx context.Context, data []byte, budget int) (string, error) {
	if !utf8.Valid(data) {
		return "", errUnsupported
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var out strings.Builder
	body, ended := false, false
	depth, rootSeen := 0, false
	last := byte(0)
	appendText := func(s string) error {
		if out.Len()+len(s) > budget {
			return errBudget
		}
		out.WriteString(s)
		if len(s) != 0 {
			last = s[len(s)-1]
		}
		return nil
	}
	boundary := func() error {
		if out.Len() != 0 && last != '\n' {
			return appendText("\n")
		}
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			if !ended || out.Len() == 0 {
				return "", errUnsupported
			}
			return out.String(), nil
		}
		if err != nil {
			return "", err
		}
		var name xml.Name
		start, end := false, false
		switch t := token.(type) {
		case xml.StartElement:
			if depth == 0 && rootSeen {
				return "", errors.New("multiple XML document elements")
			}
			depth++
			rootSeen = true
			name, start = t.Name, true
			if name.Local == "body" {
				if body || ended {
					return "", errUnsupported
				}
				body = true
			}
			if body {
				if name.Space != "" && name.Space != "http://www.w3.org/1999/xhtml" {
					return "", errUnsupported
				}
				switch name.Local {
				case "script", "style", "template", "noscript", "iframe", "object", "embed", "svg", "math", "img", "audio", "video", "canvas", "table":
					return "", errUnsupported
				}
				for _, a := range t.Attr {
					if a.Name.Local == "hidden" || (a.Name.Local == "aria-hidden" && strings.EqualFold(a.Value, "true")) {
						return "", errUnsupported
					}
				}
			}
		case xml.EndElement:
			depth--
			name, end = t.Name, true
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(t)) != "" {
				return "", errors.New("text outside XML document element")
			}
			if body {
				s := strings.ReplaceAll(strings.ReplaceAll(string(t), "\r\n", "\n"), "\r", "\n")
				if err := appendText(s); err != nil {
					return "", err
				}
			}
		}
		if body && (start || end) {
			switch name.Local {
			case "br":
				if start {
					if err := appendText("\n"); err != nil {
						return "", err
					}
				}
			case "p", "div", "section", "article", "h1", "h2", "h3", "h4", "h5", "h6", "li", "ul", "ol", "blockquote", "pre", "hr":
				if err := boundary(); err != nil {
					return "", err
				}
			}
		}
		if end && name.Local == "body" {
			if !body {
				return "", errUnsupported
			}
			body, ended = false, true
		}
	}
}
