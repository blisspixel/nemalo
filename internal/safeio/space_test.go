package safeio

import (
	"errors"
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
	FreeSpace = func(string) (int64, error) { return -1, errors.New("unknown") }
	defer func() { FreeSpace = saved }()
	if err := EnsureSpace(dir, 1<<40); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSpace(dir, 0); err != nil {
		t.Fatal(err)
	}
}
