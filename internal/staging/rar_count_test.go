package staging

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestRARContainerCountIsMeasuredAndSeparateFromTreeResults(t *testing.T) {
	for _, version := range []int{4, 5} {
		for _, mode := range []string{"empty", "mixed", "default", "output_failure", "unproven_sibling"} {
			t.Run(fmt.Sprintf("%d/%s", version, mode), func(t *testing.T) {
				child := coverageRAR(version)
				wantCount := 1
				var direct []member
				if mode == "empty" {
					direct = []member{{name: "direct.epub", body: []byte("direct book")}}
				} else {
					child = coverageRAR(version, sevenMember{name: "book.epub", body: []byte("book")})
					if mode == "output_failure" {
						child.members[0].served = []byte("xxxx")
					}
				}
				profiles := []treeProfile{child}
				members := append(direct, member{name: "child.rar", body: child.body})
				if mode == "unproven_sibling" {
					bad := coverageRAR(4, sevenMember{name: "book.epub", body: []byte("book")})
					bad.body = append(bad.body, []byte("unheld tail")...)
					profiles = append(profiles, bad)
					members = append(members, member{name: "bad.rar", body: bad.body})
				}
				treeProfiles(t, profiles...)
				body := treeZIP(t, members...)
				src, dst := treeRoots(t, "outer.zip", body)
				limits := nestedRARLimits()
				if mode == "default" {
					limits = Defaults()
					wantCount = 0
				}
				x, err := ExtractTree(context.Background(), src, []string{"outer.zip"}, dst, books, limits)
				if err != nil || x.RARContainers != wantCount || x.RARVersion != 0 {
					t.Fatal("container evidence came from names or an unproven child", x, err)
				}
				if mode == "empty" && (!x.Whole || !x.Complete || len(x.Members) != 2) {
					t.Fatal("empty RAR was lost", x)
				}
				if mode == "unproven_sibling" && (x.Whole || x.Complete) {
					t.Fatal("partial counts granted whole-tree evidence", x)
				}
				if mode == "output_failure" && (!x.Whole || x.Complete) {
					t.Fatal("container counts erased output failure", x)
				}
				encoded, err := json.Marshal(x)
				if err != nil || bytes.Contains(bytes.ToLower(encoded), []byte("rarcontainers")) || strings.Contains(string(encoded), "rar_containers") {
					t.Fatal("internal count escaped JSON", string(encoded), err)
				}
			})
		}
	}
}
