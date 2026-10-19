package safeio

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// spareBytes is kept free beyond a write's own size, so a filled disk still
// has room for journals, receipts, and the operating system.
const spareBytes = 64 << 20

// ErrNoSpace means a write would not fit in the free space of its folder.
var ErrNoSpace = errors.New("not enough free space")

// FreeSpace reports the free bytes of a folder: the one open as root when
// root is not nil, else the one named dir. A negative result is unknown.
// Tests replace it to simulate a full disk.
var FreeSpace = func(dir string, root *os.Root) (int64, error) {
	if root != nil {
		return freeBytesIn(root)
	}
	return FreeBytes(dir)
}

// EnsureSpace refuses a write of need bytes into dir, or into its nearest
// existing parent when dir is still to be created, when the filesystem
// reports less free space than that plus a small spare. When free space
// cannot be read, the write is not refused: the write itself still fails
// cleanly if the disk fills.
func EnsureSpace(dir string, need int64) error {
	if need <= 0 {
		return nil
	}
	free, err := FreeSpace(dir, nil)
	for d := dir; err != nil; {
		parent := filepath.Dir(d)
		if parent == d {
			return nil
		}
		d = parent
		free, err = FreeSpace(d, nil)
	}
	return enough(dir, free, need)
}

// EnsureSpaceIn is EnsureSpace for a write into an open root. The root is
// measured through its own descriptor, since a root opened through the
// descriptor bridge has no ordinary path.
func EnsureSpaceIn(root *os.Root, need int64) error {
	if need <= 0 {
		return nil
	}
	free, err := FreeSpace(root.Name(), root)
	if err != nil {
		return nil
	}
	return enough("this folder", free, need)
}

func enough(where string, free, need int64) error {
	if free < 0 || free >= need+spareBytes {
		return nil
	}
	return fmt.Errorf("%w in %s: this needs %s and %s more to keep in reserve, and %s are free", ErrNoSpace, where, size(need), size(spareBytes), size(free))
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
