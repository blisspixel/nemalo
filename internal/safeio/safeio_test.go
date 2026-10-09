package safeio

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestBoundRootAndRegularFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "child"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "child", "book"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	child, err := OpenRootIn(root, "child")
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	before, err := child.Lstat("book")
	if err != nil {
		t.Fatal(err)
	}
	f, err := OpenRegular(child, "book", before, os.O_RDONLY)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if _, err := OpenRegular(child, ".", nil, os.O_RDONLY); err == nil {
		t.Fatal("directory admitted")
	}
	if _, err := OpenRegular(child, "absent", nil, os.O_RDONLY); err == nil {
		t.Fatal("missing admitted")
	}
	if _, err := OpenRootIn(root, "child/book"); err == nil {
		t.Fatal("file root admitted")
	}
	if _, err := OpenRoot(filepath.Join(dir, "child", "book")); err == nil {
		t.Fatal("file root admitted")
	}
	if _, err := OpenRoot(filepath.Join(dir, "absent")); err == nil {
		t.Fatal("missing root admitted")
	}
	if _, err := OpenRootIn(root, "absent"); err == nil {
		t.Fatal("missing child admitted")
	}
	if err := child.Rename("book", "old"); err != nil {
		t.Fatal(err)
	}
	if err := child.WriteFile("book", []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenRegular(child, "book", before, os.O_RDONLY); err == nil {
		t.Fatal("replacement admitted")
	}
}

func TestDirectoryReplacementBetweenValidationAndAcquisition(t *testing.T) {
	parent := t.TempDir()
	name := filepath.Join(parent, "selected")
	if err := os.Mkdir(name, 0700); err != nil {
		t.Fatal(err)
	}
	before, err := Lstat(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(name, name+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(name, 0700); err != nil {
		t.Fatal(err)
	}
	if root, err := openRoot(name, before); err == nil {
		_ = root.Close()
		t.Fatal("replacement root admitted")
	}
	root, err := OpenRoot(name + "-original")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.Rename(name+"-original", name+"-moved"); err != nil {
		if runtime.GOOS != "windows" {
			t.Fatal(err)
		}
		// Windows directory handles deny the rename itself.
		if err := root.WriteFile("held", []byte("bound"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(name+"-original", "held")); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := root.WriteFile("held", []byte("bound"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(name, "held")); !os.IsNotExist(err) {
		t.Fatal("write redirected to replacement", err)
	}
	if _, err := os.Stat(filepath.Join(name+"-moved", "held")); err != nil {
		t.Fatal("held directory lost", err)
	}
}

func TestRootLinkReplacementAndDescriptorMismatch(t *testing.T) {
	parent := t.TempDir()
	name := filepath.Join(parent, "selected")
	other := filepath.Join(parent, "other")
	for _, p := range []string{name, other} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := Lstat(name)
	root, err := OpenRoot(other)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := checkRoot(root, before); err == nil {
		t.Fatal("mismatched descriptor admitted")
	}
	if err := os.Rename(name, name+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, name); err != nil {
		t.Skip("symlink privilege unavailable", err)
	}
	if root, err := openRoot(name, before); err == nil {
		_ = root.Close()
		t.Fatal("raced link admitted")
	}
	if _, err := OpenRoot(name); err == nil {
		t.Fatal("static link admitted")
	}
	alias, err := OpenResolvedRoot(name)
	if err != nil {
		t.Fatal("explicit alias rejected", err)
	}
	defer alias.Close()
	if err := alias.WriteFile("bound", []byte("alias target"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(other, "bound")); err != nil {
		t.Fatal(err)
	}
}
