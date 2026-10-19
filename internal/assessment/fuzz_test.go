package assessment

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
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

// mp3Frames returns n MPEG-1 Layer III frames at 128 kbps and 44.1 kHz.
func mp3Frames(n int) []byte {
	f := make([]byte, 417)
	copy(f, []byte{0xFF, 0xFB, 0x90, 0x40})
	return bytes.Repeat(f, n)
}

func TestMP3ChecksIndexFramesAndFindTruncationAndDamage(t *testing.T) {
	ctx := context.Background()
	inspect := func(data []byte) Checks {
		c, err := InspectContext(ctx, bytes.NewReader(data), int64(len(data)), "mp3")
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	clean := inspect(mp3Frames(40))
	if clean.Signature != "mp3_frames_indexed" || clean.MP3 == nil || clean.MP3.Frames != 40 || clean.MP3.SampleRate != 44100 || len(clean.Warnings) != 0 {
		t.Fatalf("%+v %+v", clean, clean.MP3)
	}
	// Junk between frames is damage.
	damaged := append(append(mp3Frames(20), bytes.Repeat([]byte{0x11}, 300)...), mp3Frames(20)...)
	if c := inspect(damaged); len(c.Warnings) != 1 || c.Warnings[0] != "mp3_unindexed_regions_between_frames" {
		t.Fatal(c.Warnings)
	}
	// An info frame that declares more frames than the file holds: cut short.
	xing := mp3Frames(1)
	copy(xing[36:], "Xing")
	binary.BigEndian.PutUint32(xing[40:], 0xF)
	binary.BigEndian.PutUint32(xing[44:], 99)
	if c := inspect(append(xing, mp3Frames(40)...)); len(c.Warnings) != 1 || c.Warnings[0] != "mp3_declared_frames_differ" {
		t.Fatal(c.Warnings)
	}
	// A trailing nonstandard tag is a note, not a finding.
	trailer := append(mp3Frames(40), []byte("APETAGEX trailing tag bytes")...)
	if c := inspect(trailer); len(c.Warnings) != 0 || c.MP3 == nil || len(c.MP3.Notes) != 1 || c.MP3.Notes[0] != "mp3_unindexed_trailing_region" {
		t.Fatal(c.Warnings, c.MP3)
	}
	// Bytes that index no frames stay unindexed.
	if c := inspect(append([]byte("ID3"), bytes.Repeat([]byte{0}, 64)...)); len(c.Warnings) != 1 || c.Warnings[0] != "mp3_frames_not_indexed" {
		t.Fatal(c.Warnings)
	}
}
