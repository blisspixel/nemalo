package staging

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func coverageSeven(label string, members ...sevenMember) treeProfile {
	packed := 0
	for i := range members {
		members[i].packed = fmt.Sprint(len(members[i].body))
		packed += len(members[i].body)
	}
	body := append([]byte("7z\xbc\xaf\x27\x1c"), make([]byte, packed+128)...)
	copy(body[8:], label)
	return treeProfile{body: body, kind: "-t7z", members: members, header: fmt.Sprintf("Type = 7z\nPhysical Size = %d\nHeaders Size = %d\n", len(body), len(body)-packed)}
}

func TestArchiveTreeCoverageRequiresEveryContainer(t *testing.T) {
	for _, kind := range []string{"complete", "inner_gap", "inner_unknown_extra", "7z_gap", "7z_comment", "rar", "nested_set", "missing_tool", "depth"} {
		t.Run(kind, func(t *testing.T) {
			inner := treeZIP(t, member{name: "book.epub", body: []byte("a synthetic book")})
			if kind == "inner_gap" {
				inner = insertDirectoryBytes(inner, len(inner)-22, []byte("retained bytes"))
			}
			if kind == "inner_unknown_extra" {
				inner = zipProofFixture(t, []byte{0xfe, 0xca, 1, 0, 'x'}, "")
			}
			middle := coverageSeven("middle", sevenMember{name: "inner.zip", body: inner})
			if kind == "7z_gap" {
				middle.header = strings.Replace(middle.header, fmt.Sprintf("Headers Size = %d", 134), "Headers Size = 1", 1)
			}
			if kind == "7z_comment" {
				middle.header += "Comment = unheld notes\n"
			}
			childName, childBody := "middle.7z", middle.body
			if kind == "rar" {
				childName, childBody = "child.rar", []byte("Rar!\x1a\x07\x01\x00 synthetic RAR")
			}
			if kind == "nested_set" {
				childName, childBody = "child.7z.001", middle.body
			}
			profiles := []treeProfile{middle, {body: inner, kind: "-tzip"}}
			if kind == "depth" {
				for i := 0; i < 3; i++ {
					childBody = treeZIP(t, member{name: childName, body: childBody})
					childName = "level.zip"
					profiles = append(profiles, treeProfile{body: childBody, kind: "-tzip"})
				}
			}
			outer := treeZIP(t, member{name: childName, body: childBody})
			treeProfiles(t, profiles...)
			if kind == "missing_tool" {
				saved := SevenZip
				SevenZip = func() (string, error) { return "", ErrNoArchiveTool }
				t.Cleanup(func() { SevenZip = saved })
			}
			src, dst := treeRoots(t, "outer.zip", outer)
			limits := Defaults()
			limits.Coverage = true
			x, err := ExtractTree(context.Background(), src, []string{"outer.zip"}, dst, books, limits)
			if kind == "complete" {
				if err != nil || !x.Whole || !x.Complete || len(x.Members) != 3 || x.Members[2].Disposition != "extracted" {
					t.Fatal(x, err)
				}
			} else if x.Whole {
				t.Fatal("an unproven child authorized the tree", x, err)
			}
			got, _ := src.ReadFile("outer.zip")
			if !bytes.Equal(got, outer) {
				t.Fatal("source changed")
			}
		})
	}
}

func TestArchiveTreeCoverageSharesProofReadsAcrossChildren(t *testing.T) {
	inner := treeZIP(t, member{name: "book.epub", body: bytes.Repeat([]byte("x"), 16384)})
	outer := treeZIP(t, member{name: "one.zip", body: inner}, member{name: "two.zip", body: inner})
	treeProfiles(t, treeProfile{body: inner, kind: "-tzip"})
	// Measure the exact single-node framing work independently. There is enough
	// for the outer and one child, never a reset at the second child's start.
	spent := func(body []byte) int64 {
		r := &zipProofReader{ctx: context.Background(), source: bytes.NewReader(body), remaining: zipProofBytes}
		if ok, why := ZIPAccounted(context.Background(), r, int64(len(body))); !ok {
			t.Fatal(why)
		}
		return zipProofBytes - r.remaining
	}
	src, dst := treeRoots(t, "outer.zip", outer)
	limits := Defaults()
	limits.Coverage = true
	limits.tree = &treeBudget{members: 10000, bytes: 1 << 30, processes: 1000, reads: allowance{remaining: 8 << 30}, diagnostics: allowance{remaining: 16 << 20}, proofs: allowance{remaining: spent(outer) + spent(inner)}}
	x, err := extractTree(context.Background(), src, []string{"outer.zip"}, dst, books, limits)
	if err != nil || x.Whole || x.Complete || limits.tree.proofs.left() != 0 {
		t.Fatal(x, err, limits.tree.proofs.left())
	}
	var leaves int
	for _, m := range x.Members {
		if m.Disposition == "extracted" {
			leaves++
		}
	}
	if leaves != 1 {
		t.Fatal("proof allowance reset or refused the proven child", x)
	}
}

func TestArchiveTree7zCoverageProbesUseSharedRunner(t *testing.T) {
	child := coverageSeven("child", sevenMember{name: "book.epub", body: []byte("book")})
	for _, diagnostics := range []bool{false, true} {
		t.Run(fmt.Sprint(diagnostics), func(t *testing.T) {
			outer := coverageSeven("outer", sevenMember{name: "child.7z", body: child.body})
			dir := treeProfiles(t, outer, child)
			src, dst := treeRoots(t, "outer.7z", outer.body)
			limits := Defaults()
			limits.Coverage = true
			limits.tree = &treeBudget{members: 10000, bytes: 1 << 30, processes: 3, reads: allowance{remaining: 8 << 30}, diagnostics: allowance{remaining: 16 << 20}, proofs: allowance{remaining: zipProofBytes}}
			if diagnostics {
				limits.tree.processes = 1000
				limits.tree.diagnostics.remaining = 1
			}
			x, err := extractTree(context.Background(), src, []string{"outer.7z"}, dst, books, limits)
			if x.Whole {
				t.Fatal("coverage probe bypassed a shared limit", x, err)
			}
			if !diagnostics && limits.tree.processes != 0 {
				t.Fatal(limits.tree.processes)
			}
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if len(calls) > 3 {
				t.Fatal("unbounded proof subprocess", len(calls))
			}
		})
	}
}

func TestArchiveTreeCoverageRetainsUnknownDirectoryBytes(t *testing.T) {
	p := coverageSeven("unknown directory", sevenMember{name: "folder", attributes: "D", body: nil})
	_, _ = sevenFixture(t, p.members)
	writeFakeRARFile(t, "kind.txt", "-t7z")
	writeFakeRARFile(t, "header.txt", p.header)
	listing, err := os.ReadFile(filepath.Join(os.Getenv("NEMALO_FAKE_7Z"), "listing.txt"))
	if err != nil {
		t.Fatal(err)
	}
	writeFakeRARFile(t, "listing.txt", strings.Replace(string(listing), "Packed Size = 0\n", "Packed Size = \n", 1))
	src, dst := treeRoots(t, "outer.7z", p.body)
	limits := Defaults()
	limits.Coverage = true
	x, err := ExtractTree(context.Background(), src, []string{"outer.7z"}, dst, books, limits)
	if err != nil || len(x.Members) != 1 || x.Members[0].Disposition != "directory_unknown_size" {
		t.Fatal(x, err)
	}
}

func TestArchiveTreeCoverageDefaultRemainsOptIn(t *testing.T) {
	inner := treeZIP(t, member{name: "book.epub", body: []byte("book")})
	outer := treeZIP(t, member{name: "child.zip", body: inner})
	treeProfiles(t, treeProfile{body: inner, kind: "-tzip"})
	src, dst := treeRoots(t, "outer.zip", outer)
	x, err := ExtractTree(context.Background(), src, []string{"outer.zip"}, dst, books, Defaults())
	if err != nil || !x.Complete || x.Whole || x.Members[1].Disposition != "extracted" {
		t.Fatal(x, err)
	}
}
