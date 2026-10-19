package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/config"
)

func TestLibraryCleanupAndRestoreCommands(t *testing.T) {
	root, folder, state := t.TempDir(), t.TempDir(), t.TempDir()
	s := app.Service{Scanner: fakeScanner("unavailable")}
	run := func(args ...string) (int, string) {
		var out, errOut bytes.Buffer
		code := Execute(context.Background(), args, &out, &errOut, s, func(string) string { return "" }, func() (config.Paths, error) {
			return config.Paths{Config: filepath.Join(state, "none"), State: state}, nil
		}, nil)
		return code, out.String() + errOut.String()
	}
	book := epubFile(t, folder)
	if err := os.WriteFile(filepath.Join(folder, "notes.txt"), []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"library", "init", root}, {"library", "ingest", root, folder, "--apply"}} {
		if code, out := run(args...); code != 0 {
			t.Fatal(args, code, out)
		}
	}
	code, out := run("library", "cleanup", root, folder, "--json")
	if code != 0 {
		t.Fatal(code, out)
	}
	var plan struct {
		Data struct {
			Digest string `json:"plan_digest"`
			Move   int    `json:"move"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &plan); err != nil || plan.Data.Move != 1 {
		t.Fatal(out, err)
	}
	if code, out := run("library", "cleanup", root, folder); code != 0 || !strings.Contains(out, "Apply with: --apply --plan "+plan.Data.Digest) {
		t.Fatal("human preview", code, out)
	}
	for _, args := range [][]string{
		{"library", "cleanup", root, folder, "--apply"},
		{"library", "cleanup", root, folder, "--plan", plan.Data.Digest},
		{"library", "cleanup", root},
		{"library", "cleanup", root, folder, "--scan"},
		{"library", "restore", root},
		{"library", "restore", root, "--cleanup", "x", "--plan", "y"},
	} {
		if code, _ := run(args...); code != 2 {
			t.Fatal("bad usage accepted", args, code)
		}
	}
	code, out = run("library", "cleanup", root, folder, "--apply", "--plan", plan.Data.Digest)
	if code != 0 || !strings.Contains(out, "Applied: moved 1") || !strings.Contains(out, "Undo with: nemalo library restore") {
		t.Fatal("apply", code, out)
	}
	if _, err := os.Stat(book); !os.IsNotExist(err) {
		t.Fatal("held source still in the folder")
	}
	id := out[strings.Index(out, "--cleanup ")+len("--cleanup ") : strings.Index(out, " --apply\n")]
	if code, out := run("library", "restore", root, "--cleanup", id, "--apply"); code != 0 || !strings.Contains(out, "restore book.epub -> restored") {
		t.Fatal("restore", code, out)
	}
	if _, err := os.Stat(book); err != nil {
		t.Fatal("restored file missing", err)
	}
}

func TestLibraryCheckCommand(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	s := app.Service{Scanner: fakeScanner("unavailable")}
	run := func(args ...string) (int, string) {
		var out, errOut bytes.Buffer
		code := Execute(context.Background(), args, &out, &errOut, s, func(string) string { return "" }, func() (config.Paths, error) {
			return config.Paths{Config: filepath.Join(state, "none"), State: state}, nil
		}, nil)
		return code, out.String() + errOut.String()
	}
	if code, out := run("library", "init", root); code != 0 {
		t.Fatal(code, out)
	}
	if code, out := run("library", "import", root, epubFile(t, t.TempDir()), "--apply"); code != 0 {
		t.Fatal(code, out)
	}
	if code, out := run("library", "check", root); code != 0 || !strings.Contains(out, "Check: healthy") || !strings.Contains(out, "library rescan") {
		t.Fatal(code, out)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "stray.epub"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out := run("library", "check", root, "--json"); code != 1 || !strings.Contains(out, `"kind":"untracked"`) {
		t.Fatal(code, out)
	}
	if code, _ := run("library", "check", root, "--apply"); code != 2 {
		t.Fatal("unrelated flag accepted")
	}
}
