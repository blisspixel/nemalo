package present

import (
	"fmt"
	"strings"

	"github.com/blisspixel/nemalo/internal/assessment"
)

func Health(r assessment.Report) string {
	var b strings.Builder
	field(&b, "File", Exact(r.File))
	field(&b, "Status", words(r.Status))
	field(&b, "Detected format", words(r.DetectedFormat))
	fmt.Fprintf(&b, "Bytes: %d\n", r.Bytes)
	field(&b, "SHA-256", Line(r.SHA256))
	field(&b, "Antivirus", words(r.Antivirus.Status))
	if e := r.Checks.EPUB; e != nil {
		fmt.Fprintf(&b, "EPUB: %d reading-order HTML documents, %d images, %d non-whitespace body text characters, %d expanded bytes\nPages: unknown (EPUB has no universal fixed page count)\n", e.ReadingOrderDocuments, e.Images, e.TextCharacters, e.ExpandedBytes)
		field(&b, "Title", list(e.Titles))
		field(&b, "Language", list(e.Languages))
	}
	if m := r.Checks.MP3; m != nil {
		fmt.Fprintf(&b, "MP3: %s, %d Hz, %d channel(s), %d frames, %s encoded, from frame headers\n", Line(m.Format), m.SampleRate, m.Channels, m.Frames, clock(m.EncodedDurationMs))
		notes(&b, "Note", m.Notes)
	}
	if m := r.Checks.MP4; m != nil {
		fmt.Fprintf(&b, "MP4 audio: %s, %d Hz, %d channel(s), %d samples, %s encoded, from sample tables\n", words(m.Codec), m.SampleRate, m.Channels, m.Samples, clock(m.EncodedDurationMs))
		if m.Chapters > 0 {
			fmt.Fprintf(&b, "Chapters: %d declared (%s)\n", m.Chapters, words(m.ChapterSource))
		}
		notes(&b, "Note", m.Notes)
	}
	if d := r.Decode; d != nil {
		switch d.Status {
		case "passed", "errors_reported":
			fmt.Fprintf(&b, "Decoded by FFmpeg %s: %s, %s at %d Hz, %d error(s)\n", Line(d.Version), words(d.Status), clock(d.DecodedDurationMs), d.SampleRate, d.Errors)
			for _, m := range d.Messages {
				fmt.Fprintf(&b, "  %s\n", Line(m))
			}
			if d.MessagesOmitted > 0 {
				fmt.Fprintf(&b, "  %d more message(s) omitted\n", d.MessagesOmitted)
			}
		default:
			fmt.Fprintf(&b, "Decode: %s", words(d.Status))
			if d.Detail != "" {
				fmt.Fprintf(&b, " (%s)", Line(d.Detail))
			}
			b.WriteString("\n")
		}
	}
	if c := r.Conformance; c != nil {
		switch c.Status {
		case "passed", "errors_reported":
			fmt.Fprintf(&b, "EPUBCheck %s: %s, %d fatal, %d error(s), %d warning(s)\n", Line(c.Version), words(c.Status), c.Fatal, c.Errors, c.Warnings)
			for _, m := range c.Messages {
				if m.Severity == "INFO" || m.Severity == "USAGE" {
					continue
				}
				where := ""
				if len(m.Locations) > 0 {
					l := m.Locations[0]
					where = " at " + Line(l.Path)
					if l.Line > 0 {
						where += fmt.Sprintf(":%d", l.Line)
					}
				}
				fmt.Fprintf(&b, "  %s %s%s: %s\n", words(strings.ToLower(m.Severity)), Line(m.ID), where, Line(m.Text))
			}
			if c.MessagesOmitted > 0 {
				fmt.Fprintf(&b, "  %d more message(s) omitted\n", c.MessagesOmitted)
			}
		default:
			fmt.Fprintf(&b, "EPUBCheck: %s", words(c.Status))
			if c.Detail != "" {
				fmt.Fprintf(&b, " (%s)", Line(c.Detail))
			}
			b.WriteString("\n")
		}
	}
	notes(&b, "Review", r.Findings)
	field(&b, "Scanner output", Line(r.Antivirus.Output))
	field(&b, "Scanner error", Line(r.Antivirus.Error))
	if r.Antivirus.OutputTruncated {
		b.WriteString("Scanner output exceeded 64 KiB; result incomplete.\n")
	}
	notes(&b, "Limit", append(append([]string{}, r.Limitations...), r.Antivirus.Limitations...))
	return b.String()
}
