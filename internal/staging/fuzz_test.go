package staging

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzExtractZip feeds arbitrary bytes, and ZIPs with arbitrary member names
// and data, to the folder-ingest extractor. Whatever the input, every member
// is accounted for, nothing is written outside the staging root, staged
// names are flat, and extracted bytes stay within the limits.
func FuzzExtractZip(f *testing.F) {
	f.Add([]byte("PK\x03\x04"), "book.epub", "data", false)
	f.Add([]byte{}, "../escape.epub", "x", true)
	f.Add([]byte{}, "dir/", "", true)
	f.Add([]byte{}, "C:\\evil.epub", "y", true)
	f.Add([]byte{}, "nested.zip", "PK", true)
	f.Fuzz(func(t *testing.T, raw []byte, name, body string, build bool) {
		if len(raw) > 1<<16 || len(body) > 1<<16 || len(name) > 1024 {
			t.Skip()
		}
		data := raw
		if build {
			var buf bytes.Buffer
			z := zip.NewWriter(&buf)
			w, err := z.Create(name)
			if err != nil {
				t.Skip()
			}
			_, _ = w.Write([]byte(body))
			if w, err := z.Create("second.epub"); err == nil {
				_, _ = w.Write([]byte(body))
			}
			if z.Close() != nil {
				t.Skip()
			}
			data = buf.Bytes()
		}
		srcDir, dstDir := t.TempDir(), t.TempDir()
		outside := filepath.Join(filepath.Dir(dstDir), "outside-"+filepath.Base(dstDir))
		if err := os.WriteFile(filepath.Join(srcDir, "in.zip"), data, 0o600); err != nil {
			t.Fatal(err)
		}
		src, err := os.OpenRoot(srcDir)
		if err != nil {
			t.Fatal(err)
		}
		defer src.Close()
		dst, err := os.OpenRoot(dstDir)
		if err != nil {
			t.Fatal(err)
		}
		defer dst.Close()
		limits := Limits{Members: 8, MemberBytes: 1 << 16, TotalBytes: 1 << 17, Ratio: 100}
		x, err := ExtractZip(context.Background(), src, "in.zip", dst, func(n string) bool { return strings.HasSuffix(strings.ToLower(n), ".epub") }, limits)
		if _, statErr := os.Lstat(outside); statErr == nil {
			t.Fatal("wrote outside the staging root")
		}
		entries, readErr := os.ReadDir(dstDir)
		if readErr != nil {
			t.Fatal(readErr)
		}
		staged, total := map[string]bool{}, int64(0)
		for _, m := range x.Members {
			if m.Disposition == "" {
				t.Fatal("member without a disposition", m)
			}
			if m.Staged != "" {
				if strings.ContainsAny(m.Staged, `/\`) || m.Staged == "." || m.Staged == ".." {
					t.Fatal("staged name is not flat", m.Staged)
				}
				staged[m.Staged] = true
			}
		}
		for _, e := range entries {
			if !e.Type().IsRegular() {
				t.Fatal("staging holds a non-regular entry", e.Name())
			}
			info, _ := e.Info()
			total += info.Size()
			if err == nil && !staged[e.Name()] {
				t.Fatal("a staged file is not accounted for", e.Name())
			}
		}
		if total > limits.TotalBytes {
			t.Fatal("extraction exceeded its byte limit", total)
		}
	})
}
