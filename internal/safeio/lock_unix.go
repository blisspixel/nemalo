//go:build linux || darwin

package safeio

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// TryLock takes a nonblocking advisory lock on f, exclusive or shared. The
// operating system releases it when the process ends, however it ends. A
// lock another process holds is ErrLocked.
func TryLock(f *os.File, exclusive bool) error {
	mode := unix.LOCK_SH | unix.LOCK_NB
	if exclusive {
		mode = unix.LOCK_EX | unix.LOCK_NB
	}
	err := unix.Flock(int(f.Fd()), mode)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return ErrLocked
	}
	return err
}

// Unlock releases a lock TryLock took.
func Unlock(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_UN) }
