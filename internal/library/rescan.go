package library

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"time"

	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/safeio"
)

// RescanRequest scans stored EPUB holdings that are in review only because no
// usable scan was recorded. Without Apply nothing is written.
type RescanRequest struct {
	Library string
	Apply   bool
	Scanner assessment.Scanner
}

// RescanItem is one holding's disposition. Before and After are holding status
// and reason; Changed means a new holding line is (or would be) recorded.
type RescanItem struct {
	AssetID      string `json:"asset_id"`
	Path         string `json:"path"`
	Format       string `json:"format"`
	Before       string `json:"before"`
	BeforeReason string `json:"before_reason"`
	After        string `json:"after,omitempty"`
	AfterReason  string `json:"after_reason,omitempty"`
	Antivirus    string `json:"antivirus,omitempty"`
	Changed      bool   `json:"changed"`
	Detail       string `json:"detail,omitempty"`
}

type RescanResult struct {
	SchemaVersion int          `json:"schema_version"`
	Applied       bool         `json:"applied"`
	Library       string       `json:"library"`
	LibraryID     string       `json:"library_id,omitempty"`
	Scanned       []RescanItem `json:"scanned"`
	Skipped       []RescanItem `json:"skipped"`
	Promoted      int          `json:"promoted"`
	Detections    int          `json:"detections"`
	Incomplete    int          `json:"incomplete"`
	Limitations   []string     `json:"limitations"`
}

// rescannable names the review reasons a scan alone can resolve: the EPUB
// checks passed and only scan evidence is missing or unusable. A previous
// detection is not rescanned automatically; it needs a deliberate review.
var rescannable = map[string]bool{"unscanned": true, "scan_unavailable": true, "scan_incomplete": true, "scan_not_scanned": true, "scan_unrecognized": true}

// Rescan assesses and scans candidate holdings in place, read-only, through the
// shared assessment pipeline. ClamAV scans them in batches so signatures load
// once per batch. With Apply, each changed holding is recorded as a journaled
// rescan_intent, holding line, and rescan_completed under the writer lock.
// Stored bytes are never modified, moved, or removed.
func Rescan(ctx context.Context, request RescanRequest) (result RescanResult, err error) {
	result = RescanResult{SchemaVersion: 1, Library: request.Library, Scanned: []RescanItem{}, Skipped: []RescanItem{}, Limitations: []string{
		"Only EPUB holdings in review for a missing, unavailable, or incomplete scan are scanned. Their limited checks run again first.",
		"A previous detection, a PDF or MP3, or other findings stay in review and are not rescanned.",
		"A no-detection result is not proof the file is safe. Stored files are never changed.",
	}}
	if request.Library == "" {
		return result, errors.New("library rescan requires an initialized library directory")
	}
	root, err := filepath.Abs(request.Library)
	if err != nil {
		return result, err
	}
	result.Library = root
	state, err := LibraryStatus(ctx, root)
	if err != nil {
		return result, err
	}
	if state.Status != "initialized" {
		return result, errors.New("library rescan requires an initialized library; run library init on this explicit directory")
	}
	result.LibraryID = state.LibraryID
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	store, err := safeio.OpenRoot(root)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, store.Close()) }()
	// A dry run takes the shared reader lock; applying takes the writer lock for
	// the whole scan so the recorded status describes the bytes that were scanned.
	control, _, err := openControl(root, request.Apply)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, control.close()) }()
	events, lines, err := lockedManaged(control, state.LibraryID)
	if err != nil {
		return result, err
	}
	if request.Apply {
		if events, lines, err = rollForwardRescan(ctx, control, events, lines); err != nil {
			return result, err
		}
	}
	if len(events) > 0 && events[len(events)-1].Event == "import_intent" {
		return result, pendingError(events[len(events)-1].Source)
	}
	current := latestHoldings(lines)
	ids := make([]string, 0, len(current))
	for id := range current {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	candidates, names, options := []Holding{}, []string{}, []assessment.Options{}
	for _, id := range ids {
		h := current[id]
		item := RescanItem{AssetID: h.ID, Path: h.Path, Format: h.Format, Before: h.Status, BeforeReason: h.Reason}
		reason := h.Reason
		if h.Format == "pdf" && reason == "format_not_checked_for_publication" {
			// Held before a PDF could become checked; it is assessed now.
			reason = "unscanned"
		}
		switch {
		case h.Status != "review":
			continue
		case h.Format != "epub" && h.Format != "pdf":
			item.Detail = "This format cannot become checked; scanning would not change its status."
		case h.Reason == "scan_findings":
			item.Detail = "A scanner reported a detection. Review the file before importing its source again with --scan."
		case !rescannable[reason]:
			item.Detail = "Assessment findings keep this holding in review; a scan cannot resolve them."
		default:
			name, pathErr := assetOSPath(h.Path)
			if pathErr != nil {
				return result, pathErr
			}
			candidates, names = append(candidates, h), append(names, filepath.ToSlash(name))
			options = append(options, assessment.Options{Scan: true, ExpectedBytes: h.Bytes, ExpectedSHA256: h.SHA256})
			continue
		}
		result.Skipped = append(result.Skipped, item)
	}
	if len(candidates) == 0 {
		result.Applied = request.Apply
		return result, nil
	}
	reports, errs := assessment.CheckManyInRoot(ctx, store, names, options, request.Scanner)
	for i, h := range candidates {
		item := RescanItem{AssetID: h.ID, Path: h.Path, Format: h.Format, Before: h.Status, BeforeReason: h.Reason}
		report := normalizeFormat(reports[i], names[i])
		item.Antivirus = report.Antivirus.Status
		switch {
		case report.SHA256 != h.SHA256 || report.Bytes != h.Bytes:
			// Includes a missing or unreadable file. Audit reports it; rescan
			// never records a status for bytes it did not scan.
			item.Detail = "Stored bytes are missing or changed; run library audit. No status was recorded."
			if errs[i] != nil {
				item.Detail += " " + errs[i].Error()
			}
			result.Incomplete++
		default:
			status, reason, classErr := classify(report, true)
			if classErr != nil {
				item.Detail = classErr.Error()
				result.Incomplete++
				break
			}
			item.After, item.AfterReason = status, reason
			item.Changed = status != h.Status || reason != h.Reason
			switch {
			case status == "checked":
				result.Promoted++
			case report.Antivirus.Status == "findings":
				result.Detections++
				item.Detail = "A scanner reported a detection. The stored file was left in place for review."
			case report.Antivirus.Status != "no_detections_reported":
				result.Incomplete++
			}
			if request.Apply && item.Changed {
				if err = recordRescan(ctx, control, &events, h, status, reason); err != nil {
					return result, err
				}
			}
		}
		result.Scanned = append(result.Scanned, item)
	}
	result.Applied = request.Apply
	return result, nil
}

// recordRescan journals one status change: intent, holding line, completion.
// A crash after the intent is completed by rollForwardRescan.
func recordRescan(ctx context.Context, control *control, events *[]operation, h Holding, status, reason string) error {
	intent := operation{SchemaVersion: 1, Sequence: len(*events) + 1, LibraryID: h.LibraryID, OperationID: newStateID("operation"), Event: "rescan_intent", Source: h.Path, AssetID: h.ID, Format: h.Format, Bytes: h.Bytes, Status: status, Reason: reason, At: time.Now().UTC().Format(time.RFC3339Nano)}
	next := h
	next.Status, next.Reason = status, reason
	if !validHolding(next, h.LibraryID) {
		return errors.New("rescan record is outside the managed journal limits")
	}
	if err := reserveNew(control, intent, next, true); err != nil {
		return err
	}
	if err := appendCanonical(ctx, control, operationsName, maxOperations, intent); err != nil {
		return err
	}
	*events = append(*events, intent)
	if err := appendCanonical(ctx, control, holdingsName, maxHoldings, next); err != nil {
		return err
	}
	done := intent
	done.Sequence, done.Event, done.At = len(*events)+1, "rescan_completed", time.Now().UTC().Format(time.RFC3339Nano)
	if err := appendCanonical(ctx, control, operationsName, maxOperations, done); err != nil {
		return err
	}
	*events = append(*events, done)
	return nil
}

// rollForwardRescan completes a trailing rescan intent left by an interruption.
// The intent carries the complete new status, so finishing it is deterministic:
// append the holding line if it is missing, then the completion. Any writer
// calls this under the writer lock before starting its own operation.
func rollForwardRescan(ctx context.Context, control *control, events []operation, lines []Holding) ([]operation, []Holding, error) {
	if len(events) == 0 || events[len(events)-1].Event != "rescan_intent" {
		return events, lines, nil
	}
	intent := events[len(events)-1]
	h, ok := latestHoldings(lines)[intent.AssetID]
	// A rescan changes only status and reason; any other difference means the
	// intent does not describe this holding, so nothing is written.
	if !ok || h.Format != intent.Format || h.Bytes != intent.Bytes || h.Path != intent.Source {
		return nil, nil, ErrStateReview
	}
	if !sameFact(h, intent) {
		h.Status, h.Reason = intent.Status, intent.Reason
		if !validHolding(h, intent.LibraryID) {
			return nil, nil, ErrStateReview
		}
		if err := reserveResume(control, intent, h, true); err != nil {
			return nil, nil, err
		}
		if err := appendCanonical(ctx, control, holdingsName, maxHoldings, h); err != nil {
			return nil, nil, err
		}
		lines = append(lines, h)
	}
	done := intent
	done.Sequence, done.Event, done.At = len(events)+1, "rescan_completed", time.Now().UTC().Format(time.RFC3339Nano)
	if err := appendCanonical(ctx, control, operationsName, maxOperations, done); err != nil {
		return nil, nil, err
	}
	return append(events, done), lines, nil
}
