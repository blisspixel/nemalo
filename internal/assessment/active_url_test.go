package assessment

import (
	"bytes"
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/scanner"
)

func TestHTMLActiveURLIndicators(t *testing.T) {
	for _, tc := range []struct {
		name, body       string
		active, external bool
	}{
		{"data navigation", `<a href="data:text/html,example">x</a>`, true, false},
		{"vbscript navigation", `<a href="vbscript:example">x</a>`, true, false},
		{"mixed case", `<a HREF="  DaTa:text/html,example  ">x</a>`, true, false},
		{"unquoted", `<a href=VbScRiPt:example>x</a>`, true, false},
		{"entity scheme", `<a href="d&#x61;ta&#58;text/html,example">x</a>`, true, false},
		{"javascript tab", `<a href="java&#x09;script:example">x</a>`, true, false},
		{"data line feed", `<a href="da&#x0a;ta:text/html,example">x</a>`, true, false},
		{"vbscript carriage return", `<a href="vb&#x0d;script:example">x</a>`, true, false},
		{"leading control", `<a href="&#x1f;data:text/html,example">x</a>`, true, false},
		{"svg navigation", `<svg xmlns:xlink="http://www.w3.org/1999/xlink"><a xlink:href="data:text/html,example">x</a></svg>`, true, false},
		{"aliased svg navigation", `<svg xmlns:l="http://www.w3.org/1999/xlink"><a l:href="data:text/html,example">x</a></svg>`, true, false},
		{"svg script scheme", `<svg xmlns:xlink="http://www.w3.org/1999/xlink"><a xlink:href="java&#x09;script:example">x</a></svg>`, true, false},
		{"metadata attribute", `<a xmlns:m="urn:metadata" m:href="data:text/plain,source">x</a>`, false, false},
		{"shadowed xlink", `<a xmlns:xlink="urn:metadata" xlink:href="data:text/plain,source">x</a>`, false, false},
		{"area navigation", `<area href="data:text/html,example"/>x`, true, false},
		{"data image", `<img src="data:image/png;base64,AAAA"/>x`, false, true},
		{"svg data image", `<svg><image xlink:href="data:image/png;base64,AAAA"/></svg>x`, false, false},
		{"svg filter image", `<svg xmlns:l="http://www.w3.org/1999/xlink"><filter><feImage l:href="data:image/png;base64,AAAA"/></filter></svg>x`, false, false},
		{"aliased svg data image", `<svg><s:image l:href="data:image/png;base64,AAAA"/></svg>x`, false, false},
		{"remote image", `<img src="https://example.invalid/image.png"/>x`, false, true},
		{"remote anchor", `<a href="https://example.invalid/data:text/html">x</a>`, false, false},
		{"relative anchor", `<a href="data-book.xhtml">x</a>`, false, false},
		{"fragment", `<a href="#javascript:example">x</a>`, false, false},
		{"encoded colon", `<a href="javascript%3Aexample">x</a>`, false, false},
		{"space in scheme", `<a href="java script:example">x</a>`, false, false},
		{"protocol relative", `<a href="//example.invalid/data:example">x</a>`, false, false},
		{"mail", `<a href="mailto:reader@example.invalid">x</a>`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			xhtml := tc.name != "unquoted" && tc.name != "leading control"
			n, active, external, err := htmlDocumentPolicyContext(context.Background(), []byte("<body>"+tc.body+"</body>"), xhtml)
			if err != nil || n != 1 || active != tc.active || external != tc.external {
				t.Fatalf("characters=%d active=%t external=%t err=%v", n, active, external, err)
			}
		})
	}
}

func TestXHTMLURLInspectionBoundsAndNamespaces(t *testing.T) {
	for _, tc := range []struct {
		body   string
		active bool
	}{
		{`<body><textarea/><a href="data:text/html,example">Source</a></body>`, true},
		{`<body><title/><a href="vbscript:example">Source</a></body>`, true},
		{`<body><svg xmlns:Q="http://www.w3.org/1999/xlink" xmlns:q="urn:metadata"><a Q:href="data:text/html,example"/></svg></body>`, true},
		{`<body><svg xmlns:Q="urn:metadata" xmlns:q="http://www.w3.org/1999/xlink"><a Q:href="data:text/html,example"/></svg></body>`, false},
		{`<body xmlns:l="http://www.w3.org/1999/xlink"><a xmlns:l="urn:metadata" l:href="data:text/plain,source"/><a l:href="data:text/html,example"/></body>`, true},
		{`<body><textarea>&lt;a href="data:text/html,example"&gt;Source&lt;/a&gt;</textarea></body>`, false},
	} {
		active, err := xhtmlActiveURLs(context.Background(), []byte(tc.body))
		if err != nil || active != tc.active {
			t.Fatal(tc, active, err)
		}
	}
	for _, body := range []string{
		`<body><a href="data:text/html,example">Unclosed`,
		`<body>` + strings.Repeat("<span>", 256) + strings.Repeat("</span>", 256) + `</body>`,
		`<body><a href="` + strings.Repeat("x", 1<<20) + `"/></body>`,
	} {
		if _, err := xhtmlActiveURLs(context.Background(), []byte(body)); err == nil {
			t.Fatal("incomplete or over-budget XML accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := xhtmlActiveURLs(ctx, []byte(`<body/>`)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestEPUBHTMLURIPolicyKeepsHTMLParsing(t *testing.T) {
	for _, tc := range []struct {
		body   string
		active bool
	}{
		{`<body><a href=data:text/html,example>Source</a></body>`, true},
		{`<body><a href="java&#x09;script:example">Source</a></body>`, true},
		{`<body><svg><a xlink:href="data:text/html,example">Source</a></svg></body>`, true},
		{`<body><textarea/><a href="data:text/html,example">Inert raw text</a></textarea></body>`, false},
		{`<body><a href="https://example.invalid/source">Source</a><br>Text &nbsp; here</body>`, false},
	} {
		data := book(t, tc.body, func(files map[string]string) {
			files["book.opf"] = strings.ReplaceAll(files["book.opf"], "application/xhtml+xml", "text/html")
		})
		r, err := Inspect(bytes.NewReader(data), int64(len(data)), "epub")
		if err != nil || slices.Contains(r.AccessRestrictions, "active_content") != tc.active {
			t.Fatal(tc, r, err)
		}
	}
}

func TestEPUBMixedMediaDeclarationsInspectEachSyntax(t *testing.T) {
	data := book(t, `<body><textarea/><a href="data:text/html,example">Source</a></body>`, func(files map[string]string) {
		files["book.opf"] = strings.Replace(files["book.opf"], "<manifest>", `<manifest><item id="html-copy" href="chapter.xhtml" media-type="text/html"/>`, 1)
	})
	r, err := Inspect(bytes.NewReader(data), int64(len(data)), "epub")
	if err != nil || !slices.Contains(r.AccessRestrictions, "active_content") {
		t.Fatal("HTML measurement hid the XHTML declaration", r, err)
	}
}

func TestEPUBActiveURLRequiresReviewDespiteNoDetections(t *testing.T) {
	for _, body := range []string{
		`<a href="data:text/html,example">Source</a>`,
		`<a href="vbscript:example">Source</a>`,
		`<a href="java&#x09;script:example">Source</a>`,
		`<svg xmlns:xlink="http://www.w3.org/1999/xlink"><a xlink:href="data:text/html,example">Source</a></svg>`,
		`<textarea/><a href="data:text/html,example">Source</a>`,
		`<title/><a href="vbscript:example">Source</a>`,
	} {
		for _, offSpine := range []bool{false, true} {
			t.Run(body+"/off-spine="+map[bool]string{false: "false", true: "true"}[offSpine], func(t *testing.T) {
				data := book(t, "<html><body>"+body+"</body></html>", func(files map[string]string) {
					if offSpine {
						files["chapter.xhtml"] = "<html><body>Ordinary source</body></html>"
						files["other.xhtml"] = "<html><body>" + body + "</body></html>"
						files["book.opf"] = strings.Replace(files["book.opf"], "</manifest>", `<item id="other" href="other.xhtml" media-type="application/xhtml+xml"/></manifest>`, 1)
					}
				})
				source := write(t, "book.epub", data)
				calls := 0
				scan := scanFunc(func(context.Context, string) scanner.Result {
					calls++
					return scanner.Result{Status: "no_detections_reported"}
				})
				r, err := Check(context.Background(), source, Options{Scan: true}, scan)
				if err == nil || calls != 1 || r.Status != "needs_review" || r.Antivirus.Status != "no_detections_reported" || len(r.Findings) == 0 || !slices.Contains(r.Checks.AccessRestrictions, "active_content") {
					t.Fatal(r, err, calls)
				}
				got, err := os.ReadFile(source)
				if err != nil || !bytes.Equal(got, data) {
					t.Fatal("source changed", err)
				}
			})
		}
	}
}
