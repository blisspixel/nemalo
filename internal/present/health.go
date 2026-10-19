package present

import (
	"fmt"
	"strings"

	"github.com/blisspixel/nemalo/internal/assessment"
)

func Health(r assessment.Report) string {
	var b strings.Builder
	field(&b, "File", Line(r.File))
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
	notes(&b, "Review", r.Findings)
	field(&b, "Scanner output", Line(r.Antivirus.Output))
	field(&b, "Scanner error", Line(r.Antivirus.Error))
	if r.Antivirus.OutputTruncated {
		b.WriteString("Scanner output exceeded 64 KiB; result incomplete.\n")
	}
	notes(&b, "Limit", append(append([]string{}, r.Limitations...), r.Antivirus.Limitations...))
	return b.String()
}
