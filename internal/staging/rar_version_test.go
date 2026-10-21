package staging

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

func TestRARVersionNamesOnlyTheMeasuredOuterSnapshot(t *testing.T) {
	for _, version := range []int{4, 5} {
		for _, nested := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/nested_%t", version, nested), func(t *testing.T) {
				kind, data := "-tRar", rar4Archive(rar4Book("book.epub"))
				if version == 5 {
					kind, data = "-tRar5", rar5Archive(rar5Book("book.epub"))
				}
				treeProfiles(t, treeProfile{body: data, kind: kind, members: []sevenMember{{name: "book.epub", body: []byte("synthetic book"), packed: "14"}}})
				name, body := "outer.rar", data
				if nested {
					name, body = "outer.zip", treeZIP(t, member{name: "child.rar", body: data})
				}
				src, dst := treeRoots(t, name, body)
				limits := Defaults()
				limits.Coverage, limits.RAR4Coverage = !nested, true
				x, err := ExtractTree(context.Background(), src, []string{name}, dst, books, limits)
				want := version
				if nested {
					want = 0
				}
				if err != nil || !x.Complete || x.RARVersion != want || x.Whole == nested {
					t.Fatal(x, err)
				}
				encoded, err := json.Marshal(x)
				if err != nil || bytes.Contains(bytes.ToLower(encoded), []byte("version")) {
					t.Fatal("internal signature fact escaped JSON", string(encoded), err)
				}
			})
		}
	}
}
