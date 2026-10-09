package release

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNoticesPreserveLicensesAndDeterministicModuleIdentity(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	for name, text := range map[string]string{"LICENSE.txt": "copyright and full license\n", "NOTICE": "required upstream notice", "README.md": "not a notice"} {
		if err := os.WriteFile(filepath.Join(a, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(b, "COPYING"), []byte("other license"), 0600); err != nil {
		t.Fatal(err)
	}
	deps := []Dependency{{"example.org/z", "v1.0.0", a}, {"example.org/a", "v2.0.0", b}}
	before := append([]Dependency(nil), deps...)
	notices, err := Notices("go1.27.2", []byte("Go copyright/license"), deps)
	if err != nil {
		t.Fatal(err)
	}
	s := string(notices)
	for _, want := range []string{"go1.27.2", "Go copyright/license", "copyright and full license\n", "required upstream notice", "other license", "example.org/a v2.0.0", "example.org/z v1.0.0"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(s, "not a notice") || strings.Index(s, "example.org/a") > strings.Index(s, "example.org/z") || !reflect.DeepEqual(before, deps) {
		t.Fatal("unexpected data, unstable ordering, or caller mutation")
	}
	again, err := Notices("go1.27.2", []byte("Go copyright/license"), []Dependency{deps[1], deps[0]})
	if err != nil || !bytes.Equal(notices, again) {
		t.Fatal("notice output depends on input ordering", err)
	}
}

func TestNoticesFailClosedOnMissingOrInvalidLicenses(t *testing.T) {
	empty, directory, zero, oversized := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "LICENSE"), 0700); err != nil {
		t.Fatal(err)
	}
	for dir, data := range map[string][]byte{zero: {}, oversized: make([]byte, (1<<20)+1)} {
		if err := os.WriteFile(filepath.Join(dir, "LICENSE"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	licensed := t.TempDir()
	if err := os.WriteFile(filepath.Join(licensed, "LICENSE"), []byte("license"), 0600); err != nil {
		t.Fatal(err)
	}
	valid := Dependency{"example.org/a", "v1.0.0", licensed}
	for _, deps := range [][]Dependency{{{}}, {valid, valid}, {{"a", "v1", empty}}, {{"a", "v1", directory}}, {{"a", "v1", zero}}, {{"a", "v1", oversized}}, {{"a", "v1", filepath.Join(empty, "missing")}}} {
		if _, err := Notices("go1", []byte("license"), deps); err == nil {
			t.Fatal("invalid dependency accepted", deps)
		}
	}
	if _, err := Notices("", []byte("license"), nil); err == nil {
		t.Fatal("empty toolchain accepted")
	}
	if _, err := Notices("go1", nil, nil); err == nil {
		t.Fatal("missing Go license accepted")
	}
	if _, err := Archive(t.TempDir(), "1.0.0", Targets[0], []byte("binary"), []byte("license"), nil); err == nil {
		t.Fatal("missing notices accepted")
	}
}
