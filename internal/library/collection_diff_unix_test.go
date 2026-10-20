//go:build linux || darwin

package library

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestCollectionDiffRefusesFIFOWithoutBlocking(t *testing.T) {
	if os.Getenv("NEMALO_COLLECTION_FIFO_TEST") == "1" {
		dir := t.TempDir()
		good, fifo := filepath.Join(dir, "good.json"), filepath.Join(dir, "pipe.json")
		writeDiffCollection(t, good, "Shelf", CollectionItem{Source: "archive:demo", File: "book.epub"})
		if err := unix.Mkfifo(fifo, 0600); err != nil {
			t.Fatal(err)
		}
		if r, err := CollectionDiff(context.Background(), good, fifo); err == nil || r.Complete {
			t.Fatal("FIFO admitted", r, err)
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCollectionDiffRefusesFIFOWithoutBlocking$")
	cmd.Env = append(os.Environ(), "NEMALO_COLLECTION_FIFO_TEST=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("collection special-file open blocked or failed: %v %s", err, out)
	}
}
