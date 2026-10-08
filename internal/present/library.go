package present

import (
	"fmt"
	"strings"

	"github.com/blisspixel/nemalo/internal/library"
)

func Snapshot(s library.Snapshot) string {
	locations := 0
	for _, a := range s.Catalog.Assets {
		locations += len(a.Locations)
	}
	return fmt.Sprintf("Snapshot: %q\nRoot hint: %q\nComplete: %t\nUnique byte assets: %d\nFile locations: %d\nExcluded links/special files: %d\nSource bytes read: %d\nSaved: %q\nDurability: %q\nNo publication, source cleanup, or antivirus scan performed.\n", s.Catalog.ID, s.Catalog.RootHint, s.Catalog.Complete, len(s.Catalog.Assets), locations, len(s.Catalog.Excluded), s.BytesRead, s.Output, s.Durability)
}

func Holdings(p library.Page) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Snapshot: %q\nMatching byte assets: %d; offset: %d\n", p.SnapshotID, p.Total, p.Offset)
	for _, a := range p.Assets {
		fmt.Fprintf(&b, "\n%q (%d bytes)\n", a.ID, a.Bytes)
		for _, l := range a.Locations {
			fmt.Fprintf(&b, "  %q [%q]\n", l.Path, l.CandidateKind)
		}
		if h := a.Health; h != nil {
			fmt.Fprintf(&b, "  Recorded health: %q; format: %q; antivirus: %q\n", h.Status, h.DetectedFormat, h.Antivirus.Status)
			if h.Checks.EPUB != nil {
				for _, title := range h.Checks.EPUB.Titles {
					fmt.Fprintf(&b, "  Title: %q\n", title)
				}
				for _, lang := range h.Checks.EPUB.Languages {
					fmt.Fprintf(&b, "  Language: %q\n", lang)
				}
			}
		}
	}
	b.WriteString("Catalog evidence is historical; audit current bytes before relying on identity.\n")
	return b.String()
}

func Audit(a library.Audit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Snapshot: %q\nRoot: %q\nInventory complete: %t\nUnchanged file locations: %d\nSource bytes read: %d\nAntivirus: not_scanned\n", a.SnapshotID, a.Root, a.Complete, a.Unchanged, a.BytesRead)
	for _, f := range a.Findings {
		fmt.Fprintf(&b, "Review: %q %q %q\n", f.Path, f.Status, f.Detail)
	}
	b.WriteString("Sources unchanged. Byte identity does not establish content safety or completeness.\n")
	return b.String()
}
