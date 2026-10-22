package library

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/blisspixel/nemalo/internal/staging"
)

const archiveTreeProof = "archive-tree/1"
const archiveRARTreeProof = "archive-tree/2"
const rar5FramesProof = "rar5-frames/1"
const rar4FramesProof = "rar4-frames/1"
const rar5SetProof = "rar5-set/1"
const rar4SetProof = "rar4-set/1"

func leafBoundArchiveProof(proof string) bool {
	return proof == archiveTreeProof || proof == archiveRARTreeProof || proof == rar5FramesProof || proof == rar4FramesProof || proof == rar5SetProof || proof == rar4SetProof
}

func knownArchiveProof(proof string) bool {
	return leafBoundArchiveProof(proof) || proof == zipStreamProof || proof == zipSegmentsProof
}

// CleanupArchiveLeaf binds an exact nested selection to verified held bytes.
// Chain components are archive member names, never filesystem paths to open.
type CleanupArchiveLeaf struct {
	Chain   []string `json:"chain"`
	Bytes   int64    `json:"bytes"`
	SHA256  string   `json:"sha256"`
	Holding string   `json:"holding"`
}

func versionedArchive(e CleanupEntry) bool {
	return entryHasZIPParts(e) || entryHasRARFiles(e) || entryHasSevenFiles(e) || e.ArchiveProof != "" || len(e.ArchiveLeaves) != 0
}

func isSevenSource(name string) bool {
	if strings.EqualFold(path.Ext(name), ".7z") {
		return true
	}
	set, _, chunk := staging.VolumeOf(name)
	return chunk && strings.EqualFold(path.Ext(set), ".7z")
}

func entryHasSevenFiles(e CleanupEntry) bool {
	if e.Kind != "archive" {
		return false
	}
	for _, f := range e.Files {
		if isSevenSource(f.Path) {
			return true
		}
	}
	return false
}

// Historical manifests can name RAR sources without proof. A member-holding
// list never authorizes their encoded bytes, including old continuations.
func entryHasRARFiles(e CleanupEntry) bool {
	if e.Kind != "archive" {
		return false
	}
	for _, f := range e.Files {
		if strings.EqualFold(path.Ext(f.Path), ".rar") {
			return true
		}
		if _, _, old := staging.OldRARVolumeOf(f.Path); old {
			return true
		}
	}
	return false
}

func validArchiveLeaves(e CleanupEntry) bool {
	if len(e.ArchiveLeaves) < 1 || len(e.ArchiveLeaves) > 10000 || len(e.ArchiveLeaves) != len(e.Holdings) {
		return false
	}
	seen := map[string]bool{}
	for i, leaf := range e.ArchiveLeaves {
		if len(leaf.Chain) < 1 || len(leaf.Chain) > 4 || leaf.Bytes < 1 || leaf.Bytes > 256<<20 ||
			!validHash(leaf.SHA256) || leaf.Holding != "sha256:"+leaf.SHA256 || leaf.Holding != e.Holdings[i] {
			return false
		}
		for j, name := range leaf.Chain {
			if len(name) > 1024 || !utf8.ValidString(name) || !fs.ValidPath(name) || strings.ContainsAny(name, "\\:\x00") {
				return false
			}
			if j+1 == len(leaf.Chain) {
				if !supportedImport(name) {
					return false
				}
			} else if ext := strings.ToLower(path.Ext(name)); ext != ".zip" && ext != ".7z" && ext != ".rar" {
				return false
			}
		}
		key, _ := json.Marshal(leaf.Chain)
		if seen[string(key)] {
			return false
		}
		seen[string(key)] = true
	}
	return true
}

func archiveLeafDigest(e CleanupEntry) string {
	data, _ := json.Marshal(e.ArchiveLeaves)
	sum := sha256.Sum256(append([]byte("nemalo-archive-leaves/1\n"), data...))
	return hex.EncodeToString(sum[:])
}

func equalArchiveLeaves(a, b []CleanupArchiveLeaf) bool {
	return slices.EqualFunc(a, b, func(a, b CleanupArchiveLeaf) bool {
		return a.Bytes == b.Bytes && a.SHA256 == b.SHA256 && a.Holding == b.Holding && slices.Equal(a.Chain, b.Chain)
	})
}

func supportedArchiveProof(e CleanupEntry) bool {
	if e.ArchiveProof == rar5SetProof || e.ArchiveProof == rar4SetProof {
		_, layout := archiveSetParts(e, ".rar")
		return layout && validArchiveLeaves(e)
	}
	if e.ArchiveProof == rar5FramesProof || e.ArchiveProof == rar4FramesProof {
		if !validArchiveLeaves(e) || len(e.Files) != 1 || e.Kind != "archive" || e.Path != e.Files[0].Path || !strings.EqualFold(path.Ext(e.Path), ".rar") {
			return false
		}
		for _, leaf := range e.ArchiveLeaves {
			if len(leaf.Chain) != 1 {
				return false
			}
		}
		return true
	}
	if e.ArchiveProof == archiveTreeProof || e.ArchiveProof == archiveRARTreeProof {
		// Expanding syntactic set validation must not expand either tree ID.
		// Old RAR continuations overlap ZIP's .zNN names. The validated
		// outer layout, not that historical retention classifier, selects this.
		if len(e.Files) > 1 && strings.EqualFold(path.Ext(e.Path), ".rar") {
			return false
		}
		_, layout := archiveEntryParts(e)
		if !layout || !validArchiveLeaves(e) {
			return false
		}
		if e.ArchiveProof == archiveTreeProof {
			if strings.EqualFold(path.Ext(e.Path), ".rar") {
				return false
			}
			for _, leaf := range e.ArchiveLeaves {
				for _, container := range leaf.Chain[:len(leaf.Chain)-1] {
					if strings.EqualFold(path.Ext(container), ".rar") {
						return false
					}
				}
			}
		}
		return true
	}
	return len(e.ArchiveLeaves) == 0 && supportedZIPSet(e)
}

// Validate only supported outer layouts, including the exact parent of every
// part. Unknown proof versions can still preserve an otherwise valid layout.
func archiveEntryParts(e CleanupEntry) ([]string, bool) {
	if len(e.Files) == 1 {
		f := e.Files[0]
		ext := strings.ToLower(path.Ext(f.Path))
		if e.Kind != "archive" || e.Path != f.Path || (ext != ".zip" && ext != ".7z" && ext != ".rar") || f.Bytes < 1 || f.Bytes > 256<<20 {
			return nil, false
		}
		return []string{path.Base(f.Path)}, true
	}
	if zipProofFor(e) != "" {
		return zipSetParts(e)
	}
	if strings.EqualFold(path.Ext(e.Path), ".rar") {
		return archiveSetParts(e, ".rar")
	}
	return archiveSetParts(e, ".7z")
}

func archiveGroupDigestValid(e CleanupEntry, group PurgeArchiveGroup) bool {
	if group.Proof != e.ArchiveProof || !supportedArchiveProof(e) {
		return false
	}
	if leafBoundArchiveProof(e.ArchiveProof) {
		return group.LeafDigest == archiveLeafDigest(e)
	}
	return group.LeafDigest == ""
}

// Canonical manifest paths are portable even when a platform adapter uses
// native separators for its family key.
func sameArchiveFamily(a, b string) bool {
	return strings.EqualFold(filepath.ToSlash(a), filepath.ToSlash(b))
}
