package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileCommandsPreserveDirectoryAliases(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "real")
	alias := filepath.Join(parent, "alias")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "book.pdf"), []byte("%PDF-1.7\n%%EOF"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dir, alias); err != nil {
		t.Skip("directory symlink unavailable", err)
	}
	file := filepath.Join(dir, "catalog.json")
	if code, out, _ := execute(t, []string{"library", "snapshot", dir, "--output", filepath.Join(parent, "snapshot.json")}, nil); code != 0 {
		t.Fatal(code, out)
	}
	data, err := os.ReadFile(filepath.Join(parent, "snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{dir, alias} {
		for _, args := range [][]string{{"check", filepath.Join(prefix, "book.pdf"), "--json"}, {"library", "list", filepath.Join(prefix, "catalog.json"), "--json"}} {
			if code, out, stderr := execute(t, args, nil); code != 0 {
				t.Fatal(args, code, out, stderr)
			}
		}
	}
	if err := os.Symlink(filepath.Join(dir, "book.pdf"), filepath.Join(dir, "linked.pdf")); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := execute(t, []string{"check", filepath.Join(alias, "linked.pdf")}, nil); code != 1 {
		t.Fatal("final file symlink admitted", code)
	}
}
