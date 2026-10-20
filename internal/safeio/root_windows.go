package safeio

import (
	"errors"
	"golang.org/x/sys/windows"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const nonblockFlag = 0

// Path Stat on Windows can defer file-ID loading until SameFile. Capture the
// identity from a held attributes-only handle as part of validation instead.
func Lstat(name string) (fs.FileInfo, error) { return statPath(name, true) }
func Stat(name string) (fs.FileInfo, error)  { return statPath(name, false) }
func statPath(name string, noFollow bool) (fs.FileInfo, error) {
	f, err := openPath(name, noFollow)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Stat()
}

func openPath(name string, noFollow bool) (*os.File, error) {
	abs, err := filepath.Abs(name)
	if err != nil {
		return nil, err
	}
	if len(abs) > 248 && !strings.HasPrefix(abs, `\\?\`) {
		if strings.HasPrefix(abs, `\\`) {
			abs = `\\?\UNC\` + strings.TrimPrefix(abs, `\\`)
		} else {
			abs = `\\?\` + abs
		}
	}
	p, err := windows.UTF16PtrFromString(abs)
	if err != nil {
		return nil, err
	}
	flags := uint32(windows.FILE_FLAG_BACKUP_SEMANTICS)
	if noFollow {
		flags |= windows.FILE_FLAG_OPEN_REPARSE_POINT
	}
	h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, flags, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), name), nil
}

// Junctions are irregular files in Go, so EvalSymlinks does not resolve them.
// Query a held target handle instead. OpenResolvedRoot still checks the final
// directory capability against its previously captured identity.
func resolvedRootPath(name string) (string, error) {
	f, err := openPath(name, false)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]uint16, 32768)
	n, err := windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &buf[0], uint32(len(buf)), 0)
	if err != nil {
		return "", err
	}
	if n == 0 || n >= uint32(len(buf)) {
		return "", errors.New("resolved directory path is empty or exceeds the Windows path limit")
	}
	return windows.UTF16ToString(buf[:n]), nil
}

// DescriptorBridge reports whether this process can reopen a held directory.
// Windows validates path identity through an attributes-only handle.
func DescriptorBridge() error { return nil }

// Windows Root rejects reserved device paths and acquires a directory handle.
// checkRoot binds that handle to the previously validated directory identity.
func platformRoot(name string) (*os.Root, error)                    { return os.OpenRoot(name) }
func platformRootIn(parent *os.Root, name string) (*os.Root, error) { return parent.OpenRoot(name) }
