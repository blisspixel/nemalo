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
	b.WriteString("Untrusted intake only. File rights, DRM, malware safety, and semantic completeness are not established.\nIncomplete folders are retained; no automatic resume or deletion.\n")
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
