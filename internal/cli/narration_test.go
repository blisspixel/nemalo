package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/config"
	"github.com/blisspixel/nemalo/internal/library"
)

func TestNarrationCLIJSONPreviewApplyReadAndWithdraw(t *testing.T) {
	ctx := context.Background()
	root, state := t.TempDir(), t.TempDir()
	service := app.Service{Providers: map[string]app.Searcher{}, Scanner: fakeScanner("unavailable")}
	run := func(args ...string) (int, string) {
		t.Helper()
		var out, stderr bytes.Buffer
		code := Execute(ctx, args, &out, &stderr, service, func(string) string { return "" }, func() (config.Paths, error) {
			return config.Paths{Config: filepath.Join(state, "none"), State: state}, nil
		}, nil)
		return code, out.String() + stderr.String()
	}
	if _, err := library.Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	for i, item := range []string{"first", "second"} {
		body := []byte(fmt.Sprintf("ID3 synthetic track %d", i))
		packet := t.TempDir()
		receipt := fmt.Sprintf(`{"schema_version":1,"complete":true,"status":"untrusted_intake","transfer":{"bytes":%d,"sha256":"%x"},"intent":{"request":{"source_id":"archive:%s","source_file":"track.mp3","recording":{"source_format":"MP3","position":1,"tracks":1}}}}`, len(body), sha256.Sum256(body), item)
		for name, data := range map[string][]byte{"content.mp3": body, "receipt.json": []byte(receipt)} {
			if err := os.WriteFile(filepath.Join(packet, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := service.Import(ctx, library.ImportRequest{Library: root, Source: packet, Apply: true}); err != nil {
			t.Fatal(err)
		}
	}
	code, out := run("library", "recordings", root, "--json")
	var listed struct {
		Data library.RecordingsResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil || code != 0 || len(listed.Data.ByteIdentities) != 2 {
		t.Fatal(out, err)
	}
	d := library.NarrationDeclaration{Schema: library.NarrationDeclarationSchema, NarrationID: "user-narration:reading-one", Recordings: []library.NarrationSelection{}}
	for _, identity := range listed.Data.ByteIdentities {
		ref := identity.Recordings[0]
		d.Recordings = append(d.Recordings, library.NarrationSelection{SourceID: ref.SourceID, Format: ref.Format, Kind: ref.Kind, ByteIdentity: identity.ID})
	}
	file := filepath.Join(t.TempDir(), "declaration.json")
	body, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, body, 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"library", "narration", root}, {"library", "narration", root, "--declaration", file, "--apply"}, {"library", "narration", root, "--declaration", file, "--plan", strings.Repeat("a", 64)}, {"library", "narration", root, "--declaration", file, "--scan"}, {"library", "narration", root, "--declaration", file, "--retract", "x"}} {
		if code, out := run(args...); code != 2 {
			t.Fatal(args, code, out)
		}
	}
	code, out = run("library", "narration", root, "--declaration", file)
	for _, want := range []string{"Preview: declare user narration declaration", "user-narration:reading-one", "user_declaration/1", "Expected ordered bytes:", "Verified assets: 2", "Preview only."} {
		if code != 0 || !strings.Contains(out, want) {
			t.Fatal(want, code, out)
		}
	}
	code, out = run("library", "narration", root, "--declaration", file, "--json")
	var env struct {
		Data library.NarrationResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil || code != 0 || env.Data.Plan == "" || env.Data.Applied {
		t.Fatal(out, err)
	}
	preview := env.Data
	if err := os.WriteFile(file, bytes.Replace(body, []byte("reading-one"), []byte("reading-two"), 1), 0600); err != nil {
		t.Fatal(err)
	}
	code, out = run("library", "narration", root, "--declaration", file, "--apply", "--plan", preview.Plan, "--json")
	env.Data = library.NarrationResult{}
	if err := json.Unmarshal([]byte(out), &env); err != nil || code != 1 || env.Data.Plan != "" || env.Data.Applied {
		t.Fatal("stale declaration accepted", out, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".nemalo", "narrations.jsonl")); !os.IsNotExist(err) {
		t.Fatal("stale apply wrote", err)
	}
	if err := os.WriteFile(file, body, 0600); err != nil {
		t.Fatal(err)
	}
	code, out = run("library", "narration", root, "--declaration", file, "--apply", "--plan", preview.Plan, "--json")
	env.Data = library.NarrationResult{}
	if err := json.Unmarshal([]byte(out), &env); err != nil || code != 0 || !env.Data.Applied {
		t.Fatal(out, err)
	}
	id := env.Data.Statement.ID
	code, out = run("library", "recordings", root)
	if code != 0 || !strings.Contains(out, "User narration: user-narration:reading-one (user_declaration/1), 2 recording(s)") || !strings.Contains(out, id) {
		t.Fatal(code, out)
	}
	code, out = run("library", "narration", root, "--retract", id, "--json")
	env.Data = library.NarrationResult{}
	if err := json.Unmarshal([]byte(out), &env); err != nil || code != 0 || env.Data.Statement.Active {
		t.Fatal(out, err)
	}
	code, out = run("library", "narration", root, "--retract", id, "--apply", "--plan", env.Data.Plan)
	if code != 0 || !strings.Contains(out, "Recorded: retract user narration declaration") || !strings.Contains(out, "State: retracted | Active: false") {
		t.Fatal(code, out)
	}
}
