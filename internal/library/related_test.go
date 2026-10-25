package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/pdftext/pdftest"
)

// identifiedReceipt is an acquisition receipt for one packet with the given
// source and declared identifiers.
func identifiedReceipt(t *testing.T, body []byte, source, file string, declared [][2]string) []byte {
	t.Helper()
	sum := sha256.Sum256(body)
	ids := []map[string]string{}
	for _, d := range declared {
		ids = append(ids, map[string]string{"field": d[0], "value": d[1]})
	}
	b, err := json.Marshal(map[string]any{
		"schema_version": 1, "complete": true, "status": "untrusted_intake", "antivirus": "not_scanned",
		"transfer": map[string]any{"bytes": len(body), "sha256": hex.EncodeToString(sum[:])},
		"intent":   map[string]any{"request": map[string]any{"source_id": source, "source_file": file}, "selection": map[string]any{"declared_identifiers": ids}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Holdings sharing a declared work, an arXiv paper, or a source item are
// grouped by that evidence alone; invalid and unknown identifiers relate
// nothing, and matching titles relate nothing.
func TestRelatedGroupsOnlyOnStatedIdentifiers(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	packet := func(dir string, body []byte, source, file string, declared ...[2]string) {
		files[dir+"/content.pdf"] = body
		files[dir+"/receipt.json"] = identifiedReceipt(t, body, source, file, declared)
	}
	// Two Archive scans of one work, one also naming a valid ISBN.
	packet("a", pdftest.Pages("", "Scan A."), "archive:scan_a", "a.pdf", [2]string{"openlibrary_work", "OL45804W"}, [2]string{"isbn", "978-0-306-40615-7"}, [2]string{"isbn", "9780306406158"})
	packet("b", pdftest.Pages("", "Scan B."), "archive:scan_b", "b.pdf", [2]string{"openlibrary_work", "OL45804W"}, [2]string{"publisher", "Same"}, [2]string{"isbn", "0306406152"})
	// Two files of one item.
	packet("c", pdftest.Pages("", "Item C one."), "archive:item_c", "one.pdf")
	packet("d", pdftest.Pages("", "Item C two."), "archive:item_c", "two.pdf")
	// Two versions of one paper, and a third paper.
	packet("e", pdftest.Pages("", "v1."), "arxiv:1706.03762v1", "1706.03762v1.pdf")
	packet("f", pdftest.Pages("", "v7."), "arxiv:1706.03762v7", "1706.03762v7.pdf")
	packet("g", pdftest.Pages("", "Other."), "arxiv:2301.01234v1", "2301.01234v1.pdf")
	folder := t.TempDir()
	writeTree(t, folder, files)
	if r, err := Ingest(ctx, IngestRequest{Library: root, Folder: folder, Apply: true}); err != nil || !r.Complete {
		t.Fatal(r, err)
	}
	r, err := Related(ctx, RelatedRequest{Directory: root, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]RelatedGroup{}
	for _, g := range r.Groups {
		got[g.Relation+" "+g.Key] = g
	}
	if len(r.Groups) != 4 || len(got["same_edition isbn:9780306406157"].Members) != 2 {
		t.Fatalf("%+v", r.Groups)
	}
	work := got["same_work openlibrary_work:OL45804W"]
	paper := got["versions_of_one_paper arxiv:1706.03762"]
	item := got["files_of_one_source_item archive:item_c"]
	if len(work.Members) != 2 || len(paper.Members) != 2 || len(item.Members) != 2 {
		t.Fatalf("%+v", r.Groups)
	}
	if paper.Members[0].Version != 1 || paper.Members[1].Version != 7 || r.Groups[0].Relation != RelationSameWork {
		t.Fatalf("%+v", r.Groups)
	}
	// The holding with the valid ISBN records it; the invalid one is dropped.
	control, _, err := openControl(root, false)
	if err != nil {
		t.Fatal(err)
	}
	_, lines, err := lockedManaged(control, r.LibraryID)
	control.close()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, h := range latestList(lines) {
		for _, s := range h.Sources {
			if s.SourceID != "archive:scan_a" {
				continue
			}
			for _, id := range s.Identities {
				ids = append(ids, id.ID)
			}
		}
	}
	if fmt.Sprint(ids) != "[isbn:9780306406157 openlibrary_work:OL45804W]" {
		t.Fatal(ids)
	}
	if k := editionKey("isbn:0306406152"); k != "isbn:9780306406157" {
		t.Fatal(k)
	}
	// Many valid ISBNs do not crowd out the work.
	var many []declaredIdentifier
	for i := 0; i < 30; i++ {
		many = append(many, declaredIdentifier{"isbn", fmt.Sprintf("97803064061%02d", i)})
	}
	kept := sourceIdentities(append(many, declaredIdentifier{"openlibrary_work", "OL45804W"}))
	if len(kept) > maxSourceIdentities || !validSourceIdentities(kept) || kept[len(kept)-1].ID != "openlibrary_work:OL45804W" {
		t.Fatal(kept)
	}
	one, err := Related(ctx, RelatedRequest{Directory: root, Holding: paper.Members[0].Holding, Limit: 100})
	if err != nil || len(one.Groups) != 1 || one.Groups[0].Key != "arxiv:1706.03762" {
		t.Fatal(one, err)
	}
	for _, bad := range []RelatedRequest{{Directory: root, Holding: "nope", Limit: 10}, {Directory: root, Holding: "sha256:" + strings.Repeat("0", 64), Limit: 10}, {Directory: root, Limit: 0}, {Directory: root, Limit: 10, Offset: -1}} {
		if _, err := Related(ctx, bad); err == nil {
			t.Fatal("accepted", bad)
		}
	}
	page, err := Related(ctx, RelatedRequest{Directory: root, Limit: 1, Offset: 2})
	if err != nil || page.Total != 4 || len(page.Groups) != 1 || page.Groups[0].Key != r.Groups[2].Key {
		t.Fatal(page, err)
	}
}

// A stored source identity that does not reclassify to itself fails closed.
func TestSourceIdentitiesFailClosed(t *testing.T) {
	good, ok := classifySourceIdentifier("openlibrary_edition", "OL7353617M")
	if !ok || good.ID != "openlibrary_edition:OL7353617M" || good.Role != "edition" {
		t.Fatal(good, ok)
	}
	forged := good
	forged.ID = "openlibrary_edition:OL1M"
	for _, list := range [][]Identity{{forged}, {good, good}, {{Role: "work", ID: "x", Raw: "x", Scheme: "title", Origin: originSourceMetadata, Confidence: confidenceDeclared}}} {
		if validSourceIdentities(list) {
			t.Fatal("accepted", list)
		}
	}
	for _, bad := range [][2]string{{"isbn", "9780306406158"}, {"openlibrary_work", "OL0W"}, {"openlibrary_work", "OL45804M"}, {"external-identifier", "urn:oclc:123"}, {"isbn", " 9780306406157"}, {"title", "OL45804W"}} {
		if _, ok := classifySourceIdentifier(bad[0], bad[1]); ok {
			t.Fatal("classified", bad)
		}
	}
	if id, ok := classifySourceIdentifier("external-identifier", "urn:isbn:0306406152"); !ok || id.ID != "isbn:0306406152" {
		t.Fatal(id, ok)
	}
}

// Work relationships span distinct declared editions while each edition and
// each set of bytes remains identifiable. Equal titles are not work evidence.
func TestRelatedPreservesDifferentEditionsOfOneDeclaredWork(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	folder := t.TempDir()
	files := map[string][]byte{}
	for i, edition := range []string{"OL1M", "OL1M", "OL2M", "OL2M", ""} {
		item := fmt.Sprintf("edition-%d", i)
		body := pdftest.Pages("Same title", fmt.Sprintf("Distinct edition bytes %d.", i))
		var ids [][2]string
		if edition != "" {
			ids = [][2]string{{"openlibrary_work", "OL1W"}, {"openlibrary_edition", edition}}
		}
		files[item+"/content.pdf"] = body
		files[item+"/receipt.json"] = identifiedReceipt(t, body, "archive:"+item, "book.pdf", ids)
	}
	writeTree(t, folder, files)
	if r, err := Ingest(ctx, IngestRequest{Library: root, Folder: folder, Apply: true}); err != nil || !r.Complete {
		t.Fatal(r, err)
	}
	held, err := FindManaged(ctx, root, "", "all", "all", 10, 0)
	if err != nil || held.Total != 5 {
		t.Fatal("declared relationships merged bytes", held, err)
	}
	related, err := Related(ctx, RelatedRequest{Directory: root, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	groups := map[string]RelatedGroup{}
	for _, group := range related.Groups {
		groups[group.Key] = group
	}
	work, first, second := groups["openlibrary_work:OL1W"], groups["openlibrary_edition:OL1M"], groups["openlibrary_edition:OL2M"]
	if len(work.Members) != 4 || len(first.Members) != 2 || len(second.Members) != 2 || len(related.Groups) != 3 {
		t.Fatal("distinct editions or unsupported equal title related incorrectly", related)
	}
	editionAssets := map[string]bool{}
	for _, member := range first.Members {
		editionAssets[member.Holding] = true
	}
	for _, member := range second.Members {
		if editionAssets[member.Holding] {
			t.Fatal("edition membership collapsed", member)
		}
	}
}
