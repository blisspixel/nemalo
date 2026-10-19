package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/blisspixel/nemalo/internal/safeio"
)

// RestoreRequest returns the files of one completed cleanup from the library's
// holding area to their original folder. Without Apply nothing is written.
type RestoreRequest struct {
	Library   string
	CleanupID string
	Apply     bool
	// Stop closes a pending restore that cannot finish; files not yet
	// restored stay held.
	Stop bool
}

// RestoreFile is one planned file of the cleanup. Disposition before applying
// is restore, already_present (the folder has the same bytes), conflict (the
// folder has different bytes and a held copy exists), not_held (no held copy:
// the file was not moved), purged (library purge removed the held copy), or
// held_copy_changed (the held copy no longer has the planned bytes). Outcome
// after applying is restored or the same values.
type RestoreFile struct {
	Path        string `json:"path"`
	Bytes       int64  `json:"bytes"`
	Disposition string `json:"disposition"`
	Outcome     string `json:"outcome,omitempty"`
}

type RestoreResult struct {
	SchemaVersion int           `json:"schema_version"`
	Applied       bool          `json:"applied"`
	Resumed       bool          `json:"resumed"`
	Library       string        `json:"library"`
	LibraryID     string        `json:"library_id,omitempty"`
	CleanupID     string        `json:"cleanup_id"`
	Folder        string        `json:"folder"`
	Files         []RestoreFile `json:"files"`
	Restore       int           `json:"restore"`
	Present       int           `json:"already_present"`
	Conflicts     int           `json:"conflicts"`
	NotHeld       int           `json:"not_held"`
	Purged        int           `json:"purged"`
	Changed       int           `json:"held_copy_changed"`
	Blocked       int           `json:"blocked"`
	Limitations   []string      `json:"limitations"`
}

// Restore copies held files back without replacing anything: a path that holds
// different bytes is a conflict and its held copy stays. A held copy is removed
// only after a separate folder copy is verified. It can be repeated.
func Restore(ctx context.Context, request RestoreRequest) (result RestoreResult, err error) {
	result = RestoreResult{SchemaVersion: 1, CleanupID: request.CleanupID, Files: []RestoreFile{}, Limitations: []string{
		"Files return to their original paths in the cleanup's folder. An existing different file is never replaced; its held copy stays as a conflict.",
		"A held copy is removed only after the restored copy is verified as a separate file. Directory-entry durability depends on the filesystem.",
	}}
	if !validStateID(request.CleanupID, "cleanup") {
		return result, errors.New("restore requires the cleanup ID reported by library cleanup")
	}
	libraryRoot, err := filepath.Abs(request.Library)
	if err != nil {
		return result, err
	}
	result.Library = libraryRoot
	state, err := LibraryStatus(ctx, libraryRoot)
	if err != nil {
		return result, err
	}
	if state.Status != "initialized" {
		return result, errors.New("restore requires an initialized library")
	}
	result.LibraryID = state.LibraryID
	ctx, cancel := context.WithTimeout(ctx, 2*time.Hour)
	defer cancel()
	control, _, err := openControl(libraryRoot, request.Apply || request.Stop)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, control.close()) }()
	events, lines, err := lockedManaged(control, state.LibraryID)
	if err != nil {
		return result, err
	}
	if request.Apply {
		if _, _, err = rollForwardRescan(ctx, control, events, lines); err != nil {
			return result, err
		}
	}
	records, err := readCleanup(control, state.LibraryID)
	if err != nil {
		return result, err
	}
	var started *cleanupRecord
	completed := false
	for i := range records {
		r := records[i]
		if r.CleanupID != request.CleanupID {
			continue
		}
		switch r.Event {
		case "cleanup_started":
			started = &records[i]
		case "cleanup_completed":
			completed = true
		}
	}
	if started == nil || !completed {
		return result, errors.New("no completed cleanup has that ID; close a pending one with library cleanup --stop first")
	}
	open := openCleanup(records)
	if open != nil && (open.Event != "restore_started" || open.CleanupID != request.CleanupID) {
		return result, cleanupPendingError(open)
	}
	if request.Stop && (open == nil || open.Event != "restore_started") {
		return result, errors.New("no restore of that cleanup is pending")
	}
	// Files already restored by an interrupted restore are journaled.
	journaled := map[int]string{}
	if open != nil {
		result.Resumed = true
		for i := range records {
			if &records[i] == open {
				for _, r := range records[i+1:] {
					if r.Event == "restore_entry" {
						journaled[r.Index] = r.Outcome
					}
				}
			}
		}
	}
	// Held copies a purge removed are reported as purged, not as never held.
	purged := map[int]bool{}
	for _, r := range records {
		if r.CleanupID == request.CleanupID && r.Event == "purge_entry" && (r.Outcome == "purged" || r.Outcome == "already_absent") {
			purged[r.Index] = true
		}
	}
	store, err := safeio.OpenRoot(libraryRoot)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, store.Close()) }()
	m, err := readManifest(store, *started, state.LibraryID)
	if err != nil {
		return result, err
	}
	result.Folder = m.Folder
	if overlapping(libraryRoot, m.Folder) {
		return result, errors.New("the cleanup's folder now overlaps the library; nothing was restored")
	}
	if request.Stop {
		return stopRestore(ctx, control, records, m, journaled, result)
	}
	if request.Apply {
		if err := os.MkdirAll(m.Folder, 0o755); err != nil {
			return result, fmt.Errorf("recreate the cleanup folder: %w (close the restore with --stop)", err)
		}
	}
	var src *os.Root
	if r, err := safeio.OpenRoot(m.Folder); err == nil {
		src = r
		defer src.Close()
	} else if request.Apply || !errors.Is(err, os.ErrNotExist) {
		return result, fmt.Errorf("cleanup folder: %w", err)
	}
	if request.Apply && open == nil {
		if err := linksWork(src, "."); err != nil {
			return result, fmt.Errorf("the folder's filesystem does not support hard links, which restore needs: %w", err)
		}
	}
	if request.Apply && open == nil {
		count := 0
		for _, e := range m.Entries {
			if e.Disposition == "move" {
				count += len(e.Files)
			}
		}
		if err := journalRoom(control, count+2); err != nil {
			return result, err
		}
	}
	holding := holdingSlash(m.CleanupID)
	files := []CleanupFile{}
	for _, e := range m.Entries {
		if e.Disposition == "move" {
			files = append(files, e.Files...)
		}
	}
	for i, f := range files {
		d := journaled[i]
		if d == "" {
			if d, err = restoreDisposition(ctx, store, src, path.Join(holding, "files", f.Path), f); err != nil {
				return result, err
			}
			if d == "not_held" && purged[i] {
				d = "purged"
			}
		}
		result.Files = append(result.Files, RestoreFile{Path: f.Path, Bytes: f.Bytes, Disposition: d})
		countRestore(&result, d)
	}
	if !request.Apply {
		return result, nil
	}
	if open == nil {
		rec := cleanupRecord{SchemaVersion: 1, Sequence: len(records) + 1, LibraryID: state.LibraryID, CleanupID: m.CleanupID, Event: "restore_started", Count: len(files), At: now()}
		if err := appendCanonical(ctx, control, cleanupName, maxCleanupJournal, rec); err != nil {
			return result, err
		}
		records = append(records, rec)
	}
	for i, f := range files {
		if outcome, ok := journaled[i]; ok {
			result.Files[i].Outcome = outcome
			continue
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		outcome, err := restoreFile(ctx, store, src, path.Join(holding, "files", f.Path), f)
		if err != nil {
			return result, err
		}
		if outcome == "not_held" && purged[i] {
			outcome = "purged"
		}
		if err := cleanupStep("restored"); err != nil {
			return result, err
		}
		rec := cleanupRecord{SchemaVersion: 1, Sequence: len(records) + 1, LibraryID: state.LibraryID, CleanupID: m.CleanupID, Event: "restore_entry", Index: i, Outcome: outcome, At: now()}
		if err := appendCanonical(ctx, control, cleanupName, maxCleanupJournal, rec); err != nil {
			return result, err
		}
		records = append(records, rec)
		result.Files[i].Outcome = outcome
	}
	// After applying, the counts describe what happened, not the preview.
	result.Restore, result.Present, result.Conflicts, result.NotHeld, result.Changed, result.Blocked, result.Purged = 0, 0, 0, 0, 0, 0, 0
	for _, f := range result.Files {
		countRestore(&result, f.Outcome)
	}
	rec := cleanupRecord{SchemaVersion: 1, Sequence: len(records) + 1, LibraryID: state.LibraryID, CleanupID: m.CleanupID, Event: "restore_completed", Count: len(files), At: now()}
	if err := appendCanonical(ctx, control, cleanupName, maxCleanupJournal, rec); err != nil {
		return result, err
	}
	result.Applied = true
	return result, nil
}

func countRestore(r *RestoreResult, d string) {
	switch d {
	case "restore", "restored":
		r.Restore++
	case "already_present", "already_present_held_kept":
		r.Present++
	case "conflict":
		r.Conflicts++
	case "held_copy_changed":
		r.Changed++
	case "blocked", "stopped":
		r.Blocked++
	case "purged":
		r.Purged++
	default:
		r.NotHeld++
	}
}

// heldState reports whether the held copy exists with the planned bytes,
// exists with other bytes, or is absent, with its file info.
func heldState(ctx context.Context, store *os.Root, held string, f CleanupFile) (string, os.FileInfo, error) {
	ok, err := heldCopy(ctx, store, held, f)
	if errors.Is(err, ErrStateReview) {
		return "changed", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	if !ok {
		return "absent", nil, nil
	}
	parent, base, err := heldParent(store, held)
	if err != nil {
		return "", nil, err
	}
	defer parent.Close()
	info, err := parent.Lstat(base)
	return "same", info, err
}

// restoreDisposition compares the held copy and the folder path.
func restoreDisposition(ctx context.Context, store, src *os.Root, held string, f CleanupFile) (string, error) {
	inHolding, _, err := heldState(ctx, store, held, f)
	if err != nil {
		return "", err
	}
	target := "absent"
	if src != nil {
		if target, _, err = targetState(ctx, src, f); err != nil {
			return "", err
		}
	}
	switch {
	case inHolding == "changed":
		return "held_copy_changed", nil
	case target == "blocked":
		return "blocked", nil
	case target == "same":
		return "already_present", nil
	case inHolding == "absent":
		return "not_held", nil
	case target == "different":
		return "conflict", nil
	}
	return "restore", nil
}

// targetState reports whether the folder path is absent, holds the same
// bytes, or holds something else, with its file info.
func targetState(ctx context.Context, src *os.Root, f CleanupFile) (string, os.FileInfo, error) {
	name := filepath.FromSlash(f.Path)
	info, err := src.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return "absent", nil, nil
	}
	if err != nil {
		// A parent that is not a folder (or cannot be read) blocks this path.
		return "blocked", nil, nil
	}
	if !info.Mode().IsRegular() || info.Size() != f.Bytes {
		return "different", info, nil
	}
	h, err := safeio.OpenRegular(src, name, info, os.O_RDONLY)
	if err != nil {
		return "", nil, err
	}
	defer h.Close()
	ok, err := sameBytes(ctx, h, f)
	if err != nil {
		return "", nil, err
	}
	if ok {
		return "same", info, nil
	}
	return "different", info, nil
}

// restoreFile copies one held file to its folder path without replacing
// anything, then removes the held copy once a separate folder copy is
// verified. A restore partial has a random name; only one that is the same
// file as the restored file, left by an interrupted restore, is removed.
func restoreFile(ctx context.Context, store, src *os.Root, held string, f CleanupFile) (string, error) {
	inHolding, heldInfo, err := heldState(ctx, store, held, f)
	if err != nil {
		return "", err
	}
	if inHolding == "changed" {
		return "held_copy_changed", nil
	}
	target, targetInfo, err := targetState(ctx, src, f)
	if err != nil {
		return "", err
	}
	restored := false
	switch {
	case target == "blocked":
		return "blocked", nil
	case target == "different" && inHolding == "absent":
		return "not_held", nil
	case target == "different":
		return "conflict", nil
	case target == "absent" && inHolding == "absent":
		return "not_held", nil
	case target == "absent":
		// A path that cannot take the file (a file where a folder was, a link)
		// blocks only this file; its held copy stays.
		if err := mkdirSlash(src, path.Dir(f.Path)); err != nil {
			return "blocked", nil
		}
		partial := filepath.FromSlash(f.Path + ".nemalo-restore-" + newStateID("r")[len("r:"):])
		in, err := openHeld(store, held, heldInfo)
		if err != nil {
			return "", err
		}
		ok, err := copyVerified(ctx, in, src, partial, f)
		_ = in.Close()
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("%w: held copy of %s changed during restore", ErrStateReview, f.Path)
		}
		if err := src.Link(partial, filepath.FromSlash(f.Path)); err != nil {
			_ = src.Remove(partial)
			if errors.Is(err, os.ErrExist) {
				return "conflict", nil
			}
			return "blocked", nil
		}
		if err := cleanupStep("linked"); err != nil {
			return "", err
		}
		if err := src.Remove(partial); err != nil {
			return "", err
		}
		if target, targetInfo, err = targetState(ctx, src, f); err != nil || target != "same" {
			return "", errors.Join(err, fmt.Errorf("restored %s could not be verified", f.Path))
		}
		restored = true
	}
	removeRestorePartials(src, f.Path, targetInfo)
	if inHolding == "same" {
		// Never remove a held copy that is the folder file itself; it stays.
		if heldInfo == nil || targetInfo == nil || os.SameFile(heldInfo, targetInfo) {
			return "already_present_held_kept", nil
		}
		if err := removeHeld(store, held, heldInfo); err != nil {
			return "", err
		}
	}
	if restored {
		return "restored", nil
	}
	return "already_present", nil
}

// removeRestorePartials removes leftovers of an interrupted restore next to
// rel: names with the restore suffix that are the same file as the restored
// file. Anything else with that name pattern is the user's and stays.
func removeRestorePartials(src *os.Root, rel string, target os.FileInfo) {
	if target == nil {
		return
	}
	dir, base := path.Split(rel)
	d, err := src.Open(filepath.FromSlash(path.Clean("./" + dir)))
	if err != nil {
		return
	}
	names, err := d.Readdirnames(-1)
	_ = d.Close()
	if err != nil {
		return
	}
	for _, n := range names {
		if !strings.HasPrefix(n, base+".nemalo-restore-") {
			continue
		}
		name := filepath.FromSlash(path.Join(dir, n))
		if info, err := src.Lstat(name); err == nil && os.SameFile(info, target) {
			_ = src.Remove(name)
		}
	}
}

// stopRestore closes a pending restore: files not yet journaled are recorded as
// stopped and stay held.
func stopRestore(ctx context.Context, control *control, records []cleanupRecord, m cleanupManifest, journaled map[int]string, result RestoreResult) (RestoreResult, error) {
	files := []CleanupFile{}
	for _, e := range m.Entries {
		if e.Disposition == "move" {
			files = append(files, e.Files...)
		}
	}
	for i, f := range files {
		outcome, ok := journaled[i]
		if !ok {
			outcome = "stopped"
			rec := cleanupRecord{SchemaVersion: 1, Sequence: len(records) + 1, LibraryID: m.LibraryID, CleanupID: m.CleanupID, Event: "restore_entry", Index: i, Outcome: outcome, At: now()}
			if err := appendCanonical(ctx, control, cleanupName, maxCleanupJournal, rec); err != nil {
				return result, err
			}
			records = append(records, rec)
		}
		result.Files = append(result.Files, RestoreFile{Path: f.Path, Bytes: f.Bytes, Disposition: outcome, Outcome: outcome})
		countRestore(&result, outcome)
	}
	rec := cleanupRecord{SchemaVersion: 1, Sequence: len(records) + 1, LibraryID: m.LibraryID, CleanupID: m.CleanupID, Event: "restore_completed", Count: len(files), At: now()}
	if err := appendCanonical(ctx, control, cleanupName, maxCleanupJournal, rec); err != nil {
		return result, err
	}
	result.Applied = true
	return result, nil
}
