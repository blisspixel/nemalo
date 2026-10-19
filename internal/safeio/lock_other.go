//go:build !linux && !darwin && !windows

package safeio

import (
	"errors"
	"os"
)

// TryLock is unavailable on this platform.
func TryLock(*os.File, bool) error {
	return errors.New("file locks require Linux, macOS, or Windows")
}

// Unlock is unavailable on this platform.
func Unlock(*os.File) error {
	return errors.New("file locks require Linux, macOS, or Windows")
}
