//go:build !linux && !darwin && !windows

package safeio

import "errors"

// FreeBytes is unknown on this platform.
func FreeBytes(string) (int64, error) {
	return -1, errors.New("free space is not available on this platform")
}
