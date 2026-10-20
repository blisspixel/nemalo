package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeDiffCollection(t *testing.T, path, name string, items ...CollectionItem) []byte {
	t.Helper()
	b, err := json.Marshal(Collection{SchemaVersion: 1, Name: name, Items: items})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCollectionDiffKeepsSelectionNotesAndOrderSeparate(t *testing.T) {
	dir := t.TempDir()
	before, after := filepath.Join(dir, "before.json"), filepath.Join(dir, "after.json")
	book := CollectionItem{Source: "archive:book", File: "book.epub", Note: "Keep this edition"}
	reading := CollectionItem{Source: "archive:reading", Recording: "64Kbps MP3"}
	oldPaper := CollectionItem{Source: "arxiv:1706.03762v1", File: "1706.03762v1.pdf"}
	classic := CollectionItem{Source: "gutenberg:84", File: "84.epub.noimages"}
	newPaper := CollectionItem{Source: "arxiv:1706.03762v2", File: "1706.03762v2.pdf"}
	encoding := CollectionItem{Source: "archive:reading", Recording: "VBR MP3", Note: "Another encoding"}
	oldBytes := writeDiffCollection(t, before, "Before", book, reading, oldPaper, classic)
	reading.Note = "New listening note"
	nextBytes := writeDiffCollection(t, after, "After", reading, book, newPaper, encoding)
	r, err := CollectionDiff(context.Background(), before, after)
	if err != nil || !r.Complete || !r.Changed || !r.NameChanged || r.Before.Path != before || r.After.Path != after || r.Before.Name != "Before" || r.After.Name != "After" || r.Before.Items != 4 || r.After.Items != 4 {
		t.Fatal(r, err)
	}
	for _, input := range []struct {
		path string
		data []byte
		rev  CollectionRevision
	}{{before, oldBytes, r.Before}, {after, nextBytes, r.After}} {
		sum := sha256.Sum256(input.data)
		if input.rev.SHA256 != hex.EncodeToString(sum[:]) {
			t.Fatal("revision hash does not identify the bytes read", input.rev)
		}
		got, err := os.ReadFile(input.path)
		if err != nil || string(got) != string(input.data) {
			t.Fatal("manifest changed", err)
		}
	}
	if !reflect.DeepEqual(r.Added, []CollectionPosition{{Position: 3, Item: newPaper}, {Position: 4, Item: encoding}}) || !reflect.DeepEqual(r.Removed, []CollectionPosition{{Position: 3, Item: oldPaper}, {Position: 4, Item: classic}}) {
		t.Fatal("versions and encodings were not exact additions/removals", r)
	}
	if !reflect.DeepEqual(r.NotesChanged, []CollectionNoteChange{{Item: reading, BeforePosition: 2, AfterPosition: 1}}) || !reflect.DeepEqual(r.RetainedOrder, []CollectionOrderEntry{{Item: reading, BeforePosition: 2, AfterPosition: 1}, {Item: book, BeforePosition: 1, AfterPosition: 2}}) {
		t.Fatal("retained notes and order", r)
	}
	for range 4 {
		again, err := CollectionDiff(context.Background(), before, after)
		if err != nil || !reflect.DeepEqual(r, again) {
			t.Fatal("comparison is not repeatable", again, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatal("comparison wrote files", entries, err)
	}
}

func TestCollectionDiffFormattingNotesAndPositionShifts(t *testing.T) {
	a := CollectionItem{Source: "archive:a", File: "a.epub"}
	b := CollectionItem{Source: "archive:b", File: "b.epub", Note: "Old note"}
	c := CollectionItem{Source: "archive:c", File: "c.epub"}
	for _, test := range []struct {
		name    string
		before  []CollectionItem
		after   []CollectionItem
		added   int
		removed int
		notes   int
		changed bool
	}{
		{"identical", []CollectionItem{a, b}, []CollectionItem{a, b}, 0, 0, 0, false},
		{"insert", []CollectionItem{a, b}, []CollectionItem{c, a, b}, 1, 0, 0, true},
		{"remove", []CollectionItem{c, a, b}, []CollectionItem{a, b}, 0, 1, 0, true},
		{"replace", []CollectionItem{c, b}, []CollectionItem{a, b}, 1, 1, 0, true},
		{"disjoint", []CollectionItem{a}, []CollectionItem{b, c}, 2, 1, 0, true},
		{"note removed", []CollectionItem{b}, []CollectionItem{{Source: b.Source, File: b.File}}, 0, 0, 1, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			before, after := filepath.Join(dir, "a.json"), filepath.Join(dir, "b.json")
			writeDiffCollection(t, before, "Shelf", test.before...)
			writeDiffCollection(t, after, "Shelf", test.after...)
			r, err := CollectionDiff(context.Background(), before, after)
			if err != nil || !r.Complete || r.Changed != test.changed || r.NameChanged || len(r.Added) != test.added || len(r.Removed) != test.removed || len(r.NotesChanged) != test.notes || len(r.RetainedOrder) != 0 {
				t.Fatal(r, err)
			}
			if test.notes == 1 && (r.NotesChanged[0].BeforeNote != b.Note || r.NotesChanged[0].Item.Note != "") {
				t.Fatal("removed note lost", r)
			}
		})
	}
	dir := t.TempDir()
	before, after := filepath.Join(dir, "a.json"), filepath.Join(dir, "b.json")
	writeDiffCollection(t, before, "Shelf", a, b)
	formatted, err := json.MarshalIndent(Collection{SchemaVersion: 1, Name: "Shelf", Items: []CollectionItem{a, b}}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(after, formatted, 0600); err != nil {
		t.Fatal(err)
	}
	r, err := CollectionDiff(context.Background(), before, after)
	if err != nil || !r.Complete || r.Changed || r.Before.SHA256 == r.After.SHA256 {
		t.Fatal("formatting mistaken for a declaration change", r, err)
	}
}

func TestCollectionDiffDoesNotUsePacketNamesAsIdentity(t *testing.T) {
	// These distinct exact selections collide in the legacy packet name's
	// 32-bit digest. Each manifest is valid; they must still compare differently.
	a := CollectionItem{Source: "archive:demo", File: "book-70653.pdf"}
	b := CollectionItem{Source: "archive:demo", File: "book-134697.pdf"}
	if a.Packet() != b.Packet() {
		t.Fatal("fixture no longer collides", a.Packet(), b.Packet())
	}
	dir := t.TempDir()
	before, after := filepath.Join(dir, "before.json"), filepath.Join(dir, "after.json")
	writeDiffCollection(t, before, "Shelf", a)
	writeDiffCollection(t, after, "Shelf", b)
	r, err := CollectionDiff(context.Background(), before, after)
	if err != nil || !r.Complete || !r.Changed || len(r.Added) != 1 || r.Added[0].Item != b || len(r.Removed) != 1 || r.Removed[0].Item != a || len(r.NotesChanged)+len(r.RetainedOrder) != 0 {
		t.Fatal("packet collision hid a selection change", r, err)
	}
}

func TestCollectionDiffEnforcesTheItemBoundOnBothRevisions(t *testing.T) {
	dir := t.TempDir()
	before, after := filepath.Join(dir, "before.json"), filepath.Join(dir, "after.json")
	items := make([]CollectionItem, maxCollectionItems)
	for i := range items {
		items[i] = CollectionItem{Source: "archive:demo", File: fmt.Sprintf("part-%d.pdf", i)}
	}
	writeDiffCollection(t, before, "Shelf", items...)
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	writeDiffCollection(t, after, "Shelf", items...)
	r, err := CollectionDiff(context.Background(), before, after)
	if err != nil || !r.Complete || len(r.RetainedOrder) != maxCollectionItems || len(r.Added)+len(r.Removed) != 0 {
		t.Fatal("maximum valid revisions", r, err)
	}
	tooMany := append(items, CollectionItem{Source: "archive:demo", File: "extra.pdf"})
	invalid := filepath.Join(dir, "invalid.json")
	writeDiffCollection(t, invalid, "Shelf", tooMany...)
	for _, paths := range [][2]string{{invalid, after}, {before, invalid}} {
		r, err := CollectionDiff(context.Background(), paths[0], paths[1])
		if err == nil || r.Complete || r.Before.SHA256 != "" || r.After.SHA256 != "" || len(r.RetainedOrder) != 0 {
			t.Fatal("oversized item count returned comparison evidence", r, err)
		}
	}
}

func TestCollectionDiffRefusesInvalidInputsWithoutPartialEvidence(t *testing.T) {
	dir := t.TempDir()
	good, bad := filepath.Join(dir, "good.json"), filepath.Join(dir, "bad.json")
	valid := writeDiffCollection(t, good, "Shelf", CollectionItem{Source: "archive:demo", File: "book.epub"})
	for name, body := range map[string]string{
		"unknown field":       `{"schema_version":1,"name":"Shelf","items":[{"source":"archive:demo","file":"book.epub"}],"fetch":true}`,
		"duplicate key":       `{"schema_version":1,"name":"Shelf","name":"Other","items":[{"source":"archive:demo","file":"book.epub"}]}`,
		"duplicate selection": `{"schema_version":1,"name":"Shelf","items":[{"source":"archive:demo","file":"book.epub"},{"source":"archive:demo","file":"book.epub","note":"other"}]}`,
		"schema":              `{"schema_version":2,"name":"Shelf","items":[{"source":"archive:demo","file":"book.epub"}]}`,
		"trailing":            string(valid) + `]`,
		"oversized":           strings.Repeat(" ", maxCollectionBytes+1),
		"nested":              strings.Repeat("[", 18) + "0" + strings.Repeat("]", 18),
		"empty":               "",
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(bad, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			for _, paths := range [][2]string{{bad, good}, {good, bad}} {
				r, err := CollectionDiff(context.Background(), paths[0], paths[1])
				if err == nil || r.Complete || r.Changed || r.Before != (CollectionRevision{}) || r.After != (CollectionRevision{}) || len(r.Added)+len(r.Removed)+len(r.NotesChanged)+len(r.RetainedOrder) != 0 {
					t.Fatal("partial or successful invalid comparison", r, err)
				}
			}
		})
	}
	for _, invalid := range []string{dir, filepath.Join(dir, "missing.json")} {
		if r, err := CollectionDiff(context.Background(), good, invalid); err == nil || r.Complete {
			t.Fatal("non-file compared successfully", r, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r, err := CollectionDiff(ctx, "absent-before", "absent-after"); !errors.Is(err, context.Canceled) || r.Complete || r.Before.SHA256 != "" || r.After.SHA256 != "" {
		t.Fatal("cancelled comparison touched or reported inputs", r, err)
	}
}

func TestCollectionReadsBoundRegularFilesAndPermitParentAliases(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "book.json")
	data := writeDiffCollection(t, file, "Shelf", CollectionItem{Source: "archive:demo", File: "book.epub"})
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Skip("directory links unavailable", err)
	}
	c, digest, err := ReadCollection(filepath.Join(alias, "book.json"))
	sum := sha256.Sum256(data)
	if err != nil || c.Name != "Shelf" || digest != hex.EncodeToString(sum[:]) {
		t.Fatal("parent alias refused", c, digest, err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadCollection(link); err == nil {
		t.Fatal("final link was followed by the service")
	}
}
