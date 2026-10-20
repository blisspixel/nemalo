package safeio

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/testfs"
)

func TestWindowsReparseResolvedParentAndLongAliases(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(t.TempDir(), strings.Repeat("long", 35), strings.Repeat("path", 35))
	child := filepath.Join(target, "child")
	if err := os.MkdirAll(child, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(parent, "alias")
	testfs.Junction(t, alias, target)
	// Explicit aliases resolve to a physical path that exceeds MAX_PATH.
	r, err := OpenResolvedRoot(alias)
	if err != nil {
		t.Fatal("long junction target refused", err)
	}
	defer r.Close()
	if err := r.WriteFile("child/bound", []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	// A plain final directory selected through a parent alias stays supported.
	plain, err := OpenRoot(filepath.Join(alias, "child"))
	if err != nil {
		t.Fatal("plain child through explicit parent alias refused", err)
	}
	defer plain.Close()
	if got, err := plain.ReadFile("bound"); err != nil || string(got) != "original" {
		t.Fatal("parent alias lost original child", string(got), err)
	}
	chain := filepath.Join(parent, "chain")
	testfs.Symlink(t, chain, alias)
	resolved, err := OpenResolvedRoot(chain)
	if err != nil {
		t.Fatal("explicit symbolic link to junction refused", err)
	}
	resolved.Close()
	if r, err := OpenResolvedRoot(filepath.Join(alias, "child", "bound")); err == nil {
		r.Close()
		t.Fatal("resolved file admitted as directory")
	}
	if r, err := OpenResolvedRoot(filepath.Join(alias, "missing")); err == nil {
		r.Close()
		t.Fatal("missing resolved path admitted")
	}
}

func TestWindowsReparseRootContracts(t *testing.T) {
	for _, kind := range []string{"junction", "directory_symlink"} {
		t.Run(kind, func(t *testing.T) {
			parent, outside := t.TempDir(), t.TempDir()
			link := filepath.Join(parent, "alias")
			if kind == "junction" {
				testfs.Junction(t, link, outside)
			} else {
				testfs.Symlink(t, link, outside)
			}
			before, err := Lstat(link)
			wantMode := os.ModeSymlink
			if kind == "junction" {
				wantMode = os.ModeIrregular
			}
			if err != nil || before.Mode()&wantMode == 0 || before.IsDir() || before.Mode().IsRegular() {
				t.Fatal("unexpected native reparse classification", before, err)
			}
			if r, err := OpenRoot(link); err == nil {
				r.Close()
				t.Fatal("strict root admitted a reparse point")
			}
			r, err := OpenRoot(parent)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			if child, err := OpenRootIn(r, "alias"); err == nil {
				child.Close()
				t.Fatal("strict child root admitted a reparse point")
			}
			alias, err := OpenResolvedRoot(link)
			if err != nil {
				t.Fatal("explicit resolved alias refused", err)
			}
			defer alias.Close()
			if err := alias.WriteFile("selected", []byte("explicit"), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(outside, "selected"))
			if err != nil || string(got) != "explicit" {
				t.Fatal("explicit alias lost its target", string(got), err)
			}
		})
	}
}

func TestWindowsReparseReplacementAndConfinement(t *testing.T) {
	parent, outside := t.TempDir(), t.TempDir()
	selected := filepath.Join(parent, "selected")
	if err := os.Mkdir(selected, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "canary"), []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := Lstat(selected)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(selected, selected+"-original"); err != nil {
		t.Fatal(err)
	}
	testfs.Junction(t, selected, outside)
	if r, err := openRoot(selected, before); err == nil {
		r.Close()
		t.Fatal("junction replacement admitted after validation")
	}
	r, err := OpenRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, name := range []string{"selected/canary", "selected/../canary"} {
		if f, err := OpenRegular(r, name, nil, os.O_RDWR); err == nil {
			f.Close()
			t.Fatal("outside junction traversed", name)
		}
	}
	if err := r.WriteFile("selected/new", []byte("redirected"), 0600); err == nil {
		t.Fatal("outside write traversed junction")
	}
	// An already acquired directory either prevents relocation on Windows or
	// remains bound to the held directory; neither result may redirect writes.
	held, err := OpenRoot(selected + "-original")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	name := selected + "-original"
	if err := os.Rename(name, name+"-moved"); err == nil {
		testfs.Junction(t, name, outside)
		name += "-moved"
	} else {
		t.Logf("held native directory prevented replacement: %v", err)
	}
	if err := held.WriteFile("bound", []byte("held"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(name, "bound")); err != nil || string(got) != "held" {
		t.Fatal("held capability lost", string(got), err)
	}
	if got, err := os.ReadFile(filepath.Join(outside, "canary")); err != nil || string(got) != "unchanged" {
		t.Fatal("outside canary changed", string(got), err)
	}
	for _, name := range []string{"bound", "new"} {
		if _, err := os.Stat(filepath.Join(outside, name)); !os.IsNotExist(err) {
			t.Fatal("outside file created", name, err)
		}
	}
}

func TestWindowsReparseFileIdentity(t *testing.T) {
	dir := t.TempDir()
	selected := filepath.Join(dir, "book")
	if err := os.WriteFile(selected, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	before, err := r.Lstat("book")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(selected, selected+"-original"); err != nil {
		t.Fatal(err)
	}
	// A relative within-root link can still reference the original identity.
	testfs.Symlink(t, selected, "book-original")
	f, err := OpenRegular(r, "book", before, os.O_RDONLY)
	// The link resolves to the exact original identity, which is permitted by
	// OpenRegular. Inventory separately refuses to follow link entries.
	if err != nil {
		t.Fatal("same original identity lost", err)
	}
	got, err := io.ReadAll(f)
	f.Close()
	if err != nil || string(got) != "original" {
		t.Fatal(string(got), err)
	}
	if err := os.Remove(selected); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "other")
	if err := os.WriteFile(other, []byte("different"), 0600); err != nil {
		t.Fatal(err)
	}
	testfs.Symlink(t, selected, "other")
	if f, err := OpenRegular(r, "book", before, os.O_RDONLY); err == nil {
		f.Close()
		t.Fatal("different identity admitted through a symlink")
	}
	if err := os.Remove(selected); err != nil {
		t.Fatal(err)
	}
	canary := filepath.Join(t.TempDir(), "canary")
	if err := os.WriteFile(canary, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	testfs.Symlink(t, selected, canary)
	if f, err := OpenRegular(r, "book", nil, os.O_RDWR); err == nil {
		f.Close()
		t.Fatal("outside file symlink admitted")
	}
	if got, err := os.ReadFile(canary); err != nil || string(got) != "outside" {
		t.Fatal("outside file changed", string(got), err)
	}
}
