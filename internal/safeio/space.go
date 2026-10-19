package safeio

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// spareBytes is kept free beyond a write's own size, so a filled disk still
// has room for journals, receipts, and the operating system.
const spareBytes = 64 << 20

// ErrNoSpace means a write would not fit in the free space of its folder.
var ErrNoSpace = errors.New("not enough free space")

// FreeSpace reads free space for EnsureSpace; tests replace it to simulate a
// full disk.
var FreeSpace = FreeBytes

// EnsureSpace refuses a write of need bytes into dir, or into its nearest
// existing parent when dir is still to be created, when the filesystem
// reports less free space than that plus a small spare. When free space
// cannot be read, the write is not refused: the write itself still fails
// cleanly if the disk fills.
func EnsureSpace(dir string, need int64) error {
	if need <= 0 {
		return nil
	}
	// A root opened through the descriptor bridge is named by its descriptor;
	// its free space is the folder that descriptor refers to.
	if strings.HasPrefix(dir, "/proc/self/fd/") || strings.HasPrefix(dir, "/dev/fd/") {
		target, err := os.Readlink(dir)
		if err != nil || !filepath.IsAbs(target) {
			return nil
		}
		dir = target
	}
	free, err := FreeSpace(dir)
	for d := dir; err != nil; {
		parent := filepath.Dir(d)
		if parent == d {
			return nil
		}
		d = parent
		free, err = FreeSpace(d)
	}
	if free < 0 {
		return nil
	}
	if free < need+spareBytes {
		return fmt.Errorf("%w in %s: this needs %s and %s more to keep in reserve, and %s are free", ErrNoSpace, dir, size(need), size(spareBytes), size(free))
	}
	return nil
}

func size(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}
