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
	"github.com/blisspixel/nemalo/internal/pdftext/pdftest"
)

func TestContentReadsAndLocatesPDFPages(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	s := app.Service{Scanner: fakeScanner("unavailable")}
	run := func(args ...string) (int, string) {
		var out, errOut bytes.Buffer
		code := Execute(context.Background(), args, &out, &errOut, s, func(string) string { return "" }, func() (config.Paths, error) {
			return config.Paths{Config: filepath.Join(state, "none"), State: state}, nil
		}, nil)
		return code, out.String() + errOut.String()
	}
	source := filepath.Join(t.TempDir(), "paper.pdf")
	if err := os.WriteFile(source, pdftest.Pages("0 << /S /r >> 1 << /S /D >>", "Abstract", "The method is described here.", "The result follows the method."), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out := run("library", "init", root); code != 0 {
		t.Fatal(code, out)
	}
	code, out := run("library", "import", root, source, "--apply", "--json")
	if code != 0 {
		t.Fatal(code, out)
	}
	var imported struct {
		Data struct {
			AssetID string `json:"asset_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &imported); err != nil {
		t.Fatal(err, out)
	}
	asset := imported.Data.AssetID
	common := []string{root, "--root", root, "--asset", asset, "--representation", "pdf-text/1"}
	if code, out := run(append([]string{"content", "units"}, common...)...); code != 0 || !strings.Contains(out, "Pages: 3") || !strings.Contains(out, `label "i"`) || !strings.Contains(out, `label "2"`) {
		t.Fatal("units", code, out)
	}
	if code, out := run(append([]string{"content", "read"}, append(common, "--unit", "1")...)...); code != 0 || !strings.Contains(out, `Page index 1 (zero-based) | printed label "1"`) || !strings.Contains(out, "The method is described here.") || !strings.Contains(out, "Continue with: --cursor") {
		t.Fatal("read", code, out)
	}
	code, out = run(append([]string{"content", "locate"}, append(common, "--text", "method")...)...)
	if code != 0 || !strings.Contains(out, "Exact matches: 2") || !strings.Contains(out, `page index 1, label "1"`) || !strings.Contains(out, `page index 2, label "2"`) {
		t.Fatal("locate", code, out)
	}
	for _, args := range [][]string{
		append([]string{"content", "locate"}, common...),
		append([]string{"content", "locate"}, append(common, "--text", "x", "--unit", "1")...),
		append([]string{"content", "read"}, append(common, "--text", "x")...),
	} {
		if code, _ := run(args...); code != 2 {
			t.Fatal("bad locate usage accepted", args, code)
		}
	}
}
