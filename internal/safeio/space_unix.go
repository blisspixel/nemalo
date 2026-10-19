//go:build linux || darwin

package safeio

import (
	"os"

	"golang.org/x/sys/unix"
)

// FreeBytes is the space available to this user in the filesystem holding dir.
func FreeBytes(dir string) (int64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return -1, err
	}
	return available(st), nil
}

// freeBytesIn measures an open root through its descriptor.
func freeBytesIn(root *os.Root) (int64, error) {
	f, err := root.Open(".")
	if err != nil {
		return -1, err
	}
	defer f.Close()
	var st unix.Statfs_t
	if err := unix.Fstatfs(int(f.Fd()), &st); err != nil {
		return -1, err
	}
	return available(st), nil
}

// available is the free space for unprivileged writes. A pseudo filesystem
// such as procfs reports no blocks; its free space is unknown.
func available(st unix.Statfs_t) int64 {
	if st.Blocks == 0 {
		return -1
	}
	free := uint64(st.Bavail) * uint64(st.Bsize)
	if free > 1<<62 {
		free = 1 << 62
	}
	return int64(free)
}
