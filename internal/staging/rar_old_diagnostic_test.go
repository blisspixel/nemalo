package staging

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func oldProbeHeader(kind string, index int, old bool, missing string) []byte {
	flags := "Volume"
	if index == 0 {
		flags += " FirstVolume"
	}
	if !old {
		flags += " NewVolName"
	}
	return []byte(fmt.Sprintf("\n--\nType = %s\nPhysical Size = 14\nCharacteristics = %s\nMultivolume = +\nVolume Index = %d\nVolumes = 1\nERROR = Missing volume : %s\n\n----------\n", kind, flags, index, missing))
}

func oldRARFallbackFixture(t *testing.T, kind string, count int) (*os.Root, *os.Root, []string) {
	t.Helper()
	src, dst := sevenFixture(t, []sevenMember{{name: "book.epub", body: []byte("an epub body")}})
	t.Setenv("NEMALO_FAKE_7Z_TYPE", "-t"+kind)
	body := []byte("Rar!\x1a\x07\x00 volume")
	if kind == "Rar5" {
		body = []byte("Rar!\x1a\x07\x01\x00 volume")
	}
	names := make([]string, count)
	for i := range names {
		names[i] = rarSnapshotName(true, i)
		if err := src.WriteFile(names[i], body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeRARProbeFixtures(t, src, names, true, kind)
	for i := 1; i < count-1; i++ {
		fallback := ""
		if kind == "Rar" && i >= 3 {
			fallback = rarSnapshotName(true, i) + ".r00"
		}
		if kind == "Rar5" && i%100 == 0 {
			fallback = fmt.Sprintf("archive.%c100", 'r'+(i-1)/100)
		}
		if fallback == "" {
			continue
		}
		file := fmt.Sprintf("volume-%04d.header", i)
		data, err := os.ReadFile(filepath.Join(os.Getenv("NEMALO_FAKE_7Z"), file))
		if err != nil {
			t.Fatal(err)
		}
		writeFakeRARFile(t, file, strings.Replace(string(data), "Missing volume : "+rarSnapshotName(true, i+1), "Missing volume : "+fallback, 1))
	}
	writeFakeRARFile(t, "header.txt", fmt.Sprintf("Type = %s\nPhysical Size = %d\nTotal Physical Size = %d\nMultivolume = +\nVolume Index = 0\nVolumes = %d\n", kind, len(body), len(body)*count, count))
	return src, dst, names
}

func TestOldRARFallbackExtractsOnlyAfterCheckedAssembly(t *testing.T) {
	for _, kind := range []string{"Rar", "Rar5"} {
		for _, count := range []int{2, 3, 7, 103} {
			t.Run(fmt.Sprintf("%s/%d", kind, count), func(t *testing.T) {
				src, dst, names := oldRARFallbackFixture(t, kind, count)
				// The new identity evidence cannot enable physical coverage.
				limits := Defaults()
				limits.Coverage = true
				limits.RAR4Coverage = true
				limits.RAR4SetCoverage = true
				limits.RAR5SetCoverage = true
				if x, err := ExtractSet(context.Background(), src, names, dst, books, limits); err == nil || x.Whole || x.RARSetVersion != 0 || len(x.Members) != 0 {
					t.Fatal("old set gained coverage authority", x, err)
				}
				x, err := ExtractSet(context.Background(), src, names, dst, books, Defaults())
				if err != nil || !x.Complete || x.Whole || x.RARSetVersion != 0 || len(x.Members) != 1 || x.Members[0].Disposition != "extracted" {
					t.Fatal(x, err)
				}
				if got, err := dst.ReadFile(x.Members[0].Staged); err != nil || string(got) != "an epub body" {
					t.Fatal(string(got), err)
				}
				for _, name := range names {
					got, err := src.ReadFile(name)
					want := "Rar!\x1a\x07\x00 volume"
					if kind == "Rar5" {
						want = "Rar!\x1a\x07\x01\x00 volume"
					}
					if err != nil || string(got) != want {
						t.Fatal("source changed", name, err)
					}
				}
			})
		}
	}
}

func TestOldRARFallbackStillRequiresContinuityAndCleanAssembly(t *testing.T) {
	for _, tc := range []struct{ name, file, from, to string }{
		{"wrong fallback", "volume-0003.header", "archive.r02.r00", "archive.r02.r01"},
		{"wrong ordinal", "volume-0003.header", "Volume Index = 3", "Volume Index = 4"},
		{"unlinked boundary", "volume-0003.listing", "Split Before = +", "Split Before = -"},
		{"different member", "volume-0003.listing", "book.epub", "other.epub"},
		{"different size", "volume-0003.listing", "Size = 12", "Size = 13"},
		{"assembled error", "header.txt", "Volumes = 7", "Volumes = 7\nERROR = Missing volume : archive.r02.r00"},
		{"assembled diagnostic", "header.txt", "Volumes = 7", "Volumes = 7\nERRORS:\nHeaders Error"},
		{"assembled unresolved split", "listing.txt", "Block = 0", "Block = 0\nSplit After = +"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, dst, names := oldRARFallbackFixture(t, "Rar", 7)
			data, err := os.ReadFile(filepath.Join(os.Getenv("NEMALO_FAKE_7Z"), tc.file))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), tc.from) {
				t.Fatal("fixture mutation did not match")
			}
			writeFakeRARFile(t, tc.file, strings.Replace(string(data), tc.from, tc.to, 1))
			if x, err := ExtractSet(context.Background(), src, names, dst, books, Defaults()); err == nil || len(x.Members) != 0 {
				t.Fatal("inconclusive set extracted", x, err)
			}
			if entries, err := os.ReadDir(dst.Name()); err != nil || len(entries) != 0 {
				t.Fatal("bytes extracted before validation", entries, err)
			}
			for _, name := range names {
				if data, err := src.ReadFile(name); err != nil || string(data) != "Rar!\x1a\x07\x00 volume" {
					t.Fatal("source changed", name, err)
				}
			}
		})
	}
}

func TestOldRARIsolatedFallbackRequiresExactIdentity(t *testing.T) {
	cases := []struct {
		kind          string
		index         int
		missing, next string
	}{
		{"Rar", 3, "archive.r02.r00", "archive.r03"},
		{"Rar", 101, "archive.s00.r00", "archive.s01"},
		{"Rar", 899, "archive.z98.r00", "archive.z99"},
	}
	for i, letter := range "rstuvwxy" {
		cases = append(cases, struct {
			kind          string
			index         int
			missing, next string
		}{"Rar", 100 * (i + 1), fmt.Sprintf("archive.%c99.r00", letter), fmt.Sprintf("archive.%c00", letter+1)})
		cases = append(cases, struct {
			kind          string
			index         int
			missing, next string
		}{"Rar5", 100 * (i + 1), fmt.Sprintf("archive.%c100", letter), fmt.Sprintf("archive.%c00", letter+1)})
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/%d", tc.kind, tc.index), func(t *testing.T) {
			header := oldProbeHeader(tc.kind, tc.index, true, tc.missing)
			if !rarVolumeHeader(header, 2, tc.kind, 14, tc.index, true, tc.next, false) {
				t.Fatal("exact known old-name identity probe refused")
			}
			canonical := oldProbeHeader(tc.kind, tc.index, true, tc.next)
			if !rarVolumeHeader(canonical, 2, tc.kind, 14, tc.index, true, tc.next, false) {
				t.Fatal("canonical next-name evidence regressed")
			}
			bad := []struct {
				name  string
				out   []byte
				code  int
				kind  string
				size  int64
				index int
				old   bool
				next  string
			}{
				{"success exit with error", header, 0, tc.kind, 14, tc.index, true, tc.next},
				{"warning exit", header, 1, tc.kind, 14, tc.index, true, tc.next},
				{"wrong size", header, 2, tc.kind, 13, tc.index, true, tc.next},
				{"wrong ordinal", header, 2, tc.kind, 14, tc.index + 1, true, tc.next},
				{"wrong handler", header, 2, "7z", 14, tc.index, true, tc.next},
				{"modern layout", oldProbeHeader(tc.kind, tc.index, false, tc.missing), 2, tc.kind, 14, tc.index, false, rarSnapshotName(false, tc.index+1)},
				{"no next part", header, 2, tc.kind, 14, tc.index, true, ""},
				{"wrong next part", header, 2, tc.kind, 14, tc.index, true, tc.next + "x"},
				{"changed fallback", oldProbeHeader(tc.kind, tc.index, true, tc.missing+"x"), 2, tc.kind, 14, tc.index, true, tc.next},
				{"path prefix", oldProbeHeader(tc.kind, tc.index, true, "other/"+tc.missing), 2, tc.kind, 14, tc.index, true, tc.next},
			}
			for _, mutation := range []struct{ name, from, to string }{
				{"duplicate error", "\nERROR = ", "\nERROR = another problem\nERROR = "},
				{"duplicate ordinal", "\nVolume Index = ", "\nVolume Index = 0\nVolume Index = "},
				{"extra diagnostic", "\n----------", "\nERRORS:\nHeaders Error\n----------"},
				{"warning", "\n----------", "\nWarnings = 1\n----------"},
				{"wrong role", "Characteristics = Volume", "Characteristics = Volume FirstVolume"},
				{"comment", "\n----------", "\nComment = hidden\n----------"},
				{"another volume", "Volumes = 1", "Volumes = 2"},
				{"tail", "\n----------", "\nTail Size = 1\n----------"},
			} {
				if mutation.name == "wrong role" && tc.kind != "Rar" {
					continue // First/naming flags are RAR4-only reader properties.
				}
				bad = append(bad, struct {
					name  string
					out   []byte
					code  int
					kind  string
					size  int64
					index int
					old   bool
					next  string
				}{mutation.name, []byte(strings.Replace(string(header), mutation.from, mutation.to, 1)), 2, tc.kind, 14, tc.index, true, tc.next})
			}
			for _, b := range bad {
				t.Run(b.name, func(t *testing.T) {
					if rarVolumeHeader(b.out, b.code, b.kind, b.size, b.index, b.old, b.next, false) {
						t.Fatal("inconclusive evidence accepted")
					}
				})
			}
		})
	}
}

func TestOldRARIsolatedFallbackRefusesUnsupportedPositions(t *testing.T) {
	for _, kind := range []string{"Rar", "Rar5"} {
		for _, index := range []int{-1, 0, 1, 2, 99, 101, 899, 900, 901} {
			// RAR4 accepts interior positions from 3; its early and terminal cases are
			// tested here. RAR5 accepts only the eight nonterminal letter rollovers.
			if kind == "Rar" && index >= 3 && index < 900 {
				continue
			}
			missing := rarSnapshotName(true, index) + ".r00"
			if kind == "Rar5" {
				missing = fmt.Sprintf("archive.%c100", 'r'+max(0, index-1)/100)
			}
			if rarVolumeHeader(oldProbeHeader(kind, index, true, missing), 2, kind, 14, index, true, rarSnapshotName(true, index+1), false) {
				t.Fatalf("unsupported %s index %d accepted", kind, index)
			}
		}
	}
}
