package library

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/safeio"
	"github.com/blisspixel/nemalo/internal/staging"
)

// Cleanup moves source files whose exact bytes the library already holds out
// of an explicit folder into a recoverable holding area inside the library:
// <library>/cleanup/<id>/files/<folder-relative path>. Nothing is deleted
// outright. A dry run lists every entry with a disposition and reason and a
// plan digest; applying requires that digest, plans again under the writer
// lock, and refuses if anything changed. Every move copies through opened
// handles, verifies the copy, and removes the source only afterwards, so the
// same steps hold within and across filesystems. A manifest and a journal
// (.nemalo/cleanup.jsonl) let an interrupted cleanup or restore finish later.

const (
	cleanupName       = "cleanup.jsonl"
	maxCleanupJournal = 32 << 20
	maxManifest       = 16 << 20
	maxPlanHashBytes  = 64 << 30
	cleanupDir        = "cleanup"
)

// CleanupFile is one file a move covers, with the identity it must still have.
type CleanupFile struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// CleanupEntry is one folder entry: a loose file, an intake packet's parts, an
// archive, or anything else. Only "move" entries are changed.
type CleanupEntry struct {
	Path        string        `json:"path"`
	Kind        string        `json:"kind"`
	Disposition string        `json:"disposition"`
	Reason      string        `json:"reason"`
	Files       []CleanupFile `json:"files,omitempty"`
	Holdings    []string      `json:"holdings,omitempty"`
	Outcome     string        `json:"outcome,omitempty"`
}

type CleanupRequest struct {
	Library string
	Folder  string
	// Apply is the plan digest from a dry run; empty means dry run.
	Apply string
	// Stop closes a pending cleanup that cannot finish, recording every entry
	// not yet moved as stopped, so its held files can be restored.
	Stop bool
}

type CleanupResult struct {
	SchemaVersion int            `json:"schema_version"`
	Applied       bool           `json:"applied"`
	Resumed       bool           `json:"resumed"`
	Library       string         `json:"library"`
	LibraryID     string         `json:"library_id,omitempty"`
	Folder        string         `json:"folder"`
	Digest        string         `json:"plan_digest"`
	CleanupID     string         `json:"cleanup_id,omitempty"`
	HoldingArea   string         `json:"holding_area,omitempty"`
	Entries       []CleanupEntry `json:"entries"`
	Move          int            `json:"move"`
	Keep          int            `json:"keep"`
	MoveBytes     int64          `json:"move_bytes"`
	Moved         int            `json:"moved"`
	NotMoved      int            `json:"not_moved"`
	Limitations   []string       `json:"limitations"`
}

// cleanupManifest is written, synced, and bound by hash into the journal
// before the first move. Resume and restore read only this, never a new plan.
type cleanupManifest struct {
	SchemaVersion int            `json:"schema_version"`
	CleanupID     string         `json:"cleanup_id"`
	LibraryID     string         `json:"library_id"`
	Folder        string         `json:"folder"`
	Digest        string         `json:"plan_digest"`
	Entries       []CleanupEntry `json:"entries"`
}

// cleanupRecord is one line of .nemalo/cleanup.jsonl. Operations are
// cleanup_started, cleanup_entry*, cleanup_completed, and later for the same
// cleanup restore_started, restore_entry*, restore_completed.
type cleanupRecord struct {
	SchemaVersion int    `json:"schema_version"`
	Sequence      int    `json:"sequence"`
	LibraryID     string `json:"library_id"`
	CleanupID     string `json:"cleanup_id"`
	Event         string `json:"event"`
	Folder        string `json:"folder,omitempty"`
	Digest        string `json:"plan_digest,omitempty"`
	Manifest      string `json:"manifest_sha256,omitempty"`
	Index         int    `json:"index"`
	Outcome       string `json:"outcome,omitempty"`
	Count         int    `json:"count"`
	At            string `json:"at"`
}

func cleanupLimitations() []string {
	return []string{
		"Only files whose exact bytes the library holds, with stored bytes verified, move. Everything else stays, with its reason.",
		"Moved sources go to the library's cleanup holding area and can be restored. Nothing is purged; remove a holding area yourself only when you no longer need it.",
		"An archive moves only when every member is a directory or a held file. Intake packets move as their receipt, intent, and content.",
		"Directory-entry durability across power loss depends on the filesystem.",
	}
}

// Cleanup plans, or with Apply moves, the held sources of one folder.
func Cleanup(ctx context.Context, request CleanupRequest) (result CleanupResult, err error) {
	result = CleanupResult{SchemaVersion: 1, Entries: []CleanupEntry{}, Limitations: cleanupLimitations()}
	libraryRoot, err := filepath.Abs(request.Library)
	if err != nil {
		return result, err
	}
	folder, err := filepath.Abs(request.Folder)
	if err != nil {
		return result, err
	}
	result.Library, result.Folder = libraryRoot, folder
	if !request.Stop && overlapping(libraryRoot, folder) {
		return result, errors.New("the folder and the library must not contain each other")
	}
	if request.Apply != "" && !validHash(request.Apply) {
		return result, errors.New("apply takes the plan digest printed by the dry run")
	}
	state, err := LibraryStatus(ctx, libraryRoot)
	if err != nil {
		return result, err
	}
	if state.Status != "initialized" {
		return result, errors.New("cleanup requires an initialized library; run library init on this explicit directory")
	}
	result.LibraryID = state.LibraryID
	ctx, cancel := context.WithTimeout(ctx, 2*time.Hour)
	defer cancel()
	write := request.Apply != "" || request.Stop
	control, _, err := openControl(libraryRoot, write)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, control.close()) }()
	events, lines, err := lockedManaged(control, state.LibraryID)
	if err != nil {
		return result, err
	}
	if write {
		// Every writer first completes an interrupted rescan.
		if _, lines, err = rollForwardRescan(ctx, control, events, lines); err != nil {
			return result, err
		}
	}
	records, err := readCleanup(control, state.LibraryID)
	if err != nil {
		return result, err
	}
	store, err := safeio.OpenRoot(libraryRoot)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, store.Close()) }()
	open := openCleanup(records)
	if request.Stop {
		if open == nil || open.Event != "cleanup_started" {
			return result, errors.New("no cleanup is pending")
		}
		manifest, err := readManifest(store, *open, state.LibraryID)
		if err != nil {
			return result, err
		}
		return stopCleanup(ctx, control, records, manifest, result)
	}
	if open != nil {
		if open.Event == "restore_started" {
			return result, fmt.Errorf("restore of %s is pending; finish it first with library restore --cleanup %s --apply", open.CleanupID, open.CleanupID)
		}
		if !samePath(open.Folder, folder) || (request.Apply != "" && open.Digest != request.Apply) {
			return result, fmt.Errorf("cleanup %s is pending for %s; finish it with the same folder and --apply %s, or close it with --stop", open.CleanupID, open.Folder, open.Digest)
		}
		if request.Apply == "" {
			result.CleanupID, result.Digest = open.CleanupID, open.Digest
			return result, fmt.Errorf("cleanup %s is pending for this folder; finish it with --apply %s", open.CleanupID, open.Digest)
		}
		manifest, err := readManifest(store, *open, state.LibraryID)
		if err != nil {
			return result, err
		}
		result.Resumed = true
		return runCleanup(ctx, control, store, records, manifest, result)
	}
	src, err := safeio.OpenRoot(folder)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, src.Close()) }()
	entries, err := planCleanup(ctx, store, src, lines)
	if err != nil {
		return result, err
	}
	result.Entries, result.Digest = entries, planDigest(folder, entries)
	for _, e := range entries {
		if e.Disposition == "move" {
			result.Move++
			for _, f := range e.Files {
				result.MoveBytes += f.Bytes
			}
		} else {
			result.Keep++
		}
	}
	if request.Apply == "" {
		return result, nil
	}
	if request.Apply != result.Digest {
		return result, errors.New("the plan changed since the dry run; nothing was moved. Review the new plan and apply its digest")
	}
	if result.Move == 0 {
		result.Applied = true
		return result, nil
	}
	manifest := cleanupManifest{SchemaVersion: 1, CleanupID: newStateID("cleanup"), LibraryID: state.LibraryID, Folder: folder, Digest: result.Digest, Entries: entries}
	// Moves publish held copies by hard link and restore needs links in the
	// folder; a filesystem without them is refused before anything starts.
	if err := linksWork(store, holdingSlash(manifest.CleanupID)); err != nil {
		return result, fmt.Errorf("the library filesystem does not support hard links, which cleanup needs: %w", err)
	}
	if err := linksWork(src, "."); err != nil {
		return result, fmt.Errorf("the folder's filesystem does not support hard links, which restore needs: %w", err)
	}
	// The journal must have room for this cleanup and one restore of it, so
	// neither can be left pending by a full journal.
	files := 0
	for _, e := range entries {
		files += len(e.Files)
	}
	if err := journalRoom(control, result.Move+files+4); err != nil {
		return result, err
	}
	sum, err := writeManifest(store, manifest)
	if err != nil {
		return result, err
	}
	if err := cleanupStep("manifest"); err != nil {
		return result, err
	}
	started := cleanupRecord{SchemaVersion: 1, Sequence: len(records) + 1, LibraryID: state.LibraryID, CleanupID: manifest.CleanupID, Event: "cleanup_started", Folder: folder, Digest: manifest.Digest, Manifest: sum, Count: len(entries), At: now()}
	if err := appendCanonical(ctx, control, cleanupName, maxCleanupJournal, started); err != nil {
		return result, err
	}
	records = append(records, started)
	return runCleanup(ctx, control, store, records, manifest, result)
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// cleanupHook lets tests stop a cleanup at a named step, as a crash would. It is
// nil in production.
var cleanupHook func(stage string) error

func cleanupStep(stage string) error {
	if cleanupHook == nil {
		return nil
	}
	return cleanupHook(stage)
}

func samePath(a, b string) bool {
	return a == b || (strings.EqualFold(a, b) && sameImportSource(a, b))
}

// overlapping reports whether either directory contains the other. Both paths
// are compared as given and as resolved through links, junctions, and short
// names (case-insensitively where the platform's filesystems usually are), and
// each existing ancestor of one is compared by identity with the other.
func overlapping(a, b string) bool {
	ca, cb := canonicalPath(a), canonicalPath(b)
	return withinPath(a, b) || withinPath(b, a) || withinPath(ca, cb) || withinPath(cb, ca) ||
		ancestorIs(a, b) || ancestorIs(b, a) || ancestorIs(ca, cb) || ancestorIs(cb, ca)
}

// canonicalPath resolves the longest existing prefix of p and keeps the rest.
func canonicalPath(p string) string {
	p = filepath.Clean(p)
	rest := ""
	for q := p; ; {
		if r, err := filepath.EvalSymlinks(q); err == nil {
			if rest == "" {
				return r
			}
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(q)
		if parent == q {
			return p
		}
		rest = filepath.Join(filepath.Base(q), rest)
		q = parent
	}
}

func withinPath(root, target string) bool {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		root, target = strings.ToLower(root), strings.ToLower(target)
	}
	return within(root, target)
}

func ancestorIs(root, p string) bool {
	ri, err := os.Stat(root)
	if err != nil {
		return false
	}
	for q := p; ; q = filepath.Dir(q) {
		if qi, err := os.Stat(q); err == nil && os.SameFile(ri, qi) {
			return true
		}
		if filepath.Dir(q) == q {
			return false
		}
	}
}

// linksWork creates and links a probe file in dir under root and removes both.
func linksWork(root *os.Root, dir string) error {
	if err := mkdirSlash(root, dir); err != nil && dir != "." {
		return err
	}
	probe := filepath.FromSlash(path.Join(dir, ".nemalo-link-"+newStateID("probe")[len("probe:"):]))
	f, err := root.OpenFile(probe, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_ = f.Close()
	defer root.Remove(probe)
	if err := root.Link(probe, probe+"-link"); err != nil {
		return err
	}
	return root.Remove(probe + "-link")
}

// stopCleanup closes a pending cleanup without moving anything more.
func stopCleanup(ctx context.Context, control *control, records []cleanupRecord, m cleanupManifest, result CleanupResult) (CleanupResult, error) {
	result.CleanupID, result.Digest, result.Folder = m.CleanupID, m.Digest, m.Folder
	result.HoldingArea = filepath.Join(result.Library, cleanupDir, m.CleanupID[len("cleanup:"):])
	result.Entries = append([]CleanupEntry{}, m.Entries...)
	done := map[int]string{}
	for _, r := range records {
		if r.CleanupID == m.CleanupID && r.Event == "cleanup_entry" {
			done[r.Index] = r.Outcome
		}
	}
	for i, e := range m.Entries {
		if e.Disposition != "move" {
			continue
		}
		outcome, ok := done[i]
		if !ok {
			outcome = "stopped"
			rec := cleanupRecord{SchemaVersion: 1, Sequence: len(records) + 1, LibraryID: m.LibraryID, CleanupID: m.CleanupID, Event: "cleanup_entry", Index: i, Outcome: outcome, At: now()}
			if err := appendCanonical(ctx, control, cleanupName, maxCleanupJournal, rec); err != nil {
				return result, err
			}
			records = append(records, rec)
		}
		result.Entries[i].Outcome = outcome
		if outcome == "held" {
			result.Moved++
		} else {
			result.NotMoved++
		}
	}
	rec := cleanupRecord{SchemaVersion: 1, Sequence: len(records) + 1, LibraryID: m.LibraryID, CleanupID: m.CleanupID, Event: "cleanup_completed", Count: result.Moved, At: now()}
	if err := appendCanonical(ctx, control, cleanupName, maxCleanupJournal, rec); err != nil {
		return result, err
	}
	result.Applied = true
	result.Limitations = append(result.Limitations, "This cleanup was stopped. Files already held can be restored with library restore.")
	return result, nil
}

// runCleanup moves each planned entry not yet journaled, then completes.
func runCleanup(ctx context.Context, control *control, store *os.Root, records []cleanupRecord, m cleanupManifest, result CleanupResult) (CleanupResult, error) {
	result.CleanupID, result.Digest, result.Folder = m.CleanupID, m.Digest, m.Folder
	result.HoldingArea = filepath.Join(result.Library, cleanupDir, m.CleanupID[len("cleanup:"):])
	result.Entries = append([]CleanupEntry{}, m.Entries...)
	done := map[int]string{}
	for _, r := range records {
		if r.CleanupID == m.CleanupID && r.Event == "cleanup_entry" {
			done[r.Index] = r.Outcome
		}
	}
	// A folder removed since the cleanup began means every remaining source
	// is missing; held copies still count.
	src, err := safeio.OpenRoot(m.Folder)
	if errors.Is(err, os.ErrNotExist) {
		src = nil
	} else if err != nil {
		return result, fmt.Errorf("cleanup folder: %w (finish later, or close it with --stop)", err)
	} else {
		defer src.Close()
	}
	holding := holdingSlash(m.CleanupID)
	for i, e := range m.Entries {
		if e.Disposition != "move" {
			continue
		}
		if outcome, ok := done[i]; ok {
			result.Entries[i].Outcome = outcome
		} else {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			outcome, err := moveEntry(ctx, store, src, holding, e)
			if err != nil {
				return result, err
			}
			if err := cleanupStep("moved"); err != nil {
				return result, err
			}
			rec := cleanupRecord{SchemaVersion: 1, Sequence: len(records) + 1, LibraryID: m.LibraryID, CleanupID: m.CleanupID, Event: "cleanup_entry", Index: i, Outcome: outcome, At: now()}
			if err := appendCanonical(ctx, control, cleanupName, maxCleanupJournal, rec); err != nil {
				return result, err
			}
			records = append(records, rec)
			result.Entries[i].Outcome = outcome
		}
		if result.Entries[i].Outcome == "held" {
			result.Moved++
		} else {
			result.NotMoved++
		}
	}
	done2 := cleanupRecord{SchemaVersion: 1, Sequence: len(records) + 1, LibraryID: m.LibraryID, CleanupID: m.CleanupID, Event: "cleanup_completed", Count: result.Moved, At: now()}
	if err := appendCanonical(ctx, control, cleanupName, maxCleanupJournal, done2); err != nil {
		return result, err
	}
	result.Applied = true
	return result, nil
}

// holdingSlash is the cleanup's directory under the library root.
func holdingSlash(id string) string {
	return path.Join(cleanupDir, id[len("cleanup:"):])
}

// moveEntry moves every file of one entry. A file already in the holding area
// with the planned bytes is kept; a source that no longer has the planned bytes
// stays where it is. Outcomes: held, source_changed, source_missing.
func moveEntry(ctx context.Context, store, src *os.Root, holding string, e CleanupEntry) (string, error) {
	outcome := "held"
	for _, f := range e.Files {
		final := path.Join(holding, "files", f.Path)
		state, err := moveFile(ctx, store, src, final, f)
		if err != nil {
			return "", err
		}
		if state != "held" {
			// The rest of the entry stays with its source; held parts restore.
			outcome = state
			break
		}
	}
	if outcome == "held" && e.Kind == "packet" && e.Path != "." && src != nil {
		// The packet folder is removed only when it is now empty.
		_ = src.Remove(filepath.FromSlash(e.Path))
	}
	return outcome, nil
}

// moveFile is idempotent across interruptions: each step checks what an
// earlier, interrupted attempt left before acting.
func moveFile(ctx context.Context, store, src *os.Root, final string, f CleanupFile) (string, error) {
	name := filepath.FromSlash(f.Path)
	held, err := heldCopy(ctx, store, final, f)
	if err != nil {
		return "", err
	}
	partial := filepath.FromSlash(final + ".partial")
	if held {
		// The holding area is Nemalo's: a partial left after publishing is removed.
		if _, err := store.Lstat(partial); err == nil {
			if err := store.Remove(partial); err != nil {
				return "", err
			}
		}
	}
	missing := func() (string, error) {
		if held {
			return "held", nil
		}
		// Nothing to finish: a partial copy of a source now gone is removed.
		if _, err := store.Lstat(partial); err == nil {
			if err := store.Remove(partial); err != nil {
				return "", err
			}
		}
		return "source_missing", nil
	}
	if src == nil {
		return missing()
	}
	info, statErr := src.Lstat(name)
	switch {
	case errors.Is(statErr, os.ErrNotExist):
		return missing()
	case statErr != nil:
		return "", statErr
	case !info.Mode().IsRegular():
		return "source_changed", nil
	}
	in, err := safeio.OpenRegular(src, name, info, os.O_RDONLY)
	if err != nil {
		return "", err
	}
	defer in.Close()
	if !held {
		if err := mkdirSlash(store, path.Dir(final)); err != nil {
			return "", err
		}
		// A partial copy is only ever this operation's own unfinished work.
		if _, err := store.Lstat(partial); err == nil {
			if err := store.Remove(partial); err != nil {
				return "", err
			}
		}
		ok, err := copyVerified(ctx, in, store, partial, f)
		if err != nil {
			return "", err
		}
		if err := cleanupStep("partial"); err != nil {
			return "", err
		}
		if !ok {
			return "source_changed", nil
		}
		// Link refuses to replace an existing name, unlike rename.
		if err := store.Link(partial, filepath.FromSlash(final)); err != nil {
			return "", fmt.Errorf("publish holding copy: %w", err)
		}
		if err := cleanupStep("linked"); err != nil {
			return "", err
		}
		if err := store.Remove(partial); err != nil {
			return "", err
		}
		if err := cleanupStep("held"); err != nil {
			return "", err
		}
	} else if ok, err := sameBytes(ctx, in, f); err != nil || !ok {
		// The held copy has the planned bytes; the source now differs, so it stays.
		return "source_changed", err
	}
	// Remove the source only if the name still refers to the file just read.
	// A change in place after the copy was verified shows in size or time.
	current, err := src.Lstat(name)
	after, statErr := in.Stat()
	if err != nil || statErr != nil || !os.SameFile(info, current) || after.Size() != f.Bytes || !after.ModTime().Equal(info.ModTime()) {
		return "source_changed", nil
	}
	if err := in.Close(); err != nil {
		return "", err
	}
	if err := src.Remove(name); err != nil {
		// On Windows the read-only attribute blocks removal. It is cleared only
		// when set, and put back if the file still cannot be removed.
		if runtime.GOOS != "windows" || current.Mode().Perm()&0o200 != 0 {
			return "source_not_removed", nil
		}
		if src.Chmod(name, 0o600) != nil {
			return "source_not_removed", nil
		}
		if src.Remove(name) != nil {
			_ = src.Chmod(name, current.Mode().Perm())
			return "source_not_removed", nil
		}
	}
	return "held", nil
}

// heldCopy reports whether final holds exactly f. A different file there means
// the holding area was changed outside Nemalo, which needs review.
func heldCopy(ctx context.Context, store *os.Root, final string, f CleanupFile) (bool, error) {
	name := filepath.FromSlash(final)
	info, err := store.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Size() != f.Bytes {
		return false, fmt.Errorf("%w: holding copy %s is not the planned file", ErrStateReview, final)
	}
	h, err := safeio.OpenRegular(store, name, info, os.O_RDONLY)
	if err != nil {
		return false, err
	}
	defer h.Close()
	ok, err := sameBytes(ctx, h, f)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, fmt.Errorf("%w: holding copy %s is not the planned file", ErrStateReview, final)
	}
	return true, nil
}

func sameBytes(ctx context.Context, r io.ReaderAt, f CleanupFile) (bool, error) {
	sum := sha256.New()
	n, err := io.Copy(sum, io.LimitReader(importReader{ctx, io.NewSectionReader(r, 0, f.Bytes+1)}, f.Bytes+1))
	if err != nil {
		return false, err
	}
	return n == f.Bytes && hex.EncodeToString(sum.Sum(nil)) == f.SHA256, nil
}

// copyVerified copies in to a new file name in root, synced, and reports
// whether the bytes were the planned ones. A mismatch leaves nothing behind.
func copyVerified(ctx context.Context, in io.ReaderAt, root *os.Root, name string, f CleanupFile) (bool, error) {
	if err := safeio.EnsureSpaceIn(root, f.Bytes); err != nil {
		return false, err
	}
	out, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return false, err
	}
	sum := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(writeFault("cleanup copy", out), sum), io.LimitReader(importReader{ctx, io.NewSectionReader(in, 0, f.Bytes+1)}, f.Bytes+1))
	syncErr, closeErr := out.Sync(), out.Close()
	if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
		_ = root.Remove(name)
		return false, err
	}
	if n != f.Bytes || hex.EncodeToString(sum.Sum(nil)) != f.SHA256 {
		_ = root.Remove(name)
		return false, nil
	}
	return true, nil
}

// planCleanup decides every entry from a complete inventory of the folder and
// the library's current holdings, whose stored bytes are verified once each.
func planCleanup(ctx context.Context, store, src *os.Root, lines []Holding) ([]CleanupEntry, error) {
	inv, err := inventory.InspectRoot(ctx, src, "folder", inventory.Limits{Entries: 10000, Depth: 32, FileBytes: 256 << 20, TotalBytes: 1})
	if err != nil || !inv.Complete {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.Join(errors.New("the folder inventory is incomplete, so not every entry can be accounted for; nothing can be planned"), err)
	}
	// A folder holding another library's control state, or reaching this
	// library by any route, is never cleaned up.
	for _, e := range inv.Entries {
		for _, part := range strings.Split(e.Path, "/") {
			if part == ".nemalo" {
				return nil, errors.New("the folder contains a library's .nemalo control state; nothing can be planned")
			}
		}
	}
	byHash := map[string]Holding{}
	for _, h := range latestHoldings(lines) {
		byHash[h.SHA256] = h
	}
	verified := map[string]bool{}
	hashed := int64(0)
	held := func(sum string) (Holding, bool, error) {
		h, ok := byHash[sum]
		if !ok {
			return h, false, nil
		}
		if v, seen := verified[h.ID]; seen {
			return h, v, nil
		}
		err := hashAgrees(ctx, store, h.Path, h.Bytes, h.SHA256)
		if err != nil && !errors.Is(err, errStoredObject) && !errors.Is(err, os.ErrNotExist) {
			return h, false, err
		}
		verified[h.ID] = err == nil
		return h, err == nil, nil
	}
	hashFile := func(rel string) (CleanupFile, error) {
		f, err := fileIdentity(ctx, src, rel)
		if hashed += f.Bytes; hashed > maxPlanHashBytes {
			return f, errors.New("the folder holds more than 64 GiB of candidate files; plan a smaller folder")
		}
		return f, err
	}
	// Names that differ only by case cannot both live in a case-insensitive
	// holding area or library, so both stay.
	folded := map[string]int{}
	for _, e := range inv.Entries {
		folded[strings.ToLower(e.Path)]++
	}
	caseClash := func(rel string) bool { return folded[strings.ToLower(rel)] > 1 }
	present := map[string]inventory.Entry{}
	for _, e := range inv.Entries {
		present[e.Path] = e
	}
	regular := func(rel string) bool {
		e, ok := present[rel]
		return ok && e.Kind != "link" && e.Kind != "special" && e.Kind != "unreadable" && e.Kind != "directory" && e.Finding == ""
	}
	// Packets: a directory with receipt.json and exactly one content file.
	packets := map[string][]string{}
	for _, e := range inv.Entries {
		if path.Base(e.Path) != "receipt.json" || !regular(e.Path) {
			continue
		}
		dir := path.Dir(e.Path)
		contents := []string{}
		for _, c := range packetContents {
			if regular(path.Join(dir, c)) {
				contents = append(contents, path.Join(dir, c))
			}
		}
		if len(contents) != 1 {
			continue
		}
		parts := []string{contents[0], e.Path}
		if regular(path.Join(dir, "intent.json")) {
			parts = append(parts, path.Join(dir, "intent.json"))
		}
		packets[dir] = parts
	}
	inPacket := map[string]bool{}
	for _, parts := range packets {
		for _, p := range parts {
			inPacket[p] = true
		}
	}
	// An intent with content but no receipt is an interrupted acquisition. As in
	// ingest, it stays for review even when the same bytes are held elsewhere.
	incomplete := map[string]bool{}
	for _, e := range inv.Entries {
		dir := path.Dir(e.Path)
		if path.Base(e.Path) != "intent.json" || packets[dir] != nil {
			continue
		}
		for _, c := range packetContents {
			if _, ok := present[path.Join(dir, c)]; ok {
				incomplete[path.Join(dir, c)] = true
			}
		}
	}
	entries := []CleanupEntry{}
	dirs := make([]string, 0, len(packets))
	for dir := range packets {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		e := CleanupEntry{Path: dir, Kind: "packet", Disposition: "keep"}
		clash := false
		for _, p := range packets[dir] {
			clash = clash || caseClash(p)
		}
		if clash {
			e.Reason = "a packet file name differs only by case from another entry; preserved"
			entries = append(entries, e)
			continue
		}
		content, err := hashFile(packets[dir][0])
		if errors.Is(err, errTooLarge) {
			e.Reason = "the packet content is over the file limit; preserved"
			entries = append(entries, e)
			continue
		}
		if err != nil {
			return nil, err
		}
		h, ok, err := held(content.SHA256)
		if err != nil {
			return nil, err
		}
		if !ok {
			e.Reason = "the packet content is not held with verified bytes"
			entries = append(entries, e)
			continue
		}
		if !receiptBinds(src, packets[dir][1], content) {
			e.Reason = "receipt.json is not a completed Nemalo receipt for this content; preserved"
			entries = append(entries, e)
			continue
		}
		e.Files = []CleanupFile{content}
		for _, p := range packets[dir][1:] {
			f, err := hashFile(p)
			if err != nil {
				return nil, err
			}
			e.Files = append(e.Files, f)
		}
		e.Disposition, e.Reason, e.Holdings = "move", "the packet content is held; its records move with it", []string{h.ID}
		entries = append(entries, e)
	}
	for _, ie := range inv.Entries {
		if inPacket[ie.Path] {
			continue
		}
		e := CleanupEntry{Path: ie.Path, Kind: "file", Disposition: "keep"}
		switch {
		case !regular(ie.Path):
			e.Kind, e.Reason = "other", "not a regular file ("+firstNonEmpty(ie.Finding, ie.Kind)+"); preserved"
		case incomplete[ie.Path]:
			e.Reason = "content of an intake packet without a receipt; retained for review"
		case caseClash(ie.Path):
			e.Reason = "the name differs only by case from another entry; preserved"
		case strings.EqualFold(path.Ext(ie.Path), ".zip"):
			e.Kind = "archive"
			if err := planArchive(ctx, src, ie.Path, held, hashFile, &e); err != nil {
				return nil, err
			}
		case importFormat(ie.Path) != "":
			f, err := hashFile(ie.Path)
			if errors.Is(err, errTooLarge) {
				e.Reason = "over the file limit; preserved"
				break
			}
			if err != nil {
				return nil, err
			}
			h, ok, err := held(f.SHA256)
			if err != nil {
				return nil, err
			}
			if !ok {
				e.Reason = "these bytes are not held with verified stored bytes"
			} else {
				e.Disposition, e.Reason, e.Files, e.Holdings = "move", "held with verified stored bytes", []CleanupFile{f}, []string{h.ID}
			}
		default:
			e.Reason = "not a supported format; preserved"
		}
		entries = append(entries, e)
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

// planArchive moves a ZIP only when every member is a directory or extracts
// to bytes the library holds. Members are staged privately and removed.
func planArchive(ctx context.Context, src *os.Root, rel string, held func(string) (Holding, bool, error), hashFile func(string) (CleanupFile, error), e *CleanupEntry) error {
	// The archive's own bytes are fixed first; the same bytes must remain
	// after its members were checked.
	before, err := hashFile(rel)
	if errors.Is(err, errTooLarge) {
		e.Reason = "over the file limit; preserved"
		return nil
	}
	if err != nil {
		return err
	}
	if accounted, why := zipAccounted(src, rel); !accounted {
		e.Reason = why + "; the archive is preserved"
		return nil
	}
	tmp, err := os.MkdirTemp("", "nemalo-cleanup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	stage, err := safeio.OpenRoot(tmp)
	if err != nil {
		return err
	}
	defer stage.Close()
	x, err := staging.ExtractZip(ctx, src, filepath.FromSlash(rel), stage, supportedImport, staging.Defaults())
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		e.Reason = "the archive could not be read; preserved"
		return nil
	}
	ids := []string{}
	for _, m := range x.Members {
		switch m.Disposition {
		case "directory":
			continue
		case "extracted":
			f, err := fileIdentity(ctx, stage, m.Staged)
			if err != nil {
				return err
			}
			h, ok, err := held(f.SHA256)
			if err != nil {
				return err
			}
			if ok {
				ids = append(ids, h.ID)
				continue
			}
		}
		e.Reason = "member " + m.Name + " is not held; the archive is preserved"
		return nil
	}
	if len(ids) == 0 {
		e.Reason = "the archive holds no book the library has; preserved"
		return nil
	}
	after, err := fileIdentity(ctx, src, rel)
	if err != nil {
		return err
	}
	if after != before {
		e.Reason = "the archive changed while it was checked; preserved"
		return nil
	}
	e.Disposition, e.Reason, e.Files, e.Holdings = "move", "every member is held with verified stored bytes", []CleanupFile{before}, ids
	return nil
}

// fileIdentity hashes one regular file through an opened handle.
func fileIdentity(ctx context.Context, root *os.Root, rel string) (CleanupFile, error) {
	name := filepath.FromSlash(rel)
	info, err := root.Lstat(name)
	if err != nil {
		return CleanupFile{}, err
	}
	if !info.Mode().IsRegular() {
		return CleanupFile{}, fmt.Errorf("%s is not a regular file", rel)
	}
	// A ZIP or any other file is held to the parsed-format limit.
	if info.Size() > assessment.MaxFileBytes(importFormat(rel)) {
		return CleanupFile{Path: filepath.ToSlash(rel)}, errTooLarge
	}
	f, err := safeio.OpenRegular(root, name, info, os.O_RDONLY)
	if err != nil {
		return CleanupFile{}, err
	}
	defer f.Close()
	sum := sha256.New()
	n, err := io.Copy(sum, io.LimitReader(importReader{ctx, f}, info.Size()+1))
	if err != nil {
		return CleanupFile{}, err
	}
	if n != info.Size() {
		return CleanupFile{}, fmt.Errorf("%s changed while it was read", rel)
	}
	return CleanupFile{Path: filepath.ToSlash(rel), Bytes: n, SHA256: hex.EncodeToString(sum.Sum(nil))}, nil
}

// planDigest binds a plan to the folder and every entry's disposition and
// file identities.
func planDigest(folder string, entries []CleanupEntry) string {
	data, _ := json.Marshal(struct {
		Folder  string
		Entries []CleanupEntry
	}{folder, entries})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeManifest(store *os.Root, m cleanupManifest) (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	if len(data) > maxManifest {
		return "", errors.New("the cleanup plan is too large for one manifest; plan a smaller folder")
	}
	dir := holdingSlash(m.CleanupID)
	if err := mkdirSlash(store, dir); err != nil {
		return "", err
	}
	name := filepath.FromSlash(path.Join(dir, "manifest.json"))
	f, err := store.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	_, writeErr := f.Write(data)
	if err := errors.Join(writeErr, f.Sync(), f.Close()); err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// readManifest reads the manifest a started record names and checks its hash.
func readManifest(store *os.Root, started cleanupRecord, libraryID string) (cleanupManifest, error) {
	var m cleanupManifest
	name := filepath.FromSlash(path.Join(holdingSlash(started.CleanupID), "manifest.json"))
	info, err := store.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxManifest {
		return m, fmt.Errorf("%w: cleanup manifest missing or not a regular file", ErrStateReview)
	}
	f, err := safeio.OpenRegular(store, name, info, os.O_RDONLY)
	if err != nil {
		return m, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxManifest+1))
	if err != nil {
		return m, err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != started.Manifest || canonical(data, &m) != nil || m.CleanupID != started.CleanupID || m.LibraryID != libraryID || m.Folder != started.Folder || m.Digest != started.Digest || len(m.Entries) != started.Count {
		return m, fmt.Errorf("%w: cleanup manifest does not match its journal record", ErrStateReview)
	}
	for _, e := range m.Entries {
		for _, file := range e.Files {
			if !validRelative(file.Path) || !validHash(file.SHA256) || file.Bytes < 0 || file.Bytes > assessment.MaxMP4Bytes {
				return m, fmt.Errorf("%w: cleanup manifest entry is invalid", ErrStateReview)
			}
		}
	}
	return m, nil
}

// validRelative accepts a clean, relative, slash-separated path.
func validRelative(p string) bool {
	return p != "" && p != "." && !strings.HasPrefix(p, "/") && !strings.Contains(p, `\`) && path.Clean(p) == p && !strings.HasPrefix(p, "../") && p != ".." && !strings.ContainsAny(p, "\x00\r\n") && len(p) <= 4096
}

// readCleanup parses and validates the cleanup journal: sequence, library, and
// the order of events within and across operations.
func readCleanup(control *control, libraryID string) ([]cleanupRecord, error) {
	data, err := readOptional(control.root, cleanupName, maxCleanupJournal)
	if err != nil || data == nil {
		return nil, err
	}
	lines := bytes.Split(data[:len(data)-1], []byte{'\n'})
	records := make([]cleanupRecord, 0, len(lines))
	completed := map[string]bool{}
	indices := map[int]bool{}
	var open *cleanupRecord
	for i, line := range lines {
		var r cleanupRecord
		if err := canonical(line, &r); err != nil || r.SchemaVersion != 1 || r.Sequence != i+1 || r.LibraryID != libraryID || !validStateID(r.CleanupID, "cleanup") || r.At == "" {
			return nil, ErrStateReview
		}
		switch r.Event {
		case "cleanup_started":
			if open != nil || completed[r.CleanupID] || !validHash(r.Digest) || !validHash(r.Manifest) || !filepath.IsAbs(r.Folder) || r.Count < 1 {
				return nil, ErrStateReview
			}
		case "restore_started":
			if open != nil || !completed[r.CleanupID] {
				return nil, ErrStateReview
			}
		case "cleanup_entry", "restore_entry":
			if open == nil || open.CleanupID != r.CleanupID || strings.TrimSuffix(open.Event, "_started") != strings.TrimSuffix(r.Event, "_entry") || r.Index < 0 || r.Index >= open.Count || indices[r.Index] || r.Outcome == "" || len(r.Outcome) > 64 {
				return nil, ErrStateReview
			}
			indices[r.Index] = true
		case "cleanup_completed", "restore_completed":
			if open == nil || open.CleanupID != r.CleanupID || strings.TrimSuffix(open.Event, "_started") != strings.TrimSuffix(r.Event, "_completed") {
				return nil, ErrStateReview
			}
			if r.Event == "cleanup_completed" {
				completed[r.CleanupID] = true
			}
		default:
			return nil, ErrStateReview
		}
		records = append(records, r)
		switch r.Event {
		case "cleanup_started", "restore_started":
			open, indices = &records[len(records)-1], map[int]bool{}
		case "cleanup_completed", "restore_completed":
			open = nil
		}
	}
	return records, nil
}

// openCleanup returns the started record of an unfinished operation, if any.
func openCleanup(records []cleanupRecord) *cleanupRecord {
	var open *cleanupRecord
	for i := range records {
		switch records[i].Event {
		case "cleanup_started", "restore_started":
			open = &records[i]
		case "cleanup_completed", "restore_completed":
			open = nil
		}
	}
	return open
}

var errTooLarge = errors.New("file over the 256 MiB limit, or 2 GiB for an M4B or M4A")

// zipAccounted requires a ZIP whose bytes are its entries: it starts with a
// local file header and carries no archive comment.
func zipAccounted(src *os.Root, rel string) (bool, string) {
	name := filepath.FromSlash(rel)
	info, err := src.Lstat(name)
	if err != nil {
		return false, "the archive could not be read"
	}
	f, err := safeio.OpenRegular(src, name, info, os.O_RDONLY)
	if err != nil {
		return false, "the archive could not be read"
	}
	defer f.Close()
	head := make([]byte, 4)
	if _, err := f.ReadAt(head, 0); err != nil || string(head) != "PK\x03\x04" {
		return false, "the archive has data before its first entry"
	}
	z, err := zip.NewReader(f, info.Size())
	if err != nil {
		return false, "the archive could not be read"
	}
	if z.Comment != "" {
		return false, "the archive carries a comment"
	}
	return true, ""
}

// receiptBinds checks that a packet receipt is a completed Nemalo receipt for
// exactly this content.
func receiptBinds(src *os.Root, rel string, content CleanupFile) bool {
	name := filepath.FromSlash(rel)
	info, err := src.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4<<20 {
		return false
	}
	f, err := safeio.OpenRegular(src, name, info, os.O_RDONLY)
	if err != nil {
		return false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 4<<20+1))
	if err != nil {
		return false
	}
	var r packetReceipt
	return json.Unmarshal(data, &r) == nil && r.Complete && r.Status == "untrusted_intake" && strings.EqualFold(r.Transfer.SHA256, content.SHA256) && r.Transfer.Bytes == content.Bytes
}

// maxCleanupRecord bounds one cleanup record line, for capacity checks, and
// cleanupJournalLimit is the journal size those checks plan against; tests
// lower it.
const maxCleanupRecord = 512

var cleanupJournalLimit int64 = maxCleanupJournal

// journalRoom refuses an operation when the cleanup journal cannot hold its
// worst-case number of records.
func journalRoom(control *control, records int) error {
	info, err := control.root.Lstat(cleanupName)
	size := int64(0)
	if err == nil {
		size = info.Size()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if size+int64(records)*maxCleanupRecord > cleanupJournalLimit {
		return errors.New("the cleanup journal is too full to record this operation completely; nothing was changed")
	}
	return nil
}
