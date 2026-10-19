package safeio

import "golang.org/x/sys/windows"

// FreeBytes is the space available to this user in the volume holding dir.
func FreeBytes(dir string) (int64, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return -1, err
	}
	var available, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &available, &total, &free); err != nil {
		return -1, err
	}
	if available > 1<<62 {
		available = 1 << 62
	}
	return int64(available), nil
}
