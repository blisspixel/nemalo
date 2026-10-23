package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/config"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/scanner"
)

type citationScanner struct{ t *testing.T }

func (s citationScanner) Scan(context.Context, string) scanner.Result {
	s.t.Fatal("citation invoked scanner")
	return scanner.Result{}
}

func citationFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			files[path] = "directory"
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[path] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestCitationCLIUsesSharedServiceAndPreservesState(t *testing.T) {
	ctx := context.Background()
	root, state := t.TempDir(), t.TempDir()
	if _, err := library.Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	service := app.Service{Scanner: citationScanner{t}}
	source := writeCLIEPUB(t, t.TempDir())
	imported, err := service.Import(ctx, library.ImportRequest{Library: root, Source: source, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(imported.Path))); err != nil {
		t.Fatal(err)
	}
	before, stateBefore := citationFiles(t, root), citationFiles(t, state)
	run := func(args ...string) (int, string) {
		var out, stderr bytes.Buffer
		code := Execute(ctx, args, &out, &stderr, service, func(string) string { return "" }, func() (config.Paths, error) {
			return config.Paths{Config: filepath.Join(state, "missing"), State: state}, nil
		}, nil)
		return code, out.String() + stderr.String()
	}
	code, text := run("library", "citation", root, "--asset", imported.AssetID, "--json")
	var env struct {
		Data  app.CitationResult `json:"data"`
		Error string             `json:"error"`
	}
	if err := json.Unmarshal([]byte(text), &env); err != nil || code != 0 || env.Error != "" {
		t.Fatal(code, text, err)
	}
	direct, err := service.Citation(ctx, root, imported.AssetID)
	if err != nil || !reflect.DeepEqual(env.Data, direct) {
		t.Fatal("CLI differs from service", env.Data, direct, err)
	}
	if code, text := run("library", "citation", root, "--asset", imported.AssetID); code != 0 || !strings.Contains(text, "CSL-JSON item array") || !strings.Contains(text, "not revalidated") {
		t.Fatal(code, text)
	}
	for _, args := range [][]string{
		{"library", "citation", root}, {"library", "citation", root, "extra", "--asset", imported.AssetID},
		{"library", "citation", root, "--asset", imported.AssetID, "--decode"}, {"library", "citation", root, "--asset", imported.AssetID, "--apply"},
	} {
		if code, text := run(args...); code != 2 {
			t.Fatal("invalid citation request accepted", args, code, text)
		}
	}
	if code, _ := run("library", "citation", root, "--asset", "sha256:"+strings.Repeat("0", 64), "--json"); code != 1 {
		t.Fatal("absent holding accepted", code)
	}
	if !reflect.DeepEqual(before, citationFiles(t, root)) || !reflect.DeepEqual(stateBefore, citationFiles(t, state)) {
		t.Fatal("citation changed library or user state")
	}
}
