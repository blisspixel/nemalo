package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/blisspixel/nemalo/internal/acquisition"
	"github.com/blisspixel/nemalo/internal/jobs"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/pdftext/pdftest"
	"github.com/blisspixel/nemalo/internal/safeio"
	"github.com/blisspixel/nemalo/internal/textsafe"
)

// Match the canonical durable record shape, while constructing all state without
// opening a reconciling store or starting a job during passive-read fixtures.
type snapshotRecord struct {
	SchemaVersion int             `json:"schema_version"`
	Event         string          `json:"event"`
	At            string          `json:"at"`
	Kind          string          `json:"kind,omitempty"`
	Request       any             `json:"request,omitempty"`
	Context       any             `json:"context,omitempty"`
	Progress      *jobs.Progress  `json:"progress,omitempty"`
	Status        string          `json:"status,omitempty"`
	Result        json.RawMessage `json:"result,omitempty"`
	Error         string          `json:"error,omitempty"`
	Detail        string          `json:"detail,omitempty"`
}

func snapshotJournal(t *testing.T, dir string, n int, kind string, binding, request, result any, torn bool) string {
	t.Helper()
	at := "2026-01-01T00:00:00Z"
	records := []snapshotRecord{{SchemaVersion: 1, Event: jobs.Queued, At: at, Kind: kind, Request: request, Context: binding}, {SchemaVersion: 1, Event: jobs.Running, At: at}}
	if result != nil {
		b, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, snapshotRecord{SchemaVersion: 1, Event: "finished", At: at, Status: jobs.Succeeded, Result: b, Error: "private source error"})
	}
	var data []byte
	for _, r := range records {
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		data = append(append(data, b...), '\n')
	}
	if torn {
		data = append(data, []byte(`{"torn`)...)
	}
	id := fmt.Sprintf("%032x", n)
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return "job:" + id
}

func snapshotLibrary(t *testing.T) (string, JobLibraryContext) {
	t.Helper()
	root := t.TempDir()
	if _, err := library.Initialize(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	binding, err := (Service{}).JobContext(context.Background(), root)
	if err != nil || binding.Schema != "nemalo-job-library/1" {
		t.Fatal(binding, err)
	}
	return root, binding
}

func TestJobSnapshotScopesRecordedIdentityAndPreservesEveryFile(t *testing.T) {
	root, binding := snapshotLibrary(t)
	_, other := snapshotLibrary(t)
	if _, err := library.EnableBookmarks(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if _, err := library.DisableBookmarks(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	req := map[string]string{"library": root, "private_title": "unrelated secret"}
	first := snapshotJournal(t, dir, 1, "library_rescan", binding, req, nil, false)
	second := snapshotJournal(t, dir, 2, "library_rescan", nil, req, map[string]any{"schema_version": 1, "library_id": binding.LibraryID, "secret": "result private"}, true)
	snapshotJournal(t, dir, 3, "library_rescan", other, req, nil, false)
	snapshotJournal(t, dir, 4, "library_rescan", nil, req, nil, false) // A path alone does not bind identity.
	snapshotJournal(t, dir, 5, "library_rescan", map[string]string{"schema": "future/2", "library_id": binding.LibraryID}, req, map[string]any{"schema_version": 1, "library_id": binding.LibraryID}, false)
	snapshotJournal(t, dir, 6, "library_rescan", binding, req, map[string]any{"schema_version": 1, "library_id": other.LibraryID}, false)
	for _, suffix := range []string{".lock", ".cancel"} {
		if err := os.WriteFile(filepath.Join(dir, strings.TrimPrefix(first, "job:")+suffix), []byte("retained"), 0400); err != nil {
			t.Fatal(err)
		}
	}
	before, libraryBefore := citationTree(t, dir), citationTree(t, root)
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	for range 3 {
		out, err := (Service{Scanner: citationNoScan{t}}).JobSnapshot(context.Background(), JobSnapshotRequest{Library: root, JobsDirectory: dir})
		if err != nil || out.Status != "incomplete" || out.Schema != "nemalo-job-snapshot/1" || out.LibraryID != binding.LibraryID || len(out.Jobs) != 2 || out.Jobs[0].ID != first || out.Jobs[1].ID != second || out.Catalog != "observed" {
			t.Fatal(out, err)
		}
		if out.Jobs[0].RecordedStatus != jobs.Running || out.Jobs[0].Owner != "not_observed" || out.Jobs[0].Relationship != "declared_target" || out.Jobs[1].Relationship != "recorded_library_result" || !out.Jobs[1].ErrorRecorded || !out.Jobs[1].Expired {
			t.Fatal(out.Jobs)
		}
		data, err := textsafe.MarshalJSON(out, false)
		if err != nil || len(data) > MaxJobSnapshotBytes || bytes.Contains(data, []byte(root)) || bytes.Contains(data, []byte(other.LibraryID)) || bytes.Contains(data, []byte("private")) || bytes.Contains(data, []byte(`"request"`)) || bytes.Contains(data, []byte(`"result"`)) {
			t.Fatal("private job material escaped", string(data), err)
		}
	}
	if !reflect.DeepEqual(before, citationTree(t, dir)) || !reflect.DeepEqual(libraryBefore, citationTree(t, root)) {
		t.Fatal("repeated observation changed journals, control files or library")
	}
}

func TestJobSnapshotObservesDeclaredJobsWhileLibraryWriterIsBusy(t *testing.T) {
	root, binding := snapshotLibrary(t)
	dir := t.TempDir()
	id := snapshotJournal(t, dir, 1, "library_rescan", binding, map[string]string{"library": root}, nil, false)
	lock, err := os.OpenFile(filepath.Join(root, ".nemalo", "writer.lock"), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := safeio.TryLock(lock, true); err != nil {
		t.Fatal(err)
	}
	defer safeio.Unlock(lock)
	out, err := (Service{}).JobSnapshot(context.Background(), JobSnapshotRequest{Library: root, JobsDirectory: dir})
	if err != nil || out.Status != "incomplete" || out.Catalog != "busy" || out.Coverage != "identity_associations_only" || len(out.Jobs) != 1 || out.Jobs[0].ID != id || out.Jobs[0].Owner != "not_observed" {
		t.Fatal(out, err)
	}
	if _, err := (Service{}).JobContext(context.Background(), root); !errors.Is(err, library.ErrBusy) {
		t.Fatal("context start validation bypassed writer lock", err)
	}
}

func TestJobSnapshotMissingInvalidAndBoundedResults(t *testing.T) {
	root, binding := snapshotLibrary(t)
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	out, err := (Service{}).JobSnapshot(context.Background(), JobSnapshotRequest{Library: root, JobsDirectory: missing})
	if err != nil || out.Status != "observed" || out.Work.Store != "missing" || len(out.Jobs) != 0 {
		t.Fatal(out, err)
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing store created", err)
	}
	for i := 0; i < 101; i++ {
		snapshotJournal(t, dir, i, "acquire", binding, map[string]string{"output": "private"}, nil, false)
	}
	for _, limit := range []int{0, 1, 100} {
		out, err := (Service{}).JobSnapshot(context.Background(), JobSnapshotRequest{Library: root, JobsDirectory: dir, Limit: limit})
		want := limit
		if want == 0 {
			want = 20
		}
		if err != nil || out.Status != "incomplete" || !out.Truncated || len(out.Jobs) != want {
			t.Fatal(out, err)
		}
		for i, j := range out.Jobs {
			if j.ID != fmt.Sprintf("job:%032x", i) {
				t.Fatal("unstable ID tie-break", out.Jobs)
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for name, request := range map[string]JobSnapshotRequest{
		"bad_limit":       {Library: root, JobsDirectory: dir, Limit: -1},
		"large_limit":     {Library: root, JobsDirectory: dir, Limit: 101},
		"relative_jobs":   {Library: root, JobsDirectory: "relative"},
		"missing_library": {Library: missing, JobsDirectory: dir},
		"empty_library":   {JobsDirectory: dir},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := (Service{}).JobSnapshot(context.Background(), request)
			if err == nil || len(out.Jobs) != 0 || out.LibraryID != "" || out.Status != "unavailable" {
				t.Fatal("invalid request retained scoped output", out, err)
			}
		})
	}
	out, err = (Service{}).JobSnapshot(ctx, JobSnapshotRequest{Library: root, JobsDirectory: dir})
	if !errors.Is(err, context.Canceled) || out.Status != "cancelled" || len(out.Jobs) != 0 || out.LibraryID != "" {
		t.Fatal(out, err)
	}
}

func TestJobSnapshotLegacyAcquisitionRequiresExactHeldProvenance(t *testing.T) {
	root, _ := snapshotLibrary(t)
	dir := t.TempDir()
	packet := filepath.Join(t.TempDir(), "packet")
	provider := &shelf{bodies: map[string][]byte{"archive:one": pdftest.Pages("", "Synthetic packet.")}}
	service := Service{Providers: map[string]Searcher{"archive": provider}, Scanner: citationNoScan{t}}
	req := acquisition.Request{SourceID: "archive:one", File: "one.pdf", Output: packet, MaxBytes: 1 << 20, NoScan: true}
	result, err := service.Acquire(context.Background(), req)
	if err != nil || !result.Complete {
		t.Fatal(result, err)
	}
	id := snapshotJournal(t, dir, 1, "acquire", nil, req, result, false)
	out, err := service.JobSnapshot(context.Background(), JobSnapshotRequest{Library: root, JobsDirectory: dir})
	if err != nil || len(out.Jobs) != 0 {
		t.Fatal("unheld packet related to a library", out, err)
	}
	if _, err := service.Import(context.Background(), library.ImportRequest{Library: root, Source: packet, Apply: true}); err != nil {
		t.Fatal(err)
	}
	// Metadata evidence remains useful with the historical intake gone; passive
	// reads must never open that job-supplied path or its receipts.
	if err := os.RemoveAll(packet); err != nil {
		t.Fatal(err)
	}
	before := citationTree(t, root)
	out, err = service.JobSnapshot(context.Background(), JobSnapshotRequest{Library: root, JobsDirectory: dir})
	if err != nil || len(out.Jobs) != 1 || out.Jobs[0].ID != id || out.Jobs[0].Relationship != "held_packet_provenance" {
		t.Fatal(out, err)
	}
	if !reflect.DeepEqual(before, citationTree(t, root)) {
		t.Fatal("provenance observation changed library")
	}
}

func TestJobRelationshipKeepsRecordingPositionsAndPartialCoverageDistinct(t *testing.T) {
	packet := filepath.Join(t.TempDir(), "recording")
	hash := strings.Repeat("a", 64)
	holding := library.Holding{SHA256: hash, Bytes: 100, Sources: []library.Source{{Kind: "packet", Path: filepath.Join(packet, "part", "content.mp3"), SourceID: "archive:demo", SourceFile: "track.mp3", Recording: &library.SourceRecording{Format: "64Kbps MP3", Position: 1, Tracks: 2}}}}
	index := heldPacketIndex([]library.Holding{holding})
	req := acquisition.RecordingRequest{SourceID: "archive:demo", Format: "64Kbps MP3", Output: packet, MaxBytes: 1 << 20}
	r := acquisition.RecordingResult{SchemaVersion: 1, Output: packet, SourceID: req.SourceID, Format: req.Format, Tracks: []acquisition.RecordingTrack{{Position: 1, File: "track.mp3", Packet: "part", SHA256: hash, Bytes: 100}, {Position: 2, File: "later.mp3", Status: "pending"}}}
	encode := func(v any) json.RawMessage {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	for _, change := range []string{"", "source", "file", "packet", "hash", "bytes", "format", "position", "duplicate", "traversal"} {
		t.Run(change, func(t *testing.T) {
			result := r
			result.Tracks = append([]acquisition.RecordingTrack{}, r.Tracks...)
			switch change {
			case "source":
				result.SourceID = "archive:other"
			case "file":
				result.Tracks[0].File = "other.mp3"
			case "packet":
				result.Tracks[0].Packet = "other"
			case "hash":
				result.Tracks[0].SHA256 = strings.Repeat("b", 64)
			case "bytes":
				result.Tracks[0].Bytes++
			case "format":
				result.Format = "VBR MP3"
			case "position":
				result.Tracks[0].Position = 2
			case "duplicate":
				result.Tracks = append(result.Tracks, result.Tracks[0])
			case "traversal":
				result.Tracks[0].Packet = "../part"
			}
			j := jobs.JournalJob{Kind: "acquire_recording", Request: encode(req), Result: encode(result)}
			relation, count := jobRelationship(j, "selected", index)
			if change == "" {
				if relation != "held_recording_packets" || count != 1 {
					t.Fatal(relation, count)
				}
			} else if relation != "" || count != 0 {
				t.Fatal("inexact recording adopted", relation, count)
			}
		})
	}
	// An explicit context selects identity even without held tracks, but cannot
	// fall through to provenance if its version is unknown.
	j := jobs.JournalJob{Kind: "acquire_recording", Request: encode(req), Result: encode(r), Context: encode(map[string]string{"schema": "future/2", "library_id": "selected"})}
	if relation, _ := jobRelationship(j, "selected", index); relation != "" {
		t.Fatal("unknown context fell through")
	}
}

func TestJobContextRequiresInitializedExistingLibrary(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	root := t.TempDir()
	if _, err := (Service{}).JobContext(context.Background(), root); err == nil {
		t.Fatal("uninitialized context accepted")
	}
	if _, err := (Service{}).JobContext(ctx, root); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("shorter caller deadline was lost", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".nemalo")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("context initialized library", err)
	}
}
