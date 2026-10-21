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

func rar4TableMatches(files []RAR4File, entries []sevenEntry) bool {
	return rarTableMatches(rar4BindingFiles(files), entries)
}

func TestRAR4ReaderTableUsesTheCommonExactBinding(t *testing.T) {
	for _, kind := range []string{"exact", "reordered", "missing", "duplicate", "physical_duplicate", "raw_name", "normalized_name", "size", "packed", "unknown_size", "unknown_packed", "unknown_crc", "crc", "directory", "link", "encrypted", "split", "zero_crc"} {
		t.Run(kind, func(t *testing.T) {
			files := []RAR4File{{Name: "book.epub", Bytes: 100, Packed: 70, CRC32: 123}, {Name: "folder", Directory: true}}
			entries := []sevenEntry{{raw: "book.epub", name: "book.epub", size: 100, packed: 70, crc: 123, crcKnown: true, sizeKnown: true, packedKnown: true, valid: true}, {raw: "folder", name: "folder", folder: true, sizeKnown: true, packedKnown: true, valid: true}}
			switch kind {
			case "reordered":
				entries[0], entries[1] = entries[1], entries[0]
			case "missing":
				entries = entries[:1]
			case "duplicate":
				entries[1] = entries[0]
			case "physical_duplicate":
				files[1] = files[0]
			case "raw_name":
				entries[0].raw = "Book.epub"
			case "normalized_name":
				entries[0].name = "alias.epub"
			case "size":
				entries[0].size++
			case "packed":
				entries[0].packed++
			case "unknown_size":
				entries[0].sizeKnown = false
			case "unknown_packed":
				entries[0].packedKnown = false
			case "unknown_crc":
				entries[0].crcKnown = false
			case "crc":
				entries[0].crc++
			case "directory":
				entries[0].folder = true
			case "link":
				entries[0].link = true
			case "encrypted":
				entries[0].encrypted = true
			case "split":
				entries[0].splitAfter = true
			case "zero_crc":
				files[0].CRC32 = 0
				entries[0].crc = 0
			}
			if got := rar4TableMatches(files, entries); got != (kind == "exact" || kind == "reordered" || kind == "zero_crc") {
				t.Fatal(kind, got)
			}
		})
	}
}

func TestRAR4ReaderBindingRequiresExplicitOptIn(t *testing.T) {
	for _, kind := range []string{"default_intake", "flag_without_coverage", "coverage_without_opt_in", "covered", "changed_size", "unknown_packed", "changed_crc", "changed_output", "child", "snapshot_mismatch"} {
		t.Run(kind, func(t *testing.T) {
			payload := []byte("synthetic book")
			member := sevenMember{name: "book.epub", body: payload, packed: "14"}
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
			writeFakeRARFile(t, "kind.txt", "-tRar")
			data := rar4Archive(rar4Book("book.epub"))
			if err := src.WriteFile("bundle.rar", data, 0600); err != nil {
				t.Fatal(err)
			}
			limits := Defaults()
			limits.Coverage = kind != "default_intake" && kind != "flag_without_coverage"
			limits.RAR4Coverage = kind != "default_intake" && kind != "coverage_without_opt_in"
			if limits.Coverage && limits.RAR4Coverage && kind != "child" && kind != "snapshot_mismatch" {
				writeFakeRARFile(t, "require-utf8", "1")
			}
			limits.child = kind == "child"
			if kind == "snapshot_mismatch" {
				limits.ExpectedSources = map[string]SnapshotIdentity{"bundle.rar": {Bytes: int64(len(data)), SHA256: "0000000000000000000000000000000000000000000000000000000000000000"}}
			}
			x, err := Extract7z(context.Background(), src, "bundle.rar", dst, books, limits)
			switch kind {
			case "covered":
				if err != nil || !x.Whole || !x.Complete || len(x.Members) != 1 || x.Members[0].Disposition != "extracted" {
					t.Fatal(x, err)
				}
				if got, err := dst.ReadFile(x.Members[0].Staged); err != nil || !bytes.Equal(got, payload) {
					t.Fatal(got, err)
				}
			case "default_intake", "flag_without_coverage":
				if err != nil || x.Whole || !x.Complete || len(x.Members) != 1 || x.Members[0].Disposition != "extracted" {
					t.Fatal("default intake changed", x, err)
				}
			case "changed_output":
				if err != nil || !x.Whole || x.Complete || len(x.Members) != 1 || x.Members[0].Disposition != "failed" {
					t.Fatal("framing and integrity conflated", x, err)
				}
			default:
				entries, readErr := os.ReadDir(dst.Name())
				if err == nil || x.Whole || readErr != nil || len(entries) != 0 {
					t.Fatal("unproven reader wrote members", x, err, readErr, entries)
				}
				if kind == "snapshot_mismatch" && !errors.Is(err, ErrSourceChanged) {
					t.Fatal(err)
				}
				if (kind == "changed_size" || kind == "unknown_packed" || kind == "changed_crc") && !errors.Is(err, ErrRAR4Coverage) {
					t.Fatal(err)
				}
			}
			if got, err := src.ReadFile("bundle.rar"); err != nil || !bytes.Equal(got, data) {
				t.Fatal("source changed", err)
			}
		})
	}
}

func TestRAR4BindingChargesSharedProofBeforeReaderAndRejectsMissingEvidence(t *testing.T) {
	src, dst := sevenFixture(t, []sevenMember{{name: "book.epub", body: []byte("synthetic book"), packed: "14"}})
	writeFakeRARFile(t, "kind.txt", "-tRar")
	if err := src.WriteFile("bundle.rar", rar4Archive(rar4Book("book.epub")), 0600); err != nil {
		t.Fatal(err)
	}
	writeFakeRARFile(t, "wait", "1")
	limits := Defaults()
	limits.Coverage = true
	limits.RAR4Coverage = true
	limits.tree = &treeBudget{reads: allowance{remaining: 32}, proofs: allowance{remaining: 15}, processes: 1000, diagnostics: allowance{remaining: 16 << 20}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	x, err := Extract7z(ctx, src, "bundle.rar", dst, books, limits)
	if !errors.Is(err, errTreeBudget) || x.Whole || len(x.Members) != 0 || limits.tree.processes != 1000 || limits.tree.proofs.left() != 0 || limits.tree.reads.left() != 18 {
		t.Fatal(x, err, limits.tree.processes, limits.tree.proofs.left(), limits.tree.reads.left())
	}
	// Volume callers provide no standalone framing facts. They cannot borrow
	// authority from an opted-in reader configuration or a successful listing.
	x, err = extractSnapshot(ctx, "unused", "Rar", "unused", 1, dst, books, limits, nil)
	if !errors.Is(err, ErrRAR4Coverage) || x.Whole || len(x.Members) != 0 || limits.tree.processes != 1000 {
		t.Fatal("missing framing reached reader", fmt.Sprint(x), err)
	}
}

func TestRAR4HiddenRecordsDoNotChangeDefaultIntake(t *testing.T) {
	for _, coverage := range []bool{false, true} {
		t.Run(fmt.Sprint(coverage), func(t *testing.T) {
			src, dst := sevenFixture(t, []sevenMember{{name: "book.epub", body: []byte("synthetic book"), packed: "14"}})
			writeFakeRARFile(t, "kind.txt", "-tRar")
			data := rar4Archive(rar4Book("book.epub"), rar4Block(0x7a, 0x8000, []byte{4, 0, 0, 0}, []byte("keep")))
			if err := src.WriteFile("bundle.rar", data, 0600); err != nil {
				t.Fatal(err)
			}
			limits := Defaults()
			limits.Coverage = coverage
			limits.RAR4Coverage = true
			x, err := Extract7z(context.Background(), src, "bundle.rar", dst, books, limits)
			if coverage {
				if !errors.Is(err, ErrRAR4Coverage) || x.Whole || len(x.Members) != 0 {
					t.Fatal("unlisted data gained coverage", x, err)
				}
			} else if err != nil || x.Whole || !x.Complete || len(x.Members) != 1 || x.Members[0].Disposition != "extracted" {
				t.Fatal("default intake changed", x, err)
			}
			got, err := src.ReadFile("bundle.rar")
			if err != nil || !bytes.Equal(got, data) {
				t.Fatal("source changed", err)
			}
		})
	}
}
