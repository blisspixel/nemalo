package safeio

import (
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
	f := os.NewFile(uintptr(h), name)
	defer f.Close()
	return f.Stat()
}

// DescriptorBridge reports whether this process can reopen a held directory.
// Windows validates path identity through an attributes-only handle.
func DescriptorBridge() error { return nil }

// Windows Root rejects reserved device paths and acquires a directory handle.
// checkRoot binds that handle to the previously validated directory identity.
func platformRoot(name string) (*os.Root, error)                    { return os.OpenRoot(name) }
func platformRootIn(parent *os.Root, name string) (*os.Root, error) { return parent.OpenRoot(name) }
