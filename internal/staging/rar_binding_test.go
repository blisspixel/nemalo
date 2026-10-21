package staging

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

func rar5TableMatches(files []RAR5File, entries []sevenEntry) bool {
	return rarTableMatches(context.Background(), rar5BindingFiles(files), entries)
}

func TestRAR5ReaderTableBindsEveryExactRecord(t *testing.T) {
	for _, kind := range []string{"exact", "reordered", "missing", "extra", "duplicate", "physical_duplicate", "raw_name", "normalized_name", "bytes", "packed", "unknown_size", "unknown_packed", "directory", "unknown_crc", "crc", "invalid", "malformed", "link", "special", "encrypted", "split", "split_before", "split_after", "directory_crc", "directory_unknown_packed"} {
		t.Run(kind, func(t *testing.T) {
			files := []RAR5File{{Name: "book.epub", Bytes: 100, Packed: 70, CRC32: 123, HasCRC: true}, {Name: "folder", Directory: true}}
			entries := []sevenEntry{{raw: "book.epub", name: "book.epub", size: 100, packed: 70, crc: 123, crcKnown: true, sizeKnown: true, packedKnown: true, valid: true},
				{raw: "folder", name: "folder", folder: true, sizeKnown: true, packedKnown: true, valid: true}}
			e := &entries[0]
			switch kind {
			case "reordered":
				entries[0], entries[1] = entries[1], entries[0]
			case "missing":
				entries = entries[:1]
			case "extra":
				entries = append(entries, entries[0])
			case "duplicate":
				entries[1] = entries[0]
			case "physical_duplicate":
				files[1] = files[0]
			case "raw_name":
				e.raw = "Book.epub"
			case "normalized_name":
				e.name = "alias.epub"
			case "bytes":
				e.size++
			case "packed":
				e.packed++
			case "unknown_size":
				e.sizeKnown = false
			case "unknown_packed":
				e.packedKnown = false
			case "directory":
				e.folder = true
			case "unknown_crc":
				e.crcKnown = false
			case "crc":
				e.crc++
			case "invalid":
				e.valid = false
			case "malformed":
				e.malformed = true
			case "link":
				e.link = true
			case "special":
				e.special = true
			case "encrypted":
				e.encrypted = true
			case "split":
				e.split = true
			case "split_before":
				e.splitBefore = true
			case "split_after":
				e.splitAfter = true
			case "directory_crc":
				entries[1].crcKnown, entries[1].crc = true, 1
			case "directory_unknown_packed":
				entries[1].packedKnown = false
			}
			if got := rar5TableMatches(files, entries); got != (kind == "exact" || kind == "reordered") {
				t.Fatal("reader table binding", kind, got)
			}
		})
	}
}

func TestRAR5RequestedCoverageBindsTheExtractionTable(t *testing.T) {
	for _, kind := range []string{"exact", "changed_size", "unknown_packed", "changed_crc", "changed_output"} {
		t.Run(kind, func(t *testing.T) {
			payload := []byte("synthetic book")
			member := sevenMember{name: "book.epub", body: payload, packed: fmt.Sprint(len(payload))}
			switch kind {
			case "changed_size":
				member.listedSize = 15
			case "unknown_packed":
				member.packed = ""
			case "changed_crc":
				member.listedCRC = "00000000"
			case "changed_output":
				member.served = bytes.Repeat([]byte("x"), len(payload))
			}
			src, dst := sevenFixture(t, []sevenMember{member})
			archive := rar5Archive(rar5Book("book.epub"))
			if err := src.WriteFile("bundle.rar", archive, 0600); err != nil {
				t.Fatal(err)
			}
			writeFakeRARFile(t, "require-utf8", "1")
			limits := Defaults()
			limits.Coverage = true
			x, err := Extract7z(context.Background(), src, "bundle.rar", dst, books, limits)
			switch kind {
			case "exact":
				if err != nil || !x.Whole || !x.Complete || len(x.Members) != 1 || x.Members[0].Disposition != "extracted" {
					t.Fatal(x, err)
				}
				got, err := dst.ReadFile(x.Members[0].Staged)
				if err != nil || !bytes.Equal(got, payload) {
					t.Fatal(got, err)
				}
			case "changed_output":
				if err != nil || !x.Whole || x.Complete || len(x.Members) != 1 || x.Members[0].Disposition != "failed" {
					t.Fatal("framing or content check was conflated", x, err)
				}
			default:
				entries, readErr := os.ReadDir(dst.Name())
				if !errors.Is(err, ErrRAR5Coverage) || readErr != nil || len(entries) != 0 {
					t.Fatal("mismatched table extracted bytes", x, err, readErr, entries)
				}
			}
			got, err := src.ReadFile("bundle.rar")
			if err != nil || !bytes.Equal(got, archive) {
				t.Fatal("source changed", err)
			}
		})
	}
}

func TestRAR5CoverageRejectsHiddenRecordsWithoutChangingDefaultIntake(t *testing.T) {
	for _, coverage := range []bool{false, true} {
		t.Run(fmt.Sprint(coverage), func(t *testing.T) {
			payload := []byte("synthetic book")
			src, dst := sevenFixture(t, []sevenMember{{name: "book.epub", body: payload, packed: fmt.Sprint(len(payload))}})
			archive := rar5Archive(rar5Book("book.epub"), rar5Record(3, 0, []byte("unlisted service"), nil))
			if err := src.WriteFile("bundle.rar", archive, 0600); err != nil {
				t.Fatal(err)
			}
			limits := Defaults()
			limits.Coverage = coverage
			x, err := Extract7z(context.Background(), src, "bundle.rar", dst, books, limits)
			if coverage {
				if !errors.Is(err, ErrRAR5Coverage) || x.Whole || len(x.Members) != 0 {
					t.Fatal("hidden record granted coverage", x, err)
				}
			} else if err != nil || x.Whole || !x.Complete || len(x.Members) != 1 || x.Members[0].Disposition != "extracted" {
				t.Fatal("default intake changed", x, err)
			}
		})
	}
}

func TestRAR5ReaderBindingDoesNotUseAZeroCRCAsMissing(t *testing.T) {
	files := []RAR5File{{Name: "book.epub", Bytes: 100, Packed: 50, CRC32: 0, HasCRC: true}}
	entries := []sevenEntry{{raw: "book.epub", name: "book.epub", size: 100, packed: 50, crcKnown: true, sizeKnown: true, packedKnown: true, valid: true}}
	if !rar5TableMatches(files, entries) {
		t.Fatal("an explicitly recorded zero CRC became missing")
	}
	files[0].HasCRC = false
	if rar5TableMatches(files, entries) {
		t.Fatal("missing physical CRC became known")
	}
}

func TestRAR5CoverageSharesProofWorkBeforeInvokingTheReader(t *testing.T) {
	payload := []byte("synthetic book")
	src, dst := sevenFixture(t, []sevenMember{{name: "book.epub", body: payload, packed: "14"}})
	if err := src.WriteFile("bundle.rar", rar5Archive(rar5Book("book.epub")), 0600); err != nil {
		t.Fatal(err)
	}
	// If the adapter forgets the shared wrapper, its first subprocess waits
	// until cancellation instead of failing at the proof allowance.
	writeFakeRARFile(t, "wait", "1")
	limits := Defaults()
	limits.Coverage = true
	limits.tree = &treeBudget{reads: allowance{remaining: 32}, proofs: allowance{remaining: 15}, processes: 1000, diagnostics: allowance{remaining: 16 << 20}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	x, err := Extract7z(ctx, src, "bundle.rar", dst, books, limits)
	if !errors.Is(err, errTreeBudget) || x.Whole || len(x.Members) != 0 || limits.tree.processes != 1000 || limits.tree.proofs.left() != 0 || limits.tree.reads.left() != 17 {
		t.Fatal("adapter bypassed shared proof work", x, err, limits.tree.processes, limits.tree.proofs.left(), limits.tree.reads.left())
	}
}

func TestRAR5CoveragePreservesChildScopeAndSnapshotBinding(t *testing.T) {
	for _, kind := range []string{"child", "snapshot_mismatch"} {
		t.Run(kind, func(t *testing.T) {
			src, dst := sevenFixture(t, []sevenMember{{name: "book.epub", body: []byte("synthetic book"), packed: "14"}})
			archive := rar5Archive(rar5Book("book.epub"))
			if err := src.WriteFile("bundle.rar", archive, 0600); err != nil {
				t.Fatal(err)
			}
			limits := Defaults()
			limits.Coverage = true
			limits.child = kind == "child"
			if kind == "snapshot_mismatch" {
				limits.ExpectedSources = map[string]SnapshotIdentity{"bundle.rar": {Bytes: int64(len(archive)), SHA256: "0000000000000000000000000000000000000000000000000000000000000000"}}
			}
			x, err := Extract7z(context.Background(), src, "bundle.rar", dst, books, limits)
			if err == nil || x.Whole || len(x.Members) != 0 || (kind == "snapshot_mismatch" && !errors.Is(err, ErrSourceChanged)) {
				t.Fatal("unsupported scope or source bytes granted coverage", x, err)
			}
			got, readErr := src.ReadFile("bundle.rar")
			if readErr != nil || !bytes.Equal(got, archive) {
				t.Fatal("source changed", readErr)
			}
		})
	}
}
