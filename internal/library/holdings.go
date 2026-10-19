package library

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"strings"

	"github.com/blisspixel/nemalo/internal/assessment"
)

const operationsName = "operations.jsonl"
const holdingsName = "holdings.jsonl"
const maxOperations = 4 << 20
const maxHoldings = 8 << 20
const maxRecord = 64 << 10

// Holding is one committed managed asset. Later lines for the same ID supersede
// earlier ones. Paths are slash-separated and are not authoritative identity.
type Holding struct {
	SchemaVersion      int        `json:"schema_version"`
	LibraryID          string     `json:"library_id"`
	ID                 string     `json:"id"`
	SHA256             string     `json:"sha256"`
	Bytes              int64      `json:"bytes"`
	Format             string     `json:"format"`
	Status             string     `json:"status"`
	Reason             string     `json:"reason"`
	Path               string     `json:"path"`
	Titles             []string   `json:"titles"`
	Languages          []string   `json:"languages"`
	IdentitiesRecorded bool       `json:"identities_recorded,omitempty"`
	IdentifiersOmitted int        `json:"identifiers_omitted,omitempty"`
	Identities         []Identity `json:"identities,omitempty"`
	Sources            []Source   `json:"sources"`
}

// Source records where a stored asset came from. Rights strings are declarations.
type Source struct {
	Kind             string   `json:"kind"`
	Path             string   `json:"path"`
	SourceID         string   `json:"source_id,omitempty"`
	SourceFile       string   `json:"source_file,omitempty"`
	DeclaredURL      string   `json:"declared_url,omitempty"`
	DeclaredRights   []string `json:"declared_rights,omitempty"`
	DeclaredLicenses []string `json:"declared_licenses,omitempty"`
	// Recording places a track packet in a declared recording, as recorded by
	// acquire --recording. It is a source declaration of order, not a check.
	Recording *SourceRecording `json:"recording,omitempty"`
}

// SourceRecording is a track's declared place in a recording.
type SourceRecording struct {
	Format          string  `json:"source_format"`
	Position        int     `json:"position"`
	Tracks          int     `json:"tracks"`
	Title           string  `json:"title,omitempty"`
	DeclaredSeconds float64 `json:"declared_seconds,omitempty"`
}

func validRecording(r *SourceRecording) bool {
	return r == nil || (validNote(r.Format, 200) && r.Position >= 1 && r.Position <= r.Tracks && r.Tracks <= 1000 &&
		(r.Title == "" || validNote(r.Title, 500)) && r.DeclaredSeconds >= 0 && r.DeclaredSeconds <= 1e6)
}

type operation struct {
	SchemaVersion int    `json:"schema_version"`
	Sequence      int    `json:"sequence"`
	LibraryID     string `json:"library_id"`
	OperationID   string `json:"operation_id"`
	Event         string `json:"event"`
	Source        string `json:"source"`
	AssetID       string `json:"asset_id"`
	Format        string `json:"format"`
	Bytes         int64  `json:"bytes"`
	Status        string `json:"status"`
	Reason        string `json:"reason"`
	At            string `json:"at"`
}

func assetRel(hash, format string) string {
	return path.Join("assets", "sha256", hash[:2], hash+"."+format)
}

func validAssetID(id string) bool {
	if len(id) != 71 || !strings.HasPrefix(id, "sha256:") {
		return false
	}
	return validHash(id[7:])
}

func classify(report assessment.Report, scan bool) (status, reason string, err error) {
	format := report.DetectedFormat
	if format != "epub" && format != "pdf" && format != "mp3" {
		return "", "", errors.New("import accepts EPUB, PDF, and MP3 assessment results")
	}
	if report.SHA256 == "" || report.Bytes < 1 || report.Status == "" || report.Status == "incomplete" {
		return "", "", errors.New("assessment incomplete; source unchanged and nothing imported")
	}
	// A PDF is checked like an EPUB, and only when its page tree was walked
	// and its active content fully examined and empty; an MP3 only when its
	// frames were indexed. Anything less is a finding that keeps it in review.
	examined := format == "epub" ||
		(format == "pdf" && report.Checks.PDF != nil && !report.Checks.PDF.Partial && report.Checks.PDF.ActiveExamined && len(report.Checks.PDF.ActiveContent) == 0) ||
		(format == "mp3" && report.Checks.MP3 != nil && report.Checks.MP3.Frames > 0)
	if scan && examined && report.Status == "limited_checks_passed" && report.Antivirus.Status == "no_detections_reported" && len(report.Findings) == 0 {
		return "checked", "checks_and_scan_recorded", nil
	}
	if format == "pdf" && !examined && report.Status == "limited_checks_passed" && len(report.Findings) == 0 {
		return "review", "pdf_not_fully_examined", nil
	}
	// A failed scan folds the assessment status to needs_review without a finding.
	// Name that scanner status instead of hiding it behind needs_review.
	if scan && len(report.Findings) == 0 && report.Antivirus.Status != "no_detections_reported" {
		return "review", scanReason(report.Antivirus.Status), nil
	}
	if report.Status != "limited_checks_passed" {
		if len(report.Status) > 200 || strings.ContainsAny(report.Status, "\x00\r\n") {
			return "", "", errors.New("assessment status cannot be recorded")
		}
		return "review", report.Status, nil
	}
	if len(report.Findings) > 0 {
		return "review", "needs_review", nil
	}
	if scan {
		return "review", scanReason(report.Antivirus.Status), nil
	}
	return "review", "unscanned", nil
}

func scanReason(status string) string {
	switch status {
	case "unavailable", "incomplete", "findings", "not_scanned":
		return "scan_" + status
	default:
		return "scan_unrecognized"
	}
}

func validateManaged(root *os.Root, libraryID string) error {
	ops, err := readOptional(root, operationsName, maxOperations)
	if err != nil {
		return err
	}
	holds, err := readOptional(root, holdingsName, maxHoldings)
	if err != nil {
		return err
	}
	if ops == nil && holds == nil {
		return nil
	}
	if libraryID == "" || (holds != nil && ops == nil) {
		return ErrStateReview
	}
	events, err := parseOperations(ops, libraryID)
	if err != nil {
		return ErrStateReview
	}
	lines, err := parseHoldings(holds, libraryID)
	if err != nil {
		return ErrStateReview
	}
	if err := consistent(events, lines); err != nil {
		return ErrStateReview
	}
	return nil
}

func readOptional(root *os.Root, name string, max int) ([]byte, error) {
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > int64(max) {
		return nil, ErrStateReview
	}
	f, err := openRegular(root, name, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, int64(max)+1))
	if err != nil || len(b) == 0 || len(b) > max {
		return nil, ErrStateReview
	}
	if b[len(b)-1] != '\n' {
		// A write interrupted before its line feed, by a crash or a full
		// disk, never committed its record: only complete lines count, and
		// the next writer removes the rest. More than one record's worth of
		// unterminated bytes is not a torn write.
		cut := bytes.LastIndexByte(b, '\n')
		if len(b)-(cut+1) > maxRecord {
			return nil, ErrStateReview
		}
		if cut < 0 {
			return nil, nil
		}
		b = b[:cut+1]
	}
	return b, nil
}

// tornTail reports a journal whose last line was cut short by an interrupted
// write.
func tornTail(root *os.Root, name string) bool {
	f, err := openRegular(root, name, os.O_RDONLY)
	if err != nil {
		return false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() == 0 {
		return false
	}
	var tail [1]byte
	_, err = f.ReadAt(tail[:], st.Size()-1)
	return err == nil && tail[0] != '\n'
}

// dropTornTail cuts an unterminated last line from a journal open for
// writing, under the writer lock, so the next record starts on its own line.
// It cuts at most one record's worth of bytes.
func dropTornTail(f *os.File, size int64) (int64, error) {
	window := min(size, int64(maxRecord)+1)
	buf := make([]byte, window)
	if _, err := f.ReadAt(buf, size-window); err != nil {
		return size, ErrStateReview
	}
	cut := bytes.LastIndexByte(buf, '\n')
	if cut < 0 && size > int64(maxRecord) {
		return size, ErrStateReview
	}
	keep := size - window + int64(cut) + 1
	if err := f.Truncate(keep); err != nil {
		return size, err
	}
	if err := f.Sync(); err != nil {
		return size, err
	}
	return keep, nil
}

func parseOperations(b []byte, libraryID string) ([]operation, error) {
	if len(b) == 0 {
		return nil, nil
	}
	lines := bytes.Split(b[:len(b)-1], []byte{'\n'})
	events := make([]operation, 0, len(lines))
	for i, line := range lines {
		var event operation
		if err := canonical(line, &event); err != nil || event.SchemaVersion != 1 || event.Sequence != i+1 || event.LibraryID != libraryID || !validStateID(event.OperationID, "operation") || event.At == "" {
			return nil, ErrStateReview
		}
		if !validAssetID(event.AssetID) || !validFormat(event.Format) || event.Bytes < 1 || event.Bytes > 256<<20 || event.Source == "" || len(event.Source) > 4096 || strings.ContainsAny(event.Source, "\x00\r\n") {
			return nil, ErrStateReview
		}
		if event.Status != "review" && event.Status != "checked" {
			return nil, ErrStateReview
		}
		if event.Status == "checked" && event.Reason != "checks_and_scan_recorded" {
			return nil, ErrStateReview
		}
		if event.Reason == "" || len(event.Reason) > 200 || strings.ContainsAny(event.Reason, "\x00\r\n") {
			return nil, ErrStateReview
		}
		if i%2 == 0 {
			if completedEvent(event.Event) == "" {
				return nil, ErrStateReview
			}
		} else {
			prev := events[i-1]
			want := completedEvent(prev.Event)
			prev.Sequence, prev.Event, prev.At = event.Sequence, event.Event, event.At
			if event.Event != want || event != prev {
				return nil, ErrStateReview
			}
		}
		events = append(events, event)
	}
	return events, nil
}

// Operations are intent/completed pairs. An import pair stores bytes and a
// holding; a rescan pair records only a new status and reason for a holding
// that is already stored. completedEvent returns "" for anything else.
func completedEvent(intent string) string {
	switch intent {
	case "import_intent":
		return "import_completed"
	case "rescan_intent":
		return "rescan_completed"
	}
	return ""
}

func isCompleted(event string) bool {
	return event == "import_completed" || event == "rescan_completed"
}

func parseHoldings(b []byte, libraryID string) ([]Holding, error) {
	if len(b) == 0 {
		return nil, nil
	}
	lines := bytes.Split(b[:len(b)-1], []byte{'\n'})
	out := make([]Holding, 0, len(lines))
	for _, line := range lines {
		var holding Holding
		if err := canonical(line, &holding); err != nil || !validHolding(holding, libraryID) {
			return nil, ErrStateReview
		}
		out = append(out, holding)
	}
	return out, nil
}

func validFormat(format string) bool {
	return format == "epub" || format == "pdf" || format == "mp3"
}

func validHolding(h Holding, libraryID string) bool {
	if h.SchemaVersion != 1 || h.LibraryID != libraryID || h.ID != "sha256:"+h.SHA256 || !validAssetID(h.ID) || !validFormat(h.Format) || h.Bytes < 1 || h.Bytes > 256<<20 {
		return false
	}
	if h.Path != assetRel(h.SHA256, h.Format) || (h.Status != "review" && h.Status != "checked") || !validNote(h.Reason, 200) {
		return false
	}
	if h.Status == "checked" && h.Reason != "checks_and_scan_recorded" {
		return false
	}
	if h.Titles == nil || h.Languages == nil || len(h.Sources) == 0 || len(h.Titles) > 32 || len(h.Languages) > 32 {
		return false
	}
	if !validIdentities(h.Identities, h.IdentitiesRecorded, h.IdentifiersOmitted) {
		return false
	}
	if h.IdentitiesRecorded && h.Format != "epub" {
		return false
	}
	for _, value := range append(append([]string{}, h.Titles...), h.Languages...) {
		if !validNote(value, 2000) {
			return false
		}
	}
	for _, source := range h.Sources {
		if (source.Kind != "file" && source.Kind != "packet" && source.Kind != "archive_member") || !validNote(source.Path, 4096) || !validToken(source.SourceID) || !validToken(source.SourceFile) {
			return false
		}
		if source.DeclaredURL != "" && !(strings.HasPrefix(source.DeclaredURL, "https://") && validNote(source.DeclaredURL, 2000) && !strings.Contains(source.DeclaredURL, " ")) {
			return false
		}
		if !validDeclaredList(source.DeclaredRights) || !validDeclaredList(source.DeclaredLicenses) {
			return false
		}
		if source.Recording != nil && (source.Kind != "packet" || h.Format != "mp3" || !validRecording(source.Recording)) {
			return false
		}
	}
	return true
}

func validNote(value string, max int) bool {
	return value != "" && len(value) <= max && !strings.ContainsAny(value, "\x00\r\n")
}

func validToken(value string) bool {
	return len(value) <= 1000 && !strings.ContainsAny(value, "\x00\r\n")
}

func validDeclaredList(values []string) bool {
	if len(values) > 32 {
		return false
	}
	for _, value := range values {
		if !validNote(value, 2000) {
			return false
		}
	}
	return true
}

func consistent(events []operation, lines []Holding) error {
	if len(events) == 0 {
		return ErrStateReview
	}
	latest := map[string]operation{}
	var pending *operation
	for i := range events {
		event := events[i]
		if completedEvent(event.Event) != "" && i == len(events)-1 {
			pending = &events[i]
			continue
		}
		if isCompleted(event.Event) {
			latest[event.AssetID] = event
		}
	}
	current := map[string]Holding{}
	for _, holding := range lines {
		current[holding.ID] = holding
	}
	for id, event := range latest {
		holding, ok := current[id]
		// An interrupted operation may already have appended the holding line
		// its pending intent describes, superseding the last completed fact.
		if ok && pending != nil && pending.AssetID == id && sameFact(holding, *pending) {
			continue
		}
		if !ok || !sameFact(holding, event) {
			return ErrStateReview
		}
	}
	if pending != nil {
		if h, ok := current[pending.AssetID]; pending.Event == "rescan_intent" && (!ok || h.Format != pending.Format || h.Bytes != pending.Bytes || h.Path != pending.Source) {
			return ErrStateReview
		}
		if holding, ok := current[pending.AssetID]; ok && !sameFact(holding, *pending) {
			if previous, ok := latest[pending.AssetID]; !ok || !sameFact(holding, previous) {
				return ErrStateReview
			}
		}
	}
	for id := range current {
		if _, ok := latest[id]; ok {
			continue
		}
		if pending != nil && pending.AssetID == id {
			continue
		}
		return ErrStateReview
	}
	return nil
}

func sameFact(h Holding, event operation) bool {
	return h.ID == event.AssetID && h.Format == event.Format && h.Bytes == event.Bytes && h.Status == event.Status && h.Reason == event.Reason
}

func canonical(line []byte, dest any) error {
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("trailing json")
	}
	encoded, err := json.Marshal(dest)
	if err != nil || !bytes.Equal(encoded, line) {
		return errors.New("journal record is not canonical")
	}
	return nil
}

func latestHoldings(lines []Holding) map[string]Holding {
	out := map[string]Holding{}
	for _, holding := range lines {
		out[holding.ID] = holding
	}
	return out
}
