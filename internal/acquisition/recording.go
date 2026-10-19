package acquisition

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/safeio"
	"github.com/blisspixel/nemalo/internal/scanner"
	"github.com/blisspixel/nemalo/internal/transfer"
)

// RecordingPosition places one track packet in a declared recording. It is
// recorded in the track's intent and receipt so import can keep the order.
type RecordingPosition struct {
	Format   string `json:"source_format"`
	Position int    `json:"position"`
	Tracks   int    `json:"tracks"`
	// Title and DeclaredSeconds are the source's declarations for this track.
	Title           string  `json:"title,omitempty"`
	DeclaredSeconds float64 `json:"declared_seconds,omitempty"`
	// Readers are the readers a linked LibriVox project credits for this
	// track's section, when its sections matched the tracks.
	Readers []discovery.Reader `json:"readers,omitempty"`
}

// samePlace compares the place of a track, not its declared title or length.
func (p RecordingPosition) samePlace(o RecordingPosition) bool {
	return p.Format == o.Format && p.Position == o.Position && p.Tracks == o.Tracks
}

// RecordingRequest acquires every track of one declared recording, such as a
// LibriVox reading in one bitrate, into a new directory of track packets.
// MaxBytes bounds the declared total. Resume continues a directory this
// operation created earlier, keeping its completed track packets.
type RecordingRequest struct {
	SourceID    string             `json:"source_id"`
	Format      string             `json:"source_format"`
	Output      string             `json:"output"`
	MaxBytes    int64              `json:"max_bytes"`
	Resume      bool               `json:"resume,omitempty"`
	NoScan      bool               `json:"no_scan,omitempty"`
	OnDetection string             `json:"on_detection,omitempty"`
	Quarantine  string             `json:"quarantine,omitempty"`
	Scanner     assessment.Scanner `json:"-"`
}

// MaxRecordingBytes bounds one recording's declared total.
const MaxRecordingBytes = 16 << 30

func (r RecordingRequest) Validate() error {
	if !discovery.ValidArchiveID(r.SourceID) || r.Format == "" || len(r.Format) > 200 || r.Output == "" || r.MaxBytes < 1 || r.MaxBytes > MaxRecordingBytes {
		return fmt.Errorf("acquire --recording requires archive:ITEM, a declared recording format, an output directory, and a 1-%d byte budget", int64(MaxRecordingBytes))
	}
	return Request{SourceID: r.SourceID, File: "track.mp3", Output: r.Output, MaxBytes: 1, OnDetection: r.OnDetection, Quarantine: r.Quarantine}.Validate()
}

// RecordingTrack is the outcome for one track. Packet is the track packet's
// directory name inside the recording directory.
type RecordingTrack struct {
	Position  int    `json:"position"`
	File      string `json:"source_file"`
	Packet    string `json:"packet,omitempty"`
	Status    string `json:"status"`
	Bytes     int64  `json:"bytes,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	Antivirus string `json:"antivirus,omitempty"`
	Reused    bool   `json:"reused,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type RecordingResult struct {
	SchemaVersion int              `json:"schema_version"`
	Complete      bool             `json:"complete"`
	Status        string           `json:"status"`
	Output        string           `json:"output,omitempty"`
	SourceID      string           `json:"source_id"`
	Format        string           `json:"source_format"`
	OrderSource   string           `json:"order_source,omitempty"`
	DeclaredBytes int64            `json:"declared_bytes"`
	Tracks        []RecordingTrack `json:"tracks"`
	CompletedAt   *time.Time       `json:"completed_at,omitempty"`
	Limitations   []string         `json:"limitations"`
	Durability    string           `json:"durability"`
}

// recordingIntent is synced before any track is requested. Selection keeps the
// item evidence, the selected recording, and only its track files.
type recordingIntent struct {
	SchemaVersion int                  `json:"schema_version"`
	RequestedAt   time.Time            `json:"requested_at"`
	Request       RecordingRequest     `json:"request"`
	Selection     discovery.Evaluation `json:"selection"`
}

const (
	recordingIntentName  = "recording-intent.json"
	recordingReceiptName = "recording.json"
	maxTrackAttempts     = 9
	recordingBatch       = 32
	recordingBatchBytes  = 256 << 20
	maxRecordBytes       = 4 << 20
)

// AcquireRecording evaluates the item anew, selects the recording by its exact
// declared format, and acquires each track, in declared order, as an ordinary
// exclusive intake packet named by its position (0001, 0002, ...). Tracks are
// transferred in groups and each group is scanned in one batch when the
// scanner supports it. recording.json is written last, only when every track
// has a receipt. A detection stops the recording. Nothing is removed: a failed
// track packet is retained and a resumed run uses a new name (0003-2).
func AcquireRecording(ctx context.Context, provider Provider, request RecordingRequest) (result RecordingResult, err error) {
	result = RecordingResult{SchemaVersion: 1, Status: "not_started", SourceID: request.SourceID, Format: request.Format, Tracks: []RecordingTrack{}, Durability: "files_synced_directory_entries_filesystem_dependent", Limitations: []string{
		"Track order, titles, and durations are source declarations and are not checked against the audio.",
		"Each track is untrusted intake. A no-detection scan is not proof of safety, and rights are not resolved per file.",
		"A partial recording is retained for review. Resume keeps completed tracks; an interrupted track is retained and fetched again under a new name.",
		"A track kept by resume keeps the scan result recorded when it was acquired.",
	}}
	if err := request.Validate(); err != nil {
		return result, err
	}
	if request.Scanner != nil && !request.NoScan && request.Quarantine == "" && request.OnDetection != "delete" {
		return result, errors.New("scanning requires a quarantine directory for detected files")
	}
	abs, err := filepath.Abs(request.Output)
	if err != nil {
		return result, err
	}
	request.Output = abs
	parentPath, base := filepath.Dir(abs), filepath.Base(abs)
	parentMissing := false
	if _, statErr := os.Lstat(parentPath); errors.Is(statErr, os.ErrNotExist) {
		if request.Resume {
			return result, errors.New("resume requires the existing recording directory")
		}
		parentMissing = true
	} else if !request.Resume {
		if err := outputAvailable(parentPath, base); err != nil {
			return result, err
		}
	}
	evalCtx, cancel := context.WithTimeout(ctx, transferDeadline)
	evaluation, err := provider.Evaluate(evalCtx, request.SourceID)
	cancel()
	if err != nil {
		return result, err
	}
	recording, files, err := selectRecording(evaluation, request)
	if err != nil {
		return result, err
	}
	result.OrderSource, result.DeclaredBytes = recording.OrderSource, recording.Bytes
	if parentMissing {
		if err := os.MkdirAll(parentPath, 0o755); err != nil {
			return result, fmt.Errorf("create output parent: %w", err)
		}
	}
	selection := evaluation
	selection.Files, selection.Recordings = files, []discovery.Recording{recording}
	intent := recordingIntent{SchemaVersion: 1, RequestedAt: time.Now().UTC(), Request: request, Selection: selection}
	if data, err := json.MarshalIndent(intent, "", "  "); err != nil || len(data) > maxRecordBytes {
		return result, errors.New("the recording's declarations exceed the 4 MiB intent record; nothing was written")
	}
	parent, err := safeio.OpenResolvedRoot(parentPath)
	if err != nil {
		return result, err
	}
	defer parent.Close()
	if !request.Resume {
		if err := parent.Mkdir(base, 0o700); err != nil {
			if errors.Is(err, os.ErrExist) {
				return result, errors.New("output must be a new directory; use --resume to continue a recording")
			}
			return result, err
		}
	}
	root, err := safeio.OpenRootIn(parent, base)
	if err != nil {
		return result, err
	}
	defer root.Close()
	result.Output, result.Status = abs, "retained_incomplete"
	// Tracks never attempted report no packet name.
	defer func() {
		for i := range result.Tracks {
			if result.Tracks[i].Status == "pending" {
				result.Tracks[i].Packet = ""
			}
		}
	}()
	if request.Resume {
		// A pending record is an interrupted record write. It is neither
		// adopted nor removed, so the directory needs review first.
		for _, name := range []string{recordingIntentName + ".pending", recordingReceiptName + ".pending"} {
			if _, err := root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
				return result, fmt.Errorf("%s remains from an interrupted record write; review the recording directory before resuming", name)
			}
		}
		if err := sameRecordingIntent(root, request, selection); err != nil {
			return result, err
		}
		if _, err := root.Lstat(recordingReceiptName); !errors.Is(err, os.ErrNotExist) {
			return result, errors.New("this recording is already complete or its receipt needs review; nothing was requested")
		}
	} else if err := writeRecord(root, recordingIntentName, intent); err != nil {
		return result, err
	}

	// Plan every track: reuse a completed packet, or pick the next free name.
	type plannedTrack struct {
		track    RecordingTrack
		file     discovery.OfferedFile
		packet   string
		position RecordingPosition
		resume   bool
	}
	ranged, canResume := provider.(RangeProvider)
	plan, trackIndex := []plannedTrack{}, map[int]int{}
	for i, t := range recording.Tracks {
		rt := RecordingTrack{Position: t.Position, File: t.File}
		position := RecordingPosition{Format: recording.Format, Position: t.Position, Tracks: len(recording.Tracks), Title: t.Title, DeclaredSeconds: t.DeclaredSeconds, Readers: t.Readers}
		packet, reused, resume, findErr := trackPacket(ctx, root, request.SourceID, position, files[i], canResume)
		switch {
		case findErr != nil:
			rt.Status, rt.Detail = "failed", findErr.Error()
			result.Tracks = append(result.Tracks, rt)
			return result, findErr
		case reused != nil:
			rt.Packet, rt.Status, rt.Bytes, rt.SHA256, rt.Antivirus, rt.Reused = packet, "untrusted_intake", reused.Transfer.Bytes, reused.Transfer.SHA256, reused.Antivirus, true
		default:
			rt.Packet, rt.Status = packet, "pending"
			plan = append(plan, plannedTrack{rt, files[i], packet, position, resume})
		}
		trackIndex[t.Position] = len(result.Tracks)
		result.Tracks = append(result.Tracks, rt)
	}

	// The tracks still to fetch must fit before the first is requested.
	var remaining int64
	for _, p := range plan {
		remaining += *p.file.Bytes
	}
	if err := safeio.EnsureSpace(abs, remaining); err != nil {
		return result, err
	}
	scan := request.Scanner != nil && !request.NoScan
	// A batch-capable scanner scans a group of transferred tracks at once, so
	// signatures load once per group; receipts wait for that scan. Groups are
	// bounded by count and bytes, which also bounds the transfers an
	// interruption can cost. Without batching each track finishes at once.
	groupLimit := 1
	if scan && batchesScans(request.Scanner) {
		groupLimit = recordingBatch
	}
	for start := 0; start < len(plan); {
		end, groupBytes := start+1, *plan[start].file.Bytes
		for end < len(plan) && end-start < groupLimit && groupBytes+*plan[end].file.Bytes <= recordingBatchBytes {
			groupBytes += *plan[end].file.Bytes
			end++
		}
		group := plan[start:end]
		start = end
		states, results := make([]*packetState, len(group)), make([]Result, len(group))
		var groupErr error
		for i, p := range group {
			if err := ctx.Err(); err != nil {
				groupErr = err
				break
			}
			trackCtx, cancel := context.WithTimeout(ctx, transferBudget(*p.file.Bytes))
			req := Request{SourceID: request.SourceID, File: p.file.Name, Output: filepath.Join(abs, p.packet), MaxBytes: *p.file.Bytes, NoScan: request.NoScan, OnDetection: request.OnDetection, Quarantine: request.Quarantine, Scanner: request.Scanner,
				Recording: &p.position}
			initial := Result{SchemaVersion: 1, Status: "not_started", Antivirus: "not_scanned", Durability: "files_synced_directory_entries_filesystem_dependent"}
			var state *packetState
			var transferred Result
			transferErr := ErrResumeRefused
			if p.resume {
				// Continue the retained bytes of an interrupted attempt in place.
				packetRoot, err := safeio.OpenRootIn(root, p.packet)
				if err != nil {
					transferErr = err
				} else if state, transferred, transferErr = resumeTransfer(trackCtx, ranged, packetRoot, req.Output, evaluation, p.file, req, initial); transferErr != nil {
					_ = packetRoot.Close()
				}
			}
			if errors.Is(transferErr, ErrResumeRefused) {
				// Refused or not resumable: the attempt stays untouched and the
				// track is fetched whole under the next free name.
				name, _, _, err := trackPacket(trackCtx, root, request.SourceID, p.position, p.file, false)
				if err != nil {
					transferErr = err
				} else {
					result.Tracks[trackIndex[p.track.Position]].Packet, req.Output = name, filepath.Join(abs, name)
					state, transferred, transferErr = transferPacket(trackCtx, provider, root, req.Output, evaluation, p.file, req, initial)
				}
			}
			cancel()
			// A track that dropped mid-transfer continues in place, as a
			// single file does, before the recording stops.
			for attempt := 1; transferErr != nil && canResume && attempt <= maxContinuations && continuable(ctx, transferred, transferErr); attempt++ {
				select {
				case <-ctx.Done():
				case <-time.After(continuationPause(attempt)):
				}
				if ctx.Err() != nil {
					break
				}
				packetRoot, err := safeio.OpenRootIn(root, filepath.Base(req.Output))
				if err != nil {
					break
				}
				retryCtx, retryCancel := context.WithTimeout(ctx, transferBudget(*p.file.Bytes))
				var next Result
				state, next, transferErr = resumeTransfer(retryCtx, ranged, packetRoot, req.Output, evaluation, p.file, req, initial)
				retryCancel()
				if transferErr != nil {
					_ = packetRoot.Close()
				}
				if next.Transfer.Bytes == 0 {
					next.Transfer, next.TransferRecorded, next.StrongValidator = transferred.Transfer, transferred.TransferRecorded, transferred.StrongValidator
				}
				next.Continuations = transferred.Continuations + 1
				transferred = next
			}
			states[i], results[i] = state, transferred
			if transferErr != nil {
				t := &result.Tracks[trackIndex[p.track.Position]]
				t.Status, t.Detail = firstStatus(transferred.Status, "retained_incomplete"), transferErr.Error()
				groupErr = fmt.Errorf("track %d: %w", p.track.Position, transferErr)
				break
			}
		}
		scans := make([]*scanner.Result, len(group))
		if scan {
			paths, at := []string{}, []int{}
			for i, st := range states {
				if st != nil {
					paths, at = append(paths, results[i].ContentPath), append(at, i)
				}
			}
			if many, ok := request.Scanner.(assessment.BatchScanner); ok && len(paths) > 1 {
				batch := many.ScanMany(ctx, paths)
				for j, i := range at {
					// Results that cannot be attributed by position are incomplete.
					r := scanner.Result{Status: "incomplete", ExitCode: -1}
					if len(batch) == len(paths) {
						r = batch[j]
					}
					scans[i] = &r
				}
			} else {
				for _, i := range at {
					r := request.Scanner.Scan(ctx, results[i].ContentPath)
					scans[i] = &r
				}
			}
		}
		detection := false
		for i, st := range states {
			if st == nil {
				continue
			}
			t := &result.Tracks[trackIndex[group[i].track.Position]]
			if err := ctx.Err(); err != nil {
				// Cancelled before the receipt: the transferred packet is
				// retained without a receipt and fetched again on resume.
				st.close()
				t.Status, t.Detail = "retained_incomplete", "cancelled before the scan and receipt; retained for review"
				groupErr = errors.Join(groupErr, err)
				continue
			}
			r, finishErr := finishPacket(ctx, st, results[i], scans[i])
			t.Status, t.Bytes, t.SHA256, t.Antivirus = r.Status, r.Transfer.Bytes, r.Transfer.SHA256, r.Antivirus
			if finishErr != nil {
				t.Detail = finishErr.Error()
				detection = detection || errors.Is(finishErr, ErrDetected)
				groupErr = errors.Join(groupErr, fmt.Errorf("track %d: %w", t.Position, finishErr))
			}
		}
		if detection {
			result.Status = "stopped_on_detection"
			return result, errors.Join(ErrDetected, groupErr)
		}
		if groupErr != nil {
			return result, groupErr
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	now := time.Now().UTC()
	result.CompletedAt, result.Complete, result.Status = &now, true, "untrusted_intake"
	if err := writeRecord(root, recordingReceiptName, result); err != nil {
		result.Complete, result.Status, result.CompletedAt = false, "retained_incomplete", nil
		return result, err
	}
	return result, nil
}

func firstStatus(values ...string) string {
	for _, v := range values {
		if v != "" && v != "not_started" {
			return v
		}
	}
	return ""
}

// selectRecording finds the exact declared format in a fresh evaluation and
// returns it with its offered files in track order. Every track must be an
// eligible single-file selection, and the declared total must fit the budget.
func selectRecording(e discovery.Evaluation, request RecordingRequest) (discovery.Recording, []discovery.OfferedFile, error) {
	if !e.Complete || e.ID != request.SourceID || e.Source != "archive" || e.Access != "no_item_restriction_declared" {
		return discovery.Recording{}, nil, errors.New("source evaluation is incomplete, restricted, uncertain, or mismatched")
	}
	var found *discovery.Recording
	for i := range e.Recordings {
		if e.Recordings[i].Format != request.Format {
			continue
		}
		if found != nil {
			return discovery.Recording{}, nil, errors.New("source declares the recording format more than once")
		}
		found = &e.Recordings[i]
	}
	if found == nil {
		return discovery.Recording{}, nil, errors.New("the item declares no recording in that format; see recordings in evaluate")
	}
	r := *found
	if !r.Acquirable || len(r.Tracks) == 0 {
		return r, nil, errors.New("this recording has a track without public access, a usable size, or a checksum; see its findings")
	}
	if r.Bytes > request.MaxBytes {
		return r, nil, fmt.Errorf("the recording declares %d bytes, over the %d byte budget", r.Bytes, request.MaxBytes)
	}
	files := make([]discovery.OfferedFile, 0, len(r.Tracks))
	for i, t := range r.Tracks {
		if t.Position != i+1 {
			return r, nil, errors.New("recording track positions are not sequential")
		}
		f, err := selectFile(e, Request{SourceID: request.SourceID, File: t.File, MaxBytes: min(int64(256<<20), max(t.Bytes, 1))})
		if err != nil {
			return r, nil, fmt.Errorf("track %d: %w", t.Position, err)
		}
		if format(f.Name) != "mp3" {
			return r, nil, fmt.Errorf("track %d is not an MP3 file", t.Position)
		}
		files = append(files, f)
	}
	return r, files, nil
}

// sameRecordingIntent requires that a resumed directory was created by this
// operation for the same item and format, and that the fresh declarations
// still describe the same tracks: same files, sizes, and checksums in the same
// order. Anything else stops before any request.
func sameRecordingIntent(root *os.Root, request RecordingRequest, selection discovery.Evaluation) error {
	var intent recordingIntent
	if err := readRecord(root, recordingIntentName, &intent); err != nil {
		return fmt.Errorf("resume requires a recording directory created by acquire --recording: %w", err)
	}
	if intent.SchemaVersion != 1 || intent.Request.SourceID != request.SourceID || intent.Request.Format != request.Format {
		return errors.New("the recording directory was created for another item or format")
	}
	before, after := intent.Selection.Files, selection.Files
	if len(before) != len(after) {
		return errors.New("source declarations for this recording changed since it began; start a new output")
	}
	for i := range before {
		b, a := before[i], after[i]
		if b.Name != a.Name || b.Bytes == nil || a.Bytes == nil || *b.Bytes != *a.Bytes || b.MD5 != a.MD5 || b.SHA1 != a.SHA1 {
			return errors.New("source declarations for this recording changed since it began; start a new output")
		}
	}
	return nil
}

// trackPacket returns the directory name for a track: an existing packet whose
// receipt completes this exact file at this position, with content that still
// matches the fresh declared size and checksums, is reused; with canResume, an
// interrupted attempt for this track that can continue its bytes is resumed;
// otherwise the first unused attempt name. A detection receipt for this track
// stops the recording.
func trackPacket(ctx context.Context, root *os.Root, sourceID string, position RecordingPosition, file discovery.OfferedFile, canResume bool) (string, *Result, bool, error) {
	for attempt := 1; attempt <= maxTrackAttempts; attempt++ {
		name := fmt.Sprintf("%04d", position.Position)
		if attempt > 1 {
			name = fmt.Sprintf("%04d-%d", position.Position, attempt)
		}
		if _, err := root.Lstat(name); errors.Is(err, os.ErrNotExist) {
			return name, nil, false, nil
		} else if err != nil {
			return "", nil, false, err
		}
		packet, err := safeio.OpenRootIn(root, name)
		if err != nil {
			continue // Not a directory; preserved, and a new name is used.
		}
		var receipt Result
		readErr := readRecord(packet, "receipt.json", &receipt)
		sameTrack := readErr == nil && receipt.Intent != nil && receipt.Intent.Request.SourceID == sourceID && receipt.Intent.Request.File == file.Name
		if sameTrack && receipt.Antivirus == "findings" {
			_ = packet.Close()
			return name, nil, false, fmt.Errorf("track %d was detected by the scanner in %s; review it before resuming", position.Position, name)
		}
		placed := sameTrack && receipt.Intent.Request.Recording != nil && receipt.Intent.Request.Recording.samePlace(position)
		if placed && receipt.Complete && receipt.Status == "untrusted_intake" && verifyStored(ctx, packet, "content.mp3", file, receipt.Transfer) == nil {
			_ = packet.Close()
			return name, &receipt, false, nil
		}
		if canResume && errors.Is(readErr, os.ErrNotExist) && resumable(packet, file) {
			var intent Intent
			if readRecord(packet, "intent.json", &intent) == nil && intent.Request.SourceID == sourceID && intent.Request.Recording != nil && intent.Request.Recording.samePlace(position) {
				_ = packet.Close()
				return name, nil, true, nil
			}
		}
		_ = packet.Close()
	}
	return "", nil, false, fmt.Errorf("track %d has %d retained attempts; review them before resuming", position.Position, maxTrackAttempts)
}

// verifyStored rehashes a retained packet's content against the fresh source
// declaration (size and MD5/SHA-1) and against its own receipt's SHA-256, so an
// edited receipt cannot vouch for replaced bytes.
func verifyStored(ctx context.Context, root *os.Root, name string, file discovery.OfferedFile, transferred transfer.Result) error {
	if file.Bytes == nil || transferred.Bytes != *file.Bytes {
		return errors.New("retained content size differs from the declaration")
	}
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	f, err := safeio.OpenRegular(root, name, info, os.O_RDONLY)
	if err != nil {
		return err
	}
	defer f.Close()
	got, err := transfer.Copy(ctx, io.Discard, f, *file.Bytes, transfer.Expectation{Bytes: *file.Bytes, MD5: file.MD5, SHA1: file.SHA1})
	if err != nil || got.SHA256 != transferred.SHA256 {
		return errors.Join(err, errors.New("retained content does not match its declaration and receipt"))
	}
	return nil
}

// readRecord decodes a bounded record written by writeRecord.
func readRecord(root *os.Root, name string, value any) error {
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() < 2 || info.Size() > maxRecordBytes {
		return errors.New("record is not a bounded regular file")
	}
	f, err := safeio.OpenRegular(root, name, info, os.O_RDONLY)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, info.Size()+1))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

// batchesScans reports whether one ScanMany call scans several files at once.
// The installed adapter batches only ClamAV; Defender scans files in turn, so
// grouping tracks for it would only delay their receipts.
func batchesScans(s assessment.Scanner) bool {
	if _, ok := s.(assessment.BatchScanner); !ok {
		return false
	}
	if k, ok := s.(interface{ Kind() (string, error) }); ok {
		kind, err := k.Kind()
		return err == nil && kind == "clamav"
	}
	return true
}
