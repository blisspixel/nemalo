package present

import (
	"fmt"
	"strings"

	"github.com/blisspixel/nemalo/internal/acquisition"
)

func Acquisition(r acquisition.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Acquisition: %q\nComplete: %t | Transfer complete: %t\nOutput: %q\nContent: %q\nTransferred: %d bytes\nSHA-256: %q\nAntivirus: %q\n", r.Status, r.Complete, r.TransferComplete, r.Output, r.ContentPath, r.Transfer.Bytes, r.Transfer.SHA256, r.Antivirus)
	if r.Scan != nil && r.Scan.Tool != "" {
		fmt.Fprintf(&b, "Scanner: %q\n", r.Scan.Tool)
	}
	switch {
	case r.ResumedFrom > 0 && r.Complete:
		fmt.Fprintf(&b, "Resumed from byte %d; the whole file was verified.\n", r.ResumedFrom)
	case r.ResumedFrom > 0:
		fmt.Fprintf(&b, "Resume from byte %d did not complete; the partial content was kept for another --resume.\n", r.ResumedFrom)
	}
	if r.Intent != nil {
		e := r.Intent.Selection
		fmt.Fprintf(&b, "Source: %q\nSelected file: %q\nTitles: %q\nDeclared rights: %q\nDeclared licenses: %q\nMetadata SHA-256: %q\n", r.Intent.Request.SourceID, r.Intent.Request.File, e.Title, e.Rights, e.LicenseURLs, e.MetadataSHA256)
	}
	if r.Checks != nil {
		fmt.Fprintf(&b, "Signature: %q | EPUB package: %q\nWarnings: %q\n", r.Checks.Signature, r.Checks.EPUBPackage, r.Checks.Warnings)
	}
	switch r.Status {
	case "detected_quarantined":
		fmt.Fprintf(&b, "Detection: the scanner flagged this download. It was moved to %q and will not be imported.\n", r.QuarantinedPath)
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
	case r.TransferRecorded && r.Transfer.Bytes > 0 && !r.TransferComplete:
		b.WriteString("Incomplete folders are retained, never deleted. This packet has a transfer record; --resume continues the interrupted transfer.\n")
	case r.Output != "" && !r.TransferRecorded:
		b.WriteString("Incomplete folders are retained, never deleted. This packet has no transfer record, so its bytes cannot be resumed; start a new output.\n")
	default:
		b.WriteString("Incomplete folders are retained, never deleted.\n")
	}
	return b.String()
}

// writeAlternatives prints ready-to-run commands. Values are quoted for display;
// nothing is fetched until the user runs one.
func writeAlternatives(b *strings.Builder, r acquisition.Result) {
	source := ""
	if r.Intent != nil {
		source = r.Intent.Request.SourceID
	}
	if len(r.Alternatives) > 0 {
		b.WriteString("Other files in the same item (source declarations, not verified):\n")
		for _, a := range r.Alternatives {
			fmt.Fprintf(b, "  nemalo acquire %s --file %q --output NEW_DIRECTORY   (%s, %d bytes)\n", source, a.File, a.Format, a.Bytes)
		}
	}
	if r.SearchSuggestion != "" {
		fmt.Fprintf(b, "Search for another source:\n  nemalo search %q --source archive\n", r.SearchSuggestion)
	}
}

// Recording summarizes a whole-recording acquisition, one line per track.
func Recording(r acquisition.RecordingResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Recording: %q\nComplete: %t\nSource: %q\nFormat: %q | Order from: %q\nOutput: %q\nTracks: %d | Declared bytes: %d\n", r.Status, r.Complete, r.SourceID, r.Format, r.OrderSource, r.Output, len(r.Tracks), r.DeclaredBytes)
	for _, t := range r.Tracks {
		fmt.Fprintf(&b, "%d. %q %s", t.Position, t.File, t.Status)
		if t.Packet != "" {
			fmt.Fprintf(&b, " in %q", t.Packet)
		}
		if t.Antivirus != "" {
			fmt.Fprintf(&b, " | antivirus %s", t.Antivirus)
		}
		if t.Reused {
			b.WriteString(" (kept from an earlier run)")
		}
		b.WriteString("\n")
		if t.Detail != "" {
			fmt.Fprintf(&b, "   %q\n", t.Detail)
		}
	}
	for _, note := range r.Limitations {
		fmt.Fprintf(&b, "Limitation: %q\n", note)
	}
	switch {
	case r.Complete:
		fmt.Fprintf(&b, "Next: nemalo library ingest LIBRARY %q --apply\n", r.Output)
	case r.Status == "stopped_on_detection":
		b.WriteString("A scanner flagged a track, so the recording stopped. Review that track before resuming. A detection can be a false positive.\n")
	case r.Output != "":
		fmt.Fprintf(&b, "Retained for review. Continue with: nemalo acquire %s --recording %q --output %q --resume\n", r.SourceID, r.Format, r.Output)
	}
	return b.String()
}
