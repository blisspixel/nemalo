package present

import (
	"fmt"
	"strings"

	"github.com/blisspixel/nemalo/internal/acquisition"
)

func Acquisition(r acquisition.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Acquisition: %q\nComplete: %t | Transfer complete: %t\nOutput: %q\nContent: %q\nTransferred: %d bytes\nSHA-256: %q\nAntivirus: %q\n", r.Status, r.Complete, r.TransferComplete, r.Output, r.ContentPath, r.Transfer.Bytes, r.Transfer.SHA256, r.Antivirus)
	if r.Intent != nil {
		e := r.Intent.Selection
		fmt.Fprintf(&b, "Source: %q\nSelected file: %q\nTitles: %q\nDeclared rights: %q\nDeclared licenses: %q\nMetadata SHA-256: %q\n", r.Intent.Request.SourceID, r.Intent.Request.File, e.Title, e.Rights, e.LicenseURLs, e.MetadataSHA256)
	}
	if r.Checks != nil {
		fmt.Fprintf(&b, "Signature: %q | EPUB package: %q\nWarnings: %q\n", r.Checks.Signature, r.Checks.EPUBPackage, r.Checks.Warnings)
	}
	b.WriteString("Untrusted intake only. File rights, DRM, malware safety, and semantic completeness are not established.\nIncomplete folders are retained; no automatic resume or deletion.\n")
	return b.String()
}
