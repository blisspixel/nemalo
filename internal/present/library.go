package present

import (
	"fmt"
	"sort"
	"strings"

	"github.com/blisspixel/nemalo/internal/library"
)

func Snapshot(s library.Snapshot) string {
	var b strings.Builder
	locations := 0
	for _, a := range s.Catalog.Assets {
		locations += len(a.Locations)
	}
	field(&b, "Snapshot", Line(s.Catalog.ID))
	field(&b, "Root hint", Line(s.Catalog.RootHint))
	fmt.Fprintf(&b, "Inventory: %s\n", map[bool]string{true: "complete", false: "incomplete"}[s.Catalog.Complete])
	fmt.Fprintf(&b, "Unique byte assets: %d | File locations: %d | Excluded links and special files: %d\nSource bytes read: %d\n", len(s.Catalog.Assets), locations, len(s.Catalog.Excluded), s.BytesRead)
	field(&b, "Saved", Exact(s.Output))
	field(&b, "Durability", words(s.Durability))
	b.WriteString("No publication, source cleanup, or antivirus scan performed.\n")
	return b.String()
}

func Holdings(p library.Page) string {
	var b strings.Builder
	field(&b, "Snapshot", Line(p.SnapshotID))
	fmt.Fprintf(&b, "Matching byte assets: %d, from offset %d\n", p.Total, p.Offset)
	if p.Format != "" && p.Format != "all" {
		fmt.Fprintf(&b, "Filename format filter: %s; not a validity verdict.\n", Line(p.Format))
	}
	for _, a := range p.Assets {
		title := a.ID
		if len(a.Locations) > 0 {
			title = a.Locations[0].Path
		}
		if a.Health != nil && a.Health.Checks.EPUB != nil && len(a.Health.Checks.EPUB.Titles) > 0 {
			title = a.Health.Checks.EPUB.Titles[0]
		}
		fmt.Fprintf(&b, "\n%s\n  %d bytes | ID: %s\n", Line(title), a.Bytes, Line(a.ID))
		for _, l := range a.Locations {
			fmt.Fprintf(&b, "  %s (%s)\n", Exact(l.Path), words(l.CandidateKind))
		}
		if h := a.Health; h != nil {
			fmt.Fprintf(&b, "  Recorded health: %s | format: %s | antivirus: %s\n", words(h.Status), words(h.DetectedFormat), words(h.Antivirus.Status))
			if h.Checks.EPUB != nil {
				if v := list(h.Checks.EPUB.Titles); v != "" {
					fmt.Fprintf(&b, "  Title: %s\n", v)
				}
				if v := list(h.Checks.EPUB.Languages); v != "" {
					fmt.Fprintf(&b, "  Language: %s\n", v)
				}
			}
		}
	}
	b.WriteString("Catalog evidence is historical; audit current bytes before relying on identity.\n")
	return b.String()
}

func Audit(a library.Audit) string {
	var b strings.Builder
	field(&b, "Snapshot", Line(a.SnapshotID))
	field(&b, "Root", Exact(a.Root))
	fmt.Fprintf(&b, "Inventory: %s\nUnchanged file locations: %d\nSource bytes read: %d\nAntivirus: not scanned\n", map[bool]string{true: "complete", false: "incomplete"}[a.Complete], a.Unchanged, a.BytesRead)
	for _, f := range a.Findings {
		writeReview(&b, f.Path, f.Status, f.Detail)
	}
	b.WriteString("Sources unchanged. Byte identity does not establish content safety or completeness.\n")
	return b.String()
}

// writeReview is one finding about one path: the path, its status as words,
// and the detail.
func writeReview(b *strings.Builder, path, status, detail string) {
	line := "Review: " + Line(path) + ": " + words(status)
	if d := words(detail); d != "" && detail != status {
		line += ". " + d
	}
	b.WriteString(line + "\n")
}

func Import(r library.ImportResult) string {
	var b strings.Builder
	state := "Preview"
	switch {
	case r.Applied && r.Resumed:
		state = "Imported, resuming an interrupted import"
	case r.Applied:
		state = "Imported"
	}
	if r.AlreadyHeld {
		state += "; the library already holds these bytes"
	}
	b.WriteString(state + "\n")
	field(&b, "Library", Exact(r.Library))
	field(&b, "Source", Exact(r.Source))
	field(&b, "Kind", words(r.SourceKind))
	field(&b, "Asset", Line(r.AssetID))
	if r.Format != "" {
		fmt.Fprintf(&b, "Format: %s | Bytes: %d\n", words(r.Format), r.Bytes)
	}
	field(&b, "SHA-256", Line(r.SHA256))
	field(&b, "Status", statusWithReason(r.Status, r.Reason))
	field(&b, "Path", Exact(r.Path))
	field(&b, "Title", list(r.Titles))
	field(&b, "Language", list(r.Languages))
	if r.IdentitiesRecorded {
		fmt.Fprintf(&b, "Package identifiers recorded: %d\n", len(r.Identities))
	}
	for _, item := range r.Identities {
		fmt.Fprintf(&b, "Identifier: %s (%s)\n", Line(item.ID), words(item.Role))
	}
	if r.IdentifiersOmitted > 0 {
		fmt.Fprintf(&b, "Identifiers omitted: %d\n", r.IdentifiersOmitted)
	}
	field(&b, "Antivirus", words(r.Antivirus))
	notes(&b, "Finding", r.Findings)
	notes(&b, "Limitation", r.Limitations)
	field(&b, "Durability", words(r.Durability))
	if !r.Applied {
		b.WriteString("Preview only. No library files were written.\n")
	}
	b.WriteString("The source was preserved. Review is not a safety guarantee.\n")
	return b.String()
}

func ManagedAudit(a library.ManagedAudit) string {
	var b strings.Builder
	field(&b, "Library", Line(a.LibraryID))
	field(&b, "Root", Exact(a.Root))
	fmt.Fprintf(&b, "Assets: %d | Checked matches: %d\n", a.Assets, a.Matching)
	field(&b, "Antivirus", words(a.Security))
	for _, finding := range a.Findings {
		writeReview(&b, finding.Path, finding.Status, finding.Detail)
	}
	b.WriteString("Stored files were not changed. Review is not a safety guarantee.\n")
	return b.String()
}

func LibraryState(s library.State) string {
	var b strings.Builder
	field(&b, "Library root", Exact(s.Root))
	state := words(s.Status)
	switch {
	case s.Created:
		state += ", created now"
	case s.Resumed:
		state += ", resumed an interrupted initialization"
	}
	field(&b, "Control state", state)
	field(&b, "Library ID", Line(s.LibraryID))
	fmt.Fprintf(&b, "Journal records: %d\n", s.JournalRecords)
	if s.InitializedAt != nil {
		fmt.Fprintf(&b, "Initialized at: %s\n", s.InitializedAt.UTC().Format("2006-01-02T15:04:05Z"))
	}
	field(&b, "Durability", words(s.Durability))
	b.WriteString("Control metadata only. Content was not imported, checked, opened, or deleted.\n")
	return b.String()
}

func Rescan(r library.RescanResult) string {
	var b strings.Builder
	field(&b, "Library", Exact(r.Library))
	field(&b, "Library ID", Line(r.LibraryID))
	fmt.Fprintf(&b, "Scanned: %d | Promoted to checked: %d | Detections: %d | Incomplete: %d\n", len(r.Scanned), r.Promoted, r.Detections, r.Incomplete)
	for _, item := range r.Scanned {
		after := statusWithReason(item.After, item.AfterReason)
		if item.After == "" {
			after = "no status recorded"
		}
		fmt.Fprintf(&b, "\n%s\n  %s -> %s", Exact(item.Path), statusWithReason(item.Before, item.BeforeReason), after)
		if item.Antivirus != "" {
			fmt.Fprintf(&b, " | antivirus %s", words(item.Antivirus))
		}
		b.WriteString("\n")
		if item.Detail != "" {
			fmt.Fprintf(&b, "  %s\n", words(item.Detail))
		}
	}
	if len(r.Skipped) > 0 {
		fmt.Fprintf(&b, "\nNot rescanned: %d\n", len(r.Skipped))
		for _, item := range r.Skipped {
			fmt.Fprintf(&b, "  %s (%s): %s\n", Exact(item.Path), words(item.BeforeReason), words(item.Detail))
		}
	}
	notes(&b, "Limitation", r.Limitations)
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
	field(&b, "Library", Line(p.LibraryID))
	fmt.Fprintf(&b, "Matching holdings: %d, from offset %d\n", p.Total, p.Offset)
	for _, h := range p.Holdings {
		title := h.Path
		if len(h.Titles) > 0 {
			title = h.Titles[0]
		}
		fmt.Fprintf(&b, "\n%s\n  %s | %s | %d bytes", Line(title), words(h.Format), statusWithReason(h.Status, h.Reason), h.Bytes)
		if languages := list(h.Languages); languages != "" {
			fmt.Fprintf(&b, " | %s", languages)
		}
		fmt.Fprintf(&b, "\n  ID: %s\n", Line(h.ID))
		for _, id := range h.Identities {
			fmt.Fprintf(&b, "  Identifier: %s (%s)\n", Line(id.ID), words(id.Role))
		}
		for _, src := range h.Sources {
			if src.SourceID != "" {
				source := Line(src.SourceID)
				if src.SourceFile != "" {
					source += ", file " + Line(src.SourceFile)
				}
				fmt.Fprintf(&b, "  Source: %s\n", source)
				if src.Recording != nil {
					fmt.Fprintf(&b, "  Track %d of %d in %s (declared order)\n", src.Recording.Position, src.Recording.Tracks, Line(src.Recording.Format))
				}
			} else {
				fmt.Fprintf(&b, "  Source: %s\n", Exact(src.Path))
			}
		}
	}
	b.WriteString("Recorded journal facts; run library audit to recheck stored bytes.\n")
	return b.String()
}

func Export(r library.ExportResult) string {
	var b strings.Builder
	field(&b, "Holding", Line(r.AssetID))
	field(&b, "Title", Line(r.Title))
	field(&b, "Copy", Exact(r.Path))
	fmt.Fprintf(&b, "Verified SHA-256: %s (%d bytes)\n", Line(r.SHA256), r.Bytes)
	field(&b, "Copy state", joinSet(yes(r.ReadOnly, "read-only"), yes(r.Reused, "reused an identical earlier copy")))
	field(&b, "Opened with", Line(r.Launched))
	notes(&b, "Limitation", r.Limitations)
	return b.String()
}

func Ingest(r library.IngestResult) string {
	var b strings.Builder
	field(&b, "Library", Exact(r.Library))
	field(&b, "Folder", Exact(r.Folder))
	state := "complete"
	if !r.Complete {
		state = "incomplete"
	}
	if !r.Applied {
		state = "preview, " + state
	}
	fmt.Fprintf(&b, "Ingest: %s\nImported: %d (checked %d, review %d) | Not imported: %d | Failed: %d\n", state, r.Imported, r.Checked, r.Review, r.NotImported, r.Failed)
	for _, it := range r.Items {
		line := fmt.Sprintf("\n%s %s (%s)", words(it.Outcome), Exact(it.Source), words(it.Kind))
		if it.Import != nil && it.Import.Status != "" {
			line += ": " + statusWithReason(it.Import.Status, it.Import.Reason)
			if it.Import.AlreadyHeld {
				line += ", already held"
			}
		}
		b.WriteString(line + "\n")
		if it.Detail != "" {
			fmt.Fprintf(&b, "  %s\n", words(it.Detail))
		}
	}
	notes(&b, "Limitation", r.Limitations)
	if !r.Applied {
		b.WriteString("Preview only. No library files were written.\n")
	}
	return b.String()
}

// Recordings shows each held recording and its tracks in declared order.
func Recordings(r library.RecordingsResult) string {
	var b strings.Builder
	field(&b, "Library", Line(r.LibraryID))
	fmt.Fprintf(&b, "Recordings: %d\n", len(r.Recordings))
	for _, m := range r.Recordings {
		complete := "incomplete"
		if m.Complete {
			complete = "complete"
		}
		fmt.Fprintf(&b, "\n%s %s: %d of %d tracks held, %s", Line(m.SourceID), Line(m.Format), m.HeldTracks, m.DeclaredTracks, complete)
		if m.ProbedMs != nil {
			fmt.Fprintf(&b, " | probed %s", clock(*m.ProbedMs))
		}
		b.WriteString("\n")
		if len(m.Missing) > 0 {
			positions := make([]string, len(m.Missing))
			for i, p := range m.Missing {
				positions[i] = fmt.Sprint(p)
			}
			fmt.Fprintf(&b, "  Missing positions: %s\n", strings.Join(positions, ", "))
		}
		for _, f := range m.Findings {
			fmt.Fprintf(&b, "  Finding: %s\n", words(f))
		}
		for _, t := range m.Tracks {
			fmt.Fprintf(&b, "  %d. %s %s", t.Position, Line(firstNonEmpty(t.Title, t.SourceFile)), words(t.Status))
			switch {
			case t.Probe == "indexed" && t.StartMs != nil:
				fmt.Fprintf(&b, " | starts %s, %s", clock(*t.StartMs), clock(t.EncodedDurationMs))
			case t.Probe == "indexed":
				fmt.Fprintf(&b, " | %s", clock(t.EncodedDurationMs))
			case t.Probe != "not_requested":
				fmt.Fprintf(&b, " | probe %s", words(t.Probe))
			}
			if len(t.Chapters) > 0 {
				fmt.Fprintf(&b, " | %d chapter(s)", len(t.Chapters))
			}
			fmt.Fprintf(&b, "\n     %s\n", Line(t.AssetID))
		}
	}
	notes(&b, "Limitation", r.Limitations)
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
	field(&b, "Library", Exact(r.Library))
	field(&b, "Folder", Exact(r.Folder))
	fmt.Fprintf(&b, "Move: %d (%s) | Keep: %d\nPlan digest: %s\n", r.Move, byteCount(r.MoveBytes), r.Keep, Line(r.Digest))
	if r.Applied {
		fmt.Fprintf(&b, "Applied: moved %d, not moved %d | Cleanup ID: %s\n", r.Moved, r.NotMoved, Line(r.CleanupID))
		field(&b, "Holding area", Exact(r.HoldingArea))
		if r.Resumed {
			b.WriteString("Resumed an interrupted cleanup.\n")
		}
	}
	for _, e := range r.Entries {
		line := fmt.Sprintf("\n%s %s (%s)", words(e.Disposition), Exact(e.Path), words(e.Kind))
		if e.Outcome != "" {
			line += " -> " + words(e.Outcome)
		}
		b.WriteString(line + "\n")
		if reason := words(e.Reason); reason != "" {
			fmt.Fprintf(&b, "  %s\n", reason)
		}
	}
	notes(&b, "Limitation", r.Limitations)
	switch {
	case !r.Applied && r.Move > 0:
		fmt.Fprintf(&b, "Preview only. Nothing was moved. Apply with: --apply --plan %s\n", Line(r.Digest))
	case !r.Applied:
		b.WriteString("Preview only. Nothing here is held by the library, so nothing would move.\n")
	case r.Moved > 0:
		if c := command("nemalo", "library", "restore", r.Library, "--cleanup", r.CleanupID, "--apply"); c != "" {
			fmt.Fprintf(&b, "Undo with: %s\n", c)
		}
	}
	return b.String()
}

// Restore shows each held file and what restoring it does or did.
func Restore(r library.RestoreResult) string {
	var b strings.Builder
	field(&b, "Library", Exact(r.Library))
	field(&b, "Cleanup", Line(r.CleanupID))
	field(&b, "Folder", Exact(r.Folder))
	fmt.Fprintf(&b, "Restore: %d | Already present: %d | Conflicts: %d | Not held: %d | Held copy changed: %d\n", r.Restore, r.Present, r.Conflicts, r.NotHeld, r.Changed)
	for _, f := range r.Files {
		line := words(f.Disposition) + " " + Exact(f.Path)
		if f.Outcome != "" {
			line += " -> " + words(f.Outcome)
		}
		b.WriteString(line + "\n")
	}
	notes(&b, "Limitation", r.Limitations)
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
	field(&b, "Library", Line(r.LibraryID))
	fmt.Fprintf(&b, "Check: %s\nHoldings: %d | verified: %d | checked: %d | unverified: %d\n", state, r.Holdings, r.Verified, r.Checked, r.Unverified)
	reasons := make([]string, 0, len(r.Review))
	for reason := range r.Review {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	for _, reason := range reasons {
		fmt.Fprintf(&b, "Review: %d %s\n", r.Review[reason], words(reason))
	}
	if r.Cleanups > 0 {
		fmt.Fprintf(&b, "Cleanups: %d | held files: %d (%s)\n", r.Cleanups, r.HeldFiles, byteCount(r.HeldBytes))
	}
	for _, i := range r.Issues {
		if i.Path != "" {
			fmt.Fprintf(&b, "Issue, %s: %s: %s\n", words(i.Kind), Line(i.Path), words(i.Detail))
		} else {
			fmt.Fprintf(&b, "Issue, %s: %s\n", words(i.Kind), words(i.Detail))
		}
	}
	for _, n := range r.Next {
		fmt.Fprintf(&b, "Next: %s\n", Line(n))
	}
	notes(&b, "Limitation", r.Limitations)
	b.WriteString("Nothing was changed.\n")
	return b.String()
}
