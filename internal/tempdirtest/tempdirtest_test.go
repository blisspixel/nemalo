package tempdirtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var parent = os.TempDir()

func TestMain(m *testing.M) {
	code := Run(m)
	if code == 0 {
		// The folder the tests used is gone and nothing else is left behind.
		if _, err := os.Stat(used); used == "" || !os.IsNotExist(err) {
			os.Stderr.WriteString("test temporary folder remains\n")
			code = 1
		}
	}
	os.Exit(code)
}

var used string

func TestRunUsesAPrivateFolder(t *testing.T) {
	used = os.TempDir()
	if filepath.Dir(used) != filepath.Clean(parent) || !strings.HasPrefix(filepath.Base(used), "nemalo-test-") {
		t.Fatalf("temporary folder %q is not a new folder in %q", used, parent)
	}
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		if os.Getenv(name) != used {
			t.Fatalf("%s = %q, want %q", name, os.Getenv(name), used)
		}
	}
	if err := os.WriteFile(filepath.Join(used, "left.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}
