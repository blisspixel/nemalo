package content

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const structuredExtractor = "nemalo.epub.structure/1"

type Part struct {
	ID              string   `json:"id"`
	Kind            string   `json:"kind"`
	Parent          string   `json:"parent,omitempty"`
	Language        string   `json:"language"`
	Direction       string   `json:"direction"`
	Start           int      `json:"start"`
	End             int      `json:"end"`
	SourceStart     int      `json:"source_start"`
	SourceEnd       int      `json:"source_end"`
	Locator         *Locator `json:"locator,omitempty"`
	Reference       string   `json:"reference,omitempty"`
	SourceReference string   `json:"source_reference,omitempty"`
	Gap             string   `json:"gap,omitempty"`
	Link            *Link    `json:"link,omitempty"`
	Image           *Image   `json:"image,omitempty"`
}

type Link struct {
	Href       string `json:"href"`
	Status     string `json:"status"`
	TargetUnit int    `json:"target_unit"`
	TargetPart string `json:"target_part,omitempty"`
	Reference  string `json:"reference,omitempty"`
	Cyclic     bool   `json:"cyclic"`
}

type Image struct {
	Source    string  `json:"source"`
	Alt       *string `json:"alt"`
	Status    string  `json:"status"`
	Member    string  `json:"member,omitempty"`
	SHA256    string  `json:"sha256,omitempty"`
	Bytes     int     `json:"bytes,omitempty"`
	Reference string  `json:"reference,omitempty"`
}

type Coverage struct {
	Text                   string   `json:"text"`
	KnownGaps              []string `json:"known_gaps"`
	ReferencedOutsideRange []string `json:"referenced_outside_range"`
	Unknown                []string `json:"unknown"`
}

type ResourceData struct {
	Member     string `json:"member"`
	SHA256     string `json:"sha256"`
	TotalBytes int    `json:"total_bytes"`
	Encoding   string `json:"encoding"`
	Data       []byte `json:"data"`
}

type document struct {
	unit  Unit
	text  string
	parts []Part
	raw   []byte
}
type frame struct {
	part                int
	language, direction string
	skip                bool
}

func attr(t xml.StartElement, name string) string {
	for _, a := range t.Attr {
		if a.Name.Local == name && a.Name.Space == "" {
			return a.Value
		}
	}
	return ""
}

func hasAttr(t xml.StartElement, name string) bool {
	for _, a := range t.Attr {
		if a.Name.Local == name && a.Name.Space == "" {
			return true
		}
	}
	return false
}

func partKind(t xml.StartElement) string {
	for _, a := range t.Attr {
		if a.Name.Local == "type" && a.Name.Space == "http://www.idpf.org/2007/ops" {
			for _, v := range strings.Fields(a.Value) {
				switch v {
				case "footnote", "endnote", "note", "bibliography", "biblioref", "noteref", "backlink", "verse", "stanza":
					return v
				}
			}
		}
	}
	switch t.Name.Local {
	case "p":
		return "paragraph"
	case "h1", "h2", "h3", "h4", "h5", "h6":
		return "heading"
	case "pre":
		return "preformatted"
	case "section", "article", "figure", "figcaption", "table", "math", "img", "a":
		return t.Name.Local
	}
	if attr(t, "id") != "" {
		return "anchor"
	}
	return ""
}

func extractDocument(ctx context.Context, data []byte, budget int) (document, error) {
	d := document{}
	if !utf8.Valid(data) {
		return d, errUnsupported
	}
	z := xml.NewDecoder(bytes.NewReader(data))
	var text strings.Builder
	stack := []frame{}
	ids := map[string]bool{}
	body, ended, rootSeen := false, false, false
	appendText := func(s string) error {
		if text.Len()+len(s) > budget {
			return errBudget
		}
		text.WriteString(s)
		return nil
	}
	boundary := func() error {
		if text.Len() > 0 && !strings.HasSuffix(text.String(), "\n") {
			return appendText("\n")
		}
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return d, err
		}
		before := int(z.InputOffset())
		token, err := z.Token()
		after := int(z.InputOffset())
		if errors.Is(err, io.EOF) {
			if !ended {
				return d, errUnsupported
			}
			d.text = text.String()
			return d, nil
		}
		if err != nil {
			return d, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			if len(stack) >= 256 {
				return d, errBudget
			}
			if len(stack) == 0 && rootSeen {
				return d, errors.New("multiple XML document elements")
			}
			rootSeen = true
			f := frame{part: -1, language: "unknown", direction: "unknown"}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				f.language, f.direction, f.skip = parent.language, parent.direction, parent.skip
			}
			for _, a := range t.Attr {
				if a.Name.Local == "lang" && (a.Name.Space == "" || a.Name.Space == "http://www.w3.org/XML/1998/namespace") {
					f.language = a.Value
				}
				if a.Name.Local == "dir" && a.Name.Space == "" {
					f.direction = a.Value
				}
			}
			// In XML-based XHTML, xml:lang takes precedence over lang.
			for _, a := range t.Attr {
				if a.Name.Local == "lang" && a.Name.Space == "http://www.w3.org/XML/1998/namespace" {
					f.language = a.Value
				}
			}
			if len(f.language) > 128 || len(f.direction) > 128 {
				return d, errBudget
			}
			if t.Name.Local == "body" {
				if body || ended {
					return d, errUnsupported
				}
				body = true
				d.unit.Language, d.unit.Direction = f.language, f.direction
			}
			if body && !f.skip {
				switch t.Name.Local {
				case "script", "iframe", "object", "embed":
					return d, errUnsupported
				}
				for _, a := range t.Attr {
					v := strings.ToLower(strings.TrimSpace(a.Value))
					if strings.HasPrefix(strings.ToLower(a.Name.Local), "on") || ((a.Name.Local == "href" || a.Name.Local == "src") && strings.HasPrefix(v, "javascript:")) {
						return d, errUnsupported
					}
				}
				kind := partKind(t)
				gap := ""
				switch t.Name.Local {
				case "img":
					gap = "image_pixels_not_text"
				case "table":
					gap = "table_relationships_not_extracted"
				case "math":
					gap = "mathematical_layout_not_extracted"
				case "svg", "audio", "video", "canvas", "object", "iframe", "embed", "template", "noscript", "script", "style":
					gap = "unsupported_embedded_content"
				}
				if t.Name.Space != "" && t.Name.Space != "http://www.w3.org/1999/xhtml" {
					if t.Name.Local != "math" {
						gap = "unsupported_namespace"
					}
				}
				if hasAttr(t, "hidden") || strings.EqualFold(attr(t, "aria-hidden"), "true") {
					gap = "hidden_content_not_rendered"
				}
				if gap != "" && kind == "" {
					kind = "gap"
				}
				if gap == "" {
					switch t.Name.Local {
					case "p", "div", "section", "article", "h1", "h2", "h3", "h4", "h5", "h6", "li", "ul", "ol", "blockquote", "pre", "hr":
						if err := boundary(); err != nil {
							return d, err
						}
					case "br":
						if err := appendText("\n"); err != nil {
							return d, err
						}
					}
				}
				if kind != "" {
					if len(d.parts) >= 1024 {
						return d, fmt.Errorf("%w: more than 1024 parts in one XHTML member", errBudget)
					}
					id := attr(t, "id")
					if len(id) > 253 {
						return d, errBudget
					}
					if id != "" {
						if ids[id] {
							return d, errors.New("duplicate source element ID")
						}
						ids[id] = true
						id = "id:" + id
					} else {
						id = fmt.Sprintf("part:%d", len(d.parts))
					}
					if !referenceFits(id, "", "") {
						return d, errBudget
					}
					parent := ""
					for i := len(stack) - 1; i >= 0; i-- {
						if stack[i].part >= 0 {
							parent = d.parts[stack[i].part].ID
							break
						}
					}
					p := Part{ID: id, Kind: kind, Parent: parent, Language: f.language, Direction: f.direction, Start: text.Len(), SourceStart: before, Gap: gap}
					if t.Name.Local == "a" && attr(t, "href") != "" {
						p.Link = &Link{Href: attr(t, "href"), Status: "unresolved", TargetUnit: -1}
					}
					if t.Name.Local == "img" {
						p.Image = &Image{Source: attr(t, "src"), Status: "unresolved"}
						for _, a := range t.Attr {
							if a.Name.Local == "alt" && a.Name.Space == "" {
								value := a.Value
								p.Image.Alt = &value
							}
						}
					}
					if (p.Link != nil && len(p.Link.Href) > 2048) || (p.Image != nil && (len(p.Image.Source) > 2048 || (p.Image.Alt != nil && len(*p.Image.Alt) > 4096))) {
						return d, errBudget
					}
					f.part = len(d.parts)
					d.parts = append(d.parts, p)
				}
				f.skip = gap != ""
			}
			stack = append(stack, f)
		case xml.CharData:
			if len(stack) == 0 && strings.TrimSpace(string(t)) != "" {
				return d, errors.New("text outside XML document element")
			}
			if body && len(stack) > 0 && !stack[len(stack)-1].skip {
				s := strings.ReplaceAll(strings.ReplaceAll(string(t), "\r\n", "\n"), "\r", "\n")
				if err := appendText(s); err != nil {
					return d, err
				}
			}
		case xml.EndElement:
			if len(stack) == 0 {
				return d, errUnsupported
			}
			f := stack[len(stack)-1]
			if body && !f.skip {
				switch t.Name.Local {
				case "p", "div", "section", "article", "h1", "h2", "h3", "h4", "h5", "h6", "li", "ul", "ol", "blockquote", "pre", "hr":
					if err := boundary(); err != nil {
						return d, err
					}
				}
			}
			if f.part >= 0 {
				d.parts[f.part].End, d.parts[f.part].SourceEnd = text.Len(), after
			}
			stack = stack[:len(stack)-1]
			if t.Name.Local == "body" {
				body, ended = false, true
			}
		}
	}
}
