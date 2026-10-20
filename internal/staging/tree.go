package staging

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
		reads: allowance{remaining: 2 * maxArchiveSnapshot}, diagnostics: allowance{remaining: maxListing}}
	return extractTree(treeCtx, src, names, dst, supported, limits)
}

func extractTree(ctx context.Context, src *os.Root, names []string, dst *os.Root, supported func(string) bool, limits Limits) (Extraction, error) {
	result := Extraction{Members: []Member{}, Complete: true}
	var visit func(*os.Root, []string, []string, int) error
	node := 0
	visit = func(source *os.Root, files, chain []string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		dir := fmt.Sprintf("node-%05d", node)
		node++
		if err := dst.Mkdir(dir, 0o700); err != nil {
			return err
		}
		stage, err := dst.OpenRoot(dir)
		if err != nil {
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
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		result.Complete = result.Complete && x.Complete
		for _, m := range x.Members {
			if err := ctx.Err(); err != nil {
				return err
			}
			m.Chain = append(append([]string{}, chain...), m.Name)
			if m.Disposition != "extracted" {
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
	}
	return result, err
}

func extractNestedZIP(ctx context.Context, src *os.Root, name string, dst *os.Root, supported func(string) bool, limits Limits) (x Extraction, err error) {
	x = Extraction{Members: []Member{}}
	exe, err := SevenZip()
	if err != nil {
		return x, ErrNoArchiveTool
	}
	info, err := src.Lstat(name)
	if err != nil {
		return x, err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > limits.MemberBytes {
		return x, errors.New("a nested ZIP must be a regular staged file within the member budget")
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
	if err := snapshotArchive(ctx, src, name, info, snapshot, "archive.zip"); err != nil {
		return x, err
	}
	if !zipSignature(snapshot, "archive.zip", false) {
		return x, errors.New("the nested ZIP lacks an ordinary ZIP signature; original sources are preserved")
	}
	if err := ordinaryZIP(ctx, exe, filepath.Join(dir, "archive.zip"), info.Size(), maxListing, limits); err != nil {
		return x, err
	}
	return ExtractZip(ctx, snapshot, "archive.zip", dst, supported, limits)
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
