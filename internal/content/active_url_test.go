package content

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestActiveURLSourceRestrictionsReturnNoPartialContent(t *testing.T) {
	for _, body := range []string{
		`<a href="data:text/html,example">Source</a>`,
		`<a href="vbscript:example">Source</a>`,
		`<a href="java&#x09;script:example">Source</a>`,
		`<svg xmlns:xlink="http://www.w3.org/1999/xlink"><a xlink:href="data:text/html,example">Source</a></svg>`,
		`<textarea/><a href="data:text/html,example">Source</a>`,
		`<title/><a href="vbscript:example">Source</a>`,
	} {
		for _, representation := range []string{"epub-text/1", "epub-text/2", "epub-structure/1"} {
			t.Run(body+"/"+representation, func(t *testing.T) {
				q := fixture(t, []string{`<p>Before</p>` + body + `<p>After</p>`}, nil)
				q.Representation = representation
				r, err := Get(context.Background(), q)
				if err == nil || r.Status != "unsupported_extraction" || r.Text != "" || r.Continuation != "" || r.Locator != nil || len(r.Parts) != 0 || r.Resource != nil {
					t.Fatal("restricted source delivered content", r, err)
				}
			})
		}
	}
}

func TestStaticURLControlsKeepExactContinuations(t *testing.T) {
	for _, representation := range []string{"epub-text/1", "epub-text/2", "epub-structure/1"} {
		for _, image := range []bool{false, true} {
			if representation == "epub-text/1" && image {
				continue
			} // This older extractor already refuses external-resource warnings.
			t.Run(representation+"/image="+map[bool]string{false: "false", true: "true"}[image], func(t *testing.T) {
				body := `<p>` + strings.Repeat("Ordinary source with stable references. ", 20) + `</p><a href="https://example.invalid/reference">External</a><a href="#p">Local</a><a xmlns:m="urn:metadata" m:href="data:text/plain,source">Metadata</a>`
				if image {
					body += `<img src="data:image/png;base64,AAAA" alt="Source figure"/>`
					body += `<svg xmlns:l="http://www.w3.org/1999/xlink"><filter><feImage l:href="data:image/png;base64,AAAA"/></filter></svg>`
				}
				q := fixture(t, []string{body}, nil)
				q.Representation = representation
				q.MaxBytes = 31
				before, err := os.ReadFile(filepath.Join(q.Root, "book.epub"))
				if err != nil {
					t.Fatal(err)
				}
				first, err := Get(context.Background(), q)
				if err != nil || first.Continuation == "" {
					t.Fatal(first, err)
				}
				q.Cursor = first.Continuation
				next, err := Get(context.Background(), q)
				if err != nil {
					t.Fatal(next, err)
				}
				repeat, err := Get(context.Background(), q)
				a, _ := json.Marshal(next)
				b, _ := json.Marshal(repeat)
				if err != nil || !bytes.Equal(a, b) || next.Extractor != first.Extractor || next.Representation != first.Representation || next.Locator == nil || first.Locator == nil || next.Locator.RepresentationID != first.Locator.RepresentationID {
					t.Fatal("continuation changed representation", next, repeat, err)
				}
				after, err := os.ReadFile(filepath.Join(q.Root, "book.epub"))
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("source changed", err)
				}
			})
		}
	}
}
