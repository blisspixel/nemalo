package library

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportCopiesVerifiedBytesAndNeverOverwrites(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	book := importUnscanned(t, root, "Export")[0]
	stored := mustRead(t, filepath.Join(root, filepath.FromSlash(book.Path)))
	dest := t.TempDir()
	r, err := Export(ctx, ExportRequest{Library: root, AssetID: book.AssetID, Directory: dest, ReadOnly: true})
	if err != nil || r.SHA256 != book.SHA256 || !r.ReadOnly || r.Title != "Test" || filepath.Dir(r.Path) != dest || !strings.HasSuffix(r.Path, "Test-"+book.SHA256[:8]+".epub") {
		t.Fatal(r, err)
	}
	if got := mustRead(t, r.Path); !bytes.Equal(got, stored) {
		t.Fatal("exported bytes differ")
	}
	if info, err := os.Stat(r.Path); err != nil || info.Mode().Perm()&0o222 != 0 {
		t.Fatal("handoff copy is writable", info.Mode(), err)
	}
	if _, err := Export(ctx, ExportRequest{Library: root, AssetID: book.AssetID, Directory: dest}); err == nil || !strings.Contains(err.Error(), "never overwrites") {
		t.Fatal("existing export overwritten", err)
	}
	if got := mustRead(t, filepath.Join(root, filepath.FromSlash(book.Path))); !bytes.Equal(got, stored) {
		t.Fatal("stored file changed")
	}
}

func TestHandoffReusesIdenticalCopiesAndKeepsChangedOnes(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	book := importUnscanned(t, root, "Handoff")[0]
	dest := t.TempDir()
	req := ExportRequest{Library: root, AssetID: book.AssetID, Directory: dest, ReadOnly: true, Reuse: true}
	first, err := Export(ctx, req)
	if err != nil || first.Reused {
		t.Fatal(first, err)
	}
	again, err := Export(ctx, req)
	if err != nil || !again.Reused || again.Path != first.Path {
		t.Fatal("identical copy not reused", again, err)
	}
	// A reader writes annotations into its copy, as calibre does for EPUBs.
	if err := os.Chmod(first.Path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first.Path, []byte("annotated by a reader"), 0o644); err != nil {
		t.Fatal(err)
	}
	fresh, err := Export(ctx, req)
	if err != nil || fresh.Reused || fresh.Path == first.Path || !strings.HasSuffix(fresh.Path, "-2.epub") {
		t.Fatal("changed copy reused", fresh, err)
	}
	if got := mustRead(t, first.Path); string(got) != "annotated by a reader" {
		t.Fatal("the reader's changed copy was not preserved")
	}
}

func TestExportRefusesDamagedOrUnknownHoldings(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	book := importUnscanned(t, root, "Damaged")[0]
	dest := t.TempDir()
	for _, req := range []ExportRequest{
		{Library: root, AssetID: "sha256:" + strings.Repeat("0", 64), Directory: dest},
		{Library: root, AssetID: "bad", Directory: dest},
		{Library: root, AssetID: book.AssetID},
		{Library: t.TempDir(), AssetID: book.AssetID, Directory: dest},
		{Library: root, AssetID: book.AssetID, Directory: filepath.Join(dest, "missing")},
	} {
		if _, err := Export(ctx, req); err == nil {
			t.Fatal("invalid export accepted", req)
		}
	}
	stored := filepath.Join(root, filepath.FromSlash(book.Path))
	if err := os.WriteFile(stored, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Export(ctx, ExportRequest{Library: root, AssetID: book.AssetID, Directory: dest}); err == nil || !strings.Contains(err.Error(), "library audit") {
		t.Fatal("changed stored bytes exported", err)
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Fatal("a partial copy was left behind", entries)
	}
}

func TestExportStemIsAPortableName(t *testing.T) {
	hash := strings.Repeat("ab", 32)
	for title, want := range map[string]string{
		"Le Petit Prince":        "Le-Petit-Prince-abababab",
		"../../etc/passwd":       "etc-passwd-abababab",
		`C:\Windows\System32`:    "C-Windows-System32-abababab",
		"日本語の本":                  "日本語の本-abababab",
		"":                       hash[:16],
		"...":                    hash[:16],
		"a\x00b\nc":              "abc-abababab",
		strings.Repeat("x", 300): strings.Repeat("x", 80) + "-abababab",
	} {
		if got := exportStem(title, hash); got != want {
			t.Fatalf("%q: %q, want %q", title, got, want)
		}
	}
}
