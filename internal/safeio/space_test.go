package safeio

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureSpaceRefusesWhatWouldNotFit(t *testing.T) {
	dir := t.TempDir()
	free, err := FreeBytes(dir)
	if err != nil || free <= 0 {
		t.Fatal(free, err)
	}
	if err := EnsureSpace(dir, 1); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSpace(dir, free); !errors.Is(err, ErrNoSpace) || !strings.Contains(err.Error(), "are free") {
		t.Fatal("a write as large as the free space must keep a reserve", err)
	}
	// A folder still to be created is checked at its nearest existing parent.
	if err := EnsureSpace(filepath.Join(dir, "missing", "deeper"), free); !errors.Is(err, ErrNoSpace) {
		t.Fatal("a new folder was not checked at its parent", err)
	}
	// Unknown free space never refuses: the write itself still fails cleanly.
	saved := FreeSpace
	FreeSpace = func(string, *os.Root) (int64, error) { return -1, errors.New("unknown") }
	defer func() { FreeSpace = saved }()
	if err := EnsureSpace(dir, 1<<40); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSpace(dir, 0); err != nil {
		t.Fatal(err)
	}
}

// A root is measured through its descriptor, so a root opened through the
// descriptor bridge is checked like any folder.
func TestEnsureSpaceInMeasuresAnOpenRoot(t *testing.T) {
	dir := t.TempDir()
	root, err := OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	free, err := freeBytesIn(root)
	if err != nil || free <= 0 {
		t.Fatal(free, err)
	}
	if err := EnsureSpaceIn(root, 1); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSpaceIn(root, free); !errors.Is(err, ErrNoSpace) {
		t.Fatal("an open root was not measured", root.Name(), err)
	}
}
