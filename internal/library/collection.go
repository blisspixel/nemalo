package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/blisspixel/nemalo/internal/discovery"
)

const (
	maxCollectionBytes = 1 << 20
	maxCollectionItems = 1000
)

// Collection is a user's declared intent: exact selections from permitted
// sources. The library's holdings are the resolved record; nothing else is
// stored for a collection.
type Collection struct {
	SchemaVersion int              `json:"schema_version"`
	Name          string           `json:"name"`
	Items         []CollectionItem `json:"items"`
}

// CollectionItem selects one source file, or one Archive recording by its
// declared format.
type CollectionItem struct {
	Source    string `json:"source"`
	File      string `json:"file,omitempty"`
	Recording string `json:"recording,omitempty"`
	Note      string `json:"note,omitempty"`
}

// Packet is the intake directory name an acquisition of the item uses: the
// source and a digest of the selection, so it is the same however items are
// ordered.
func (i CollectionItem) Packet() string {
	sum := sha256.Sum256([]byte(i.Source + "\x00" + i.File + "\x00" + i.Recording))
	var b strings.Builder
	for _, c := range i.Source {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-' {
			b.WriteRune(c)
		} else {
			b.WriteByte('-')
		}
		if b.Len() >= 60 {
			break
		}
	}
	return b.String() + "-" + hex.EncodeToString(sum[:4])
}

func (i CollectionItem) validate() error {
	archive, gutenberg := discovery.ValidArchiveID(i.Source), discovery.ValidGutenbergID(i.Source)
	paper, version, arxiv := discovery.SplitArxivID(i.Source)
	switch {
	case !archive && !gutenberg && !arxiv:
		return fmt.Errorf("source %q is not archive:ITEM, gutenberg:NUMBER, or arxiv:IDvN", i.Source)
	case (i.File == "") == (i.Recording == ""):
		return fmt.Errorf("%s needs exactly one of file or recording", i.Source)
	case i.Recording != "" && (!archive || len(i.Recording) > 200 || strings.ContainsAny(i.Recording, "\x00\r\n")):
		return fmt.Errorf("%s: a recording is an Archive item's declared format, at most 200 bytes", i.Source)
	case i.File != "" && (i.File == "." || !fs.ValidPath(i.File) || len(i.File) > 1000 || strings.ContainsAny(i.File, "\\:\x00\r\n")):
		return fmt.Errorf("%s: file must be an exact source file name", i.Source)
	case gutenberg && !discovery.GutenbergEPUBName(i.Source, i.File):
		return fmt.Errorf("%s: a Gutenberg file is an EPUB named by its record, such as 1342.epub.noimages", i.Source)
	case arxiv && (version == 0 || i.File != discovery.ArxivPDFName(paper, version)):
		return fmt.Errorf("%s: an arXiv item names an explicit version and its PDF, such as arxiv:1706.03762v7 with 1706.03762v7.pdf", i.Source)
	case len(i.Note) > 500 || strings.ContainsAny(i.Note, "\x00\r\n"):
		return fmt.Errorf("%s: a note is one line of at most 500 bytes", i.Source)
	}
	return nil
}

// Validate checks the declared form, without reaching any source.
func (c Collection) Validate() error {
	if c.SchemaVersion != 1 {
		return errors.New("collection schema_version must be 1")
	}
	if !validNote(c.Name, 200) {
		return errors.New("collection name must be one line of 1 to 200 bytes")
	}
	if len(c.Items) == 0 || len(c.Items) > maxCollectionItems {
		return fmt.Errorf("a collection holds 1 to %d items", maxCollectionItems)
	}
	seen := map[string]bool{}
	for _, item := range c.Items {
		if err := item.validate(); err != nil {
			return err
		}
		if seen[item.Packet()] {
			return fmt.Errorf("%s %s%s is listed twice", item.Source, item.File, item.Recording)
		}
		seen[item.Packet()] = true
	}
	return nil
}

// ReadCollection reads and validates one collection file, at most 1 MiB,
// refusing unknown fields, and returns its SHA-256.
func ReadCollection(path string) (Collection, string, error) {
	var c Collection
	f, err := os.Open(path)
	if err != nil {
		return c, "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return c, "", err
	}
	if !info.Mode().IsRegular() {
		return c, "", errors.New("a collection must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxCollectionBytes+1))
	if err != nil {
		return c, "", err
	}
	if len(data) > maxCollectionBytes {
		return c, "", errors.New("a collection file is at most 1 MiB")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return c, "", fmt.Errorf("read collection: %w", err)
	}
	if dec.More() {
		return c, "", errors.New("read collection: more than one JSON value")
	}
	if err := c.Validate(); err != nil {
		return c, "", err
	}
	sum := sha256.Sum256(data)
	return c, hex.EncodeToString(sum[:]), nil
}

// Item statuses against the library.
const (
	CollectionHeld       = "held"
	CollectionInReview   = "held_in_review"
	CollectionIncomplete = "recording_incomplete"
	CollectionNotHeld    = "not_held"
)

// CollectionStatusResult resolves every item of a collection against the
// library's holdings, offline.
type CollectionStatusResult struct {
	SchemaVersion int                    `json:"schema_version"`
	Collection    string                 `json:"collection"`
	SHA256        string                 `json:"collection_sha256"`
	Name          string                 `json:"name"`
	Library       string                 `json:"library"`
	LibraryID     string                 `json:"library_id"`
	Items         []CollectionItemStatus `json:"items"`
	Held          int                    `json:"held"`
	InReview      int                    `json:"held_in_review"`
	Incomplete    int                    `json:"recording_incomplete"`
	NotHeld       int                    `json:"not_held"`
	Limitations   []string               `json:"limitations"`
}

// CollectionItemStatus is one item and the holdings that resolve it.
type CollectionItemStatus struct {
	Item           CollectionItem `json:"item"`
	Status         string         `json:"status"`
	Holdings       []string       `json:"holdings"`
	HeldTracks     int            `json:"held_tracks,omitempty"`
	DeclaredTracks int            `json:"declared_tracks,omitempty"`
	Packet         string         `json:"packet"`
}

// CollectionStatus reads the collection and the library journal. An item is
// held when a holding's source is that exact source and file, or, for a
// recording, when every declared track position of that format is held.
// Nothing is fetched and nothing is written.
func CollectionStatus(ctx context.Context, directory, collection string) (CollectionStatusResult, error) {
	r := CollectionStatusResult{SchemaVersion: 1, Items: []CollectionItemStatus{}, Limitations: []string{
		"An item is resolved only by an exact source and file, or recording format, recorded at acquisition. Titles and identifiers are not matched.",
		"Held means the library records those bytes; library check verifies them. A source may have changed since.",
	}}
	path, err := filepath.Abs(collection)
	if err != nil {
		return r, err
	}
	r.Collection = path
	c, digest, err := ReadCollection(path)
	if err != nil {
		return r, err
	}
	r.SHA256, r.Name = digest, c.Name
	root, err := filepath.Abs(directory)
	if err != nil {
		return r, err
	}
	r.Library = root
	state, err := LibraryStatus(ctx, root)
	if err != nil {
		return r, err
	}
	if state.Status != "initialized" {
		return r, errors.New("collection status requires an initialized library; run library init on this explicit directory")
	}
	r.LibraryID = state.LibraryID
	control, _, err := openControl(root, false)
	if err != nil {
		return r, err
	}
	defer control.close()
	_, lines, err := lockedManaged(control, state.LibraryID)
	if err != nil {
		return r, err
	}
	type fileKey struct{ source, file string }
	type trackKey struct{ source, format string }
	files := map[fileKey][]Holding{}
	tracks := map[trackKey]map[int]Holding{}
	declared := map[trackKey]int{}
	for _, h := range latestList(lines) {
		for _, s := range h.Sources {
			if s.SourceID == "" {
				continue
			}
			if s.SourceFile != "" {
				k := fileKey{s.SourceID, s.SourceFile}
				files[k] = appendHolding(files[k], h)
			}
			if rec := s.Recording; rec != nil {
				k := trackKey{s.SourceID, rec.Format}
				if tracks[k] == nil {
					tracks[k] = map[int]Holding{}
				}
				if _, ok := tracks[k][rec.Position]; !ok {
					tracks[k][rec.Position] = h
				}
				declared[k] = max(declared[k], rec.Tracks)
			}
		}
	}
	for _, item := range c.Items {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		st := CollectionItemStatus{Item: item, Status: CollectionNotHeld, Holdings: []string{}, Packet: item.Packet()}
		var held []Holding
		if item.File != "" {
			held = files[fileKey{item.Source, item.File}]
		} else {
			k := trackKey{item.Source, item.Recording}
			st.DeclaredTracks, st.HeldTracks = declared[k], len(tracks[k])
			for position := 1; position <= st.DeclaredTracks; position++ {
				if h, ok := tracks[k][position]; ok {
					held = appendHolding(held, h)
				}
			}
			if len(held) > 0 && len(held) < st.DeclaredTracks {
				st.Status = CollectionIncomplete
			}
		}
		for _, h := range held {
			st.Holdings = append(st.Holdings, h.ID)
		}
		if len(held) > 0 && st.Status == CollectionNotHeld {
			st.Status = CollectionHeld
			for _, h := range held {
				if h.Status != "checked" {
					st.Status = CollectionInReview
				}
			}
		}
		switch st.Status {
		case CollectionHeld:
			r.Held++
		case CollectionInReview:
			r.InReview++
		case CollectionIncomplete:
			r.Incomplete++
		default:
			r.NotHeld++
		}
		r.Items = append(r.Items, st)
	}
	return r, nil
}

func appendHolding(list []Holding, h Holding) []Holding {
	for _, existing := range list {
		if existing.ID == h.ID {
			return list
		}
	}
	return append(list, h)
}
