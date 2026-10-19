package present

import (
	"fmt"
	"strings"

	"github.com/blisspixel/nemalo/internal/acquisition"
)

func Acquisition(r acquisition.Result) string {
	var b strings.Builder
	state := "incomplete"
	switch {
	case strings.HasPrefix(r.Status, "detected_"):
		state = "not kept as intake"
	case r.Complete:
		state = "complete"
	case r.TransferComplete:
		state = "transfer complete, checks incomplete"
	}
	fmt.Fprintf(&b, "Acquisition: %s, %s\n", words(r.Status), state)
	field(&b, "Output", Exact(r.Output))
	field(&b, "Content", Exact(r.ContentPath))
	fmt.Fprintf(&b, "Transferred: %d bytes\n", r.Transfer.Bytes)
	field(&b, "SHA-256", Line(r.Transfer.SHA256))
	antivirus := words(r.Antivirus)
	if r.Scan != nil && r.Scan.Tool != "" {
		antivirus += ", by " + Line(r.Scan.Tool)
	}
	field(&b, "Antivirus", antivirus)
	if r.Continuations > 0 {
		fmt.Fprintf(&b, "Continued after %d dropped connection(s), each from the retained bytes with the same validator.\n", r.Continuations)
	}
	switch {
	case r.ResumedFrom > 0 && r.Complete:
		fmt.Fprintf(&b, "Resumed from byte %d; the whole file was verified.\n", r.ResumedFrom)
	case r.ResumedFrom > 0:
		fmt.Fprintf(&b, "Resume from byte %d did not complete; the partial content was kept for another --resume.\n", r.ResumedFrom)
	}
	if r.Intent != nil {
		e := r.Intent.Selection
		field(&b, "Source", Line(r.Intent.Request.SourceID))
		field(&b, "Selected file", Line(r.Intent.Request.File))
		field(&b, "Title", list(e.Title))
		field(&b, "Declared rights", list(e.Rights))
		field(&b, "Declared license", list(e.LicenseURLs))
		field(&b, "Metadata SHA-256", Line(e.MetadataSHA256))
	}
	field(&b, "Size note", Line(r.SizeNote))
	if r.Checks != nil {
		field(&b, "Signature", words(r.Checks.Signature))
		if r.Checks.EPUBPackage != "" && r.Checks.EPUBPackage != "not_applicable" {
			field(&b, "EPUB package", words(r.Checks.EPUBPackage))
		}
		notes(&b, "Warning", r.Checks.Warnings)
	}
	switch r.Status {
	case "detected_quarantined":
		fmt.Fprintf(&b, "Detection: the scanner flagged this download. It was moved to %s and will not be imported.\n", Exact(r.QuarantinedPath))
	case "detected_deleted":
		b.WriteString("Detection: the scanner flagged this download. It was deleted and will not be imported.\n")
	case "detected_retained":
		b.WriteString("Detection: the scanner flagged this download, but it could not be removed from the folder above. Do not open it. It will not be imported.\n")
	case "detected_changed_during_scan":
		b.WriteString("Detection: the scanner flagged this download, and the file changed or disappeared during the scan, possibly through host antivirus. Do not open anything left in the folder above. It will not be imported.\n")
	}
	if strings.HasPrefix(r.Status, "detected_") {
		writeAlternatives(&b, r)
		b.WriteString("A detection can be a false positive; a no-detection result is not proof of safety.\n")
		return b.String()
	}
	b.WriteString("Untrusted intake only. File rights, DRM, malware safety, and semantic completeness are not established.\n")
	switch {
	case r.Complete:
		b.WriteString("Incomplete folders are retained, never deleted. --resume continues a packet that has a transfer record and partial bytes.\n")
	case r.ResumedFrom > 0:
		b.WriteString("Incomplete folders are retained, never deleted.\n")
	case r.TransferRecorded && r.Transfer.Bytes > 0 && !r.TransferComplete && r.StrongValidator:
		b.WriteString("Incomplete folders are retained, never deleted. This packet has a transfer record; --resume continues the interrupted transfer.\n")
	case r.TransferRecorded && r.Transfer.Bytes > 0 && !r.TransferComplete:
		b.WriteString("Incomplete folders are retained, never deleted. This packet has a transfer record, but the source sent no strong validator, so it cannot be resumed; start a new output.\n")
	case r.Output != "" && !r.TransferRecorded:
		b.WriteString("Incomplete folders are retained, never deleted. This packet has no transfer record, so its bytes cannot be resumed; start a new output.\n")
	default:
		b.WriteString("Incomplete folders are retained, never deleted.\n")
	}
	return b.String()
}

// writeAlternatives prints ready-to-run commands. Values are quoted for the
// shell, and a name a terminal cannot show is left out; nothing is fetched
// until the user runs one.
func writeAlternatives(b *strings.Builder, r acquisition.Result) {
	source := ""
	if r.Intent != nil {
		source = r.Intent.Request.SourceID
	}
	if len(r.Alternatives) > 0 {
		b.WriteString("Other files in the same item (source declarations, not verified):\n")
		for _, a := range r.Alternatives {
			if c := command("nemalo", "acquire", source, "--file", a.File, "--output", "NEW_DIRECTORY"); c != "" {
				fmt.Fprintf(b, "  %s   (%s, %d bytes)\n", c, a.Format, a.Bytes)
			}
		}
	}
	if c := command("nemalo", "search", r.SearchSuggestion, "--source", "archive"); r.SearchSuggestion != "" && c != "" {
		fmt.Fprintf(b, "Search for another source:\n  %s\n", c)
	}
}

// Recording summarizes a whole-recording acquisition, one line per track.
func Recording(r acquisition.RecordingResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Recording: %s, %s\n", words(r.Status), map[bool]string{true: "complete", false: "incomplete"}[r.Complete])
	field(&b, "Source", Line(r.SourceID))
	format := Line(r.Format)
	if r.OrderSource != "" {
		format += ", in order from " + words(r.OrderSource)
	}
	field(&b, "Format", format)
	field(&b, "Output", Exact(r.Output))
	fmt.Fprintf(&b, "Tracks: %d | Declared bytes: %d\n", len(r.Tracks), r.DeclaredBytes)
	for _, t := range r.Tracks {
		fmt.Fprintf(&b, "%d. %s %s", t.Position, Exact(t.File), words(t.Status))
		if t.Packet != "" {
			fmt.Fprintf(&b, " in %s", Exact(t.Packet))
		}
		if t.Antivirus != "" {
			fmt.Fprintf(&b, " | antivirus %s", words(t.Antivirus))
		}
		if t.Reused {
			b.WriteString(" (kept from an earlier run)")
		}
		b.WriteString("\n")
		if t.Detail != "" {
			fmt.Fprintf(&b, "   %s\n", Line(t.Detail))
		}
	}
	notes(&b, "Limitation", r.Limitations)
	switch {
	case r.Complete:
		if c := command("nemalo", "library", "ingest", "LIBRARY", r.Output, "--apply"); c != "" {
			fmt.Fprintf(&b, "Next: %s\n", c)
		}
	case r.Status == "stopped_on_detection":
		b.WriteString("A scanner flagged a track, so the recording stopped. Review that track before resuming. A detection can be a false positive.\n")
	case r.Output != "":
		b.WriteString("Retained for review.")
		if c := command("nemalo", "acquire", r.SourceID, "--recording", r.Format, "--output", r.Output, "--resume"); c != "" {
			fmt.Fprintf(&b, " Continue with: %s", c)
		}
		b.WriteString("\n")
	}
	return b.String()
}
