package assessment

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func coverBook(t *testing.T, manifest, meta string, extra map[string]string) []byte {
	t.Helper()
	return book(t, "<html><body><p>text</p></body></html>", func(files map[string]string) {
		files["book.opf"] = `<package xmlns="http://www.idpf.org/2007/opf"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Test</dc:title><dc:language>en</dc:language>` + meta + `</metadata><manifest><item id="c" href="chapter.xhtml" media-type="application/xhtml+xml"/>` + manifest + `</manifest><spine><itemref idref="c"/></spine></package>`
		for name, data := range extra {
			files[name] = data
		}
	})
}

func TestDeclaredCoverIsTheOnlyCover(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, manifest, meta string
		declaration, member  string
		media, data          string
		extra                map[string]string
		wantErr              bool
	}{
		{name: "epub3", manifest: `<item id="i" href="image.png" media-type="image/png"/><item id="k" href="art.jpg" media-type="image/jpeg" properties="cover-image"/>`, meta: `<meta name="cover" content="i"/>`, declaration: "cover-image", member: "art.jpg", media: "image/jpeg", data: "jpeg bytes", extra: map[string]string{"art.jpg": "jpeg bytes"}},
		{name: "epub2", manifest: `<item id="i" href="image.png" media-type="image/png"/>`, meta: `<meta name="cover" content="i"/>`, declaration: "meta-cover", member: "image.png", media: "image/png", data: "image candidate"},
		// A file named cover.jpg is not a declaration.
		{name: "undeclared", manifest: `<item id="i" href="cover.jpg" media-type="image/jpeg"/>`, extra: map[string]string{"cover.jpg": "x"}},
		{name: "missing", meta: `<meta name="cover" content="absent"/>`, declaration: "missing"},
		{name: "remote", manifest: `<item id="r" href="https://example.invalid/cover.jpg" media-type="image/jpeg" properties="cover-image"/>`, declaration: "remote"},
		{name: "oversized", manifest: `<item id="i" href="big.png" media-type="image/png" properties="cover-image"/>`, extra: map[string]string{"big.png": strings.Repeat("x", 2048)}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := coverBook(t, tc.manifest, tc.meta, tc.extra)
			_, cover, err := ReadEPUBCover(ctx, bytes.NewReader(data), int64(len(data)), 1024)
			if tc.wantErr {
				if !errors.Is(err, ErrLimit) || cover.Data != nil {
					t.Fatal(cover, err)
				}
				return
			}
			if err != nil || cover.Declaration != tc.declaration || cover.Member != tc.member || cover.Media != tc.media || string(cover.Data) != tc.data {
				t.Fatalf("%+v %v", cover, err)
			}
		})
	}
	if _, _, err := ReadEPUBCover(ctx, bytes.NewReader(nil), 0, 9<<20); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	// The whole container is still validated: a broken package has no cover.
	data := book(t, "<html><body>x</body></html>", func(files map[string]string) { files["mimetype"] = "text/plain" })
	if _, cover, err := ReadEPUBCover(ctx, bytes.NewReader(data), int64(len(data)), 1024); err == nil || cover.Data != nil {
		t.Fatal(cover, err)
	}
}
