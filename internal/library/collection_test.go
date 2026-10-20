package library

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A collection file is refused unless every item is an exact, valid
// selection, listed once, with no unknown field.
func TestCollectionFilesAreExactSelections(t *testing.T) {
	dir := t.TempDir()
	read := func(body string) error {
		p := filepath.Join(dir, "c.json")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		_, _, err := ReadCollection(p)
		return err
	}
	good := `{"schema_version":1,"name":"Books","items":[{"source":"archive:x","file":"x.epub"},{"source":"archive:y","recording":"64Kbps MP3"},{"source":"gutenberg:1342","file":"1342.epub.noimages"},{"source":"arxiv:1706.03762v7","file":"1706.03762v7.pdf"}]}`
	if err := read(good); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"unknown field":     `{"schema_version":1,"name":"B","items":[{"source":"archive:x","file":"x.epub","url":"https://x"}]}`,
		"both":              `{"schema_version":1,"name":"B","items":[{"source":"archive:x","file":"x.epub","recording":"MP3"}]}`,
		"neither":           `{"schema_version":1,"name":"B","items":[{"source":"archive:x"}]}`,
		"duplicate":         `{"schema_version":1,"name":"B","items":[{"source":"archive:x","file":"x.epub"},{"source":"archive:x","file":"x.epub"}]}`,
		"path":              `{"schema_version":1,"name":"B","items":[{"source":"archive:x","file":"../x.epub"}]}`,
		"gutenberg name":    `{"schema_version":1,"name":"B","items":[{"source":"gutenberg:1342","file":"pg1342.epub"}]}`,
		"unversioned arxiv": `{"schema_version":1,"name":"B","items":[{"source":"arxiv:1706.03762","file":"1706.03762v7.pdf"}]}`,
		"gutenberg audio":   `{"schema_version":1,"name":"B","items":[{"source":"gutenberg:1342","recording":"MP3"}]}`,
		"other source":      `{"schema_version":1,"name":"B","items":[{"source":"openlibrary:OL1M","file":"x.epub"}]}`,
		"schema":            `{"schema_version":2,"name":"B","items":[{"source":"archive:x","file":"x.epub"}]}`,
		"empty":             `{"schema_version":1,"name":"B","items":[]}`,
		"two values":        good + good,
		"trailing bracket":  good + "]",
		"trailing brace":    good + "}",
		"duplicate key":     `{"schema_version":1,"name":"B","name":"C","items":[{"source":"archive:x","file":"x.epub"}]}`,
		"duplicate in item": `{"schema_version":1,"name":"B","items":[{"source":"archive:x","file":"x.epub","file":"y.epub"}]}`,
	} {
		if err := read(body); err == nil {
			t.Error(name, "accepted")
		}
	}
	if err := read(`{"schema_version":1,"name":"B","items":[{"source":"archive:x","file":"x.epub","note":"` + strings.Repeat("n", 501) + `"}]}`); err == nil {
		t.Error("a long note was accepted")
	}
	a, b := CollectionItem{Source: "archive:x", File: "x.epub"}, CollectionItem{Source: "archive:x", File: "y.epub"}
	if a.Packet() == b.Packet() || !strings.HasPrefix(a.Packet(), "archive-x-") || (CollectionItem{Source: "arxiv:hep-th/9901001v1", File: "hep-th_9901001v1.pdf"}).Packet()[:15] != "arxiv-hep-th-99" {
		t.Fatal(a.Packet(), b.Packet())
	}
}

// A recording item is held only when every declared track position of that
// format is held.
func TestCollectionRecordingNeedsEveryTrack(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	collection := filepath.Join(t.TempDir(), "c.json")
	if err := os.WriteFile(collection, []byte(`{"schema_version":1,"name":"Listen","items":[{"source":"archive:demo","recording":"64Kbps MP3"},{"source":"archive:demo","file":"Demo.m4b"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ingest := func(position int) {
		body := mp3Track(60 + position)
		dir := fmt.Sprintf("tracks/%04d", position)
		folder := t.TempDir()
		writeTree(t, folder, map[string][]byte{dir + "/content.mp3": body, dir + "/receipt.json": audioReceipt(t, body, "demo", fmt.Sprintf("part_%02d.mp3", position), "", "", map[string]any{"source_format": "64Kbps MP3", "position": position, "tracks": 2})})
		if r, err := Ingest(ctx, IngestRequest{Library: root, Folder: folder, Apply: true}); err != nil {
			t.Fatal(r, err)
		}
	}
	ingest(1)
	r, err := CollectionStatus(ctx, root, collection)
	if err != nil || r.Items[0].Status != CollectionIncomplete || r.Items[0].HeldTracks != 1 || r.Items[0].DeclaredTracks != 2 || r.Items[1].Status != CollectionNotHeld {
		t.Fatalf("%+v %v", r.Items, err)
	}
	ingest(2)
	if r, err = CollectionStatus(ctx, root, collection); err != nil || r.Items[0].Status == CollectionIncomplete || r.Items[0].Status == CollectionNotHeld || len(r.Items[0].Holdings) != 2 {
		t.Fatalf("%+v %v", r.Items, err)
	}
	// Two byte-identical tracks are one holding at two positions; the
	// recording is still held.
	same := t.TempDir()
	if _, err := Initialize(ctx, same); err != nil {
		t.Fatal(err)
	}
	body := mp3Track(70)
	folder := t.TempDir()
	tree := map[string][]byte{}
	for position := 1; position <= 2; position++ {
		dir := fmt.Sprintf("tracks/%04d", position)
		tree[dir+"/content.mp3"] = body
		tree[dir+"/receipt.json"] = audioReceipt(t, body, "demo", fmt.Sprintf("part_%02d.mp3", position), "", "", map[string]any{"source_format": "64Kbps MP3", "position": position, "tracks": 2})
	}
	writeTree(t, folder, tree)
	if r, err := Ingest(ctx, IngestRequest{Library: same, Folder: folder, Apply: true}); err != nil {
		t.Fatal(r, err)
	}
	if r, err := CollectionStatus(ctx, same, collection); err != nil || r.Items[0].HeldTracks != 2 || r.Items[0].Status == CollectionIncomplete || len(r.Items[0].Holdings) != 1 {
		t.Fatalf("%+v %v", r.Items, err)
	}
	if _, err := CollectionStatus(ctx, t.TempDir(), collection); err == nil {
		t.Fatal("an uninitialized library was accepted")
	}
}

func TestCollectionRecordingRetainsConflictingPositionClaims(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	collection := filepath.Join(t.TempDir(), "c.json")
	if err := os.WriteFile(collection, []byte(`{"schema_version":1,"name":"Listen","items":[{"source":"archive:demo","recording":"MP3"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	ingestIdentityTrack(t, root, "demo", "MP3", 1, 2, 41)
	ingestIdentityTrack(t, root, "demo", "MP3", 2, 2, 42)
	// A second packet for the same asset and position is just provenance.
	ingestIdentityTrack(t, root, "demo", "MP3", 1, 2, 41)
	r, err := CollectionStatus(ctx, root, collection)
	if err != nil || r.Incomplete != 0 || len(r.Items[0].Holdings) != 2 {
		t.Fatal(r, err)
	}
	// Different bytes claiming an occupied position are an unresolved conflict,
	// even though every declared position has at least one held asset.
	ingestIdentityTrack(t, root, "demo", "MP3", 1, 2, 43)
	r, err = CollectionStatus(ctx, root, collection)
	if err != nil || r.Incomplete != 1 || r.Items[0].Status != CollectionIncomplete || r.Items[0].HeldTracks != 2 || r.Items[0].DeclaredTracks != 2 || len(r.Items[0].Holdings) != 3 || !strings.Contains(r.Items[0].Detail, "conflicting") {
		t.Fatal("conflicting recording was treated as resolved", r, err)
	}
	m := mustRecording(t, root)
	if m.Complete || len(m.Tracks) != 3 {
		t.Fatal("collection and recording manifest disagree", m)
	}
}
