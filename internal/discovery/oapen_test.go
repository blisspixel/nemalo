package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

const (
	oapenHandle = "oapen:20.500.12657/25287"
	oapenSuffix = "25287"
	oapenPDF    = "4ca2f021-851f-483a-af4e-efd1b86654ba"
	oapenMD5    = "fad59e1f7accb638a009eb242938b54e"
)

type oapenTrip func(*http.Request) (*http.Response, error)

func (f oapenTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func oapenClient(t *testing.T, want int, body string, status int, contentType string) (*http.Client, *[]string) {
	t.Helper()
	var urls []string
	return &http.Client{Transport: oapenTrip(func(r *http.Request) (*http.Response, error) {
		urls = append(urls, r.URL.String())
		if len(urls) > want {
			t.Errorf("unexpected request %s", r.URL)
		}
		if r.Method != http.MethodGet || r.URL.Scheme != "https" || r.URL.Host != OAPENHost {
			t.Errorf("request left the pinned host: %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Accept") != "application/json" || !strings.Contains(r.UserAgent(), "nemalo/") {
			t.Errorf("metadata headers: %v", r.Header)
		}
		h := make(http.Header)
		if contentType != "" {
			h.Set("Content-Type", contentType)
		}
		return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}, &urls
}

func oapenOriginal() map[string]any {
	return map[string]any{
		"uuid": oapenPDF, "name": "9789085551201_OA_version.pdf", "type": "bitstream",
		"bundleName": "ORIGINAL", "mimeType": "application/pdf", "sizeBytes": 9593943,
		"retrieveLink": "/rest/bitstreams/" + oapenPDF + "/retrieve",
		"checkSum":     map[string]any{"value": oapenMD5, "checkSumAlgorithm": "MD5"},
		"code":         "CC-BY-NC",
		"metadata": []any{
			map[string]any{"key": "dc.rights.uri", "value": "http://creativecommons.org/licenses/by-nc/3.0/"},
			map[string]any{"key": "dc.identifier.isbn", "value": "9789085551201"},
		},
	}
}

func oapenBundle(name, bundle, uuid string) map[string]any {
	return map[string]any{
		"uuid": uuid, "name": name, "type": "bitstream", "bundleName": bundle,
		"mimeType": "text/plain", "sizeBytes": 10, "code": "Other open license",
		"retrieveLink": "/rest/bitstreams/" + uuid + "/retrieve",
		"checkSum":     map[string]any{"value": oapenMD5, "checkSumAlgorithm": "MD5"},
		"metadata": []any{
			map[string]any{"key": "dc.rights.uri", "value": "https://publisher.example/not-the-file"},
		},
	}
}

func oapenBody(t *testing.T, mutate func(map[string]any)) string {
	t.Helper()
	item := map[string]any{
		"uuid": "0870af05-e1de-49f2-92fe-8fc1e3f3dec9", "name": "The deliverance of open access books",
		"handle": "20.500.12657/" + oapenSuffix, "type": "item",
		"archived": "true", "withdrawn": "false",
		"metadata": []any{
			map[string]any{"key": "dc.title", "value": "The deliverance of open access books"},
			map[string]any{"key": "dc.contributor.author", "value": "Snijder, Ronald"},
			map[string]any{"key": "dc.language.iso", "value": "English"},
			map[string]any{"key": "dc.language", "value": "eng"},
			map[string]any{"key": "dc.type", "value": "book"},
			map[string]any{"key": "dc.date.issued", "value": "2019"},
			map[string]any{"key": "oapen.identifier.doi", "value": "10.26530/OAPEN_1004809"},
			map[string]any{"key": "oapen.identifier.isbn", "value": "9789085551201"},
			map[string]any{"key": "publisher.name", "value": "Amsterdam University Press"},
			map[string]any{"key": "dc.rights", "value": "info:eu-repo/semantics/openAccess"},
			map[string]any{"key": "dc.identifier.uri", "value": "https://publisher.example/book.pdf"},
		},
		"bitstreams": []any{
			oapenOriginal(),
			oapenBundle("9789085551201_OA_version.pdf.txt", "TEXT", "94dcbd4e-458a-4201-ae6a-c4114b99c5c7"),
			oapenBundle("9789085551201_OA_version.pdf.jpg", "THUMBNAIL", "d1d9cb50-1a1a-4473-be74-0e2ac779ca06"),
			oapenBundle("0870af05-e1de-49f2-92fe-8fc1e3f3dec9.tsv", "EXPORT", "4774d0e7-2c76-49c8-8bd1-6bfdd55be256"),
		},
	}
	if mutate != nil {
		mutate(item)
	}
	b, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func oapenEval(t *testing.T, id, body string, status int, contentType string) (Evaluation, []string, error) {
	t.Helper()
	p := NewOAPEN()
	client, urls := oapenClient(t, 1, body, status, contentType)
	p.SetHTTPClient(client)
	e, err := p.Evaluate(context.Background(), id)
	return e, *urls, err
}

func TestOAPENEvaluateKeepsOriginalDeclarationsOnly(t *testing.T) {
	body := oapenBody(t, nil)
	p := NewOAPEN()
	client, urls := oapenClient(t, 1, body, 200, "application/json")
	p.SetHTTPClient(client)
	var downloaded []string
	p.SetDownloadClient(&http.Client{Transport: oapenTrip(func(r *http.Request) (*http.Response, error) {
		downloaded = append(downloaded, r.URL.String())
		if r.Header.Get("Accept-Encoding") != "identity" || !strings.Contains(r.UserAgent(), "nemalo/") {
			t.Errorf("download headers: %v", r.Header)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("%PDF-1.7")), Request: r}, nil
	})})
	start := time.Now()
	e, err := p.Evaluate(context.Background(), oapenHandle)
	if err != nil || !e.Complete || e.ID != oapenHandle || e.Source != "oapen" || e.Access != OAPENAccess {
		t.Fatal(err, e.Complete, e.ID, e.Access)
	}
	sum := sha256.Sum256([]byte(body))
	if e.MetadataSHA256 != hex.EncodeToString(sum[:]) || e.CheckedAt.IsZero() {
		t.Fatal("metadata was not bound", e.MetadataSHA256, e.CheckedAt)
	}
	if len(*urls) != 1 {
		t.Fatal("requests", *urls)
	}
	got, err := url.Parse((*urls)[0])
	if err != nil || got.Path != "/rest/handle/20.500.12657/25287" || got.Query().Get("expand") != "metadata,bitstreams" || got.Host != OAPENHost {
		t.Fatal((*urls)[0], err)
	}
	if strings.Join(e.Title, "|") != "The deliverance of open access books" || strings.Join(e.Creators, "|") != "Snijder, Ronald" || strings.Join(e.Languages, "|") != "English" || strings.Join(e.MediaType, "|") != "book" || strings.Join(e.Dates, "|") != "2019" || strings.Join(e.Publishers, "|") != "Amsterdam University Press" {
		t.Fatalf("declarations %+v", e)
	}
	if strings.Contains(strings.Join(e.Languages, " "), "eng") {
		t.Fatal("language was normalized", e.Languages)
	}
	if len(e.Files) != 1 || e.Files[0].Name != "9789085551201_OA_version.pdf" || e.Files[0].Access != OAPENOriginalAccess || e.Files[0].Format != "application/pdf" || e.Files[0].MD5 != oapenMD5 || e.Files[0].Bytes == nil || *e.Files[0].Bytes != 9593943 {
		t.Fatalf("file %+v", e.Files)
	}
	if e.Files[0].DownloadURL != oapenRetrieveURL(oapenPDF) || e.Files[0].RightsStatus != "declared code: CC-BY-NC" {
		t.Fatal(e.Files[0].DownloadURL, e.Files[0].RightsStatus)
	}
	joined := strings.Join(e.Files[0].Findings, "\n")
	if !strings.Contains(joined, "dc.rights.uri: http://creativecommons.org/licenses/by-nc/3.0/") || strings.Contains(joined, "publisher.example") || strings.Contains(joined, "TEXT") {
		t.Fatal(joined)
	}
	rights, licenses := strings.Join(e.Rights, "\n"), strings.Join(e.LicenseURLs, "\n")
	if rights != "info:eu-repo/semantics/openAccess" || strings.Contains(licenses, "creativecommons.org") || strings.Contains(rights, "CC-BY-NC") || strings.Contains(licenses, "openAccess") {
		t.Fatal(rights, licenses)
	}
	var doi, isbn bool
	for _, id := range e.Identifiers {
		switch id.Field + "=" + id.Value {
		case "oapen.identifier.doi=10.26530/OAPEN_1004809":
			doi = true
		case "oapen.identifier.isbn=9789085551201":
			isbn = true
		}
		if id.Field == "dc.identifier.isbn" || id.Field == "dc.identifier.doi" || strings.Contains(id.Value, "publisher.example") {
			t.Fatal("bitstream or publisher identifier copied onto the item", id)
		}
	}
	if !doi || !isbn {
		t.Fatal(e.Identifiers)
	}
	if e.Restrictions["archived"] != "true" || e.Restrictions["withdrawn"] != "false" {
		t.Fatal(e.Restrictions)
	}
	if e.OpenAccess != nil {
		t.Fatal("OpenAlex open access used for an OAPEN item")
	}
	resp, err := p.Download(context.Background(), e.Files[0])
	if err != nil || resp.StatusCode != 200 {
		t.Fatal(err, resp)
	}
	resp.Body.Close()
	if len(downloaded) != 1 || downloaded[0] != e.Files[0].DownloadURL {
		t.Fatal(downloaded)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Fatal("metadata and download did not share the one-request-per-second pace")
	}
}

func TestOAPENRefusesAnythingButTheLibraryHandle(t *testing.T) {
	for _, id := range []string{
		"oapen:20.500.12854/26303", "oapen:20.500.12657/", "oapen:20.500.12657/12345678901",
		"oapen:20.500.12657/12/34", "oapen:20.500.12657/../1", "oapen:20.500.12657/1?expand=all",
		"oapen:20.500.12657/1/extra", "OAPEN:20.500.12657/1", "oapen:20.500.12657/25287 ",
		"archive:20.500.12657/1", "oapen:library.oapen.org/20.500.12657/1", "",
	} {
		t.Run(id, func(t *testing.T) {
			e, urls, err := oapenEval(t, id, `{"type":"item"}`, 200, "application/json")
			if err == nil || e.Complete || len(e.Files) != 0 || len(urls) != 0 || ValidOAPENID(id) {
				t.Fatal(err, e.Complete, urls)
			}
		})
	}
	if !ValidOAPENID("oapen:20.500.12657/0") || !ValidOAPENID("oapen:20.500.12657/1234567890") {
		t.Fatal("1 to 10 digits includes these handles")
	}
}

func TestOAPENSuppressesURLsUnlessTheStringFlagsMatch(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"withdrawn string true": func(m map[string]any) { m["withdrawn"] = "true" },
		"withdrawn absent":      func(m map[string]any) { delete(m, "withdrawn") },
		"withdrawn bool":        func(m map[string]any) { m["withdrawn"] = false },
		"withdrawn null":        func(m map[string]any) { m["withdrawn"] = nil },
		"archived string false": func(m map[string]any) { m["archived"] = "false" },
		"archived absent":       func(m map[string]any) { delete(m, "archived") },
		"archived bool true":    func(m map[string]any) { m["archived"] = true },
		"archived other":        func(m map[string]any) { m["archived"] = "yes" },
	} {
		t.Run(name, func(t *testing.T) {
			e, _, err := oapenEval(t, oapenHandle, oapenBody(t, mutate), 200, "application/json")
			if err != nil || !e.Complete || len(e.Files) != 1 || e.Files[0].DownloadURL != "" || e.Files[0].Access == OAPENOriginalAccess {
				t.Fatal(err, e.Complete, e.Files)
			}
			if e.Restrictions["archived"] == "true" && e.Restrictions["withdrawn"] == "false" {
				t.Fatal("flags still offer", e.Restrictions)
			}
		})
	}
}

func TestOAPENOriginalSelectionRefusesAmbiguousOrUnusableFiles(t *testing.T) {
	text := oapenBundle("notes.txt", "TEXT", "94dcbd4e-458a-4201-ae6a-c4114b99c5c7")
	second := oapenOriginal()
	second["uuid"] = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	second["name"] = "other.epub"
	second["mimeType"] = "application/epub+zip"
	second["retrieveLink"] = "/rest/bitstreams/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/retrieve"
	for name, mutate := range map[string]func(map[string]any){
		"no original": func(m map[string]any) { m["bitstreams"] = []any{text} },
		"duplicate name": func(m map[string]any) {
			again := oapenOriginal()
			m["bitstreams"] = []any{oapenOriginal(), again}
		},
		"duplicate uuid": func(m map[string]any) {
			again := oapenOriginal()
			again["name"] = "second.pdf"
			m["bitstreams"] = []any{oapenOriginal(), again}
		},
		"duplicate uuid differs only by case": func(m map[string]any) {
			again := oapenOriginal()
			again["name"] = "second.pdf"
			again["uuid"] = strings.ToUpper(oapenPDF)
			again["retrieveLink"] = "/rest/bitstreams/" + strings.ToUpper(oapenPDF) + "/retrieve"
			m["bitstreams"] = []any{oapenOriginal(), again}
		},
		"lowercase bundle is not original": func(m map[string]any) {
			one := oapenOriginal()
			one["bundleName"] = "original"
			m["bitstreams"] = []any{one}
		},
	} {
		t.Run(name, func(t *testing.T) {
			e, urls, err := oapenEval(t, oapenHandle, oapenBody(t, mutate), 200, "application/json")
			switch name {
			case "no original", "lowercase bundle is not original":
				if err != nil || !e.Complete || len(e.Files) != 0 || len(urls) != 1 {
					t.Fatal(err, e.Complete, e.Files, urls)
				}
			default:
				if err == nil || e.Complete || len(e.Files) != 0 {
					t.Fatal("ambiguous original accepted", err, e.Files)
				}
			}
		})
	}
	e, _, err := oapenEval(t, oapenHandle, oapenBody(t, func(m map[string]any) {
		m["bitstreams"] = []any{oapenOriginal(), second}
	}), 200, "application/json")
	if err != nil || len(e.Files) != 2 || e.Files[0].DownloadURL == "" || e.Files[1].Name != "other.epub" || e.Files[1].Access != OAPENOriginalAccess || !strings.HasSuffix(e.Files[1].DownloadURL, "/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/retrieve") {
		t.Fatal(err, e.Files)
	}
}

func TestOAPENDoesNotOfferAFileItCannotBind(t *testing.T) {
	for name, change := range map[string]func(map[string]any){
		"filename url": func(b map[string]any) {
			b["retrieveLink"] = "https://library.oapen.org/bitstream/20.500.12657/25287/1/9789085551201_OA_version.pdf"
		},
		"other uuid": func(b map[string]any) {
			b["retrieveLink"] = "/rest/bitstreams/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/retrieve"
		},
		"string size": func(b map[string]any) { b["sizeBytes"] = "9593943" },
		"zero size":   func(b map[string]any) { b["sizeBytes"] = 0 },
		"sha1": func(b map[string]any) {
			b["checkSum"] = map[string]any{"value": oapenMD5, "checkSumAlgorithm": "SHA-1"}
		},
		"bad uuid":      func(b map[string]any) { b["uuid"] = "../etc/passwd" },
		"bool code":     func(b map[string]any) { b["code"] = true },
		"unsafe name":   func(b map[string]any) { b["name"] = "../book.pdf" },
		"text name":     func(b map[string]any) { b["name"] = "chapter.txt" },
		"mime clash":    func(b map[string]any) { b["mimeType"] = "application/epub+zip" },
		"not bitstream": func(b map[string]any) { b["type"] = "item" },
		"float size":    func(b map[string]any) { b["sizeBytes"] = 9593943.5 },
		"negative size": func(b map[string]any) { b["sizeBytes"] = -1 },
		"exponent size": func(b map[string]any) { b["sizeBytes"] = json.Number("1e6") },
		"huge size":     func(b map[string]any) { b["sizeBytes"] = json.Number("1" + strings.Repeat("0", 40)) },
		"lower algorithm": func(b map[string]any) {
			b["checkSum"] = map[string]any{"value": strings.ToUpper(oapenMD5), "checkSumAlgorithm": "md5"}
		},
		"missing checksum": func(b map[string]any) { delete(b, "checkSum") },
	} {
		t.Run(name, func(t *testing.T) {
			e, urls, err := oapenEval(t, oapenHandle, oapenBody(t, func(m map[string]any) {
				one := oapenOriginal()
				change(one)
				m["bitstreams"] = []any{one}
			}), 200, "application/json")
			if name == "unsafe name" {
				if err == nil || len(urls) != 1 {
					t.Fatal(err, urls)
				}
				return
			}
			if err != nil || !e.Complete || len(e.Files) != 1 || e.Files[0].DownloadURL != "" || e.Files[0].Access == OAPENOriginalAccess {
				t.Fatal(err, e.Files)
			}
			if name == "mime clash" && e.Files[0].Format != "" {
				t.Fatal("clashing MIME stored", e.Files[0].Format)
			}
			if name == "bool code" && e.Files[0].RightsStatus != "not_declared" {
				t.Fatal(e.Files[0].RightsStatus)
			}
			if strings.Contains(e.Files[0].DownloadURL, "publisher") || strings.Contains(strings.Join(e.Files[0].Findings, " "), "bitstream/20.500") {
				t.Fatal("filename path became a request", e.Files[0])
			}
		})
	}
}

func TestOAPENMetadataFailuresDoNotInventAFile(t *testing.T) {
	for name, tc := range map[string]struct {
		body, ctype string
		status      int
	}{
		"html":         {body: "<html>503</html>", ctype: "text/html", status: 200},
		"not object":   {body: `[{"handle":"20.500.12657/25287"}]`, ctype: "application/json", status: 200},
		"wrong handle": {body: oapenBody(t, func(m map[string]any) { m["handle"] = "20.500.12657/1" }), ctype: "application/json", status: 200},
		"doab handle":  {body: oapenBody(t, func(m map[string]any) { m["handle"] = "20.500.12854/26303" }), ctype: "application/json", status: 200},
		"not item":     {body: oapenBody(t, func(m map[string]any) { m["type"] = "community" }), ctype: "application/json", status: 200},
		"no list":      {body: oapenBody(t, func(m map[string]any) { delete(m, "bitstreams") }), ctype: "application/json", status: 200},
		"null list":    {body: oapenBody(t, func(m map[string]any) { m["bitstreams"] = nil }), ctype: "application/json", status: 200},
		"list object":  {body: oapenBody(t, func(m map[string]any) { m["bitstreams"] = map[string]any{} }), ctype: "application/json", status: 200},
		"no title":     {body: oapenBody(t, func(m map[string]any) { m["name"] = ""; m["metadata"] = []any{} }), ctype: "application/json", status: 200},
		"not found":    {body: `{}`, ctype: "application/json", status: 404},
		"too big":      {body: `{"type":"item","pad":"` + strings.Repeat("a", maxResponse) + `"}`, ctype: "application/json", status: 200},
	} {
		t.Run(name, func(t *testing.T) {
			e, _, err := oapenEval(t, oapenHandle, tc.body, tc.status, tc.ctype)
			if err == nil || e.Complete || len(e.Files) != 0 {
				t.Fatal(err, e.Complete, e.Files)
			}
		})
	}
}

func TestOAPENRetriesATransientMetadataStatusOnce(t *testing.T) {
	var n int
	body := oapenBody(t, nil)
	p := NewOAPEN()
	p.SetHTTPClient(&http.Client{Transport: oapenTrip(func(r *http.Request) (*http.Response, error) {
		n++
		if r.URL.Path != "/rest/handle/20.500.12657/25287" {
			t.Errorf("path %s", r.URL.Path)
		}
		if n == 1 {
			h := make(http.Header)
			h.Set("Content-Type", "text/html")
			return &http.Response{StatusCode: 503, Header: h, Body: io.NopCloser(strings.NewReader("busy")), Request: r}, nil
		}
		h := make(http.Header)
		h.Set("Content-Type", "application/json")
		return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})})
	e, err := p.Evaluate(context.Background(), oapenHandle)
	if err != nil || !e.Complete || n != 2 || len(e.Files) != 1 {
		t.Fatal(err, n, e.Files)
	}
}

func TestOAPENHonorsALongRetryAfterWithoutAnotherRequest(t *testing.T) {
	var n int
	p := NewOAPEN()
	p.SetHTTPClient(&http.Client{Transport: oapenTrip(func(r *http.Request) (*http.Response, error) {
		n++
		h := make(http.Header)
		h.Set("Retry-After", "120")
		h.Set("Content-Type", "application/json")
		return &http.Response{StatusCode: 429, Header: h, Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
	})})
	e, err := p.Evaluate(context.Background(), oapenHandle)
	if err == nil || e.Complete || n != 1 || !strings.Contains(err.Error(), "deadline") {
		t.Fatal(n, err, e.Complete)
	}
}

func TestOAPENDownloadRefusesAURLTheEvaluationDidNotBuild(t *testing.T) {
	p := NewOAPEN()
	n := int64(10)
	good := OfferedFile{Name: "book.pdf", Access: OAPENOriginalAccess, Bytes: &n, MD5: oapenMD5, DownloadURL: oapenRetrieveURL(oapenPDF)}
	if !ValidOAPENOffer(good) {
		t.Fatal("valid offer refused")
	}
	for _, file := range []OfferedFile{
		{Name: "book.pdf", Access: "not_offered", Bytes: &n, MD5: oapenMD5, DownloadURL: good.DownloadURL},
		{Name: "book.pdf", Access: OAPENOriginalAccess, Bytes: &n, MD5: "", DownloadURL: good.DownloadURL},
		{Name: "book.pdf", Access: OAPENOriginalAccess, Bytes: &n, MD5: oapenMD5, DownloadURL: "https://library.oapen.org/bitstream/20.500.12657/25287/1/book.pdf"},
		{Name: "book.pdf", Access: OAPENOriginalAccess, Bytes: &n, MD5: oapenMD5, DownloadURL: "https://directory.doabooks.org/rest/bitstreams/" + oapenPDF + "/retrieve"},
		{Name: "book.pdf", Access: OAPENOriginalAccess, Bytes: &n, MD5: oapenMD5, DownloadURL: oapenRetrieveURL(oapenPDF) + "?x=1"},
		{Name: "../book.pdf", Access: OAPENOriginalAccess, Bytes: &n, MD5: oapenMD5, DownloadURL: good.DownloadURL},
		{Name: "book.pdf", Access: OAPENOriginalAccess, MD5: oapenMD5, DownloadURL: good.DownloadURL},
	} {
		called := false
		p.SetDownloadClient(&http.Client{Transport: oapenTrip(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, errors.New("must not download")
		})})
		if _, err := p.Download(context.Background(), file); err == nil || called {
			t.Fatal("downloaded", file.DownloadURL, err, called)
		}
	}
	p.SetDownloadClient(&http.Client{Transport: oapenTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != good.DownloadURL {
			t.Errorf("url %s", r.URL)
		}
		return &http.Response{StatusCode: 404, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("missing")), Request: r}, nil
	})})
	resp, err := p.Download(context.Background(), good)
	if err != nil || resp.StatusCode != 404 {
		t.Fatal(err, resp)
	}
	resp.Body.Close()
}

func TestNewOAPENPinsTheLibraryHost(t *testing.T) {
	p := NewOAPEN()
	if p.client.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("proxy enabled")
	}
	other, _ := http.NewRequest(http.MethodGet, "https://directory.doabooks.org/rest/handle/20.500.12854/1", nil)
	if err := p.client.CheckRedirect(other, nil); err == nil {
		t.Fatal("DOAB host accepted")
	}
	same, _ := http.NewRequest(http.MethodGet, oapenMetadataURL("1"), nil)
	if err := p.client.CheckRedirect(same, []*http.Request{same}); err != nil {
		t.Fatal(err)
	}
	if err := p.fileClient().CheckRedirect(same, nil); err == nil {
		t.Fatal("bitstream redirect accepted")
	}
	bare := &OAPEN{}
	bare.SetHTTPClient(http.DefaultClient)
	if bare.metadataClient != nil {
		t.Fatal("a provider without a metadata client gained one")
	}
	p.downloadClient = nil
	if err := p.fileClient().CheckRedirect(same, nil); err == nil {
		t.Fatal("fallback file client followed a redirect")
	}
}

func TestOAPENTitleFallsBackToTheItemName(t *testing.T) {
	e, _, err := oapenEval(t, oapenHandle, oapenBody(t, func(m map[string]any) {
		m["metadata"] = []any{map[string]any{"key": "dc.title", "value": 12}, map[string]any{"key": "dc.language", "value": "eng"}}
	}), 200, "application/json")
	if err != nil || strings.Join(e.Title, "") != "The deliverance of open access books" || strings.Join(e.Languages, "") != "eng" {
		t.Fatal(err, e.Title, e.Languages)
	}
}

func TestOAPENUppercaseUUIDIsCanonicalInTheURL(t *testing.T) {
	e, _, err := oapenEval(t, oapenHandle, oapenBody(t, func(m map[string]any) {
		one := oapenOriginal()
		one["uuid"] = strings.ToUpper(oapenPDF)
		one["retrieveLink"] = "/rest/bitstreams/" + strings.ToUpper(oapenPDF) + "/retrieve"
		m["bitstreams"] = []any{one}
	}), 200, "application/json")
	if err != nil || e.Files[0].DownloadURL != oapenRetrieveURL(oapenPDF) {
		t.Fatal(err, e.Files)
	}
}

func oapenOriginalN(i int) map[string]any {
	uuid := fmt.Sprintf("00000000-0000-4000-8000-%012x", i+1)
	return map[string]any{
		"uuid": uuid, "name": fmt.Sprintf("book-%d.pdf", i), "type": "bitstream",
		"bundleName": "ORIGINAL", "mimeType": "application/pdf", "sizeBytes": 1,
		"retrieveLink": "/rest/bitstreams/" + uuid + "/retrieve",
		"checkSum":     map[string]any{"value": oapenMD5, "checkSumAlgorithm": "MD5"},
	}
}

func TestOAPENBoundsTheBitstreamListAndFileDeclarations(t *testing.T) {
	e, urls, err := oapenEval(t, oapenHandle, oapenBody(t, func(m map[string]any) {
		m["bitstreams"] = []any{}
	}), 200, "application/json")
	if err != nil || !e.Complete || len(e.Files) != 0 || len(urls) != 1 || !strings.Contains(strings.Join(e.Limitations, "\n"), "no ORIGINAL") {
		t.Fatal(err, e.Complete, e.Files, urls)
	}
	text := make([]any, 500)
	for i := range text {
		text[i] = map[string]any{"bundleName": "TEXT", "name": "notes.txt"}
	}
	e, _, err = oapenEval(t, oapenHandle, oapenBody(t, func(m map[string]any) { m["bitstreams"] = text }), 200, "application/json")
	if err != nil || !e.Complete || len(e.Files) != 0 {
		t.Fatal("500 non-original bitstreams", err, len(e.Files))
	}
	text = append(text, map[string]any{"bundleName": "EXPORT"})
	e, _, err = oapenEval(t, oapenHandle, oapenBody(t, func(m map[string]any) { m["bitstreams"] = text }), 200, "application/json")
	if err == nil || e.Complete || !strings.Contains(err.Error(), "more than 500") {
		t.Fatal(err, e.Complete)
	}
	fifty := make([]any, 50)
	for i := range fifty {
		fifty[i] = oapenOriginalN(i)
	}
	e, _, err = oapenEval(t, oapenHandle, oapenBody(t, func(m map[string]any) { m["bitstreams"] = fifty }), 200, "application/json")
	if err != nil || len(e.Files) != 50 || e.Files[49].DownloadURL == "" {
		t.Fatal(err, len(e.Files))
	}
	e, _, err = oapenEval(t, oapenHandle, oapenBody(t, func(m map[string]any) {
		m["bitstreams"] = append(fifty, oapenOriginalN(50))
	}), 200, "application/json")
	if err == nil || e.Complete || len(e.Files) != 0 || !strings.Contains(err.Error(), "more than 50") {
		t.Fatal(err, e.Complete, e.Files)
	}
	for _, name := range []string{"", ".", "..", `a\b.pdf`, "a:b.pdf", "a\x00b.pdf", strings.Repeat("a", 1001) + ".pdf"} {
		e, urls, err = oapenEval(t, oapenHandle, oapenBody(t, func(m map[string]any) {
			one := oapenOriginal()
			one["name"] = name
			m["bitstreams"] = []any{one}
		}), 200, "application/json")
		if err == nil || len(urls) != 1 || !strings.Contains(err.Error(), "unsafe or duplicate") {
			t.Fatalf("%q: %v %v", name, err, urls)
		}
	}
	rights := make([]any, 6)
	for i := range rights {
		rights[i] = map[string]any{"key": "dc.rights.uri", "value": fmt.Sprintf("https://creativecommons.org/licenses/by/%d/", i)}
	}
	e, _, err = oapenEval(t, oapenHandle, oapenBody(t, func(m map[string]any) {
		one := oapenOriginal()
		one["metadata"] = rights
		m["bitstreams"] = []any{one}
	}), 200, "application/json")
	joined := ""
	if err == nil && len(e.Files) == 1 {
		joined = strings.Join(e.Files[0].Findings, "\n")
	}
	if err != nil || e.Files[0].DownloadURL != "" || !strings.Contains(joined, "further bitstream dc.rights.uri") || strings.Contains(joined, "/5/") {
		t.Fatal(err, e.Files, joined)
	}
	e, _, err = oapenEval(t, oapenHandle, oapenBody(t, func(m map[string]any) {
		one := oapenOriginal()
		one["metadata"] = []any{
			map[string]any{"key": "dc.rights.uri", "value": "not a url"},
			map[string]any{"key": "dc.rights.uri", "value": true},
			map[string]any{"key": "dc.rights.uri", "value": "https://creativecommons.org/licenses/by/4.0/"},
		}
		m["bitstreams"] = []any{one}
	}), 200, "application/json")
	if err != nil || e.Files[0].DownloadURL != oapenRetrieveURL(oapenPDF) || !strings.Contains(strings.Join(e.Files[0].Findings, "\n"), "was not an http(s) URL") || !strings.Contains(strings.Join(e.Files[0].Findings, "\n"), "was not a string") {
		t.Fatal(err, e.Files)
	}
	meta := make([]any, 101)
	for i := range meta {
		meta[i] = map[string]any{"key": "dc.description", "value": "x"}
	}
	e, _, err = oapenEval(t, oapenHandle, oapenBody(t, func(m map[string]any) {
		one := oapenOriginal()
		one["metadata"] = meta
		m["bitstreams"] = []any{one}
	}), 200, "application/json")
	if err != nil || e.Files[0].DownloadURL != "" || !strings.Contains(strings.Join(e.Files[0].Findings, "\n"), "exceeded 100") {
		t.Fatal(err, e.Files)
	}
	e, _, err = oapenEval(t, oapenHandle, oapenBody(t, func(m map[string]any) {
		one := oapenOriginal()
		one["code"] = strings.Repeat("C", 100)
		m["bitstreams"] = []any{one}
	}), 200, "application/json")
	if err != nil || e.Files[0].RightsStatus != "declared code: "+strings.Repeat("C", 100) || e.Files[0].DownloadURL == "" {
		t.Fatal(err, e.Files)
	}
	e, _, err = oapenEval(t, oapenHandle, oapenBody(t, func(m map[string]any) {
		one := oapenOriginal()
		one["code"] = strings.Repeat("C", 101)
		m["bitstreams"] = []any{one}
	}), 200, "application/json")
	if err != nil || e.Files[0].DownloadURL == "" || e.Files[0].RightsStatus != "not_declared" || !strings.Contains(strings.Join(e.Files[0].Findings, "\n"), "bitstream code was not copied") {
		t.Fatal(err, e.Files)
	}
}

func TestOAPENKeepsLeadingZerosAndItemDeclarations(t *testing.T) {
	id := "oapen:20.500.12657/00025287"
	e, urls, err := oapenEval(t, id, oapenBody(t, func(m map[string]any) {
		m["handle"] = "20.500.12657/00025287"
		m["metadata"] = append(m["metadata"].([]any),
			map[string]any{"key": "dc.contributor.editor", "value": "Editor, A"},
			map[string]any{"key": "dc.publisher", "value": "Second Press"},
			map[string]any{"key": "dc.identifier.doi", "value": "10.0000/item"},
			map[string]any{"key": "dc.rights.uri", "value": "https://creativecommons.org/licenses/by/4.0/"},
			map[string]any{"key": "dc.rights.uri", "value": "javascript:alert(1)"},
			map[string]any{"key": "dc.rights.accessrights", "value": "open access"},
			map[string]any{"key": "dc.description", "value": "info:eu-repo/semantics/openAccess"},
		)
	}), 200, "application/json")
	if err != nil || len(urls) != 1 {
		t.Fatal(err, urls)
	}
	got, err := url.Parse(urls[0])
	if err != nil || got.Path != "/rest/handle/20.500.12657/00025287" || e.LandingPage != "https://library.oapen.org/handle/20.500.12657/00025287" {
		t.Fatal(urls, e.LandingPage, err)
	}
	if strings.Join(e.Creators, "|") != "Snijder, Ronald|Editor, A" || strings.Join(e.Publishers, "|") != "Amsterdam University Press|Second Press" {
		t.Fatal(e.Creators, e.Publishers)
	}
	if strings.Join(e.LicenseURLs, "|") != "https://creativecommons.org/licenses/by/4.0/" {
		t.Fatal(e.LicenseURLs)
	}
	rights := strings.Join(e.Rights, "\n")
	if !strings.Contains(rights, "info:eu-repo/semantics/openAccess") || !strings.Contains(rights, "open access") || strings.Contains(rights, "javascript") {
		t.Fatal(rights)
	}
	var itemDOI bool
	for _, identifier := range e.Identifiers {
		if identifier.Field == "dc.identifier.doi" && identifier.Value == "10.0000/item" {
			itemDOI = true
		}
	}
	if !itemDOI {
		t.Fatal(e.Identifiers)
	}
}

func TestOAPENDownloadDoesNotFollowARedirect(t *testing.T) {
	p := NewOAPEN()
	var requested []string
	p.downloadClient.Transport = oapenTrip(func(r *http.Request) (*http.Response, error) {
		requested = append(requested, r.URL.String())
		h := make(http.Header)
		h.Set("Location", "https://"+OAPENHost+"/rest/bitstreams/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/retrieve")
		return &http.Response{StatusCode: http.StatusFound, Header: h, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	n := int64(10)
	file := OfferedFile{Name: "book.pdf", Access: OAPENOriginalAccess, Bytes: &n, MD5: oapenMD5, DownloadURL: oapenRetrieveURL(oapenPDF)}
	resp, err := p.Download(context.Background(), file)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err == nil || len(requested) != 1 || requested[0] != file.DownloadURL || !strings.Contains(err.Error(), "redirects are not supported") {
		t.Fatal(requested, err)
	}
	for _, raw := range []string{
		"http://" + OAPENHost + "/rest/bitstreams/" + oapenPDF + "/retrieve",
		"https://user@" + OAPENHost + "/rest/bitstreams/" + oapenPDF + "/retrieve",
		"https://" + OAPENHost + ":443/rest/bitstreams/" + oapenPDF + "/retrieve",
		"https://" + OAPENHost + "/rest/bitstreams/" + oapenPDF + "/retrieve#x",
		"https://" + strings.ToUpper(OAPENHost) + "/rest/bitstreams/" + oapenPDF + "/retrieve",
		oapenRetrieveURL(strings.ToUpper(oapenPDF)),
	} {
		file.DownloadURL = raw
		if ValidOAPENOffer(file) {
			t.Fatal("accepted", raw)
		}
	}
}

func TestOAPENCapsCopiedDeclarations(t *testing.T) {
	authors := make([]any, 51)
	for i := range authors {
		authors[i] = map[string]any{"key": "dc.contributor.author", "value": fmt.Sprintf("Author %02d", i)}
	}
	authors = append(authors, map[string]any{"key": "dc.contributor.author", "value": 12})
	licenses := make([]any, 11)
	for i := range licenses {
		licenses[i] = map[string]any{"key": "dc.rights.uri", "value": fmt.Sprintf("https://licenses.example/%d", i)}
	}
	rights := []any{
		map[string]any{"key": "dc.rights", "value": "same"},
		map[string]any{"key": "dc.rights", "value": "same"},
	}
	for i := 0; i < 20; i++ {
		rights = append(rights, map[string]any{"key": "dc.rights", "value": fmt.Sprintf("right %02d", i)})
	}
	identifiers := []any{
		map[string]any{"key": "oapen.identifier.isbn", "value": "9780000000000"},
		map[string]any{"key": "oapen.identifier.isbn", "value": "9780000000000"},
	}
	for i := 0; i < maxFieldIdentifiers; i++ {
		identifiers = append(identifiers, map[string]any{"key": "oapen.identifier.isbn", "value": fmt.Sprintf("978000000%04d", i+1)})
	}
	meta := append(append(append(authors, licenses...), rights...), identifiers...)
	e, _, err := oapenEval(t, oapenHandle, oapenBody(t, func(m map[string]any) { m["metadata"] = meta }), 200, "application/json")
	if err != nil || strings.Join(e.Title, "") != "The deliverance of open access books" || len(e.Creators) != 50 || e.Creators[0] != "Author 00" || e.Creators[49] != "Author 49" {
		t.Fatal(err, e.Title, len(e.Creators), e.Creators)
	}
	if len(e.LicenseURLs) != 10 || e.LicenseURLs[0] != "https://licenses.example/0" || e.LicenseURLs[9] != "https://licenses.example/9" {
		t.Fatal(e.LicenseURLs)
	}
	if len(e.Rights) != 20 || e.Rights[0] != "same" || e.Rights[1] != "right 00" {
		t.Fatal(e.Rights)
	}
	isbn, copies := 0, 0
	for _, id := range e.Identifiers {
		if id.Field != "oapen.identifier.isbn" {
			continue
		}
		isbn++
		if id.Value == "9780000000000" {
			copies++
		}
	}
	if isbn != maxFieldIdentifiers || copies != 1 {
		t.Fatal(isbn, copies, e.Identifiers)
	}
}

func TestOAPENStopsAfterTwoMetadataRetries(t *testing.T) {
	var n int
	p := NewOAPEN()
	p.SetHTTPClient(&http.Client{Transport: oapenTrip(func(r *http.Request) (*http.Response, error) {
		n++
		if r.URL.Path != "/rest/handle/20.500.12657/25287" || r.URL.Query().Get("expand") != "metadata,bitstreams" {
			t.Errorf("request %s", r.URL)
		}
		h := make(http.Header)
		h.Set("Content-Type", "text/html")
		return &http.Response{StatusCode: 503, Header: h, Body: io.NopCloser(strings.NewReader("busy")), Request: r}, nil
	})})
	e, err := p.Evaluate(context.Background(), oapenHandle)
	if err == nil || e.Complete || n != 3 || len(e.Files) != 0 || !strings.Contains(err.Error(), "503") {
		t.Fatal(n, err, e.Complete, e.Files)
	}
}
