//go:build linux || darwin

package safeio

import (
	"fmt"
	"io/fs"
	"os"
	"runtime"

	"golang.org/x/sys/unix"
)

const nonblockFlag = unix.O_NONBLOCK
const directoryFlags = os.O_RDONLY | unix.O_DIRECTORY | unix.O_NONBLOCK | unix.O_NOFOLLOW

func Lstat(name string) (fs.FileInfo, error) { return os.Lstat(name) }
func Stat(name string) (fs.FileInfo, error)  { return os.Stat(name) }

func platformRoot(name string) (*os.Root, error) {
	f, err := os.OpenFile(name, directoryFlags, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return rootFromDirectory(f)
}

func platformRootIn(parent *os.Root, name string) (*os.Root, error) {
	f, err := parent.OpenFile(name, directoryFlags, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return rootFromDirectory(f)
}

func rootFromDirectory(f *os.File) (*os.Root, error) {
	// Root has no exported descriptor constructor. Reopen the held directory
	// capability, never the mutable user pathname. These native descriptor
	// filesystems must be available; there is no pathname fallback.
	prefix := "/dev/fd"
	if runtime.GOOS == "linux" {
		prefix = "/proc/self/fd"
	}
	root, err := os.OpenRoot(fmt.Sprintf("%s/%d", prefix, f.Fd()))
	runtime.KeepAlive(f)
	return root, err
}
