package staging

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func rar4SetLimits() Limits {
	l := Defaults()
	l.Coverage, l.RAR4Coverage, l.RAR4SetCoverage = true, true, true
	return l
}

func rar4SetFixture(t *testing.T, count int) (*os.Root, *os.Root, []string, [][]byte, []byte) {
	t.Helper()
	whole := []byte("generated volume body split across every adjacent boundary")
	src, dst := sevenFixture(t, []sevenMember{{name: "book.epub", body: whole, packed: fmt.Sprint(len(whole)), extra: "Split Before = -\nSplit After = -\nVolume Index = 0\n"}})
	writeFakeRARFile(t, "kind.txt", "-tRar")
	var names []string
	var parts [][]byte
	for i := range count {
		piece := whole[i*len(whole)/count : (i+1)*len(whole)/count]
		b := rar4TestVolume(i, count, i%2 == 1, rar4TestSplit("book.epub", whole, piece, i > 0, i+1 < count, 0x30))
		name := fmt.Sprintf("book.part%d.rar", i+1)
		if err := src.WriteFile(name, b, 0600); err != nil {
			t.Fatal(err)
		}
		names, parts = append(names, name), append(parts, b)
		writeFakeRARFile(t, fmt.Sprintf("volume-%04d.listing", i), rarPartListing(whole, piece, i, count))
	}
	updateRARSetHeaders(t, src, names, "Rar")
	return src, dst, names, parts, whole
}

func TestRAR4SetOptInBindsOneTableAndPreservesEverySource(t *testing.T) {
	for _, count := range []int{2, 3, 7} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			src, dst, names, parts, whole := rar4SetFixture(t, count)
			writeFakeRARFile(t, "require-utf8", "")
			writeFakeRARFile(t, "reject-ba", "")
			writeFakeRARFile(t, "trace-enabled", "")
			x, err := ExtractTree(context.Background(), src, names, dst, books, rar4SetLimits())
			if err != nil || !x.Whole || !x.Complete || x.RARSetVersion != 4 || x.RARVersion != 0 || x.RARContainers != 0 || len(x.Members) != 1 || x.Members[0].Disposition != "extracted" {
				t.Fatal(x, err)
			}
			got, err := dst.ReadFile(x.Members[0].Staged)
			if err != nil || !bytes.Equal(got, whole) {
				t.Fatal(got, err)
			}
			for i, n := range names {
				got, err := src.ReadFile(n)
				if err != nil || !bytes.Equal(got, parts[i]) {
					t.Fatal("source changed", n, err)
				}
			}
			trace, err := os.ReadFile(filepath.Join(os.Getenv("NEMALO_FAKE_7Z"), "trace"))
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(trace)), "\n")
			if len(lines) != count+2 || !strings.HasPrefix(lines[len(lines)-1], "e ") {
				t.Fatal("replacement table or unexpected process", string(trace))
			}
			data, err := json.Marshal(x)
			if err != nil || bytes.Contains(data, []byte("RARSet")) || bytes.Contains(data, []byte("rar_set")) {
				t.Fatal(string(data), err)
			}
		})
	}
}

func TestRAR4SetRequiresEveryExplicitCapability(t *testing.T) {
	for _, coverage := range []bool{false, true} {
		for _, four := range []bool{false, true} {
			for _, set := range []bool{false, true} {
				t.Run(fmt.Sprintf("%t/%t/%t", coverage, four, set), func(t *testing.T) {
					src, dst, names, parts, _ := rar4SetFixture(t, 3)
					l := Defaults()
					l.Coverage, l.RAR4Coverage, l.RAR4SetCoverage = coverage, four, set
					x, err := ExtractSet(context.Background(), src, names, dst, books, l)
					if coverage && four && set {
						if err != nil || !x.Whole || !x.Complete || x.RARSetVersion != 4 {
							t.Fatal(x, err)
						}
					} else if x.Whole || x.RARSetVersion != 0 || (!coverage && (err != nil || !x.Complete)) {
						t.Fatal("coverage appeared without every opt-in", x, err)
					}
					for i, n := range names {
						got, err := src.ReadFile(n)
						if err != nil || !bytes.Equal(got, parts[i]) {
							t.Fatal("source changed", n, err)
						}
					}
				})
			}
		}
	}
	for _, kind := range []string{"Rar", "Rar5"} {
		for _, both := range []bool{false, true} {
			t.Run(kind+fmt.Sprint(both), func(t *testing.T) {
				fixture, l, wantErr, wantVersion := rar4SetFixture, rar5SetLimits(), ErrRAR5Coverage, 4
				if kind == "Rar5" {
					fixture, l, wantErr, wantVersion = rar5SetFixture, rar4SetLimits(), ErrRAR4Coverage, 5
				}
				src, dst, names, _, _ := fixture(t, 3)
				if both {
					l.RAR4Coverage, l.RAR4SetCoverage, l.RAR5SetCoverage = true, true, true
				}
				x, err := ExtractTree(context.Background(), src, names, dst, books, l)
				if both {
					if err != nil || !x.Whole || !x.Complete || x.RARSetVersion != wantVersion {
						t.Fatal(x, err)
					}
				} else if !errors.Is(err, wantErr) || x.Whole || x.RARSetVersion != 0 {
					t.Fatal("one version borrowed another's capability", x, err)
				}
			})
		}
	}
}

func TestRAR4SetRefusesUnaccountedPhysicalAndReaderEvidence(t *testing.T) {
	for _, kind := range []string{"service", "tail", "ordinal", "reserved", "isolated_split", "isolated_crc", "assembled_size", "assembled_packed", "assembled_crc", "assembled_volume", "assembled_errors"} {
		t.Run(kind, func(t *testing.T) {
			src, dst, names, parts, whole := rar4SetFixture(t, 3)
			switch kind {
			case "service", "tail", "ordinal", "reserved":
				b := bytes.Clone(parts[1])
				end := len(b) - 16
				switch kind {
				case "service":
					body := append(bytes.Clone(b[:end]), rar4Block(0x7a, 0x8000, []byte{4, 0, 0, 0}, []byte("keep"))...)
					b = append(body, b[end:]...)
				case "tail":
					b = append(b, []byte("unlisted")...)
				case "ordinal":
					copy(b[end:], rar4Block(0x7b, 13, []byte{2, 0, 0, 0, 0, 0, 0, 0, 0}, nil))
				case "reserved":
					copy(b[end:], rar4Block(0x7b, 13, []byte{1, 0, 0, 0, 0, 0, 0, 0, 1}, nil))
				}
				parts[1] = b
				if err := src.WriteFile(names[1], b, 0600); err != nil {
					t.Fatal(err)
				}
				updateRARSetHeaders(t, src, names, "Rar")
			case "isolated_split":
				writeFakeRARFile(t, "volume-0001.listing", strings.Replace(rarPartListing(whole, whole[len(whole)/3:2*len(whole)/3], 1, 3), "Split Before = +", "Split Before = -", 1))
			case "isolated_crc":
				writeFakeRARFile(t, "volume-0001.listing", strings.Replace(rarPartListing(whole, whole[len(whole)/3:2*len(whole)/3], 1, 3), "CRC = ", "CRC = 1", 1))
			default:
				path := filepath.Join(os.Getenv("NEMALO_FAKE_7Z"), "listing.txt")
				b, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				listing := string(b)
				switch kind {
				case "assembled_size":
					listing = strings.Replace(listing, fmt.Sprintf("Size = %d\n", len(whole)), fmt.Sprintf("Size = %d\n", len(whole)+1), 1)
				case "assembled_packed":
					listing = strings.Replace(listing, fmt.Sprintf("Packed Size = %d\n", len(whole)), "Packed Size = 1\n", 1)
				case "assembled_crc":
					listing = strings.Replace(listing, "CRC = ", "CRC = 1", 1)
				case "assembled_volume":
					listing = strings.Replace(listing, "Volume Index = 0", "Volume Index = 1", 1)
				case "assembled_errors":
					listing += "\n\nErrors: 1\n"
				}
				writeFakeRARFile(t, "listing.txt", listing)
			}
			writeFakeRARFile(t, "trace-enabled", "")
			x, err := ExtractTree(context.Background(), src, names, dst, books, rar4SetLimits())
			if err == nil || x.Whole || x.RARSetVersion != 0 || len(x.Members) != 0 {
				t.Fatal("unaccounted set accepted", x, err)
			}
			for i, n := range names {
				got, err := src.ReadFile(n)
				if err != nil || !bytes.Equal(got, parts[i]) {
					t.Fatal("source changed", n, err)
				}
			}
			trace, err := os.ReadFile(filepath.Join(os.Getenv("NEMALO_FAKE_7Z"), "trace"))
			if err != nil || strings.Contains("\n"+string(trace), "\ne ") {
				t.Fatal("output attempted before complete evidence", string(trace), err)
			}
		})
	}
}

func TestRAR4SetSharesAllBudgetsIncludingFailures(t *testing.T) {
	testRARSetBudgets(t, rar4SetFixture, rar4SetLimits)
}

func TestRAR4SetCancellationStopsActiveIdentityProbe(t *testing.T) {
	testRARSetCancellation(t, rar4SetFixture, rar4SetLimits)
}

func TestRAR4SetSnapshotBindingAndOutputAreIndependent(t *testing.T) {
	for _, kind := range []string{"source", "output"} {
		t.Run(kind, func(t *testing.T) {
			src, dst, names, parts, _ := rar4SetFixture(t, 3)
			l := rar4SetLimits()
			if kind == "source" {
				l.ExpectedSources = map[string]SnapshotIdentity{}
				for i, n := range names {
					l.ExpectedSources[n] = SnapshotIdentity{Bytes: int64(len(parts[i])), SHA256: fmt.Sprintf("%x", sha256.Sum256(parts[i]))}
				}
				b := bytes.Clone(parts[1])
				b[55] ^= 1
				if err := src.WriteFile(names[1], b, 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				writeFakeRARFile(t, filepath.Join("members", fmt.Sprintf("%x", "book.epub")), "wrong")
			}
			x, err := ExtractTree(context.Background(), src, names, dst, books, l)
			if kind == "source" {
				if !errors.Is(err, ErrSourceChanged) || x.Whole || x.RARSetVersion != 0 {
					t.Fatal(x, err)
				}
			} else if err != nil || !x.Whole || x.Complete || x.RARSetVersion != 4 || x.Members[0].Disposition != "failed" {
				t.Fatal(x, err)
			}
		})
	}
}

func TestRAR4SetRefusesOldAndNestedLayouts(t *testing.T) {
	for _, kind := range []string{"old", "nested"} {
		t.Run(kind, func(t *testing.T) {
			src, dst, names, parts, _ := rar4SetFixture(t, 3)
			l := rar4SetLimits()
			if kind == "nested" {
				l.child = true
			} else {
				for i, name := range names {
					old := rarSnapshotName(true, i)
					if err := src.Rename(name, old); err != nil {
						t.Fatal(err)
					}
					names[i] = old
				}
			}
			x, err := ExtractSet(context.Background(), src, names, dst, books, l)
			if !errors.Is(err, ErrRAR4Coverage) || x.Whole || x.RARSetVersion != 0 {
				t.Fatal(x, err)
			}
			for i, n := range names {
				got, err := src.ReadFile(n)
				if err != nil || !bytes.Equal(got, parts[i]) {
					t.Fatal("unsupported source changed", n, err)
				}
			}
		})
	}
}

func TestRAR4SetDirectBudgetDoesNotEnableTraversal(t *testing.T) {
	for _, tree := range []bool{false, true} {
		t.Run(fmt.Sprint(tree), func(t *testing.T) {
			src, dst, names, _, whole := rar4SetFixture(t, 3)
			for i, n := range names {
				piece := whole[i*len(whole)/3 : (i+1)*len(whole)/3]
				b := rar4TestVolume(i, 3, false, rar4TestSplit("inside.zip", whole, piece, i > 0, i < 2, 0x30))
				if err := src.WriteFile(n, b, 0600); err != nil {
					t.Fatal(err)
				}
				replaceRARFixture(t, fmt.Sprintf("volume-%04d.listing", i), "Path = book.epub", "Path = inside.zip")
			}
			updateRARSetHeaders(t, src, names, "Rar")
			replaceRARFixture(t, "listing.txt", "Path = book.epub", "Path = inside.zip")
			writeFakeRARFile(t, filepath.Join("members", fmt.Sprintf("%x", "inside.zip")), string(whole))
			var x Extraction
			var err error
			if tree {
				x, err = ExtractTree(context.Background(), src, names, dst, func(string) bool { return true }, rar4SetLimits())
			} else {
				x, err = ExtractSet(context.Background(), src, names, dst, func(string) bool { return true }, rar4SetLimits())
			}
			if err != nil || x.RARSetVersion != 4 || len(x.Members) != 1 {
				t.Fatal(x, err)
			}
			if tree {
				if x.Whole || x.Complete || x.CoverageGap == nil || !reflect.DeepEqual(x.CoverageGap.Chain, []string{"inside.zip"}) {
					t.Fatal("unread child gained coverage", x)
				}
			} else if !x.Whole || !x.Complete || x.Members[0].Disposition != "nested_archive" || x.Members[0].Staged != "" {
				t.Fatal("budget enabled traversal", x)
			}
		})
	}
}

func TestRAR4SetCapabilityDoesNotChangeStandaloneFacts(t *testing.T) {
	payload := []byte("synthetic book")
	src, dst := sevenFixture(t, []sevenMember{{name: "book.epub", body: payload, packed: fmt.Sprint(len(payload))}})
	writeFakeRARFile(t, "kind.txt", "-tRar")
	b := rar4Archive(rar4Book("book.epub"))
	if err := src.WriteFile("bundle.rar", b, 0600); err != nil {
		t.Fatal(err)
	}
	x, err := ExtractTree(context.Background(), src, []string{"bundle.rar"}, dst, books, rar4SetLimits())
	if err != nil || !x.Whole || !x.Complete || x.RARVersion != 4 || x.RARContainers != 1 || x.RARSetVersion != 0 {
		t.Fatal("standalone facts conflated with set", x, err)
	}
}
