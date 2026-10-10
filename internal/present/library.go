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
	if p.Format != "" && p.Format != "all" {
		fmt.Fprintf(&b, "Filename format filter: %q; not a validity verdict.\n", p.Format)
	}
	for _, a := range p.Assets {
		title := a.ID
		if len(a.Locations) > 0 {
			title = a.Locations[0].Path
		}
		if a.Health != nil && a.Health.Checks.EPUB != nil && len(a.Health.Checks.EPUB.Titles) > 0 {
			title = a.Health.Checks.EPUB.Titles[0]
		}
		fmt.Fprintf(&b, "\n%q\n  %d bytes | ID: %q\n", title, a.Bytes, a.ID)
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

func Import(r library.ImportResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Library: %q\nSource: %q\nKind: %q\nApplied: %t | Resumed: %t | Already held: %t\n", r.Library, r.Source, r.SourceKind, r.Applied, r.Resumed, r.AlreadyHeld)
	fmt.Fprintf(&b, "Asset: %q\nFormat: %q | Bytes: %d\nSHA-256: %q\nStatus: %q\nReason: %q\nPath: %q\n", r.AssetID, r.Format, r.Bytes, r.SHA256, r.Status, r.Reason, r.Path)
	for _, title := range r.Titles {
		fmt.Fprintf(&b, "Title: %q\n", title)
	}
	for _, language := range r.Languages {
		fmt.Fprintf(&b, "Language: %q\n", language)
	}
	if r.Antivirus != "" {
		fmt.Fprintf(&b, "Antivirus: %q\n", r.Antivirus)
	}
	for _, finding := range r.Findings {
		fmt.Fprintf(&b, "Finding: %q\n", finding)
	}
	for _, note := range r.Limitations {
		fmt.Fprintf(&b, "Limitation: %q\n", note)
	}
	fmt.Fprintf(&b, "Durability: %q\n", r.Durability)
	if !r.Applied {
		b.WriteString("Preview only. No library files were written.\n")
	}
	b.WriteString("The source was preserved. Review is not a safety guarantee.\n")
	return b.String()
}

func ManagedAudit(a library.ManagedAudit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Library: %q\nRoot: %q\nAssets: %d\nChecked matches: %d\nAntivirus: %q\n", a.LibraryID, a.Root, a.Assets, a.Matching, a.Security)
	for _, finding := range a.Findings {
		fmt.Fprintf(&b, "Review: %q %q %q\n", finding.Path, finding.Status, finding.Detail)
	}
	b.WriteString("Stored files were not changed. Review is not a safety guarantee.\n")
	return b.String()
}

func LibraryState(s library.State) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Library root: %q\nControl state: %q\nLibrary ID: %q\nJournal records: %d\nCreated: %t | Resumed: %t\n", s.Root, s.Status, s.LibraryID, s.JournalRecords, s.Created, s.Resumed)
	if s.InitializedAt != nil {
		fmt.Fprintf(&b, "Initialized at: %s\n", s.InitializedAt.UTC().Format("2006-01-02T15:04:05Z"))
	}
	fmt.Fprintf(&b, "Durability: %q\n", s.Durability)
	b.WriteString("Control metadata only. Content was not imported, checked, opened, or deleted.\n")
	return b.String()
}
