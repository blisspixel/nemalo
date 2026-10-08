package inventory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, root, name, body string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestInspectIsReadOnlyAndDoesNotClaimSafety(t *testing.T) {
	dir := t.TempDir()
	fixture(t, dir, "fr/book.EPUB", "test content")
	fixture(t, dir, "audio/001.mp3", "audio")
	fixture(t, dir, "bundle.rar", "archive")
	fixture(t, dir, "repair.par2", "parity")
	fixture(t, dir, "paper.pdf", "pdf")
	fixture(t, dir, "readme.txt", "note")
	limits := Defaults()
	limits.Hashes = true
	r, err := Inspect(context.Background(), dir, limits)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Complete || r.Security != "not_scanned" || len(r.Entries) != 6 {
		t.Fatalf("unexpected report: %+v", r)
	}
	want := sha256.Sum256([]byte("test content"))
	found := false
	for _, entry := range r.Entries {
		if entry.Path == "fr/book.EPUB" {
			found = true
			if entry.Kind != "ebook_candidate" || entry.SHA256 != hex.EncodeToString(want[:]) {
				t.Fatalf("wrong hash/format: %+v", entry)
			}
		}
	}
	if !found {
		t.Fatal("nested book missing")
	}
	body, err := os.ReadFile(filepath.Join(dir, "fr", "book.EPUB"))
	if err != nil || string(body) != "test content" {
		t.Fatalf("source changed: %q %v", body, err)
	}
	r, err = Inspect(context.Background(), dir, Defaults())
	if err != nil || r.BytesRead != 0 {
		t.Fatalf("default inventory opened files: %+v %v", r, err)
	}
	for _, entry := range r.Entries {
		if entry.SHA256 != "" {
			t.Fatal("hashing without opt-in")
		}
	}
}

func TestInventoryLimitsAreIncomplete(t *testing.T) {
	dir := t.TempDir()
	fixture(t, dir, "a.epub", "123456")
	fixture(t, dir, "nested/deeper/book.epub", "1234")
	for _, change := range []func(*Limits){func(l *Limits) { l.Entries = 1 }, func(l *Limits) { l.Depth = 1 }, func(l *Limits) { l.Hashes = true; l.FileBytes = 3 }, func(l *Limits) { l.Hashes = true; l.TotalBytes = 5 }} {
		l := Defaults()
		change(&l)
		r, err := Inspect(context.Background(), dir, l)
		if err == nil || r.Complete {
			t.Fatalf("limit treated as success: %+v %v", r, err)
		}
		if r.BytesRead > l.TotalBytes {
			t.Fatal("read budget exceeded")
		}
	}
	for _, l := range []Limits{{}, {Entries: 100001, Depth: 1, FileBytes: 1, TotalBytes: 1}, {Entries: 1, Depth: 257, FileBytes: 1, TotalBytes: 1}} {
		if _, err := Inspect(context.Background(), dir, l); err == nil {
			t.Fatal("invalid limits accepted")
		}
	}
}

func TestInvalidRootsAndCancellation(t *testing.T) {
	dir := t.TempDir()
	fixture(t, dir, "file", "content")
	for _, path := range []string{filepath.Join(dir, "missing"), filepath.Join(dir, "file")} {
		if _, err := Inspect(context.Background(), path, Defaults()); err == nil {
			t.Fatal("invalid root accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := Inspect(ctx, dir, Defaults())
	if err == nil || r.Complete {
		t.Fatal("cancelled inventory succeeded")
	}
}

func TestSymlinksNeverReadOutsideRoot(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	fixture(t, outside, "secret.epub", "private")
	link := filepath.Join(dir, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	l := Defaults()
	l.Hashes = true
	r, err := Inspect(context.Background(), dir, l)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Entries) != 1 || r.Entries[0].Kind != "link" || r.BytesRead != 0 {
		t.Fatalf("followed link: %+v", r)
	}
	if _, err := Inspect(context.Background(), link, l); err == nil {
		t.Fatal("symlink root accepted")
	}
}

func TestHashRejectsChangedOrMissingFile(t *testing.T) {
	dir := t.TempDir()
	fixture(t, dir, "a", "before")
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	before, err := root.Lstat("a")
	if err != nil {
		t.Fatal(err)
	}
	fixture(t, dir, "a", "after and larger")
	var read int64
	if _, err := hash(context.Background(), root, "a", before, 100, &read); err == nil {
		t.Fatal("changed file accepted")
	}
	if _, err := hash(context.Background(), root, "missing", before, 100, &read); err == nil {
		t.Fatal("missing file accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := hash(ctx, root, "a", before, 100, &read); err == nil {
		t.Fatal("cancellation ignored")
	}
	if _, err := hash(context.Background(), root, ".", before, 100, &read); err == nil {
		t.Fatal("directory accepted")
	}
}

func TestKinds(t *testing.T) {
	for name, want := range map[string]string{"a.m4b": "audio_candidate", "a.flac": "audio_candidate", "a.ogg": "audio_candidate", "a.zip": "archive_candidate", "a.7z": "archive_candidate", "a.tar": "archive_candidate", "a.gz": "archive_candidate", "a.bin": "other"} {
		if got := kind(name); got != want {
			t.Fatalf("%s: %s", name, got)
		}
	}
}
