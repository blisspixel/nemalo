package library

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// A failed child or publication can leave live descendant writes or unmerged
// evidence. Retain owned counters then; cleanup never changes that failed result.
func finishCoverageScratch(root string, published bool) error {
	if !published {
		fmt.Fprintln(os.Stderr, "retained unpublished coverage scratch:", root)
		return nil
	}
	return removeCoverageScratch(root)
}

func TestCoverageScratchRetainsUnpublishedBytes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "owned-counters")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "temporary-unmerged-counter")
	if err := os.WriteFile(file, []byte("retain before publication"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := finishCoverageScratch(root, false); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "retain before publication" {
		t.Fatal("unpublished counters were removed", err)
	}
	if err := finishCoverageScratch(root, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("published owned scratch remains", err)
	}
	if err := finishCoverageScratch(root+"\x00", true); err == nil {
		t.Fatal("successful-run cleanup failure was hidden")
	}
}

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
