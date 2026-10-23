package library

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// Only caller-owned coverage scratch reaches this helper, after all child
// processes and required counter publication have finished. WalkDir does not
// follow links. Keep every production/fixture cleanup on its original path.
func removeCoverageScratch(root string) error {
	var directories []string
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Type()&fs.ModeSymlink == 0 {
			directories = append(directories, name)
			return nil
		}
		return os.Remove(name)
	})
	if errors.Is(err, os.ErrNotExist) && len(directories) == 0 {
		return nil
	}
	if err != nil {
		return err
	}
	for i := len(directories) - 1; i >= 0; i-- {
		if err := os.Remove(directories[i]); err != nil {
			return err
		}
	}
	return nil
}

func TestCoverageScratchRemovalKeepsSiblingsAndReportsFailures(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "counter-scratch")
	for _, folder := range []string{"counters", "reader-counters", "merged", "reader-merged/nested"} {
		if err := os.MkdirAll(filepath.Join(root, folder), 0700); err != nil {
			t.Fatal(err)
		}
		for i := range 32 {
			name := filepath.Join(root, folder, fmt.Sprintf("%04d.counter", i))
			if err := os.WriteFile(name, []byte("synthetic instrumented counter"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	sibling := filepath.Join(base, "preserved")
	if err := os.WriteFile(sibling, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := removeCoverageScratch(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("scratch remains", err)
	}
	if err := removeCoverageScratch(root); err != nil {
		t.Fatal("missing scratch must be harmless", err)
	}
	if err := removeCoverageScratch(root + "\x00"); err == nil {
		t.Fatal("cleanup failure was hidden")
	}
	if got, err := os.ReadFile(sibling); err != nil || string(got) != "keep" {
		t.Fatal("unrelated sibling changed", string(got), err)
	}
}
