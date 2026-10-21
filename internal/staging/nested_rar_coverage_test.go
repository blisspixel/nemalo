package staging

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func coverageRAR(version int, members ...sevenMember) treeProfile {
	records := make([][]byte, 0, len(members))
	for i := range members {
		members[i].packed = fmt.Sprint(len(members[i].body))
		if version == 4 {
			records = append(records, rar4File(members[i].name, members[i].body, false))
		} else {
			records = append(records, rar5Record(2, 2, rar5FileBody(members[i].name, members[i].body, 6, 0100644, 0, 1), members[i].body))
		}
	}
	if version == 4 {
		return treeProfile{body: rar4Archive(records...), kind: "-tRar", members: members}
	}
	return treeProfile{body: rar5Archive(records...), kind: "-tRar5", members: members}
}

func nestedRARLimits() Limits {
	l := Defaults()
	l.Coverage, l.RAR4Coverage, l.NestedRARCoverage = true, true, true
	return l
}

func TestNestedRARCoverageRequiresEveryContainerAndExactChain(t *testing.T) {
	for _, version := range []int{4, 5} {
		for _, outerKind := range []string{"zip", "7z", "rar4", "rar5"} {
			t.Run(fmt.Sprintf("child%d/%s", version, outerKind), func(t *testing.T) {
				payload := []byte("synthetic nested book")
				child := coverageRAR(version, sevenMember{name: "folder/book.epub", body: payload})
				name, body, wantVersion := "outer.zip", treeZIP(t, member{name: "child.rar", body: child.body}), 0
				profiles := []treeProfile{child}
				switch outerKind {
				case "7z":
					outer := coverageSeven("outer", sevenMember{name: "child.rar", body: child.body})
					name, body = "outer.7z", outer.body
					profiles = append(profiles, outer)
				case "rar4", "rar5":
					wantVersion = 4
					if outerKind == "rar5" {
						wantVersion = 5
					}
					outer := coverageRAR(wantVersion, sevenMember{name: "child.rar", body: child.body})
					name, body = "outer.rar", outer.body
					profiles = append(profiles, outer)
				}
				treeProfiles(t, profiles...)
				src, dst := treeRoots(t, name, body)
				x, err := ExtractTree(context.Background(), src, []string{name}, dst, books, nestedRARLimits())
				if err != nil || !x.Whole || !x.Complete || x.CoverageGap != nil || x.RARVersion != wantVersion || len(x.Members) != 2 || x.Members[1].Disposition != "extracted" || !reflect.DeepEqual(x.Members[1].Chain, []string{"child.rar", "folder/book.epub"}) {
					t.Fatal(x, err)
				}
				if got, err := dst.ReadFile(x.Members[1].Staged); err != nil || !bytes.Equal(got, payload) {
					t.Fatal("exact nested leaf differs", got, err)
				}
				if got, err := src.ReadFile(name); err != nil || !bytes.Equal(got, body) {
					t.Fatal("outer source changed", err)
				}
			})
		}
	}
}

func TestNestedRARCoverageRequiresItsOwnExplicitOptIn(t *testing.T) {
	for _, version := range []int{4, 5} {
		for _, mode := range []string{"default", "nested_flag_only", "coverage_only", "missing_rar4_opt_in", "enabled"} {
			t.Run(fmt.Sprintf("%d/%s", version, mode), func(t *testing.T) {
				child := coverageRAR(version, sevenMember{name: "book.epub", body: []byte("book")})
				treeProfiles(t, child)
				body := treeZIP(t, member{name: "child.rar", body: child.body})
				src, dst := treeRoots(t, "outer.zip", body)
				l := nestedRARLimits()
				switch mode {
				case "default":
					l = Defaults()
				case "nested_flag_only":
					l.Coverage = false
				case "coverage_only":
					l.NestedRARCoverage = false
				case "missing_rar4_opt_in":
					l.RAR4Coverage = false
				}
				x, err := ExtractTree(context.Background(), src, []string{"outer.zip"}, dst, books, l)
				wantWhole := mode == "enabled" || mode == "missing_rar4_opt_in" && version == 5
				wantComplete := wantWhole || mode == "default" || mode == "nested_flag_only"
				if err != nil || x.Whole != wantWhole || x.Complete != wantComplete {
					t.Fatal(x, err)
				}
				if !wantComplete && (x.CoverageGap == nil || !reflect.DeepEqual(x.CoverageGap.Chain, []string{"child.rar"})) {
					t.Fatal("refusal lost its chain", x)
				}
			})
		}
	}
}

func TestNestedRARCoverageRefusesHiddenRecordsAndUnknownPreflight(t *testing.T) {
	for _, version := range []int{4, 5} {
		for _, refusal := range []string{"hidden", "tail", "physical_volume", "volume", "unknown_volume", "wrong_type", "wrong_size", "stub", "warning"} {
			t.Run(fmt.Sprintf("%d/%s", version, refusal), func(t *testing.T) {
				child := coverageRAR(version, sevenMember{name: "book.epub", body: []byte("book")})
				switch refusal {
				case "hidden":
					if version == 4 {
						child.body = rar4Archive(rar4File("book.epub", []byte("book"), false), rar4Block(0x7a, 0, []byte("unheld notes"), nil))
					} else {
						child.body = rar5Archive(rar5Record(2, 2, rar5FileBody("book.epub", []byte("book"), 6, 0100644, 0, 1), []byte("book")), rar5Record(3, 0, rar5Numbers(0), nil))
					}
				case "tail":
					child.body = append(child.body, []byte("unheld tail")...)
				case "physical_volume":
					if version == 4 {
						child.body = append([]byte("Rar!\x1a\x07\x00"), rar4Block(0x73, 1, make([]byte, 6), nil)...)
						child.body = append(child.body, rar4File("book.epub", []byte("book"), false)...)
						child.body = append(child.body, rar4Block(0x7b, 0, nil, nil)...)
					} else {
						child.body = append([]byte("Rar!\x1a\x07\x01\x00"), rar5Record(1, 0, rar5Numbers(1), nil)...)
						child.body = append(child.body, rar5Record(2, 2, rar5FileBody("book.epub", []byte("book"), 6, 0100644, 0, 1), []byte("book"))...)
						child.body = append(child.body, rar5Record(5, 0, rar5Numbers(0), nil)...)
					}
				default:
					child.header = fmt.Sprintf("Type = %s\nPhysical Size = %d\nMultivolume = -\nVolumes = 1\n", strings.TrimPrefix(child.kind, "-t"), len(child.body))
					switch refusal {
					case "volume":
						child.header = strings.Replace(child.header, "Multivolume = -", "Multivolume = +", 1)
					case "unknown_volume":
						child.header = strings.Replace(child.header, "Volumes = 1\n", "", 1)
					case "wrong_type":
						child.header = strings.Replace(child.header, "Type = "+strings.TrimPrefix(child.kind, "-t"), "Type = 7z", 1)
					case "wrong_size":
						child.header = strings.Replace(child.header, fmt.Sprint(len(child.body)), "1", 1)
					case "stub":
						child.header += "Embedded Stub Size = 1\n"
					case "warning":
						child.header += "ambiguous reader warning\n"
					}
				}
				dir := treeProfiles(t, child)
				body := treeZIP(t, member{name: "child.rar", body: child.body})
				src, dst := treeRoots(t, "outer.zip", body)
				x, err := ExtractTree(context.Background(), src, []string{"outer.zip"}, dst, books, nestedRARLimits())
				if err != nil || x.Whole || x.Complete || x.CoverageGap == nil || !reflect.DeepEqual(x.CoverageGap.Chain, []string{"child.rar"}) {
					t.Fatal("unproven child gained coverage", x, err)
				}
				calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
				if (refusal == "hidden" || refusal == "tail" || refusal == "physical_volume") && len(calls) != 0 {
					t.Fatal("unaccounted framing reached reader", len(calls))
				}
				if got, _ := src.ReadFile("outer.zip"); !bytes.Equal(got, body) {
					t.Fatal("refused source changed")
				}
			})
		}
	}
}

func proofWork(t *testing.T, p treeProfile) int64 {
	t.Helper()
	r := &coverageReaderAt{ctx: context.Background(), source: bytes.NewReader(p.body), remaining: archiveProofBytes}
	switch p.kind {
	case "-tRar":
		if _, err := RAR4Accounted(context.Background(), r, int64(len(p.body))); err != nil {
			t.Fatal(err)
		}
	case "-tRar5":
		if _, err := RAR5Accounted(context.Background(), r, int64(len(p.body))); err != nil {
			t.Fatal(err)
		}
	default:
		if ok, why := ZIPAccounted(context.Background(), r, int64(len(p.body))); !ok {
			t.Fatal(why)
		}
	}
	return archiveProofBytes - r.remaining
}

func TestNestedRARCoverageSharesAllWorkAcrossSiblings(t *testing.T) {
	for _, budget := range []string{"proof", "reader", "process", "diagnostic", "member", "output"} {
		t.Run(budget, func(t *testing.T) {
			one := coverageRAR(4, sevenMember{name: "book.epub", body: []byte("first book")})
			two := coverageRAR(5, sevenMember{name: "book.epub", body: []byte("second book")})
			if budget == "diagnostic" {
				two.stderr = strings.Repeat("x", 2000)
			}
			outer := treeZIP(t, member{name: "one.rar", body: one.body}, member{name: "two.rar", body: two.body})
			name := "outer.zip"
			rootProfile := treeProfile{body: outer, kind: "-tzip"}
			profiles := []treeProfile{one, two}
			if budget == "reader" {
				rootProfile = coverageRAR(4, sevenMember{name: "one.rar", body: one.body}, sevenMember{name: "two.rar", body: two.body})
				name, outer = "outer.rar", rootProfile.body
				profiles = append(profiles, rootProfile)
			}
			dir := treeProfiles(t, profiles...)
			src, dst := treeRoots(t, name, outer)
			l := nestedRARLimits()
			l.tree = &treeBudget{members: 10000, bytes: 1 << 30, processes: 1000, reads: allowance{remaining: 8 << 30}, diagnostics: allowance{remaining: 16 << 20}, proofs: allowance{remaining: archiveProofBytes}}
			switch budget {
			case "proof":
				l.tree.proofs.remaining = proofWork(t, treeProfile{body: outer, kind: "-tzip"}) + proofWork(t, one)
			case "reader":
				l.tree.reads.remaining = proofWork(t, rootProfile) + proofWork(t, one)
			case "process":
				l.tree.processes = 3 // first child's preflight, table and exact extraction
			case "diagnostic":
				l.tree.diagnostics.remaining = 1700
			case "member":
				l.tree.members = 3 // two outer selections plus the first leaf
			case "output":
				l.tree.bytes = int64(len(one.body) + len(two.body) + 2 + len(one.members[0].body) + 1)
			}
			x, err := extractTree(context.Background(), src, []string{name}, dst, books, l)
			if err != nil || x.Whole != (budget == "output") || x.Complete {
				t.Fatal("a sibling reset its budget", x, err)
			}
			leaves := 0
			for _, m := range x.Members {
				if m.Disposition == "extracted" {
					leaves++
				}
			}
			if leaves != 1 {
				t.Fatal("expected exactly the first verified leaf", x)
			}
			switch budget {
			case "proof":
				if l.tree.proofs.left() != 0 {
					t.Fatal("proof failure refunded work", l.tree.proofs.left())
				}
			case "reader":
				if l.tree.reads.left() != 0 {
					t.Fatal("reader failure refunded work", l.tree.reads.left())
				}
			case "process":
				calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
				if len(calls) != 3 || l.tree.processes != 0 {
					t.Fatal("unbounded process retries", len(calls), l.tree.processes)
				}
			case "diagnostic":
				if l.tree.diagnostics.left() != 0 {
					t.Fatal("diagnostic failure refunded work", l.tree.diagnostics.left())
				}
			case "member":
				if l.tree.members != 0 {
					t.Fatal("member failure refunded work", l.tree.members)
				}
			case "output":
				if l.tree.bytes != 0 {
					t.Fatal("output allowance was reset", l.tree.bytes)
				}
			}
		})
	}
}

func TestNestedRARCoverageKeepsPhysicalFactsSeparateFromMemberResults(t *testing.T) {
	for _, version := range []int{4, 5} {
		for _, kind := range []string{"notes", "table_size", "table_crc", "table_name", "output_crc"} {
			t.Run(fmt.Sprintf("%d/%s", version, kind), func(t *testing.T) {
				book := sevenMember{name: "book.epub", body: []byte("synthetic book")}
				child := coverageRAR(version, book)
				if kind == "notes" {
					child = coverageRAR(version, book, sevenMember{name: "notes.txt", body: []byte("keep these notes")})
				}
				switch kind {
				case "table_size":
					child.members[0].listedSize = 15
				case "table_crc":
					child.members[0].listedCRC = "00000000"
				case "table_name":
					child.members[0].name = "alias.epub"
				case "output_crc":
					child.members[0].served = bytes.Repeat([]byte("x"), len(book.body))
				}
				treeProfiles(t, child)
				body := treeZIP(t, member{name: "child.rar", body: child.body})
				src, dst := treeRoots(t, "outer.zip", body)
				x, err := ExtractTree(context.Background(), src, []string{"outer.zip"}, dst, books, nestedRARLimits())
				if err != nil {
					t.Fatal(err)
				}
				switch kind {
				case "notes":
					if !x.Whole || !x.Complete || len(x.Members) != 3 || x.Members[2].Disposition != "unsupported_format" {
						t.Fatal("physical evidence swallowed unheld notes", x)
					}
				case "output_crc":
					if !x.Whole || x.Complete || len(x.Members) != 2 || x.Members[1].Disposition != "failed" {
						t.Fatal("framing and output integrity conflated", x)
					}
				default:
					if x.Whole || x.Complete || len(x.Members) != 1 || x.Members[0].Disposition != "failed" {
						t.Fatal("reader mismatch granted evidence", x)
					}
				}
			})
		}
	}
}

func TestNestedRARCoverageKeepsTheWholeTreeDepthLimit(t *testing.T) {
	child := coverageRAR(4, sevenMember{name: "book.epub", body: []byte("book")})
	profiles := []treeProfile{child}
	for depth := 0; depth < 4; depth++ {
		child = coverageRAR(4+depth%2, sevenMember{name: "next.rar", body: child.body})
		profiles = append(profiles, child)
	}
	treeProfiles(t, profiles...)
	body := treeZIP(t, member{name: "child.rar", body: child.body})
	src, dst := treeRoots(t, "outer.zip", body)
	x, err := ExtractTree(context.Background(), src, []string{"outer.zip"}, dst, books, nestedRARLimits())
	if err != nil || x.Whole || x.Complete || len(x.Members) != 4 || !strings.Contains(x.Members[3].Detail, "four-level") || x.CoverageGap == nil || len(x.CoverageGap.Chain) != 4 {
		t.Fatal("RAR child reset depth", x, err)
	}
	for _, m := range x.Members {
		if m.Staged != "" {
			t.Fatal("depth-refused bytes escaped staging", m)
		}
	}
}

func TestNestedRARCoverageCancelsAnActiveChildPreflight(t *testing.T) {
	for _, version := range []int{4, 5} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			child := coverageRAR(version, sevenMember{name: "book.epub", body: []byte("book")})
			child.wait = true
			dir := treeProfiles(t, child)
			body := treeZIP(t, member{name: "child.rar", body: child.body})
			src, dst := treeRoots(t, "outer.zip", body)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			observed := make(chan struct{})
			go func() {
				defer close(observed)
				tick := time.NewTicker(5 * time.Millisecond)
				defer tick.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-tick.C:
						if calls, err := os.ReadFile(filepath.Join(dir, "calls")); err == nil && len(calls) > 0 {
							cancel()
							return
						}
					}
				}
			}()
			x, err := ExtractTree(ctx, src, []string{"outer.zip"}, dst, books, nestedRARLimits())
			<-observed
			if !errors.Is(err, context.Canceled) || x.Whole || x.Complete {
				t.Fatal("active child ignored cancellation", x, err)
			}
			if got, _ := src.ReadFile("outer.zip"); !bytes.Equal(got, body) {
				t.Fatal("cancelled source changed")
			}
		})
	}
}

func TestNestedRARCoverageNeverAssemblesChildVolumeNames(t *testing.T) {
	for _, names := range [][]string{{"book.part1.rar", "book.part2.rar"}, {"book.rar", "book.r00"}} {
		t.Run(names[0], func(t *testing.T) {
			child := coverageRAR(4, sevenMember{name: "book.epub", body: []byte("book")})
			saved := SevenZip
			SevenZip = func() (string, error) { t.Fatal("nested volume reached reader"); return "", nil }
			t.Cleanup(func() { SevenZip = saved })
			body := treeZIP(t, member{name: names[0], body: child.body}, member{name: names[1], body: []byte("continuation")})
			src, dst := treeRoots(t, "outer.zip", body)
			x, err := ExtractTree(context.Background(), src, []string{"outer.zip"}, dst, books, nestedRARLimits())
			if err != nil || x.Whole || len(x.Members) != 2 || x.Members[0].Disposition != "nested_volume" || x.Members[1].Disposition != "nested_volume" {
				t.Fatal("child volume names gained coverage", x, err)
			}
		})
	}
}
