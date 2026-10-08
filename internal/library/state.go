package library

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const journalName = "journal.jsonl"
const lockName = "writer.lock"
const maxJournalBytes = 64 << 10

var ErrBusy = errors.New("library state is busy; retry after the active operation")
var ErrStateReview = errors.New("library control state needs review; preserve .nemalo and do not remove its lock file")
var ErrInitializationPending = errors.New("initialization is pending; repeat library init with this explicit root")
var errNoControl = errors.New("no library control directory")

// State describes control metadata only, never content eligibility or integrity.
type State struct {
	Root           string     `json:"root"`
	LibraryID      string     `json:"library_id,omitempty"`
	Status         string     `json:"status"`
	InitializedAt  *time.Time `json:"initialized_at,omitempty"`
	JournalRecords int        `json:"journal_records"`
	Created        bool       `json:"created"`
	Resumed        bool       `json:"resumed"`
	Durability     string     `json:"durability"`
}

type journalEvent struct {
	SchemaVersion int       `json:"schema_version"`
	Sequence      int       `json:"sequence"`
	LibraryID     string    `json:"library_id"`
	OperationID   string    `json:"operation_id"`
	Event         string    `json:"event"`
	At            time.Time `json:"at"`
}

type control struct {
	root *os.Root
	lock *os.File
}

func (c *control) close() error {
	return errors.Join(unlockState(c.lock), c.lock.Close(), c.root.Close())
}

func stateReport(root string) State {
	return State{Root: root, Status: "not_initialized", Durability: "journal files synced on writes; directory-entry power-loss durability depends on filesystem"}
}

// LibraryStatus reads only .nemalo and uses a shared nonblocking OS lock. It does
// not create directories/files, read content, or repair incomplete records.
func LibraryStatus(ctx context.Context, directory string) (report State, err error) {
	if directory == "" {
		return report, errors.New("library status requires an explicit existing directory")
	}
	abs, err := filepath.Abs(directory)
	if err != nil {
		return report, err
	}
	report = stateReport(abs)
	if err = ctx.Err(); err != nil {
		return report, err
	}
	c, _, err := openControl(abs, false)
	if errors.Is(err, errNoControl) {
		return report, nil
	}
	if err != nil {
		report.Status = "unavailable"
		if errors.Is(err, ErrStateReview) {
			report.Status = "needs_review"
		}
		if errors.Is(err, ErrBusy) {
			report.Status = "busy"
		}
		return report, err
	}
	defer func() { err = errors.Join(err, c.close()) }()
	f, err := openRegular(c.root, journalName, os.O_RDONLY)
	if err != nil {
		report.Status = "needs_review"
		return report, errors.Join(ErrStateReview, err)
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	events, err := readJournal(f)
	if err != nil {
		report.Status = "needs_review"
		return report, err
	}
	applyEvents(&report, events)
	if err = ctx.Err(); err != nil {
		return report, err
	}
	if len(events) == 1 {
		return report, ErrInitializationPending
	}
	return report, nil
}

// Initialize creates control metadata inside an existing, explicitly selected
// directory. A valid started initialization can resume; corrupt state is retained.
func Initialize(ctx context.Context, directory string) (State, error) {
	return initialize(ctx, directory, nil)
}

func initialize(ctx context.Context, directory string, checkpoint func(string) error) (report State, err error) {
	if directory == "" {
		return report, errors.New("library init requires an explicit existing directory")
	}
	abs, err := filepath.Abs(directory)
	if err != nil {
		return report, err
	}
	report = stateReport(abs)
	if err = ctx.Err(); err != nil {
		return report, err
	}
	c, created, err := openControl(abs, true)
	if err != nil {
		report.Status = "unavailable"
		if errors.Is(err, ErrStateReview) {
			report.Status = "needs_review"
		}
		if errors.Is(err, ErrBusy) {
			report.Status = "busy"
		}
		return report, err
	}
	report.Created = created
	report.Status = "initialization_pending"
	defer func() { err = errors.Join(err, c.close()) }()
	flags := os.O_RDWR | os.O_APPEND
	if created {
		flags |= os.O_CREATE | os.O_EXCL
	}
	f, err := openRegular(c.root, journalName, flags)
	if err != nil {
		report.Status = "needs_review"
		return report, errors.Join(ErrStateReview, err)
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	var events []journalEvent
	if created {
		e := journalEvent{SchemaVersion: 1, Sequence: 1, LibraryID: newStateID("library"), OperationID: newStateID("operation"), Event: "initialization_started", At: time.Now().UTC()}
		if err = appendEvent(ctx, c, f, e); err != nil {
			return report, err
		}
		events = []journalEvent{e}
		applyEvents(&report, events)
		if checkpoint != nil {
			if err = checkpoint("started"); err != nil {
				return report, err
			}
		}
	} else {
		events, err = readJournal(f)
		if err != nil {
			report.Status = "needs_review"
			return report, err
		}
		applyEvents(&report, events)
	}
	if len(events) == 1 {
		e := events[0]
		e.Sequence, e.Event, e.At = 2, "initialization_completed", time.Now().UTC()
		if err = appendEvent(ctx, c, f, e); err != nil {
			return report, err
		}
		events = append(events, e)
		report.Resumed = !created
	} else {
		// Repeating init is idempotent and flushes already valid journal bytes.
		if err = ctx.Err(); err != nil {
			return report, err
		}
		if err = f.Sync(); err != nil {
			return report, err
		}
	}
	applyEvents(&report, events)
	return report, nil
}

func applyEvents(s *State, events []journalEvent) {
	s.LibraryID, s.JournalRecords = events[0].LibraryID, len(events)
	s.Status = "initialization_pending"
	s.InitializedAt = nil
	if len(events) == 2 {
		at := events[1].At
		s.Status, s.InitializedAt = "initialized", &at
	}
}

func newStateID(prefix string) string {
	return prefix + ":" + hex.EncodeToString(randomID())
}

func validStateID(value, prefix string) bool {
	if len(value) != len(prefix)+33 || value[:len(prefix)+1] != prefix+":" {
		return false
	}
	b, err := hex.DecodeString(value[len(prefix)+1:])
	return err == nil && len(b) == 16 && value == prefix+":"+hex.EncodeToString(b)
}

func readJournal(f *os.File) ([]journalEvent, error) {
	b, err := io.ReadAll(io.LimitReader(f, maxJournalBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) == 0 || len(b) > maxJournalBytes || b[len(b)-1] != '\n' {
		return nil, ErrStateReview
	}
	lines := bytes.Split(b[:len(b)-1], []byte{'\n'})
	if len(lines) < 1 || len(lines) > 2 {
		return nil, ErrStateReview
	}
	events := make([]journalEvent, 0, len(lines))
	for i, line := range lines {
		var e journalEvent
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, errors.Join(ErrStateReview, err)
		}
		canonical, err := json.Marshal(e)
		// Canonical encoding rejects duplicate/unknown fields and trailing data.
		if err != nil || !bytes.Equal(canonical, line) || e.SchemaVersion != 1 || e.Sequence != i+1 || !validStateID(e.LibraryID, "library") || !validStateID(e.OperationID, "operation") || e.At.IsZero() {
			return nil, ErrStateReview
		}
		if i == 0 && e.Event != "initialization_started" {
			return nil, ErrStateReview
		}
		if i == 1 && (e.Event != "initialization_completed" || e.LibraryID != events[0].LibraryID || e.OperationID != events[0].OperationID) {
			return nil, ErrStateReview
		}
		events = append(events, e)
	}
	return events, nil
}

func appendEvent(ctx context.Context, c *control, f *os.File, event journalEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateControl(c.root); err != nil {
		return err
	}
	if err := sameEntry(c.root, journalName, f); err != nil {
		return err
	}
	if err := sameEntry(c.root, lockName, c.lock); err != nil {
		return err
	}
	b, err := json.Marshal(event)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = f.Write(b)
	if err != nil {
		return err
	}
	return f.Sync()
}

func openControl(directory string, write bool) (*control, bool, error) {
	parent, err := os.OpenRoot(directory)
	if err != nil {
		return nil, false, fmt.Errorf("explicit library root: %w", err)
	}
	defer parent.Close()
	created := false
	info, err := parent.Lstat(".nemalo")
	if errors.Is(err, os.ErrNotExist) {
		if !write {
			return nil, false, errNoControl
		}
		if err = parent.Mkdir(".nemalo", 0700); err != nil {
			return nil, false, err
		}
		created = true
	} else if err != nil {
		return nil, false, err
	} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, false, ErrStateReview
	}
	root, err := parent.OpenRoot(".nemalo")
	if err != nil {
		return nil, false, err
	}
	if !created {
		if err = validateControl(root); err != nil {
			_ = root.Close()
			return nil, false, err
		}
	}
	flags := os.O_RDONLY
	if write {
		flags = os.O_RDWR
	}
	if created {
		flags |= os.O_CREATE | os.O_EXCL
	}
	lock, err := openRegular(root, lockName, flags)
	if err != nil {
		_ = root.Close()
		return nil, false, errors.Join(ErrStateReview, err)
	}
	if err = lockState(lock, write); err != nil {
		_ = lock.Close()
		_ = root.Close()
		return nil, false, err
	}
	if err = sameEntry(root, lockName, lock); err != nil {
		_ = lock.Close()
		_ = root.Close()
		return nil, false, err
	}
	return &control{root, lock}, created, nil
}

func validateControl(root *os.Root) error {
	f, err := root.Open(".")
	if err != nil {
		return err
	}
	defer f.Close()
	entries, err := f.ReadDir(3)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(entries) != 2 {
		return ErrStateReview
	}
	names := map[string]bool{lockName: true, journalName: true}
	for _, e := range entries {
		if !names[e.Name()] || !e.Type().IsRegular() {
			return ErrStateReview
		}
		delete(names, e.Name())
	}
	return nil
}

func openRegular(root *os.Root, name string, flags int) (*os.File, error) {
	before, err := root.Lstat(name)
	if err == nil && !before.Mode().IsRegular() {
		return nil, ErrStateReview
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	f, err := root.OpenFile(name, flags, 0600)
	if err != nil {
		return nil, err
	}
	if err = sameEntry(root, name, f); err != nil {
		_ = f.Close()
		return nil, err
	}
	if before != nil {
		after, err := f.Stat()
		if err != nil || !os.SameFile(before, after) {
			_ = f.Close()
			return nil, errors.Join(ErrStateReview, err)
		}
	}
	return f, nil
}

func sameEntry(root *os.Root, name string, f *os.File) error {
	entry, err := root.Lstat(name)
	if err != nil {
		return err
	}
	opened, err := f.Stat()
	if err != nil {
		return err
	}
	if !entry.Mode().IsRegular() || !opened.Mode().IsRegular() || !os.SameFile(entry, opened) {
		return ErrStateReview
	}
	if name == lockName && opened.Size() != 0 {
		return ErrStateReview
	}
	return singleLink(f)
}
