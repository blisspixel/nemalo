package mcpserver

import (
	"context"
	"errors"

	"github.com/blisspixel/nemalo/internal/acquisition"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/jobs"
	"github.com/blisspixel/nemalo/internal/library"
)

type jobInput struct {
	ID string `json:"id" jsonschema:"job ID as job:HEX, from a start tool or job_list"`
}

type acquisitionInput struct {
	SourceID        string `json:"source_id" jsonschema:"archive:ITEM to acquire from"`
	File            string `json:"file,omitempty" jsonschema:"exact name of one EPUB, PDF, MP3, M4B, or M4A file the evaluation lists; omit with recording_format"`
	RecordingFormat string `json:"recording_format,omitempty" jsonschema:"a declared recording format the evaluation lists, such as 64Kbps MP3, to acquire every track in order"`
	Output          string `json:"output" jsonschema:"absolute path of a new intake folder whose parent is inside a configured scope"`
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
		addWrite(r, "acquisition_start", "Start acquiring one selected Archive file, or every track of a declared recording, into a new untrusted intake folder, as a durable job. Returns the job at once; read it with job_get. The download is checked against the source's size and checksums, scanned by the installed antivirus unless no_scan, and a detection is quarantined. Nothing enters a library until it is imported. Ask the person before starting it.", false,
			func(ctx context.Context, in acquisitionInput) (jobs.Job, error) {
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
				if in.RecordingFormat != "" {
					req := acquisition.RecordingRequest{SourceID: in.SourceID, Format: in.RecordingFormat, Output: output, MaxBytes: in.MaxBytes, Resume: in.Resume, NoScan: in.NoScan}
					if req.MaxBytes == 0 {
						req.MaxBytes = 4 << 30
					}
					if err := req.Validate(); err != nil {
						return jobs.Job{}, err
					}
					return store.Start("acquire_recording", req, func(ctx context.Context, report func(jobs.Progress)) (any, error) {
						req.Progress = func(phase string, done, total int64) {
							report(jobs.Progress{Phase: phase, Done: done, Total: total, Unit: "tracks"})
						}
						return service.AcquireRecording(ctx, req)
					})
				}
				req := acquisition.Request{SourceID: in.SourceID, File: in.File, Output: output, MaxBytes: in.MaxBytes, Resume: in.Resume, NoScan: in.NoScan}
				if req.MaxBytes == 0 {
					req.MaxBytes = acquisition.DefaultMaxBytes(in.File)
				}
				if err := req.Validate(); err != nil {
					return jobs.Job{}, err
				}
				return store.Start("acquire", req, func(ctx context.Context, report func(jobs.Progress)) (any, error) {
					req.Progress = func(phase string, done, total int64) {
						report(jobs.Progress{Phase: phase, Done: done, Total: total, Unit: "bytes"})
					}
					return service.Acquire(ctx, req)
				})
			})
	}
	addWrite(r, "library_rescan_start", "Start a rescan of an initialized library as a durable job: the limited checks run again and the installed antivirus scans each holding in review only because it lacks scan evidence, and the results are recorded. Stored files are never changed. Returns the job at once; read it with job_get.", false,
		func(ctx context.Context, in rescanInput) (jobs.Job, error) {
			lib, err := scopes.path(in.Library)
			if err != nil {
				return jobs.Job{}, err
			}
			req := library.RescanRequest{Library: lib, Apply: true}
			return store.Start("library_rescan", map[string]string{"library": lib}, func(ctx context.Context, report func(jobs.Progress)) (any, error) {
				report(jobs.Progress{Phase: "rescan"})
				return service.Rescan(ctx, req)
			})
		})
	add(r, "job_get", "Read one durable job: its kind, status (queued, running, succeeded, failed, cancelled, or interrupted), last progress, and, once finished, its result or error. An interrupted job's process ended before it finished; start it again, with resume for an acquisition.", false,
		func(ctx context.Context, in jobInput) (jobs.Job, error) { return store.Get(in.ID) })
	add(r, "job_list", "List up to 100 durable jobs, newest first, with their status.", false,
		func(ctx context.Context, _ emptyInput) ([]jobs.Job, error) { return store.List() })
	addWrite(r, "job_cancel", "Ask a job this server runs to stop. It stops at its next cancellation check and records cancelled; an interrupted acquisition keeps its packet for resume.", false,
		func(ctx context.Context, in jobInput) (jobs.Job, error) { return store.Cancel(in.ID) })
}
