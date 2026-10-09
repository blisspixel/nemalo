//go:build !linux && !darwin && !windows

package safeio

import (
	"errors"
	"io/fs"
	"os"
)

const nonblockFlag = 0

func Lstat(name string) (fs.FileInfo, error) { return os.Lstat(name) }
func Stat(name string) (fs.FileInfo, error)  { return os.Stat(name) }

func platformRoot(string) (*os.Root, error) {
	return nil, errors.New("bound roots unsupported on this platform")
}
func platformRootIn(*os.Root, string) (*os.Root, error) {
	return nil, errors.New("bound roots unsupported on this platform")
}
