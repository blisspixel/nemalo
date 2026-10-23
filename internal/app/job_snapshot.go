package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/blisspixel/nemalo/internal/acquisition"
	"github.com/blisspixel/nemalo/internal/jobs"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/textsafe"
)

const MaxJobSnapshotBytes = 1 << 20

// JobLibraryContext is immutable declared intent, never library permission.
type JobLibraryContext struct {
	Schema    string `json:"schema"`
	LibraryID string `json:"library_id"`
}

// JobContext validates an explicitly selected initialized library before a job
// starts. Its shared lock is released before asynchronous work is queued.
func (s Service) JobContext(ctx context.Context, directory string) (JobLibraryContext, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	state, err := library.LibraryStatus(ctx, directory)
	if err != nil {
		return JobLibraryContext{}, err
	}
	if state.Status != "initialized" {
		return JobLibraryContext{}, errors.New("job context requires an initialized library")
	}
	return JobLibraryContext{Schema: "nemalo-job-library/1", LibraryID: state.LibraryID}, nil
}

type JobSnapshotRequest struct {
	Library, JobsDirectory string
	Limit                  int
}

type RecordedJob struct {
	ID             string         `json:"id"`
	Kind           string         `json:"kind"`
	RecordedStatus string         `json:"recorded_status"`
	Created        string         `json:"recorded_created"`
	Updated        string         `json:"recorded_updated"`
	Progress       *jobs.Progress `json:"recorded_progress,omitempty"`
	ErrorRecorded  bool           `json:"error_recorded"`
	Integrity      string         `json:"journal_integrity"`
	Owner          string         `json:"owner_observation"`
	Expired        bool           `json:"past_retention_age"`
	Relationship   string         `json:"relationship"`
	HeldTracks     int            `json:"related_held_tracks,omitempty"`
}

type JobSnapshotLimits struct {
	Entries      int `json:"directory_entries"`
	JournalBytes int `json:"bytes_per_journal"`
	ReadBytes    int `json:"total_journal_bytes"`
	Records      int `json:"records_per_journal"`
	Returned     int `json:"returned_jobs"`
	OutputBytes  int `json:"safe_json_bytes"`
	Seconds      int `json:"deadline_seconds"`
}

type JobSnapshotResult struct {
	SchemaVersion int               `json:"schema_version"`
	Schema        string            `json:"schema"`
	Status        string            `json:"status"`
	LibraryID     string            `json:"library_id,omitempty"`
	Catalog       string            `json:"catalog_observation"`
	Started       string            `json:"observation_started"`
	Finished      string            `json:"observation_finished"`
	Jobs          []RecordedJob     `json:"jobs"`
	Work          jobs.Observation  `json:"work"`
	Coverage      string            `json:"relationship_coverage"`
	Truncated     bool              `json:"results_truncated"`
	Limits        JobSnapshotLimits `json:"limits"`
	Limitations   []string          `json:"limitations"`
}

// JobSnapshot observes existing state without reconciliation, retention cleanup,
// write authority, asset reads or source-path opens. Public data contains only
// exact selected-library relationships and recorded summaries, never raw jobs.
func (s Service) JobSnapshot(ctx context.Context, request JobSnapshotRequest) (out JobSnapshotResult, err error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	limit := request.Limit
	if limit == 0 {
		limit = 20
	}
	out = JobSnapshotResult{SchemaVersion: 1, Schema: "nemalo-job-snapshot/1", Status: "unavailable", Catalog: "unavailable", Coverage: "unavailable", Jobs: []RecordedJob{},
		Started: time.Now().UTC().Format(time.RFC3339Nano),
		Limits:  JobSnapshotLimits{Entries: jobs.MaxObservedEntries, JournalBytes: jobs.MaxObservedJournalBytes, ReadBytes: jobs.MaxObservedReadBytes, Records: jobs.MaxObservedRecords, Returned: limit, OutputBytes: MaxJobSnapshotBytes, Seconds: 15},
		Limitations: []string{
			"Recorded outcomes over an observation interval, not an atomic snapshot, live owner check or proof that unfinished work is progressing. Job locks are not probed.",
			"Relationships are declarations or current recorded packet provenance, never library permission, fresh stored-byte checks, safety or consumption evidence.",
			"Unbound legacy jobs are omitted. Empty results do not prove that no unbound work exists. Acquisition associations need an available canonical holdings view.",
			"Missing state stays missing; abandoned jobs, torn tails, expired history and control files are never changed or pruned. Filesystem calls can stall beyond the deadline.",
			"Ordering is newest recorded creation time within observed records, with an ID tie-break. Incomplete traversal cannot establish global recency or absence.",
		}}
	defer func() {
		if out.Finished == "" {
			out.Finished = time.Now().UTC().Format(time.RFC3339Nano)
		}
		if err != nil {
			out.Status, out.Coverage, out.Jobs = "unavailable", "unavailable", []RecordedJob{}
			out.Catalog = "unavailable"
			out.LibraryID = ""
			if ctx.Err() != nil {
				out.Status = "cancelled"
			}
		}
	}()
	if limit < 1 || limit > 100 || request.Library == "" || !filepath.IsAbs(request.JobsDirectory) {
		return out, errors.New("job snapshot needs an explicit existing library, absolute jobs directory and limit 1-100")
	}
	err = library.WithJobScope(ctx, request.Library, func(scope library.JobScope) error {
		out.LibraryID, out.Catalog = scope.LibraryID, scope.Catalog
		packets := heldPacketIndex(scope.Holdings)
		associationGap := false
		var err error
		out.Work, err = jobs.Observe(ctx, request.JobsDirectory, func(job jobs.JournalJob, integrity string) error {
			relation, tracks := jobRelationship(job, scope.LibraryID, packets)
			if unsupportedJobAssociation(job, scope.LibraryID, relation) {
				associationGap = true
			}
			if relation == "" {
				return nil
			}
			owner := "not_applicable"
			if job.Status == jobs.Queued || job.Status == jobs.Running {
				owner = "not_observed"
			}
			updated, _ := time.Parse(time.RFC3339Nano, job.Updated)
			out.Jobs = append(out.Jobs, RecordedJob{ID: job.ID, Kind: job.Kind, RecordedStatus: job.Status, Created: job.Created, Updated: job.Updated, Progress: job.Progress, ErrorRecorded: job.Error != "", Integrity: integrity, Owner: owner, Expired: time.Since(updated) > 30*24*time.Hour, Relationship: relation, HeldTracks: tracks})
			sort.Slice(out.Jobs, func(i, j int) bool {
				a, _ := time.Parse(time.RFC3339Nano, out.Jobs[i].Created)
				b, _ := time.Parse(time.RFC3339Nano, out.Jobs[j].Created)
				if a.Equal(b) {
					return out.Jobs[i].ID < out.Jobs[j].ID
				}
				return a.After(b)
			})
			if len(out.Jobs) > limit {
				out.Truncated = true
				out.Jobs = out.Jobs[:limit]
			}
			return nil
		})
		if associationGap {
			out.Work.Complete = false
			out.Work.Problems = append(out.Work.Problems, "unsupported_or_conflicting_association")
		}
		return err
	})
	if err != nil {
		return out, err
	}
	out.Status, out.Coverage = "observed", "recorded_associations"
	if !out.Work.Complete || out.Catalog != "observed" || out.Truncated {
		out.Status = "incomplete"
	}
	if out.Catalog != "observed" {
		out.Coverage = "identity_associations_only"
	}
	// Include the final timestamp in the actual safely encoded allowance.
	out.Finished = time.Now().UTC().Format(time.RFC3339Nano)
	data, marshalErr := textsafe.MarshalJSON(out, false)
	if marshalErr != nil || len(data) > MaxJobSnapshotBytes {
		return out, errors.New("job snapshot exceeds its output allowance")
	}
	return out, ctx.Err()
}

func unsupportedJobAssociation(j jobs.JournalJob, selected, relation string) bool {
	if j.Kind != "acquire" && j.Kind != "acquire_recording" && j.Kind != "library_rescan" {
		return true
	}
	if len(j.Context) == 0 {
		return false // Unbound legacy intent is explicitly outside coverage.
	}
	var fields map[string]json.RawMessage
	var binding JobLibraryContext
	if json.Unmarshal(j.Context, &fields) != nil || len(fields) != 2 || json.Unmarshal(j.Context, &binding) != nil || binding.Schema != "nemalo-job-library/1" || !validJobLibraryID(binding.LibraryID) {
		return true
	}
	return binding.LibraryID == selected && relation == ""
}

func validJobLibraryID(value string) bool {
	rest, ok := strings.CutPrefix(value, "library:")
	b, err := hex.DecodeString(rest)
	return ok && err == nil && len(b) == 16 && hex.EncodeToString(b) == rest
}

type packetKey struct {
	Parent, Content, SourceID, File, Hash string
	Bytes                                 int64
	Format                                string
	Position                              int
}

func heldPacketIndex(holdings []library.Holding) map[packetKey]bool {
	out := map[packetKey]bool{}
	for _, h := range holdings {
		for _, source := range h.Sources {
			if source.Kind != "packet" || !cleanAbsolute(source.Path) {
				continue
			}
			key := packetKey{Parent: filepath.Dir(source.Path), Content: source.Path, SourceID: source.SourceID, File: source.SourceFile, Hash: h.SHA256, Bytes: h.Bytes}
			if source.Recording != nil {
				key.Format, key.Position = source.Recording.Format, source.Recording.Position
			}
			out[key] = true
		}
	}
	return out
}

func cleanAbsolute(name string) bool {
	return name != "" && filepath.IsAbs(name) && filepath.Clean(name) == name
}

func recordedHash(value string) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == 32 && hex.EncodeToString(b) == value
}

func jobRelationship(job jobs.JournalJob, selected string, packets map[packetKey]bool) (string, int) {
	if job.Kind != "library_rescan" && job.Kind != "acquire" && job.Kind != "acquire_recording" {
		return "", 0
	}
	if len(job.Context) > 0 {
		var fields map[string]json.RawMessage
		var binding JobLibraryContext
		if json.Unmarshal(job.Context, &fields) != nil || len(fields) != 2 || json.Unmarshal(job.Context, &binding) != nil || binding.Schema != "nemalo-job-library/1" || binding.LibraryID != selected {
			return "", 0
		}
		if job.Kind == "library_rescan" && len(job.Result) > 0 && string(job.Result) != "null" {
			var result struct {
				SchemaVersion int    `json:"schema_version"`
				LibraryID     string `json:"library_id"`
			}
			if json.Unmarshal(job.Result, &result) != nil || result.SchemaVersion != 1 || (result.LibraryID != "" && result.LibraryID != selected) {
				return "", 0
			}
		}
		return "declared_target", 0
	}
	if job.Kind == "library_rescan" {
		var result struct {
			SchemaVersion int    `json:"schema_version"`
			LibraryID     string `json:"library_id"`
		}
		if json.Unmarshal(job.Result, &result) == nil && result.SchemaVersion == 1 && result.LibraryID == selected {
			return "recorded_library_result", 0
		}
		return "", 0
	}
	if job.Kind == "acquire" {
		var req acquisition.Request
		var result struct {
			SchemaVersion    int    `json:"schema_version"`
			TransferComplete bool   `json:"transfer_complete"`
			Output           string `json:"output"`
			ContentPath      string `json:"content_path"`
			Intent           *struct {
				SchemaVersion int                 `json:"schema_version"`
				Request       acquisition.Request `json:"request"`
			} `json:"intent"`
			Transfer struct {
				SHA256 string `json:"sha256"`
				Bytes  int64  `json:"bytes"`
			} `json:"transfer"`
		}
		if json.Unmarshal(job.Request, &req) != nil || req.Validate() != nil || !cleanAbsolute(req.Output) || json.Unmarshal(job.Result, &result) != nil || result.SchemaVersion != 1 || !result.TransferComplete || result.Intent == nil || result.Intent.SchemaVersion != 1 || result.Output != req.Output || !cleanAbsolute(result.ContentPath) || filepath.Dir(result.ContentPath) != req.Output || result.Intent.Request.SourceID != req.SourceID || result.Intent.Request.File != req.File || result.Intent.Request.Output != req.Output || !recordedHash(result.Transfer.SHA256) || result.Transfer.Bytes < 1 {
			return "", 0
		}
		if req.Recording != nil || result.Intent.Request.Recording != nil {
			return "", 0
		}
		key := packetKey{Parent: req.Output, Content: result.ContentPath, SourceID: req.SourceID, File: req.File, Hash: result.Transfer.SHA256, Bytes: result.Transfer.Bytes}
		if packets[key] {
			return "held_packet_provenance", 0
		}
		return "", 0
	}
	var req acquisition.RecordingRequest
	var result acquisition.RecordingResult
	if json.Unmarshal(job.Request, &req) != nil || req.Validate() != nil || !cleanAbsolute(req.Output) || json.Unmarshal(job.Result, &result) != nil || result.SchemaVersion != 1 || result.Output != req.Output || result.SourceID != req.SourceID || result.Format != req.Format || len(result.Tracks) > 1000 {
		return "", 0
	}
	held, seen := 0, map[int]bool{}
	for _, track := range result.Tracks {
		if track.Position < 1 || track.Position > 1000 || seen[track.Position] {
			return "", 0
		}
		seen[track.Position] = true
		if !fs.ValidPath(track.Packet) || strings.ContainsAny(track.Packet, "\\:\x00\r\n") || !recordedHash(track.SHA256) || track.Bytes < 1 {
			continue
		}
		packet := filepath.Join(req.Output, filepath.FromSlash(track.Packet))
		key := packetKey{Parent: packet, Content: filepath.Join(packet, "content.mp3"), SourceID: req.SourceID, File: track.File, Hash: track.SHA256, Bytes: track.Bytes, Format: req.Format, Position: track.Position}
		if packets[key] {
			held++
		}
	}
	if held > 0 {
		return "held_recording_packets", held
	}
	return "", 0
}
