package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLibraryControlCommands(t *testing.T) {
	root := t.TempDir()
	code, out, _ := execute(t, []string{"library", "status", root, "--json"}, nil)
	if code != 0 || !strings.Contains(out, `"status":"not_initialized"`) {
		t.Fatal(code, out)
	}
	if _, err := os.Stat(filepath.Join(root, ".nemalo")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("status created state")
	}
	code, out, _ = execute(t, []string{"library", "init", root, "--json"}, nil)
	if code != 0 {
		t.Fatal(code, out)
	}
	var envelope Envelope
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || envelope.SchemaVersion != 1 || envelope.Command != "library" || envelope.Error != "" {
		t.Fatal(envelope, err)
	}
	for _, action := range []string{"init", "status"} {
		code, out, _ = execute(t, []string{"library", action, root}, nil)
		if code != 0 || !strings.Contains(out, `Control state: "initialized"`) || !strings.Contains(out, "Journal records: 2") {
			t.Fatal(action, code, out)
		}
		code, out, _ = execute(t, []string{"library", action, root, "--json"}, nil)
		if code != 0 || !strings.Contains(out, `"status":"initialized"`) {
			t.Fatal(action, code, out)
		}
	}
	for _, args := range [][]string{{"library", "init"}, {"library", "status"}, {"library", "init", root, "extra"}, {"library", "status", root, "--scan"}, {"library", "init", root, "--output", "x"}, {"library", "status", root, "--max-entries", "1"}} {
		if code, _, _ := execute(t, args, nil); code != 2 {
			t.Fatal("invalid state command accepted", args, code)
		}
	}
	for _, action := range []string{"init", "status"} {
		if code, _, _ := execute(t, []string{"library", action, filepath.Join(root, "missing"), "--json"}, nil); code != 1 {
			t.Fatal("missing root accepted", action, code)
		}
	}
	journal := filepath.Join(root, ".nemalo", "journal.jsonl")
	if err := os.WriteFile(journal, []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"init", "status"} {
		code, out, _ := execute(t, []string{"library", action, root, "--json"}, nil)
		if code != 1 || !strings.Contains(out, "needs_review") || !strings.Contains(out, `"error":`) {
			t.Fatal("invalid state accepted", action, code, out)
		}
	}
}
