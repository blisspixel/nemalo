//go:build !linux && !darwin && !windows

package library

import (
	"errors"
	"os"
)

func lockState(*os.File, bool) error {
	return errors.New("managed library locks require Linux, macOS, or Windows")
}
func unlockState(*os.File) error {
	return errors.New("managed library locks require Linux, macOS, or Windows")
}
func singleLink(*os.File) error {
	return errors.New("managed library link checks require Linux, macOS, or Windows")
}
