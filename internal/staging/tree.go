package staging

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blisspixel/nemalo/internal/safeio"
)

const archiveLevels = 4

// ExtractTree reads one outer archive or checked volume set and its supported
// standalone children. Every node shares work limits; existing Extract callers
// keep their single-container behavior. dst is a fresh, caller-owned private
// staging root, removed by the caller after import or on failure.
func ExtractTree(ctx context.Context, src *os.Root, names []string, dst *os.Root, supported func(string) bool, limits Limits) (Extraction, error) {
	if len(names) == 0 {
		return Extraction{}, errors.New("an archive tree requires an outer source")
	}
	var size int64
	for _, name := range names {
		info, err := src.Lstat(name)
		if err != nil {
			return Extraction{}, err
		}
		size = min(maxArchiveSnapshot, size+min(info.Size(), maxArchiveSnapshot))
	}
	treeCtx, cancel := context.WithTimeout(ctx, archiveDeadline(size))
	defer cancel()
	limits.tree = &treeBudget{members: limits.Members, bytes: limits.TotalBytes, processes: maxRuns,
		reads: allowance{remaining: 2 * maxArchiveSnapshot}, diagnostics: allowance{remaining: maxListing},
		proofs: allowance{remaining: archiveProofBytes}}
	return extractTree(treeCtx, src, names, dst, supported, limits)
}

func extractTree(ctx context.Context, src *os.Root, names []string, dst *os.Root, supported func(string) bool, limits Limits) (Extraction, error) {
	result := Extraction{Members: []Member{}, Complete: true, Whole: limits.Coverage}
	gap := func(chain []string, detail string) {
		if limits.Coverage && result.CoverageGap == nil {
			result.CoverageGap = &CoverageGap{Chain: append([]string{}, chain...), Detail: detail}
		}
	}
	var visit func(*os.Root, []string, []string, int) error
	node := 0
	visit = func(source *os.Root, files, chain []string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if limits.memberLimit() <= 0 {
			return errTreeBudget
		}
		dir := fmt.Sprintf("node-%05d", node)
		node++
		if err := dst.Mkdir(dir, 0o700); err != nil {
			return err
		}
		stage, err := dst.OpenRoot(dir)
		if err != nil {
			result.Whole = false
			gap(chain, err.Error())
			return err
		}
		defer stage.Close()
		accept := func(name string) bool { return supported(name) || Supported(name) }
		nodeLimits := limits
		nodeLimits.child = depth > 1
		var x Extraction
		if len(files) > 1 {
			x, err = ExtractSet(ctx, source, files, stage, accept, nodeLimits)
		} else if depth > 1 && strings.EqualFold(filepath.Ext(files[0]), ".zip") {
			x, err = extractNestedZIP(ctx, source, files[0], stage, accept, nodeLimits)
		} else {
			x, err = Extract(ctx, source, files[0], stage, accept, nodeLimits)
		}
		if err != nil {
			gap(chain, err.Error())
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		result.Complete = result.Complete && x.Complete
		result.Whole = result.Whole && x.Whole
		if !x.Whole {
			gap(chain, "a comment, leading data, or bytes beyond the archive's members prevent complete accounting")
		}
		for _, m := range x.Members {
			if err := ctx.Err(); err != nil {
				return err
			}
			m.Chain = append(append([]string{}, chain...), m.Name)
			if m.Disposition != "extracted" {
				if limits.Coverage && (Supported(m.Name) || m.Disposition == "nested_volume" || m.Disposition == "nested_archive") {
					result.Whole = false
					gap(m.Chain, m.Detail)
				}
				result.Members = append(result.Members, m)
				continue
			}
			if !Supported(m.Name) {
				m.Staged = filepath.Join(dir, m.Staged)
				result.Members = append(result.Members, m)
				continue
			}
			file := m.Staged
			m.Staged = ""
			at := len(result.Members)
			m.Disposition, m.Detail = "nested_archive", "read as a nested archive; original bytes remain in the outer source"
			result.Members = append(result.Members, m)
			if depth >= archiveLevels {
				err = errors.New("the archive tree's four-level nesting limit was reached; child contents remain preserved")
			} else {
				err = visit(stage, []string{file}, m.Chain, depth+1)
			}
			if removeErr := stage.Remove(file); removeErr != nil {
				return errors.Join(err, fmt.Errorf("remove private child snapshot: %w", removeErr))
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				result.Whole = false
				gap(m.Chain, err.Error())
				result.Members[at].Detail = err.Error()
				if !errors.Is(err, ErrNoArchiveTool) {
					result.Members[at].Disposition = "failed"
					result.Complete = false
				}
			}
		}
		return nil
	}
	err := visit(src, names, nil, 1)
	if err != nil {
		result.Complete = false
		result.Whole = false
	}
	return result, err
}

func extractNestedZIP(ctx context.Context, src *os.Root, name string, dst *os.Root, supported func(string) bool, limits Limits) (x Extraction, err error) {
	return extractZIPSnapshot(ctx, src, name, dst, supported, limits, true)
}

func extractZIPSnapshot(ctx context.Context, src *os.Root, name string, dst *os.Root, supported func(string) bool, limits Limits, requireExtent bool) (x Extraction, err error) {
	x = Extraction{Members: []Member{}}
	var exe string
	if requireExtent {
		exe, err = SevenZip()
		if err != nil {
			return x, ErrNoArchiveTool
		}
	}
	info, err := src.Lstat(name)
	if err != nil {
		return x, err
	}
	maxBytes := int64(maxArchiveSnapshot)
	if requireExtent {
		maxBytes = limits.MemberBytes
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxBytes {
		return x, errors.New("a ZIP snapshot must be a regular file within its source or member budget")
	}
	dir, err := os.MkdirTemp("", "nemalo-nested-zip-")
	if err != nil {
		return x, err
	}
	defer func() {
		if cleanupErr := os.RemoveAll(dir); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("remove private nested ZIP snapshot: %w", cleanupErr))
		}
	}()
	snapshot, err := safeio.OpenRoot(dir)
	if err != nil {
		return x, err
	}
	defer snapshot.Close()
	prepCtx, cancel := ctx, func() {}
	if limits.Coverage {
		prepCtx, cancel = context.WithTimeout(ctx, 2*time.Minute)
	}
	defer cancel()
	if err := snapshotArchive(prepCtx, src, name, info, snapshot, "archive.zip", limits); err != nil {
		return x, err
	}
	if !zipSignature(snapshot, "archive.zip", false) {
		return x, errors.New("the nested ZIP lacks an ordinary ZIP signature; original sources are preserved")
	}
	if requireExtent {
		if err := ordinaryZIP(prepCtx, exe, filepath.Join(dir, "archive.zip"), info.Size(), maxListing, limits); err != nil {
			return x, err
		}
	}
	cancel()
	return extractZipFile(ctx, snapshot, "archive.zip", dst, supported, limits)
}

// Nested sets are preserved, not resolved. Include possible first/terminal
// siblings so a ZIP terminal is not accidentally read as a standalone child.
func nestedVolumes(names []string) map[string]bool {
	possible := map[string]bool{}
	key := func(name string) string {
		return filepath.Join(filepath.Dir(name), strings.ToLower(filepath.Base(name)))
	}
	for _, name := range names {
		if _, _, ok := VolumeOf(name); ok {
			possible[key(name)] = true
		}
		if set, _, ok := OldRARVolumeOf(name); ok {
			possible[key(name)], possible[key(set)] = true, true
		}
		if set, _, ok := ZIPSegmentOf(name); ok {
			possible[key(name)], possible[key(set)] = true, true
		}
	}
	out := map[string]bool{}
	for _, name := range names {
		out[name] = possible[key(name)]
	}
	return out
}
