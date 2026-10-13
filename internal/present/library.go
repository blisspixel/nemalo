package present

import (
	"fmt"
	"sort"
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
	if r.IdentitiesRecorded {
		fmt.Fprintf(&b, "Package identifiers recorded: %d\n", len(r.Identities))
	}
	for _, item := range r.Identities {
		fmt.Fprintf(&b, "Identity: %q %q\n", item.Role, item.ID)
	}
	if r.IdentifiersOmitted > 0 {
		fmt.Fprintf(&b, "Identifiers omitted: %d\n", r.IdentifiersOmitted)
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

func Rescan(r library.RescanResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Library: %q\nLibrary ID: %q\nApplied: %t\nScanned: %d | Promoted to checked: %d | Detections: %d | Incomplete: %d\n", r.Library, r.LibraryID, r.Applied, len(r.Scanned), r.Promoted, r.Detections, r.Incomplete)
	for _, item := range r.Scanned {
		after := item.After + " " + item.AfterReason
		if item.After == "" {
			after = "no status recorded"
		}
		fmt.Fprintf(&b, "\n%q\n  %s %s -> %s | antivirus %s\n", item.Path, item.Before, item.BeforeReason, after, item.Antivirus)
		if item.Detail != "" {
			fmt.Fprintf(&b, "  %q\n", item.Detail)
		}
	}
	if len(r.Skipped) > 0 {
		fmt.Fprintf(&b, "\nNot rescanned: %d\n", len(r.Skipped))
		for _, item := range r.Skipped {
			fmt.Fprintf(&b, "  %q %s: %q\n", item.Path, item.BeforeReason, item.Detail)
		}
	}
	for _, note := range r.Limitations {
		fmt.Fprintf(&b, "Limitation: %q\n", note)
	}
	if !r.Applied {
		changed := 0
		for _, item := range r.Scanned {
			if item.Changed {
				changed++
			}
		}
		fmt.Fprintf(&b, "Preview only. %d status change(s) would be recorded with --apply.\n", changed)
	}
	return b.String()
}

// ManagedHoldings lists recorded holdings. Status and titles are journal facts;
// library audit rechecks the stored bytes.
func ManagedHoldings(p library.ManagedPage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Library: %q\nMatching holdings: %d; offset: %d\n", p.LibraryID, p.Total, p.Offset)
	for _, h := range p.Holdings {
		title := h.Path
		if len(h.Titles) > 0 {
			title = h.Titles[0]
		}
		fmt.Fprintf(&b, "\n%q\n  %s | %s %s | %d bytes | languages %q\n  ID: %q\n", title, h.Format, h.Status, h.Reason, h.Bytes, h.Languages, h.ID)
		for _, id := range h.Identities {
			fmt.Fprintf(&b, "  Identity: %s %q\n", id.Role, id.ID)
		}
		for _, src := range h.Sources {
			if src.SourceID != "" {
				fmt.Fprintf(&b, "  Source: %q %q\n", src.SourceID, src.SourceFile)
				if src.Recording != nil {
					fmt.Fprintf(&b, "  Track %d of %d in %q (declared order)\n", src.Recording.Position, src.Recording.Tracks, src.Recording.Format)
				}
			} else {
				fmt.Fprintf(&b, "  Source: %q\n", src.Path)
			}
		}
	}
	b.WriteString("Recorded journal facts; run library audit to recheck stored bytes.\n")
	return b.String()
}

func Export(r library.ExportResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Holding: %q\nTitle: %q\nCopy: %q\nVerified SHA-256: %q (%d bytes)\nRead-only: %t | Reused: %t\n", r.AssetID, r.Title, r.Path, r.SHA256, r.Bytes, r.ReadOnly, r.Reused)
	if r.Launched != "" {
		fmt.Fprintf(&b, "Opened with: %q\n", r.Launched)
	}
	for _, note := range r.Limitations {
		fmt.Fprintf(&b, "Limitation: %q\n", note)
	}
	return b.String()
}

func Ingest(r library.IngestResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Library: %q\nFolder: %q\nApplied: %t | Complete: %t\nImported: %d (checked %d, review %d) | Not imported: %d | Failed: %d\n", r.Library, r.Folder, r.Applied, r.Complete, r.Imported, r.Checked, r.Review, r.NotImported, r.Failed)
	for _, it := range r.Items {
		line := fmt.Sprintf("\n%s %q [%s]", it.Outcome, it.Source, it.Kind)
		if it.Import != nil && it.Import.Status != "" {
			line += fmt.Sprintf(" %s %s", it.Import.Status, it.Import.Reason)
			if it.Import.AlreadyHeld {
				line += " (already held)"
			}
		}
		b.WriteString(line + "\n")
		if it.Detail != "" {
			fmt.Fprintf(&b, "  %q\n", it.Detail)
		}
	}
	for _, note := range r.Limitations {
		fmt.Fprintf(&b, "Limitation: %q\n", note)
	}
	if !r.Applied {
		b.WriteString("Preview only. No library files were written.\n")
	}
	return b.String()
}

// Recordings shows each held recording and its tracks in declared order.
func Recordings(r library.RecordingsResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Library: %q\nRecordings: %d\n", r.LibraryID, len(r.Recordings))
	for _, m := range r.Recordings {
		fmt.Fprintf(&b, "\n%q %q: %d of %d tracks held | complete: %t", m.SourceID, m.Format, m.HeldTracks, m.DeclaredTracks, m.Complete)
		if m.ProbedMs != nil {
			fmt.Fprintf(&b, " | probed %s", clock(*m.ProbedMs))
		}
		b.WriteString("\n")
		if len(m.Missing) > 0 {
			fmt.Fprintf(&b, "  Missing positions: %v\n", m.Missing)
		}
		for _, f := range m.Findings {
			fmt.Fprintf(&b, "  Finding: %q\n", f)
		}
		for _, t := range m.Tracks {
			fmt.Fprintf(&b, "  %d. %q %s", t.Position, firstNonEmpty(t.Title, t.SourceFile), t.Status)
			switch {
			case t.Probe == "indexed" && t.StartMs != nil:
				fmt.Fprintf(&b, " | starts %s, %s", clock(*t.StartMs), clock(t.EncodedDurationMs))
			case t.Probe == "indexed":
				fmt.Fprintf(&b, " | %s", clock(t.EncodedDurationMs))
			case t.Probe != "not_requested":
				fmt.Fprintf(&b, " | probe %s", t.Probe)
			}
			if len(t.Chapters) > 0 {
				fmt.Fprintf(&b, " | %d chapter(s)", len(t.Chapters))
			}
			fmt.Fprintf(&b, "\n     %s\n", t.AssetID)
		}
	}
	for _, note := range r.Limitations {
		fmt.Fprintf(&b, "Limitation: %q\n", note)
	}
	return b.String()
}

// clock formats milliseconds as H:MM:SS.mmm.
func clock(ms int64) string {
	return fmt.Sprintf("%d:%02d:%02d.%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// Cleanup shows every folder entry with its disposition, and the plan digest
// that --apply --plan must repeat.
func Cleanup(r library.CleanupResult) string {
	// A failure before a plan exists has no digest. Printing a preview would
	// claim that nothing would move, beside the error that stopped planning.
	if r.Digest == "" && !r.Applied {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Library: %q\nFolder: %q\nMove: %d (%s) | Keep: %d\nPlan digest: %s\n", r.Library, r.Folder, r.Move, byteCount(r.MoveBytes), r.Keep, r.Digest)
	if r.Applied {
		fmt.Fprintf(&b, "Applied: moved %d, not moved %d | Cleanup ID: %s\nHolding area: %q\n", r.Moved, r.NotMoved, r.CleanupID, r.HoldingArea)
		if r.Resumed {
			b.WriteString("Resumed an interrupted cleanup.\n")
		}
	}
	for _, e := range r.Entries {
		line := fmt.Sprintf("\n%s %q [%s]", e.Disposition, e.Path, e.Kind)
		if e.Outcome != "" {
			line += " -> " + e.Outcome
		}
		fmt.Fprintf(&b, "%s\n  %q\n", line, e.Reason)
	}
	for _, note := range r.Limitations {
		fmt.Fprintf(&b, "Limitation: %q\n", note)
	}
	switch {
	case !r.Applied && r.Move > 0:
		fmt.Fprintf(&b, "Preview only. Nothing was moved. Apply with: --apply --plan %s\n", r.Digest)
	case !r.Applied:
		b.WriteString("Preview only. Nothing here is held by the library, so nothing would move.\n")
	case r.Moved > 0:
		fmt.Fprintf(&b, "Undo with: nemalo library restore %q --cleanup %s --apply\n", r.Library, r.CleanupID)
	}
	return b.String()
}

// Restore shows each held file and what restoring it does or did.
func Restore(r library.RestoreResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Library: %q\nCleanup: %s\nFolder: %q\nRestore: %d | Already present: %d | Conflicts: %d | Not held: %d | Held copy changed: %d\n", r.Library, r.CleanupID, r.Folder, r.Restore, r.Present, r.Conflicts, r.NotHeld, r.Changed)
	for _, f := range r.Files {
		line := fmt.Sprintf("%s %q", f.Disposition, f.Path)
		if f.Outcome != "" {
			line += " -> " + f.Outcome
		}
		b.WriteString(line + "\n")
	}
	for _, note := range r.Limitations {
		fmt.Fprintf(&b, "Limitation: %q\n", note)
	}
	if !r.Applied {
		b.WriteString("Preview only. Nothing was written. Apply with --apply.\n")
	}
	return b.String()
}

func byteCount(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}

// LibraryCheck summarizes a read-only library check.
func LibraryCheck(r library.LibraryCheck) string {
	var b strings.Builder
	state := "healthy"
	if !r.Healthy {
		state = "needs attention"
	}
	fmt.Fprintf(&b, "Library: %q\nCheck: %s\nHoldings: %d | verified: %d | checked: %d | unverified: %d\n", r.LibraryID, state, r.Holdings, r.Verified, r.Checked, r.Unverified)
	reasons := make([]string, 0, len(r.Review))
	for reason := range r.Review {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	for _, reason := range reasons {
		fmt.Fprintf(&b, "Review: %d %q\n", r.Review[reason], reason)
	}
	if r.Cleanups > 0 {
		fmt.Fprintf(&b, "Cleanups: %d | held files: %d (%s)\n", r.Cleanups, r.HeldFiles, byteCount(r.HeldBytes))
	}
	for _, i := range r.Issues {
		if i.Path != "" {
			fmt.Fprintf(&b, "Issue %s %q: %q\n", i.Kind, i.Path, i.Detail)
		} else {
			fmt.Fprintf(&b, "Issue %s: %q\n", i.Kind, i.Detail)
		}
	}
	for _, n := range r.Next {
		fmt.Fprintf(&b, "Next: %s\n", n)
	}
	for _, note := range r.Limitations {
		fmt.Fprintf(&b, "Limitation: %q\n", note)
	}
	b.WriteString("Nothing was changed.\n")
	return b.String()
}
