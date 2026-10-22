package library

import (
	"fmt"
	"strings"
	"testing"
)

func oldRARShape(count int) CleanupEntry {
	hash := strings.Repeat("a", 64)
	e := CleanupEntry{Kind: "archive", Path: "books/set.rar", ArchiveProof: rar4OldSetProof, Holdings: []string{"sha256:" + hash}, ArchiveLeaves: []CleanupArchiveLeaf{{Chain: []string{"book.epub"}, Bytes: 1, SHA256: hash, Holding: "sha256:" + hash}}}
	for i := range count {
		e.Files = append(e.Files, CleanupFile{Path: oldSetPartName("books/set", i), Bytes: 1, SHA256: hash})
	}
	return e
}

func TestOldRARSetShapeNeverExpandsExistingProofs(t *testing.T) {
	for _, count := range []int{2, 3, 103, 901} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			e := oldRARShape(count)
			if parts, ok := archiveEntryParts(e); !ok || len(parts) != count || parts[0] != "set.rar" || parts[count-1] != oldSetPartName("set", count-1) {
				t.Fatal(parts, ok)
			}
			for _, proof := range []string{rar4OldSetProof, rar5OldSetProof, rar4SetProof, rar5SetProof, rar4FramesProof, rar5FramesProof, archiveTreeProof, archiveRARTreeProof, zipStreamProof, zipSegmentsProof, "future", ""} {
				e.ArchiveProof = proof
				if supportedArchiveProof(e) != (proof == rar4OldSetProof || proof == rar5OldSetProof) {
					t.Fatal("proof scope changed", proof, count)
				}
			}
			modern := e
			modern.Files = append([]CleanupFile(nil), e.Files...)
			for i := range modern.Files {
				modern.Files[i].Path = fmt.Sprintf("books/set.part%d.rar", i+1)
			}
			modern.Path = modern.Files[0].Path
			for _, proof := range []string{rar4OldSetProof, rar5OldSetProof} {
				modern.ArchiveProof = proof
				if supportedArchiveProof(modern) {
					t.Fatal("old authority accepted modern layout", proof)
				}
			}
		})
	}
}

func TestOldRARSetShapeRefusesUncertainBindings(t *testing.T) {
	for _, mutation := range []string{"one part", "too many", "missing first", "wrong entry", "gap", "duplicate", "reordered", "other parent", "parent alias", "other family", "bad hash", "zero bytes", "large part", "large total", "unsafe leaf", "missing leaf"} {
		t.Run(mutation, func(t *testing.T) {
			e := oldRARShape(3)
			switch mutation {
			case "one part":
				e = oldRARShape(1)
			case "too many":
				e = oldRARShape(902)
			case "missing first":
				e.Files = e.Files[1:]
			case "wrong entry":
				e.Path = "other.rar"
			case "gap":
				e.Files[2].Path = "books/set.r02"
			case "duplicate":
				e.Files[2] = e.Files[1]
			case "reordered":
				e.Files[1], e.Files[2] = e.Files[2], e.Files[1]
			case "other parent":
				e.Files[2].Path = "Books/set.r01"
			case "parent alias":
				e.Files[2].Path = "books/../books/set.r01"
			case "other family":
				e.Files[2].Path = "books/other.r01"
			case "bad hash":
				e.Files[1].SHA256 = ""
			case "zero bytes":
				e.Files[1].Bytes = 0
			case "large part":
				e.Files[1].Bytes = (256 << 20) + 1
			case "large total":
				e = oldRARShape(17)
				for i := range e.Files {
					e.Files[i].Bytes = 256 << 20
				}
			case "unsafe leaf":
				e.ArchiveLeaves[0].Chain = []string{"../book.epub"}
			case "missing leaf":
				e.ArchiveLeaves = nil
			}
			for _, proof := range []string{rar4OldSetProof, rar5OldSetProof} {
				e.ArchiveProof = proof
				if supportedArchiveProof(e) {
					t.Fatal("uncertain old set gained authority", mutation, proof)
				}
			}
		})
	}
}
