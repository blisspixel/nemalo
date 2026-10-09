package library

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDestinationRetainsValidatedParent(t *testing.T) {
	base := t.TempDir()
	source := filepath.Join(base, "source")
	output := filepath.Join(base, "output")
	for _, dir := range []string{source, output} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	parent, _, err := openDestination(source, filepath.Join(output, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if err := os.Rename(output, output+"-original"); err != nil {
		if runtime.GOOS != "windows" {
			t.Fatal(err)
		}
		// Windows retains a directory handle that prevents the swap.
		return
	}
	if err := os.Mkdir(output, 0700); err != nil {
		t.Fatal(err)
	}
	if err := parent.WriteFile("bound", []byte("original parent"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(output, "bound")); !os.IsNotExist(err) {
		t.Fatal("publication redirected", err)
	}
	if _, err := os.Stat(filepath.Join(output+"-original", "bound")); err != nil {
		t.Fatal(err)
	}
}
