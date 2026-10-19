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
	for _, r := range e.Recordings {
		acquirable := "not acquirable"
		if r.Acquirable {
			acquirable = "acquirable"
		}
		fmt.Fprintf(&b, "\nRecording %s: %d track(s), %d declared bytes, order from %s, %s\n", Line(r.Format), len(r.Tracks), r.Bytes, words(r.OrderSource), acquirable)
		for _, t := range r.Tracks {
			fmt.Fprintf(&b, "   %d. %s", t.Position, Line(t.File))
			if title := Line(t.Title); title != "" {
				fmt.Fprintf(&b, " (%s)", title)
			}
			b.WriteString("\n")
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
