package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blisspixel/nemalo/internal/library"
)

// mp3Frames returns n MPEG-1 Layer III frames at 128 kbps and 44.1 kHz.
func mp3Frames(n int) []byte {
	f := make([]byte, 417)
	copy(f, []byte{0xFF, 0xFB, 0x90, 0x40})
	return bytes.Repeat(f, n)
}

func audioFixture(t *testing.T, data []byte) Request {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "track.mp3"), data, 0600); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(data)
	hash := hex.EncodeToString(h[:])
	c := library.Catalog{SchemaVersion: 1, ID: "snapshot:" + strings.Repeat("2", 32), CreatedAt: time.Unix(1, 0).UTC(), RootHint: "unused", Complete: true, Assets: []library.Asset{{ID: "sha256:" + hash, SHA256: hash, Bytes: int64(len(data)), Locations: []library.Location{{Path: "track.mp3", CandidateKind: "audio_candidate"}}}}}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	catalog := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(catalog, b, 0600); err != nil {
		t.Fatal(err)
	}
	return Request{SchemaVersion: 1, Catalog: catalog, Root: root, AssetID: c.Assets[0].ID, MaxBytes: 4096, Representation: "mpeg-audio/1"}
}

func TestAudioManifestAndContiguousRanges(t *testing.T) {
	data := mp3Frames(40)
	req := audioFixture(t, data)
	req.UnitsOnly = true
	r, err := Get(context.Background(), req)
	if err != nil || r.Status != "units_available" || r.Audio == nil || r.Audio.Frames != 40 || r.Audio.EncodedSamples != 40*1152 || len(r.Units) != 1 || r.Units[0].Kind != "mpeg_audio_track" {
		t.Fatal(r, err)
	}
	req.UnitsOnly = false
	var delivered bytes.Buffer
	next, calls := int64(0), 0
	for {
		got, err := Get(context.Background(), req)
		if err != nil || got.AudioRange == nil || got.Resource == nil || got.Locator == nil || got.Locator.OffsetUnit != "encoded_samples" {
			t.Fatal(got, err)
		}
		a := got.AudioRange
		if a.SampleStart != next || int64(got.Locator.Start) != a.SampleStart || int64(got.Locator.End) != a.SampleEnd {
			t.Fatal("ranges do not tile the track", a, next)
		}
		if !bytes.Equal(got.Resource.Data, data[a.ByteStart:a.ByteEnd]) || len(got.Resource.Data) > req.MaxBytes {
			t.Fatal("delivered bytes are not the file range")
		}
		// Requested frames, after the preroll, are appended in order.
		delivered.Write(data[a.ByteEnd-int64(a.Frames*417) : a.ByteEnd])
		next, calls = a.SampleEnd, calls+1
		if got.EndOfSource {
			if got.Continuation != "" || got.Status != "complete_range" {
				t.Fatal(got.Status, got.Continuation)
			}
			break
		}
		if got.Status != "partial_range" || got.Continuation == "" || calls > 50 {
			t.Fatal(got.Status)
		}
		req.Cursor, req.Representation, req.Sample = got.Continuation, "", 0
	}
	if next != 40*1152 || !bytes.Equal(delivered.Bytes(), data) {
		t.Fatal("track not delivered exactly once", next, delivered.Len())
	}
}

func TestAudioRequestsFailExplicitly(t *testing.T) {
	data := mp3Frames(10)
	base := audioFixture(t, data)
	for _, tc := range []struct {
		mutate func(*Request)
		status string
	}{
		{func(r *Request) { r.Sample = 10 * 1152 }, "invalid_request"},
		{func(r *Request) { r.MaxBytes = 100 }, "invalid_request"},
		{func(r *Request) { r.Unit = 1 }, "invalid_request"},
		{func(r *Request) { r.Part = "id:x" }, "invalid_request"},
	} {
		req := base
		tc.mutate(&req)
		r, err := Get(context.Background(), req)
		if err == nil || r.Status != tc.status || r.Resource != nil || r.AudioRange != nil {
			t.Fatal("invalid audio request accepted", tc.status, r, err)
		}
	}
	// Mid-track, one 417-byte frame plus one preroll frame exceeds 500 bytes.
	small := base
	small.MaxBytes, small.Sample = 500, 5*1152
	if _, err := Get(context.Background(), small); err == nil || !strings.Contains(err.Error(), "at least") {
		t.Fatal("undersized range must name the minimum", err)
	}
	if err := (Request{SchemaVersion: 1, Catalog: "c", Root: "r", AssetID: base.AssetID, MaxBytes: 4096, Sample: 5}).Validate(); err == nil {
		t.Fatal("sample accepted outside mpeg-audio/1")
	}
	// An EPUB is not audio, and audio is not an EPUB.
	epub := fixture(t, []string{"<p>Text</p>"}, nil)
	epub.Representation = "mpeg-audio/1"
	if r, err := Get(context.Background(), epub); err == nil || r.Status != "unsupported_extraction" {
		t.Fatal(r, err)
	}
	text := base
	text.Representation = "epub-text/2"
	if r, err := Get(context.Background(), text); err == nil || r.Status != "unsupported_extraction" {
		t.Fatal(r, err)
	}
}

func TestAudioReferencesGoStaleWhenTheIndexChanges(t *testing.T) {
	req := audioFixture(t, mp3Frames(10))
	req.MaxBytes = 1000
	r, err := Get(context.Background(), req)
	if err != nil || r.Continuation == "" {
		t.Fatal(r, err)
	}
	c, err := parseCursor(r.Continuation)
	if err != nil {
		t.Fatal(err)
	}
	other := audioFixture(t, append(mp3Frames(10), mp3Frames(1)...))
	c.AssetID = other.AssetID
	other.Cursor, other.Representation = encodeCursor(c), ""
	if r, err := Get(context.Background(), other); err == nil || r.Status != "stale_reference" {
		t.Fatal("changed frame index accepted an old cursor", r, err)
	}
	// A text cursor cannot address audio.
	req.Cursor, req.Representation = encodeCursor(cursor{SchemaVersion: 1, AssetID: req.AssetID, Extractor: TextExtractorV2, Representation: "epub-text/2"}), "mpeg-audio/1"
	if r, err := Get(context.Background(), req); err == nil || r.Status != "stale_reference" {
		t.Fatal(r, err)
	}
	if !strings.Contains(strings.Join(Available().Unsupported, ","), "multi-file recording") {
		t.Fatal("capabilities overstate audio support")
	}
}
