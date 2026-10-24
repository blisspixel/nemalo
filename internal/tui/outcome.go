package tui

import (
	"strings"

	"github.com/blisspixel/nemalo/internal/acquisition"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/library"
)

// outcome is what an operation's result says about it: nothing to mark, done,
// needing attention, or failed. It comes from the result's own fields.
type outcome int

const (
	outcomeNone outcome = iota
	outcomeOK
	outcomeAttention
	outcomeFailed
)

// resultOutcome reads an operation's outcome from its result. An error, a
// detection, or a failed item is a failure; a completed write is done; a
// finished check that found something is attention. Complete listings and reads
// stay unmarked; incomplete passive observations need attention.
func resultOutcome(data any, err error) outcome {
	if err != nil {
		return outcomeFailed
	}
	switch v := data.(type) {
	case app.CollectionAcquireResult:
		if v.Applied {
			if !v.Complete {
				return outcomeFailed
			}
			return outcomeOK
		}
	case app.JobSnapshotResult:
		if v.Status != "observed" {
			return outcomeAttention
		}
	case acquisition.Result:
		switch {
		case strings.HasPrefix(v.Status, "detected_"):
			return outcomeFailed
		case v.Complete:
			return outcomeOK
		}
	case acquisition.RecordingResult:
		switch {
		case v.Status == "stopped_on_detection":
			return outcomeFailed
		case v.Complete:
			return outcomeOK
		}
	case library.ImportResult:
		if v.Applied {
			return outcomeOK
		}
	case library.IngestResult:
		switch {
		case !v.Applied:
		case v.Failed > 0 || !v.Complete:
			return outcomeFailed
		default:
			return outcomeOK
		}
	case library.State:
		if v.Created || v.Resumed {
			return outcomeOK
		}
	case library.CleanupResult:
		if v.Applied {
			return outcomeOK
		}
	case library.RestoreResult:
		if v.Applied {
			return outcomeOK
		}
	case library.PurgeResult:
		if v.Applied {
			return outcomeOK
		}
	case library.RescanResult:
		switch {
		case v.Detections > 0:
			return outcomeFailed
		case v.Incomplete > 0:
			return outcomeAttention
		case v.Applied:
			return outcomeOK
		}
	case library.LibraryCheck:
		if v.Healthy {
			return outcomeOK
		}
		return outcomeAttention
	case library.ManagedAudit:
		if len(v.Findings) > 0 {
			return outcomeAttention
		}
		return outcomeOK
	case library.Audit:
		if len(v.Findings) > 0 || !v.Complete {
			return outcomeAttention
		}
		return outcomeOK
	case library.ExportResult, library.Bookmark:
		return outcomeOK
	case assessment.Report:
		switch {
		case v.Antivirus.Status == "findings":
			return outcomeFailed
		case v.Status == "limited_checks_passed":
			return outcomeOK
		default:
			return outcomeAttention
		}
	}
	return outcomeNone
}
