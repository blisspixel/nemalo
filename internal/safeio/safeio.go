// Package safeio binds filesystem validation to opened capabilities before use.
package safeio

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

func OpenRoot(name string) (*os.Root, error) {
	abs, err := filepath.Abs(name)
	if err != nil {
		return nil, err
	}
	before, err := Lstat(abs)
	if err != nil {
		return nil, err
	}
	return openRoot(abs, before)
}

// OpenResolvedRoot permits an explicitly selected directory alias while binding
// its resolved capability to the identity observed before resolution.
func OpenResolvedRoot(name string) (*os.Root, error) {
	abs, err := filepath.Abs(name)
	if err != nil {
		return nil, err
	}
	before, err := Stat(abs)
	if err != nil {
		return nil, err
	}
	physical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	root, err := OpenRoot(physical)
	if err != nil {
		return nil, err
	}
	return checkRoot(root, before)
}

func openRoot(name string, before fs.FileInfo) (*os.Root, error) {
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("root must be a directory, not a link")
	}
	root, err := platformRoot(name)
	if err != nil {
		return nil, err
	}
	return checkRoot(root, before)
}

func OpenRootIn(parent *os.Root, name string) (*os.Root, error) {
	before, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("root must be a directory, not a link")
	}
	root, err := platformRootIn(parent, name)
	if err != nil {
		return nil, err
	}
	return checkRoot(root, before)
}

func checkRoot(root *os.Root, before fs.FileInfo) (*os.Root, error) {
	opened, err := root.Stat(".")
	if err != nil || !opened.IsDir() || !os.SameFile(before, opened) {
		_ = root.Close()
		return nil, errors.Join(err, errors.New("directory changed while opening"))
	}
	return root, nil
}

// OpenRegular uses nonblocking acquisition where the OS supports special files,
// then checks type and identity before any content read. Root retains traversal
// confinement; this does not claim a sandbox against mounts or stalled devices.
func OpenRegular(root *os.Root, name string, before fs.FileInfo, flags int) (*os.File, error) {
	f, err := root.OpenFile(name, flags|nonblockFlag, 0600)
	if err != nil {
		return nil, err
	}
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || (before != nil && !os.SameFile(before, opened)) {
		_ = f.Close()
		return nil, errors.Join(err, errors.New("regular file changed while opening"))
	}
	return f, nil
}
