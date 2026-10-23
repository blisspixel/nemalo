package library

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/blisspixel/nemalo/internal/testfs"
)

func TestWindowsReparseCoverageScratchKeepsLinkedTargets(t *testing.T) {
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "preserved")
	if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(t.TempDir(), "private-")
	if err != nil {
		t.Fatal(err)
	}
	testfs.Junction(t, filepath.Join(root, "junction"), outside)
	testfs.Symlink(t, filepath.Join(root, "directory-link"), outside)
	testfs.Symlink(t, filepath.Join(root, "file-link"), sentinel)
	if err := os.Link(sentinel, filepath.Join(root, "hard-link")); err != nil {
		t.Fatal(err)
	}
	if err := removeCoverageScratch(root); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(sentinel); err != nil || string(got) != "keep" {
		t.Fatal("linked target changed", string(got), err)
	}
}
