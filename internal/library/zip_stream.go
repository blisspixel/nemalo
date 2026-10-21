package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/blisspixel/nemalo/internal/staging"
)

// Increment an identifier when its accepted whole-set evidence changes.
// A partial purge must never silently apply different coverage rules.
const zipStreamProof = "zip-stream/1"
const zipSegmentsProof = "zip-segments/1"

func retainedZIPSetOutcome(store *os.Root, held string, f PurgeFile) string {
	parent, base, err := heldParent(store, held)
	if errors.Is(err, os.ErrNotExist) {
		return "not_held"
	}
	if err != nil {
		return "held_copy_changed"
	}
	defer parent.Close()
	info, err := parent.Lstat(base)
	if errors.Is(err, os.ErrNotExist) {
		return "not_held"
	}
	if err != nil {
		return "not_readable"
	}
	if !info.Mode().IsRegular() || info.Size() != f.Bytes {
		return "held_copy_changed"
	}
	return "archive_not_accounted"
}

type PurgeArchiveGroup struct {
	Proof      string `json:"proof"`
	Indices    []int  `json:"file_indices"`
	LeafDigest string `json:"leaf_digest,omitempty"`
}

func isZIPChunk(name string) bool {
	set, n, ok := staging.VolumeOf(name)
	return ok && n > 0 && strings.EqualFold(filepath.Ext(set), ".zip")
}

func isZIPPart(name string) bool {
	_, _, segmented := staging.ZIPSegmentOf(name)
	return isZIPChunk(name) || segmented
}

func entryHasZIPParts(e CleanupEntry) bool {
	for _, f := range e.Files {
		if isZIPPart(f.Path) {
			return true
		}
	}
	return false
}

func zipProofFor(e CleanupEntry) string {
	if _, _, ok := staging.ZIPSegmentOf(e.Path); ok {
		return zipSegmentsProof
	}
	if isZIPChunk(e.Path) {
		return zipStreamProof
	}
	return ""
}

func supportedZIPSet(e CleanupEntry) bool {
	return e.ArchiveProof != "" && e.ArchiveProof == zipProofFor(e)
}

func zipSetParts(e CleanupEntry) ([]string, bool) {
	return archiveSetParts(e, ".zip")
}

func archiveSetParts(e CleanupEntry, extension string) ([]string, bool) {
	if e.Kind != "archive" || len(e.Files) < 2 || len(e.Files) > 1000 || e.Path != e.Files[0].Path {
		return nil, false
	}
	set, n, ok := staging.VolumeOf(e.Files[0].Path)
	segmented := extension == ".zip" && zipProofFor(e) == zipSegmentsProof
	if segmented {
		set, n, ok = staging.ZIPSegmentOf(e.Path)
	}
	if !ok || n != 1 || !strings.EqualFold(filepath.Ext(set), extension) {
		return nil, false
	}
	// Staging names are native paths; manifests always use portable slashes.
	set = filepath.ToSlash(set)
	var total int64
	parts := make([]string, len(e.Files))
	for i, f := range e.Files {
		other, ordinal, ok := staging.VolumeOf(f.Path)
		if segmented {
			other, ordinal, ok = staging.ZIPSegmentOf(f.Path)
			if i+1 == len(e.Files) {
				other, ordinal, ok = f.Path, i+1, strings.EqualFold(path.Ext(f.Path), ".zip")
			}
		}
		if !ok || ordinal != i+1 || !sameArchiveFamily(other, set) || path.Dir(f.Path) != path.Dir(e.Path) || f.Bytes < 1 || f.Bytes > 256<<20 || total > (4<<30)-f.Bytes {
			return nil, false
		}
		total += f.Bytes
		parts[i] = path.Base(f.Path)
	}
	return parts, true
}

// Fresh authorization requires the complete set. Later partial recovery uses
// only the immutable, supported proof bound into its already durable intent.
func planArchivePurge(ctx context.Context, store *os.Root, m cleanupManifest, lines []Holding, files []PurgeFile, verified map[string]bool) ([]PurgeArchiveGroup, error) {
	groups := []PurgeArchiveGroup{}
	current := latestHoldings(lines)
	byHash := map[string]Holding{}
	for _, h := range current {
		if verified[h.ID] {
			byHash[h.SHA256] = h
		}
	}
	remaining := int64(maxPlanHashBytes)
	index := 0
	for _, e := range m.Entries {
		if e.Disposition != "move" {
			continue
		}
		start := index
		index += len(e.Files)
		if !versionedArchive(e) {
			continue
		}
		ready := supportedArchiveProof(e)
		for _, f := range files[start:index] {
			ready = ready && f.Disposition == "purge"
		}
		if _, ok := archiveEntryParts(e); !ok {
			ready = false
		}
		if ready {
			var err error
			ready, err = checkHeldArchive(ctx, store, m.CleanupID, e, byHash, &remaining)
			if err != nil {
				return nil, err
			}
		}
		if !ready {
			for i := start; i < index; i++ {
				if files[i].Disposition == "purge" {
					files[i].Disposition = "archive_not_accounted"
				}
			}
			continue
		}
		group := PurgeArchiveGroup{Proof: e.ArchiveProof}
		if leafBoundArchiveProof(e.ArchiveProof) {
			group.LeafDigest = archiveLeafDigest(e)
		}
		for i := start; i < index; i++ {
			group.Indices = append(group.Indices, i)
		}
		groups = append(groups, group)
	}
	return groups, nil
}

func checkHeldArchive(ctx context.Context, store *os.Root, cleanupID string, e CleanupEntry, byHash map[string]Holding, remaining *int64) (bool, error) {
	parts, ok := archiveEntryParts(e)
	if !ok {
		return false, nil
	}
	parent, first, err := heldParent(store, path.Join(holdingSlash(cleanupID), "files", e.Files[0].Path))
	if err != nil {
		return false, ctx.Err()
	}
	defer parent.Close()
	hashFile := func(name string) (CleanupFile, error) {
		return fileIdentityWithin(ctx, parent, name, remaining)
	}
	held := func(sum string) (Holding, bool, error) {
		h, ok := byHash[sum]
		return h, ok, nil
	}
	candidate := CleanupEntry{Path: first, Kind: "archive"}
	volumes := parts
	if len(parts) == 1 {
		volumes = nil
	}
	if err := planArchive(ctx, parent, first, volumes, held, hashFile, remaining, &candidate); err != nil {
		return false, ctx.Err()
	}
	if candidate.Disposition != "move" || candidate.ArchiveProof != e.ArchiveProof || !slices.Equal(candidate.Holdings, e.Holdings) || !equalArchiveLeaves(candidate.ArchiveLeaves, e.ArchiveLeaves) || len(candidate.Files) != len(e.Files) {
		return false, nil
	}
	for i, f := range candidate.Files {
		if f.Path != parts[i] || f.Bytes != e.Files[i].Bytes || f.SHA256 != e.Files[i].SHA256 {
			return false, nil
		}
	}
	return true, nil
}

func purgePlanDigest(cleanupID string, files []PurgeFile, groups []PurgeArchiveGroup) string {
	legacy := purgeDigest(cleanupID, files)
	if len(groups) == 0 {
		return legacy
	}
	data, _ := json.Marshal(groups)
	sum := sha256.Sum256(append([]byte("nemalo-purge-plan/2\n"+legacy+"\n"), data...))
	return hex.EncodeToString(sum[:])
}

// The returned map includes false entries for ZIP parts without supported
// evidence. Validate every binding before any removal, including legacy intents.
func archivePermissions(m cleanupManifest, pm purgeManifest) (map[int]bool, error) {
	expected, entries := cleanupFiles(m)
	permissions := map[int]bool{}
	bad := fmt.Errorf("%w: invalid archive purge binding", ErrStateReview)
	for _, e := range m.Entries {
		if e.Disposition == "move" && len(e.ArchiveLeaves) != 0 && !validArchiveLeaves(e) {
			return nil, bad
		}
		if e.Disposition == "move" && knownArchiveProof(e.ArchiveProof) && !supportedArchiveProof(e) {
			return nil, bad
		}
	}
	for i, e := range entries {
		if versionedArchive(e) {
			permissions[i] = false
		}
	}
	if len(permissions) == 0 && len(pm.ArchiveGroups) == 0 {
		return permissions, nil
	}
	if len(expected) != len(pm.Files) || pm.Digest != purgePlanDigest(pm.CleanupID, pm.Files, pm.ArchiveGroups) {
		return nil, bad
	}
	for i, f := range pm.Files {
		if f.Index != i || f.Outcome != "" || f.Path != expected[i].Path || f.Bytes != expected[i].Bytes || f.SHA256 != expected[i].SHA256 ||
			(f.Disposition == "purge" && !slices.Equal(f.Holdings, entries[i].Holdings)) {
			return nil, bad
		}
	}
	seen := map[int]bool{}
	for _, group := range pm.ArchiveGroups {
		if pm.SchemaVersion != 2 || len(group.Proof) > 64 || group.Proof == "" || len(group.Indices) < 1 || len(group.Indices) > 1000 {
			return nil, bad
		}
		start := group.Indices[0]
		if start < 0 || start >= len(expected) {
			return nil, bad
		}
		e := entries[start]
		if _, ok := archiveEntryParts(e); !ok || expected[start].Path != e.Files[0].Path || len(group.Indices) != len(e.Files) {
			return nil, bad
		}
		switch group.Proof {
		case zipStreamProof, zipSegmentsProof, archiveTreeProof, archiveRARTreeProof, rar5FramesProof, rar4FramesProof:
			if group.Proof != e.ArchiveProof && (leafBoundArchiveProof(e.ArchiveProof) || leafBoundArchiveProof(group.Proof)) {
				return nil, bad
			}
		}
		allowed := archiveGroupDigestValid(e, group)
		if group.Proof == e.ArchiveProof && supportedArchiveProof(e) && !allowed {
			return nil, bad
		}
		for j, i := range group.Indices {
			if i != start+j || i >= len(expected) || seen[i] || expected[i] != e.Files[j] || pm.Files[i].Disposition != "purge" {
				return nil, bad
			}
			seen[i] = true
			permissions[i] = allowed
		}
	}
	return permissions, nil
}
