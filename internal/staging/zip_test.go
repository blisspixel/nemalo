package staging

import (
	"archive/zip"
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type member struct {
	name   string
	body   []byte
	method uint16
	mode   fs.FileMode
	flags  uint16
}

func archive(t *testing.T, members []member) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, m := range members {
		h := &zip.FileHeader{Name: m.name, Method: m.method, Flags: m.flags}
		if m.mode != 0 {
			h.SetMode(m.mode)
		}
		w, err := z.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(m.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func roots(t *testing.T, data []byte) (*os.Root, *os.Root, string) {
	t.Helper()
	srcDir, dstDir := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "a.zip"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	src, err := os.OpenRoot(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	dst, err := os.OpenRoot(dstDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { src.Close(); dst.Close() })
	return src, dst, dstDir
}

func epubOnly(name string) bool { return strings.HasSuffix(strings.ToLower(name), ".epub") }

func dispositions(x Extraction) map[string]string {
	out := map[string]string{}
	for _, m := range x.Members {
		out[m.Name] = m.Disposition
	}
	return out
}

func TestExtractZipAccountsForEveryMember(t *testing.T) {
	book := bytes.Repeat([]byte("epub bytes "), 50)
	data := archive(t, []member{
		{name: "books/", method: zip.Store},
		{name: "books/One.epub", body: book, method: zip.Deflate},
		{name: "books/one.EPUB", body: book, method: zip.Store},
		{name: "notes.txt", body: []byte("hello"), method: zip.Deflate},
		{name: "inner.zip", body: []byte("PK"), method: zip.Store},
		{name: "../escape.epub", body: book, method: zip.Store},
		{name: "/abs.epub", body: book, method: zip.Store},
		{name: `dir\win.epub`, body: book, method: zip.Store},
		{name: "C:drive.epub", body: book, method: zip.Store},
		{name: "link.epub", body: []byte("target"), method: zip.Store, mode: fs.ModeSymlink | 0o777},
		{name: "secret.epub", body: book, method: zip.Store, flags: 0x1},
		{name: "empty.epub", method: zip.Store},
	})
	src, dst, dstDir := roots(t, data)
	x, err := ExtractZip(context.Background(), src, "a.zip", dst, epubOnly, Defaults())
	if err != nil || !x.Complete || len(x.Members) != 12 {
		t.Fatal(x, err)
	}
	want := map[string]string{
		"books/": "directory", "books/One.epub": "extracted", "books/one.EPUB": "extracted",
		"notes.txt": "unsupported_format", "inner.zip": "nested_archive", "../escape.epub": "unsafe_name",
		"/abs.epub": "unsafe_name", `dir\win.epub`: "unsafe_name", "C:drive.epub": "unsafe_name",
		"link.epub": "link", "secret.epub": "encrypted", "empty.epub": "empty",
	}
	got := dispositions(x)
	for name, d := range want {
		if got[name] != d {
			t.Fatalf("%q: %s, want %s", name, got[name], d)
		}
	}
	// Case-colliding names do not collide on disk: staged names are indexed.
	entries, err := os.ReadDir(dstDir)
	if err != nil || len(entries) != 2 {
		t.Fatal("staging holds something other than the two extracted members", entries, err)
	}
	for _, e := range entries {
		got, _ := os.ReadFile(filepath.Join(dstDir, e.Name()))
		if !bytes.Equal(got, book) || filepath.Ext(e.Name()) != ".epub" {
			t.Fatal("staged bytes or name", e.Name())
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dstDir), "escape.epub")); err == nil {
		t.Fatal("a member escaped the staging directory")
	}
}

// corrupt rewrites a local header's declared sizes or CRC to model a lying archive.
func TestExtractZipRejectsLyingOrCorruptMembers(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 1000)
	stored := archive(t, []member{{name: "b.epub", body: body, method: zip.Store}})
	// Corrupt one stored byte: the CRC check at end of data must fail.
	bad := append([]byte{}, stored...)
	i := bytes.Index(bad, body)
	bad[i+10] ^= 0xFF
	src, dst, _ := roots(t, bad)
	x, err := ExtractZip(context.Background(), src, "a.zip", dst, epubOnly, Defaults())
	if err != nil || x.Complete || x.Members[0].Disposition != "failed" {
		t.Fatal("CRC mismatch extracted", x, err)
	}
	if entries, _ := os.ReadDir(dst.Name()); len(entries) != 0 {
		t.Fatal("a failed member was left in staging")
	}
	// A highly compressible member beyond the ratio budget is not extracted.
	bomb := archive(t, []member{{name: "bomb.epub", body: bytes.Repeat([]byte{0}, 4<<20), method: zip.Deflate}})
	src, dst, _ = roots(t, bomb)
	x, err = ExtractZip(context.Background(), src, "a.zip", dst, epubOnly, Defaults())
	if err != nil || dispositions(x)["bomb.epub"] != "suspicious_ratio" {
		t.Fatal(x, err)
	}
	// Per-member and total budgets.
	small := Limits{Members: 10, MemberBytes: 500, TotalBytes: 1500, Ratio: 100}
	many := archive(t, []member{
		{name: "big.epub", body: body, method: zip.Store},
		{name: "a.epub", body: body[:400], method: zip.Store},
		{name: "b.epub", body: body[:400], method: zip.Store},
		{name: "c.epub", body: body[:400], method: zip.Store},
		{name: "d.epub", body: body[:400], method: zip.Store},
	})
	src, dst, _ = roots(t, many)
	x, err = ExtractZip(context.Background(), src, "a.zip", dst, epubOnly, small)
	d := dispositions(x)
	if err != nil || d["big.epub"] != "too_large" || d["a.epub"] != "extracted" || d["c.epub"] != "extracted" || d["d.epub"] != "too_large" || x.Complete {
		t.Fatal("budgets", d, err)
	}
	small.Members = 2
	if _, err := ExtractZip(context.Background(), src, "a.zip", dst, epubOnly, small); err == nil {
		t.Fatal("member count limit ignored")
	}
}

func TestExtractZipRefusesNonArchivesAndCancellation(t *testing.T) {
	src, dst, _ := roots(t, []byte("not a zip"))
	if _, err := ExtractZip(context.Background(), src, "a.zip", dst, epubOnly, Defaults()); err == nil {
		t.Fatal("non-archive accepted")
	}
	src, dst, _ = roots(t, archive(t, []member{{name: "a.epub", body: []byte("abc"), method: zip.Store}}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ExtractZip(ctx, src, "a.zip", dst, epubOnly, Defaults()); err == nil {
		t.Fatal("cancellation ignored")
	}
	if _, err := ExtractZip(context.Background(), src, "missing.zip", dst, epubOnly, Defaults()); err == nil {
		t.Fatal("missing archive accepted")
	}
}

// Regression: archive/zip skips its check when the recorded CRC is zero.
func TestExtractZipChecksCRCEvenWhenRecordedAsZero(t *testing.T) {
	body := bytes.Repeat([]byte("y"), 300)
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	w, err := z.CreateRaw(&zip.FileHeader{Name: "zero.epub", Method: zip.Store, CRC32: 0, CompressedSize64: uint64(len(body)), UncompressedSize64: uint64(len(body))})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	src, dst, _ := roots(t, buf.Bytes())
	x, err := ExtractZip(context.Background(), src, "a.zip", dst, epubOnly, Defaults())
	if err != nil || x.Complete || x.Members[0].Disposition != "failed" || !strings.Contains(x.Members[0].Detail, "CRC") {
		t.Fatal("zero-CRC member extracted unverified", x, err)
	}
}

// A member name is provenance, so a repeated or overlong name is never staged
// under an ambiguous label.
func TestExtractZipRefusesAmbiguousNames(t *testing.T) {
	body := bytes.Repeat([]byte("z"), 200)
	long := strings.Repeat("n", 1100) + ".epub"
	data := archive(t, []member{{name: "same.epub", body: body}, {name: "same.epub", body: body}, {name: long, body: body}})
	src, dst, _ := roots(t, data)
	x, err := ExtractZip(context.Background(), src, "a.zip", dst, epubOnly, Defaults())
	if err != nil {
		t.Fatal(err)
	}
	if x.Members[0].Disposition != "extracted" || x.Members[1].Disposition != "duplicate_name" || x.Members[2].Disposition != "unsafe_name" {
		t.Fatal("ambiguous names staged", x.Members[0].Disposition, x.Members[1].Disposition, x.Members[2].Disposition)
	}
}
