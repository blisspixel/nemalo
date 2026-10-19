package safeio

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// TryLock takes a nonblocking lock on f, exclusive or shared. The operating
// system releases it when the process ends, however it ends. A lock another
// process holds is ErrLocked.
func TryLock(f *os.File, exclusive bool) error {
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if exclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	err := windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, &windows.Overlapped{})
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return ErrLocked
	}
	return err
}

// Unlock releases a lock TryLock took.
func Unlock(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &windows.Overlapped{})
}
