package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/blisspixel/nemalo/internal/safeio"
	"github.com/blisspixel/nemalo/internal/staging"
)

// PurgeRequest removes held copies of one completed cleanup whose bytes the
// library holds. Without Apply nothing is written; Apply is the plan digest a
// preview printed.
type PurgeRequest struct {
	Library   string
	CleanupID string
	Apply     string
	// Stop closes a pending purge that cannot finish; held copies not yet
	// removed stay, and the cleanup can be restored or purged again.
	Stop bool
}

// PurgeFile is one held file of the cleanup, indexed as restore indexes it.
// Disposition before applying is purge, or why it stays: provenance_record
// (an intake packet's receipt or intent), not_held (no held copy: never moved,
// restored, or already purged), held_copy_changed, archive_not_accounted (a plain
// ZIP no longer meets the current whole-container proof), or not_in_library (its
// bytes are not a current holding whose stored bytes verify). Outcome after
// applying is purged, already_absent, held_copy_changed, or archive_not_accounted
// (a plain ZIP no longer meets the current whole-container proof).
type PurgeFile struct {
	Index       int      `json:"index"`
	Path        string   `json:"path"`
	Bytes       int64    `json:"bytes"`
	SHA256      string   `json:"sha256"`
	Disposition string   `json:"disposition"`
	Holdings    []string `json:"holdings,omitempty"`
	Outcome     string   `json:"outcome,omitempty"`
}

type PurgeResult struct {
	SchemaVersion int                 `json:"schema_version"`
	Applied       bool                `json:"applied"`
	Resumed       bool                `json:"resumed"`
	Library       string              `json:"library"`
	LibraryID     string              `json:"library_id,omitempty"`
	CleanupID     string              `json:"cleanup_id"`
	Folder        string              `json:"folder"`
	Digest        string              `json:"plan_digest"`
	Files         []PurgeFile         `json:"files"`
	ArchiveGroups []PurgeArchiveGroup `json:"archive_groups,omitempty"`
	Purge         int                 `json:"purge"`
	Keep          int                 `json:"keep"`
	PurgeBytes    int64               `json:"purge_bytes"`
	Purged        int                 `json:"purged"`
	PurgedBytes   int64               `json:"purged_bytes"`
	Limitations   []string            `json:"limitations"`
}

type purgeManifest struct {
	SchemaVersion int                 `json:"schema_version"`
	CleanupID     string              `json:"cleanup_id"`
	LibraryID     string              `json:"library_id"`
	Digest        string              `json:"plan_digest"`
	Files         []PurgeFile         `json:"files"`
	ArchiveGroups []PurgeArchiveGroup `json:"archive_groups,omitempty"`
}

func purgeLimitations() []string {
	return []string{
		"Purge removes only held copies whose exact bytes are a current holding whose stored bytes verify, and archives whose every member is. The library's own copy is never touched.",
		"A ZIP must still account for all its bytes. Unsupported or unaccounted archive data stays available to restore, including after an older purge is resumed.",
		"A purged file cannot be restored from the holding area; restore reports it as purged. Receipts, intents, and anything not held stay.",
		"Directory-entry durability across power loss depends on the filesystem.",
	}
}

// Purge plans, or with Apply removes, the held copies of one completed
// cleanup that the library holds. It is the only operation that deletes held
// source bytes, and it never deletes bytes the library does not also hold.
func Purge(ctx context.Context, request PurgeRequest) (result PurgeResult, err error) {
	result = PurgeResult{SchemaVersion: 1, CleanupID: request.CleanupID, Files: []PurgeFile{}, Limitations: purgeLimitations()}
	if !validStateID(request.CleanupID, "cleanup") {
		return result, errors.New("purge requires the cleanup ID reported by library cleanup")
	}
	if request.Apply != "" && !validHash(request.Apply) {
		return result, errors.New("apply takes the plan digest printed by the preview")
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
		return result, errors.New("purge requires an initialized library")
	}
	result.LibraryID = state.LibraryID
	ctx, cancel := context.WithTimeout(ctx, 2*time.Hour)
	defer cancel()
	if request.Stop && request.Apply != "" {
		return result, errors.New("stop closes a pending purge; it takes no plan")
	}
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
		if _, lines, err = rollForwardRescan(ctx, control, events, lines); err != nil {
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
		if records[i].CleanupID != request.CleanupID {
			continue
		}
		switch records[i].Event {
		case "cleanup_started":
			started = &records[i]
		case "cleanup_completed":
			completed = true
		}
	}
	if started == nil || !completed {
		return result, errors.New("no completed cleanup has that ID; close a pending one with library cleanup --stop first")
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
	open := openCleanup(records)
	if request.Stop {
		if open == nil || open.Event != "purge_started" || open.CleanupID != request.CleanupID {
			return result, errors.New("no purge of that cleanup is pending")
		}
		return stopPurge(ctx, control, records, *open, result)
	}
	if open != nil {
		if open.Event != "purge_started" || open.CleanupID != request.CleanupID {
			return result, cleanupPendingError(open)
		}
		result.Digest = open.Digest
		if request.Apply != open.Digest {
			return result, fmt.Errorf("a purge of %s is pending; finish it with --apply --plan %s", open.CleanupID, open.Digest)
		}
		pm, err := readPurgeManifest(store, *open, state.LibraryID)
		if err != nil {
			return result, err
		}
		result.Resumed = true
		return runPurge(ctx, control, store, records, *open, pm, m, latestHoldings(lines), map[string]bool{}, result)
	}
	files, verified, err := planPurge(ctx, store, m, lines)
	if err != nil {
		return result, err
	}
	groups, err := planZIPStreamPurge(ctx, store, m, lines, files, verified)
	if err != nil {
		return result, err
	}
	result.Files, result.ArchiveGroups, result.Digest = files, groups, purgePlanDigest(m.CleanupID, files, groups)
	for _, f := range files {
		if f.Disposition == "purge" {
			result.Purge++
			result.PurgeBytes += f.Bytes
		} else {
			result.Keep++
		}
	}
	if request.Apply == "" {
		return result, nil
	}
	if request.Apply != result.Digest {
		return result, errors.New("the plan changed since the preview; nothing was removed. Review the new plan and apply its digest")
	}
	if result.Purge == 0 {
		result.Applied = true
		return result, nil
	}
	if err := journalRoom(control, result.Purge+2); err != nil {
		return result, err
	}
	rec := cleanupRecord{SchemaVersion: 1, Sequence: len(records) + 1, LibraryID: state.LibraryID, CleanupID: m.CleanupID, Event: "purge_started", Digest: result.Digest, Count: len(files), At: now()}
	pm := purgeManifest{SchemaVersion: 1, CleanupID: m.CleanupID, LibraryID: state.LibraryID, Digest: result.Digest, Files: files, ArchiveGroups: groups}
	if len(groups) != 0 {
		pm.SchemaVersion = 2
	}
	if rec.Manifest, err = writePurgeManifest(store, pm, rec.Sequence); err != nil {
		return result, err
	}
	if err := cleanupStep("purge manifest"); err != nil {
		return result, err
	}
	if err := appendCanonical(ctx, control, cleanupName, maxCleanupJournal, rec); err != nil {
		return result, err
	}
	records = append(records, rec)
	return runPurge(ctx, control, store, records, rec, pm, m, latestHoldings(lines), verified, result)
}

// stopPurge closes a pending purge without removing anything more. Removals
// already journaled stand; every other held copy stays.
func stopPurge(ctx context.Context, control *control, records []cleanupRecord, started cleanupRecord, result PurgeResult) (PurgeResult, error) {
	result.Digest = started.Digest
	for _, r := range records {
		if r.Sequence > started.Sequence && r.CleanupID == started.CleanupID && r.Event == "purge_entry" && (r.Outcome == "purged" || r.Outcome == "already_absent") {
			result.Purged++
		}
	}
	rec := cleanupRecord{SchemaVersion: 1, Sequence: len(records) + 1, LibraryID: started.LibraryID, CleanupID: started.CleanupID, Event: "purge_completed", Count: result.Purged, At: now()}
	if err := appendCanonical(ctx, control, cleanupName, maxCleanupJournal, rec); err != nil {
		return result, err
	}
	result.Applied = true
	return result, nil
}

// cleanupFiles lists the files of a cleanup's moved entries in the order
// restore and purge index them, with each file's entry.
func cleanupFiles(m cleanupManifest) ([]CleanupFile, []CleanupEntry) {
	files, entries := []CleanupFile{}, []CleanupEntry{}
	for _, e := range m.Entries {
		if e.Disposition == "move" {
			for _, f := range e.Files {
				files, entries = append(files, f), append(entries, e)
			}
		}
	}
	return files, entries
}

// planPurge decides every held file. verified caches which holdings' stored
// bytes were hashed and agree, so an apply in the same call reuses them.
func planPurge(ctx context.Context, store *os.Root, m cleanupManifest, lines []Holding) ([]PurgeFile, map[string]bool, error) {
	current := latestHoldings(lines)
	verified := map[string]bool{}
	holds := func(ids []string) (bool, error) {
		if len(ids) == 0 {
			return false, nil
		}
		for _, id := range ids {
			h, ok := current[id]
			if !ok {
				return false, nil
			}
			v, seen := verified[id]
			if !seen {
				err := storedVerifies(ctx, store, h)
				if ctx.Err() != nil {
					return false, ctx.Err()
				}
				// A stored file that cannot be read is not vouched for.
				v = err == nil
				verified[id] = v
			}
			if !v {
				return false, nil
			}
		}
		return true, nil
	}
	holding := holdingSlash(m.CleanupID)
	files, entries := cleanupFiles(m)
	out := make([]PurgeFile, 0, len(files))
	for i, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		e := entries[i]
		p := PurgeFile{Index: i, Path: f.Path, Bytes: f.Bytes, SHA256: f.SHA256}
		inHolding, _, err := heldState(ctx, store, path.Join(holding, "files", f.Path), f)
		if err != nil {
			return nil, nil, err
		}
		switch {
		case inHolding == "absent":
			p.Disposition = "not_held"
		case inHolding == "changed":
			p.Disposition = "held_copy_changed"
		case e.Kind == "packet" && f != e.Files[0]:
			// A packet's receipt and intent record where its content came
			// from; they are small and stay.
			p.Disposition = "provenance_record"
		default:
			ok, err := holds(e.Holdings)
			if err != nil {
				return nil, nil, err
			}
			// A loose file or a packet's content is one holding; an archive
			// is every member's.
			p.Disposition, p.Holdings = "not_in_library", e.Holdings
			if ok && (e.Kind == "archive" || (len(e.Holdings) == 1 && current[e.Holdings[0]].SHA256 == f.SHA256)) {
				p.Disposition = "purge"
				if strings.EqualFold(path.Ext(f.Path), ".zip") {
					parent, base, err := heldParent(store, path.Join(holding, "files", f.Path))
					if err != nil {
						return nil, nil, err
					}
					accounted, _ := zipAccounted(ctx, parent, base)
					parent.Close()
					if err := ctx.Err(); err != nil {
						return nil, nil, err
					}
					if !accounted {
						p.Disposition = "archive_not_accounted"
					}
				}
			}
		}
		out = append(out, p)
	}
	return out, verified, nil
}

func purgeDigest(cleanupID string, files []PurgeFile) string {
	h := sha256.New()
	write := func(s string) { h.Write([]byte(strconv.Itoa(len(s)) + ":" + s)) }
	write("nemalo-purge-plan/1")
	write(cleanupID)
	for _, f := range files {
		write(strconv.Itoa(f.Index))
		write(f.Path)
		write(strconv.FormatInt(f.Bytes, 10))
		write(f.SHA256)
		write(f.Disposition)
		write(strings.Join(f.Holdings, ","))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// runPurge removes each planned file not yet journaled. verified holds the
// stored-byte checks the plan just made; a resumed purge checks again.
func runPurge(ctx context.Context, control *control, store *os.Root, records []cleanupRecord, started cleanupRecord, pm purgeManifest, m cleanupManifest, current map[string]Holding, verified map[string]bool, result PurgeResult) (PurgeResult, error) {
	permissions, err := zipStreamPermissions(m, pm)
	if err != nil {
		return result, err
	}
	result.Digest, result.Files = pm.Digest, append([]PurgeFile{}, pm.Files...)
	result.ArchiveGroups = pm.ArchiveGroups
	done := map[int]string{}
	for i := range records {
		if records[i].Sequence > started.Sequence && records[i].CleanupID == pm.CleanupID && records[i].Event == "purge_entry" {
			done[records[i].Index] = records[i].Outcome
		}
	}
	holding := holdingSlash(pm.CleanupID)
	result.Purge, result.Keep, result.PurgeBytes = 0, 0, 0
	for i, f := range result.Files {
		if f.Disposition != "purge" {
			result.Keep++
			continue
		}
		result.Purge++
		result.PurgeBytes += f.Bytes
		if outcome, ok := done[f.Index]; ok {
			result.Files[i].Outcome = outcome
			continue
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		outcome, err := "archive_not_accounted", error(nil)
		if allowed, stream := permissions[f.Index]; (!stream && !isZIPChunk(f.Path)) || allowed {
			outcome, err = purgeFile(ctx, store, path.Join(holding, "files", f.Path), f, current, verified)
		} else {
			outcome = retainedStreamOutcome(store, path.Join(holding, "files", f.Path), f)
		}
		if err != nil {
			return result, err
		}
		if err := cleanupStep("purged"); err != nil {
			return result, err
		}
		rec := cleanupRecord{SchemaVersion: 1, Sequence: len(records) + 1, LibraryID: pm.LibraryID, CleanupID: pm.CleanupID, Event: "purge_entry", Index: f.Index, Outcome: outcome, At: now()}
		if err := appendCanonical(ctx, control, cleanupName, maxCleanupJournal, rec); err != nil {
			return result, err
		}
		records = append(records, rec)
		result.Files[i].Outcome = outcome
	}
	for _, f := range result.Files {
		if f.Outcome == "purged" || f.Outcome == "already_absent" {
			result.Purged++
			result.PurgedBytes += f.Bytes
		}
	}
	removeEmptyDirs(store, path.Join(holding, "files"))
	rec := cleanupRecord{SchemaVersion: 1, Sequence: len(records) + 1, LibraryID: pm.LibraryID, CleanupID: pm.CleanupID, Event: "purge_completed", Count: result.Purged, At: now()}
	if err := appendCanonical(ctx, control, cleanupName, maxCleanupJournal, rec); err != nil {
		return result, err
	}
	result.Applied = true
	return result, nil
}

// purgeFile removes one held copy only when it still has the planned bytes,
// every holding it stands for is current with verified stored bytes, and its
// name still refers to the file that was hashed. Every folder on the way is
// opened without following links. A copy that cannot be read, verified, or
// removed stays with its outcome; only cancellation stops the purge.
func purgeFile(ctx context.Context, store *os.Root, held string, f PurgeFile, current map[string]Holding, verified map[string]bool) (string, error) {
	for _, id := range f.Holdings {
		h, ok := current[id]
		if !ok {
			return "not_in_library", nil
		}
		if !verified[id] {
			err := storedVerifies(ctx, store, h)
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if err != nil {
				return "not_in_library", nil
			}
			verified[id] = true
		}
	}
	parent, name, err := heldParent(store, held)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "already_absent", nil
	case err != nil:
		return "held_copy_changed", nil
	}
	defer parent.Close()
	before, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		// Removed before an interruption, after the plan was journaled.
		return "already_absent", nil
	}
	if err != nil {
		return "not_readable", nil
	}
	if !before.Mode().IsRegular() || before.Size() != f.Bytes {
		return "held_copy_changed", nil
	}
	h, err := safeio.OpenRegular(parent, name, before, os.O_RDONLY)
	if err != nil {
		return "not_readable", nil
	}
	defer h.Close()
	same, hashErr := sameBytes(ctx, h, CleanupFile{Path: f.Path, Bytes: f.Bytes, SHA256: f.SHA256})
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if hashErr != nil {
		return "not_readable", nil
	}
	if !same {
		return "held_copy_changed", nil
	}
	// Pending purge manifests can predate the current proof. Check the same
	// opened, rehashed ZIP before every removal, including resumed operations.
	if strings.EqualFold(path.Ext(f.Path), ".zip") {
		if accounted, _ := staging.ZIPAccounted(ctx, h, before.Size()); !accounted {
			return "archive_not_accounted", ctx.Err()
		}
	}
	_ = h.Close()
	after, err := parent.Lstat(name)
	if err != nil || !os.SameFile(before, after) || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return "held_copy_changed", nil
	}
	// A cleanup closed with --stop can leave its publishing partial, which is
	// a second name for this same file; the space is freed only with both.
	partial, partialErr := parent.Lstat(name + ".partial")
	if err := parent.Remove(name); err != nil {
		return "not_removed", nil
	}
	if partialErr == nil && os.SameFile(before, partial) {
		_ = parent.Remove(name + ".partial")
	}
	return "purged", nil
}

// storedVerifies requires a holding's stored file to hash to its record and to
// have no other link, as library check does.
func storedVerifies(ctx context.Context, store *os.Root, h Holding) error {
	if err := hashAgrees(ctx, store, h.Path, h.Bytes, h.SHA256); err != nil {
		return err
	}
	if n, err := regularLinkCount(store, h.Path); err != nil || n != 1 {
		return errStoredObject
	}
	return nil
}

// removeEmptyDirs removes folders left empty under dir, deepest first. The
// folders are opened without following links, and a folder that still holds
// anything stays.
func removeEmptyDirs(store *os.Root, dir string) {
	parent, base, err := heldParent(store, dir)
	if err != nil {
		return
	}
	defer parent.Close()
	files, err := safeio.OpenRootIn(parent, base)
	if err != nil {
		return
	}
	defer files.Close()
	dirs := []string{}
	_ = fs.WalkDir(files.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		// WalkDir does not descend into a link; only real folders are listed.
		if err == nil && d.IsDir() && p != "." {
			dirs = append(dirs, p)
		}
		return nil
	})
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, d := range dirs {
		_ = files.Remove(filepath.FromSlash(d))
	}
}

func purgeManifestName(cleanupID string, sequence int) string {
	return path.Join(holdingSlash(cleanupID), fmt.Sprintf("purge-%d.json", sequence))
}

func writePurgeManifest(store *os.Root, pm purgeManifest, sequence int) (string, error) {
	data, err := json.Marshal(pm)
	if err != nil {
		return "", err
	}
	if len(data) > maxManifest {
		return "", errors.New("the purge plan is too large for one manifest")
	}
	name := filepath.FromSlash(purgeManifestName(pm.CleanupID, sequence))
	// No journal record names this sequence yet, so a file already there was
	// left by a purge interrupted before it was journaled, and is replaced.
	if info, err := store.Lstat(name); err == nil && info.Mode().IsRegular() {
		if err := store.Remove(name); err != nil {
			return "", err
		}
	}
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

func readPurgeManifest(store *os.Root, started cleanupRecord, libraryID string) (purgeManifest, error) {
	var pm purgeManifest
	f, err := store.Open(filepath.FromSlash(purgeManifestName(started.CleanupID, started.Sequence)))
	if err != nil {
		return pm, fmt.Errorf("%w: purge manifest: %v", ErrStateReview, err)
	}
	defer f.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(io.LimitReader(f, maxManifest+1)); err != nil {
		return pm, err
	}
	data := buf.Bytes()
	sum := sha256.Sum256(data)
	if len(data) > maxManifest || hex.EncodeToString(sum[:]) != started.Manifest || canonical(data, &pm) != nil || (pm.SchemaVersion != 1 && pm.SchemaVersion != 2) || pm.CleanupID != started.CleanupID || pm.LibraryID != libraryID || pm.Digest != started.Digest || len(pm.Files) != started.Count ||
		(pm.SchemaVersion == 1 && len(pm.ArchiveGroups) != 0) || (pm.SchemaVersion == 2 && len(pm.ArchiveGroups) == 0) {
		return pm, fmt.Errorf("%w: purge manifest does not match its journal record", ErrStateReview)
	}
	for _, f := range pm.Files {
		if !validRelative(f.Path) || !validHash(f.SHA256) || f.Bytes < 0 || f.Index < 0 || f.Index >= started.Count {
			return pm, fmt.Errorf("%w: purge manifest entry is invalid", ErrStateReview)
		}
	}
	return pm, nil
}
