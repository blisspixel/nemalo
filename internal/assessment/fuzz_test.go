package assessment

import (
	"archive/zip"
	"bytes"
	"context"
	"testing"
)

const fuzzContainer = `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`

// FuzzEPUBInspection inspects arbitrary bytes, and well-formed ZIPs whose
// package, document, and navigation members are arbitrary. Inspection,
// document visits, and navigation reads must never panic, must agree from
// one run to the next, and must keep visits inside the declared spine.
func FuzzEPUBInspection(f *testing.F) {
	f.Add([]byte("PK\x03\x04"), `<package xmlns="http://www.idpf.org/2007/opf"><metadata><title>T</title><language>en</language></metadata><manifest><item id="c" href="c.xhtml" media-type="application/xhtml+xml"/><item id="n" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/></manifest><spine><itemref idref="c"/></spine></package>`, `<html xmlns="http://www.w3.org/1999/xhtml"><body><p>x</p></body></html>`, `<nav/>`, true)
	f.Add([]byte{}, `<package xmlns="http://www.idpf.org/2007/opf"><manifest><item id="c" href="../../etc/passwd" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="c"/><itemref idref="c"/></spine></package>`, `<script>alert(1)</script>`, ``, true)
	f.Add([]byte{}, `<package xmlns="http://www.idpf.org/2007/opf"><manifest><item id="c" href="http://example.org/x" media-type="application/xhtml+xml"/></manifest><spine toc="missing"><itemref idref="c" linear="maybe"/></spine></package>`, `<!DOCTYPE html [<!ENTITY a "aaaa">]><html>&a;</html>`, `<ncx/>`, true)
	f.Fuzz(func(t *testing.T, raw []byte, opf, doc, nav string, build bool) {
		if len(raw)+len(opf)+len(doc)+len(nav) > 1<<17 {
			t.Skip()
		}
		data := raw
		if build {
			var buf bytes.Buffer
			z := zip.NewWriter(&buf)
			w, _ := z.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
			_, _ = w.Write([]byte("application/epub+zip"))
			for _, m := range [][2]string{{"META-INF/container.xml", fuzzContainer}, {"book.opf", opf}, {"c.xhtml", doc}, {"nav.xhtml", nav}} {
				w, err := z.Create(m[0])
				if err != nil {
					t.Skip()
				}
				_, _ = w.Write([]byte(m[1]))
			}
			if z.Close() != nil {
				t.Skip()
			}
			data = buf.Bytes()
		}
		ctx := context.Background()
		r := bytes.NewReader(data)
		first, err1 := InspectContext(ctx, r, int64(len(data)), "epub")
		second, err2 := InspectContext(ctx, r, int64(len(data)), "epub")
		if (err1 == nil) != (err2 == nil) || first.EPUBPackage != second.EPUBPackage || len(first.Warnings) != len(second.Warnings) {
			t.Fatal("inspection is not deterministic")
		}
		seen := map[int]bool{}
		_, visitErr := VisitEPUB(ctx, r, int64(len(data)), func(d EPUBDocument) error {
			if seen[d.Index] || d.Index < 0 {
				t.Fatal("a spine index was visited twice or is negative", d.Index)
			}
			seen[d.Index] = true
			return nil
		})
		if err1 != nil && visitErr == nil && len(seen) > 0 {
			t.Fatal("documents were visited in a package that does not inspect")
		}
		if nav, _, err := ReadEPUBNavigation(ctx, r, int64(len(data))); err == nil {
			for _, s := range nav.Sources {
				if len(s.Data) > 2<<20 {
					t.Fatal("navigation member over its budget", len(s.Data))
				}
			}
		}
	})
}
