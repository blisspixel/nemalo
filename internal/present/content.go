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
	fmt.Fprintf(&b, "Source access: %s\n", words(r.Status))
	field(&b, "Asset", Line(r.AssetID))
	field(&b, "Representation", Line(r.Representation))
	if e := r.Evidence; e != nil {
		field(&b, "Evidence", Line(e.Schema)+" | "+words(e.Kind)+" | single returned range")
		field(&b, "Evidence reference", Line(e.Reference))
		notes(&b, "Evidence limit", e.Limitations)
	}
	// An audio track is one unit, described by its index below. A page list
	// lists its markers instead of the spine units.
	if r.Status == "units_available" && r.Audio == nil && r.MP4 == nil && r.MP4Packets == nil && r.MP4PCM == nil && r.Representation != "epub-page-list/1" {
		writeUnits(&b, r)
	}
	if len(r.Navigation) > 0 {
		writeNavigation(&b, r)
	}
	if r.Representation == "epub-page-list/1" {
		writePages(&b, r)
	}
	if r.Locator != nil {
		if r.Representation == "pdf-text/1" {
			fmt.Fprintf(&b, "Page index %d (zero-based) | printed label %s | bytes %d..%d (%s)\n", r.Locator.Unit, pageLabel(r.Locator.PageLabel), r.Locator.Start, r.Locator.End, words(offsetUnit(r.Locator)))
		} else if r.Representation == content.MP4PacketRepresentation {
			fmt.Fprintf(&b, "Unit %d | packets %d..%d (zero-based, half-open)\n", r.Locator.Unit, r.Locator.Start, r.Locator.End)
		} else {
			fmt.Fprintf(&b, "Unit %d | bytes %d..%d (%s)\n", r.Locator.Unit, r.Locator.Start, r.Locator.End, words(offsetUnit(r.Locator)))
		}
	}
	if r.Status == "matches_found" || r.Status == "matches_truncated" || r.Status == "no_matches" {
		fmt.Fprintf(&b, "Exact matches: %d", len(r.Matches))
		if r.Status == "matches_truncated" {
			b.WriteString(" (more exist; refine the text)")
		}
		b.WriteString("\n")
		for _, m := range r.Matches {
			where := fmt.Sprintf("unit %d", m.Unit)
			if r.Representation == "pdf-text/1" {
				where = fmt.Sprintf("page index %d, label %s", m.Unit, pageLabel(m.PageLabel))
			}
			fmt.Fprintf(&b, "  %s | bytes %d..%d | read with --cursor %s\n", where, m.Start, m.End, m.Cursor)
		}
	}
	if a := r.Audio; a != nil {
		fmt.Fprintf(&b, "Audio: %s, %d Hz, %d channel(s) %s, %s\n", Line(a.Format), a.SampleRate, a.Channels, words(a.ChannelMode), words(a.Bitrate))
		fmt.Fprintf(&b, "Indexed frames: %d | encoded samples: %d | encoded duration: %d ms | bytes %d..%d\n", a.Frames, a.EncodedSamples, a.EncodedDurationMs, a.AudioStart, a.AudioEnd)
		if a.PresentedSamples != nil {
			fmt.Fprintf(&b, "Presented samples after declared encoder delay and padding: %d (declared, not verified)\n", *a.PresentedSamples)
		}
		if a.Declared.LengthMs != nil {
			fmt.Fprintf(&b, "ID3 declared length: %d ms\n", *a.Declared.LengthMs)
		}
		field(&b, "Declared title", Line(a.Metadata.Title))
		field(&b, "Declared artist", Line(a.Metadata.Artist))
		for _, c := range a.Chapters {
			fmt.Fprintf(&b, "Declared chapter %s: %d..%d ms", Line(c.ID), c.StartMs, c.EndMs)
			if title := Line(c.Title); title != "" {
				fmt.Fprintf(&b, " (%s)", title)
			}
			b.WriteString("\n")
		}
		for _, g := range a.Gaps {
			fmt.Fprintf(&b, "Unindexed bytes %d..%d: %s\n", g.Start, g.End, words(g.Kind))
		}
		notes(&b, "Finding", a.Findings)
		b.WriteString("Decoded: not decoded; timing comes from frame headers.\n")
	}
	if a := r.MP4; a != nil {
		fmt.Fprintf(&b, "Audio: %s in %s, %d Hz, %d channel(s)\n", words(a.Codec), firstNonEmpty(Line(a.Brand), "MP4"), a.SampleRate, a.Channels)
		fmt.Fprintf(&b, "Located samples: %d in %d chunk(s) | %d sample bytes | encoded duration: %s\n", a.Samples, a.Chunks, a.SampleBytes, clock(a.EncodedDurationMs))
		fmt.Fprintf(&b, "Declared media duration: %s | movie duration: %s\n", clock(a.MediaDurationMs), clock(a.MovieDurationMs))
		field(&b, "Declared title", Line(a.Metadata.Title))
		field(&b, "Declared artist", Line(a.Metadata.Artist))
		field(&b, "Declared narrator", Line(a.Metadata.Narrator))
		for _, c := range a.Chapters {
			fmt.Fprintf(&b, "Declared chapter %s: %s..%s", Line(c.ID), clock(c.StartMs), clock(c.EndMs))
			if title := Line(c.Title); title != "" {
				fmt.Fprintf(&b, " (%s)", title)
			}
			b.WriteString("\n")
		}
		field(&b, "Chapter source", words(a.ChapterSource))
		notes(&b, "Finding", a.Findings)
		notes(&b, "Note", a.Notes)
		b.WriteString("Decoded: not decoded; timing comes from the sample tables. Play it with library open or export.\n")
	}
	if a := r.AudioRange; a != nil {
		fmt.Fprintf(&b, "Frames %d..%d (%d preroll frame(s), complete: %t) | samples %d..%d at %d Hz | file bytes %d..%d\n", a.FirstFrame, a.FirstFrame+a.Frames, a.PrerollFrames, a.PrerollComplete, a.SampleStart, a.SampleEnd, a.SampleRate, a.ByteStart, a.ByteEnd)
	}
	if a := r.MP4Packets; a != nil {
		fmt.Fprintf(&b, "Audio: %s | %d source packets | declared %d Hz, %d channel(s)\n", words(a.Codec), a.PacketCount, a.DeclaredRate, a.DeclaredChannels)
		fmt.Fprintf(&b, "Original MP4 packets: %d returned | next %d | stop %s | media timescale %d\n", len(a.Packets), a.NextPacket, words(a.Stop), a.Timescale)
		fmt.Fprintf(&b, "Initialization: original sample entry, stsd version %d, %d bytes | SHA-256 %s\n", a.Initialization.STSDVersion, len(a.Initialization.Data), Line(a.Initialization.SHA256))
		b.WriteString("Packet bytes are not a playable MP4 container. Preroll, PCM equivalence and edited player timing are not established. Use --json for original bytes and exact source spans.\n")
	}
	if a := r.MP4PCM; a != nil {
		fmt.Fprintf(&b, "Decoded output: mono s16le at %d Hz | %s full samples | FFmpeg %s\n", a.OutputRate, Line(a.TotalSamples), Line(a.ToolVersion))
		fmt.Fprintf(&b, "Full PCM SHA-256: %s\n", Line(a.FullPCMSHA256))
		fmt.Fprintf(&b, "Encoded source: %s ticks at %d Hz; declared media %s, movie %s at %d Hz. Declarations are not decoder measurements.\n", Line(a.SourceClock.EncodedMediaTicks), a.SourceClock.MediaTimescale, Line(a.SourceClock.DeclaredMediaTicks), Line(a.SourceClock.DeclaredMovieTicks), a.SourceClock.MovieTimescale)
		if a.Range != nil {
			fmt.Fprintf(&b, "Decoded samples %s..%s (zero-based, half-open) | PCM SHA-256 %s\n", Line(a.Range.StartSample), Line(a.Range.EndSample), Line(a.RangePCMSHA256))
		}
		b.WriteString("Derived bounded WAV; use --json for bytes. Mapping to original packets, chapters and edited player times is not established. No playback or consumption was recorded.\n")
	}
	if r.Resource != nil {
		fmt.Fprintf(&b, "Resource %s | SHA-256 %s | %d of %d bytes. Bytes are not rendered here; use --json for base64 data.\n", Line(r.Resource.Member), Line(r.Resource.SHA256), len(r.Resource.Data), r.Resource.TotalBytes)
	}
	markers := gapMarkers(r)
	if r.Coverage != nil {
		fmt.Fprintf(&b, "Coverage: %s\n", words(r.Coverage.Text))
		if len(markers) > 0 {
			fmt.Fprintf(&b, "Omitted from text: %d located gap(s), marked [ ] below.\n", len(markers))
		}
		fmt.Fprintf(&b, "References outside range: %d\n", len(r.Coverage.ReferencedOutsideRange))
		notes(&b, "Not established", r.Coverage.Unknown)
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

// pageLabel is a printed PDF page label, or "none" when the page has none.
func pageLabel(label string) string {
	if label = Line(label); label != "" {
		return label
	}
	return "none"
}

func offsetUnit(l *content.Locator) string {
	if l.OffsetUnit == "" {
		return "utf8_bytes"
	}
	return l.OffsetUnit
}

func writeUnits(b *strings.Builder, r content.Result) {
	if r.Representation == "pdf-text/1" {
		fmt.Fprintf(b, "Pages: %d (index is zero-based and physical; label is the printed page label)\n", len(r.Units))
		for _, u := range r.Units {
			fmt.Fprintf(b, "  %4d  label %-10s  %-26s  %8d text bytes  %d gaps\n", u.Index, pageLabel(u.PageLabel), words(u.Extraction), u.TextBytes, u.KnownGaps)
		}
		return
	}
	fmt.Fprintf(b, "Reading-order units: %d (not necessarily chapters)\n", len(r.Units))
	for _, u := range r.Units {
		fmt.Fprintf(b, "  %3d  %-26s  %8d text bytes  %d gaps  linear %s  %s\n", u.Index, words(u.Extraction), u.TextBytes, u.KnownGaps, words(u.Linear), Line(u.Member))
	}
	if len(r.UnsupportedUnits) > 0 {
		fmt.Fprintf(b, "Unsupported units %v return no text; select another unit explicitly.\n", r.UnsupportedUnits)
	}
}

// writePages shows declared page markers, the stated pagination source, and
// the marker a read starts at.
func writePages(b *strings.Builder, r content.Result) {
	switch r.Status {
	case "no_page_list":
		b.WriteString("This EPUB declares no page list. No page labels are made up from text length or screen pages.\n")
	case "ambiguous_label":
		b.WriteString("Several page markers carry this label. Choose one with its --cursor:\n")
	}
	if p := r.Pagination; p != nil {
		switch {
		case p.Status == "declared":
			fmt.Fprintf(b, "Pagination source, as the package states it: %s\n", list(append(append([]string{}, p.Sources...), p.PageBreakSources...)))
		default:
			b.WriteString("Pagination source: not stated by the package.\n")
		}
	}
	for _, l := range r.PageLists {
		hidden := ""
		if l.Hidden {
			hidden = ", hidden in reading systems"
		}
		fmt.Fprintf(b, "Page list %s%s: %d marker(s)", Exact(l.Path), hidden, len(l.Entries))
		if l.Problem != "" {
			fmt.Fprintf(b, ", not used: %s", words(l.Problem))
		}
		b.WriteString("\n")
		for _, e := range l.Entries {
			where := words(e.Status)
			if e.Unit != nil && e.Offset != nil {
				where = fmt.Sprintf("unit %d byte %d", *e.Unit, *e.Offset)
			}
			fmt.Fprintf(b, "  %4d  %-10s  %s\n", e.Order, Line(e.Label), where)
			if r.Status == "ambiguous_label" && e.Reference != "" {
				fmt.Fprintf(b, "        --cursor %s\n", e.Reference)
			}
		}
	}
	if p := r.Page; p != nil {
		fmt.Fprintf(b, "Page label %s (marker %d), as the EPUB declares it\n", Line(p.Label), p.Order)
		if r.PageEnd != nil {
			fmt.Fprintf(b, "Next marker: unit %d byte %d\n", r.PageEnd.Unit, r.PageEnd.Start)
		} else {
			b.WriteString("Where this page ends is not established.\n")
		}
	}
}

func writeNavigation(b *strings.Builder, r content.Result) {
	fmt.Fprintf(b, "Declared navigation: %d source(s). Labels and targets come from the book, not from heading text.\n", len(r.Navigation))
	if r.Coverage != nil && r.Coverage.NavigationConflict {
		b.WriteString("NAV and NCX disagree. A read must name --navigation nav-doc or --navigation ncx.\n")
	}
	for _, doc := range r.Navigation {
		path := doc.Path
		if path == "" {
			path = "(none)"
		}
		fmt.Fprintf(b, "  %s %s", words(doc.Evidence), Line(path))
		if doc.Problem != "" {
			fmt.Fprintf(b, "  problem: %s", words(doc.Problem))
		}
		b.WriteByte('\n')
		for _, e := range doc.Entries {
			parent := ""
			if e.Parent != nil {
				parent = fmt.Sprintf("  parent %d", *e.Parent)
			}
			where := words(e.Status)
			if e.Unit != nil && e.Offset != nil {
				where = fmt.Sprintf("unit %d byte %d", *e.Unit, *e.Offset)
			}
			fmt.Fprintf(b, "    %4d  L%d  %-22s  %s%s\n", e.Order, e.Level, Line(e.Label), where, parent)
			if e.Range != nil {
				fmt.Fprintf(b, "          range %s  %d:%d .. %d:%d\n", words(e.Range.Status), e.Range.StartUnit, e.Range.StartOffset, e.Range.EndUnit, e.Range.EndOffset)
			}
		}
	}
	if r.RangeStatus != "" {
		fmt.Fprintf(b, "Declared range: %s\n", words(r.RangeStatus))
		if r.RangeEnd != nil {
			fmt.Fprintf(b, "Declared end: unit %d byte %d\n", r.RangeEnd.Unit, r.RangeEnd.Start)
		}
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
	case element == "no_text_layer":
		return "[no text layer: scanned or image-only page]"
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
