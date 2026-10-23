package library

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/blisspixel/nemalo/internal/safeio"
)

// JobScope is recorded identity and, only under a shared lock, current canonical
// holdings. It does not establish content integrity or authorize an operation.
// Holdings contain private provenance and must not be returned as job output.
type JobScope struct {
	LibraryID string
	Catalog   string // observed or busy
	Holdings  []Holding
}

// WithJobScope observes the immutable initialization identity even while a
// writer owns the library. It never opens mutable journals in that case. When
// available the shared lock remains held through use. Any error invalidates the
// callback's output. No directory, file, lock or initialization is created.
func WithJobScope(ctx context.Context, directory string, use func(JobScope) error) (err error) {
	if directory == "" || use == nil {
		return errors.New("job scope needs an explicit existing library and callback")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	abs, err := filepath.Abs(directory)
	if err != nil {
		return err
	}
	parent, err := safeio.OpenRoot(abs)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, parent.Close()) }()
	root, err := safeio.OpenRootIn(parent, ".nemalo")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	if err := validateControl(root); err != nil {
		return err
	}
	files := []*scopeFile{}
	absent := []string{}
	defer func() {
		for _, f := range files {
			err = errors.Join(err, f.file.Close())
		}
	}()
	init, err := openScopeFile(ctx, root, journalName, maxJournalBytes, false)
	if err != nil {
		return err
	}
	files = append(files, init)
	events, err := parseInitializationJournal(init.data)
	if err != nil {
		return err
	}
	if len(events) != 2 {
		return ErrInitializationPending
	}
	view := JobScope{LibraryID: events[0].LibraryID, Catalog: "busy"}
	lock, err := openRegular(root, lockName, os.O_RDONLY)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Close()) }()
	locked := lockState(lock, false)
	if locked != nil && !errors.Is(locked, ErrBusy) {
		return locked
	}
	if locked == nil {
		defer func() { err = errors.Join(err, unlockState(lock)) }()
		data := map[string][]byte{}
		for _, spec := range []struct {
			name string
			max  int
		}{{operationsName, maxOperations}, {holdingsName, maxHoldings}, {narrationName, maxNarrationBytes}} {
			if err := ctx.Err(); err != nil {
				return err
			}
			f, err := openScopeFile(ctx, root, spec.name, spec.max, true)
			if err != nil {
				return err
			}
			if f != nil {
				files = append(files, f)
				data[spec.name] = f.data
			} else {
				absent = append(absent, spec.name)
			}
		}
		_, holds, err := parseManagedJournals(data[operationsName], data[holdingsName], view.LibraryID)
		if err != nil {
			return err
		}
		if _, err := parseNarrationJournal(data[narrationName], view.LibraryID); err != nil {
			return err
		}
		view.Catalog, view.Holdings = "observed", []Holding{}
		for _, h := range latestHoldings(holds) {
			view.Holdings = append(view.Holdings, h)
		}
		sort.Slice(view.Holdings, func(i, j int) bool { return view.Holdings[i].ID < view.Holdings[j].ID })
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := use(view); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// The initialization journal is immutable after completion. Re-read its
	// exact opened handle so even an in-place identity change invalidates use.
	b := make([]byte, len(init.data)+1)
	n, readErr := init.file.ReadAt(b, 0)
	if (readErr != nil && !errors.Is(readErr, io.EOF)) || !bytes.Equal(b[:n], init.data) {
		return errors.Join(ErrStateReview, readErr)
	}
	for _, f := range files {
		if err := f.unchanged(root); err != nil {
			return err
		}
	}
	for _, name := range absent {
		if _, err := root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
			return errors.Join(ErrStateReview, err)
		}
	}
	if err := sameEntry(root, lockName, lock); err != nil {
		return err
	}
	if err := validateControl(root); err != nil {
		return err
	}
	// Keep parent aliases supported, but reject replacement of the selected
	// root or control directory during this observation interval.
	named, err := parent.Lstat(".nemalo")
	if err != nil {
		return err
	}
	opened, err := root.Stat(".")
	if err != nil || !named.IsDir() || named.Mode()&os.ModeSymlink != 0 || !os.SameFile(named, opened) {
		return errors.Join(ErrStateReview, err)
	}
	named, err = safeio.Lstat(abs)
	if err != nil {
		return err
	}
	opened, err = parent.Stat(".")
	if err != nil || !named.IsDir() || named.Mode()&os.ModeSymlink != 0 || !os.SameFile(named, opened) {
		return errors.Join(ErrStateReview, err)
	}
	return nil
}

type scopeFile struct {
	name   string
	file   *os.File
	before os.FileInfo
	data   []byte
}

func openScopeFile(ctx context.Context, root *os.Root, name string, max int, optional bool) (*scopeFile, error) {
	info, err := root.Lstat(name)
	if optional && errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !info.Mode().IsRegular() || (info.Size() == 0 && name != narrationName) || info.Size() > int64(max) {
		return nil, errors.Join(ErrStateReview, err)
	}
	f, err := openRegular(root, name, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(importReader{ctx: ctx, r: f}, int64(max)+1))
	if readErr != nil || int64(len(data)) != info.Size() || (len(data) > 0 && data[len(data)-1] != '\n') {
		return nil, errors.Join(ErrStateReview, readErr, f.Close())
	}
	v := &scopeFile{name: name, file: f, before: info, data: data}
	if err := v.unchanged(root); err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return v, nil
}

func (f *scopeFile) unchanged(root *os.Root) error {
	if err := sameEntry(root, f.name, f.file); err != nil {
		return err
	}
	after, err := f.file.Stat()
	if err != nil || !os.SameFile(f.before, after) || after.Size() != f.before.Size() || !after.ModTime().Equal(f.before.ModTime()) {
		return errors.Join(ErrStateReview, err)
	}
	return nil
}
