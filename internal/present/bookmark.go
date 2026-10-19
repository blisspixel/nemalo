package present

import (
	"fmt"
	"strings"

	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/library"
)

// Bookmarks formats explicit saved locations. Notes are the reader's text.
func Bookmarks(s library.BookmarkState) string {
	var b strings.Builder
	state := "disabled"
	if s.Enabled {
		state = "enabled"
	}
	field(&b, "Library", Line(s.Library))
	field(&b, "Library ID", Line(s.LibraryID))
	fmt.Fprintf(&b, "Bookmarks: %s\nSaved locations: %d\n", state, len(s.Bookmarks))
	if len(s.Bookmarks) == 0 {
		b.WriteString("No saved locations.\n")
	}
	for _, mark := range s.Bookmarks {
		fmt.Fprintf(&b, "%s\n  %s  unit %d  offset %d\n", Line(mark.AssetID), Line(mark.Representation), mark.Unit, mark.Offset)
		if mark.Note != "" {
			fmt.Fprintf(&b, "  note: %s\n", Line(mark.Note))
		}
	}
	field(&b, "Wrote", Line(s.Output))
	field(&b, "Durability", words(s.Durability))
	b.WriteString("Notes are not part of the book. A bookmark changes only when you save, note, delete, or import one.\n")
	return b.String()
}

// Bookmark formats one saved location.
func Bookmark(mark library.Bookmark) string {
	return fmt.Sprintf("Saved %s\n%s  unit %d  offset %d\n%sThe note is not part of the book. Closing does not save a bookmark.\n", Line(mark.AssetID), Line(mark.Representation), mark.Unit, mark.Offset, noteLine(mark.Note))
}

// BookmarkResume formats an explicit restart. Source text is included only
// when the saved bytes still match. The note is printed apart from that text.
func BookmarkResume(status, detail string, mark library.Bookmark, delivered *content.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Bookmark: %s\n%s\n", words(status), Line(detail))
	if mark.AssetID != "" {
		fmt.Fprintf(&b, "%s\n%s  unit %d  offset %d\n%sThe note is not part of the book.\n", Line(mark.AssetID), Line(mark.Representation), mark.Unit, mark.Offset, noteLine(mark.Note))
	}
	if delivered != nil {
		b.WriteString("\n")
		b.WriteString(Content(*delivered))
		return b.String()
	}
	if status != "" && status != "restored" {
		b.WriteString("No source text was returned.\n")
	}
	return b.String()
}

// noteLine is the reader's note on its own row, or nothing without one.
func noteLine(note string) string {
	if note == "" {
		return ""
	}
	return "note: " + Line(note) + "\n"
}
