package library

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestImportActiveURLRetainsReviewWithNoDetections(t *testing.T) {
	for _, tc := range []struct{ name, paragraph, want string }{
		{"data link", `<a href="data:text/html,example">Source</a>`, "review"},
		{"vbscript link", `<a href="vbscript:example">Source</a>`, "review"},
		{"encoded scheme", `<a href="da&#x0a;ta:text/html,example">Source</a>`, "review"},
		{"empty textarea", `<textarea/><a href="data:text/html,example">Source</a>`, "review"},
		{"metadata attribute", `<a xmlns:m="urn:metadata" m:href="data:text/plain,source">Source</a>`, "checked"},
		{"ordinary link", `<a href="https://example.invalid/reference">Source</a>`, "checked"},
		{"local fragment", `<a href="#source">Source</a>`, "checked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			directory := t.TempDir()
			if _, err := Initialize(ctx, directory); err != nil {
				t.Fatal(err)
			}
			source := writeEPUBParagraph(t, t.TempDir(), tc.paragraph)
			original, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			r, err := Import(ctx, ImportRequest{Library: directory, Source: source, Apply: true, Scan: true, Scanner: cleanScan{}})
			if err != nil || !r.Applied || r.Status != tc.want {
				t.Fatal(r, err)
			}
			for _, file := range []string{source, filepath.Join(directory, filepath.FromSlash(r.Path))} {
				data, err := os.ReadFile(file)
				if err != nil || !bytes.Equal(data, original) {
					t.Fatal("import changed source bytes", file, err)
				}
			}
		})
	}
}
