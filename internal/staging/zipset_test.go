package staging

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/assessment"
)

func zipSetCopies(t *testing.T, copies map[string][]byte) {
	t.Helper()
	var names []string
	for name, body := range copies {
		writeFakeRARFile(t, "copy-"+name, string(body))
		names = append(names, name)
	}
	writeFakeRARFile(t, "copies.list", strings.Join(names, "\n"))
}

func streamZIPFixture(t *testing.T) (*os.Root, *os.Root, []string, []byte) {
	t.Helper()
	src, dst := sevenFixture(t, nil)
	t.Setenv("NEMALO_FAKE_7Z_TYPE", "-tzip")
	data := archive(t, []member{{name: "book.epub", body: []byte("a synthetic book"), method: zip.Store}, {name: "notes.txt", body: []byte("retained"), method: zip.Store}})
	names := []string{"stream.ZIP.001", "stream.zip.0002"}
	for i, body := range [][]byte{data[:3], data[3:]} {
		if err := src.WriteFile(names[i], body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeFakeRARFile(t, "header.txt", fmt.Sprintf("Type = zip\nPhysical Size = %d\n", len(data)))
	zipSetCopies(t, map[string][]byte{"archive.zip": data})
	return src, dst, names, data
}

func segmentZIPFixture(t *testing.T, count int) (*os.Root, *os.Root, []string, map[string][]byte) {
	t.Helper()
	src, dst := sevenFixture(t, []sevenMember{{name: "book.epub", body: []byte("a synthetic book"), packed: "16", extra: "Volume Index = 0\nOffset = 4\n"}, {name: "notes.txt", body: []byte("retained"), packed: "8", extra: "Volume Index = 0\nOffset = 60\n"}})
	t.Setenv("NEMALO_FAKE_7Z_TYPE", "-tzip")
	names := make([]string, count)
	copies := map[string][]byte{}
	for i := range names {
		name, private := fmt.Sprintf("parts.z%02d", i+1), fmt.Sprintf("archive.z%02d", i+1)
		if i+1 == count {
			name, private = "parts.ZIP", "archive.zip"
		}
		names[i] = name
		body := make([]byte, 128)
		if i == 0 {
			copy(body, "PK\x07\x08")
		} else {
			copy(body, fmt.Sprintf("segment-%d", i))
		}
		if err := src.WriteFile(name, body, 0o600); err != nil {
			t.Fatal(err)
		}
		copies[private] = body
		writeFakeRARFile(t, "header-"+private, fmt.Sprintf("Type = zip\nPhysical Size = 128\nTotal Physical Size = %d\nEmbedded Stub Size = 4\nMultivolume = +\nVolume Index = %d\nVolumes = %d\n", count*128, i, count))
	}
	zipSetCopies(t, copies)
	return src, dst, names, copies
}

func TestSplitZIPLayoutsExtractThroughTheirReaders(t *testing.T) {
	t.Run("byte chunks", func(t *testing.T) {
		src, dst, names, data := streamZIPFixture(t)
		x, err := ExtractSet(context.Background(), src, names, dst, books, Defaults())
		if err != nil || !x.Complete || x.Whole || len(x.Members) != 2 || x.Members[0].Disposition != "extracted" || x.Members[1].Disposition != "unsupported_format" {
			t.Fatal(x, err)
		}
		got, err := dst.ReadFile(x.Members[0].Staged)
		if err != nil || string(got) != "a synthetic book" {
			t.Fatal(string(got), err)
		}
		a, _ := src.ReadFile(names[0])
		b, _ := src.ReadFile(names[1])
		if !bytes.Equal(append(a, b...), data) {
			t.Fatal("source chunks changed")
		}
	})
	for _, count := range []int{2, 3} {
		t.Run(fmt.Sprintf("%d disk segments", count), func(t *testing.T) {
			src, dst, names, copies := segmentZIPFixture(t, count)
			x, err := ExtractSet(context.Background(), src, names, dst, books, Defaults())
			if err != nil || !x.Complete || x.Whole || len(x.Members) != 2 || x.Members[0].Disposition != "extracted" || x.Members[1].Disposition != "unsupported_format" {
				t.Fatal(x, err)
			}
			for i, name := range names {
				private := fmt.Sprintf("archive.z%02d", i+1)
				if i+1 == count {
					private = "archive.zip"
				}
				got, err := src.ReadFile(name)
				if err != nil || !bytes.Equal(got, copies[private]) {
					t.Fatal("source segment changed", name, err)
				}
			}
		})
	}
}

func TestSplitZIPRefusesInconclusiveEvidenceBeforeExtraction(t *testing.T) {
	for _, tc := range []struct{ name, file, from, to string }{
		{"wrong type", "header-archive.zip", "Type = zip", "Type = 7z"},
		{"missing part", "header-archive.zip", "Total Physical Size = 256", "Total Physical Size = 128"},
		{"unrelated first", "header-archive.z01", "Multivolume = +", "Multivolume = -"},
		{"wrong size", "header-archive.zip", "Physical Size = 128\n", "Physical Size = 127\n"},
		{"wrong ordinal", "header-archive.zip", "Volume Index = 1", "Volume Index = 0"},
		{"wrong count", "header-archive.zip", "Volumes = 2", "Volumes = 3"},
		{"missing ordinal", "header-archive.zip", "Volume Index = 1\n", ""},
		{"duplicate key", "header-archive.zip", "Volumes = 2", "Volumes = 2\nVolumes = 2"},
		{"archive comment", "header-archive.zip", "Type = zip", "Type = zip\nComment = Volumes = 2"},
		{"leading offset", "header-archive.zip", "Type = zip", "Type = zip\nOffset = 4"},
		{"wrong stub", "header-archive.zip", "Embedded Stub Size = 4", "Embedded Stub Size = 128"},
		{"unaccounted tail", "header-archive.zip", "Type = zip", "Type = zip\nTail Size = 1"},
		{"warning", "header-archive.zip", "Type = zip", "Type = zip\nWARNINGS:\ntrailing data"},
		{"reader error", "header-archive.zip", "Type = zip", "Type = zip\nERROR = Missing volume : archive.z01"},
		{"nonzero exit", "list.code", "", "2"},
		{"member disk", "listing.txt", "Volume Index = 0", "Volume Index = 2"},
		{"unknown disk", "listing.txt", "Volume Index = 0\n", ""},
		{"unstated disk", "listing.txt", "Volume Index = 0\n", "Volume Index = \n"},
		{"unstated offset", "listing.txt", "Offset = 4\n", "Offset = \n"},
		{"unknown uncompressed size", "listing.txt", "Size = 16\n", ""},
		{"negative offset", "listing.txt", "Offset = 4", "Offset = -1"},
		{"offset beyond disk", "listing.txt", "Offset = 4", "Offset = 200"},
		{"packed extent beyond set", "listing.txt", "Packed Size = 16", "Packed Size = 255"},
		{"different member table", "listing-archive.zip", "", "Path = another.epub\nSize = 16\nPacked Size = 16\nVolume Index = 0\nOffset = 4\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, dst, names, _ := segmentZIPFixture(t, 2)
			b, _ := os.ReadFile(filepath.Join(os.Getenv("NEMALO_FAKE_7Z"), tc.file))
			if tc.from != "" && !strings.Contains(string(b), tc.from) {
				t.Fatal("fixture mutation absent", tc.file, tc.from)
			}
			writeFakeRARFile(t, tc.file, strings.Replace(string(b), tc.from, tc.to, 1))
			x, err := ExtractSet(context.Background(), src, names, dst, books, Defaults())
			if err == nil || len(x.Members) != 0 {
				t.Fatal("inconclusive set accepted", x, err)
			}
			files, err := os.ReadDir(dst.Name())
			if err != nil || len(files) != 0 {
				t.Fatal("bytes extracted before readiness", files, err)
			}
		})
	}
}

func TestByteSplitZIPRequiresExactlyOneOrdinaryContainer(t *testing.T) {
	for _, tc := range []struct{ name, suffix string }{
		{"multi interpretation", "Multivolume = +\nVolumes = 2\n"},
		{"stub", "Embedded Stub Size = 4\n"},
		{"comment", "Comment = text\n"},
		{"tail", "Tail Size = 1\n"},
		{"error", "ERROR = Headers Error\n"},
		{"volume index", "Volume Index = 1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, dst, names, data := streamZIPFixture(t)
			writeFakeRARFile(t, "header.txt", fmt.Sprintf("Type = zip\nPhysical Size = %d\n%s", len(data), tc.suffix))
			if _, err := ExtractSet(context.Background(), src, names, dst, books, Defaults()); err == nil {
				t.Fatal("uncertain joined container accepted")
			}
		})
	}
	t.Run("ignored extra chunk", func(t *testing.T) {
		src, dst, names, data := streamZIPFixture(t)
		if err := src.WriteFile("stream.zip.003", []byte("unrelated"), 0o600); err != nil {
			t.Fatal(err)
		}
		names = append(names, "stream.zip.003")
		zipSetCopies(t, map[string][]byte{"archive.zip": append(append([]byte{}, data...), []byte("unrelated")...)})
		if _, err := ExtractSet(context.Background(), src, names, dst, books, Defaults()); err == nil {
			t.Fatal("ignored chunk accepted")
		}
	})
}

func TestSplitZIPNamesAndCancellation(t *testing.T) {
	for name, want := range map[string]int{"a.z01": 1, "a.Z099": 99, "a.z1000": 1000} {
		set, n, ok := ZIPSegmentOf(filepath.Join("dir", name))
		if !ok || set != filepath.Join("dir", "a.zip") || n != want {
			t.Fatal(name, set, n, ok)
		}
	}
	for _, name := range []string{"a.zip", "a.z1", "a.z00001", "a.z01.more"} {
		if _, _, ok := ZIPSegmentOf(name); ok {
			t.Fatal(name)
		}
	}
	for _, names := range [][]string{{"parts.z01", "parts.z02"}, {"parts.z02", "parts.zip"}, {"parts.z01", "other.zip"}, {"parts.z01", "parts.zip.002"}} {
		src, dst, _, _ := segmentZIPFixture(t, 2)
		if _, err := ExtractSet(context.Background(), src, names, dst, books, Defaults()); err == nil {
			t.Fatal("invalid family accepted", names)
		}
	}
	src, dst, names, _ := segmentZIPFixture(t, 2)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ExtractSet(ctx, src, names, dst, books, Defaults()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestZIPDirectoryParsingHasAReadBudget(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	extra := make([]byte, 60004)
	binary.LittleEndian.PutUint16(extra, 0xCAFE)
	binary.LittleEndian.PutUint16(extra[2:], 60000)
	for i := 0; i < 300; i++ {
		out, err := w.CreateHeader(&zip.FileHeader{Name: fmt.Sprintf("book-%03d.epub", i), Method: zip.Store, Extra: extra})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = out.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	if z, err := zip.NewReader(bytes.NewReader(data), int64(len(data))); err != nil || len(z.File) != 300 {
		t.Fatal("invalid oversized directory fixture", err)
	}
	src, dst, _ := roots(t, data)
	if x, err := ExtractZip(context.Background(), src, "a.zip", dst, books, Defaults()); !errors.Is(err, assessment.ErrLimit) || len(x.Members) != 0 {
		t.Fatal("oversized directory reached member processing", x, err)
	}
}

func TestSplitZIPSharedProbeBudgetAndBadSourceSignatures(t *testing.T) {
	t.Run("shared output", func(t *testing.T) {
		src, dst, names, _ := segmentZIPFixture(t, 2)
		for _, name := range []string{"header-archive.z01", "header-archive.zip"} {
			b, err := os.ReadFile(filepath.Join(os.Getenv("NEMALO_FAKE_7Z"), name))
			if err != nil {
				t.Fatal(err)
			}
			writeFakeRARFile(t, name, string(b)+"Padding = "+strings.Repeat("x", maxListing/2)+"\n")
		}
		if x, err := ExtractSet(context.Background(), src, names, dst, books, Defaults()); err == nil || len(x.Members) != 0 {
			t.Fatal("output budgets reset per segment", x, err)
		}
	})
	for _, stream := range []bool{true, false} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			var src, dst *os.Root
			var names []string
			var body []byte
			if stream {
				src, dst, names, body = streamZIPFixture(t)
			} else {
				var copies map[string][]byte
				src, dst, names, copies = segmentZIPFixture(t, 2)
				body = copies["archive.z01"]
			}
			bad := append([]byte{}, body...)
			copy(bad, "MZxx")
			if stream {
				if err := src.WriteFile(names[0], bad[:3], 0o600); err != nil {
					t.Fatal(err)
				}
				if err := src.WriteFile(names[1], bad[3:], 0o600); err != nil {
					t.Fatal(err)
				}
			} else if err := src.WriteFile(names[0], bad, 0o600); err != nil {
				t.Fatal(err)
			}
			if x, err := ExtractSet(context.Background(), src, names, dst, books, Defaults()); err == nil || len(x.Members) != 0 || !strings.Contains(err.Error(), "signature") && !strings.Contains(err.Error(), "marker") {
				t.Fatal("foreign executable signature reached reader", x, err)
			}
		})
	}
}
