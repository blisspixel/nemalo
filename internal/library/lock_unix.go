//go:build linux || darwin

package library

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func lockState(f *os.File, write bool) error {
	mode := unix.LOCK_SH | unix.LOCK_NB
	if write {
		mode = unix.LOCK_EX | unix.LOCK_NB
	}
	err := unix.Flock(int(f.Fd()), mode)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return ErrBusy
	}
	return err
}

func unlockState(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_UN) }

func linkCount(f *os.File) (uint64, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &stat); err != nil {
		return 0, err
	}
	return uint64(stat.Nlink), nil
}

func singleLink(f *os.File) error {
	n, err := linkCount(f)
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrStateReview
	}
	return nil
}
