package present

import (
	"fmt"
	"strings"

	"github.com/blisspixel/nemalo/internal/assessment"
)

func Health(r assessment.Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "File: %q\nStatus: %s\nDetected format: %q\nBytes: %d\nSHA-256: %q\nAntivirus: %s\n", r.File, r.Status, r.DetectedFormat, r.Bytes, r.SHA256, r.Antivirus.Status)
	if e := r.Checks.EPUB; e != nil {
		fmt.Fprintf(&b, "EPUB: %d reading-order HTML documents, %d images, %d non-whitespace body text characters, %d expanded bytes\nPages: unknown (EPUB has no universal fixed page count)\n", e.ReadingOrderDocuments, e.Images, e.TextCharacters, e.ExpandedBytes)
		for _, title := range e.Titles {
			fmt.Fprintf(&b, "Title: %q\n", title)
		}
		for _, lang := range e.Languages {
			fmt.Fprintf(&b, "Language: %q\n", lang)
		}
	}
	for _, finding := range r.Findings {
		fmt.Fprintf(&b, "Review: %q\n", finding)
	}
	if r.Antivirus.Output != "" {
		fmt.Fprintf(&b, "Scanner output: %q\n", r.Antivirus.Output)
	}
	if r.Antivirus.Error != "" {
		fmt.Fprintf(&b, "Scanner error: %q\n", r.Antivirus.Error)
	}
	if r.Antivirus.OutputTruncated {
		b.WriteString("Scanner output exceeded 64 KiB; result incomplete.\n")
	}
	for _, limitation := range append(append([]string{}, r.Limitations...), r.Antivirus.Limitations...) {
		fmt.Fprintf(&b, "Limit: %q\n", limitation)
	}
	return b.String()
}
