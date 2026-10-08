package discovery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func archiveProvider(t *testing.T, handler http.HandlerFunc) *Archive {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	p := NewArchive()
	p.endpoint, p.client = server.URL, server.Client()
	return p
}

func TestArchiveSearchAndOffset(t *testing.T) {
	p := archiveProvider(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/advancedsearch.php" || q.Get("start") != "2" || q.Get("rows") != "2" || q.Get("sort[]") != "identifier asc" || !strings.Contains(q.Get("q"), "(mediatype:texts OR mediatype:audio)") || !strings.Contains(q.Get("q"), "Verne & ocean") {
			t.Error("incorrect search contract", r.URL)
		}
		_, _ = io.WriteString(w, `{"responseHeader":{"status":0},"response":{"numFound":4,"start":2,"docs":[{"identifier":"Demo-1","title":"Voyage","creator":"Verne","language":["fre"],"mediatype":"texts"},{"identifier":"Demo-2","title":"Audio","mediatype":"audio"}]}}`)
	})
	page, err := p.Search(context.Background(), "Verne & ocean", 2, 2)
	if err != nil || page.Total != 4 || len(page.Results) != 2 || page.Results[0].ID != "archive:Demo-1" || page.Results[0].Access != "discovery_only" || page.Results[1].Languages == nil {
		t.Fatal(page, err)
	}
}

func TestArchiveSearchRejectsInvalidEvidence(t *testing.T) {
	for _, body := range []string{`{}`, `{"responseHeader":{"status":0},"response":{"numFound":0,"start":0,"docs":[]}}`, `{"responseHeader":{"status":1},"response":{"numFound":1,"start":0,"docs":[]}}`, `{"responseHeader":{"status":0},"response":{"numFound":1,"start":1,"docs":[]}}`, `{"responseHeader":{"status":0},"response":{"numFound":1,"start":0,"docs":[{"identifier":"../x","title":"X","mediatype":"texts"}]}}`, `{"responseHeader":{"status":0},"response":{"numFound":1,"start":0,"docs":[{"identifier":"demo","title":"X","mediatype":"software"}]}}`} {
		p := archiveProvider(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) })
		page, err := p.Search(context.Background(), "query", 1, 0)
		validEmpty := strings.Contains(body, `"numFound":0`)
		if (err == nil) != validEmpty || (validEmpty && page.Results == nil) {
			t.Fatal(body, page, err)
		}
	}
	p := NewArchive()
	for _, tc := range []struct {
		q             string
		limit, offset int
	}{{"", 1, 0}, {strings.Repeat("x", 1001), 1, 0}, {"x", 51, 0}, {"x", 1, -1}, {"x", 1, 10001}} {
		if _, err := p.Search(context.Background(), tc.q, tc.limit, tc.offset); err == nil {
			t.Fatal("bad input accepted")
		}
	}
}

func itemFixture() map[string]any {
	return map[string]any{"metadata": map[string]any{"identifier": "demo", "title": "Knowledge", "creator": []string{"Author"}, "language": "ja", "mediatype": "texts", "date": "1900", "publisher": "Publisher", "licenseurl": "https://creativecommons.org/licenses/by/4.0/", "rights": "Source declaration"}, "files": []any{
		map[string]any{"name": "folder/a book#1.epub", "format": "EPUB", "size": "100", "md5": strings.Repeat("a", 32), "sha1": strings.Repeat("B", 40)},
		map[string]any{"name": "private.pdf", "private": "true", "size": 200},
		map[string]any{"name": "loan.acsm", "size": "broken"},
	}}
}

func evaluateFixture(t *testing.T, data any) (Evaluation, error) {
	t.Helper()
	p := archiveProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metadata/demo" || r.URL.Query().Get("extended_err") != "1" {
			t.Error("bad metadata request", r.URL)
		}
		_ = json.NewEncoder(w).Encode(data)
	})
	return p.Evaluate(context.Background(), "archive:demo")
}

func TestArchiveFileEvaluationAndRights(t *testing.T) {
	e, err := evaluateFixture(t, itemFixture())
	if err != nil || !e.Complete || e.Access != "no_item_restriction_declared" || len(e.Files) != 3 || e.MetadataSHA256 == "" || len(e.Rights) != 1 || e.Languages[0] != "ja" {
		t.Fatal(e, err)
	}
	f := e.Files[0]
	if f.DownloadURL != "https://archive.org/download/demo/folder/a%20book%231.epub" || f.Access != "public_file_candidate" || *f.Bytes != 100 || f.SHA1 != strings.Repeat("b", 40) {
		t.Fatal(f)
	}
	if e.Files[1].DownloadURL != "" || e.Files[1].Access != "restricted_or_uncertain" || e.Files[2].DownloadURL != "" || e.Files[2].Access != "license_document_only" || e.Files[2].Bytes != nil {
		t.Fatal("restricted/unknown evidence overpromised", e.Files)
	}
	if e.Restrictions["access-restricted-item"] != "not_declared" {
		t.Fatal(e.Restrictions)
	}
	if f.RightsStatus != "not_resolved_at_file_level" || f.DRMStatus != "not_assessed" || f.PrivateState != "not_declared" {
		t.Fatal("missing file uncertainty", f)
	}
}

func TestArchiveRestrictionFlagsFailClosed(t *testing.T) {
	for _, field := range []string{"access-restricted-item", "access-restricted", "is_lending_required", "lending___status", "is_dark", "is_restricted"} {
		for _, value := range []any{true, "true", nil, []string{"false"}, map[string]any{"new": "state"}} {
			x := itemFixture()
			if field == "is_dark" || field == "is_restricted" {
				x[field] = value
			} else {
				x["metadata"].(map[string]any)[field] = value
			}
			e, err := evaluateFixture(t, x)
			if err != nil || e.Access != "restricted_or_uncertain" {
				t.Fatal(field, value, e, err)
			}
			for _, f := range e.Files {
				if f.DownloadURL != "" {
					t.Fatal("uncertain item offered a download", field, value)
				}
			}
		}
	}
	for _, value := range []any{false, "false", 0, "0"} {
		x := itemFixture()
		x["metadata"].(map[string]any)["access-restricted-item"] = value
		e, err := evaluateFixture(t, x)
		if err != nil || e.Restrictions["access-restricted-item"] != "false" {
			t.Fatal(value, e, err)
		}
	}
}

func TestArchiveMalformedAndUnsafeMetadata(t *testing.T) {
	for _, mutate := range []func(map[string]any){
		func(x map[string]any) { x["metadata"].(map[string]any)["identifier"] = "another" },
		func(x map[string]any) { x["metadata"].(map[string]any)["title"] = nil },
		func(x map[string]any) { x["metadata"].(map[string]any)["language"] = 42 },
		func(x map[string]any) { delete(x, "files") }, func(x map[string]any) { x["files"] = nil },
		func(x map[string]any) { x["error"] = "unavailable" },
		func(x map[string]any) { x["files"].([]any)[0].(map[string]any)["name"] = "../escape.epub" },
		func(x map[string]any) { x["files"].([]any)[0].(map[string]any)["name"] = `C:\escape.epub` },
		func(x map[string]any) { x["files"].([]any)[1] = x["files"].([]any)[0] },
	} {
		x := itemFixture()
		mutate(x)
		if e, err := evaluateFixture(t, x); err == nil || e.Complete {
			t.Fatal("malformed metadata accepted", e)
		}
	}
	for _, value := range []any{[]any{}, map[string]any{}, nil} {
		if _, err := evaluateFixture(t, value); err == nil {
			t.Fatal("missing record accepted", value)
		}
	}
	for _, id := range []string{"demo", "archive:", "archive:../x", "archive:_x", "archive:" + strings.Repeat("x", 101), "archive:x?query", "archive:x/y", "archive:é"} {
		if ValidArchiveID(id) {
			t.Fatal("bad ID permitted", id)
		}
		if _, err := NewArchive().Evaluate(context.Background(), id); err == nil {
			t.Fatal("bad lookup accepted", id)
		}
	}
}

func TestSourceSizeChecksumsAndCandidates(t *testing.T) {
	for _, raw := range []string{`"100"`, `100`, `0`, `-1`, `1.5`, `null`, `true`, `"bad"`, `9223372036854775808`} {
		n, ok := sourceSize(json.RawMessage(raw))
		want := raw == `"100"` || raw == `100` || raw == `0`
		if ok != want || (ok && n < 0) {
			t.Fatal(raw, n, ok)
		}
	}
	for _, name := range []string{"x.epub", "x.pdf", "x.mp3", "x.m4b", "x.zip", "x.xml"} {
		if offeredKind(name) == "" {
			t.Fatal(name)
		}
	}
	x := itemFixture()
	f := x["files"].([]any)[0].(map[string]any)
	f["md5"] = "bad"
	f["sha1"] = "bad"
	e, err := evaluateFixture(t, x)
	if err != nil || e.Files[0].MD5 != "" || e.Files[0].SHA1 != "" || len(e.Files[0].Findings) != 2 {
		t.Fatal(e, err)
	}
}
