package staging

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func oldRAR4Main(data []byte) []byte {
	b := bytes.Clone(data)
	copy(b[7:20], rar4Change(b[7:20], func(h []byte) {
		binary.LittleEndian.PutUint16(h[3:], binary.LittleEndian.Uint16(h[3:])&^0x10)
	}))
	return b
}

func oldRARSetLimits(version int) Limits {
	l := rar5SetLimits()
	if version == 4 {
		l = rar4SetLimits()
	}
	l.OldRARSetCoverage = true
	return l
}

func oldRARSetFixture(t *testing.T, version, count int) (*os.Root, *os.Root, []string, [][]byte, []byte) {
	t.Helper()
	whole := bytes.Repeat([]byte("generated old volume body across each adjacent boundary. "), 40)
	src, dst := sevenFixture(t, []sevenMember{{name: "book.epub", body: whole, packed: fmt.Sprint(len(whole)), extra: "Split Before = -\nSplit After = -\nVolume Index = 0\n"}})
	kind := "Rar5"
	if version == 4 {
		kind = "Rar"
	}
	writeFakeRARFile(t, "kind.txt", "-t"+kind)
	var names []string
	var parts [][]byte
	for i := range count {
		piece := whole[i*len(whole)/count : (i+1)*len(whole)/count]
		b := rar5TestVolume(i, count, rar5TestSplit("book.epub", whole, piece, i > 0, i+1 < count, 0))
		if version == 4 {
			b = oldRAR4Main(rar4TestVolume(i, count, i%2 == 1, rar4TestSplit("book.epub", whole, piece, i > 0, i+1 < count, 0x30)))
		}
		name := "book" + strings.TrimPrefix(rarSnapshotName(true, i), "archive")
		if err := src.WriteFile(name, b, 0600); err != nil {
			t.Fatal(err)
		}
		names, parts = append(names, name), append(parts, b)
		writeFakeRARFile(t, fmt.Sprintf("volume-%04d.listing", i), rarPartListing(whole, piece, i, count))
	}
	updateOldRARSetHeaders(t, src, names, kind)
	return src, dst, names, parts, whole
}

func updateOldRARSetHeaders(t *testing.T, src *os.Root, names []string, kind string) {
	t.Helper()
	updateRARSetHeaders(t, src, names, kind)
	for i := range names {
		prefix := fmt.Sprintf("volume-%04d", i)
		header, err := os.ReadFile(filepath.Join(os.Getenv("NEMALO_FAKE_7Z"), prefix+".header"))
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(string(header), " NewVolName", "")
		if i+1 < len(names) {
			next := rarSnapshotName(true, i+1)
			if kind == "Rar" && i >= 3 {
				next = rarSnapshotName(true, i) + ".r00"
			} else if kind == "Rar5" && i > 0 && i%100 == 0 {
				next = fmt.Sprintf("archive.%c100", 'r'+(i-1)/100)
			}
			text = strings.Replace(text, rarSnapshotName(false, i+1), next, 1)
		}
		writeFakeRARFile(t, prefix+".header", text)
		writeFakeRARFile(t, prefix+".name", rarSnapshotName(true, i))
	}
}

func TestOldRARSetCoverageIsSeparateFromModernAndStandaloneFacts(t *testing.T) {
	for _, version := range []int{4, 5} {
		for _, count := range []int{2, 3, 7, 103} {
			t.Run(fmt.Sprintf("%d/%d", version, count), func(t *testing.T) {
				src, dst, names, parts, whole := oldRARSetFixture(t, version, count)
				writeFakeRARFile(t, "require-utf8", "")
				writeFakeRARFile(t, "reject-ba", "")
				x, err := ExtractTree(context.Background(), src, names, dst, books, oldRARSetLimits(version))
				if err != nil || !x.Complete || !x.Whole || x.OldRARSetVersion != version || x.RARSetVersion != 0 || x.RARVersion != 0 || x.RARContainers != 0 || len(x.Members) != 1 {
					t.Fatal(x, err)
				}
				got, err := dst.ReadFile(x.Members[0].Staged)
				if err != nil || !bytes.Equal(got, whole) {
					t.Fatal("output identity changed", err)
				}
				for i, name := range names {
					got, err := src.ReadFile(name)
					if err != nil || !bytes.Equal(got, parts[i]) {
						t.Fatal("source changed", name, err)
					}
				}
				data, err := json.Marshal(x)
				if err != nil || bytes.Contains(data, []byte("OldRAR")) || bytes.Contains(data, []byte("old_rar")) {
					t.Fatal(string(data), err)
				}
			})
		}
	}
}

func TestOldRARSetRequiresEveryVersionAndLayoutCapability(t *testing.T) {
	for _, version := range []int{4, 5} {
		for mask := 0; mask < 32; mask++ {
			t.Run(fmt.Sprintf("%d/%05b", version, mask), func(t *testing.T) {
				src, dst, names, parts, _ := oldRARSetFixture(t, version, 3)
				l := Defaults()
				l.Coverage, l.RAR4Coverage, l.RAR4SetCoverage = mask&1 != 0, mask&2 != 0, mask&4 != 0
				l.RAR5SetCoverage, l.OldRARSetCoverage = mask&8 != 0, mask&16 != 0
				want := l.Coverage && l.OldRARSetCoverage && ((version == 4 && l.RAR4Coverage && l.RAR4SetCoverage) || (version == 5 && l.RAR5SetCoverage))
				x, err := ExtractSet(context.Background(), src, names, dst, books, l)
				if want {
					if err != nil || !x.Complete || !x.Whole || x.OldRARSetVersion != version {
						t.Fatal(x, err)
					}
				} else if x.Whole || x.OldRARSetVersion != 0 || (!l.Coverage && (err != nil || !x.Complete)) {
					t.Fatal("old coverage appeared without every capability", x, err)
				}
				if x.RARSetVersion != 0 || x.RARVersion != 0 || x.RARContainers != 0 {
					t.Fatal("old evidence supplied another proof", x)
				}
				for i, name := range names {
					got, err := src.ReadFile(name)
					if err != nil || !bytes.Equal(got, parts[i]) {
						t.Fatal("source changed", name, err)
					}
				}
			})
		}
	}
}

func TestOldRARCapabilityNeitherReinterpretsModernSetsNorEnablesChildren(t *testing.T) {
	for _, version := range []int{4, 5} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			fixture := rar5SetFixture
			if version == 4 {
				fixture = rar4SetFixture
			}
			src, dst, names, _, _ := fixture(t, 3)
			x, err := ExtractTree(context.Background(), src, names, dst, books, oldRARSetLimits(version))
			if err != nil || !x.Whole || !x.Complete || x.RARSetVersion != version || x.OldRARSetVersion != 0 {
				t.Fatal(x, err)
			}
			src, dst, names, _, _ = oldRARSetFixture(t, version, 3)
			l := oldRARSetLimits(version)
			l.child = true
			x, err = ExtractSet(context.Background(), src, names, dst, books, l)
			if err == nil || x.Whole || x.OldRARSetVersion != 0 || x.RARSetVersion != 0 || len(x.Members) != 0 {
				t.Fatal("nested set gained root evidence", x, err)
			}
		})
	}
}

func TestOldRARCoverageRequiresOneExactOrderedSourceFamily(t *testing.T) {
	for _, version := range []int{4, 5} {
		for _, mutation := range []string{"missing", "duplicate", "other family", "other parent", "late first", "mixed naming", "beyond z99"} {
			t.Run(fmt.Sprintf("%d/%s", version, mutation), func(t *testing.T) {
				src, dst, names, parts, _ := oldRARSetFixture(t, version, 3)
				original := append([]string(nil), names...)
				switch mutation {
				case "missing":
					names = []string{names[0], names[2]}
				case "duplicate":
					names[2] = names[1]
				case "other family":
					names[2] = "other.r01"
				case "other parent":
					names[2] = filepath.Join("other", names[2])
				case "late first":
					names = names[1:]
				case "mixed naming":
					names[2] = "book.part3.rar"
				case "beyond z99":
					names[2] = "book.{00"
				}
				x, err := ExtractSet(context.Background(), src, names, dst, books, oldRARSetLimits(version))
				if err == nil || x.Whole || x.OldRARSetVersion != 0 || x.RARSetVersion != 0 || len(x.Members) != 0 {
					t.Fatal("invalid source family accepted", x, err)
				}
				for i, name := range original {
					got, err := src.ReadFile(name)
					if err != nil || !bytes.Equal(got, parts[i]) {
						t.Fatal("source changed", name, err)
					}
				}
			})
		}
	}
}

func TestOldRAR4PhysicalModeKeepsStrictModernAndStandaloneBoundaries(t *testing.T) {
	for _, index := range []int{0, 1, 899, 900} {
		count := 901
		b := oldRAR4Main(rar4TestVolume(index, count, true, rar4TestSplit("book.epub", []byte("abc"), []byte("a"), index > 0, index+1 < count, 0x30)))
		v, err := rar4VolumeAccounted(context.Background(), bytes.NewReader(b), int64(len(b)), true)
		if err != nil || v.Ordinal != index || v.First != (index == 0) || v.More != (index+1 < count) || len(v.Files) != 1 {
			t.Fatal(index, v, err)
		}
		f := v.Files[0]
		protected := protectedRARData{bytes.NewReader(b), f.DataOffset, f.DataOffset + f.Packed}
		if got, err := rar4VolumeAccounted(context.Background(), protected, int64(len(b)), true); err != nil || !reflect.DeepEqual(got, v) {
			t.Fatal("old framing read payload", got, err)
		}
		if got, err := RAR4VolumeAccounted(context.Background(), bytes.NewReader(b), int64(len(b))); !errors.Is(err, ErrRAR4Coverage) || !reflect.DeepEqual(got, RAR4Volume{}) {
			t.Fatal("modern entry point accepted old layout", got, err)
		}
		if got, err := RAR4Accounted(context.Background(), bytes.NewReader(b), int64(len(b))); !errors.Is(err, ErrRAR4Coverage) || got != nil {
			t.Fatal("standalone entry point accepted old volume", got, err)
		}
	}
	for _, index := range []int{901, 999} {
		b := oldRAR4Main(rar4TestVolume(index, index+1, false, rar4TestSplit("book.epub", []byte("abc"), []byte("a"), true, false, 0x30)))
		if got, err := rar4VolumeAccounted(context.Background(), bytes.NewReader(b), int64(len(b)), true); !errors.Is(err, ErrRAR4Coverage) || !reflect.DeepEqual(got, RAR4Volume{}) {
			t.Fatal("old ordinal escaped naming range", got, err)
		}
	}
	modern := rar4TestVolume(0, 2, false, rar4TestSplit("book.epub", []byte("abc"), []byte("a"), false, true, 0x30))
	if _, err := rar4VolumeAccounted(context.Background(), bytes.NewReader(modern), int64(len(modern)), true); !errors.Is(err, ErrRAR4Coverage) {
		t.Fatal("old mode accepted modern naming", err)
	}
	old := oldRAR4Main(modern)
	for stop := range len(old) {
		if got, err := rar4VolumeAccounted(context.Background(), bytes.NewReader(old[:stop]), int64(stop), true); err == nil || !reflect.DeepEqual(got, RAR4Volume{}) {
			t.Fatal("truncation returned facts", stop, got, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := rar4VolumeAccounted(ctx, bytes.NewReader(old), int64(len(old)), true); !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, RAR4Volume{}) {
		t.Fatal(got, err)
	}
}

func TestOldRARSetSourceBindingAndFailedOutputRemainSeparate(t *testing.T) {
	for _, version := range []int{4, 5} {
		for _, sourceChanged := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/source=%t", version, sourceChanged), func(t *testing.T) {
				src, dst, names, parts, _ := oldRARSetFixture(t, version, 3)
				l := oldRARSetLimits(version)
				if sourceChanged {
					l.ExpectedSources = map[string]SnapshotIdentity{}
					for i, name := range names {
						l.ExpectedSources[name] = SnapshotIdentity{Bytes: int64(len(parts[i])), SHA256: fmt.Sprintf("%x", sha256.Sum256(parts[i]))}
					}
					b := bytes.Clone(parts[1])
					b[len(b)/2] ^= 1
					if err := src.WriteFile(names[1], b, 0600); err != nil {
						t.Fatal(err)
					}
				} else {
					writeFakeRARFile(t, filepath.Join("members", fmt.Sprintf("%x", "book.epub")), "wrong")
				}
				x, err := ExtractTree(context.Background(), src, names, dst, books, l)
				if sourceChanged {
					if !errors.Is(err, ErrSourceChanged) || x.Whole || x.OldRARSetVersion != 0 || len(x.Members) != 0 {
						t.Fatal(x, err)
					}
				} else if err != nil || !x.Whole || x.Complete || x.OldRARSetVersion != version || x.RARSetVersion != 0 || x.Members[0].Disposition != "failed" {
					t.Fatal(x, err)
				}
			})
		}
	}
}

func TestOldRARSetSharesExistingBudgetsAndActiveCancellation(t *testing.T) {
	for _, version := range []int{4, 5} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			fixture := func(t *testing.T, count int) (*os.Root, *os.Root, []string, [][]byte, []byte) {
				return oldRARSetFixture(t, version, count)
			}
			bounds := func() Limits { return oldRARSetLimits(version) }
			testRARSetBudgets(t, fixture, bounds)
			t.Run("active cancellation", func(t *testing.T) { testRARSetCancellation(t, fixture, bounds) })
		})
	}
}

func FuzzOldRAR4VolumeAccounted(f *testing.F) {
	for _, index := range []int{0, 1, 899, 900} {
		f.Add(oldRAR4Main(rar4TestVolume(index, 901, true, rar4TestSplit("book.epub", []byte("abc"), []byte("a"), index > 0, index < 900, 0x30))))
	}
	f.Add(rar4TestVolume(0, 2, false, rar4Book("book.epub")))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 64<<10 {
			t.Skip()
		}
		v, err := rar4VolumeAccounted(context.Background(), bytes.NewReader(b), int64(len(b)), true)
		if err != nil {
			if !reflect.DeepEqual(v, RAR4Volume{}) {
				t.Fatal("partial facts on error", v)
			}
			return
		}
		if v.Ordinal < 0 || v.Ordinal > 900 || v.First != (v.Ordinal == 0) {
			t.Fatal(v)
		}
		at := int64(20)
		for _, p := range v.Files {
			if p.HeaderOffset != at || p.DataOffset < at+33 || p.Packed < 0 || p.Packed > int64(len(b))-p.DataOffset {
				t.Fatal("invalid physical span", p)
			}
			at = p.DataOffset + p.Packed
		}
		if at != int64(len(b)-9) && at != int64(len(b)-16) {
			t.Fatal("unpartitioned bytes", at, len(b))
		}
	})
}

func TestOldRARSetRefusesUnprovenRecordsAndTablesBeforeExtraction(t *testing.T) {
	for _, version := range []int{4, 5} {
		for _, mutation := range []string{"service", "tail", "ordinal", "split metadata", "isolated CRC", "isolated size", "assembled packed", "assembled duplicate", "assembled count", "reordered names"} {
			t.Run(fmt.Sprintf("%d/%s", version, mutation), func(t *testing.T) {
				src, dst, names, parts, whole := oldRARSetFixture(t, version, 3)
				kind := "Rar5"
				if version == 4 {
					kind = "Rar"
				}
				switch mutation {
				case "service", "tail", "ordinal", "split metadata":
					b := bytes.Clone(parts[1])
					piece := whole[len(whole)/3 : 2*len(whole)/3]
					if mutation == "tail" {
						b = append(b, []byte("unaccounted")...)
					} else if version == 4 {
						end := len(b) - 16 // The second fixture volume has seven reserved end bytes.
						switch mutation {
						case "service":
							b = append(append(bytes.Clone(b[:end]), rar4Block(0x7a, 0x8000, []byte{4, 0, 0, 0}, []byte("keep"))...), b[end:]...)
						case "ordinal":
							copy(b[end:], rar4Block(0x7b, 13, []byte{2, 0, 0, 0, 0, 0, 0, 0, 0}, nil))
						case "split metadata":
							b = oldRAR4Main(rar4TestVolume(1, 3, true, rar4TestSplit("book.epub", whole, piece, true, true, 0x33)))
						}
					} else {
						end := len(b) - len(rar5Record(5, 0, rar5Numbers(1), nil))
						switch mutation {
						case "service":
							service := rar5Record(3, 2, rar5FileBody("service", []byte("keep"), 4, 0100644, 0, 1), []byte("keep"))
							b = append(append(bytes.Clone(b[:end]), service...), b[end:]...)
						case "ordinal":
							b = append([]byte("Rar!\x1a\x07\x01\x00"), rar5Record(1, 0, rar5Numbers(3, 2), nil)...)
							b = append(b, rar5TestSplit("book.epub", whole, piece, true, true, 0)...)
							b = append(b, rar5Record(5, 0, rar5Numbers(1), nil)...)
						case "split metadata":
							b = rar5TestVolume(1, 3, rar5TestSplit("book.epub", whole, piece, true, true, 128))
						}
					}
					parts[1] = b
					if err := src.WriteFile(names[1], b, 0600); err != nil {
						t.Fatal(err)
					}
					updateOldRARSetHeaders(t, src, names, kind)
				case "isolated CRC":
					replaceRARFixture(t, "volume-0001.listing", "CRC = ", "CRC = 1")
				case "isolated size":
					replaceRARFixture(t, "volume-0001.listing", fmt.Sprintf("Size = %d\n", len(whole)), fmt.Sprintf("Size = %d\n", len(whole)+1))
				case "assembled packed":
					replaceRARFixture(t, "listing.txt", fmt.Sprintf("Packed Size = %d\n", len(whole)), "Packed Size = 1\n")
				case "assembled duplicate":
					b, err := os.ReadFile(filepath.Join(os.Getenv("NEMALO_FAKE_7Z"), "listing.txt"))
					if err != nil {
						t.Fatal(err)
					}
					writeFakeRARFile(t, "listing.txt", string(b)+string(b))
				case "assembled count":
					replaceRARFixture(t, "header.txt", "Volumes = 3", "Volumes = 4")
				case "reordered names":
					names[1], names[2] = names[2], names[1]
					parts[1], parts[2] = parts[2], parts[1]
				}
				writeFakeRARFile(t, "trace-enabled", "")
				x, err := ExtractTree(context.Background(), src, names, dst, books, oldRARSetLimits(version))
				if err == nil || x.Whole || x.OldRARSetVersion != 0 || x.RARSetVersion != 0 || len(x.Members) != 0 {
					t.Fatal("unproven old set produced evidence", x, err)
				}
				trace, readErr := os.ReadFile(filepath.Join(os.Getenv("NEMALO_FAKE_7Z"), "trace"))
				if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
					t.Fatal(readErr)
				}
				if strings.Contains("\n"+string(trace), "\ne ") {
					t.Fatal("extraction preceded complete evidence", string(trace))
				}
				for i, name := range names {
					got, err := src.ReadFile(name)
					if err != nil || !bytes.Equal(got, parts[i]) {
						t.Fatal("unproven source changed", name, err)
					}
				}
			})
		}
	}
}
