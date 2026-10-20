package present

import (
	"fmt"
	"sort"
	"strings"

	"github.com/blisspixel/nemalo/internal/app"
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
	case r.ApplyRequested:
		state = "Import not completed"
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
	if r.SourceEvidence != nil {
		b.WriteString("Incoming source evidence (declarations):\n")
		importSource(&b, *r.SourceEvidence)
	}
	if r.Holding != nil {
		fmt.Fprintf(&b, "Proposed holding sources: %d\n", len(r.Holding.Sources))
		for i, source := range r.Holding.Sources {
			fmt.Fprintf(&b, "Source record %d:\n", i+1)
			importSource(&b, source)
		}
	}
	field(&b, "Antivirus", words(r.Antivirus))
	notes(&b, "Finding", r.Findings)
	notes(&b, "Limitation", r.Limitations)
	field(&b, "Durability", words(r.Durability))
	if r.RecoveredRescan {
		b.WriteString("Completed an earlier pending rescan before this import.\n")
	}
	if !r.Applied {
		field(&b, "Plan", Line(r.Plan))
		if r.ApplyRequested || r.RecoveredRescan {
			b.WriteString("This import has not completed. Retained state and errors must be reviewed before retrying.\n")
		} else {
			b.WriteString("Preview only. No library files were written.\n")
		}
	}
	b.WriteString("The source was preserved. Review is not a safety guarantee.\n")
	return b.String()
}

// importSource shows every normalized source field, including declarations
// retained from an earlier import rather than replaced by incoming evidence.
func importSource(b *strings.Builder, source library.Source) {
	field(b, "  Kind", words(source.Kind))
	field(b, "  Path", Exact(source.Path))
	field(b, "  Source ID", Line(source.SourceID))
	field(b, "  Source file", Exact(source.SourceFile))
	field(b, "  Declared URL", Line(source.DeclaredURL))
	notes(b, "  Declared rights", source.DeclaredRights)
	notes(b, "  Declared license", source.DeclaredLicenses)
	if r := source.Recording; r != nil {
		fmt.Fprintf(b, "  Recording: %s | Track: %d of %d | Declared seconds: %g\n", Line(r.Format), r.Position, r.Tracks, r.DeclaredSeconds)
		field(b, "  Declared track title", Line(r.Title))
		for _, reader := range r.Readers {
			fmt.Fprintf(b, "  Declared reader: %s (LibriVox %s)\n", Line(reader.Name), Line(reader.ID))
		}
	}
	if n := source.Narration; n != nil {
		fmt.Fprintf(b, "  Narration: %s | Evidence: %s\n", Line(n.ID), words(n.Evidence))
		field(b, "  Declared narration title", Line(n.Title))
	}
	for _, id := range source.Identities {
		fmt.Fprintf(b, "  Declared identifier: %s (%s) | Raw: %s | Scheme: %s | Origin: %s | Confidence: %s\n", Line(id.ID), words(id.Role), Line(id.Raw), Line(id.Scheme), words(id.Origin), words(id.Confidence))
	}
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
	if p.Language != "" {
		fmt.Fprintf(&b, "Language: %s", Line(p.Language))
		if p.LanguageUnknown > 0 {
			fmt.Fprintf(&b, " | %d other holding(s) declare no readable language and were not matched", p.LanguageUnknown)
		}
		b.WriteString("\n")
	}
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

// Related shows each group of holdings and the evidence placing each one.
func Related(r library.RelatedResult) string {
	var b strings.Builder
	field(&b, "Library", Line(r.LibraryID))
	field(&b, "Holding", Line(r.Holding))
	fmt.Fprintf(&b, "Related groups: %d, from offset %d\n", r.Total, r.Offset)
	for _, g := range r.Groups {
		fmt.Fprintf(&b, "\n%s: %s (%d holdings)\n", words(g.Relation), Line(g.Key), len(g.Members))
		for _, m := range g.Members {
			version := ""
			if m.Version > 0 {
				version = fmt.Sprintf(" version %d", m.Version)
			}
			line := Line(m.Holding) + " " + Line(m.Format) + version
			if m.Title != "" {
				line += " " + Line(m.Title)
			}
			fmt.Fprintf(&b, "  %s\n", line)
			for _, e := range m.Evidence {
				fmt.Fprintf(&b, "    %s\n", Line(e))
			}
		}
	}
	notes(&b, "Limitation", r.Limitations)
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
		if n := m.Narration; n != nil {
			fmt.Fprintf(&b, "  Narration: %s %s (LibriVox project names this item back)\n", Line(n.ID), Line(n.Title))
		}
		if len(m.Narrators) > 0 {
			names := make([]string, len(m.Narrators))
			for i, n := range m.Narrators {
				names[i] = Line(n.Name)
			}
			fmt.Fprintf(&b, "  Read by: %s (LibriVox)\n", strings.Join(names, ", "))
		}
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
	if len(r.Narrations) > 0 {
		fmt.Fprintf(&b, "\nNarrations: %d\n", len(r.Narrations))
		for _, g := range r.Narrations {
			fmt.Fprintf(&b, "%s %s: %d recording(s)\n", Line(g.Narration.ID), Line(g.Narration.Title), len(g.Recordings))
			for _, ref := range g.Recordings {
				complete := "incomplete"
				if ref.Complete {
					complete = "complete"
				}
				fmt.Fprintf(&b, "  %s %s, %s\n", Line(ref.SourceID), Line(ref.Format), complete)
			}
		}
	}
	for _, group := range r.UserNarrations {
		fmt.Fprintf(&b, "\nUser narration: %s (%s), %d recording(s)\n", Line(group.Narration.ID), Line(group.Narration.Evidence), len(group.Recordings))
		for _, ref := range group.Recordings {
			fmt.Fprintf(&b, "  %s %s (%s)\n", Line(ref.SourceID), Line(ref.Format), words(ref.Kind))
		}
	}
	for _, statement := range r.Declarations {
		b.WriteString("\nUser statement:\n")
		narrationStatement(&b, statement, false)
	}
	for _, g := range r.ByteIdentities {
		if len(g.Recordings) < 2 {
			continue
		}
		fmt.Fprintf(&b, "\nSame ordered track bytes: %s (%d recording declarations)\n", Line(g.ID), len(g.Recordings))
		for _, ref := range g.Recordings {
			fmt.Fprintf(&b, "  %s %s (%s)\n", Line(ref.SourceID), Line(ref.Format), Line(ref.Kind))
		}
	}
	notes(&b, "Limitation", r.Limitations)
	return b.String()
}

// Clock formats milliseconds as H:MM:SS.mmm for the terminal interface.
func Clock(ms int64) string { return clock(ms) }

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
	fmt.Fprintf(&b, "Restore: %d | Already present: %d | Conflicts: %d | Not held: %d | Purged: %d | Held copy changed: %d\n", r.Restore, r.Present, r.Conflicts, r.NotHeld, r.Purged, r.Changed)
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

// Purge lists every held file of the cleanup with what a purge does to it,
// and the plan digest --apply --plan must repeat.
func Purge(r library.PurgeResult) string {
	var b strings.Builder
	field(&b, "Library", Exact(r.Library))
	field(&b, "Cleanup", Line(r.CleanupID))
	field(&b, "Folder", Exact(r.Folder))
	if r.Applied && len(r.Files) == 0 {
		fmt.Fprintf(&b, "Purge closed. Purged before it stopped: %d. Other held copies stay.\n", r.Purged)
	} else if r.Applied {
		fmt.Fprintf(&b, "Purged: %d (%s) | Kept: %d\n", r.Purged, byteCount(r.PurgedBytes), r.Keep)
	} else {
		fmt.Fprintf(&b, "Purge: %d (%s) | Keep: %d\n", r.Purge, byteCount(r.PurgeBytes), r.Keep)
	}
	for _, f := range r.Files {
		line := words(f.Disposition) + " " + Exact(f.Path)
		if f.Outcome != "" {
			line += " -> " + words(f.Outcome)
		}
		b.WriteString(line + "\n")
	}
	notes(&b, "Limitation", r.Limitations)
	if !r.Applied && r.Digest != "" {
		fmt.Fprintf(&b, "Plan: %s\n", r.Digest)
		if r.Purge > 0 {
			if c := command("nemalo", "library", "purge", r.Library, "--cleanup", r.CleanupID, "--apply", "--plan", r.Digest); c != "" {
				b.WriteString("Preview only. Nothing was removed.\nNext: " + c + "\n")
			}
		} else {
			b.WriteString("Nothing to purge. Nothing was removed.\n")
		}
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

func collectionItem(i library.CollectionItem) string {
	if i.Recording != "" {
		return Line(i.Source) + " recording " + Line(i.Recording)
	}
	return Line(i.Source) + " " + Line(i.File)
}

// CollectionDiff shows declaration changes, keeping added selections separate
// from notes and relative-order changes that do not need acquisition.
func CollectionDiff(r library.CollectionDiffResult) string {
	if !r.Complete {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Before: %s | %s | %d items\n  %s\n", Line(r.Before.Name), Line(r.Before.Path), r.Before.Items, Line(r.Before.SHA256))
	fmt.Fprintf(&b, "After: %s | %s | %d items\n  %s\n", Line(r.After.Name), Line(r.After.Path), r.After.Items, Line(r.After.SHA256))
	if !r.Changed {
		b.WriteString("No declaration changes.\n")
	}
	if r.NameChanged {
		fmt.Fprintf(&b, "Name changed: %s -> %s\n", Line(r.Before.Name), Line(r.After.Name))
	}
	for _, p := range r.Removed {
		fmt.Fprintf(&b, "Removed (before %d): %s\n", p.Position, collectionItem(p.Item))
		if p.Item.Note != "" {
			fmt.Fprintf(&b, "  Note: %s\n", Line(p.Item.Note))
		}
	}
	for _, p := range r.Added {
		fmt.Fprintf(&b, "Added (after %d): %s\n", p.Position, collectionItem(p.Item))
		if p.Item.Note != "" {
			fmt.Fprintf(&b, "  Note: %s\n", Line(p.Item.Note))
		}
	}
	for _, n := range r.NotesChanged {
		fmt.Fprintf(&b, "Note changed: %s (before %d, after %d)\n  Before: %s\n  After: %s\n", collectionItem(n.Item), n.BeforePosition, n.AfterPosition, Line(n.BeforeNote), Line(n.Item.Note))
	}
	if len(r.RetainedOrder) > 0 {
		b.WriteString("Retained selection order changed (before -> after positions):\n")
		for _, p := range r.RetainedOrder {
			fmt.Fprintf(&b, "  %d -> %d: %s\n", p.BeforePosition, p.AfterPosition, collectionItem(p.Item))
		}
	}
	notes(&b, "Limitation", r.Limitations)
	return b.String()
}

// CollectionStatus shows each item of a collection and the holdings that
// resolve it.
func CollectionStatus(r library.CollectionStatusResult) string {
	var b strings.Builder
	field(&b, "Collection", Line(r.Name))
	field(&b, "Library", Line(r.LibraryID))
	fmt.Fprintf(&b, "Items: %d | held %d | in review %d | recording incomplete %d | not held %d\n", len(r.Items), r.Held, r.InReview, r.Incomplete, r.NotHeld)
	for _, it := range r.Items {
		fmt.Fprintf(&b, "\n%s %s\n", words(it.Status), collectionItem(it.Item))
		if it.DeclaredTracks > 0 {
			fmt.Fprintf(&b, "  %d of %d tracks held\n", it.HeldTracks, it.DeclaredTracks)
		}
		for _, h := range it.Holdings {
			fmt.Fprintf(&b, "  %s\n", Line(h))
		}
		if it.Item.Note != "" {
			fmt.Fprintf(&b, "  Note: %s\n", Line(it.Item.Note))
		}
	}
	notes(&b, "Limitation", r.Limitations)
	return b.String()
}

// CollectionAcquire shows the acquisitions a collection needs and, once
// applied, their outcomes.
func CollectionAcquire(r app.CollectionAcquireResult) string {
	var b strings.Builder
	field(&b, "Collection", Line(r.Status.Name))
	field(&b, "Intake", Line(r.Intake))
	fmt.Fprintf(&b, "Items: %d | held %d | to acquire %d\n", len(r.Status.Items), r.Status.Held+r.Status.InReview, len(r.Steps))
	for _, s := range r.Steps {
		fmt.Fprintf(&b, "\n%s %s\n", words(s.Action), collectionItem(s.Item))
		fmt.Fprintf(&b, "  Packet: %s\n", Line(s.Packet))
		if s.Outcome != "" {
			fmt.Fprintf(&b, "  Outcome: %s", words(s.Outcome))
			if s.Bytes > 0 {
				fmt.Fprintf(&b, ", %d bytes", s.Bytes)
			}
			if s.Antivirus != "" {
				fmt.Fprintf(&b, ", antivirus %s", words(s.Antivirus))
			}
			b.WriteString("\n")
		}
		if s.Detail != "" {
			fmt.Fprintf(&b, "  %s\n", Line(s.Detail))
		}
	}
	notes(&b, "Limitation", r.Limitations)
	switch {
	case !r.Applied && len(r.Steps) > 0:
		b.WriteString("Preview only. Nothing was fetched. Add --apply to acquire these items.\n")
	case !r.Applied:
		b.WriteString("Every item is held. Nothing to acquire.\n")
	case r.Complete:
		fmt.Fprintf(&b, "Transferred: %d bytes\nNext: nemalo library ingest %s %s --apply\n", r.Transferred, Line(r.Status.Library), Line(r.Intake))
	default:
		fmt.Fprintf(&b, "Transferred: %d bytes\nSome items did not complete; they are kept for review. Run the same command again to resume interrupted ones.\n", r.Transferred)
	}
	return b.String()
}

// NarrationChange shows the exact user statement and selected assets before
// an append or withdrawal. Provider evidence and audio remain separate.
func NarrationChange(r library.NarrationResult) string {
	if r.LibraryID == "" {
		return ""
	}
	var b strings.Builder
	state := "Preview"
	if r.Applied {
		state = "Recorded"
	} else if r.ApplyRequested {
		state = "Narration change not completed"
	}
	fmt.Fprintf(&b, "%s: %s user narration declaration\n", state, words(r.Action))
	field(&b, "Library", Exact(r.Library))
	field(&b, "Library ID", Line(r.LibraryID))
	if r.Statement != nil {
		narrationStatement(&b, *r.Statement, true)
	}
	fmt.Fprintf(&b, "Verified assets: %d | Verified bytes: %d\n", r.VerifiedAssets, r.VerifiedBytes)
	if r.AlreadyRecorded {
		b.WriteString("The requested state is already recorded; no declaration append is needed.\n")
	}
	if r.RecoveredRescan {
		b.WriteString("Completed an earlier pending rescan before this change.\n")
	}
	if !r.Applied {
		field(&b, "Plan", Line(r.Plan))
		if !r.ApplyRequested {
			b.WriteString("Preview only. No library files were written.\n")
		}
	}
	notes(&b, "Limitation", r.Limitations)
	return b.String()
}

func narrationStatement(b *strings.Builder, s library.NarrationDeclarationStatus, assets bool) {
	field(b, "Declaration", Line(s.ID))
	field(b, "User narration", Line(s.Declaration.NarrationID))
	field(b, "Evidence", Line(s.Evidence))
	fmt.Fprintf(b, "State: %s | Active: %t\n", words(s.Status), s.Active)
	for _, selection := range s.Selections {
		ref := selection.Selection
		fmt.Fprintf(b, "  Selection: %s | %s | %s | %s\n", Line(ref.SourceID), Line(ref.Format), words(ref.Kind), words(selection.Status))
		field(b, "  Expected ordered bytes", Line(ref.ByteIdentity))
		if assets {
			for i, asset := range selection.Assets {
				fmt.Fprintf(b, "    %d. %s\n", i+1, Line(asset))
			}
		}
	}
}
