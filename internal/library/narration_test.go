package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
)

// audioReceipt is an acquisition receipt for one audio packet from item,
// with a LibriVox link naming archiveItem back when project is set.
func audioReceipt(t *testing.T, body []byte, item, file, project, archiveItem string, recording map[string]any) []byte {
	t.Helper()
	sum := sha256.Sum256(body)
	request := map[string]any{"source_id": "archive:" + item, "source_file": file}
	if recording != nil {
		request["recording"] = recording
	}
	selection := map[string]any{}
	if project != "" {
		selection["librivox"] = map[string]any{"project_id": project, "status": "linked", "project": map[string]any{"id": project, "title": "Reading " + project, "archive_item": archiveItem}}
	}
	b, err := json.Marshal(map[string]any{
		"complete": true, "status": "untrusted_intake", "antivirus": "not_scanned",
		"transfer": map[string]any{"bytes": len(body), "sha256": hex.EncodeToString(sum[:])},
		"intent":   map[string]any{"request": request, "selection": selection},
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Recordings of one LibriVox reading in different formats share a narration;
// another reading, a link naming another item, and no link stay apart.
func TestRecordingsGroupByNarrationOnlyOnLinkedEvidence(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for position := 1; position <= 2; position++ {
		body := mp3Track(40 + position)
		dir := fmt.Sprintf("tracks/%04d", position)
		files[dir+"/content.mp3"] = body
		files[dir+"/receipt.json"] = audioReceipt(t, body, "demo", fmt.Sprintf("part_%02d.mp3", position), "7", "demo", map[string]any{"source_format": "64Kbps MP3", "position": position, "tracks": 2})
	}
	book := mp4Audiobook(30)
	files["book/content.m4b"] = book
	files["book/receipt.json"] = audioReceipt(t, book, "demo", "Demo_librivox.m4b", "7", "demo", nil)
	other := mp4Audiobook(31)
	files["other/content.m4b"] = other
	files["other/receipt.json"] = audioReceipt(t, other, "other", "Other_librivox.m4b", "8", "other", nil)
	elsewhere := mp4Audiobook(32)
	files["elsewhere/content.m4b"] = elsewhere
	files["elsewhere/receipt.json"] = audioReceipt(t, elsewhere, "third", "Third.m4b", "9", "not-third", nil)
	plain := mp4Audiobook(33)
	files["plain/content.m4b"] = plain
	files["plain/receipt.json"] = audioReceipt(t, plain, "plain", "Plain.m4b", "", "", nil)
	folder := t.TempDir()
	writeTree(t, folder, files)
	if r, err := Ingest(ctx, IngestRequest{Library: root, Folder: folder, Apply: true}); err != nil {
		t.Fatal(r, err)
	}
	r, err := Recordings(ctx, root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Recordings) != 5 {
		t.Fatal(len(r.Recordings), r.Recordings)
	}
	byFormat := map[string]RecordingManifest{}
	for _, m := range r.Recordings {
		byFormat[m.Format] = m
	}
	tracks, single := byFormat["64Kbps MP3"], byFormat["Demo_librivox.m4b"]
	if tracks.Kind != "tracks" || !tracks.Complete || tracks.Narration == nil || tracks.Narration.ID != "librivox:7" || tracks.Narration.Title != "Reading 7" {
		t.Fatalf("tracks %+v", tracks)
	}
	if single.Kind != "single_file" || !single.Complete || single.DeclaredTracks != 1 || single.Narration == nil || single.Narration.ID != "librivox:7" {
		t.Fatalf("single file %+v", single)
	}
	if byFormat["Third.m4b"].Narration != nil || byFormat["Plain.m4b"].Narration != nil {
		t.Fatal("a recording without linked-back evidence named a narration")
	}
	if len(r.Narrations) != 2 || r.Narrations[0].Narration.ID != "librivox:7" || len(r.Narrations[0].Recordings) != 2 || r.Narrations[1].Narration.ID != "librivox:8" || len(r.Narrations[1].Recordings) != 1 {
		t.Fatalf("narrations %+v", r.Narrations)
	}
	// With a source, a single-file recording is probed from its sample tables.
	probed, err := Recordings(ctx, root, "archive:demo")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range probed.Recordings {
		if m.Kind == "single_file" && (m.Tracks[0].Probe != "indexed" || m.ProbedMs == nil || *m.ProbedMs != m.Tracks[0].EncodedDurationMs || m.Tracks[0].EncodedDurationMs == 0) {
			t.Fatalf("probed single file %+v", m.Tracks[0])
		}
	}
}

// A recording whose tracks name different readings names none.
func TestATrackSetNamingTwoNarrationsIsAFinding(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for position, project := range map[int]string{1: "7", 2: "8"} {
		body := mp3Track(40 + position)
		dir := fmt.Sprintf("%04d", position)
		files[dir+"/content.mp3"] = body
		files[dir+"/receipt.json"] = audioReceipt(t, body, "demo", fmt.Sprintf("p%d.mp3", position), project, "demo", map[string]any{"source_format": "64Kbps MP3", "position": position, "tracks": 2})
	}
	folder := t.TempDir()
	writeTree(t, folder, files)
	if _, err := Ingest(ctx, IngestRequest{Library: root, Folder: folder, Apply: true}); err != nil {
		t.Fatal(err)
	}
	r, err := Recordings(ctx, root, "")
	if err != nil || len(r.Recordings) != 1 || r.Recordings[0].Narration != nil || len(r.Narrations) != 0 || len(r.Recordings[0].Findings) != 1 {
		t.Fatal(r, err)
	}
}

// Importing a packet again records a narration found since its first
// import, and an older source of the same bytes does not hide it.
func TestReimportAddsANarrationAndOldSourcesDoNotHideIt(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	book := mp4Audiobook(30)
	packet := t.TempDir()
	writeTree(t, packet, map[string][]byte{"content.m4b": book, "receipt.json": audioReceipt(t, book, "demo", "Demo.m4b", "", "", nil)})
	if _, err := Import(ctx, ImportRequest{Library: root, Source: packet, Apply: true}); err != nil {
		t.Fatal(err)
	}
	if r, _ := Recordings(ctx, root, ""); len(r.Recordings) != 1 || r.Recordings[0].Narration != nil {
		t.Fatal(r)
	}
	writeTree(t, packet, map[string][]byte{"receipt.json": audioReceipt(t, book, "demo", "Demo.m4b", "7", "demo", nil)})
	if _, err := Import(ctx, ImportRequest{Library: root, Source: packet, Apply: true}); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	writeTree(t, other, map[string][]byte{"content.m4b": book, "receipt.json": audioReceipt(t, book, "demo", "Demo.m4b", "", "", nil)})
	if _, err := Import(ctx, ImportRequest{Library: root, Source: other, Apply: true}); err != nil {
		t.Fatal(err)
	}
	r, err := Recordings(ctx, root, "")
	if err != nil || len(r.Recordings) != 1 || r.Recordings[0].Narration == nil || r.Recordings[0].Narration.ID != "librivox:7" || len(r.Recordings[0].Findings) != 0 {
		t.Fatal(r, err)
	}
}
