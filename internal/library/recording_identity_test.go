package library

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// This vector pins the public digest construction independently of file fixtures.
func TestRecordingByteIdentityDigestContract(t *testing.T) {
	asset := "sha256:" + strings.Repeat("0", 64)
	groups := recordingByteIdentities([]RecordingManifest{{Complete: true, Tracks: []RecordingEntry{{Position: 1, AssetID: asset}}}})
	const want = "recording-sha256:8825539a8913204d2467c41537d40b86c04835103d46f5513fbbcb1f1aef7b31"
	if len(groups) != 1 || groups[0].ID != want {
		t.Fatal(groups)
	}
}

// ingestIdentityTrack gives one held asset a source's declared position, without
// a LibriVox project. All bodies are synthetic and import does not run a scanner.
func ingestIdentityTrack(t *testing.T, root, item, format string, position, count, frames int) {
	t.Helper()
	body := mp3Track(frames)
	packet := t.TempDir()
	recording := map[string]any{"source_format": format, "position": position, "tracks": count}
	writeTree(t, packet, map[string][]byte{
		"content.mp3":  body,
		"receipt.json": audioReceipt(t, body, item, fmt.Sprintf("part-%d.mp3", position), "", "", recording),
	})
	if _, err := Import(context.Background(), ImportRequest{Library: root, Source: packet, Apply: true}); err != nil {
		t.Fatal(err)
	}
}

func TestRecordingByteIdentityRelatesSourcesWithoutNarrationEvidence(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	for _, item := range []string{"original", "mirror"} {
		for position := 1; position <= 2; position++ {
			ingestIdentityTrack(t, root, item, item+" format", position, 2, 40+position)
		}
	}
	r, err := Recordings(ctx, root, "")
	if err != nil || len(r.Recordings) != 2 || len(r.ByteIdentities) != 1 || len(r.Narrations) != 0 {
		t.Fatal(r, err)
	}
	g := r.ByteIdentities[0]
	if g.Schema != "recording-bytes/1" || len(g.Assets) != 2 || len(g.Recordings) != 2 || g.Recordings[0].SourceID != "archive:mirror" || g.Recordings[1].SourceID != "archive:original" {
		t.Fatalf("identity %+v", g)
	}
	for i, track := range r.Recordings[0].Tracks {
		if g.Assets[i] != track.AssetID {
			t.Fatal("asset order", g, r.Recordings[0])
		}
	}
	held, err := FindManaged(ctx, root, "", "all", "all", 10, 0)
	if err != nil || held.Total != 2 {
		t.Fatal("mirror bytes were stored again", held, err)
	}
	// A source filter narrows the references without changing the byte identity.
	filtered, err := Recordings(ctx, root, "archive:original")
	if err != nil || len(filtered.ByteIdentities) != 1 || filtered.ByteIdentities[0].ID != g.ID || len(filtered.ByteIdentities[0].Recordings) != 1 {
		t.Fatal(filtered, err)
	}
	// Journal-only listing remains useful when stored bytes are missing. It must
	// not imply a fresh check or hide the failure when probing is requested.
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(held.Holdings[0].Path))); err != nil {
		t.Fatal(err)
	}
	again, err := Recordings(ctx, root, "")
	if err != nil || !reflect.DeepEqual(r.ByteIdentities, again.ByteIdentities) {
		t.Fatal(again, err)
	}
	probed, err := Recordings(ctx, root, "archive:original")
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	for _, track := range probed.Recordings[0].Tracks {
		changed = changed || track.Probe == "stored_bytes_changed"
	}
	if !changed {
		t.Fatal("missing stored bytes were not disclosed", probed)
	}
}

func TestRecordingByteIdentityPreservesOrderMultiplicityAndUncertainty(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		item   string
		frames []int
		count  int
	}{
		{"original", []int{41, 42}, 2},
		{"reverse", []int{42, 41}, 2},
		{"changed", []int{41, 43}, 2},
		{"repeated", []int{41, 41}, 2},
		{"shorter", []int{41}, 1},
		{"missing", []int{41}, 2},
		{"conflict", []int{41, 42}, 2},
		{"counts", []int{41, 42}, 2},
	} {
		for i, frames := range test.frames {
			ingestIdentityTrack(t, root, test.item, "same title", i+1, test.count, frames)
		}
	}
	// A second asset claims one position; another recording disagrees on count.
	ingestIdentityTrack(t, root, "conflict", "same title", 1, 2, 44)
	ingestIdentityTrack(t, root, "counts", "same title", 1, 3, 41)
	// The same asset and position imported from another packet is provenance,
	// not another track or another identity.
	ingestIdentityTrack(t, root, "original", "same title", 1, 2, 41)
	r, err := Recordings(ctx, root, "")
	if err != nil || len(r.ByteIdentities) != 5 {
		t.Fatal(r, err)
	}
	for _, g := range r.ByteIdentities {
		if len(g.Recordings) != 1 {
			t.Fatal("distinct sequences grouped", g)
		}
		switch g.Recordings[0].SourceID {
		case "archive:missing", "archive:conflict", "archive:counts":
			t.Fatal("uncertain declaration given an identity", g)
		case "archive:repeated":
			if len(g.Assets) != 2 || g.Assets[0] != g.Assets[1] {
				t.Fatal("repeated positions lost", g)
			}
		}
	}
	// Stable ordering is part of the read contract, despite map traversal.
	for range 5 {
		again, err := Recordings(ctx, root, "")
		if err != nil || !reflect.DeepEqual(r, again) {
			t.Fatal("unstable report", again, err)
		}
	}
}

func TestRecordingByteIdentityKeepsContradictoryNarrationDeclarations(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	body := mp4Audiobook(30)
	for _, item := range []string{"first", "second"} {
		project := "7"
		if item == "second" {
			project = "8"
		}
		packet := t.TempDir()
		writeTree(t, packet, map[string][]byte{"content.m4b": body, "receipt.json": audioReceipt(t, body, item, item+".m4b", project, item, nil)})
		if _, err := Import(ctx, ImportRequest{Library: root, Source: packet, Apply: true}); err != nil {
			t.Fatal(err)
		}
	}
	r, err := Recordings(ctx, root, "")
	if err != nil || len(r.ByteIdentities) != 1 || len(r.ByteIdentities[0].Recordings) != 2 || len(r.Narrations) != 2 || r.Narrations[0].Narration.ID != "librivox:7" || r.Narrations[1].Narration.ID != "librivox:8" {
		t.Fatal("byte equality overrode narration evidence", r, err)
	}
}
