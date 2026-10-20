package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/testfs"
)

func TestWindowsReparseInventoryServices(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	canary := filepath.Join(outside, "outside.par2")
	canaryBytes := []byte("PAR2\x00PKT outside bytes must not be probed or hashed")
	if err := os.WriteFile(canary, canaryBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "local"), []byte("local"), 0600); err != nil {
		t.Fatal(err)
	}
	testfs.Junction(t, filepath.Join(dir, "junction"), outside)
	testfs.Junction(t, filepath.Join(dir, "loop"), dir)
	testfs.Symlink(t, filepath.Join(dir, "directory_link"), outside)
	testfs.Symlink(t, filepath.Join(dir, "file_link.par2"), canary)
	s := Service{Scanner: kindScanner{}}
	for _, mode := range []struct {
		name         string
		hashes, par2 bool
		bytes        int64
	}{{"default", false, false, 0}, {"hashes", true, false, 5}, {"par2", false, true, 5}, {"both", true, true, 10}} {
		t.Run(mode.name, func(t *testing.T) {
			limits := inventory.Defaults()
			limits.Hashes, limits.PAR2 = mode.hashes, mode.par2
			r, err := s.Inspect(context.Background(), dir, limits)
			if err != nil || !r.Complete || r.BytesRead != mode.bytes || len(r.Entries) != 5 {
				t.Fatal(r, err)
			}
			for _, e := range r.Entries {
				if e.Path == "local" {
					if (e.SHA256 != "") != mode.hashes {
						t.Fatal("ordinary hash behavior changed", e)
					}
					continue
				}
				kind, finding := "link", "not_followed"
				if e.Path == "junction" || e.Path == "loop" {
					kind, finding = "special", "not_opened"
				}
				if e.Kind != kind || e.Finding != finding || e.SHA256 != "" || e.PAR2 != nil {
					t.Fatal("reparse point was followed", e)
				}
			}
			if mode.par2 && (r.PAR2 == nil || !r.PAR2.Complete || r.PAR2.FilesProbed != 1 || len(r.PAR2.Sets) != 0) {
				t.Fatal("reparse point affected recovery metadata", r.PAR2)
			}
			if !mode.par2 && r.PAR2 != nil {
				t.Fatal("implicit recovery metadata", r.PAR2)
			}
		})
	}
	for _, name := range []string{"junction", "directory_link"} {
		if _, err := s.Inspect(context.Background(), filepath.Join(dir, name), inventory.Defaults()); err == nil {
			t.Fatal("inventory accepted a reparse root", name)
		}
	}
	got, err := os.ReadFile(canary)
	if err != nil || string(got) != string(canaryBytes) {
		t.Fatal("outside canary changed", string(got), err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 1 {
		t.Fatal("outside directory changed", entries, err)
	}
}
