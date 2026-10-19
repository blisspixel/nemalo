//go:build linux || darwin

package library

import (
	"errors"
	"github.com/blisspixel/nemalo/internal/safeio"
	"os"

	"golang.org/x/sys/unix"
)

func lockState(f *os.File, write bool) error {
	if err := safeio.TryLock(f, write); err != nil {
		if errors.Is(err, safeio.ErrLocked) {
			return ErrBusy
		}
		return err
	}
	return nil
}

func unlockState(f *os.File) error { return safeio.Unlock(f) }

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
