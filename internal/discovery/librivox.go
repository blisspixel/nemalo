package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// LibriVox reads one project record from the LibriVox catalog API, for the
// readers of each section. It is consulted only for an Archive item that
// names a LibriVox project, and its record is linked only when it names that
// item back.
type LibriVox struct {
	*metadataClient
	endpoint string
}

func NewLibriVox() *LibriVox {
	return &LibriVox{metadataClient: newMetadataClient("librivox.org"), endpoint: "https://librivox.org/api/feed/audiobooks"}
}

// LibriVoxLink is the LibriVox evidence for an Archive item. Status is
// "linked" when the project names this item back, "not_linked" when it names
// another item or none, "unavailable" when the record could not be read, and
// "not_checked" when no LibriVox client was configured. Readers and sections
// are declarations of the LibriVox catalog, not checks of the audio.
type LibriVoxLink struct {
	ProjectID string           `json:"project_id"`
	Status    string           `json:"status"`
	Detail    string           `json:"detail,omitempty"`
	Project   *LibriVoxProject `json:"project,omitempty"`
}

// LibriVoxProject is one recording project: a reading of one text. Another
// reading of the same text is another project.
type LibriVoxProject struct {
	ID             string            `json:"id"`
	Title          string            `json:"title"`
	Language       string            `json:"language,omitempty"`
	URL            string            `json:"url,omitempty"`
	ArchiveItem    string            `json:"archive_item,omitempty"`
	Sections       []LibriVoxSection `json:"sections"`
	MetadataSHA256 string            `json:"metadata_sha256"`
}

// LibriVoxSection is one declared section, in LibriVox's own numbering, with
// the readers the catalog credits for it.
type LibriVoxSection struct {
	Number          int      `json:"number"`
	Title           string   `json:"title,omitempty"`
	DeclaredSeconds int      `json:"declared_seconds,omitempty"`
	Readers         []Reader `json:"readers"`
}

// Reader is a reader the LibriVox catalog credits, by its stable reader ID.
type Reader struct {
	ID   string `json:"librivox_reader_id"`
	Name string `json:"name"`
}

const (
	maxLibriVoxSections = 2000
	maxSectionReaders   = 50
)

// ValidLibriVoxID reports a LibriVox project number.
func ValidLibriVoxID(id string) bool {
	if len(id) == 0 || len(id) > 9 || id[0] == '0' {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// Project reads one project. A record outside the bounds is an error rather
// than a partial list of readers.
func (p *LibriVox) Project(ctx context.Context, id string) (LibriVoxProject, error) {
	if !ValidLibriVoxID(id) {
		return LibriVoxProject{}, errors.New("a LibriVox project ID is a positive number")
	}
	q := url.Values{"id": {id}, "format": {"json"}, "extended": {"1"}}
	var raw json.RawMessage
	if err := p.getJSON(ctx, p.endpoint+"?"+q.Encode(), &raw); err != nil {
		return LibriVoxProject{}, err
	}
	return parseLibriVox(raw, id)
}

func parseLibriVox(raw json.RawMessage, id string) (LibriVoxProject, error) {
	var wire struct {
		Books []struct {
			ID       json.RawMessage `json:"id"`
			Title    json.RawMessage `json:"title"`
			Language json.RawMessage `json:"language"`
			Page     json.RawMessage `json:"url_librivox"`
			Archive  json.RawMessage `json:"url_iarchive"`
			Sections []struct {
				Number   json.RawMessage `json:"section_number"`
				Title    json.RawMessage `json:"title"`
				Playtime json.RawMessage `json:"playtime"`
				Readers  []struct {
					ID   json.RawMessage `json:"reader_id"`
					Name json.RawMessage `json:"display_name"`
				} `json:"readers"`
			} `json:"sections"`
		} `json:"books"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return LibriVoxProject{}, errors.New("decode LibriVox project: " + err.Error())
	}
	if len(wire.Books) != 1 || stringField(wire.Books[0].ID) != id {
		return LibriVoxProject{}, errors.New("LibriVox returned another project or none")
	}
	b := wire.Books[0]
	if len(b.Sections) > maxLibriVoxSections {
		return LibriVoxProject{}, errors.New("LibriVox project sections outside limits")
	}
	project := LibriVoxProject{ID: id, Title: textField(stringField(b.Title), 500), Language: textField(stringField(b.Language), 100), Sections: []LibriVoxSection{}, MetadataSHA256: digest(raw)}
	if u := stringField(b.Page); librivoxPage(u) {
		project.URL = u
	}
	project.ArchiveItem = archiveDetailsItem(stringField(b.Archive))
	seen := map[int]bool{}
	for _, s := range b.Sections {
		number, err := strconv.Atoi(stringField(s.Number))
		if err != nil || number < 0 || number > maxLibriVoxSections || seen[number] || len(s.Readers) > maxSectionReaders {
			return LibriVoxProject{}, errors.New("LibriVox section numbering or readers outside limits")
		}
		seen[number] = true
		section := LibriVoxSection{Number: number, Title: textField(stringField(s.Title), 500), Readers: []Reader{}}
		if seconds, err := strconv.Atoi(stringField(s.Playtime)); err == nil && seconds > 0 && seconds <= 1e6 {
			section.DeclaredSeconds = seconds
		}
		for _, r := range s.Readers {
			reader := Reader{ID: stringField(r.ID), Name: textField(stringField(r.Name), 200)}
			if !ValidLibriVoxID(reader.ID) || reader.Name == "" {
				return LibriVoxProject{}, errors.New("LibriVox reader without an ID or name")
			}
			section.Readers = append(section.Readers, reader)
		}
		project.Sections = append(project.Sections, section)
	}
	return project, nil
}

// archiveDetailsItem returns the item an Archive details URL names, or "".
func archiveDetailsItem(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || (u.Host != "archive.org" && u.Host != "www.archive.org") || u.User != nil {
		return ""
	}
	item, ok := strings.CutPrefix(strings.TrimSuffix(u.Path, "/"), "/details/")
	if !ok || !ValidArchiveID("archive:"+item) {
		return ""
	}
	return item
}

func librivoxPage(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "librivox.org" && u.User == nil && len(raw) <= 500
}

// librivoxCandidate returns the LibriVox project an Archive item names: an
// item in the LibriVox audio collection whose call number is a project ID.
func librivoxCandidate(metadata map[string]json.RawMessage) string {
	var collections Strings
	if json.Unmarshal(metadata["collection"], &collections) != nil {
		return ""
	}
	for _, c := range collections {
		if c == "librivoxaudio" {
			if id := stringField(metadata["call_number"]); ValidLibriVoxID(id) {
				return id
			}
		}
	}
	return ""
}

// linkLibriVox reads the named project and links it only when it names item
// back. A failure leaves the Archive evaluation intact with the reason.
func (p *LibriVox) link(ctx context.Context, id, item string) *LibriVoxLink {
	link := &LibriVoxLink{ProjectID: id, Status: "not_checked"}
	if p == nil {
		return link
	}
	project, err := p.Project(ctx, id)
	switch {
	case err != nil:
		link.Status, link.Detail = "unavailable", textField(err.Error(), 300)
	case project.ArchiveItem != item:
		link.Status, link.Detail = "not_linked", "the LibriVox project names another Archive item or none"
	default:
		link.Status, link.Project = "linked", &project
	}
	return link
}

// Summary keeps the link and project identity without the sections, for a
// record of one selected file whose own readers are recorded beside it.
func (l *LibriVoxLink) Summary() *LibriVoxLink {
	if l == nil {
		return nil
	}
	s := *l
	if l.Project != nil {
		p := *l.Project
		p.Sections = []LibriVoxSection{}
		s.Project = &p
	}
	return &s
}

// Reader evidence for a recording's tracks.
const (
	ReadersMatched    = "librivox_sections_matched"
	ReadersNotMatched = "librivox_sections_not_matched"
)

// sectionTolerance is how far a section's declared length may be from its
// track's before the two are not taken as the same part of the reading.
const sectionTolerance = 3.0

// matchReaders credits each track with its section's readers, in section
// order, only when there are as many sections as tracks and every track's
// declared length agrees with its section's. Otherwise no track is credited
// and the recording says why; nothing is guessed from titles or names.
func matchReaders(link *LibriVoxLink, recordings []Recording) {
	if link == nil || link.Project == nil {
		return
	}
	sections := append([]LibriVoxSection{}, link.Project.Sections...)
	sort.Slice(sections, func(i, j int) bool { return sections[i].Number < sections[j].Number })
	for i := range recordings {
		r := &recordings[i]
		matched := len(sections) == len(r.Tracks) && len(sections) > 0
		for j := 0; matched && j < len(sections); j++ {
			declared, section := r.Tracks[j].DeclaredSeconds, float64(sections[j].DeclaredSeconds)
			matched = declared > 0 && section > 0 && math.Abs(declared-section) <= sectionTolerance && len(sections[j].Readers) > 0
		}
		if !matched {
			r.ReaderEvidence = ReadersNotMatched
			continue
		}
		r.ReaderEvidence = ReadersMatched
		for j := range r.Tracks {
			r.Tracks[j].Readers = append([]Reader{}, sections[j].Readers...)
		}
	}
}
