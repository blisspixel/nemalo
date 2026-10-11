package present

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/blisspixel/nemalo/internal/content"
)

// Content escapes each source line before terminal rendering. Newlines carry
// source structure; terminal controls remain inert, visible escape sequences.
// Omitted images, tables, and other gaps appear as bracketed display markers at
// their located offsets. Markers are presentation only, never source text.
func Content(r content.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Source access: %q\nAsset: %q\nRepresentation: %q\n", r.Status, r.AssetID, r.Representation)
	if r.Status == "units_available" {
		writeUnits(&b, r)
	}
	if r.Locator != nil {
		fmt.Fprintf(&b, "Unit %d | bytes %d..%d (%s)\n", r.Locator.Unit, r.Locator.Start, r.Locator.End, offsetUnit(r.Locator))
	}
	if r.Resource != nil {
		fmt.Fprintf(&b, "Resource %q | SHA-256 %q | %d of %d bytes. Bytes are not rendered here; use --json for base64 data.\n", r.Resource.Member, r.Resource.SHA256, len(r.Resource.Data), r.Resource.TotalBytes)
	}
	markers := gapMarkers(r)
	if r.Coverage != nil {
		fmt.Fprintf(&b, "Coverage: %q\n", r.Coverage.Text)
		if len(markers) > 0 {
			fmt.Fprintf(&b, "Omitted from text: %d located gap(s), marked [ ] below.\n", len(markers))
		}
		fmt.Fprintf(&b, "References outside range: %d\n", len(r.Coverage.ReferencedOutsideRange))
		for _, unknown := range r.Coverage.Unknown {
			fmt.Fprintf(&b, "Not established: %q\n", unknown)
		}
	}
	if r.Text != "" || len(markers) > 0 {
		b.WriteString("\n")
		writeText(&b, r, markers)
	}
	if r.Continuation != "" {
		b.WriteString("\nNext range available. Explicit continuation required.\n")
	}
	b.WriteString("Delivery is not evidence of reading or understanding.\n")
	return b.String()
}

// ContentCommand adds what a command-line caller needs to continue explicitly.
func ContentCommand(r content.Result) string {
	s := Content(r)
	if r.Continuation != "" {
		s += fmt.Sprintf("Continue with: --cursor %s\n", r.Continuation)
	}
	return s
}

func offsetUnit(l *content.Locator) string {
	if l.OffsetUnit == "" {
		return "utf8_bytes"
	}
	return l.OffsetUnit
}

func writeUnits(b *strings.Builder, r content.Result) {
	fmt.Fprintf(b, "Reading-order units: %d (not necessarily chapters)\n", len(r.Units))
	for _, u := range r.Units {
		fmt.Fprintf(b, "  %3d  %-26s  %8d text bytes  %d gaps  linear %s  %q\n", u.Index, u.Extraction, u.TextBytes, u.KnownGaps, u.Linear, u.Member)
	}
	if len(r.UnsupportedUnits) > 0 {
		fmt.Fprintf(b, "Unsupported units %v return no text; select another unit explicitly.\n", r.UnsupportedUnits)
	}
}

type marker struct {
	offset int
	label  string
}

func gapMarkers(r content.Result) []marker {
	if r.Locator == nil || r.Representation == "epub-source/1" {
		return nil
	}
	markers := []marker{}
	for _, g := range r.Gaps {
		markers = append(markers, marker{g.Offset, gapLabel(g.Element, g.Reason, g.Image)})
	}
	if r.Coverage != nil && len(r.Gaps) == 0 {
		known := map[string]bool{}
		for _, id := range r.Coverage.KnownGaps {
			known[id] = true
		}
		for _, p := range r.Parts {
			if p.Gap == "" || !known[p.ID] {
				continue
			}
			var image *content.GapImage
			element := p.Kind
			if p.Image != nil {
				element, image = "img", &content.GapImage{Source: p.Image.Source, Alt: p.Image.Alt}
			}
			markers = append(markers, marker{p.Start, gapLabel(element, p.Gap, image)})
		}
	}
	sort.SliceStable(markers, func(i, j int) bool { return markers[i].offset < markers[j].offset })
	return markers
}

func gapLabel(element, reason string, image *content.GapImage) string {
	switch {
	case image != nil && image.Alt == nil:
		return "[image, no alt text]"
	case image != nil && *image.Alt == "":
		return "[image, empty alt text]"
	case image != nil:
		return "[image: " + escapeLine(*image.Alt) + "]"
	case reason == "table_relationships_not_extracted":
		return "[table omitted]"
	case reason == "mathematical_layout_not_extracted":
		return "[mathematics omitted]"
	case reason == "hidden_content_not_rendered":
		return "[hidden content omitted]"
	}
	return "[" + escapeLine(element) + " omitted: " + escapeLine(reason) + "]"
}

func writeText(b *strings.Builder, r content.Result, markers []marker) {
	start := 0
	if r.Locator != nil {
		start = r.Locator.Start
	}
	cursor := 0
	for _, m := range markers {
		at := min(max(m.offset-start, cursor), len(r.Text))
		writeEscaped(b, r.Text[cursor:at])
		b.WriteString(m.label)
		cursor = at
	}
	writeEscaped(b, r.Text[cursor:])
	if !strings.HasSuffix(b.String(), "\n") {
		b.WriteByte('\n')
	}
}

// writeEscaped keeps real newlines and escapes everything else in each line.
func writeEscaped(b *strings.Builder, s string) {
	for i, line := range strings.Split(s, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(escapeLine(line))
	}
}

func escapeLine(s string) string {
	quoted := strconv.Quote(s)
	return quoted[1 : len(quoted)-1]
}
