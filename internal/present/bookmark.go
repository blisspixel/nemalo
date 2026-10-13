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
	fmt.Fprintf(&b, "Library: %q\nLibrary ID: %q\nBookmarks: %s\nSaved locations: %d\n", s.Library, s.LibraryID, state, len(s.Bookmarks))
	if len(s.Bookmarks) == 0 {
		b.WriteString("No saved locations.\n")
	}
	for _, mark := range s.Bookmarks {
		fmt.Fprintf(&b, "%s\n  %s  unit %d  offset %d\n  note: %q\n", mark.AssetID, mark.Representation, mark.Unit, mark.Offset, mark.Note)
	}
	if s.Output != "" {
		fmt.Fprintf(&b, "Wrote: %q\n", s.Output)
	}
	fmt.Fprintf(&b, "Durability: %q\n", s.Durability)
	b.WriteString("Notes are not part of the book. A bookmark changes only when you save, note, delete, or import one.\n")
	return b.String()
}

// Bookmark formats one saved location.
func Bookmark(mark library.Bookmark) string {
	return fmt.Sprintf("Saved %s\n%s  unit %d  offset %d\nnote: %q\nThe note is not part of the book. Closing does not save a bookmark.\n", mark.AssetID, mark.Representation, mark.Unit, mark.Offset, mark.Note)
}

// BookmarkResume formats an explicit restart. Source text is included only
// when the saved bytes still match. The note is printed apart from that text.
func BookmarkResume(status, detail string, mark library.Bookmark, delivered *content.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Bookmark: %s\n%s\n", status, detail)
	if mark.AssetID != "" {
		fmt.Fprintf(&b, "%s\n%s  unit %d  offset %d\nnote: %q\nThe note is not part of the book.\n", mark.AssetID, mark.Representation, mark.Unit, mark.Offset, mark.Note)
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
