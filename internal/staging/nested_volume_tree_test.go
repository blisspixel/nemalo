package staging

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func nestedAllSetLimits() Limits {
	l := oldRARSetLimits(4)
	l.RAR5SetCoverage, l.NestedRARCoverage, l.NestedVolumeSets = true, true, true
	return l
}

func TestNestedVolumeReaderFolderAliasCannotSelectAChildSet(t *testing.T) {
	body, _, _ := nestedRARSetFixture(t, 5, false)
	parts := zipMembersForNestedTest(t, body)
	var members []sevenMember
	for _, part := range parts {
		members = append(members, sevenMember{name: part.name, body: part.body})
	}
	members = append(members, sevenMember{name: parts[1].name + "/child.epub", body: []byte("a distinct generated member")})
	outer := coverageSeven("ambiguous nested selection", members...)
	treeProfiles(t, outer)
	saved, calls := SevenZip, 0
	SevenZip = func() (string, error) { calls++; return saved() }
	t.Cleanup(func() { SevenZip = saved })
	src, dst := treeRoots(t, "outer.7z", outer.body)
	x, err := ExtractTree(context.Background(), src, []string{"outer.7z"}, dst, books, nestedAllSetLimits())
	if err != nil || x.Complete || x.Whole || x.CoveredVolumeSets != 0 || calls != 1 {
		t.Fatal("folder-alias part reached a child reader", x, err, calls)
	}
	for _, m := range x.Members {
		if m.Disposition == "extracted" && m.Name != members[len(members)-1].name {
			t.Fatal("group bytes escaped ambiguous selection", m)
		}
	}
	if got, err := src.ReadFile("outer.7z"); err != nil || !bytes.Equal(got, outer.body) {
		t.Fatal("source changed", err)
	}
}

func TestNestedVolumeIndependentInterleavedGroupsKeepEverySourceSelection(t *testing.T) {
	fourBody, fourNames, fourWhole := nestedRARSetFixture(t, 4, true)
	fourParts, fourState := zipMembersForNestedTest(t, fourBody), os.Getenv("NEMALO_FAKE_7Z")
	fiveBody, fiveNames, fiveWhole := nestedRARSetFixture(t, 5, false)
	fiveParts, fiveState := zipMembersForNestedTest(t, fiveBody), os.Getenv("NEMALO_FAKE_7Z")
	registry := treeProfiles(t)
	registerNestedFixtureParts(t, registry, fourState, fourParts)
	registerNestedFixtureParts(t, registry, fiveState, fiveParts)
	entries := []member{fourParts[1], {name: "direct.epub", body: []byte("direct reference")}, fiveParts[2], fourParts[2], fiveParts[0], fourParts[0], fiveParts[1]}
	body := treeZIP(t, entries...)
	src, dst := treeRoots(t, "outer.zip", body)
	x, err := ExtractTree(context.Background(), src, []string{"outer.zip"}, dst, books, nestedAllSetLimits())
	if err != nil || !x.Whole || !x.Complete || x.CoveredVolumeSets != 2 || len(x.Members) != 9 || x.RARVersion != 0 || x.RARContainers != 0 || x.RARSetVersion != 0 || x.OldRARSetVersion != 0 {
		t.Fatal("independent groups changed the shared tree", x, err)
	}
	want := map[string][]byte{strings.Join([]string{"shelf/" + fourNames[0], "book.epub"}, "\x00"): fourWhole, strings.Join([]string{"shelf/" + fiveNames[0], "book.epub"}, "\x00"): fiveWhole, "direct.epub": []byte("direct reference")}
	originals := map[string]int{}
	leaves := 0
	for _, m := range x.Members {
		if m.Disposition != "extracted" {
			originals[m.Name]++
			continue
		}
		leaves++
		got, err := dst.ReadFile(m.Staged)
		if err != nil || !bytes.Equal(got, want[strings.Join(m.Chain, "\x00")]) {
			t.Fatal("group lost its exact output or provenance", m, err)
		}
	}
	if leaves != 3 || len(originals) != 6 {
		t.Fatal("missing or repeated group records", x)
	}
	for _, count := range originals {
		if count != 1 {
			t.Fatal("a physical member was accounted more than once", originals)
		}
	}
}

func TestNestedRARSetRequiresSelectedCapabilitiesWithoutBorrowing(t *testing.T) {
	for _, version := range []int{4, 5} {
		for _, old := range []bool{false, true} {
			for _, missing := range []string{"layout", "selected format", "other format", "standalone child"} {
				t.Run(fmt.Sprintf("%d/%t/%s", version, old, missing), func(t *testing.T) {
					body, _, _ := nestedRARSetFixture(t, version, old)
					src, dst := treeRoots(t, "outer.zip", body)
					l := nestedAllSetLimits()
					want := true
					switch missing {
					case "layout":
						l.OldRARSetCoverage, want = false, !old
					case "selected format":
						if version == 4 {
							l.RAR4Coverage = false
						} else {
							l.RAR5SetCoverage = false
						}
						want = false
					case "other format":
						if version == 4 {
							l.RAR5SetCoverage = false
						} else {
							l.RAR4Coverage, l.RAR4SetCoverage = false, false
						}
					case "standalone child":
						l.NestedRARCoverage = false
					}
					x, err := ExtractTree(context.Background(), src, []string{"outer.zip"}, dst, books, l)
					if err != nil || x.Whole != want || (x.CoveredVolumeSets == 1) != want || x.RARSetVersion != 0 || x.OldRARSetVersion != 0 || x.RARVersion != 0 || x.RARContainers != 0 {
						t.Fatal("a capability lent another layout or format its proof", x, err)
					}
					if !want && x.Complete {
						t.Fatal("requested unsupported group was reported complete", x)
					}
				})
			}
		}
	}
}

func registerNestedFixtureParts(t *testing.T, registry, state string, members []member) {
	t.Helper()
	for _, m := range members {
		if err := os.WriteFile(filepath.Join(registry, fmt.Sprintf("profile-%x", sha256.Sum256(m.body))), []byte(state), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNestedRARSetCoverageBindsEachOuterContainerSeparately(t *testing.T) {
	for _, version := range []int{4, 5} {
		for _, old := range []bool{false, true} {
			for _, kind := range []string{"rar4", "rar5", "7z"} {
				t.Run(fmt.Sprintf("%d/%t/%s", version, old, kind), func(t *testing.T) {
					zipBody, names, whole := nestedRARSetFixture(t, version, old)
					parts := zipMembersForNestedTest(t, zipBody)
					state := os.Getenv("NEMALO_FAKE_7Z")
					var members []sevenMember
					for _, part := range parts {
						members = append(members, sevenMember{name: part.name, body: part.body})
					}
					profile := coverageSeven("outer nested volumes", members...)
					outerName, wantVersion, wantStandalone := "outer.7z", 0, 0
					if kind != "7z" {
						wantVersion, wantStandalone, outerName = 4, 1, "outer.rar"
						if kind == "rar5" {
							wantVersion = 5
						}
						profile = coverageRAR(wantVersion, members...)
					}
					registry := treeProfiles(t, profile)
					registerNestedFixtureParts(t, registry, state, parts)
					src, dst := treeRoots(t, outerName, profile.body)
					x, err := ExtractTree(context.Background(), src, []string{outerName}, dst, books, nestedAllSetLimits())
					if err != nil || !x.Whole || !x.Complete || x.CoveredVolumeSets != 1 || x.RARVersion != wantVersion || x.RARContainers != wantStandalone || x.RARSetVersion != 0 || x.OldRARSetVersion != 0 {
						t.Fatal("child facts replaced ancestor evidence", x, err)
					}
					var leaves int
					for _, m := range x.Members {
						if m.Disposition != "extracted" {
							continue
						}
						leaves++
						got, err := dst.ReadFile(m.Staged)
						if err != nil || !bytes.Equal(got, whole) || !reflect.DeepEqual(m.Chain, []string{"shelf/" + names[0], "book.epub"}) {
							t.Fatal("ancestor lost the exact child selection", m, err)
						}
					}
					if leaves != 1 {
						t.Fatal("missing child leaf", x)
					}
					if got, err := src.ReadFile(outerName); err != nil || !bytes.Equal(got, profile.body) {
						t.Fatal("ancestor source changed", err)
					}
				})
			}
		}
	}
}

func TestNestedVolumeSetsShareDepthAndDoNotLendCoverageToAnAncestor(t *testing.T) {
	for _, wrappers := range []int{1, 2, 3} {
		t.Run(fmt.Sprint(wrappers), func(t *testing.T) {
			body, names, _ := nestedRARSetFixture(t, 5, true)
			parts := zipMembersForNestedTest(t, body)
			state := os.Getenv("NEMALO_FAKE_7Z")
			profiles := []treeProfile{{body: body, kind: "-tzip"}}
			for range wrappers {
				body = treeZIP(t, member{name: "level.zip", body: body})
				profiles = append(profiles, treeProfile{body: body, kind: "-tzip"})
			}
			registry := treeProfiles(t, profiles...)
			registerNestedFixtureParts(t, registry, state, parts)
			src, dst := treeRoots(t, "outer.zip", body)
			x, err := ExtractTree(context.Background(), src, []string{"outer.zip"}, dst, books, nestedAllSetLimits())
			if err != nil {
				t.Fatal(x, err)
			}
			if wrappers < 3 {
				if !x.Whole || !x.Complete || x.CoveredVolumeSets != 1 {
					t.Fatal("supported depth was refused", x)
				}
				for _, m := range x.Members {
					if m.Disposition == "extracted" && (len(m.Chain) != wrappers+2 || m.Chain[wrappers] != "shelf/"+names[0]) {
						t.Fatal("group depth or provenance changed", m)
					}
				}
			} else if x.Whole || x.Complete || x.CoveredVolumeSets != 0 || x.CoverageGap == nil {
				t.Fatal("nested group reset the depth allowance", x)
			}
		})
	}
	t.Run("unproven ancestor", func(t *testing.T) {
		body, _, _ := nestedRARSetFixture(t, 4, false)
		parts, state := zipMembersForNestedTest(t, body), os.Getenv("NEMALO_FAKE_7Z")
		var members []sevenMember
		for _, part := range parts {
			members = append(members, sevenMember{name: part.name, body: part.body})
		}
		outer := coverageSeven("unproven ancestor", members...)
		outer.header += "Comment = retained metadata\n"
		registry := treeProfiles(t, outer)
		registerNestedFixtureParts(t, registry, state, parts)
		src, dst := treeRoots(t, "outer.7z", outer.body)
		x, err := ExtractTree(context.Background(), src, []string{"outer.7z"}, dst, books, nestedAllSetLimits())
		if err != nil || !x.Complete || x.Whole || x.CoveredVolumeSets != 1 || x.CoverageGap == nil || len(x.CoverageGap.Chain) != 0 {
			t.Fatal("child repaired unsupported ancestor evidence", x, err)
		}
	})
}
