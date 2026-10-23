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

func TestNestedRARSetPhysicalAndReaderFailuresPrecedeLeafExtraction(t *testing.T) {
	for _, version := range []int{4, 5} {
		for _, old := range []bool{false, true} {
			for _, mutation := range []string{"hidden service", "physical CRC", "isolated size", "assembled packed", "output"} {
				t.Run(fmt.Sprintf("%d/%t/%s", version, old, mutation), func(t *testing.T) {
					body, _, whole := nestedRARSetFixture(t, version, old)
					parts := zipMembersForNestedTest(t, body)
					switch mutation {
					case "hidden service", "physical CRC":
						b := bytes.Clone(parts[1].body)
						oldSize := len(b)
						if mutation == "physical CRC" {
							at := 8
							if version == 4 {
								at = 7
							}
							b[at] ^= 1 // Reader claims unchanged facts; the physical checksum must win.
						} else {
							end := len(b) - len(rar5Record(5, 0, rar5Numbers(1), nil))
							service := rar5Record(3, 2, rar5FileBody("service", []byte("keep"), 4, 0100644, 0, 1), []byte("keep"))
							if version == 4 {
								end = len(b) - 16
								service = rar4Block(0x7a, 0x8000, []byte{4, 0, 0, 0}, []byte("keep"))
							}
							b = append(append(bytes.Clone(b[:end]), service...), b[end:]...)
						}
						parts[1].body = b
						replaceRARFixture(t, "volume-0001.header", fmt.Sprintf("Physical Size = %d\n", oldSize), fmt.Sprintf("Physical Size = %d\n", len(b)))
					case "isolated size":
						replaceRARFixture(t, "volume-0001.listing", fmt.Sprintf("Size = %d\n", len(whole)), fmt.Sprintf("Size = %d\n", len(whole)+1))
					case "assembled packed":
						replaceRARFixture(t, "listing.txt", fmt.Sprintf("Packed Size = %d\n", len(whole)), "Packed Size = 1\n")
					case "output":
						writeFakeRARFile(t, fmt.Sprintf("members/%x", "book.epub"), "wrong bytes")
					}
					writeFakeRARFile(t, "trace-enabled", "")
					body = treeZIP(t, parts...)
					src, dst := treeRoots(t, "outer.zip", body)
					x, err := ExtractTree(context.Background(), src, []string{"outer.zip"}, dst, books, nestedAllSetLimits())
					if err != nil || x.Complete || x.RARVersion != 0 || x.RARSetVersion != 0 || x.OldRARSetVersion != 0 || x.RARContainers != 0 {
						t.Fatal("failed nested group gained another fact", x, err)
					}
					if mutation == "output" {
						if !x.Whole || x.CoveredVolumeSets != 1 || len(x.Members) != 4 || x.Members[3].Disposition != "failed" {
							t.Fatal("output failure erased or promoted physical evidence", x)
						}
					} else {
						if x.Whole || x.CoveredVolumeSets != 0 || len(x.Members) != 3 {
							t.Fatal("incomplete proof supplied evidence or output", x)
						}
						trace, err := os.ReadFile(filepath.Join(os.Getenv("NEMALO_FAKE_7Z"), "trace"))
						if err != nil || strings.Contains("\n"+string(trace), "\ne ") {
							t.Fatal("leaf extraction preceded complete binding", string(trace), err)
						}
					}
					if got, err := src.ReadFile("outer.zip"); err != nil || !bytes.Equal(got, body) {
						t.Fatal("failed source changed", err)
					}
				})
			}
		}
	}
}
