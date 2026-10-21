package staging

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func rar5TestVolume(index, count int, records ...[]byte) []byte {
	main := rar5Numbers(1)
	if index > 0 {
		main = rar5Numbers(3, uint64(index))
	}
	b := append([]byte("Rar!\x1a\x07\x01\x00"), rar5Record(1, 0, main, nil)...)
	for _, record := range records {
		b = append(b, record...)
	}
	more := uint64(0)
	if index+1 < count {
		more = 1
	}
	return append(b, rar5Record(5, 0, rar5Numbers(more), nil)...)
}

func rar5TestSplit(name string, whole, packed []byte, before, after bool, compression uint64) []byte {
	flags := uint64(2)
	if before {
		flags |= 8
	}
	if after {
		flags |= 16
	}
	body := rar5Numbers(4, uint64(len(whole)), 0100644)
	crc := crc32.ChecksumIEEE(whole)
	if after {
		crc = crc32.ChecksumIEEE(packed)
	}
	body = binary.LittleEndian.AppendUint32(body, crc)
	body = append(body, rar5Numbers(compression, 1, uint64(len(name)))...)
	body = append(body, name...)
	return rar5Record(2, flags, body, packed)
}

func rarPartListing(whole, packed []byte, index, count int) string {
	before, after := "-", "-"
	if index > 0 {
		before = "+"
	}
	if index+1 < count {
		after = "+"
	}
	crc := crc32.ChecksumIEEE(whole)
	if after == "+" {
		crc = crc32.ChecksumIEEE(packed)
	}
	return fmt.Sprintf("Path = book.epub\nSize = %d\nPacked Size = %d\nAttributes = -rw-r--r--\nCRC = %08X\nEncrypted = -\nSplit Before = %s\nSplit After = %s\nVolume Index = %d\n\n", len(whole), len(packed), crc, before, after, index)
}

func updateRAR5SetHeaders(t *testing.T, src *os.Root, names []string) {
	updateRARSetHeaders(t, src, names, "Rar5")
}

func updateRARSetHeaders(t *testing.T, src *os.Root, names []string, kind string) {
	t.Helper()
	var total int64
	var first int64
	for i, name := range names {
		info, err := src.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		total += info.Size()
		if i == 0 {
			first = info.Size()
		}
		prefix := fmt.Sprintf("volume-%04d", i)
		header := fmt.Sprintf("Type = %s\nPhysical Size = %d\nMultivolume = +\nVolume Index = %d\nVolumes = 1\n", kind, info.Size(), i)
		if kind == "Rar" {
			header += "Characteristics = Volume NewVolName"
			if i == 0 {
				header += " FirstVolume"
			}
			header += "\n"
		}
		code := "0"
		if i+1 < len(names) {
			header += "ERROR = Missing volume : " + rarSnapshotName(false, i+1) + "\n"
			code = "2"
		}
		if i > 0 && kind == "Rar5" {
			header += "ERRORS:\nHeaders Error\n"
			code = "2"
		}
		writeFakeRARFile(t, prefix+".header", header)
		writeFakeRARFile(t, prefix+".code", code)
		writeFakeRARFile(t, prefix+".name", rarSnapshotName(false, i))
	}
	writeFakeRARFile(t, "header.txt", fmt.Sprintf("Type = %s\nPhysical Size = %d\nTotal Physical Size = %d\nMultivolume = +\nVolume Index = 0\nVolumes = %d\n", kind, first, total, len(names)))
}

func rar5SetFixture(t *testing.T, count int) (*os.Root, *os.Root, []string, [][]byte, []byte) {
	t.Helper()
	whole := []byte("generated volume body split across every adjacent boundary")
	src, dst := sevenFixture(t, []sevenMember{{name: "book.epub", body: whole, packed: fmt.Sprint(len(whole)), extra: "Split Before = -\nSplit After = -\nVolume Index = 0\n"}})
	var names []string
	var parts [][]byte
	for i := range count {
		piece := whole[i*len(whole)/count : (i+1)*len(whole)/count]
		b := rar5TestVolume(i, count, rar5TestSplit("book.epub", whole, piece, i > 0, i+1 < count, 0))
		name := fmt.Sprintf("book.part%d.rar", i+1)
		if err := src.WriteFile(name, b, 0o600); err != nil {
			t.Fatal(err)
		}
		names, parts = append(names, name), append(parts, b)
		writeFakeRARFile(t, fmt.Sprintf("volume-%04d.listing", i), rarPartListing(whole, piece, i, count))
	}
	updateRAR5SetHeaders(t, src, names)
	return src, dst, names, parts, whole
}

func rar5SetLimits() Limits { l := Defaults(); l.Coverage, l.RAR5SetCoverage = true, true; return l }

func TestRAR5SetExplicitCoverageUsesOneTableAndPreservesSources(t *testing.T) {
	for _, count := range []int{2, 3, 7} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			src, dst, names, parts, whole := rar5SetFixture(t, count)
			writeFakeRARFile(t, "require-utf8", "")
			writeFakeRARFile(t, "reject-ba", "")
			writeFakeRARFile(t, "trace-enabled", "")
			x, err := ExtractTree(context.Background(), src, names, dst, books, rar5SetLimits())
			if err != nil || !x.Whole || !x.Complete || x.RARSetVersion != 5 || x.RARVersion != 0 || x.RARContainers != 0 || len(x.Members) != 1 || x.Members[0].Disposition != "extracted" {
				t.Fatal(x, err)
			}
			got, err := dst.ReadFile(x.Members[0].Staged)
			if err != nil || !bytes.Equal(got, whole) {
				t.Fatal(got, err)
			}
			for i, name := range names {
				got, err := src.ReadFile(name)
				if err != nil || !bytes.Equal(got, parts[i]) {
					t.Fatal("source changed", name, err)
				}
			}
			trace, err := os.ReadFile(filepath.Join(os.Getenv("NEMALO_FAKE_7Z"), "trace"))
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(trace)), "\n")
			if len(lines) != count+2 || !strings.HasPrefix(lines[len(lines)-1], "e ") {
				t.Fatal("a replacement table or unexpected process was used", string(trace))
			}
			data, err := json.Marshal(x)
			if err != nil || bytes.Contains(data, []byte("RARSet")) || bytes.Contains(data, []byte("rar_set")) {
				t.Fatal(string(data), err)
			}
		})
	}
}

func TestRAR5SetFlagsLeaveDefaultIntakeAndStandaloneUnchanged(t *testing.T) {
	for _, coverage := range []bool{false, true} {
		for _, set := range []bool{false, true} {
			t.Run(fmt.Sprintf("%t/%t", coverage, set), func(t *testing.T) {
				src, dst, names, parts, _ := rar5SetFixture(t, 3)
				l := Defaults()
				l.Coverage, l.RAR5SetCoverage = coverage, set
				x, err := ExtractSet(context.Background(), src, names, dst, books, l)
				if coverage && !set {
					if err == nil || x.Whole {
						t.Fatal("coverage appeared without opt-in", x, err)
					}
				} else if err != nil || !x.Complete || x.Whole != (coverage && set) || x.RARSetVersion != map[bool]int{true: 5, false: 0}[coverage && set] {
					t.Fatal(x, err)
				}
				for _, part := range parts {
					if files, err := RAR5Accounted(context.Background(), bytes.NewReader(part), int64(len(part))); !errors.Is(err, ErrRAR5Coverage) || files != nil {
						t.Fatal("standalone authority broadened", files, err)
					}
				}
			})
		}
	}
}

func TestRAR5SetRefusesHiddenPhysicalRecordsAndTableMismatches(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*testing.T, *os.Root, []string, [][]byte, []byte)
	}{
		{"hidden_service", func(t *testing.T, src *os.Root, names []string, _ [][]byte, whole []byte) {
			piece := whole[:len(whole)/3]
			b := rar5TestVolume(0, 3, rar5TestSplit("book.epub", whole, piece, false, true, 0), rar5Record(3, 4, []byte("hidden"), nil))
			if err := src.WriteFile(names[0], b, 0o600); err != nil {
				t.Fatal(err)
			}
			updateRAR5SetHeaders(t, src, names)
		}},
		{"tail", func(t *testing.T, src *os.Root, names []string, parts [][]byte, _ []byte) {
			if err := src.WriteFile(names[1], append(parts[1], 0), 0o600); err != nil {
				t.Fatal(err)
			}
			updateRAR5SetHeaders(t, src, names)
		}},
		{"isolated_crc", func(t *testing.T, _ *os.Root, _ []string, _ [][]byte, whole []byte) {
			replaceRARFixture(t, "volume-0000.listing", fmt.Sprintf("CRC = %08X", crc32.ChecksumIEEE(whole[:len(whole)/3])), "CRC = DEADBEEF")
		}},
		{"isolated_packed", func(t *testing.T, _ *os.Root, _ []string, _ [][]byte, _ []byte) {
			replaceRARFixture(t, "volume-0001.listing", "Packed Size = ", "Packed Size = 9")
		}},
		{"isolated_ordinal", func(t *testing.T, _ *os.Root, _ []string, _ [][]byte, _ []byte) {
			replaceRARFixture(t, "volume-0001.listing", "Volume Index = 1", "Volume Index = 0")
		}},
		{"assembled_crc", func(t *testing.T, _ *os.Root, _ []string, _ [][]byte, whole []byte) {
			replaceRARFixture(t, "listing.txt", fmt.Sprintf("CRC = %08X", crc32.ChecksumIEEE(whole)), "CRC = DEADBEEF")
		}},
		{"assembled_start", func(t *testing.T, _ *os.Root, _ []string, _ [][]byte, _ []byte) {
			replaceRARFixture(t, "listing.txt", "Volume Index = 0", "Volume Index = 1")
		}},
		{"assembled_split_field", func(t *testing.T, _ *os.Root, _ []string, _ [][]byte, _ []byte) {
			replaceRARFixture(t, "listing.txt", "Split After = -\n", "")
		}},
		{"assembled_probe_summary", func(t *testing.T, _ *os.Root, _ []string, _ [][]byte, _ []byte) {
			replaceRARFixture(t, "listing.txt", "Volume Index = 0\n", "Volume Index = 0\n\nErrors: 1\n")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, dst, names, parts, whole := rar5SetFixture(t, 3)
			tc.mutate(t, src, names, parts, whole)
			x, err := ExtractTree(context.Background(), src, names, dst, books, rar5SetLimits())
			if err == nil || x.Whole || x.RARSetVersion != 0 || len(x.Members) != 0 {
				t.Fatal("unproven set extracted", x, err)
			}
		})
	}
}

func TestRAR5SetAssembledTableNormalizesCRLFWithoutProbeExceptions(t *testing.T) {
	text := "Listing archive: x\n\n--\nType = Rar5\n\n----------\nPath = book.epub\nSize = 3\n\n"
	for _, crlf := range []bool{false, true} {
		t.Run(fmt.Sprint(crlf), func(t *testing.T) {
			out := text
			if crlf {
				out = strings.ReplaceAll(out, "\n", "\r\n")
			}
			entries, err := rarListedMembers([]byte(out), false)
			if err != nil || len(entries) != 1 || entries[0].raw != "book.epub" {
				t.Fatal(entries, err)
			}
			out += "\nErrors: 1\n"
			if crlf {
				out = strings.ReplaceAll(out, "\nErrors: 1\n", "\r\nErrors: 1\r\n")
			}
			if entries, err := rarListedMembers([]byte(out), false); err == nil || entries != nil {
				t.Fatal("assembled table borrowed identity exception", entries, err)
			}
			if entries, err := rarListedMembers([]byte(out), true); err != nil || len(entries) != 1 {
				t.Fatal("isolated exception changed", entries, err)
			}
		})
	}
}

func replaceRARFixture(t *testing.T, name, from, to string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(os.Getenv("NEMALO_FAKE_7Z"), name))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), from) {
		t.Fatal("missing mutation target", name, from)
	}
	writeFakeRARFile(t, name, strings.Replace(string(b), from, to, 1))
}

func TestRAR5SetSnapshotIdentityAndOutputAreIndependent(t *testing.T) {
	t.Run("expected source", func(t *testing.T) {
		src, dst, names, parts, _ := rar5SetFixture(t, 3)
		l := rar5SetLimits()
		l.ExpectedSources = map[string]SnapshotIdentity{}
		for i, name := range names {
			l.ExpectedSources[name] = SnapshotIdentity{Bytes: int64(len(parts[i])), SHA256: fmt.Sprintf("%x", sha256.Sum256(parts[i]))}
		}
		if err := src.WriteFile(names[2], append(parts[2], 0), 0o600); err != nil {
			t.Fatal(err)
		}
		if x, err := ExtractTree(context.Background(), src, names, dst, books, l); err == nil || x.Whole || x.RARSetVersion != 0 {
			t.Fatal(x, err)
		}
	})
	t.Run("failed output", func(t *testing.T) {
		src, dst, names, _, _ := rar5SetFixture(t, 3)
		writeFakeRARFile(t, filepath.Join("members", fmt.Sprintf("%x", "book.epub")), "wrong")
		x, err := ExtractTree(context.Background(), src, names, dst, books, rar5SetLimits())
		if err != nil || !x.Whole || x.Complete || x.RARSetVersion != 5 || x.Members[0].Disposition != "failed" {
			t.Fatal(x, err)
		}
	})
}

func TestRAR5SetDirectCallsShareWorkAcrossVolumesAndFailures(t *testing.T) {
	testRARSetBudgets(t, rar5SetFixture, rar5SetLimits)
}

func testRARSetBudgets(t *testing.T, fixture func(*testing.T, int) (*os.Root, *os.Root, []string, [][]byte, []byte), bounds func() Limits) {
	t.Run("proof", func(t *testing.T) {
		src, dst, names, parts, whole := fixture(t, 3)
		l := bounds()
		l.tree = newTreeBudget(l)
		l.tree.proofs.remaining = int64(len(parts[0]) + len(parts[1]) - len(whole)*2/3 - 1)
		x, err := ExtractSet(context.Background(), src, names, dst, books, l)
		if !errors.Is(err, errTreeBudget) || x.Whole || x.RARSetVersion != 0 || l.tree.proofs.left() != 0 || l.tree.processes != maxRuns-2 {
			t.Fatal(x, err, l.tree.proofs.left(), l.tree.processes)
		}
		reads := l.tree.reads.left()
		x, err = ExtractSet(context.Background(), src, names, dst, books, l)
		if !errors.Is(err, errTreeBudget) || x.Whole || l.tree.proofs.left() != 0 || l.tree.reads.left() >= reads || l.tree.processes != maxRuns-3 {
			t.Fatal(x, err, l.tree.reads.left(), reads, l.tree.processes)
		}
	})
	t.Run("source copies", func(t *testing.T) {
		src, dst, names, parts, _ := fixture(t, 3)
		l := bounds()
		l.tree = newTreeBudget(l)
		l.tree.reads.remaining = int64(len(parts[0]) + 1)
		x, err := ExtractSet(context.Background(), src, names, dst, books, l)
		if !errors.Is(err, errTreeBudget) || x.Whole || l.tree.reads.left() != 0 || l.tree.processes != maxRuns-1 {
			t.Fatal(x, err, l.tree.reads.left(), l.tree.processes)
		}
		processes := l.tree.processes
		if x, err := ExtractSet(context.Background(), src, names, dst, books, l); !errors.Is(err, errTreeBudget) || x.Whole || l.tree.processes != processes {
			t.Fatal(x, err)
		}
	})
	t.Run("processes", func(t *testing.T) {
		src, dst, names, _, _ := fixture(t, 3)
		l := bounds()
		l.tree = newTreeBudget(l)
		l.tree.processes = 2
		if x, err := ExtractSet(context.Background(), src, names, dst, books, l); !errors.Is(err, errTreeBudget) || x.Whole || l.tree.processes != 0 {
			t.Fatal(x, err, l.tree.processes)
		}
	})
	t.Run("diagnostics", func(t *testing.T) {
		src, dst, names, _, _ := fixture(t, 3)
		l := bounds()
		l.tree = newTreeBudget(l)
		l.tree.diagnostics.remaining = 1
		if x, err := ExtractSet(context.Background(), src, names, dst, books, l); err == nil || x.Whole || l.tree.diagnostics.left() != 0 {
			t.Fatal(x, err, l.tree.diagnostics.left())
		}
	})
	t.Run("logical member count", func(t *testing.T) {
		src, dst, names, _, _ := fixture(t, 7)
		l := bounds()
		l.Members = 1
		if x, err := ExtractSet(context.Background(), src, names, dst, books, l); err != nil || !x.Whole || !x.Complete {
			t.Fatal(x, err)
		}
	})
	t.Run("output budget", func(t *testing.T) {
		src, dst, names, _, whole := fixture(t, 3)
		l := bounds()
		l.TotalBytes = int64(len(whole))
		if x, err := ExtractSet(context.Background(), src, names, dst, books, l); err != nil || !x.Whole || x.Complete || x.Members[0].Disposition != "failed" {
			t.Fatal(x, err)
		}
	})
}

func TestRAR5SetRefusesOtherLayoutsWithoutChangingSources(t *testing.T) {
	for _, mode := range []string{"old_names", "child", "rar4"} {
		t.Run(mode, func(t *testing.T) {
			src, dst, names, parts, _ := rar5SetFixture(t, 3)
			l := rar5SetLimits()
			switch mode {
			case "old_names":
				for i, old := range names {
					next := "legacy.rar"
					if i > 0 {
						next = fmt.Sprintf("legacy.r%02d", i-1)
					}
					if err := src.Rename(old, next); err != nil {
						t.Fatal(err)
					}
					names[i] = next
				}
			case "child":
				l.child = true
			case "rar4":
				for i, name := range names {
					parts[i] = []byte("Rar!\x1a\x07\x00 volume")
					if err := src.WriteFile(name, parts[i], 0o600); err != nil {
						t.Fatal(err)
					}
				}
				l.RAR4Coverage = true
			}
			if x, err := ExtractSet(context.Background(), src, names, dst, books, l); err == nil || x.Whole || x.RARSetVersion != 0 || len(x.Members) != 0 {
				t.Fatal(x, err)
			}
			for i, name := range names {
				got, err := src.ReadFile(name)
				if err != nil || !bytes.Equal(got, parts[i]) {
					t.Fatal("unsupported source changed", name, err)
				}
			}
		})
	}
}

func TestRAR5SetCancellationStopsActiveIdentityProbe(t *testing.T) {
	testRARSetCancellation(t, rar5SetFixture, rar5SetLimits)
}

func testRARSetCancellation(t *testing.T, fixture func(*testing.T, int) (*os.Root, *os.Root, []string, [][]byte, []byte), bounds func() Limits) {
	src, dst, names, _, _ := fixture(t, 3)
	writeFakeRARFile(t, "trace-enabled", "")
	writeFakeRARFile(t, "wait", "1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		x   Extraction
		err error
	}
	done := make(chan outcome, 1)
	go func() { x, err := ExtractSet(ctx, src, names, dst, books, bounds()); done <- outcome{x, err} }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		b, _ := os.ReadFile(filepath.Join(os.Getenv("NEMALO_FAKE_7Z"), "trace"))
		if len(b) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("identity process did not start")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case result := <-done:
		if !errors.Is(result.err, context.Canceled) || result.x.Whole || result.x.RARSetVersion != 0 {
			t.Fatal(result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("active identity process ignored cancellation")
	}
}

func TestRAR5SetDirectWorkBudgetDoesNotEnableNestedTraversal(t *testing.T) {
	for _, tree := range []bool{false, true} {
		t.Run(fmt.Sprint(tree), func(t *testing.T) {
			src, dst, names, _, whole := rar5SetFixture(t, 3)
			for i, name := range names {
				piece := whole[i*len(whole)/3 : (i+1)*len(whole)/3]
				b := rar5TestVolume(i, 3, rar5TestSplit("inside.zip", whole, piece, i > 0, i < 2, 0))
				if err := src.WriteFile(name, b, 0o600); err != nil {
					t.Fatal(err)
				}
				replaceRARFixture(t, fmt.Sprintf("volume-%04d.listing", i), "Path = book.epub", "Path = inside.zip")
			}
			updateRAR5SetHeaders(t, src, names)
			replaceRARFixture(t, "listing.txt", "Path = book.epub", "Path = inside.zip")
			writeFakeRARFile(t, filepath.Join("members", fmt.Sprintf("%x", "inside.zip")), string(whole))
			var x Extraction
			var err error
			if tree {
				x, err = ExtractTree(context.Background(), src, names, dst, func(string) bool { return true }, rar5SetLimits())
			} else {
				x, err = ExtractSet(context.Background(), src, names, dst, func(string) bool { return true }, rar5SetLimits())
			}
			if err != nil || x.RARSetVersion != 5 || len(x.Members) != 1 {
				t.Fatal(x, err)
			}
			if tree {
				if x.Whole || x.Complete || x.CoverageGap == nil || !reflect.DeepEqual(x.CoverageGap.Chain, []string{"inside.zip"}) {
					t.Fatal("unread child gained tree coverage", x)
				}
			} else {
				if !x.Whole || !x.Complete || x.Members[0].Disposition != "nested_archive" || x.Members[0].Staged != "" {
					t.Fatal("a shared work budget enabled nested extraction", x)
				}
			}
		})
	}
}

func TestVolumeSizeBoundHandlesExtremeMetadata(t *testing.T) {
	for _, c := range []struct {
		total, next int64
		want        bool
	}{
		{0, maxArchiveSnapshot, true}, {maxArchiveSnapshot - 1, 1, true}, {maxArchiveSnapshot, 1, false},
		{1, math.MaxInt64, false}, {maxArchiveSnapshot, math.MaxInt64, false}, {math.MaxInt64, 1, false},
		{-1, 1, false}, {0, 0, false}, {0, -1, false},
	} {
		if got := volumeSizeFits(c.total, c.next); got != c.want {
			t.Fatalf("total=%d next=%d fits=%t want=%t", c.total, c.next, got, c.want)
		}
	}
}
