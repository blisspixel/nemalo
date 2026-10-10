package content

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/blisspixel/nemalo/internal/library"
)

func fixture(t *testing.T, bodies []string, mutate func(map[string]string)) Request {
	t.Helper()
	files := map[string]string{
		"META-INF/container.xml": `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`,
	}
	var manifest, spine strings.Builder
	for i, body := range bodies {
		name := fmt.Sprintf("unit%d.xhtml", i)
		files[name] = "<html><body>" + body + "</body></html>"
		fmt.Fprintf(&manifest, `<item id="u%d" href="%s" media-type="application/xhtml+xml"/>`, i, name)
		fmt.Fprintf(&spine, `<itemref idref="u%d"/>`, i)
	}
	files["book.opf"] = `<package xmlns="http://www.idpf.org/2007/opf"><metadata><title>Fixture</title><language>mul</language></metadata><manifest>` + manifest.String() + `</manifest><spine>` + spine.String() + `</spine></package>`
	if mutate != nil {
		mutate(files)
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	f, err := z.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write([]byte("application/epub+zip")); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		f, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write([]byte(files[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "book.epub"), buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(buf.Bytes())
	hash := hex.EncodeToString(h[:])
	c := library.Catalog{SchemaVersion: 1, ID: "snapshot:" + strings.Repeat("1", 32), CreatedAt: time.Unix(1, 0).UTC(), RootHint: "untrusted-unused-root", Complete: true, Assets: []library.Asset{{ID: "sha256:" + hash, SHA256: hash, Bytes: int64(buf.Len()), Locations: []library.Location{{Path: "book.epub", CandidateKind: "ebook_candidate"}}}}}
	// The fixture catalog is outside the explicit source root, like real snapshots.
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	catalog := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(catalog, b, 0600); err != nil {
		t.Fatal(err)
	}
	return Request{SchemaVersion: 1, Catalog: catalog, Root: root, AssetID: c.Assets[0].ID, MaxBytes: 4}
}

func TestSourceBoundRetrievalOver300Cycles(t *testing.T) {
	ctx := context.Background()
	works := []struct {
		bodies   []string
		expected []string
	}{
		{[]string{strings.Repeat("A &amp; B ", 60), "<p>Chapter two.</p>"}, []string{strings.Repeat("A & B ", 60), "Chapter two.\n"}},
		{[]string{strings.Repeat("日本語😀e&#x301; ", 50), "<pre>line one\n\nline two</pre>"}, []string{strings.Repeat("日本語😀e\u0301 ", 50), "line one\n\nline two\n"}},
		{[]string{"<p>Poem<br/>line<br/><br/>next stanza</p>", strings.Repeat("Savoir. ", 50)}, []string{"Poem\nline\n\nnext stanza\n", strings.Repeat("Savoir. ", 50)}},
	}
	requests := make([]Request, len(works))
	for i, work := range works {
		requests[i] = fixture(t, work.bodies, nil)
	}
	var calls int
	for i, original := range requests {
		req := original
		wantUnit, wantOffset := 0, 0
		gotUnits := make([]string, len(works[i].expected))
		for {
			r, err := Get(ctx, req)
			calls++
			if err != nil {
				t.Fatal(i, r, err)
			}
			if r.Locator == nil || r.Locator.Unit != wantUnit || r.Locator.Start != wantOffset || r.Locator.End != wantOffset+len(r.Text) || len(r.Text) > req.MaxBytes || !utf8.ValidString(r.Text) {
				t.Fatal("incorrect range", r)
			}
			// A retry/peek is byte-identical. Switching to another work cannot
			// mutate this work's continuation or create acknowledgement state.
			again, err := Get(ctx, req)
			calls++
			b1, _ := json.Marshal(r)
			b2, _ := json.Marshal(again)
			if err != nil || !bytes.Equal(b1, b2) {
				t.Fatal("non-deterministic retry", err)
			}
			other := requests[(i+1)%len(requests)]
			if _, err := Get(ctx, other); err != nil {
				t.Fatal(err)
			}
			calls++
			gotUnits[wantUnit] += r.Text
			wantOffset = r.Locator.End
			if r.EndOfUnit {
				if gotUnits[wantUnit] != works[i].expected[wantUnit] {
					t.Fatal("omitted, duplicated, or changed source", i, wantUnit, gotUnits[wantUnit])
				}
				wantUnit++
				wantOffset = 0
			}
			if r.EndOfSource {
				if r.Continuation != "" || wantUnit != len(works[i].expected) {
					t.Fatal("false end of source", r)
				}
				break
			}
			if r.Continuation == "" {
				t.Fatal("cap mistaken for end of source")
			}
			// Serialize and restore the caller-owned reference between calls.
			encoded, _ := json.Marshal(r.Continuation)
			var restored string
			if err := json.Unmarshal(encoded, &restored); err != nil {
				t.Fatal(err)
			}
			req = original
			req.Cursor = restored
		}
		entries, err := os.ReadDir(original.Root)
		if err != nil || len(entries) != 1 || entries[0].Name() != "book.epub" {
			t.Fatal("retrieval wrote state", entries, err)
		}
		listing := original
		listing.UnitsOnly = true
		r, err := Get(ctx, listing)
		if err != nil || r.Status != "units_available" || len(r.Units) != len(works[i].expected) || r.Text != "" || r.Locator != nil || r.Continuation != "" || r.EndOfSource {
			t.Fatal(r, err)
		}
	}
	if calls < 300 {
		t.Fatal("insufficient actual service retrieval cycles", calls)
	}
	t.Logf("%d actual retrieval cycles, including deterministic retries and interleaved works", calls)
}

func TestFailuresNeverReturnSourceOrContinuation(t *testing.T) {
	for _, test := range []struct {
		name, status string
		change       func(*Request)
	}{
		{"missing catalog", "unavailable", func(r *Request) { r.Catalog += ".missing" }},
		{"missing root", "unavailable", func(r *Request) { r.Root += ".missing" }},
		{"missing asset", "missing_asset", func(r *Request) { r.AssetID = "sha256:" + strings.Repeat("0", 64) }},
		{"missing location", "missing_asset", func(r *Request) {
			if err := os.Remove(filepath.Join(r.Root, "book.epub")); err != nil {
				t.Fatal(err)
			}
		}},
		{"changed source", "changed_source", func(r *Request) {
			p := filepath.Join(r.Root, "book.epub")
			b, _ := os.ReadFile(p)
			b[len(b)-1] ^= 1
			if err := os.WriteFile(p, b, 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"changed size", "changed_source", func(r *Request) {
			if err := os.WriteFile(filepath.Join(r.Root, "book.epub"), []byte("different"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"unit range", "invalid_request", func(r *Request) { r.Unit = 10 }},
		{"offset range", "invalid_request", func(r *Request) { r.Offset = 10000 }},
		{"split unicode", "invalid_request", func(r *Request) { r.Offset = 1 }},
		{"bad cursor", "invalid_request", func(r *Request) { r.Cursor = "broken" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := fixture(t, []string{"日本語 text"}, nil)
			test.change(&req)
			r, err := Get(context.Background(), req)
			if err == nil || r.Status != test.status || r.Text != "" || r.Continuation != "" || r.Locator != nil || r.EndOfSource || len(r.Units) != 0 {
				t.Fatal(r, err)
			}
		})
	}
	for _, mutation := range []func(*cursor){
		func(c *cursor) { c.AssetID = "sha256:" + strings.Repeat("0", 64) },
		func(c *cursor) { c.Extractor = "incompatible/2" },
		func(c *cursor) { c.RepresentationID = "sha256:" + strings.Repeat("0", 64) },
	} {
		req := fixture(t, []string{"long enough text"}, nil)
		r, err := Get(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		c, err := parseCursor(r.Continuation)
		if err != nil {
			t.Fatal(err)
		}
		mutation(&c)
		req.Cursor = encodeCursor(c)
		r, err = Get(context.Background(), req)
		if err == nil || r.Status != "stale_reference" || r.Text != "" || r.Continuation != "" {
			t.Fatal(r, err)
		}
	}
	req := fixture(t, []string{"Text"}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := Get(ctx, req)
	if err == nil || r.Status != "cancelled" || r.Text != "" {
		t.Fatal(r, err)
	}
}

func TestUnsupportedAndMalformedDocuments(t *testing.T) {
	for _, test := range []struct{ body, status string }{
		{"<img src=\"cover.png\"/>", "unsupported_extraction"},
		{"<p hidden=\"hidden\">secret</p>", "unsupported_extraction"},
		{"<script>window.x=1</script>", "unsupported_extraction"},
		{"<table><tr><td>cell</td></tr></table>", "unsupported_extraction"},
		{"<p>unclosed", "malformed_content"},
		{"", "unsupported_extraction"},
	} {
		req := fixture(t, []string{test.body}, nil)
		r, err := Get(context.Background(), req)
		if err == nil || r.Status != test.status || r.Text != "" || r.Continuation != "" {
			t.Fatal(test.body, r, err)
		}
	}
	req := fixture(t, []string{"Text"}, func(files map[string]string) { delete(files, "unit0.xhtml") })
	r, err := Get(context.Background(), req)
	if err == nil || r.Status != "malformed_content" {
		t.Fatal(r, err)
	}
	for _, mutate := range []func(map[string]string){
		func(f map[string]string) {
			f["META-INF/container.xml"] = strings.Replace(f["META-INF/container.xml"], "</rootfiles>", `<rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles>`, 1)
		},
		func(f map[string]string) { f["META-INF/encryption.xml"] = "declaration" },
		func(f map[string]string) {
			f["book.opf"] = strings.ReplaceAll(f["book.opf"], `idref="u0"`, `idref="u0" linear="invalid"`)
		},
		func(f map[string]string) {
			f["book.opf"] = strings.ReplaceAll(f["book.opf"], "application/xhtml+xml", "text/html")
		},
	} {
		req := fixture(t, []string{"Text"}, mutate)
		r, err := Get(context.Background(), req)
		if err == nil || r.Status != "unsupported_extraction" {
			t.Fatal(r, err)
		}
	}
}

func TestRequestAndCursorValidation(t *testing.T) {
	base := fixture(t, []string{"Text"}, nil)
	for _, mutate := range []func(*Request){
		func(r *Request) { r.SchemaVersion = 2 },
		func(r *Request) { r.Root = "" }, func(r *Request) { r.Catalog = "" }, func(r *Request) { r.AssetID = "bad" },
		func(r *Request) { r.AssetID = "sha256:" + strings.Repeat("z", 64) }, func(r *Request) { r.AssetID = "sha256:" + strings.Repeat("A", 64) },
		func(r *Request) { r.MaxBytes = 3 }, func(r *Request) { r.MaxBytes = 65537 }, func(r *Request) { r.Unit = -1 }, func(r *Request) { r.Unit = 512 },
		func(r *Request) { r.Offset = -1 }, func(r *Request) { r.Offset = maxText + 1 }, func(r *Request) { r.Cursor = strings.Repeat("x", 2049) },
		func(r *Request) { r.Cursor = "token"; r.Unit = 1 }, func(r *Request) { r.Cursor = "token"; r.UnitsOnly = true },
	} {
		r := base
		mutate(&r)
		if _, err := Get(context.Background(), r); err == nil {
			t.Fatal("accepted invalid request", r)
		}
	}
	for _, token := range []string{"%", encodeCursor(cursor{SchemaVersion: 2, AssetID: "a", Extractor: Extractor, RepresentationID: "b"}), encodeCursor(cursor{SchemaVersion: 1, AssetID: "a", Extractor: Extractor, RepresentationID: "b", Unit: -1}), "e30", "bm90IGpzb24"} {
		if _, err := parseCursor(token); err == nil {
			t.Fatal("accepted invalid cursor", token)
		}
	}
	valid, _ := json.Marshal(cursor{SchemaVersion: 1, AssetID: base.AssetID, Extractor: Extractor, RepresentationID: "sha256:" + strings.Repeat("0", 64)})
	for _, raw := range []string{
		strings.Replace(string(valid), `"schema_version":1`, `"schema_version":1,"schema_version":1`, 1),
		strings.Replace(string(valid), `"schema_version":1`, `"unknown":true,"schema_version":1`, 1),
		string(valid) + " trailing",
		" " + string(valid),
	} {
		if _, err := parseCursor(base64.RawURLEncoding.EncodeToString([]byte(raw))); err == nil {
			t.Fatal("accepted noncanonical cursor", raw)
		}
	}
	diagnostic := diagnosticError{errors.Join(context.Canceled, errors.New(strings.Repeat("日", 3000)))}
	if len(diagnostic.Error()) > 2100 || !utf8.ValidString(diagnostic.Error()) || !errors.Is(diagnostic, context.Canceled) {
		t.Fatal("diagnostic cap lost encoding or cause")
	}
}

func TestTextBoundariesAndLimits(t *testing.T) {
	ctx := context.Background()
	text, err := extractText(ctx, []byte("<html><body><pre>A\r\n\rB e&#x301;</pre><p>C<br/><br/>D</p></body></html>"), 100)
	if err != nil || text != "A\n\nB e\u0301\nC\n\nD\n" {
		t.Fatal(text, err)
	}
	text, err = extractText(ctx, []byte(`<h:html xmlns:h="http://www.w3.org/1999/xhtml"><h:body><h:p>A<![CDATA[<B>]]>C</h:p></h:body></h:html>`), 100)
	if err != nil || text != "A<B>C\n" {
		t.Fatal("CDATA or qualified XHTML text lost", text, err)
	}
	for _, data := range []string{"<body>text", "<body><svg/></body>", "<body><template>hidden</template></body>", "<body><p aria-hidden=\"true\">hidden</p></body>", "<body>text</body><other>omitted</other>", "before<body>text</body>", string([]byte{255})} {
		if _, err := extractText(ctx, []byte(data), 100); err == nil {
			t.Fatal("unsupported text accepted", data)
		}
	}
	if _, err := extractText(ctx, []byte("<body>Long text</body>"), 4); err != errBudget {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := extractText(cancelled, []byte("<body>Text</body>"), 100); err == nil {
		t.Fatal("cancellation ignored")
	}
	if _, _, err := readAsset(ctx, "", "", library.Asset{Bytes: maxInput + 1}); err != errBudget {
		t.Fatal(err)
	}
	if _, _, err := readAsset(ctx, "", "", library.Asset{Bytes: maxManagedInput + 1, Locations: []library.Location{{CandidateKind: "managed_holding"}}}); err != errBudget {
		t.Fatal(err)
	}
	_, status, err := readAsset(ctx, t.TempDir(), "missing", library.Asset{Bytes: maxInput + 1, SHA256: strings.Repeat("ab", 32), Locations: []library.Location{{Path: "missing", CandidateKind: "managed_holding"}}})
	if errors.Is(err, errBudget) || status == "budget_exceeded" {
		t.Fatal("managed holding kept the snapshot byte cap", status, err)
	}
	req := fixture(t, []string{"Text"}, nil)
	target := filepath.Join(req.Root, "book.epub")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(req.Catalog, target); err != nil {
		t.Skip("symlinks unavailable")
	}
	r, err := Get(ctx, req)
	if err == nil || r.Status != "unsupported_extraction" {
		t.Fatal(r, err)
	}
}

func TestUnsupportedUnitsAreNeverSilentlySkipped(t *testing.T) {
	req := fixture(t, []string{"<img src=\"cover.png\"/>", "<p>Supported source.</p>", "<svg/>", "Last text."}, nil)
	req.UnitsOnly = true
	r, err := Get(context.Background(), req)
	if err != nil || len(r.Units) != 4 || len(r.UnsupportedUnits) != 2 || r.UnsupportedUnits[0] != 0 || r.UnsupportedUnits[1] != 2 || r.Units[0].Extraction != "unsupported_document" || r.Units[1].Extraction != "supported_plain_text" {
		t.Fatal(r, err)
	}
	req.UnitsOnly, req.Unit, req.MaxBytes = false, 1, 65536
	r, err = Get(context.Background(), req)
	if err != nil || r.Text != "Supported source.\n" || !r.EndOfUnit || r.EndOfSource || len(r.UnsupportedUnits) != 2 {
		t.Fatal(r, err)
	}
	c, err := parseCursor(r.Continuation)
	if err != nil || c.Unit != 2 || c.Offset != 0 {
		t.Fatal("continuation skipped unsupported source", c, err)
	}
	req.Unit, req.Cursor = 0, r.Continuation
	r, err = Get(context.Background(), req)
	if err == nil || r.Status != "unsupported_extraction" || r.Text != "" || r.Continuation != "" || r.EndOfSource {
		t.Fatal(r, err)
	}
	// Only an explicit caller selection may move beyond an unsupported unit.
	req.Cursor, req.Unit = "", 3
	r, err = Get(context.Background(), req)
	if err != nil || !r.EndOfSource || len(r.UnsupportedUnits) != 2 || r.Text != "Last text." {
		t.Fatal(r, err)
	}
}

func TestUnitBudgetAndNonEPUB(t *testing.T) {
	bodies := make([]string, 513)
	for i := range bodies {
		bodies[i] = "Text"
	}
	req := fixture(t, bodies, nil)
	r, err := Get(context.Background(), req)
	if err == nil || r.Status != "budget_exceeded" || r.Text != "" || r.Continuation != "" {
		t.Fatal(r, err)
	}
	req = fixture(t, []string{strings.Repeat("x", 1<<20+1)}, nil)
	r, err = Get(context.Background(), req)
	if err == nil || r.Status != "budget_exceeded" {
		t.Fatal(r, err)
	}
	req = fixture(t, []string{"Text"}, nil)
	catalog, err := library.Load(req.Catalog)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("%PDF-1.7\n%%EOF")
	hash := sha256.Sum256(data)
	a := &catalog.Assets[0]
	a.SHA256, a.Bytes = hex.EncodeToString(hash[:]), int64(len(data))
	a.ID = "sha256:" + a.SHA256
	encoded, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(req.Catalog, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(req.Root, "book.epub"), data, 0600); err != nil {
		t.Fatal(err)
	}
	req.AssetID = a.ID
	r, err = Get(context.Background(), req)
	if err == nil || r.Status != "unsupported_extraction" {
		t.Fatal(r, err)
	}
}

func FuzzTextExtraction(f *testing.F) {
	for _, seed := range []string{"<html><body>Source 日本語</body></html>", "<body>broken", "<body><script>untrusted</script></body>"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 65536 {
			t.Skip()
		}
		text, err := extractText(context.Background(), []byte(input), 4096)
		if err != nil && text != "" {
			t.Fatal("failed extraction returned text")
		}
		if len(text) > 4096 || !utf8.ValidString(text) {
			t.Fatal("output violates text budget or encoding")
		}
	})
}
