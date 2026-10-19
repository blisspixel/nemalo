//go:build linux || darwin

package safeio

import "golang.org/x/sys/unix"

// FreeBytes is the space available to this user in the filesystem holding dir.
func FreeBytes(dir string) (int64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return -1, err
	}
	// A pseudo filesystem such as procfs reports no blocks; its free space
	// says nothing about where bytes would land.
	if st.Blocks == 0 {
		return -1, nil
	}
	free := uint64(st.Bavail) * uint64(st.Bsize)
	if free > 1<<62 {
		free = 1 << 62
	}
	return int64(free), nil
}
