package library

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/blisspixel/nemalo/internal/safeio"
)

// CheckIssue is one fact that needs attention. Kind names it; Path is relative
// to the library root when it names a file.
type CheckIssue struct {
	Kind   string `json:"kind"`
	Path   string `json:"path,omitempty"`
	Detail string `json:"detail"`
}

// LibraryCheck is a read-only account of a managed library. Integrity issues
// are stored bytes that are missing, changed, or unaccounted for, unfinished
// operations, and damaged held copies; review counts are holdings that are
// intact but not checked, by reason. Nothing is repaired.
type LibraryCheck struct {
	SchemaVersion int            `json:"schema_version"`
	Library       string         `json:"library"`
	LibraryID     string         `json:"library_id"`
	Healthy       bool           `json:"healthy"`
	Holdings      int            `json:"holdings"`
	Verified      int            `json:"verified"`
	Checked       int            `json:"checked"`
	Review        map[string]int `json:"review_by_reason"`
	Unverified    int            `json:"unverified"`
	Cleanups      int            `json:"cleanups"`
	HeldFiles     int            `json:"held_files"`
	HeldBytes     int64          `json:"held_bytes"`
	Issues        []CheckIssue   `json:"issues"`
	Next          []string       `json:"next"`
	Limitations   []string       `json:"limitations"`
}

const (
	checkByteBudget = 64 << 30
	maxCheckIssues  = 1000
)

// CheckLibrary verifies every current holding's stored bytes, finds files in
// the store that no holding accounts for, reports unfinished operations, and
// verifies the held copies of every cleanup against its manifest. It takes the
// shared reader lock and writes nothing.
func CheckLibrary(ctx context.Context, directory string) (r LibraryCheck, err error) {
	r = LibraryCheck{SchemaVersion: 1, Review: map[string]int{}, Issues: []CheckIssue{}, Next: []string{}, Limitations: []string{
		"Check verifies sizes and SHA-256 of stored and held bytes. It does not rescan, repair, move, or delete anything.",
		"A healthy library can still hold review items: limited checks and scans are evidence, not a safety guarantee.",
		"Files outside assets/ and cleanup/ in the library folder are yours and are not examined.",
	}}
	abs, err := filepath.Abs(directory)
	if err != nil {
		return r, err
	}
	r.Library = abs
	state, err := LibraryStatus(ctx, abs)
	if err != nil {
		return r, err
	}
	if state.Status != "initialized" {
		return r, fmt.Errorf("check requires an initialized library; control state is %q", state.Status)
	}
	r.LibraryID = state.LibraryID
	ctx, cancel := context.WithTimeout(ctx, time.Hour)
	defer cancel()
	store, err := safeio.OpenRoot(abs)
	if err != nil {
		return r, err
	}
	defer func() { err = errors.Join(err, store.Close()) }()
	control, _, err := openControl(abs, false)
	if err != nil {
		return r, err
	}
	defer control.close()
	events, lines, err := lockedManaged(control, state.LibraryID)
	if err != nil {
		return r, err
	}
	records, err := readCleanup(control, state.LibraryID)
	if err != nil {
		return r, err
	}
	issue := func(kind, p, detail string) {
		if len(r.Issues) < maxCheckIssues {
			r.Issues = append(r.Issues, CheckIssue{Kind: kind, Path: p, Detail: detail})
		} else if len(r.Issues) == maxCheckIssues {
			r.Issues = append(r.Issues, CheckIssue{Kind: "more_issues", Detail: "further issues were not listed"})
		}
	}

	// Unfinished operations.
	if n := len(events); n > 0 {
		switch events[n-1].Event {
		case "import_intent":
			issue("pending_import", "", "an import of "+events[n-1].Source+" was interrupted; repeat it to finish")
		case "rescan_intent":
			issue("pending_rescan", events[n-1].Source, "a rescan was interrupted; the next library write finishes it")
		}
	}
	if open := openCleanup(records); open != nil {
		issue("pending_"+strings.TrimSuffix(open.Event, "_started"), "", open.CleanupID+" was interrupted; finish it or close it with --stop")
	}

	// Current holdings against stored bytes.
	budget := int64(checkByteBudget)
	known := map[string]bool{}
	for _, h := range latestList(lines) {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		r.Holdings++
		known[h.Path] = true
		if h.Bytes > budget {
			r.Unverified++
			issue("unverified", h.Path, "the check's byte budget was spent before this holding")
			continue
		}
		budget -= h.Bytes
		hashErr := hashAgrees(ctx, store, h.Path, h.Bytes, h.SHA256)
		if hashErr == nil {
			if n, err := regularLinkCount(store, h.Path); err != nil || n != 1 {
				hashErr = errStoredObject
			}
		}
		switch {
		case errors.Is(hashErr, os.ErrNotExist):
			issue("missing", h.Path, "the stored file of "+h.ID+" is missing")
			continue
		case hashErr != nil:
			if ctx.Err() != nil {
				return r, ctx.Err()
			}
			issue("changed", h.Path, "the stored file of "+h.ID+" no longer has its recorded bytes, or is linked elsewhere")
			continue
		}
		r.Verified++
		if h.Status == "checked" {
			r.Checked++
		} else {
			r.Review[h.Reason]++
		}
	}

	// Files in the store that no holding accounts for.
	err = fs.WalkDir(store.FS(), "assets", func(p string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			if p == "assets" && errors.Is(walkErr, fs.ErrNotExist) {
				return fs.SkipDir
			}
			issue("unreadable", p, walkErr.Error())
			return nil
		}
		if d.IsDir() {
			return nil
		}
		switch {
		case known[p]:
		case strings.HasSuffix(p, ".partial"):
			issue("partial", p, "an unfinished copy from an interrupted import; the next import of its source finishes or replaces it")
		case d.Type()&fs.ModeSymlink != 0 || !d.Type().IsRegular():
			issue("unexpected", p, "not a regular file; the store holds only regular files")
		default:
			issue("untracked", p, "no holding records this file")
		}
		return nil
	})
	if err != nil {
		return r, err
	}

	// Held copies of every cleanup against its manifest.
	held, err := checkHeld(ctx, store, records, state.LibraryID, &budget, issue)
	if err != nil {
		return r, err
	}
	r.Cleanups, r.HeldFiles, r.HeldBytes = held.cleanups, held.files, held.bytes

	for _, i := range r.Issues {
		if i.Kind != "unverified" {
			continue
		}
		r.Next = append(r.Next, "Some bytes were not verified within the check's budget; run check again on a smaller library or after audit.")
		break
	}
	if scans := r.Review["unscanned"] + r.Review["scan_unavailable"] + r.Review["scan_incomplete"] + r.Review["scan_not_scanned"]; scans > 0 {
		r.Next = append(r.Next, fmt.Sprintf("%d holding(s) lack usable scan evidence: run library rescan --apply.", scans))
	}
	if r.Cleanups > 0 && r.HeldFiles > 0 {
		r.Next = append(r.Next, "Held cleanup sources can be returned with library restore, or removed by hand once no longer needed.")
	}
	r.Healthy = len(r.Issues) == 0
	if !r.Healthy {
		return r, errors.New("the library needs attention; see issues. Nothing was changed")
	}
	return r, nil
}

type heldSummary struct {
	cleanups, files int
	bytes           int64
}

// checkHeld verifies each completed cleanup's held copies that its journal
// says were moved and not restored since, and reports cleanup folders with
// no journal record.
func checkHeld(ctx context.Context, store *os.Root, records []cleanupRecord, libraryID string, budget *int64, issue func(kind, p, detail string)) (heldSummary, error) {
	var s heldSummary
	started := map[string]cleanupRecord{}
	for _, rec := range records {
		if rec.Event == "cleanup_started" {
			started[rec.CleanupID] = rec
		}
	}
	ids := make([]string, 0, len(started))
	for id := range started {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return s, err
		}
		s.cleanups++
		m, err := readManifest(store, started[id], libraryID)
		if err != nil {
			issue("cleanup_manifest", holdingSlash(id)+"/manifest.json", "the manifest of "+id+" is missing or does not match its journal record")
			continue
		}
		for _, e := range m.Entries {
			if e.Disposition != "move" {
				continue
			}
			for _, f := range e.Files {
				held := path.Join(holdingSlash(id), "files", f.Path)
				info, err := store.Lstat(filepath.FromSlash(held))
				if errors.Is(err, os.ErrNotExist) {
					continue // not moved, or restored
				}
				if f.Bytes > *budget {
					issue("unverified", held, "the check's byte budget was spent before this held copy")
					continue
				}
				*budget -= f.Bytes
				// An unreadable or mismatched copy is reported, never treated as held.
				ok := false
				if err == nil && info.Mode().IsRegular() {
					ok, _ = heldCopy(ctx, store, held, f)
				}
				if ctx.Err() != nil {
					return s, ctx.Err()
				}
				if !ok {
					issue("held_changed", held, "a held cleanup copy no longer has its planned bytes")
					continue
				}
				s.files++
				s.bytes += f.Bytes
			}
		}
	}
	// A cleanup folder without a journal record is left from an interruption
	// before the cleanup began; it holds at most a manifest.
	dirs, err := fs.ReadDir(store.FS(), cleanupDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return s, err
	}
	for _, d := range dirs {
		if _, ok := started["cleanup:"+d.Name()]; !ok {
			issue("cleanup_orphan", path.Join(cleanupDir, d.Name()), "no cleanup journal record names this folder; it was left before a cleanup began")
		}
	}
	return s, nil
}
