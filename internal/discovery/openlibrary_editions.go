package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// OpenLibraryEditionList is one page of editions for a work. It names an
	// Archive item only when that edition's ocaid does.
	OpenLibraryEditionList = "edition_list"
	// OpenLibraryEditionRecord is one edition that does not name an Archive item.
	OpenLibraryEditionRecord = "edition_record"
)

// Edition is one Open Library edition on a work page. ArchiveID is set only
// when the edition's ocaid is one valid Archive identifier.
type Edition struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Languages   []string `json:"languages"`
	Publishers  []string `json:"publishers"`
	Dates       []string `json:"dates"`
	ISBNs       []string `json:"isbns,omitempty"`
	ArchiveID   string   `json:"archive_id,omitempty"`
	LandingPage string   `json:"landing_page"`
	Findings    []string `json:"findings,omitempty"`
}

// EditionRecord is the parsed bibliographic record for one edition.
// OtherArchiveIDs are ia: source records that differ from ocaid. They are
// visible and are not a second selection.
type EditionRecord struct {
	ID              string
	Title           string
	Languages       []string
	Publishers      []string
	Dates           []string
	ISBNs           []string
	ArchiveID       string
	OtherArchiveIDs []string
	LandingPage     string
	MetadataURL     string
	MetadataSHA256  string
	Findings        []string
}

// ValidOpenLibraryWork reports an openlibrary:OL<digits>W identifier.
func ValidOpenLibraryWork(id string) bool {
	rest, ok := strings.CutPrefix(id, "openlibrary:")
	return ok && validWorkID(rest)
}

// ValidOpenLibraryEdition reports an openlibrary:OL<digits>M identifier.
func ValidOpenLibraryEdition(id string) bool {
	rest, ok := strings.CutPrefix(id, "openlibrary:")
	return ok && validEditionID(rest)
}

func validEditionID(id string) bool {
	if len(id) < 4 || !strings.HasPrefix(id, "OL") || !strings.HasSuffix(id, "M") {
		return false
	}
	for _, c := range id[2 : len(id)-1] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// ListEditions fetches one page of editions for a work. It does not read
// work-level ia identifiers, search availability, or Archive metadata.
func (p *OpenLibrary) ListEditions(ctx context.Context, id string, limit, offset int) (Evaluation, error) {
	e := Evaluation{ID: id, Source: "openlibrary", CheckedAt: time.Now().UTC(), Access: OpenLibraryEditionList, Restrictions: map[string]string{}, Files: []OfferedFile{}, Editions: []Edition{}, Limitations: []string{
		"This is one page of edition records for the selected work. Reading it does not download a file.",
		"An Archive identifier is listed only when that edition's ocaid names one. Work-level ia identifiers are not read.",
		"Search availability, and the first Archive identifier on a work, are not used.",
		"A listed Archive identifier is not download permission. Evaluate that edition to read the Archive item.",
		"Author records on an edition are identifiers and were not requested.",
	}}
	work, ok := strings.CutPrefix(id, "openlibrary:")
	if !ok || !validWorkID(work) || limit < 1 || limit > 50 || offset < 0 || offset > 10000 {
		return e, errors.New("edition list requires openlibrary:WORK, limit 1-50, and offset 0-10000")
	}
	origin, err := p.origin()
	if err != nil {
		return e, err
	}
	e.LandingPage = "https://openlibrary.org/works/" + work
	u, err := url.Parse(origin + "/works/" + work + "/editions.json")
	if err != nil {
		return e, err
	}
	q := u.Query()
	q.Set("limit", strconv.Itoa(limit))
	q.Set("offset", strconv.Itoa(offset))
	u.RawQuery = q.Encode()
	e.MetadataURL = u.String()
	var raw json.RawMessage
	if err := p.getJSON(ctx, e.MetadataURL, &raw); err != nil {
		return e, err
	}
	e.MetadataSHA256 = digest(raw)
	var wire struct {
		Links struct {
			Work string `json:"work"`
		} `json:"links"`
		Size    *int              `json:"size"`
		Entries []json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil || wire.Size == nil || *wire.Size < 0 || *wire.Size > 1000000 || wire.Entries == nil || len(wire.Entries) > limit || wire.Links.Work != "/works/"+work {
		return e, errors.New("edition list returned an invalid result set")
	}
	remaining := 0
	if *wire.Size > offset {
		remaining = *wire.Size - offset
	}
	expected := limit
	if remaining < expected {
		expected = remaining
	}
	// A short page would make the next fixed-size offset skip entries.
	if len(wire.Entries) != expected {
		return e, errors.New("edition list count does not match the page")
	}
	e.EditionCount = *wire.Size
	e.EditionOffset = offset
	seen := map[string]bool{}
	omitted := 0
	for _, entry := range wire.Entries {
		edition, keep, err := parseEdition(entry, work)
		if err != nil || !keep {
			omitted++
			continue
		}
		if seen[edition.ID] {
			return e, errors.New("edition list returned a duplicate edition")
		}
		seen[edition.ID] = true
		e.Editions = append(e.Editions, edition)
	}
	if omitted > 0 {
		e.Limitations = append(e.Limitations, fmt.Sprintf("%d edition entries did not name this work, had an invalid identifier, or did not have one Archive identifier in ocaid, and were omitted.", omitted))
	}
	e.Complete = true
	return e, nil
}

// ReadEdition fetches one edition record. A valid ocaid is ArchiveID.
// An ocaid that is not one Archive identifier is an error. Source records
// that name a different Archive item are reported and not selected.
func (p *OpenLibrary) ReadEdition(ctx context.Context, id string) (EditionRecord, error) {
	edition, ok := strings.CutPrefix(id, "openlibrary:")
	if !ok || !validEditionID(edition) {
		return EditionRecord{}, errors.New("edition record requires openlibrary:EDITION")
	}
	origin, err := p.origin()
	if err != nil {
		return EditionRecord{}, err
	}
	endpoint := origin + "/books/" + edition + ".json"
	var raw json.RawMessage
	if err := p.getJSON(ctx, endpoint, &raw); err != nil {
		return EditionRecord{}, err
	}
	parsed, keep, err := parseEdition(raw, "")
	if err != nil {
		return EditionRecord{}, err
	}
	if !keep || parsed.ID != id {
		return EditionRecord{}, errors.New("edition record identity does not match the request")
	}
	record := EditionRecord{
		ID: parsed.ID, Title: parsed.Title, Languages: parsed.Languages, Publishers: parsed.Publishers,
		Dates: parsed.Dates, ISBNs: parsed.ISBNs, ArchiveID: parsed.ArchiveID, LandingPage: parsed.LandingPage,
		MetadataURL: endpoint, MetadataSHA256: digest(raw), Findings: parsed.Findings,
	}
	var extra struct {
		SourceRecords Strings `json:"source_records"`
	}
	if err := json.Unmarshal(raw, &extra); err != nil {
		return EditionRecord{}, errors.New("edition source records are not a list of strings")
	}
	record.OtherArchiveIDs = otherArchiveIDs(extra.SourceRecords, record.ArchiveID, &record.Findings)
	return record, nil
}

func (p *OpenLibrary) origin() (string, error) {
	u, err := url.Parse(p.endpoint)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return "", errors.New("open library endpoint is invalid")
	}
	return u.Scheme + "://" + u.Host, nil
}

func parseEdition(raw json.RawMessage, requiredWork string) (Edition, bool, error) {
	var wire struct {
		Key         string  `json:"key"`
		Title       string  `json:"title"`
		Publishers  Strings `json:"publishers"`
		PublishDate string  `json:"publish_date"`
		Languages   []struct {
			Key string `json:"key"`
		} `json:"languages"`
		Works []struct {
			Key string `json:"key"`
		} `json:"works"`
		OCAID  json.RawMessage `json:"ocaid"`
		ISBN10 Strings         `json:"isbn_10"`
		ISBN13 Strings         `json:"isbn_13"`
		Type   struct {
			Key string `json:"key"`
		} `json:"type"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return Edition{}, false, errors.New("edition record is not the expected JSON")
	}
	if wire.Type.Key != "" && wire.Type.Key != "/type/edition" {
		return Edition{}, false, nil
	}
	key := strings.TrimPrefix(wire.Key, "/books/")
	if key == wire.Key || !validEditionID(key) {
		return Edition{}, false, nil
	}
	if requiredWork != "" && !namesWork(wire.Works, requiredWork) {
		return Edition{}, false, nil
	}
	edition := Edition{ID: "openlibrary:" + key, LandingPage: "https://openlibrary.org/books/" + key, Languages: []string{}, Publishers: []string{}, Dates: []string{}, Findings: []string{}}
	if title, ok := plain(wire.Title, 500); ok {
		edition.Title = title
	} else if strings.TrimSpace(wire.Title) != "" {
		edition.Findings = append(edition.Findings, "The edition title was omitted because it is empty, too long, or contains a control character.")
	}
	for _, language := range wire.Languages {
		code, ok := strings.CutPrefix(language.Key, "/languages/")
		if !ok || !plainLanguage(code) {
			edition.Findings = append(edition.Findings, "A language value was omitted because it is not a language identifier.")
			continue
		}
		if len(edition.Languages) == 8 {
			edition.Findings = append(edition.Findings, "Further language values were not listed.")
			break
		}
		edition.Languages = append(edition.Languages, code)
	}
	edition.Publishers = boundedPlain(wire.Publishers, 8, 200, &edition.Findings, "publisher")
	if date, ok := plain(wire.PublishDate, 80); ok {
		edition.Dates = []string{date}
	} else if strings.TrimSpace(wire.PublishDate) != "" {
		edition.Findings = append(edition.Findings, "The publication date was omitted because it is too long or contains a control character.")
	}
	isbns := append(append([]string{}, wire.ISBN13...), wire.ISBN10...)
	isbnNoted := false
	for _, isbn := range isbns {
		if !plainISBN(isbn) {
			if !isbnNoted {
				edition.Findings = append(edition.Findings, "An ISBN value was omitted because it is not a 10- or 13-character ISBN.")
				isbnNoted = true
			}
			continue
		}
		if len(edition.ISBNs) == 8 {
			edition.Findings = append(edition.Findings, "Further ISBN values were not listed.")
			break
		}
		edition.ISBNs = append(edition.ISBNs, isbn)
	}
	archiveID, err := oneArchiveID(wire.OCAID)
	if err != nil {
		return Edition{}, false, err
	}
	edition.ArchiveID = archiveID
	return edition, true, nil
}

func namesWork(works []struct {
	Key string `json:"key"`
}, work string) bool {
	if len(works) > 20 {
		return false
	}
	want := "/works/" + work
	for _, item := range works {
		if item.Key == want {
			return true
		}
	}
	return false
}

func oneArchiveID(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var id string
	if err := json.Unmarshal(raw, &id); err != nil {
		return "", errors.New("edition ocaid is not one Archive identifier")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return "", nil
	}
	if !ValidArchiveID("archive:" + id) {
		return "", errors.New("edition ocaid is not a valid Archive identifier")
	}
	return "archive:" + id, nil
}

func otherArchiveIDs(records []string, named string, findings *[]string) []string {
	if len(records) > 64 {
		*findings = append(*findings, "Further source records were not read.")
		records = records[:64]
	}
	var others []string
	seen := map[string]bool{}
	for _, record := range records {
		id, ok := strings.CutPrefix(record, "ia:")
		if !ok || len(record) > 200 || !ValidArchiveID("archive:"+id) {
			continue
		}
		full := "archive:" + id
		if full == named || seen[full] {
			continue
		}
		seen[full] = true
		if len(others) == 8 {
			*findings = append(*findings, "Further Archive identifiers in source records were not listed.")
			break
		}
		others = append(others, full)
	}
	return others
}

func boundedPlain(values []string, max, width int, findings *[]string, kind string) []string {
	out := []string{}
	noted := false
	for _, value := range values {
		text, ok := plain(value, width)
		if !ok {
			if strings.TrimSpace(value) != "" && !noted {
				*findings = append(*findings, "A "+kind+" value was omitted because it is empty, too long, or contains a control character.")
				noted = true
			}
			continue
		}
		if len(out) == max {
			*findings = append(*findings, "Further "+kind+" values were not listed.")
			break
		}
		out = append(out, text)
	}
	return out
}

func plain(value string, max int) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > max || strings.ContainsAny(value, "\x00\r\n") {
		return "", false
	}
	return value, true
}

func plainLanguage(code string) bool {
	if len(code) < 2 || len(code) > 16 {
		return false
	}
	for _, c := range code {
		if c < 'a' || c > 'z' {
			return false
		}
	}
	return true
}

func plainISBN(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 10 && len(value) != 13 {
		return false
	}
	for i, c := range value {
		digit := c >= '0' && c <= '9'
		check := i == len(value)-1 && (c == 'X' || c == 'x')
		if !digit && !check {
			return false
		}
	}
	return true
}
