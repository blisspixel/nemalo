package library

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
)

func TestRARReadOnlyCoverageDoesNotAuthorizeARenamedSource(t *testing.T) {
	root, folder, book := archiveCleanup(t, nil)
	data := append([]byte("Rar!\x1a\x07\x01\x00"), cleanupRAR5Block(1, "", nil)...)
	data = append(data, cleanupRAR5Block(2, "book.epub", book)...)
	data = append(data, cleanupRAR5Block(5, "", nil)...)
	writeTree(t, folder, map[string][]byte{"renamed.7z": data})
	stagingtest.Install(t, stagingtest.Archive{Kind: "-tRar5", Body: data, Members: map[string][]byte{"book.epub": book}})
	p, err := Cleanup(context.Background(), CleanupRequest{Library: root, Folder: folder})
	if err != nil || p.Move != 0 || p.Keep != 1 {
		t.Fatal("read-only proof enabled RAR source cleanup", p, err)
	}
	if got, err := os.ReadFile(filepath.Join(folder, "renamed.7z")); err != nil || !bytes.Equal(got, data) {
		t.Fatal("renamed source changed", err)
	}
}
