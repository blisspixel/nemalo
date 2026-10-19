package content

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzNavigationLists reads arbitrary navigation documents as a table of
// contents and as a page list. Neither may panic, exceed its limits, invent
// labels over 256 bytes, or vary between two reads of the same bytes.
func FuzzNavigationLists(f *testing.F) {
	for _, seed := range []string{
		navDocument(`<li><a href="a.xhtml#x">One</a><ol><li><a href="a.xhtml">Two</a></li></ol></li>`),
		`<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><body><nav epub:type="page-list" hidden=""><ol><li><a href="a.xhtml#p1">iv</a></li></ol></nav></body></html>`,
		`<nav xmlns:epub="http://www.idpf.org/2007/ops" epub:type="toc"><li><span>x</span></li></nav><nav epub:type="toc"/>`,
		`<!DOCTYPE x [<!ENTITY a "b">]><nav>&a;</nav>`,
		"\xff\xfe",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, doc string) {
		if len(doc) > 1<<16 {
			t.Skip()
		}
		for _, kind := range []string{"toc", "page-list"} {
			entries, hidden, navs, err := parseNavList(context.Background(), []byte(doc), kind)
			again, hidden2, navs2, err2 := parseNavList(context.Background(), []byte(doc), kind)
			if (err == nil) != (err2 == nil) || hidden != hidden2 || navs != navs2 || len(entries) != len(again) {
				t.Fatal("non-deterministic navigation read")
			}
			if err != nil {
				continue
			}
			if len(entries) > maxNavEntries || (kind == "toc" && navs > 1) {
				t.Fatal("limits not kept", len(entries), navs)
			}
			for i, e := range entries {
				if len(e.Label) > maxNavLabel || len(e.Href) > maxNavHref || !utf8.ValidString(e.Label) || e.Order != i || e.Level < 1 || e.Level > maxNavDepth {
					t.Fatalf("invalid entry %#v", e)
				}
				if e.Parent != nil && (*e.Parent < 0 || *e.Parent >= i) {
					t.Fatal("parent after its child", e)
				}
				if again[i].Label != e.Label || again[i].Href != e.Href {
					t.Fatal("non-deterministic entry")
				}
			}
		}
	})
}

// FuzzCursorDecoding accepts only canonical cursors: any token that parses
// encodes back to itself, and none carries an out-of-range position.
func FuzzCursorDecoding(f *testing.F) {
	f.Add(encodeCursor(cursor{SchemaVersion: 1, AssetID: "sha256:" + strings.Repeat("a", 64), Extractor: TextExtractorV2, RepresentationID: "sha256:" + strings.Repeat("b", 64), Unit: 3, Offset: 9, Representation: "epub-text/2"}))
	f.Add("eyJzY2hlbWFfdmVyc2lvbiI6MX0")
	f.Add("not a cursor")
	f.Add("")
	f.Fuzz(func(t *testing.T, token string) {
		if len(token) > 4096 {
			t.Skip()
		}
		c, err := parseCursor(token)
		if err != nil {
			return
		}
		if encodeCursor(c) != token {
			t.Fatal("accepted a non-canonical cursor", token)
		}
		if c.SchemaVersion != 1 || c.Unit < 0 || c.Unit >= maxRequestUnit || c.Offset < 0 || c.Offset > maxText {
			t.Fatal("accepted an out-of-range cursor", c)
		}
	})
}
