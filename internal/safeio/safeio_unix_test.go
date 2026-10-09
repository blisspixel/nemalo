//go:build linux || darwin

package safeio

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestSpecialReplacementDoesNotBlock(t *testing.T) {
	if os.Getenv("NEMALO_FIFO_TEST") == "1" {
		dir := t.TempDir()
		name := filepath.Join(dir, "book")
		if err := os.WriteFile(name, []byte("regular"), 0600); err != nil {
			t.Fatal(err)
		}
		before, _ := os.Lstat(name)
		root, err := OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		if err := os.Remove(name); err != nil {
			t.Fatal(err)
		}
		if err := unix.Mkfifo(name, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenRegular(root, "book", before, os.O_RDONLY); err == nil {
			t.Fatal("FIFO admitted")
		}
		if _, err := platformRoot(name); err == nil {
			t.Fatal("FIFO root admitted")
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSpecialReplacementDoesNotBlock$")
	cmd.Env = append(os.Environ(), "NEMALO_FIFO_TEST=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("blocking or failed special-file open: %v %s", err, out)
	}
}
