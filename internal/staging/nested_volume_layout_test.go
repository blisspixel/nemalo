package staging

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"testing"
)

func nestedOtherSetFixture(t *testing.T, kind string) ([]member, string, map[string][]byte) {
	t.Helper()
	var outer []member
	want := map[string][]byte{}
	switch kind {
	case "zip chunks":
		src, _, names, _ := streamZIPFixture(t)
		for i := len(names) - 1; i >= 0; i-- {
			b, err := src.ReadFile(names[i])
			if err != nil {
				t.Fatal(err)
			}
			outer = append(outer, member{name: "shelf/" + names[i], body: b})
		}
		want["book.epub"] = []byte("a synthetic book")
		return outer, "shelf/" + names[0], want
	case "7z chunks":
		p := coverageSeven("nested volume reference", sevenMember{name: "book.epub", body: []byte("a generated child book")})
		_, _ = sevenFixture(t, p.members)
		writeFakeRARFile(t, "kind.txt", p.kind)
		writeFakeRARFile(t, "header.txt", p.header)
		for i := 2; i >= 0; i-- {
			outer = append(outer, member{name: fmt.Sprintf("shelf/child.7z.%03d", i+1), body: p.body[i*len(p.body)/3 : (i+1)*len(p.body)/3]})
		}
		want["book.epub"] = p.members[0].body
		return outer, "shelf/child.7z.001", want
	case "zip segments":
		_, _ = sevenFixture(t, nil)
		t.Setenv("NEMALO_FAKE_7Z_TYPE", "-tzip")
		s, entries := segmentProofFixture(t, true)
		var listing bytes.Buffer
		for _, e := range entries {
			fmt.Fprintf(&listing, "Path = %s\nSize = %d\nPacked Size = %d\nCRC = %08X\nVolume Index = %d\nOffset = %d\n\n", e.name, e.size, e.packed, e.crc, e.volume, e.offset)
			body := bytes.Repeat([]byte(e.name), 10000)
			want[e.name] = body
			writeFakeRARFile(t, fmt.Sprintf("members/%x", e.name), string(body))
		}
		writeFakeRARFile(t, "listing.txt", listing.String())
		copies := map[string][]byte{}
		for i, part := range s.Parts {
			name := fmt.Sprintf("archive.z%02d", i+1)
			if i+1 == len(s.Parts) {
				name = "archive.zip"
			}
			copies[name] = part
			writeFakeRARFile(t, "header-"+name, fmt.Sprintf("Type = zip\nPhysical Size = %d\nTotal Physical Size = %d\nEmbedded Stub Size = 4\nMultivolume = +\nVolume Index = %d\nVolumes = %d\n", len(part), len(s.Raw), i, len(s.Parts)))
			outer = append(outer, member{name: "shelf/" + name, body: part})
		}
		zipSetCopies(t, copies)
		// Deliberately put the terminal ZIP before the first disk.
		outer[0], outer[len(outer)-1] = outer[len(outer)-1], outer[0]
		return outer, "shelf/archive.z01", want
	default:
		t.Fatal("unknown nested fixture", kind)
		return nil, "", nil
	}
}

func TestNestedOtherRootLayoutsReuseTheirProofsAndOriginalNames(t *testing.T) {
	for _, kind := range []string{"zip chunks", "7z chunks", "zip segments"} {
		for _, coverage := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", kind, coverage), func(t *testing.T) {
				members, first, want := nestedOtherSetFixture(t, kind)
				body := treeZIP(t, members...)
				src, dst := treeRoots(t, "outer.zip", body)
				l := Defaults()
				l.Coverage, l.NestedVolumeSets = coverage, true
				x, err := ExtractTree(context.Background(), src, []string{"outer.zip"}, dst, books, l)
				count := 0
				if coverage {
					count = 1
				}
				if err != nil || !x.Complete || x.Whole != coverage || x.CoveredVolumeSets != count || x.RARVersion != 0 || x.RARContainers != 0 || x.RARSetVersion != 0 || x.OldRARSetVersion != 0 {
					t.Fatal("nested layout did not retain its separate evidence", x, err)
				}
				found := map[string]bool{}
				staged := map[string]bool{}
				for _, m := range x.Members {
					if m.Disposition != "extracted" {
						continue
					}
					got, err := dst.ReadFile(m.Staged)
					if err != nil || !bytes.Equal(got, want[m.Name]) || !reflect.DeepEqual(m.Chain, []string{first, m.Name}) {
						t.Fatal("leaf identity or source chain changed", m, err)
					}
					found[m.Name] = true
					staged[filepath.ToSlash(m.Staged)] = true
				}
				if len(found) != len(want) {
					t.Fatal("missing nested output", found, want)
				}
				if got, err := src.ReadFile("outer.zip"); err != nil || !bytes.Equal(got, body) {
					t.Fatal("source changed", err)
				}
				// Only expected leaves remain; physical part snapshots are removed.
				files := 0
				err = fs.WalkDir(dst.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if !entry.IsDir() {
						files++
						if !staged[name] {
							t.Error("retained private part", name)
						}
					}
					return nil
				})
				if err != nil || files != len(want) {
					t.Fatal("private staging cleanup differs", files, err)
				}
			})
		}
	}
}
