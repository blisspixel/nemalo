package library

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func lockState(f *os.File, write bool) error {
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if write {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	err := windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, &windows.Overlapped{})
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return ErrBusy
	}
	return err
}

func unlockState(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &windows.Overlapped{})
}

func linkCount(f *os.File) (uint64, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info); err != nil {
		return 0, err
	}
	return uint64(info.NumberOfLinks), nil
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
