package staging

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNestedVolumePartRefusalsPreventWholeGroupRead(t *testing.T) {
	for _, coverage := range []bool{false, true} {
		for _, kind := range []string{"link", "special", "directory", "empty", "encrypted", "method", "member limit", "missing reader"} {
			t.Run(fmt.Sprintf("%s/coverage=%t", kind, coverage), func(t *testing.T) {
				body, _, _ := nestedRARSetFixture(t, 5, false)
				parts := zipMembersForNestedTest(t, body)
				l := nestedAllSetLimits()
				l.Coverage = coverage
				switch kind {
				case "link":
					parts[1].mode = fs.ModeSymlink | 0600
				case "special":
					parts[1].mode = fs.ModeNamedPipe | 0600
				case "directory":
					parts[1].mode = fs.ModeDir | 0700
				case "empty":
					parts[1].body = nil
				case "member limit":
					l.MemberBytes = int64(len(parts[1].body) - 1)
				}
				body = treeZIP(t, parts...)
				if kind == "encrypted" || kind == "method" {
					// Set both physical and central declarations on the same part.
					pos := 0
					for range 2 {
						at := bytes.Index(body[pos:], []byte("PK\x01\x02")) + pos
						if at < pos {
							t.Fatal("missing synthetic directory")
						}
						local := int(binary.LittleEndian.Uint32(body[at+42:]))
						if string(body[at+46:at+46+len(parts[1].name)]) == parts[1].name {
							if kind == "encrypted" {
								binary.LittleEndian.PutUint16(body[at+8:], 1)
								binary.LittleEndian.PutUint16(body[local+6:], 1)
							} else {
								binary.LittleEndian.PutUint16(body[at+10:], 99)
								binary.LittleEndian.PutUint16(body[local+8:], 99)
							}
							break
						}
						pos = at + 46
					}
				}
				saved, calls := SevenZip, 0
				SevenZip = func() (string, error) { calls++; return "", ErrNoArchiveTool }
				t.Cleanup(func() { SevenZip = saved })
				src, dst := treeRoots(t, "outer.zip", body)
				x, err := ExtractTree(context.Background(), src, []string{"outer.zip"}, dst, books, l)
				physicalRefusal := coverage && (kind == "encrypted" || kind == "method")
				if physicalRefusal {
					if !errors.Is(err, ErrZIPCoverage) || len(x.Members) != 0 {
						t.Fatal("outer framing did not refuse unsupported declarations", x, err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if x.Complete || x.Whole || x.CoveredVolumeSets != 0 || (x.CoverageGap != nil) != coverage {
					t.Fatal("refused part gained a complete group", x, err)
				}
				if kind == "missing reader" {
					if calls != 1 {
						t.Fatal("missing reader not reached", calls)
					}
				} else if calls != 0 {
					t.Fatal("a refused part reached the child reader", calls)
				}
				for _, m := range x.Members {
					if m.Staged != "" || m.Disposition == "extracted" {
						t.Fatal("a partial group escaped", m)
					}
				}
				if got, err := src.ReadFile("outer.zip"); err != nil || !bytes.Equal(got, body) {
					t.Fatal("source changed", err)
				}
			})
		}
	}
}

func TestNestedVolumeEvidenceStaysOutOfJSON(t *testing.T) {
	x := Extraction{Complete: true, Whole: true, CoveredVolumeSets: 7}
	b, err := json.Marshal(x)
	if err != nil || bytes.Contains(bytes.ToLower(b), []byte("volume")) {
		t.Fatal("internal evidence became a wire contract", string(b), err)
	}
	var decoded Extraction
	if err := json.Unmarshal(b, &decoded); err != nil || decoded.CoveredVolumeSets != 0 {
		t.Fatal("serialized data conferred internal evidence", decoded, err)
	}
}

func TestNestedVolumeActiveDeadlineRetainsUnresolvedGroup(t *testing.T) {
	body, _, _ := nestedRARSetFixture(t, 5, false)
	src, dst := treeRoots(t, "outer.zip", body)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	saved := SevenZip
	entered := false
	SevenZip = func() (string, error) { entered = true; <-ctx.Done(); return "", ctx.Err() }
	t.Cleanup(func() { SevenZip = saved })
	x, err := ExtractTree(ctx, src, []string{"outer.zip"}, dst, books, nestedAllSetLimits())
	if !entered || !errors.Is(err, context.DeadlineExceeded) || x.Complete || x.Whole || x.CoveredVolumeSets != 0 {
		t.Fatal("active deadline escaped the unresolved group", entered, x, err)
	}
	if got, err := src.ReadFile("outer.zip"); err != nil || !bytes.Equal(got, body) {
		t.Fatal("expired source changed", err)
	}
}

func nestedRARSetFixture(t *testing.T, version int, old bool) ([]byte, []string, []byte) {
	t.Helper()
	fixture := rar5SetFixture
	if version == 4 {
		fixture = rar4SetFixture
	}
	var names []string
	var parts [][]byte
	var whole []byte
	if old {
		_, _, names, parts, whole = oldRARSetFixture(t, version, 3)
	} else {
		_, _, names, parts, whole = fixture(t, 3)
	}
	var entries []member
	for i := len(names) - 1; i >= 0; i-- {
		entries = append(entries, member{name: "shelf/" + names[i], body: parts[i]})
	}
	return treeZIP(t, entries...), names, whole
}

func zipMembersForNestedTest(t *testing.T, body []byte) []member {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	var entries []member
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(r)
		closeErr := r.Close()
		if err != nil || closeErr != nil {
			t.Fatal(err, closeErr)
		}
		entries = append(entries, member{name: f.Name, body: b})
	}
	return entries
}

func TestNestedVolumeUncertainGroupsStayUnextractedAndCountEachOriginalOnce(t *testing.T) {
	for _, mutation := range []string{"missing middle", "missing first", "singleton first", "orphan", "duplicate", "overlap", "unsafe path", "outer CRC"} {
		t.Run(mutation, func(t *testing.T) {
			body, _, _ := nestedRARSetFixture(t, 5, false)
			entries := zipMembersForNestedTest(t, body)
			switch mutation {
			case "missing middle":
				entries = []member{entries[0], entries[2]}
			case "missing first":
				entries = entries[:2]
			case "singleton first":
				entries = entries[2:]
			case "orphan":
				entries = []member{{name: "book.r00", body: entries[0].body}}
			case "duplicate":
				entries = append(entries, entries[0])
			case "overlap":
				entries = []member{{name: "a.part1.rar", body: entries[2].body}, {name: "a.part2.rar", body: entries[1].body}, {name: "a.part1.r00", body: entries[0].body}}
			case "unsafe path":
				entries[1].name = "./" + entries[1].name
			}
			body = treeZIP(t, entries...)
			if mutation == "outer CRC" {
				z, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
				if err != nil {
					t.Fatal(err)
				}
				at, err := z.File[1].DataOffset()
				if err != nil {
					t.Fatal(err)
				}
				body[at] ^= 1
			}
			saved := SevenZip
			SevenZip = func() (string, error) { t.Fatal("uncertain child reached reader"); return "", nil }
			t.Cleanup(func() { SevenZip = saved })
			src, dst := treeRoots(t, "outer.zip", body)
			l := oldRARSetLimits(5)
			l.NestedVolumeSets = true
			x, err := ExtractTree(context.Background(), src, []string{"outer.zip"}, dst, books, l)
			if err != nil || x.Complete || x.Whole || x.CoveredVolumeSets != 0 || len(x.Members) != len(entries) {
				t.Fatal("uncertain group lost or duplicated an original", x, err)
			}
			for _, m := range x.Members {
				if m.Staged != "" || m.Disposition == "extracted" {
					t.Fatal("part escaped as content", m)
				}
			}
			if got, err := src.ReadFile("outer.zip"); err != nil || !bytes.Equal(got, body) {
				t.Fatal("uncertain source changed", err)
			}
		})
	}
}

func TestNestedVolumeOptInDoesNotEnableDirectTraversalOrEarlierCapabilities(t *testing.T) {
	for _, tree := range []bool{false, true} {
		t.Run(fmt.Sprint(tree), func(t *testing.T) {
			body, _, _ := nestedRARSetFixture(t, 4, true)
			src, dst := treeRoots(t, "outer.zip", body)
			l := oldRARSetLimits(4)
			if !tree {
				l.NestedVolumeSets = true
			}
			var x Extraction
			var err error
			if tree {
				x, err = ExtractTree(context.Background(), src, []string{"outer.zip"}, dst, books, l)
			} else {
				x, err = Extract(context.Background(), src, "outer.zip", dst, books, l)
			}
			if err != nil || x.CoveredVolumeSets != 0 || len(x.Members) != 3 || (tree && x.Whole) {
				t.Fatal("earlier capability enabled nested sets", x, err)
			}
			for _, m := range x.Members {
				if m.Staged != "" || m.Disposition == "extracted" {
					t.Fatal("direct or unrequested traversal read a part", m)
				}
			}
		})
	}
}

func TestNestedRARSetOptInPreservesExactOriginalChainsAndRootFacts(t *testing.T) {
	for _, version := range []int{4, 5} {
		for _, old := range []bool{false, true} {
			for _, coverage := range []bool{false, true} {
				t.Run(fmt.Sprintf("%d/old=%t/coverage=%t", version, old, coverage), func(t *testing.T) {
					body, names, whole := nestedRARSetFixture(t, version, old)
					src, dst := treeRoots(t, "outer.zip", body)
					l := oldRARSetLimits(version)
					l.Coverage, l.NestedVolumeSets = coverage, true
					x, err := ExtractTree(context.Background(), src, []string{"outer.zip"}, dst, books, l)
					wantCovered := 0
					if coverage {
						wantCovered = 1
					}
					if err != nil || !x.Complete || x.Whole != coverage || x.CoveredVolumeSets != wantCovered || x.RARSetVersion != 0 || x.OldRARSetVersion != 0 || x.RARVersion != 0 || x.RARContainers != 0 || len(x.Members) != 4 {
						t.Fatal("nested set did not stay separate from root facts", x, err)
					}
					var leaves int
					for _, m := range x.Members {
						if m.Disposition != "extracted" {
							if m.Disposition != "nested_archive" || m.Staged != "" {
								t.Fatal("part escaped group bookkeeping", m)
							}
							continue
						}
						leaves++
						if !reflect.DeepEqual(m.Chain, []string{"shelf/" + names[0], "book.epub"}) || strings.Contains(m.Staged, "shelf") {
							t.Fatal("private names replaced original provenance", m)
						}
						got, err := dst.ReadFile(m.Staged)
						if err != nil || !bytes.Equal(got, whole) {
							t.Fatal("nested output identity differs", m, err)
						}
					}
					if leaves != 1 {
						t.Fatal("missing exact leaf", x)
					}
					if got, err := src.ReadFile("outer.zip"); err != nil || !bytes.Equal(got, body) {
						t.Fatal("source changed", err)
					}
				})
			}
		}
	}
}
