package library

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/blisspixel/nemalo/internal/safeio"
)

// heldParent opens the folder of a held copy one level at a time from the
// library root, refusing a link or anything but a folder at every level, so a
// held path can never lead out of its cleanup's holding area, for example
// into assets/ through a planted link. A missing folder is os.ErrNotExist; a
// link or other object is ErrStateReview. The caller closes the root.
func heldParent(store *os.Root, held string) (*os.Root, string, error) {
	dir, base := path.Split(held)
	if base == "" || !fs.ValidPath(held) {
		return nil, "", fmt.Errorf("%w: invalid held path %s", ErrStateReview, held)
	}
	parent := store
	for _, part := range strings.Split(strings.TrimSuffix(dir, "/"), "/") {
		next, err := safeio.OpenRootIn(parent, part)
		if parent != store {
			_ = parent.Close()
		}
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, "", err
			}
			return nil, "", fmt.Errorf("%w: holding folder %s is not a plain folder: %v", ErrStateReview, strings.TrimSuffix(dir, "/"), err)
		}
		parent = next
	}
	return parent, base, nil
}

// removeHeld removes a held copy through its parent folder, only while its
// name still refers to the file described by before.
func removeHeld(store *os.Root, held string, before os.FileInfo) error {
	parent, base, err := heldParent(store, held)
	if err != nil {
		return err
	}
	defer parent.Close()
	current, err := parent.Lstat(base)
	if err != nil {
		return err
	}
	if !os.SameFile(before, current) {
		return fmt.Errorf("%w: held copy %s changed before removal", ErrStateReview, held)
	}
	return parent.Remove(base)
}

// openHeld opens a held copy through its parent folder, only when it is the
// file described by before.
func openHeld(store *os.Root, held string, before os.FileInfo) (*os.File, error) {
	parent, base, err := heldParent(store, held)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	return safeio.OpenRegular(parent, filepath.FromSlash(base), before, os.O_RDONLY)
}
