package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// trackReceipt is the receipt acquire --recording writes for one track packet.
func trackReceipt(t *testing.T, body []byte, position, tracks int) []byte {
	t.Helper()
	sum := sha256.Sum256(body)
	receipt, err := json.Marshal(map[string]any{
		"schema_version": 1, "complete": true, "status": "untrusted_intake", "antivirus": "not_scanned",
		"transfer": map[string]any{"bytes": len(body), "sha256": hex.EncodeToString(sum[:])},
		"intent": map[string]any{"request": map[string]any{"source_id": "archive:demo", "source_file": fmt.Sprintf("part_%02d_64kb.mp3", position),
			"recording": map[string]any{"source_format": "64Kbps MP3", "position": position, "tracks": tracks}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

// A recording directory imports each completed track packet with its declared
// place in the recording, and never imports a failed attempt's partial bytes.
func TestIngestRecordsRecordingPositionsAndSkipsFailedAttempts(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	folder := t.TempDir()
	tracks := map[string][]byte{}
	for i, packet := range []string{"0001", "0002-2", "0003"} {
		body := []byte(fmt.Sprintf("ID3\x04\x00\x00\x00\x00\x00\x00track %d audio", i+1))
		tracks[packet+"/content.mp3"] = body
		tracks[packet+"/intent.json"] = []byte(`{"schema_version":1}`)
		tracks[packet+"/receipt.json"] = trackReceipt(t, body, i+1, 3)
	}
	tracks["0002/intent.json"] = []byte(`{"schema_version":1}`)
	tracks["0002/content.mp3"] = []byte("ID3 partial")
	tracks["recording-intent.json"] = []byte(`{"schema_version":1}`)
	tracks["recording.json"] = []byte(`{"schema_version":1,"complete":true}`)
	writeTree(t, folder, tracks)
	r, err := Ingest(ctx, IngestRequest{Library: root, Folder: folder, Apply: true})
	if err != nil {
		t.Fatal(r, err)
	}
	outcomes := map[string]string{}
	for _, it := range r.Items {
		outcomes[it.Source] = it.Outcome
	}
	for source, want := range map[string]string{
		"0001": "review", "0002-2": "review", "0003": "review",
		"0001/intent.json": "packet_part", "recording.json": "packet_part", "recording-intent.json": "packet_part",
		"0002/content.mp3": "not_imported", "0002/intent.json": "not_imported",
	} {
		if outcomes[source] != want {
			t.Fatalf("%s: %q, want %q (%v)", source, outcomes[source], want, outcomes)
		}
	}
	if r.Imported != 3 || r.NotImported != 2 {
		t.Fatal("counts", r.Imported, r.NotImported)
	}
	page, err := FindManaged(ctx, root, "", "mp3", "all", 10, 0)
	if err != nil || page.Total != 3 {
		t.Fatal(page, err)
	}
	positions := map[int]bool{}
	for _, h := range page.Holdings {
		rec := h.Sources[0].Recording
		if rec == nil || rec.Format != "64Kbps MP3" || rec.Tracks != 3 || h.Sources[0].SourceID != "archive:demo" {
			t.Fatal("recording position not recorded", h.Sources)
		}
		positions[rec.Position] = true
	}
	if len(positions) != 3 {
		t.Fatal("positions", positions)
	}
	for _, it := range r.Items {
		if strings.HasPrefix(it.Source, "0002/") && it.Import != nil {
			t.Fatal("partial bytes imported", it)
		}
	}
}

func TestRecordingPositionsAreValidatedInHoldings(t *testing.T) {
	for _, bad := range []*SourceRecording{{Format: "", Position: 1, Tracks: 1}, {Format: "x", Position: 0, Tracks: 1}, {Format: "x", Position: 2, Tracks: 1}, {Format: "x", Position: 1, Tracks: 1001}, {Format: "a\nb", Position: 1, Tracks: 1}} {
		if validRecording(bad) {
			t.Fatal("accepted", bad)
		}
	}
	if !validRecording(nil) || !validRecording(&SourceRecording{Format: "VBR MP3", Position: 3, Tracks: 3}) {
		t.Fatal("rejected a valid position")
	}
}

// mp3Track returns n MPEG-1 Layer III frames at 128 kbps and 44.1 kHz, each
// 1152 samples, so n frames last n*1152/44100 seconds.
func mp3Track(n int) []byte {
	f := make([]byte, 417)
	copy(f, []byte{0xFF, 0xFB, 0x90, 0x40})
	return bytes.Repeat(f, n)
}

// ingestTracks writes one track packet per position into a new folder and
// ingests it. frames maps a position to its frame count.
func ingestTracks(t *testing.T, root string, tracks int, frames map[int]int) {
	t.Helper()
	folder := t.TempDir()
	files := map[string][]byte{}
	for position, n := range frames {
		body := mp3Track(n)
		dir := fmt.Sprintf("%04d", position)
		files[dir+"/content.mp3"] = body
		files[dir+"/receipt.json"] = trackReceipt(t, body, position, tracks)
	}
	writeTree(t, folder, files)
	if r, err := Ingest(context.Background(), IngestRequest{Library: root, Folder: folder, Apply: true}); err != nil {
		t.Fatal(r, err)
	}
}

func TestRecordingManifestOrdersTracksAndProbesStoredBytes(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	ingestTracks(t, root, 3, map[int]int{1: 100, 2: 50, 3: 25})
	listed, err := Recordings(ctx, root, "")
	if err != nil || len(listed.Recordings) != 1 {
		t.Fatal(listed, err)
	}
	m := listed.Recordings[0]
	if m.Schema != RecordingManifestVersion || !m.Complete || m.Probed || m.HeldTracks != 3 || len(m.Missing) != 0 || m.Tracks[0].Probe != "not_requested" {
		t.Fatal("journal-only manifest", m)
	}
	probed, err := Recordings(ctx, root, "archive:demo")
	if err != nil {
		t.Fatal(err)
	}
	m = probed.Recordings[0]
	ms := func(frames int) int64 { return int64(frames) * 1152 * 1000 / 44100 }
	if !m.Probed || m.ProbedMs == nil || *m.ProbedMs != m.Tracks[0].EncodedDurationMs+m.Tracks[1].EncodedDurationMs+m.Tracks[2].EncodedDurationMs {
		t.Fatal("probed total", m)
	}
	for i, frames := range []int{100, 50, 25} {
		track := m.Tracks[i]
		if track.Position != i+1 || track.Probe != "indexed" || track.SampleRate != 44100 || track.EncodedSamples != int64(frames*1152) || track.StartMs == nil {
			t.Fatal("track", track)
		}
		if diff := track.EncodedDurationMs - ms(frames); diff < -1 || diff > 1 {
			t.Fatal("duration", track.EncodedDurationMs, ms(frames))
		}
	}
	if *m.Tracks[2].StartMs != m.Tracks[0].EncodedDurationMs+m.Tracks[1].EncodedDurationMs {
		t.Fatal("offsets", m.Tracks)
	}
	if other, err := Recordings(ctx, root, "archive:other"); err != nil || len(other.Recordings) != 0 {
		t.Fatal("source filter", other, err)
	}
}

func TestRecordingManifestReportsGapsConflictsAndChangedBytes(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	ingestTracks(t, root, 4, map[int]int{1: 40, 3: 30, 4: 20})
	m := mustRecording(t, root)
	if m.Complete || len(m.Missing) != 1 || m.Missing[0] != 2 || m.HeldTracks != 3 || m.ProbedMs != nil {
		t.Fatal("missing track", m)
	}
	if m.Tracks[0].StartMs == nil || m.Tracks[1].StartMs != nil {
		t.Fatal("offsets must stop at a gap", m.Tracks)
	}
	// A different asset claims position 3.
	ingestTracks(t, root, 4, map[int]int{2: 10, 3: 31})
	m = mustRecording(t, root)
	if m.Complete || len(m.Missing) != 0 || len(m.Findings) != 1 || !strings.Contains(m.Findings[0], "position 3") {
		t.Fatal("conflict", m)
	}
	// Stored bytes that no longer match their holding are not indexed.
	stored := filepath.Join(root, filepath.FromSlash(holdingPath(t, root, m.Tracks[0].AssetID)))
	if err := os.Chmod(stored, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stored, mp3Track(41), 0o644); err != nil {
		t.Fatal(err)
	}
	m = mustRecording(t, root)
	if m.Tracks[0].Probe != "stored_bytes_changed" || m.Tracks[0].StartMs != nil {
		t.Fatal("changed bytes indexed", m.Tracks[0])
	}
}

func mustRecording(t *testing.T, root string) RecordingManifest {
	t.Helper()
	r, err := Recordings(context.Background(), root, "archive:demo")
	if err != nil || len(r.Recordings) != 1 {
		t.Fatal(r, err)
	}
	return r.Recordings[0]
}

func holdingPath(t *testing.T, root, assetID string) string {
	t.Helper()
	page, err := FindManaged(context.Background(), root, assetID, "all", "all", 1, 0)
	if err != nil || page.Total != 1 {
		t.Fatal(page, err)
	}
	return page.Holdings[0].Path
}
