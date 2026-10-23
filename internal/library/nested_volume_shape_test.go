package library

import (
	"strings"
	"testing"
)

func TestNestedVolumeProofFirstSelectionsDoNotBroadenEarlierSyntax(t *testing.T) {
	for _, name := range []string{"shelf/set.zip.001", "shelf/set.ZIP.0001", "shelf/set.7z.001", "shelf/set.z01", "shelf/set.Z0001", "shelf/set.part1.rar", "shelf/set.rar"} {
		t.Run(name, func(t *testing.T) {
			e := oldRARShape(3)
			e.ArchiveProof = archiveVolumeTreeProof
			e.ArchiveLeaves[0].Chain = []string{name, "book.epub"}
			if !supportedArchiveProof(e) || !validArchiveProofLeaves(e) {
				t.Fatal("supported first selection refused", e)
			}
			ordinaryRAR := strings.HasSuffix(strings.ToLower(name), ".rar")
			if validArchiveLeaves(e) != ordinaryRAR {
				t.Fatal("earlier leaf syntax changed", name)
			}
			for _, proof := range []string{rar4OldSetProof, rar5OldSetProof, rar4SetProof, rar5SetProof, rar4FramesProof, rar5FramesProof, archiveTreeProof, archiveRARTreeProof} {
				e.ArchiveProof = proof
				if !ordinaryRAR && supportedArchiveProof(e) {
					t.Fatal("new first-part syntax supplied earlier authority", proof, name)
				}
			}
		})
	}
}

func TestNestedVolumeProofRefusesContinuationsAndMalformedLeaves(t *testing.T) {
	for _, name := range []string{"shelf/set.zip.002", "shelf/set.7z.0000", "shelf/set.z02", "shelf/set.part2.rar", "shelf/set.r00", "../set.zip.001", "/set.rar", "shelf\\set.rar", "drive:set.rar", "bad\x00.rar", ""} {
		t.Run(name, func(t *testing.T) {
			e := oldRARShape(3)
			e.ArchiveProof = archiveVolumeTreeProof
			e.ArchiveLeaves[0].Chain = []string{name, "book.epub"}
			if supportedArchiveProof(e) || validArchiveProofLeaves(e) {
				t.Fatal("unsafe or continuation selection accepted", e)
			}
		})
	}
	for _, kind := range []string{"leaf hash", "holding", "duplicate", "depth", "empty", "root parent", "root order"} {
		t.Run(kind, func(t *testing.T) {
			e := oldRARShape(3)
			e.ArchiveProof = archiveVolumeTreeProof
			e.ArchiveLeaves[0].Chain = []string{"set.zip.001", "book.epub"}
			switch kind {
			case "leaf hash":
				e.ArchiveLeaves[0].SHA256 = "invalid"
			case "holding":
				e.ArchiveLeaves[0].Holding = "sha256:" + strings.Repeat("b", 64)
			case "duplicate":
				e.ArchiveLeaves = append(e.ArchiveLeaves, e.ArchiveLeaves[0])
				e.Holdings = append(e.Holdings, e.Holdings[0])
			case "depth":
				e.ArchiveLeaves[0].Chain = []string{"a.zip", "b.zip", "c.zip", "d.zip", "book.epub"}
			case "empty":
				e.ArchiveLeaves = nil
				e.Holdings = nil
			case "root parent":
				e.Files[1].Path = "other/set.r00"
			case "root order":
				e.Files[1], e.Files[2] = e.Files[2], e.Files[1]
			}
			if supportedArchiveProof(e) {
				t.Fatal("malformed new proof accepted", kind, e)
			}
		})
	}
}
