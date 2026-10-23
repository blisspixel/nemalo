package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/audio/audiotest"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/library"
)

func TestMP4PacketCLIUsesSharedContentAndRejectsExplicitZeroElsewhere(t *testing.T) {
	root := t.TempDir()
	data := audiotest.MP4(4)
	path := filepath.Join(root, "track.m4b")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := library.Build(context.Background(), root, inventory.Defaults(), false)
	if err != nil {
		t.Fatal(err)
	}
	catalog := filepath.Join(t.TempDir(), "catalog.json")
	if err := library.Save(context.Background(), catalog, &snapshot); err != nil {
		t.Fatal(err)
	}
	asset := snapshot.Catalog.Assets[0].ID
	common := []string{catalog, "--root", root, "--asset", asset, "--representation", "mp4-audio/2", "--max-bytes", "200", "--json"}
	args := append([]string{"content", "read"}, common...)
	args = append(args, "--packet", "0")
	code, out, stderr := execute(t, args, nil)
	var envelope struct {
		Data  content.Result `json:"data"`
		Error string         `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || code != 0 || stderr != "" || envelope.Data.MP4Packets == nil {
		t.Fatal(code, out, stderr, err)
	}
	zero := 0
	direct, err := content.Get(context.Background(), content.Request{SchemaVersion: 1, Catalog: catalog, Root: root, AssetID: asset, MaxBytes: 200, Representation: "mp4-audio/2", Packet: &zero})
	want, _ := json.Marshal(direct)
	got, _ := json.Marshal(envelope.Data)
	if err != nil || !bytes.Equal(want, got) {
		t.Fatal("CLI differs from shared service", err, string(want), string(got))
	}
	for _, action := range []string{"units", "read"} {
		invalid := append([]string{"content", action}, common...)
		if action == "read" {
			invalid = append(invalid, "--representation", "mp4-audio/1")
		}
		invalid = append(invalid, "--packet", "0")
		if code, out, _ := execute(t, invalid, nil); code != 2 {
			t.Fatal("zero selector ignored", action, code, out)
		}
	}
	withCursor := append(append([]string{}, args...), "--cursor", direct.Continuation)
	if code, out, _ := execute(t, withCursor, nil); code != 2 {
		t.Fatal("cursor combined with zero packet", code, out)
	}
	human := append([]string{"content", "read", catalog, "--root", root, "--asset", asset, "--representation", "mp4-audio/2"}, "--packet", "3")
	if code, out, _ := execute(t, human, nil); code != 0 || !strings.Contains(out, "not a playable MP4 container") || strings.Contains(out, "WlpaWlpa") {
		t.Fatal("human packet output", code, out)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, data) {
		t.Fatal("source changed", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatal("content changed source tree", entries, err)
	}
}
