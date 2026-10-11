package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// trackReceipt is the receipt acquire --recording writes for one track packet.
func trackReceipt(t *testing.T, body []byte, position, tracks int) []byte {
	t.Helper()
	sum := sha256.Sum256(body)
	receipt, err := json.Marshal(map[string]any{
		"complete": true, "status": "untrusted_intake", "antivirus": "not_scanned",
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
