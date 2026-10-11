package acquisition

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/scanner"
)

// recordingProvider serves a three-track recording with distinct bodies.
type recordingProvider struct {
	e         discovery.Evaluation
	bodies    map[string]string
	fail      map[string]error
	downloads []string
	before    func(string)
}

func recordingFixture() *recordingProvider {
	p := &recordingProvider{bodies: map[string]string{}, fail: map[string]error{}}
	p.e = discovery.Evaluation{Complete: true, ID: "archive:demo", Source: "archive", Access: "no_item_restriction_declared", Title: []string{"A reading"}}
	rec := discovery.Recording{Format: "64Kbps MP3", OrderSource: "declared_track", Acquirable: true, Findings: []string{}}
	for i := 1; i <= 3; i++ {
		name := fmt.Sprintf("part_%02d_64kb.mp3", i)
		body := fmt.Sprintf("ID3 track %d audio", i)
		p.bodies[name] = body
		n := int64(len(body))
		sum := md5.Sum([]byte(body))
		p.e.Files = append(p.e.Files, discovery.OfferedFile{Name: name, Bytes: &n, MD5: hex.EncodeToString(sum[:]), Access: "public_file_candidate", DownloadURL: "https://archive.org/download/demo/" + name})
		rec.Tracks = append(rec.Tracks, discovery.RecordingTrack{Position: i, DeclaredTrack: i, File: name, Bytes: n})
		rec.Bytes += n
	}
	p.e.Recordings = []discovery.Recording{rec}
	return p
}

func (p *recordingProvider) Evaluate(context.Context, string) (discovery.Evaluation, error) {
	return p.e, nil
}

func (p *recordingProvider) Download(_ context.Context, f discovery.OfferedFile) (*http.Response, error) {
	p.downloads = append(p.downloads, f.Name)
	if p.before != nil {
		p.before(f.Name)
	}
	if err := p.fail[f.Name]; err != nil {
		return nil, err
	}
	body := p.bodies[f.Name]
	return &http.Response{StatusCode: 200, ContentLength: int64(len(body)), Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

// batchCounter is a batch-capable scanner that records batch sizes.
type batchCounter struct {
	batches *[]int
	singles *int
	detect  string
}

func (b batchCounter) Scan(_ context.Context, path string) scanner.Result {
	*b.singles++
	return b.result(path)
}

func (b batchCounter) ScanMany(_ context.Context, paths []string) []scanner.Result {
	*b.batches = append(*b.batches, len(paths))
	out := make([]scanner.Result, len(paths))
	for i, p := range paths {
		out[i] = b.result(p)
	}
	return out
}

func (b batchCounter) result(path string) scanner.Result {
	data, _ := os.ReadFile(path)
	if b.detect != "" && string(data) == b.detect {
		return scanner.Result{Status: "findings", Tool: "test-scanner"}
	}
	return scanner.Result{Status: "no_detections_reported", Tool: "test-scanner"}
}

func recordingRequest(t *testing.T) RecordingRequest {
	t.Helper()
	return RecordingRequest{SourceID: "archive:demo", Format: "64Kbps MP3", Output: filepath.Join(t.TempDir(), "recording"), MaxBytes: 1 << 20}
}

func TestAcquireRecordingWritesOrderedTrackPacketsAndReceiptLast(t *testing.T) {
	p, r := recordingFixture(), recordingRequest(t)
	r.Quarantine = filepath.Join(t.TempDir(), "quarantine")
	var batches []int
	singles := 0
	r.Scanner = batchCounter{batches: &batches, singles: &singles}
	p.before = func(string) {
		if _, err := os.Stat(filepath.Join(r.Output, recordingIntentName)); err != nil {
			t.Error("a track was requested before the recording intent was synced")
		}
		if _, err := os.Stat(filepath.Join(r.Output, recordingReceiptName)); err == nil {
			t.Error("recording receipt written before every track")
		}
	}
	result, err := AcquireRecording(context.Background(), p, r)
	if err != nil || !result.Complete || result.Status != "untrusted_intake" || len(result.Tracks) != 3 {
		t.Fatal(result, err)
	}
	if len(batches) != 1 || batches[0] != 3 || singles != 0 {
		t.Fatal("tracks were not scanned in one batch", batches, singles)
	}
	for i, track := range result.Tracks {
		packet := filepath.Join(r.Output, fmt.Sprintf("%04d", i+1))
		receipt := readReceipt(t, packet)
		pos := receipt.Intent.Request.Recording
		if track.Packet != filepath.Base(packet) || receipt.Antivirus != "no_detections_reported" || pos == nil || pos.Position != i+1 || pos.Tracks != 3 || pos.Format != "64Kbps MP3" {
			t.Fatal("track packet", track, receipt.Intent.Request)
		}
		if got, _ := os.ReadFile(filepath.Join(packet, "content.mp3")); string(got) != p.bodies[track.File] {
			t.Fatal("track bytes", track)
		}
	}
	var saved RecordingResult
	b, err := os.ReadFile(filepath.Join(r.Output, recordingReceiptName))
	if err != nil || json.Unmarshal(b, &saved) != nil || !saved.Complete || len(saved.Tracks) != 3 {
		t.Fatal("recording receipt", string(b), err)
	}
	if _, err := AcquireRecording(context.Background(), p, r); err == nil {
		t.Fatal("existing output reused without resume")
	}
}

func TestAcquireRecordingResumesKeepingCompletedTracks(t *testing.T) {
	p, r := recordingFixture(), recordingRequest(t)
	p.fail["part_02_64kb.mp3"] = errors.New("connection reset")
	result, err := AcquireRecording(context.Background(), p, r)
	if err == nil || result.Complete || result.Tracks[0].Status != "untrusted_intake" || result.Tracks[1].Status != "retained_incomplete" || result.Tracks[2].Status != "pending" {
		t.Fatal("interrupted recording", result, err)
	}
	if _, err := os.Stat(filepath.Join(r.Output, recordingReceiptName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("partial recording has a receipt")
	}
	delete(p.fail, "part_02_64kb.mp3")
	p.downloads = nil
	r.Resume = true
	resumed, err := AcquireRecording(context.Background(), p, r)
	if err != nil || !resumed.Complete {
		t.Fatal(resumed, err)
	}
	if strings.Join(p.downloads, ",") != "part_02_64kb.mp3,part_03_64kb.mp3" {
		t.Fatal("completed track fetched again", p.downloads)
	}
	if !resumed.Tracks[0].Reused || resumed.Tracks[1].Packet != "0002-2" || resumed.Tracks[2].Packet != "0003" {
		t.Fatal("resume packets", resumed.Tracks)
	}
	// The failed attempt is retained, not removed or adopted.
	if _, err := os.Stat(filepath.Join(r.Output, "0002", "intent.json")); err != nil {
		t.Fatal("failed attempt removed", err)
	}
	if _, err := AcquireRecording(context.Background(), p, r); err == nil || !strings.Contains(err.Error(), "already complete") {
		t.Fatal("complete recording resumed again", err)
	}
}

func TestAcquireRecordingResumeRefusesChangedSourcesAndTamperedTracks(t *testing.T) {
	p, r := recordingFixture(), recordingRequest(t)
	p.fail["part_03_64kb.mp3"] = errors.New("timeout")
	if _, err := AcquireRecording(context.Background(), p, r); err == nil {
		t.Fatal("failure ignored")
	}
	delete(p.fail, "part_03_64kb.mp3")
	r.Resume = true

	changed := recordingFixture()
	changed.e.Files[1].MD5 = strings.Repeat("0", 32)
	if _, err := AcquireRecording(context.Background(), changed, r); err == nil || !strings.Contains(err.Error(), "changed") || len(changed.downloads) != 0 {
		t.Fatal("changed source declarations accepted", err, changed.downloads)
	}
	other := r
	other.Format = "VBR MP3"
	p.e.Recordings = append(p.e.Recordings, discovery.Recording{Format: "VBR MP3", Acquirable: true, OrderSource: "declared_track", Tracks: p.e.Recordings[0].Tracks, Bytes: p.e.Recordings[0].Bytes})
	if _, err := AcquireRecording(context.Background(), p, other); err == nil || !strings.Contains(err.Error(), "another item or format") {
		t.Fatal("directory resumed for another format", err)
	}
	p.e.Recordings = p.e.Recordings[:1]

	// A completed track whose content no longer matches its receipt is not
	// reused; it is retained and fetched again under a new name.
	if err := os.WriteFile(filepath.Join(r.Output, "0001", "content.mp3"), []byte("ID3 track X audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	p.downloads = nil
	resumed, err := AcquireRecording(context.Background(), p, r)
	if err != nil || !resumed.Complete || resumed.Tracks[0].Reused || resumed.Tracks[0].Packet != "0001-2" || !resumed.Tracks[1].Reused {
		t.Fatal("tampered track reused", resumed.Tracks, err)
	}
}

func TestAcquireRecordingStopsOnDetection(t *testing.T) {
	p, r := recordingFixture(), recordingRequest(t)
	r.Quarantine = filepath.Join(t.TempDir(), "quarantine")
	var batches []int
	singles := 0
	r.Scanner = batchCounter{batches: &batches, singles: &singles, detect: p.bodies["part_02_64kb.mp3"]}
	result, err := AcquireRecording(context.Background(), p, r)
	if !errors.Is(err, ErrDetected) || result.Complete || result.Status != "stopped_on_detection" || result.Tracks[1].Antivirus != "findings" {
		t.Fatal(result, err)
	}
	if _, err := os.Stat(filepath.Join(r.Output, recordingReceiptName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("recording with a detection has a receipt")
	}
	if _, err := os.Stat(filepath.Join(r.Output, "0002", "content.mp3")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("detected track left in its packet")
	}
	r.Resume = true
	if _, err := AcquireRecording(context.Background(), p, r); err == nil || !strings.Contains(err.Error(), "detected") {
		t.Fatal("resume ignored a detection", err)
	}
}

func TestAcquireRecordingRefusesBeforeWriting(t *testing.T) {
	for name, mutate := range map[string]func(*recordingProvider, *RecordingRequest){
		"budget":      func(_ *recordingProvider, r *RecordingRequest) { r.MaxBytes = 10 },
		"format":      func(_ *recordingProvider, r *RecordingRequest) { r.Format = "Ogg Vorbis" },
		"unusable":    func(p *recordingProvider, _ *RecordingRequest) { p.e.Recordings[0].Acquirable = false },
		"restricted":  func(p *recordingProvider, _ *RecordingRequest) { p.e.Access = "restricted_or_uncertain" },
		"no checksum": func(p *recordingProvider, _ *RecordingRequest) { p.e.Files[2].MD5 = "" },
		"duplicate": func(p *recordingProvider, _ *RecordingRequest) {
			p.e.Recordings = append(p.e.Recordings, p.e.Recordings[0])
		},
		"resume missing": func(_ *recordingProvider, r *RecordingRequest) { r.Resume = true },
	} {
		p, r := recordingFixture(), recordingRequest(t)
		mutate(p, &r)
		if _, err := AcquireRecording(context.Background(), p, r); err == nil || len(p.downloads) != 0 {
			t.Fatal(name, "accepted", err)
		}
		if _, err := os.Stat(r.Output); !errors.Is(err, os.ErrNotExist) {
			t.Fatal(name, "created output")
		}
	}
}

// Without batch scanning each track's receipt is written before the next
// track is requested, so an interruption costs at most the track in flight.
func TestAcquireRecordingFinishesEachTrackWithoutBatchScanning(t *testing.T) {
	p, r := recordingFixture(), recordingRequest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.before = func(name string) {
		if name == "part_02_64kb.mp3" {
			if _, err := os.Stat(filepath.Join(r.Output, "0001", "receipt.json")); err != nil {
				t.Error("track 1 had no receipt when track 2 was requested")
			}
			cancel()
		}
	}
	if _, err := AcquireRecording(ctx, p, r); err == nil {
		t.Fatal("interruption ignored")
	}
	p.before, p.downloads, r.Resume = nil, nil, true
	resumed, err := AcquireRecording(context.Background(), p, r)
	if err != nil || !resumed.Tracks[0].Reused || strings.Join(p.downloads, ",") != "part_02_64kb.mp3,part_03_64kb.mp3" {
		t.Fatal(resumed, err, p.downloads)
	}
}

// Regression: a record write interrupted after its pending file was created
// made every later resume fail at the final write.
func TestAcquireRecordingResumeRefusesPendingRecords(t *testing.T) {
	p, r := recordingFixture(), recordingRequest(t)
	p.fail["part_03_64kb.mp3"] = errors.New("timeout")
	if _, err := AcquireRecording(context.Background(), p, r); err == nil {
		t.Fatal("failure ignored")
	}
	delete(p.fail, "part_03_64kb.mp3")
	if err := os.WriteFile(filepath.Join(r.Output, recordingReceiptName+".pending"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	p.downloads, r.Resume = nil, true
	if _, err := AcquireRecording(context.Background(), p, r); err == nil || !strings.Contains(err.Error(), "interrupted record write") || len(p.downloads) != 0 {
		t.Fatal("pending record ignored", err, p.downloads)
	}
}

// Regression: resume trusted a receipt's own hash, so replaced bytes with an
// edited receipt were reused. Content must match the fresh declaration.
func TestAcquireRecordingResumeRejectsEditedReceipts(t *testing.T) {
	p, r := recordingFixture(), recordingRequest(t)
	p.fail["part_03_64kb.mp3"] = errors.New("timeout")
	if _, err := AcquireRecording(context.Background(), p, r); err == nil {
		t.Fatal("failure ignored")
	}
	delete(p.fail, "part_03_64kb.mp3")
	forged := []byte("ID3 track X audio")
	packet := filepath.Join(r.Output, "0001")
	if err := os.WriteFile(filepath.Join(packet, "content.mp3"), forged, 0o600); err != nil {
		t.Fatal(err)
	}
	receipt := readReceipt(t, packet)
	sum := sha256.Sum256(forged)
	receipt.Transfer.SHA256 = hex.EncodeToString(sum[:])
	data, _ := json.Marshal(receipt)
	if err := os.WriteFile(filepath.Join(packet, "receipt.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	r.Resume = true
	resumed, err := AcquireRecording(context.Background(), p, r)
	if err != nil || resumed.Tracks[0].Reused || resumed.Tracks[0].Packet != "0001-2" {
		t.Fatal("edited receipt vouched for replaced bytes", resumed.Tracks, err)
	}
}

// sequentialScanner advertises ScanMany, like the installed adapter, but
// reports a kind that scans files one at a time.
type sequentialScanner struct{ batchCounter }

func (sequentialScanner) Kind() (string, error) { return "defender", nil }

// Regression: Defender was grouped like ClamAV, so receipts waited for up to
// 32 transfers with no batching benefit.
func TestAcquireRecordingDoesNotGroupForSequentialScanners(t *testing.T) {
	p, r := recordingFixture(), recordingRequest(t)
	r.Quarantine = filepath.Join(t.TempDir(), "quarantine")
	var batches []int
	singles := 0
	r.Scanner = sequentialScanner{batchCounter{batches: &batches, singles: &singles}}
	p.before = func(name string) {
		if name == "part_02_64kb.mp3" {
			if _, err := os.Stat(filepath.Join(r.Output, "0001", "receipt.json")); err != nil {
				t.Error("track 1 receipt waited for later transfers")
			}
		}
	}
	result, err := AcquireRecording(context.Background(), p, r)
	if err != nil || !result.Complete || len(batches) != 0 || singles != 3 {
		t.Fatal(result, err, batches, singles)
	}
}

// shortBatch returns fewer results than files.
type shortBatch struct{ batchCounter }

func (s shortBatch) ScanMany(ctx context.Context, paths []string) []scanner.Result {
	return s.batchCounter.ScanMany(ctx, paths)[:len(paths)-1]
}

// Batch results that cannot be attributed by position are incomplete, never
// assigned to the wrong track.
func TestAcquireRecordingTreatsUnattributableBatchesAsIncomplete(t *testing.T) {
	p, r := recordingFixture(), recordingRequest(t)
	r.Quarantine = filepath.Join(t.TempDir(), "quarantine")
	var batches []int
	singles := 0
	r.Scanner = shortBatch{batchCounter{batches: &batches, singles: &singles}}
	result, err := AcquireRecording(context.Background(), p, r)
	if err != nil {
		t.Fatal(err)
	}
	for _, track := range result.Tracks {
		if track.Antivirus != "incomplete" {
			t.Fatal("unattributable result assigned", result.Tracks)
		}
	}
}

// cancelOnBatch cancels the run while a group is being scanned.
type cancelOnBatch struct {
	batchCounter
	cancel context.CancelFunc
}

func (c cancelOnBatch) ScanMany(ctx context.Context, paths []string) []scanner.Result {
	c.cancel()
	return c.batchCounter.ScanMany(ctx, paths)
}

func TestAcquireRecordingCancelledGroupKeepsNoReceipts(t *testing.T) {
	p, r := recordingFixture(), recordingRequest(t)
	r.Quarantine = filepath.Join(t.TempDir(), "quarantine")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var batches []int
	singles := 0
	r.Scanner = cancelOnBatch{batchCounter{batches: &batches, singles: &singles}, cancel}
	result, err := AcquireRecording(ctx, p, r)
	if err == nil || result.Complete {
		t.Fatal("cancellation ignored", result)
	}
	for i, track := range result.Tracks {
		if track.Status != "retained_incomplete" || !strings.Contains(track.Detail, "cancelled") {
			t.Fatal("cancelled track", track)
		}
		if _, err := os.Stat(filepath.Join(r.Output, fmt.Sprintf("%04d", i+1), "receipt.json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("receipt written after cancellation")
		}
	}
}
