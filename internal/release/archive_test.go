package release

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchivesContainExactExecutableLicenseAndGuide(t *testing.T) {
	binary := []byte("native executable fixture")
	license := []byte("license fixture")
	for _, target := range Targets {
		t.Run(target.OS+"_"+target.Arch, func(t *testing.T) {
			dir := t.TempDir()
			a, err := Archive(dir, "0.1.0-alpha.1", target, binary, license)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(dir, a.Name))
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(data)
			if a.SHA256 != hex.EncodeToString(sum[:]) || a.Bytes != len(data) {
				t.Fatal("incorrect checksum")
			}
			files := map[string][]byte{}
			if target.OS == "windows" {
				z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
				if err != nil {
					t.Fatal(err)
				}
				for _, f := range z.File {
					r, err := f.Open()
					if err != nil {
						t.Fatal(err)
					}
					files[f.Name], err = io.ReadAll(r)
					_ = r.Close()
					if err != nil {
						t.Fatal(err)
					}
				}
			} else {
				g, err := gzip.NewReader(bytes.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}
				defer g.Close()
				r := tar.NewReader(g)
				for {
					header, err := r.Next()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
					files[header.Name], err = io.ReadAll(r)
					if err != nil {
						t.Fatal(err)
					}
					if header.Name == "nemalo" && header.Mode != 0755 {
						t.Fatal("binary not executable")
					}
				}
			}
			name := "nemalo"
			if target.OS == "windows" {
				name += ".exe"
			}
			if len(files) != 3 || !bytes.Equal(files[name], binary) || !bytes.Equal(files["LICENSE"], license) || !strings.Contains(string(files["USAGE.txt"]), "0.1.0-alpha.1") {
				t.Fatal("archive contents changed")
			}
			if _, err := Archive(dir, "0.1.0-alpha.1", target, binary, license); err == nil {
				t.Fatal("archive overwritten")
			}
			second := t.TempDir()
			b, err := Archive(second, "0.1.0-alpha.1", target, binary, license)
			if err != nil || b.SHA256 != a.SHA256 {
				t.Fatal("nonreproducible packaging", err)
			}
		})
	}
	for _, version := range []string{"../escape", "v1.0.0", "", "1.0.0\n"} {
		if _, err := Archive(t.TempDir(), version, Targets[0], binary, license); err == nil {
			t.Fatal("bad version", version)
		}
	}
	if _, err := Archive(t.TempDir(), "1.0.0", Target{"other", "amd64"}, binary, license); err == nil {
		t.Fatal("unsupported target")
	}
	if _, err := Archive(t.TempDir(), "1.0.0", Targets[0], nil, license); err == nil {
		t.Fatal("empty binary")
	}
}
