package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/library"
)

// BookmarkResume is the result of an explicit restart. Content is set only
// when the saved bytes still match. A note stays on Bookmark and is never
// mixed into the source text. Bookmarks are saved locations, not reading
// acknowledgements: delivery does not confirm or advance one.
type BookmarkResume struct {
	Status   string           `json:"status"`
	Detail   string           `json:"detail,omitempty"`
	Bookmark library.Bookmark `json:"bookmark"`
	Content  *content.Result  `json:"content,omitempty"`
}

// BookmarkSave is one explicit location. RepresentationID is the id the
// caller observed. A changed source is not rewritten to the current id.
type BookmarkSave struct {
	Library          string
	AssetID          string
	Representation   string
	RepresentationID string
	Unit             int
	Offset           int
	Cursor           string
}

func (s Service) EnableBookmarks(ctx context.Context, directory string) (library.BookmarkState, error) {
	return library.EnableBookmarks(ctx, directory)
}

func (s Service) DisableBookmarks(ctx context.Context, directory string) (library.BookmarkState, error) {
	return library.DisableBookmarks(ctx, directory)
}

func (s Service) ListBookmarks(ctx context.Context, directory string) (library.BookmarkState, error) {
	return library.ListBookmarks(ctx, directory)
}

func (s Service) SetBookmarkNote(ctx context.Context, directory, assetID, note string) (library.Bookmark, error) {
	return library.SetBookmarkNote(ctx, directory, assetID, note)
}

func (s Service) DeleteBookmark(ctx context.Context, directory, assetID string) (library.BookmarkState, error) {
	return library.DeleteBookmark(ctx, directory, assetID)
}

func (s Service) ExportBookmarks(ctx context.Context, directory, output string) (library.BookmarkState, error) {
	return library.ExportBookmarks(ctx, directory, output)
}

func (s Service) ImportBookmarks(ctx context.Context, directory, input string, replace bool) (library.BookmarkState, error) {
	return library.ImportBookmarks(ctx, directory, input, replace)
}

// SaveBookmark checks the location against the current bytes, then records it.
// A mismatch writes nothing and does not substitute the representation id.
func (s Service) SaveBookmark(ctx context.Context, request BookmarkSave) (library.Bookmark, error) {
	if !readingRepresentation(request.Representation) {
		return library.Bookmark{}, errors.New("this representation cannot be saved as a bookmark")
	}
	if request.Representation == "epub-nav/1" && request.Cursor == "" {
		return library.Bookmark{}, errors.New("an epub-nav/1 bookmark needs the cursor for its declared entry")
	}
	if request.Representation == "mpeg-audio/1" && request.Cursor == "" && (request.Unit != 0 || request.Offset != 0) {
		return library.Bookmark{}, errors.New("an audio bookmark needs the cursor for that frame, or unit 0 and offset 0 for the start")
	}
	state, err := library.ListBookmarks(ctx, request.Library)
	if err != nil {
		return library.Bookmark{}, err
	}
	if !state.Enabled {
		return library.Bookmark{}, library.ErrBookmarksDisabled
	}
	if err = s.confirmBookmark(ctx, request); err != nil {
		return library.Bookmark{}, err
	}
	return library.SaveBookmark(ctx, request.Library, library.BookmarkLocation{
		AssetID: request.AssetID, Representation: request.Representation, RepresentationID: request.RepresentationID,
		Unit: request.Unit, Offset: request.Offset, Cursor: request.Cursor,
	})
}

// ResumeBookmark opens the saved location when the bytes and representation
// still match. A conflict, a changed file, or a missing asset returns no text
// and does not guess another place. Retrieval itself does not move the bookmark.
func (s Service) ResumeBookmark(ctx context.Context, directory, assetID string) (BookmarkResume, error) {
	state, err := library.ListBookmarks(ctx, directory)
	if err != nil {
		if errors.Is(err, library.ErrBookmarksNotEnabled) {
			return bookmarkOutcome("not_enabled", library.Bookmark{}, nil, err.Error(), err)
		}
		return BookmarkResume{}, err
	}
	mark, found := bookmarkFor(state, assetID)
	if !state.Enabled {
		detail := "Bookmarks are disabled. The saved location was not opened."
		if !found {
			detail = "Bookmarks are disabled. No location is saved for this asset."
			mark = library.Bookmark{}
		}
		return bookmarkOutcome("not_enabled", mark, nil, detail, nil)
	}
	if !found {
		return BookmarkResume{}, errors.New("no bookmark is saved for this asset")
	}
	if !readingRepresentation(mark.Representation) || (mark.Representation == "epub-nav/1" && mark.Cursor == "") {
		return bookmarkOutcome("conflict", mark, nil, "The saved representation does not match this source. Nothing was remapped.", nil)
	}
	req := content.Request{SchemaVersion: 1, Catalog: directory, Root: directory, AssetID: assetID, Representation: mark.Representation, MaxBytes: 4096}
	switch {
	case mark.Cursor != "":
		req.Cursor = mark.Cursor
	case mark.Representation == "mpeg-audio/1":
		req.Sample = 0
	default:
		req.Unit, req.Offset = mark.Unit, mark.Offset
	}
	got, err := s.Content(ctx, req)
	if err != nil {
		switch got.Status {
		case "changed_source":
			return bookmarkOutcome("changed_source", mark, nil, "The stored bytes changed. The bookmark was kept and no nearby text was substituted.", nil)
		case "missing_asset", "unavailable":
			return bookmarkOutcome("unavailable", mark, nil, "The asset is not available in this library. The bookmark was kept.", nil)
		case "stale_reference", "unsupported_extraction", "no_navigation", "unresolved_navigation", "navigation_choice_required":
			return bookmarkOutcome("conflict", mark, nil, "The saved representation does not match this source. Nothing was remapped.", nil)
		default:
			return BookmarkResume{}, err
		}
	}
	if got.RepresentationID != mark.RepresentationID || got.Representation != mark.Representation || got.Locator == nil || got.Locator.Unit != mark.Unit || got.Locator.Start != mark.Offset {
		return bookmarkOutcome("conflict", mark, nil, "The saved representation does not match this source. Nothing was remapped.", nil)
	}
	return bookmarkOutcome("restored", mark, &got, "Restored the saved location. The note is not part of the book.", nil)
}

func bookmarkOutcome(status string, mark library.Bookmark, delivered *content.Result, detail string, cause error) (BookmarkResume, error) {
	if status != "restored" {
		if cause == nil {
			cause = errors.New(detail)
		}
		return BookmarkResume{Status: status, Detail: detail, Bookmark: mark}, cause
	}
	return BookmarkResume{Status: status, Detail: detail, Bookmark: mark, Content: delivered}, nil
}

func bookmarkFor(state library.BookmarkState, assetID string) (library.Bookmark, bool) {
	for _, mark := range state.Bookmarks {
		if mark.AssetID == assetID {
			return mark, true
		}
	}
	return library.Bookmark{}, false
}

func (s Service) confirmBookmark(ctx context.Context, request BookmarkSave) error {
	units, err := s.Content(ctx, content.Request{
		SchemaVersion: 1, Catalog: request.Library, Root: request.Library, AssetID: request.AssetID,
		Representation: request.Representation, UnitsOnly: true, MaxBytes: 4096,
	})
	if err != nil {
		return fmt.Errorf("the source could not be read; the bookmark was not saved: %w", err)
	}
	if units.RepresentationID != request.RepresentationID {
		return errors.New("the source representation changed; the bookmark was not saved")
	}
	read := content.Request{
		SchemaVersion: 1, Catalog: request.Library, Root: request.Library, AssetID: request.AssetID,
		Representation: request.Representation, MaxBytes: 4096,
	}
	switch {
	case request.Cursor != "":
		read.Cursor = request.Cursor
	case request.Representation == "mpeg-audio/1":
		read.Sample = 0
	default:
		read.Unit, read.Offset, read.MaxBytes = request.Unit, request.Offset, 4
	}
	got, err := s.Content(ctx, read)
	if err != nil || got.Locator == nil || got.RepresentationID != request.RepresentationID || got.Locator.Unit != request.Unit || got.Locator.Start != request.Offset {
		return errors.New("this location is not in the current source; the bookmark was not saved")
	}
	return nil
}

func readingRepresentation(id string) bool {
	if id == "epub-source/1" {
		return false
	}
	for _, rep := range content.Available().Representations {
		if rep.ID != id {
			continue
		}
		for _, op := range rep.Operations {
			if op == "read" {
				return true
			}
		}
	}
	return false
}
