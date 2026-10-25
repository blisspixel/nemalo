package discovery

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

type pmcTransport func(*http.Request) (*http.Response, error)

func (f pmcTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func pmcReply(r *http.Request, status int, b string) *http.Response {
	media := "application/xml"
	if strings.HasSuffix(r.URL.Path, ".json") {
		media = "binary/octet-stream"
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {media}}, Body: io.NopCloser(strings.NewReader(b)), ContentLength: int64(len(b)), Request: r}
}

const pmcMeta = `{"pmcid":"PMC13901","version":2,"title":"A paper","doi":"10.123/example","pmid":123,"citation":"Journal 1","is_pmc_openaccess":true,"is_manuscript":false,"is_historical_ocr":false,"is_retracted":true,"license_code":"CC BY","pdf_url":"s3://pmc-oa-opendata/PMC13901.2/PMC13901.2.pdf?md5=0123456789abcdef0123456789abcdef"}`

func pmcList(prefix, entries string) string {
	return `<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>pmc-oa-opendata</Name><Prefix>` + prefix + `</Prefix><IsTruncated>false</IsTruncated>` + entries + `</ListBucketResult>`
}
func pmcFixture(t *testing.T, modify func(string, string) string, at *[]time.Time) *PMC {
	t.Helper()
	return &PMC{Client: &http.Client{Transport: pmcTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != PMCHost || r.Method != "GET" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
		}
		if at != nil {
			*at = append(*at, time.Now())
		}
		prefix := r.URL.Query().Get("prefix")
		var b string
		switch {
		case prefix == "PMC13901.":
			b = pmcList(prefix, `<CommonPrefixes><Prefix>PMC13901.2/</Prefix></CommonPrefixes><CommonPrefixes><Prefix>PMC13901.7/</Prefix></CommonPrefixes>`)
		case prefix == "PMC13901.2/PMC13901.2.pdf":
			b = pmcList(prefix, `<Contents><Key>PMC13901.2/PMC13901.2.pdf</Key><Size>100</Size></Contents>`)
		case r.URL.Path == "/metadata/PMC13901.2.json":
			b = pmcMeta
		case r.URL.Path == "/metadata/PMC13901.7.json":
			b = strings.ReplaceAll(strings.ReplaceAll(pmcMeta, `"version":2`, `"version":7`), "PMC13901.2", "PMC13901.7")
			b = strings.ReplaceAll(b, "CC BY", "TDM")
		case prefix == "PMC13901.7/PMC13901.7.pdf":
			b = pmcList(prefix, `<Contents><Key>PMC13901.7/PMC13901.7.pdf</Key><Size>200</Size></Contents>`)
		case r.URL.Path == "/PMC13901.2/PMC13901.2.pdf":
			b = "PDF bytes"
		default:
			t.Fatalf("unexpected URL %s", r.URL)
		}
		if modify != nil {
			b = modify(r.URL.Path+"?"+prefix, b)
		}
		return pmcReply(r, 200, b), nil
	})}}
}
func TestPMCExactVersionRightsAndPacing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var at []time.Time
		p := pmcFixture(t, nil, &at)
		e, err := p.Evaluate(context.Background(), "pmc:PMC13901.2")
		if err != nil || !e.Complete || e.ID != "pmc:PMC13901.2" || e.Restrictions["is_retracted"] != "true" || len(e.Files) != 1 {
			t.Fatalf("%+v %v", e, err)
		}
		f := e.Files[0]
		if f.Access != PMCPDFAccess || f.Bytes == nil || *f.Bytes != 100 || f.MD5 != "0123456789abcdef0123456789abcdef" || !strings.Contains(strings.Join(e.Limitations, " "), "NLM") || !strings.Contains(strings.Join(e.Publishers, " "), "doi:10.123/example") {
			t.Fatal(e)
		}
		resp, err := p.Download(context.Background(), f)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		latest, err := p.Evaluate(context.Background(), "pmc:PMC13901")
		if err != nil || latest.ID != "pmc:PMC13901.7" || latest.Files[0].DownloadURL != "" || !strings.Contains(latest.Rights[0], "TDM") {
			t.Fatal(latest, err)
		}
		for i := 1; i < len(at); i++ {
			if at[i].Sub(at[i-1]) < time.Second {
				t.Fatal("pace compressed", at)
			}
		}
	})
}
func TestPMCRejectsMalformedOrIncompleteEvidence(t *testing.T) {
	cases := map[string][2]string{
		"duplicate license": {`"license_code":"CC BY"`, `"license_code":"TDM","license_code":"CC BY"`},
		"case variant":      {`"is_pmc_openaccess":true`, `"IS_PMC_OPENACCESS":true`},
		"wrong article":     {`"pmcid":"PMC13901"`, `"pmcid":"PMC13902"`},
		"wrong version":     {`"version":2`, `"version":1`},
		"bad json":          {`"title":"A paper"`, `"title":`},
		"trailing json":     {`"citation":"Journal 1"`, `"citation":"Journal 1"} {`},
		"bad flag":          {`"is_pmc_openaccess":true`, `"is_pmc_openaccess":"yes"`},
		"foreign bucket":    {"s3://pmc-oa-opendata/", "s3://evil/"},
		"wrong pdf":         {"PMC13901.2.pdf?md5", "PMC13901.1.pdf?md5"},
		"bad md5":           {"0123456789abcdef0123456789abcdef", "not-a-digest"},
		"extra query":       {"?md5=", "?other=1&md5="},
		"fragment":          {"?md5=0123456789abcdef0123456789abcdef", "?md5=0123456789abcdef0123456789abcdef#fragment"},
		"encoded path":      {"s3://pmc-oa-opendata/PMC13901.2/", "s3://pmc-oa-opendata/%50MC13901.2/"},
		"truncated":         {"<IsTruncated>false</IsTruncated>", "<IsTruncated>true</IsTruncated>"},
		"missing complete":  {"<IsTruncated>false</IsTruncated>", ""},
		"foreign listing":   {"<Name>pmc-oa-opendata</Name>", "<Name>elsewhere</Name>"},
		"duplicate version": {"<CommonPrefixes><Prefix>PMC13901.7/</Prefix></CommonPrefixes>", "<CommonPrefixes><Prefix>PMC13901.2/</Prefix></CommonPrefixes>"},
		"unknown version":   {"<CommonPrefixes><Prefix>PMC13901.2/</Prefix></CommonPrefixes>", ""},
		"foreign prefix":    {"<CommonPrefixes><Prefix>PMC13901.2/</Prefix></CommonPrefixes>", "<CommonPrefixes><Prefix>PMC2.2/</Prefix></CommonPrefixes>"},
		"zero size":         {"<Size>100</Size>", "<Size>0</Size>"},
		"different key":     {"<Key>PMC13901.2/PMC13901.2.pdf</Key>", "<Key>PMC13901.2/other.pdf</Key>"},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				p := pmcFixture(t, func(_ string, b string) string { return strings.ReplaceAll(b, change[0], change[1]) }, nil)
				if _, err := p.Evaluate(context.Background(), "pmc:PMC13901.2"); err == nil {
					t.Fatal("accepted invalid evidence")
				}
			})
		})
	}
}
func TestPMCMissingAndUnknownRightsRemainVisible(t *testing.T) {
	for _, license := range []string{"TDM", "", "CC unknown", "CC BY 4.0", "public domain"} {
		t.Run(license, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				p := pmcFixture(t, func(_ string, b string) string { return strings.ReplaceAll(b, "CC BY", license) }, nil)
				e, err := p.Evaluate(context.Background(), "pmc:PMC13901.2")
				if err != nil || len(e.Files) != 1 || e.Files[0].DownloadURL != "" || e.Files[0].Access == PMCPDFAccess {
					t.Fatal(e, err)
				}
			})
		})
	}
	for _, flag := range []string{"false", "null"} {
		t.Run(flag, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				p := pmcFixture(t, func(_ string, b string) string {
					return strings.ReplaceAll(b, `"is_pmc_openaccess":true`, `"is_pmc_openaccess":`+flag)
				}, nil)
				e, err := p.Evaluate(context.Background(), "pmc:PMC13901.2")
				if err != nil || e.Files[0].DownloadURL != "" {
					t.Fatal(e, err)
				}
			})
		})
	}
	synctest.Test(t, func(t *testing.T) {
		p := pmcFixture(t, func(_ string, b string) string {
			return strings.ReplaceAll(b, `"pdf_url":"s3://pmc-oa-opendata/PMC13901.2/PMC13901.2.pdf?md5=0123456789abcdef0123456789abcdef"`, `"pdf_url":null`)
		}, nil)
		e, err := p.Evaluate(context.Background(), "pmc:PMC13901.2")
		if err != nil || !e.Complete || len(e.Files) != 0 {
			t.Fatal(e, err)
		}
	})
}
func TestPMCBoundsRetriesCancellationAndDownloadBinding(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, status := range []int{403, 429, 503} {
			calls := 0
			p := &PMC{Client: &http.Client{Transport: pmcTransport(func(r *http.Request) (*http.Response, error) { calls++; return pmcReply(r, status, "refused"), nil })}}
			_, err := p.Evaluate(context.Background(), "pmc:PMC13901")
			want := 3
			if status == 403 {
				want = 1
			}
			if err == nil || calls != want {
				t.Fatal(status, calls, err)
			}
		}
		p := &PMC{Client: &http.Client{Transport: pmcTransport(func(r *http.Request) (*http.Response, error) {
			return pmcReply(r, 200, strings.Repeat("x", maxPMCRecord+1)), nil
		})}}
		if _, err := p.Evaluate(context.Background(), "pmc:PMC13901"); err == nil {
			t.Fatal("oversize response accepted")
		}
		p = pmcFixture(t, nil, nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := p.Evaluate(ctx, "pmc:PMC13901.2"); err == nil {
			t.Fatal("cancelled request accepted")
		}
		e, err := p.Evaluate(context.Background(), "pmc:PMC13901.2")
		if err != nil {
			t.Fatal(err)
		}
		for _, change := range []func(*OfferedFile){func(f *OfferedFile) { f.DownloadURL += "?x=1" }, func(f *OfferedFile) { f.Name = "PMC13901.1.pdf" }, func(f *OfferedFile) { f.MD5 = "" }, func(f *OfferedFile) { f.Access = "discovery_only" }} {
			f := e.Files[0]
			change(&f)
			if _, err := p.Download(context.Background(), f); err == nil {
				t.Fatal("unbound download accepted", f)
			}
		}
		c := NewPMC().Client
		req, _ := http.NewRequest("GET", "https://"+PMCHost+"/other", nil)
		if c.CheckRedirect(req, nil) == nil {
			t.Fatal("redirect allowed")
		}
	})
}
func TestPMCIdentifiers(t *testing.T) {
	for _, id := range []string{"pmc:PMC13901", "pmc:PMC13901.2", "pmc:PMC1.9999"} {
		if !ValidPMCID(id) {
			t.Fatal(id)
		}
	}
	for _, id := range []string{"PMC13901", "pmc:pmc13901", "pmc:PMC0", "pmc:PMC01", "pmc:PMC1.0", "pmc:PMC1.01", "pmc:PMC1.10000", "pmc:PMC1/../../x", "pmc:PMC1.1?x"} {
		if ValidPMCID(id) {
			t.Fatal(id)
		}
	}
	for _, l := range []string{"CC0", "CC BY", "CC BY-SA", "CC BY-ND", "CC BY-NC", "CC BY-NC-SA", "CC BY-NC-ND"} {
		if !pmcCC(l) {
			t.Fatal(l)
		}
	}
	if pmcFlag(nil) != "not_declared" {
		t.Fatal("absent flag")
	}
	p := NewPMC()
	if _, err := p.Evaluate(context.Background(), "bad"); err == nil {
		t.Fatal("bad identifier")
	}
}

func TestPMCRecordErrorsAndQuota(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		p := &PMC{Client: &http.Client{Transport: pmcTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			resp := pmcReply(r, 429, "busy")
			resp.Header.Set("Retry-After", "120")
			return resp, nil
		})}}
		for range 2 {
			if _, err := p.Evaluate(context.Background(), "pmc:PMC13901"); err == nil {
				t.Fatal("quota accepted")
			}
		}
		if calls != 1 {
			t.Fatal("long source quota not shared", calls)
		}
		p = &PMC{Client: &http.Client{Transport: pmcTransport(func(r *http.Request) (*http.Response, error) {
			resp := pmcReply(r, 200, "{}")
			resp.Header.Set("Content-Type", "text/html")
			return resp, nil
		})}}
		if _, err := p.record(context.Background(), "https://"+PMCHost+"/metadata/PMC13901.2.json"); err == nil {
			t.Fatal("HTML accepted")
		}
	})
	for _, body := range []string{"null", "[]", `{"pmcid":`, `{"pmcid":"PMC13901","pmcid":"PMC1"}`, `{"title":"x"} {}`} {
		if _, err := parsePMCMetadata([]byte(body)); err == nil {
			t.Fatal("ambiguous metadata", body)
		}
	}
}
func FuzzPMCMetadata(f *testing.F) {
	f.Add(pmcMeta)
	f.Add(`{"license_code":"TDM","license_code":"CC BY"}`)
	f.Fuzz(func(t *testing.T, b string) {
		if len(b) > maxPMCRecord {
			t.Skip()
		}
		m, err := parsePMCMetadata([]byte(b))
		again, err2 := parsePMCMetadata([]byte(b))
		if (err == nil) != (err2 == nil) || m.PMCID != again.PMCID || m.Version != again.Version || m.License != again.License {
			t.Fatal("unstable metadata")
		}
	})
}
