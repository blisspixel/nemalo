package staging

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFakeRARFile(t *testing.T, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(os.Getenv("NEMALO_FAKE_7Z"), name), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeRARProbeFixtures(t *testing.T, src *os.Root, names []string, old bool, kind string) {
	t.Helper()
	for i, name := range names {
		info, err := src.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		prefix := fmt.Sprintf("volume-%04d", i)
		flags := "Volume"
		if !old {
			flags += " NewVolName"
		}
		if i == 0 {
			flags += " FirstVolume"
		}
		header := fmt.Sprintf("Path = x\nType = %s\nPhysical Size = %d\nCharacteristics = %s\nMultivolume = +\nVolume Index = %d\nVolumes = 1\n", kind, info.Size(), flags, i)
		code := "0"
		if i+1 < len(names) {
			header += "ERROR = Missing volume : " + rarSnapshotName(old, i+1) + "\n"
			code = "2"
		}
		if kind == "Rar5" && i > 0 {
			header += "ERRORS:\nHeaders Error\n"
			code = "2"
		}
		writeFakeRARFile(t, prefix+".header", header)
		writeFakeRARFile(t, prefix+".name", rarSnapshotName(old, i))
		writeFakeRARFile(t, prefix+".code", code)
		before, after := "-", "-"
		if i > 0 {
			before = "+"
		}
		if i+1 < len(names) {
			after = "+"
		}
		writeFakeRARFile(t, prefix+".listing", fmt.Sprintf("Path = book.epub\nSize = 12\nSplit Before = %s\nSplit After = %s\n\n", before, after))
	}
}

func oldRARFixture(t *testing.T) (*os.Root, *os.Root) {
	t.Helper()
	src, dst := sevenFixture(t, []sevenMember{{name: "book.epub", body: []byte("an epub body")}})
	t.Setenv("NEMALO_FAKE_7Z_TYPE", "-tRar")
	for _, name := range []string{"Book.RAR", "book.R00"} {
		if err := src.WriteFile(name, []byte("Rar!\x1a\x07\x00 volume"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeRARProbeFixtures(t, src, []string{"Book.RAR", "book.R00"}, true, "Rar")
	writeFakeRARFile(t, "header.txt", "Type = Rar\nPhysical Size = 14\nTotal Physical Size = 28\nMultivolume = +\nVolume Index = 0\nVolumes = 2\n")
	return src, dst
}

func TestOldRARSetUsesIsolatedSnapshotsThenExtracts(t *testing.T) {
	src, dst := oldRARFixture(t)
	x, err := ExtractSet(context.Background(), src, []string{"Book.RAR", "book.R00"}, dst, books, Defaults())
	if err != nil || len(x.Members) != 1 || x.Members[0].Disposition != "extracted" || !x.Complete || x.Whole {
		t.Fatal(x, err)
	}
	got, err := dst.ReadFile(x.Members[0].Staged)
	if err != nil || string(got) != "an epub body" {
		t.Fatal(string(got), err)
	}
	for _, name := range []string{"Book.RAR", "book.R00"} {
		if got, err := src.ReadFile(name); err != nil || string(got) != "Rar!\x1a\x07\x00 volume" {
			t.Fatal("source changed", string(got), err)
		}
	}
}

func TestRARVolumeEvidenceRefusesBeforeExtraction(t *testing.T) {
	for _, tc := range []struct{ name, file, from, to string }{
		{"unrelated standalone", "volume-0001.header", "Multivolume = +", "Multivolume = -"},
		{"wrong ordinal", "volume-0001.header", "Volume Index = 1", "Volume Index = 0"},
		{"no ordinal", "volume-0001.header", "Volume Index = 1\n", ""},
		{"wrong role", "volume-0001.header", "Characteristics = Volume", "Characteristics = Volume FirstVolume"},
		{"wrong naming", "volume-0000.header", "Characteristics = Volume FirstVolume", "Characteristics = Volume FirstVolume NewVolName"},
		{"wrong size", "volume-0001.header", "Physical Size = 14", "Physical Size = 13"},
		{"another visible volume", "volume-0001.header", "Volumes = 1", "Volumes = 2"},
		{"wrong handler", "volume-0001.header", "Type = Rar", "Type = Rar5"},
		{"duplicate ordinal", "volume-0001.header", "Volume Index = 1", "Volume Index = 1\nVolume Index = 1"},
		{"comment forgery", "volume-0001.header", "Volume Index = 1", "Comment = text\nVolume Index = 1"},
		{"unaccounted tail", "volume-0001.header", "Volume Index = 1", "Tail Size = 1\nVolume Index = 1"},
		{"unaccounted offset", "volume-0001.header", "Volume Index = 1", "Offset = 1\nVolume Index = 1"},
		{"warning", "volume-0001.header", "Volume Index = 1", "WARNINGS:\nVolume Index = 1"},
		{"missing other name", "volume-0000.header", "Missing volume : archive.r00", "Missing volume : another.r00"},
		{"missing final", "volume-0001.header", "Volumes = 1", "Volumes = 1\nERROR = Missing volume : archive.r01"},
		{"unexpected probe exit", "volume-0000.code", "2", "1"},
		{"hidden probe error", "volume-0001.header", "Volumes = 1", "Volumes = 1\nERROR = Headers Error"},
		{"wrong total", "header.txt", "Total Physical Size = 28", "Total Physical Size = 29"},
		{"wrong count", "header.txt", "Volumes = 2", "Volumes = 3"},
		{"full set error", "list.code", "", "2"},
		{"unresolved member", "listing.txt", "Block = 0", "Block = 0\nSplit After = +"},
		{"unrelated same ordinal", "volume-0001.listing", "Path = book.epub", "Path = another.epub"},
		{"different declared size", "volume-0001.listing", "Size = 12", "Size = 13"},
		{"unlinked boundary", "volume-0000.listing", "Split After = +", "Split After = -"},
		{"missing continuation", "volume-0001.listing", "Split Before = +", "Split Before = -"},
		{"unknown boundary", "volume-0001.listing", "Split After = -\n", ""},
		{"malformed boundary", "volume-0001.listing", "Split After = -", "Split After = maybe"},
		{"unexpected leading split", "volume-0000.listing", "Split Before = -", "Split Before = +"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, dst := oldRARFixture(t)
			b, _ := os.ReadFile(filepath.Join(os.Getenv("NEMALO_FAKE_7Z"), tc.file))
			writeFakeRARFile(t, tc.file, strings.Replace(string(b), tc.from, tc.to, 1))
			x, err := ExtractSet(context.Background(), src, []string{"Book.RAR", "book.R00"}, dst, books, Defaults())
			if err == nil || len(x.Members) != 0 {
				t.Fatal("inconclusive set accepted", x, err)
			}
			entries, readErr := os.ReadDir(dst.Name())
			if readErr != nil || len(entries) != 0 {
				t.Fatal("bytes extracted before set validation", entries, readErr)
			}
		})
	}
}

func TestRAR5IsolatedSplitErrorRequiresCleanAssembly(t *testing.T) {
	src, dst := sevenFixture(t, []sevenMember{{name: "book.epub", body: []byte("an epub body")}})
	names := []string{"book.part1.rar", "book.part02.rar"}
	for _, name := range names {
		if err := src.WriteFile(name, []byte("Rar!\x1a\x07\x01\x00 volume"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeRARProbeFixtures(t, src, names, false, "Rar5")
	header := "Type = Rar5\nPhysical Size = 15\nTotal Physical Size = 30\nMultivolume = +\nVolume Index = 0\nVolumes = 2\n"
	for _, bad := range []string{header + "ERRORS:\nHeaders Error\n", header + "ERROR = Headers Error\n", header + "ERROR = \n"} {
		writeFakeRARFile(t, "header.txt", bad)
		if x, err := ExtractSet(context.Background(), src, names, dst, books, Defaults()); err == nil || len(x.Members) != 0 {
			t.Fatal("probe exception masked an assembly error", x, err)
		}
	}
	writeFakeRARFile(t, "header.txt", header)
	if x, err := ExtractSet(context.Background(), src, names, dst, books, Defaults()); err != nil || !x.Complete || x.Members[0].Disposition != "extracted" {
		t.Fatal(x, err)
	}
}

func TestOldRARNamesAndSetBoundaries(t *testing.T) {
	for name, want := range map[string]int{"x.r00": 2, "x.R01": 3, "x.r99": 101, "x.s00": 102, "x.z99": 901} {
		set, n, ok := OldRARVolumeOf(filepath.Join("dir", name))
		if !ok || set != filepath.Join("dir", "x.rar") || n != want {
			t.Fatal(name, set, n, ok)
		}
	}
	for _, name := range []string{"x.rar", "x.r0", "x.r000", "x.q99", "x.{00", "x.r00.extra"} {
		if _, _, ok := OldRARVolumeOf(name); ok {
			t.Fatal("unexpected continuation", name)
		}
	}
	for _, names := range [][]string{{"Book.RAR", "book.R01"}, {"Book.RAR", "other.r00"}, {"Book.RAR", "book.part2.rar"}, {"Book.RAR"}} {
		src, dst := oldRARFixture(t)
		if _, err := ExtractSet(context.Background(), src, names, dst, books, Defaults()); err == nil {
			t.Fatal("invalid set accepted", names)
		}
	}
}

func TestRARSetBudgetsAndCancellation(t *testing.T) {
	t.Run("shared output", func(t *testing.T) {
		src, dst := oldRARFixture(t)
		for i := 0; i < 2; i++ {
			name := fmt.Sprintf("volume-%04d.header", i)
			b, err := os.ReadFile(filepath.Join(os.Getenv("NEMALO_FAKE_7Z"), name))
			if err != nil {
				t.Fatal(err)
			}
			writeFakeRARFile(t, name, string(b)+"Padding = "+strings.Repeat("x", maxListing/2)+"\n")
		}
		x, err := ExtractSet(context.Background(), src, []string{"Book.RAR", "book.R00"}, dst, books, Defaults())
		if err == nil || len(x.Members) != 0 {
			t.Fatal("probe outputs did not share a budget", x, err)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		src, dst := oldRARFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := ExtractSet(ctx, src, []string{"Book.RAR", "book.R00"}, dst, books, Defaults()); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
	t.Run("too many", func(t *testing.T) {
		src, dst := oldRARFixture(t)
		if _, err := ExtractSet(context.Background(), src, make([]string, 1001), dst, books, Defaults()); err == nil {
			t.Fatal("accepted 1,001 volumes")
		}
	})
	t.Run("too many bytes", func(t *testing.T) {
		src, dst := oldRARFixture(t)
		f, err := src.OpenFile("Book.RAR", os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		err = f.Truncate(maxArchiveSnapshot)
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			t.Fatal(err, closeErr)
		}
		if _, err := ExtractSet(context.Background(), src, []string{"Book.RAR", "book.R00"}, dst, books, Defaults()); err == nil || !strings.Contains(err.Error(), "4 GiB") {
			t.Fatal(err)
		}
	})
	t.Run("different signatures", func(t *testing.T) {
		src, dst := oldRARFixture(t)
		if err := src.WriteFile("book.R00", []byte("Rar!\x1a\x07\x01\x00 volume"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ExtractSet(context.Background(), src, []string{"Book.RAR", "book.R00"}, dst, books, Defaults()); err == nil || !strings.Contains(err.Error(), "format") {
			t.Fatal(err)
		}
	})
}

func TestRARSetContinuityAcrossThreeVolumes(t *testing.T) {
	for _, kind := range []string{"Rar", "Rar5"} {
		t.Run(kind, func(t *testing.T) {
			src, dst := sevenFixture(t, []sevenMember{{name: "book.epub", body: []byte("an epub body")}})
			t.Setenv("NEMALO_FAKE_7Z_TYPE", "-t"+kind)
			names := []string{"book.part1.rar", "book.part02.rar", "book.part003.rar"}
			body := []byte("Rar!\x1a\x07\x00 volume")
			if kind == "Rar5" {
				body = []byte("Rar!\x1a\x07\x01\x00 volume")
			}
			for _, name := range names {
				if err := src.WriteFile(name, body, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			writeRARProbeFixtures(t, src, names, false, kind)
			writeFakeRARFile(t, "header.txt", fmt.Sprintf("Type = %s\nPhysical Size = %d\nTotal Physical Size = %d\nMultivolume = +\nVolume Index = 0\nVolumes = 3\n", kind, len(body), 3*len(body)))
			if x, err := ExtractSet(context.Background(), src, names, dst, books, Defaults()); err != nil || !x.Complete || len(x.Members) != 1 || x.Members[0].Disposition != "extracted" {
				t.Fatal(x, err)
			}
		})
	}
}
