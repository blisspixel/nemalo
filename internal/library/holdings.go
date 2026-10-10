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
	if format != "epub" {
		return "review", "format_not_checked_for_publication", nil
	}
	if scan && report.Status == "limited_checks_passed" && report.Antivirus.Status == "no_detections_reported" && len(report.Findings) == 0 {
		return "checked", "checks_and_scan_recorded", nil
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
	if err != nil || len(b) == 0 || len(b) > max || b[len(b)-1] != '\n' {
		return nil, ErrStateReview
	}
	return b, nil
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
		if event.Status == "checked" && (event.Format != "epub" || event.Reason != "checks_and_scan_recorded") {
			return nil, ErrStateReview
		}
		if event.Reason == "" || len(event.Reason) > 200 || strings.ContainsAny(event.Reason, "\x00\r\n") {
			return nil, ErrStateReview
		}
		if i%2 == 0 {
			if event.Event != "import_intent" {
				return nil, ErrStateReview
			}
		} else {
			prev := events[i-1]
			prev.Sequence, prev.Event, prev.At = event.Sequence, event.Event, event.At
			if event.Event != "import_completed" || event != prev {
				return nil, ErrStateReview
			}
		}
		events = append(events, event)
	}
	return events, nil
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
	if h.Status == "checked" && (h.Format != "epub" || h.Reason != "checks_and_scan_recorded") {
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
		if (source.Kind != "file" && source.Kind != "packet") || !validNote(source.Path, 4096) || !validToken(source.SourceID) || !validToken(source.SourceFile) {
			return false
		}
		if source.DeclaredURL != "" && !(strings.HasPrefix(source.DeclaredURL, "https://") && validNote(source.DeclaredURL, 2000) && !strings.Contains(source.DeclaredURL, " ")) {
			return false
		}
		if !validDeclaredList(source.DeclaredRights) || !validDeclaredList(source.DeclaredLicenses) {
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
		if event.Event == "import_intent" && i == len(events)-1 {
			pending = &events[i]
			continue
		}
		if event.Event == "import_completed" {
			latest[event.AssetID] = event
		}
	}
	current := map[string]Holding{}
	for _, holding := range lines {
		current[holding.ID] = holding
	}
	for id, event := range latest {
		holding, ok := current[id]
		if !ok || !sameFact(holding, event) {
			return ErrStateReview
		}
	}
	if pending != nil {
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
