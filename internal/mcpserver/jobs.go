package mcpserver

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/blisspixel/nemalo/internal/acquisition"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/jobs"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type jobInput struct {
	ID string `json:"id" jsonschema:"job ID as job:HEX, from a start tool or job_list"`
}

type acquisitionInput struct {
	SourceID        string `json:"source_id" jsonschema:"archive:ITEM to acquire from"`
	File            string `json:"file,omitempty" jsonschema:"exact name of one EPUB, PDF, MP3, M4B, or M4A file the evaluation lists; omit with recording_format"`
	RecordingFormat string `json:"recording_format,omitempty" jsonschema:"a declared recording format the evaluation lists, such as 64Kbps MP3, to acquire every track in order"`
	Output          string `json:"output" jsonschema:"absolute path of a new intake folder whose parent is inside a configured scope"`
	Library         string `json:"library,omitempty" jsonschema:"optional initialized scoped library to record as intended context only; grants no import or library write authority"`
	MaxBytes        int64  `json:"max_bytes,omitempty" jsonschema:"byte budget; defaults to the format's limit, or 4 GiB for a recording"`
	Resume          bool   `json:"resume,omitempty" jsonschema:"continue an interrupted acquisition into its existing output folder"`
	NoScan          bool   `json:"no_scan,omitempty" jsonschema:"skip the post-download antivirus scan"`
}

type rescanInput struct {
	Library string `json:"library" jsonschema:"absolute path of an initialized library inside a configured scope"`
}

// addJobs registers the long operations as durable jobs and the tools that
// read and cancel them. A start returns at once with the job's ID; the work
// does not depend on the call or the connection staying open.
func addJobs(r *registrar, service app.Service, opts Options, scopes scope) {
	store := opts.Jobs
	if opts.Network {
		addJob(r, "acquisition_start", true, "Start acquiring one selected Archive file, or every track of a declared recording, into a new untrusted intake folder, as a durable job. Returns the job at once; read it with job_get. The download is checked against the source's size and checksums, scanned by the installed antivirus unless no_scan, and a detection is quarantined. Nothing enters a library until it is imported. Ask the person before starting it.",
			func(ctx context.Context, in acquisitionInput) (jobs.Job, error) {
				// Archive only: Gutenberg stays out of MCP.
				if !discovery.ValidArchiveID(in.SourceID) {
					return jobs.Job{}, errors.New("source_id must be archive:ITEM")
				}
				output, err := scopes.newPath(in.Output)
				if in.Resume {
					output, err = scopes.path(in.Output)
				}
				if err != nil {
					return jobs.Job{}, err
				}
				if (in.File == "") == (in.RecordingFormat == "") {
					return jobs.Job{}, errors.New("give exactly one of file or recording_format")
				}
				var binding any
				if in.Library != "" {
					lib, err := scopes.path(in.Library)
					if err != nil {
						return jobs.Job{}, err
					}
					binding, err = service.JobContext(ctx, lib)
					if err != nil {
						return jobs.Job{}, err
					}
				}
				if in.RecordingFormat != "" {
					req := acquisition.RecordingRequest{SourceID: in.SourceID, Format: in.RecordingFormat, Output: output, MaxBytes: in.MaxBytes, Resume: in.Resume, NoScan: in.NoScan}
					if req.MaxBytes == 0 {
						req.MaxBytes = 4 << 30
					}
					if err := req.Validate(); err != nil {
						return jobs.Job{}, err
					}
					return store.StartWithContext("acquire_recording", req, binding, func(ctx context.Context, report func(jobs.Progress)) (any, error) {
						req.Progress = func(phase string, done, total int64) {
							report(jobs.Progress{Phase: phase, Done: done, Total: total, Unit: "tracks"})
						}
						result, err := service.AcquireRecording(ctx, req)
						if err != nil && result.Status == "" {
							return nil, err
						}
						return result, err
					})
				}
				req := acquisition.Request{SourceID: in.SourceID, File: in.File, Output: output, MaxBytes: in.MaxBytes, Resume: in.Resume, NoScan: in.NoScan}
				if req.MaxBytes == 0 {
					req.MaxBytes = acquisition.DefaultMaxBytes(in.File)
				}
				if err := req.Validate(); err != nil {
					return jobs.Job{}, err
				}
				return store.StartWithContext("acquire", req, binding, func(ctx context.Context, report func(jobs.Progress)) (any, error) {
					req.Progress = func(phase string, done, total int64) {
						report(jobs.Progress{Phase: phase, Done: done, Total: total, Unit: "bytes"})
					}
					// A refusal before any work records no empty result.
					result, err := service.Acquire(ctx, req)
					if err != nil && result.Status == "not_started" {
						return nil, err
					}
					return result, err
				})
			})
	}
	addJob(r, "library_rescan_start", false, "Start a rescan of an initialized library as a durable job: the limited checks run again and the installed antivirus scans each holding in review only because it lacks scan evidence, and the results are recorded. Stored files are never changed. Returns the job at once; read it with job_get.",
		func(ctx context.Context, in rescanInput) (jobs.Job, error) {
			lib, err := scopes.path(in.Library)
			if err != nil {
				return jobs.Job{}, err
			}
			req := library.RescanRequest{Library: lib, Apply: true}
			binding, err := service.JobContext(ctx, lib)
			if err != nil {
				return jobs.Job{}, err
			}
			return store.StartWithContext("library_rescan", map[string]string{"library": lib}, binding, func(ctx context.Context, report func(jobs.Progress)) (any, error) {
				report(jobs.Progress{Phase: "rescan"})
				return service.Rescan(ctx, req)
			})
		})
	visible := func(j jobs.Job) bool { return jobInScope(scopes, j) }
	addJob(r, "job_get", false, "Read one durable job: its kind, status (queued, running, succeeded, failed, cancelled, or interrupted), last progress, and, once finished, its result or error. An interrupted job's process ended before it finished; start it again, with resume for an acquisition.",
		func(ctx context.Context, in jobInput) (jobs.Job, error) {
			j, err := store.Get(in.ID)
			if err == nil && !visible(j) {
				return jobs.Job{}, jobs.ErrNotFound
			}
			return j, err
		})
	addJob(r, "job_list", false, "List up to 100 durable jobs within this server's scopes, newest first, with their status.",
		func(ctx context.Context, _ emptyInput) ([]jobs.Job, error) {
			all, err := store.List()
			out := []jobs.Job{}
			for _, j := range all {
				if visible(j) {
					out = append(out, j)
				}
			}
			return out, err
		})
	addJob(r, "job_cancel", false, "Ask a job to stop, whichever server runs it. It stops at its next cancellation check and records cancelled; an interrupted acquisition keeps its packet for resume.",
		func(ctx context.Context, in jobInput) (jobs.Job, error) {
			if j, err := store.Get(in.ID); err != nil || !visible(j) {
				return jobs.Job{}, jobs.ErrNotFound
			}
			return store.Cancel(in.ID)
		})
}

// jobInScope shows a job only to a server whose scopes hold the paths it
// names, so one server never reveals another's folders.
func jobInScope(s scope, j jobs.Job) bool {
	req, ok := j.Request.(map[string]any)
	if !ok {
		return false
	}
	for _, key := range []string{"output", "library"} {
		if p, ok := req[key].(string); ok && p != "" {
			clean := filepath.Clean(p)
			if within(s.roots, clean) || within(s.lexical, clean) {
				return true
			}
		}
	}
	return false
}

// addJob registers a job tool. Starting, reading, and cancelling a job are
// not destructive, are not idempotent (a start starts another job, and a
// read may record that a job was interrupted), and only an acquisition
// reaches the network.
func addJob[In, Out any](r *registrar, name string, network bool, description string, run func(context.Context, In) (Out, error)) {
	notDestructive := false
	annotations := &mcp.ToolAnnotations{ReadOnlyHint: false, IdempotentHint: false, DestructiveHint: &notDestructive, OpenWorldHint: &network}
	register(r, name, description, annotations, run)
}
