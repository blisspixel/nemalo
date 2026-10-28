package present

import (
	"fmt"
	"sort"
	"strings"

	"github.com/blisspixel/nemalo/internal/discovery"
)

func Evaluation(e discovery.Evaluation) string {
	var b strings.Builder
	complete := "complete"
	if !e.Complete {
		complete = "incomplete"
	}
	fmt.Fprintf(&b, "Metadata evaluation %s: %s\n", complete, Line(e.ID))
	field(&b, "Title", list(e.Title))
	field(&b, "Creator", list(e.Creators))
	field(&b, "Language", list(e.Languages))
	field(&b, "Media type", list(e.MediaType))
	field(&b, "Date", list(e.Dates))
	field(&b, "Publisher", list(e.Publishers))
	field(&b, "Rights statement", list(e.Rights))
	field(&b, "Declared license", list(e.LicenseURLs))
	field(&b, "Access", words(e.Access))
	field(&b, "Page", Line(e.LandingPage))
	field(&b, "Metadata SHA-256", Line(e.MetadataSHA256))
	field(&b, "Open Library edition", Line(e.OpenLibraryEdition))
	if len(e.Identifiers) > 0 {
		ids := make([]string, len(e.Identifiers))
		for i, d := range e.Identifiers {
			ids[i] = Line(d.Field) + " " + Line(d.Value)
		}
		field(&b, "Declared identifiers", strings.Join(ids, "; "))
	}
	if len(e.Files) > 0 {
		fmt.Fprintf(&b, "Files listed: %d\n", len(e.Files))
	}
	if e.EditionCount > 0 || len(e.Editions) > 0 {
		fmt.Fprintf(&b, "Editions: %d listed, %d declared, offset %d\n", len(e.Editions), e.EditionCount, e.EditionOffset)
	}
	for i, edition := range e.Editions {
		fmt.Fprintf(&b, "\nEdition %d. %s\n", i+1, Line(edition.Title))
		indent := func(label, value string) {
			if value != "" {
				fmt.Fprintf(&b, "   %s: %s\n", label, value)
			}
		}
		indent("ID", Line(edition.ID))
		indent("Language", list(edition.Languages))
		indent("Publisher", list(edition.Publishers))
		indent("Date", list(edition.Dates))
		indent("ISBN", list(edition.ISBNs))
		archive := "none named"
		if edition.ArchiveID != "" {
			archive = Line(edition.ArchiveID)
		}
		indent("Archive item", archive)
		indent("Page", Line(edition.LandingPage))
		for _, finding := range edition.Findings {
			indent("Finding", words(finding))
		}
	}
	if oa := e.OpenAccess; oa != nil {
		status := "not open"
		if oa.Open {
			status = "open"
		}
		if oa.Status != "" {
			status += ", " + Line(oa.Status)
		}
		fmt.Fprintf(&b, "Open access (stated by OpenAlex): %s; %d location(s) listed, %d declared\n", status, len(oa.Locations), oa.Count)
		for i, l := range oa.Locations {
			kind := "not open"
			if l.Open {
				kind = "open"
			}
			source := Line(l.Source)
			if source == "" {
				source = "unnamed source"
			}
			fmt.Fprintf(&b, "\nLocation %d. %s (%s)\n", i+1, source, joinSet(kind, words(l.SourceType)))
			indent := func(label, value string) {
				if value != "" {
					fmt.Fprintf(&b, "   %s: %s\n", label, value)
				}
			}
			indent("Page", Line(l.LandingPage))
			indent("PDF", Line(l.PDFURL))
			indent("License", Line(l.License))
			indent("Version", Line(l.Version))
			if c := command("nemalo", "evaluate", l.Identifier); l.Identifier != "" && c != "" {
				indent("Evaluate", c)
			}
		}
	}
	field(&b, "Subject", list(e.Subjects))
	field(&b, "Bookshelf", list(e.Bookshelves))
	field(&b, "Library of Congress class", list(e.Classes))
	keys := make([]string, 0, len(e.Restrictions))
	for key := range e.Restrictions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&b, "Restriction evidence: %s = %s\n", Line(key), Line(e.Restrictions[key]))
	}
	for i, f := range e.Files {
		kind := joinSet(words(f.CandidateKind), words(f.Access))
		fmt.Fprintf(&b, "\n%d. %s", i+1, Line(f.Name))
		if kind != "" {
			fmt.Fprintf(&b, " (%s)", kind)
		}
		b.WriteString("\n")
		indent := func(label, value string) {
			if value != "" {
				fmt.Fprintf(&b, "   %s: %s\n", label, value)
			}
		}
		indent("Source format", Line(f.Format))
		if f.Bytes != nil {
			indent("Declared bytes", fmt.Sprint(*f.Bytes))
		} else {
			indent("Declared bytes", "unknown")
		}
		indent("Private flag", words(f.PrivateState))
		indent("Restricted flag", words(f.RestrictionState))
		indent("File rights", words(f.RightsStatus))
		indent("DRM", words(f.DRMStatus))
		indent("Source MD5", Line(f.MD5))
		indent("Source SHA-1", Line(f.SHA1))
		indent("Candidate URL", Line(f.DownloadURL))
		for _, finding := range f.Findings {
			indent("Finding", words(finding))
		}
	}
	if l := e.LibriVox; l != nil {
		switch l.Status {
		case "linked":
			fmt.Fprintf(&b, "\nLibriVox project %s links back to this item: %d section(s)\n", Line(l.ProjectID), len(l.Project.Sections))
		default:
			fmt.Fprintf(&b, "\nLibriVox project %s: %s", Line(l.ProjectID), words(l.Status))
			if l.Detail != "" {
				fmt.Fprintf(&b, " (%s)", Line(l.Detail))
			}
			b.WriteString("; no readers are credited\n")
		}
	}
	for _, r := range e.Recordings {
		acquirable := "not acquirable"
		if r.Acquirable {
			acquirable = "acquirable"
		}
		fmt.Fprintf(&b, "\nRecording %s: %d track(s), %d declared bytes, order from %s, %s\n", Line(r.Format), len(r.Tracks), r.Bytes, words(r.OrderSource), acquirable)
		for _, t := range r.Tracks {
			fmt.Fprintf(&b, "   %d. %s", t.Position, Exact(t.File))
			if title := Line(t.Title); title != "" {
				fmt.Fprintf(&b, " (%s)", title)
			}
			if len(t.Readers) > 0 {
				names := make([]string, len(t.Readers))
				for i, reader := range t.Readers {
					names[i] = Line(reader.Name)
				}
				fmt.Fprintf(&b, ", read by %s", strings.Join(names, ", "))
			}
			b.WriteString("\n")
		}
		if r.ReaderEvidence == discovery.ReadersNotMatched {
			b.WriteString("   Readers: LibriVox sections did not match these tracks by count and length; none credited\n")
		}
		for _, finding := range r.Findings {
			fmt.Fprintf(&b, "   Finding: %s\n", words(finding))
		}
		if c := command("nemalo", "acquire", e.ID, "--recording", r.Format, "--output", "NEW_DIRECTORY"); r.Acquirable && c != "" {
			fmt.Fprintf(&b, "   Acquire all: %s\n", c)
		}
	}
	notes(&b, "Limit", e.Limitations)
	return b.String()
}
