package library

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/blisspixel/nemalo/internal/safeio"
)

const bookmarkName = "bookmarks.jsonl"
const bookmarkKind = "nemalo-bookmarks/1"
const maxBookmarkNote = 2000
const bookmarkTailSlack = maxRecord

// maxBookmarkBytes is the bookmark journal cap. Tests lower it to prove a
// failed append keeps the previous records; production uses 1 MiB.
var maxBookmarkBytes = 1 << 20

const bookmarkDurability = "bookmark journal synced on writes; directory-entry power-loss durability depends on the filesystem"

// ErrBookmarksNotEnabled means this library has no bookmark journal.
// Enable is the only operation that creates one.
var ErrBookmarksNotEnabled = errors.New("bookmarks are not enabled; run bookmark enable on this library")

// ErrBookmarksDisabled means the journal exists and saves and resumes are refused.
// Listing, export, notes, and deletion still use the saved records.
var ErrBookmarksDisabled = errors.New("bookmarks are disabled")

// Bookmark is one explicit saved location. The note is the reader's text,
// never a passage from the book.
type Bookmark struct {
	ID               string `json:"id"`
	AssetID          string `json:"asset_id"`
	Representation   string `json:"representation"`
	RepresentationID string `json:"representation_id"`
	Unit             int    `json:"unit"`
	Offset           int    `json:"offset"`
	Cursor           string `json:"cursor,omitempty"`
	Note             string `json:"note"`
}

// BookmarkLocation is the position a caller asks to save. The journal assigns
// the bookmark ID and keeps any note already stored for the asset.
type BookmarkLocation struct {
	AssetID          string
	Representation   string
	RepresentationID string
	Unit             int
	Offset           int
	Cursor           string
}

// BookmarkState is the current explicit locations, one per asset.
type BookmarkState struct {
	Library    string     `json:"library"`
	LibraryID  string     `json:"library_id"`
	Enabled    bool       `json:"enabled"`
	Bookmarks  []Bookmark `json:"bookmarks"`
	Output     string     `json:"output,omitempty"`
	Durability string     `json:"durability"`
}

// bookmarkEvent is one canonical journal line. Unit and offset stay present
// at zero: a start position must not disappear. Note stays present so a
// cleared note is distinct from a line that forgot the field.
type bookmarkEvent struct {
	SchemaVersion    int    `json:"schema_version"`
	Sequence         int    `json:"sequence"`
	LibraryID        string `json:"library_id"`
	Event            string `json:"event"`
	ID               string `json:"id,omitempty"`
	AssetID          string `json:"asset_id,omitempty"`
	Representation   string `json:"representation,omitempty"`
	RepresentationID string `json:"representation_id,omitempty"`
	Unit             int    `json:"unit"`
	Offset           int    `json:"offset"`
	Cursor           string `json:"cursor,omitempty"`
	Note             string `json:"note"`
	At               string `json:"at"`
}

type bookmarkView struct {
	events  []bookmarkEvent
	enabled bool
	current map[string]Bookmark
}

type bookmarkFile struct {
	SchemaVersion int        `json:"schema_version"`
	Kind          string     `json:"kind"`
	LibraryID     string     `json:"library_id"`
	Bookmarks     []Bookmark `json:"bookmarks"`
}

// EnableBookmarks creates the journal, or appends an enable line when a
// previous disable is in effect. Repeating enable writes nothing.
func EnableBookmarks(ctx context.Context, directory string) (BookmarkState, error) {
	return mutateBookmarks(ctx, directory, func(_ string, view bookmarkView) ([]bookmarkEvent, error) {
		if view.enabled {
			return nil, nil
		}
		return []bookmarkEvent{{Event: "enabled"}}, nil
	})
}

// DisableBookmarks keeps every saved location and refuses later saves.
// Repeating disable writes nothing. A library that was never enabled is unchanged.
func DisableBookmarks(ctx context.Context, directory string) (BookmarkState, error) {
	return mutateBookmarks(ctx, directory, func(_ string, view bookmarkView) ([]bookmarkEvent, error) {
		if len(view.events) == 0 {
			return nil, ErrBookmarksNotEnabled
		}
		if !view.enabled {
			return nil, nil
		}
		return []bookmarkEvent{{Event: "disabled"}}, nil
	})
}

// ListBookmarks returns the current locations. A trailing partial line is
// ignored. A damaged complete line returns ErrStateReview and changes nothing.
func ListBookmarks(ctx context.Context, directory string) (BookmarkState, error) {
	abs, control, err := openBookmark(ctx, directory, false)
	if err != nil {
		return BookmarkState{}, err
	}
	defer control.close()
	libraryID, view, err := lockedBookmarkView(control)
	if err != nil {
		return BookmarkState{}, err
	}
	if len(view.events) == 0 {
		return BookmarkState{}, ErrBookmarksNotEnabled
	}
	return stateFrom(abs, libraryID, view), nil
}

// SaveBookmark records one location for an asset. The same place again does
// not append, and the existing note stays. A new place gets a new ID and
// carries that note forward. Disabled and never-enabled libraries write nothing.
func SaveBookmark(ctx context.Context, directory string, loc BookmarkLocation) (Bookmark, error) {
	if err := validLocation(loc); err != nil {
		return Bookmark{}, err
	}
	var saved Bookmark
	_, err := mutateBookmarks(ctx, directory, func(_ string, view bookmarkView) ([]bookmarkEvent, error) {
		if err := requireEnabled(view); err != nil {
			return nil, err
		}
		if prev, ok := view.current[loc.AssetID]; ok && samePlace(prev, loc) {
			saved = prev
			return nil, nil
		}
		note := ""
		if prev, ok := view.current[loc.AssetID]; ok {
			note = prev.Note
		}
		event := saveEvent(loc, note)
		saved = bookmarkFrom(event)
		return []bookmarkEvent{event}, nil
	})
	if err != nil {
		return Bookmark{}, err
	}
	return saved, nil
}

// SetBookmarkNote stores the reader's own text on the current bookmark.
// An empty note clears it. Without a bookmark the file is unchanged.
func SetBookmarkNote(ctx context.Context, directory, assetID, note string) (Bookmark, error) {
	if !validAssetID(assetID) {
		return Bookmark{}, errors.New("bookmark note requires an exact sha256: asset ID")
	}
	if !validBookmarkNote(note) {
		return Bookmark{}, errors.New("bookmark note must be valid UTF-8 of at most 2000 bytes, without a NUL")
	}
	var saved Bookmark
	_, err := mutateBookmarks(ctx, directory, func(_ string, view bookmarkView) ([]bookmarkEvent, error) {
		if len(view.events) == 0 {
			return nil, ErrBookmarksNotEnabled
		}
		prev, ok := view.current[assetID]
		if !ok {
			return nil, errors.New("no bookmark is saved for this asset")
		}
		if prev.Note == note {
			saved = prev
			return nil, nil
		}
		saved = prev
		saved.Note = note
		return []bookmarkEvent{{Event: "note", ID: prev.ID, AssetID: assetID, Note: note}}, nil
	})
	if err != nil {
		return Bookmark{}, err
	}
	return saved, nil
}

// DeleteBookmark appends a deletion and drops the asset's current location.
// Earlier journal lines stay. Deleting an asset that has no bookmark writes nothing.
func DeleteBookmark(ctx context.Context, directory, assetID string) (BookmarkState, error) {
	if !validAssetID(assetID) {
		return BookmarkState{}, errors.New("bookmark delete requires an exact sha256: asset ID")
	}
	return mutateBookmarks(ctx, directory, func(_ string, view bookmarkView) ([]bookmarkEvent, error) {
		if len(view.events) == 0 {
			return nil, ErrBookmarksNotEnabled
		}
		prev, ok := view.current[assetID]
		if !ok {
			return nil, errors.New("no bookmark is saved for this asset")
		}
		return []bookmarkEvent{{Event: "delete", ID: prev.ID, AssetID: assetID}}, nil
	})
}

// ExportBookmarks writes the current locations to a new file outside .nemalo.
// It does not overwrite. A disabled journal still exports.
func ExportBookmarks(ctx context.Context, directory, output string) (BookmarkState, error) {
	state, err := ListBookmarks(ctx, directory)
	if err != nil {
		return BookmarkState{}, err
	}
	if err = writeBookmarkFile(directory, output, state); err != nil {
		return BookmarkState{}, err
	}
	state.Output = output
	return state, nil
}

// ImportBookmarks adds locations from an export. The library ID must match
// and bookmarks must be enabled. Without replace, any different location or
// note fails the whole import and writes nothing. Identical records write
// nothing. Replace updates a different location with a save and a different
// note with a note event. Assets absent from the file are left in place.
func ImportBookmarks(ctx context.Context, directory, input string, replace bool) (BookmarkState, error) {
	file, err := readBookmarkFile(input)
	if err != nil {
		return BookmarkState{}, err
	}
	return mutateBookmarks(ctx, directory, func(libraryID string, view bookmarkView) ([]bookmarkEvent, error) {
		if libraryID != file.LibraryID {
			return nil, errors.New("bookmark file is for a different library")
		}
		if err := requireEnabled(view); err != nil {
			return nil, err
		}
		return planImport(view, file, replace)
	})
}

func requireEnabled(view bookmarkView) error {
	if len(view.events) == 0 {
		return ErrBookmarksNotEnabled
	}
	if !view.enabled {
		return ErrBookmarksDisabled
	}
	return nil
}

func mutateBookmarks(ctx context.Context, directory string, fn func(string, bookmarkView) ([]bookmarkEvent, error)) (BookmarkState, error) {
	abs, control, err := openBookmark(ctx, directory, true)
	if err != nil {
		return BookmarkState{}, err
	}
	defer control.close()
	libraryID, view, err := lockedBookmarkView(control)
	if err != nil {
		return BookmarkState{}, err
	}
	events, err := fn(libraryID, view)
	if err != nil {
		return BookmarkState{}, err
	}
	if len(events) > 0 {
		if err = stampEvents(libraryID, view, events); err != nil {
			return BookmarkState{}, err
		}
		if _, _, err = replay(append(append([]bookmarkEvent{}, view.events...), events...)); err != nil {
			return BookmarkState{}, err
		}
		if err = appendBookmarkEvents(ctx, control, libraryID, events); err != nil {
			return BookmarkState{}, err
		}
		libraryID, view, err = lockedBookmarkView(control)
		if err != nil {
			return BookmarkState{}, err
		}
	}
	if len(view.events) == 0 {
		return BookmarkState{}, ErrBookmarksNotEnabled
	}
	return stateFrom(abs, libraryID, view), nil
}

func stampEvents(libraryID string, view bookmarkView, events []bookmarkEvent) error {
	at := time.Now().UTC().Format(time.RFC3339Nano)
	if !validBookmarkTime(at) {
		return errors.New("bookmark timestamp is not canonical UTC")
	}
	for i := range events {
		events[i].SchemaVersion = 1
		events[i].Sequence = len(view.events) + 1 + i
		events[i].LibraryID = libraryID
		events[i].At = at
	}
	return nil
}

func openBookmark(ctx context.Context, directory string, write bool) (string, *control, error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	if directory == "" {
		return "", nil, errors.New("bookmarks require an explicit library directory")
	}
	abs, err := filepath.Abs(directory)
	if err != nil {
		return "", nil, err
	}
	control, _, err := openControl(abs, write)
	if err != nil {
		return "", nil, err
	}
	return abs, control, nil
}

func lockedBookmarkView(control *control) (string, bookmarkView, error) {
	initEvents, err := readInit(control)
	if err != nil {
		return "", bookmarkView{}, err
	}
	if len(initEvents) != 2 {
		return "", bookmarkView{}, errors.New("bookmarks require an initialized library")
	}
	libraryID := initEvents[0].LibraryID
	prefix, err := readBookmarkPrefix(control.root)
	if err != nil {
		return "", bookmarkView{}, err
	}
	view, err := parseBookmarkJournal(prefix, libraryID)
	if err != nil {
		return "", bookmarkView{}, err
	}
	return libraryID, view, nil
}

func stateFrom(abs, libraryID string, view bookmarkView) BookmarkState {
	list := make([]Bookmark, 0, len(view.current))
	for _, mark := range view.current {
		list = append(list, mark)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].AssetID < list[j].AssetID })
	return BookmarkState{Library: abs, LibraryID: libraryID, Enabled: view.enabled, Bookmarks: list, Durability: bookmarkDurability}
}

func readBookmarkPrefix(root *os.Root) ([]byte, error) {
	info, err := root.Lstat(bookmarkName)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrStateReview
	}
	// A torn tail may run past the cap by one record. A complete file past
	// the cap, or a tail longer than that, is kept and refused.
	if info.Size() > int64(maxBookmarkBytes)+bookmarkTailSlack {
		return nil, ErrStateReview
	}
	f, err := openRegular(root, bookmarkName, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, info.Size()+1))
	if err != nil || int64(len(b)) != info.Size() {
		return nil, ErrStateReview
	}
	if len(b) == 0 {
		return nil, nil
	}
	if b[len(b)-1] == '\n' {
		if len(b) > maxBookmarkBytes {
			return nil, ErrStateReview
		}
		return b, nil
	}
	cut := bytes.LastIndexByte(b, '\n')
	if cut < 0 {
		return nil, nil
	}
	prefix := b[:cut+1]
	if len(prefix) > maxBookmarkBytes {
		return nil, ErrStateReview
	}
	return prefix, nil
}

func parseBookmarkJournal(b []byte, libraryID string) (bookmarkView, error) {
	view := bookmarkView{current: map[string]Bookmark{}}
	if len(b) == 0 {
		return view, nil
	}
	if b[len(b)-1] != '\n' {
		return bookmarkView{}, ErrStateReview
	}
	lines := bytes.Split(b[:len(b)-1], []byte{'\n'})
	events := make([]bookmarkEvent, 0, len(lines))
	for _, line := range lines {
		if len(line) == 0 {
			return bookmarkView{}, ErrStateReview
		}
		var event bookmarkEvent
		if err := canonical(line, &event); err != nil {
			return bookmarkView{}, ErrStateReview
		}
		if event.SchemaVersion != 1 || event.LibraryID != libraryID || !validBookmarkTime(event.At) {
			return bookmarkView{}, ErrStateReview
		}
		events = append(events, event)
	}
	enabled, current, err := replay(events)
	if err != nil {
		return bookmarkView{}, err
	}
	view.events, view.enabled, view.current = events, enabled, current
	return view, nil
}

func replay(events []bookmarkEvent) (bool, map[string]Bookmark, error) {
	current := map[string]Bookmark{}
	enabled := false
	for i, event := range events {
		if event.Sequence != i+1 || event.SchemaVersion != 1 || !validStateID(event.LibraryID, "library") || !validBookmarkTime(event.At) {
			return false, nil, ErrStateReview
		}
		if i > 0 && event.LibraryID != events[0].LibraryID {
			return false, nil, ErrStateReview
		}
		switch event.Event {
		case "enabled":
			if !bareBookmarkEvent(event) {
				return false, nil, ErrStateReview
			}
			enabled = true
		case "disabled":
			if i == 0 || !bareBookmarkEvent(event) {
				return false, nil, ErrStateReview
			}
			enabled = false
		case "save":
			if !enabled || !validSaveEvent(event) {
				return false, nil, ErrStateReview
			}
			current[event.AssetID] = bookmarkFrom(event)
		case "note":
			mark, ok := current[event.AssetID]
			if !ok || event.ID != mark.ID || !validNoteEvent(event) {
				return false, nil, ErrStateReview
			}
			mark.Note = event.Note
			current[event.AssetID] = mark
		case "delete":
			mark, ok := current[event.AssetID]
			if !ok || event.ID != mark.ID || !validDeleteEvent(event) {
				return false, nil, ErrStateReview
			}
			delete(current, event.AssetID)
		default:
			return false, nil, ErrStateReview
		}
	}
	if len(events) > 0 && events[0].Event != "enabled" {
		return false, nil, ErrStateReview
	}
	return enabled, current, nil
}

func appendBookmarkEvents(ctx context.Context, control *control, libraryID string, events []bookmarkEvent) error {
	if err := repairBookmarkTail(ctx, control, libraryID); err != nil {
		return err
	}
	info, err := control.root.Lstat(bookmarkName)
	size := int64(0)
	if err == nil {
		size = info.Size()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			return err
		}
		size += int64(len(data) + 1)
		if len(data)+1 > maxRecord || size > int64(maxBookmarkBytes) {
			return errors.New("library journal exceeds its byte limit; existing records were preserved")
		}
	}
	for _, event := range events {
		if err := appendCanonical(ctx, control, bookmarkName, maxBookmarkBytes, event); err != nil {
			return err
		}
	}
	return nil
}

// repairBookmarkTail drops only a trailing incomplete line, and only when the
// complete prefix parses. A blank or invalid complete line is left in place.
func repairBookmarkTail(ctx context.Context, control *control, libraryID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := control.root.Lstat(bookmarkName)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return ErrStateReview
	}
	if info.Size() > int64(maxBookmarkBytes)+bookmarkTailSlack {
		return ErrStateReview
	}
	f, err := openRegular(control.root, bookmarkName, os.O_RDWR)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, info.Size()+1))
	if err != nil || int64(len(b)) != info.Size() {
		return ErrStateReview
	}
	if len(b) == 0 || b[len(b)-1] == '\n' {
		return nil
	}
	cut := bytes.LastIndexByte(b, '\n')
	prefix := []byte(nil)
	if cut >= 0 {
		prefix = b[:cut+1]
	}
	if len(prefix) > maxBookmarkBytes {
		return ErrStateReview
	}
	if _, err = parseBookmarkJournal(prefix, libraryID); err != nil {
		return err
	}
	if err = f.Truncate(int64(len(prefix))); err != nil {
		return err
	}
	return f.Sync()
}

func saveEvent(loc BookmarkLocation, note string) bookmarkEvent {
	return bookmarkEvent{
		Event: "save", ID: newStateID("bookmark"), AssetID: loc.AssetID,
		Representation: loc.Representation, RepresentationID: loc.RepresentationID,
		Unit: loc.Unit, Offset: loc.Offset, Cursor: loc.Cursor, Note: note,
	}
}

func bookmarkFrom(event bookmarkEvent) Bookmark {
	return Bookmark{
		ID: event.ID, AssetID: event.AssetID, Representation: event.Representation,
		RepresentationID: event.RepresentationID, Unit: event.Unit, Offset: event.Offset,
		Cursor: event.Cursor, Note: event.Note,
	}
}

func samePlace(mark Bookmark, loc BookmarkLocation) bool {
	return mark.AssetID == loc.AssetID && mark.Representation == loc.Representation && mark.RepresentationID == loc.RepresentationID && mark.Unit == loc.Unit && mark.Offset == loc.Offset && mark.Cursor == loc.Cursor
}

func validLocation(loc BookmarkLocation) error {
	if !validAssetID(loc.AssetID) || !validAssetID(loc.RepresentationID) || !validRepresentation(loc.Representation) {
		return errors.New("bookmark save requires an asset ID, a representation, and a sha256: representation ID")
	}
	if loc.Unit < 0 || loc.Unit >= 10000 || loc.Offset < 0 || loc.Offset > 8388608 {
		return errors.New("bookmark position requires unit 0-9999 and offset 0-8388608")
	}
	if !validCursor(loc.Cursor) {
		return errors.New("bookmark cursor must be at most 2048 bytes and must not contain a NUL")
	}
	return nil
}

func validSaveEvent(event bookmarkEvent) bool {
	return validStateID(event.ID, "bookmark") && validAssetID(event.AssetID) && validAssetID(event.RepresentationID) && validRepresentation(event.Representation) && event.Unit >= 0 && event.Unit < 10000 && event.Offset >= 0 && event.Offset <= 8388608 && validCursor(event.Cursor) && validBookmarkNote(event.Note)
}

func validNoteEvent(event bookmarkEvent) bool {
	return validStateID(event.ID, "bookmark") && validAssetID(event.AssetID) && event.Representation == "" && event.RepresentationID == "" && event.Unit == 0 && event.Offset == 0 && event.Cursor == "" && validBookmarkNote(event.Note)
}

func validDeleteEvent(event bookmarkEvent) bool {
	return validNoteEvent(event) && event.Note == ""
}

func bareBookmarkEvent(event bookmarkEvent) bool {
	return event.ID == "" && event.AssetID == "" && event.Representation == "" && event.RepresentationID == "" && event.Unit == 0 && event.Offset == 0 && event.Cursor == "" && event.Note == ""
}

func validRepresentation(value string) bool {
	return value != "" && len(value) <= 80 && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\r\n")
}

func validCursor(value string) bool {
	return len(value) <= 2048 && utf8.ValidString(value) && !strings.Contains(value, "\x00")
}

func validBookmarkNote(value string) bool {
	return len(value) <= maxBookmarkNote && utf8.ValidString(value) && !strings.Contains(value, "\x00")
}

func validBookmarkTime(value string) bool {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && parsed.Location() == time.UTC && parsed.Format(time.RFC3339Nano) == value
}

func planImport(view bookmarkView, file bookmarkFile, replace bool) ([]bookmarkEvent, error) {
	seen := map[string]bool{}
	var events []bookmarkEvent
	for _, mark := range file.Bookmarks {
		if seen[mark.AssetID] {
			return nil, errors.New("bookmark file lists the same asset twice")
		}
		seen[mark.AssetID] = true
		loc := BookmarkLocation{AssetID: mark.AssetID, Representation: mark.Representation, RepresentationID: mark.RepresentationID, Unit: mark.Unit, Offset: mark.Offset, Cursor: mark.Cursor}
		if err := validLocation(loc); err != nil {
			return nil, err
		}
		if !validBookmarkNote(mark.Note) {
			return nil, errors.New("bookmark file has a note that cannot be stored")
		}
		prev, ok := view.current[mark.AssetID]
		if ok && samePlace(prev, loc) && prev.Note == mark.Note {
			continue
		}
		if ok && !replace {
			return nil, errors.New("bookmark file differs from a saved location; nothing was imported")
		}
		if ok && samePlace(prev, loc) {
			events = append(events, bookmarkEvent{Event: "note", ID: prev.ID, AssetID: prev.AssetID, Note: mark.Note})
			prev.Note = mark.Note
			view.current[mark.AssetID] = prev
			continue
		}
		event := saveEvent(loc, mark.Note)
		events = append(events, event)
		view.current[mark.AssetID] = bookmarkFrom(event)
	}
	return events, nil
}

func writeBookmarkFile(directory, output string, state BookmarkState) error {
	if output == "" {
		return errors.New("bookmark export requires a new file path")
	}
	absLib, err := filepath.Abs(directory)
	if err != nil {
		return err
	}
	absOut, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	if within(filepath.Join(absLib, ".nemalo"), absOut) {
		return errors.New("bookmark export must be created outside .nemalo")
	}
	parent, base := filepath.Dir(absOut), filepath.Base(absOut)
	if base == "." || base == ".." || strings.Contains(base, "\x00") {
		return errors.New("bookmark export requires a file name")
	}
	payload := bookmarkFile{SchemaVersion: 1, Kind: bookmarkKind, LibraryID: state.LibraryID, Bookmarks: state.Bookmarks}
	if payload.Bookmarks == nil {
		payload.Bookmarks = []Bookmark{}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	root, err := safeio.OpenRoot(parent)
	if err != nil {
		return fmt.Errorf("bookmark export directory: %w", err)
	}
	defer root.Close()
	f, err := root.OpenFile(base, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.New("bookmark export never overwrites an existing file")
		}
		return err
	}
	_, writeErr := f.Write(data)
	syncErr, closeErr := f.Sync(), f.Close()
	if err = errors.Join(writeErr, syncErr, closeErr); err != nil {
		_ = root.Remove(base)
		return err
	}
	return nil
}

func readBookmarkFile(input string) (bookmarkFile, error) {
	var file bookmarkFile
	if input == "" {
		return file, errors.New("bookmark import requires a file")
	}
	info, err := os.Lstat(input)
	if err != nil {
		return file, err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 1<<20 {
		return file, errors.New("bookmark import requires a regular file of at most 1 MiB")
	}
	f, err := os.Open(input)
	if err != nil {
		return file, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, info.Size()+1))
	if err != nil || int64(len(data)) != info.Size() {
		return file, errors.New("bookmark import could not read the file")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&file); err != nil {
		return bookmarkFile{}, errors.New("bookmark file is malformed; nothing was imported")
	}
	var extra any
	if err = dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return bookmarkFile{}, errors.New("bookmark file is malformed; nothing was imported")
	}
	if file.SchemaVersion != 1 || file.Kind != bookmarkKind || !validStateID(file.LibraryID, "library") {
		return bookmarkFile{}, errors.New("bookmark file is not a nemalo-bookmarks/1 export for one library")
	}
	if file.Bookmarks == nil {
		file.Bookmarks = []Bookmark{}
	}
	return file, nil
}
