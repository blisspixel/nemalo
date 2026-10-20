package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/blisspixel/nemalo/internal/acquisition"
	"github.com/blisspixel/nemalo/internal/library"
)

// CollectionAcquireRequest acquires a collection's missing items into one
// intake directory. Without Apply nothing is fetched or written.
type CollectionAcquireRequest struct {
	Library       string
	Collection    string
	Intake        string
	Apply         bool
	MaxTotalBytes int64
	// MaxRecordingBytes bounds one recording's declared size; zero is the
	// acquire default.
	MaxRecordingBytes int64
	NoScan            bool
	OnDetection       string
}

// Step actions, decided from the library and the intake directory alone.
const (
	StepAcquire        = "acquire"
	StepResume         = "resume"
	StepAwaitingImport = "awaiting_import"
	StepRetained       = "retained_attempt"
)

// CollectionStep is one item the library does not hold, the packet it uses,
// and, once applied, what happened.
type CollectionStep struct {
	Item      library.CollectionItem `json:"item"`
	Packet    string                 `json:"packet"`
	Action    string                 `json:"action"`
	Outcome   string                 `json:"outcome,omitempty"`
	Remaining int64                  `json:"remaining_bytes,omitempty"`
	Bytes     int64                  `json:"bytes,omitempty"`
	SHA256    string                 `json:"sha256,omitempty"`
	Antivirus string                 `json:"antivirus,omitempty"`
	Detail    string                 `json:"detail,omitempty"`
}

// CollectionAcquireResult is the plan, and with Apply its outcome.
type CollectionAcquireResult struct {
	SchemaVersion int                            `json:"schema_version"`
	Status        library.CollectionStatusResult `json:"status"`
	Intake        string                         `json:"intake"`
	Applied       bool                           `json:"applied"`
	Complete      bool                           `json:"complete"`
	Steps         []CollectionStep               `json:"steps"`
	Transferred   int64                          `json:"transferred_bytes"`
	Limitations   []string                       `json:"limitations"`
}

const defaultRecordingBytes = 4 << 30

// CollectionAcquire plans and, with Apply, runs one acquisition per item the
// library does not hold, each through the same selected acquisition as
// acquire, one at a time. A packet already holding a receipt waits for
// ingest; an interrupted one is resumed; any other existing packet is left as
// it is and reported. Downloads stay untrusted intake for library ingest.
func (s Service) CollectionAcquire(ctx context.Context, request CollectionAcquireRequest) (CollectionAcquireResult, error) {
	r := CollectionAcquireResult{SchemaVersion: 1, Steps: []CollectionStep{}, Limitations: []string{
		"Each item is evaluated again at its source before any download, exactly as acquire does; a source that changed or withdrew a file fails that item only.",
		"Downloads are untrusted intake. Run library ingest on the intake directory to assess and import them.",
		"A retained attempt is never removed or reused; rename or remove it to try that item again.",
	}}
	status, err := library.CollectionStatus(ctx, request.Library, request.Collection)
	r.Status = status
	if err != nil {
		return r, err
	}
	if !filepath.IsAbs(request.Intake) {
		return r, errors.New("the intake directory must be an absolute path")
	}
	intake := filepath.Clean(request.Intake)
	r.Intake = intake
	if library.Overlaps(intake, status.Library) {
		return r, errors.New("the intake directory must be outside the library")
	}
	if request.MaxTotalBytes < 0 || request.MaxRecordingBytes < 0 || request.MaxRecordingBytes > acquisition.MaxRecordingBytes {
		return r, errors.New("byte budgets must be positive, and a recording's at most 16 GiB")
	}
	recordingBudget := request.MaxRecordingBytes
	if recordingBudget == 0 {
		recordingBudget = defaultRecordingBytes
	}
	for _, item := range status.Items {
		if item.Status != library.CollectionNotHeld && item.Status != library.CollectionIncomplete {
			continue
		}
		step := CollectionStep{Item: item.Item, Packet: filepath.Join(intake, item.Packet), Action: StepAcquire}
		if info, err := os.Lstat(step.Packet); err == nil {
			// The acquisition package reads what an existing packet holds.
			state := acquisition.PacketState{State: acquisition.PacketRetained, Detail: "the packet path is not a directory"}
			switch {
			case !info.IsDir():
			case item.Item.Recording != "":
				state = acquisition.InspectRecording(step.Packet)
			default:
				state = acquisition.InspectPacket(step.Packet)
			}
			// Packet names are lookup hints. A copied packet, or a collision in
			// their short digest, must not satisfy a different exact selection.
			if state.State != acquisition.PacketRetained && (state.SourceID != item.Item.Source || state.File != item.Item.File || state.Recording != item.Item.Recording) {
				state = acquisition.PacketState{State: acquisition.PacketRetained, Detail: "the existing packet does not record this exact collection selection; it was retained"}
			}
			switch state.State {
			case acquisition.PacketImportable:
				step.Action = StepAwaitingImport
			case acquisition.PacketResumable:
				step.Action, step.Remaining = StepResume, state.Remaining
			default:
				step.Action, step.Detail = StepRetained, state.Detail
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return r, err
		}
		r.Steps = append(r.Steps, step)
	}
	if !request.Apply {
		return r, nil
	}
	r.Applied = true
	if err := os.MkdirAll(intake, 0o700); err != nil {
		return r, err
	}
	r.Complete = true
	for i := range r.Steps {
		step := &r.Steps[i]
		if step.Action == StepAwaitingImport {
			step.Outcome = "awaiting_import"
			continue
		}
		if step.Action == StepRetained {
			step.Outcome, r.Complete = "not_started", false
			continue
		}
		if err := ctx.Err(); err != nil {
			step.Outcome, step.Detail, r.Complete = "not_started", "cancelled", false
			continue
		}
		remaining := int64(-1)
		if request.MaxTotalBytes > 0 {
			if remaining = request.MaxTotalBytes - r.Transferred; remaining < 1 {
				step.Outcome, step.Detail, r.Complete = "not_started", "the total byte budget is spent", false
				continue
			}
		}
		budget := func(limit int64) int64 {
			if remaining > 0 && remaining < limit {
				return remaining
			}
			return limit
		}
		resume := step.Action == StepResume
		if resume && remaining > 0 && step.Remaining > remaining {
			// A resume keeps the budget its packet was started with, so it is
			// started only when what it may still fetch fits.
			step.Outcome, step.Detail, r.Complete = "not_started", "what this resume may still transfer exceeds the remaining total byte budget", false
			continue
		}
		if step.Item.Recording != "" {
			res, err := s.AcquireRecording(ctx, acquisition.RecordingRequest{SourceID: step.Item.Source, Format: step.Item.Recording, Output: step.Packet, MaxBytes: budget(recordingBudget), Resume: resume, NoScan: request.NoScan, OnDetection: request.OnDetection})
			for _, t := range res.Tracks {
				if !t.Reused {
					r.Transferred += t.Bytes
					step.Bytes += t.Bytes
				}
			}
			step.Outcome = res.Status
			if err != nil {
				step.Detail = err.Error()
			}
			r.Complete = r.Complete && err == nil && res.Complete
			continue
		}
		res, err := s.Acquire(ctx, acquisition.Request{SourceID: step.Item.Source, File: step.Item.File, Output: step.Packet, MaxBytes: budget(acquisition.DefaultMaxBytes(step.Item.File)), Resume: resume, NoScan: request.NoScan, OnDetection: request.OnDetection})
		// A resumed transfer counts only the bytes this run fetched.
		fetched := max(0, res.Transfer.Bytes-res.ResumedFrom)
		r.Transferred += fetched
		step.Outcome, step.Bytes, step.SHA256, step.Antivirus = res.Status, fetched, res.Transfer.SHA256, res.Antivirus
		if err != nil {
			step.Detail = err.Error()
		}
		r.Complete = r.Complete && err == nil && res.Complete
	}
	return r, nil
}
