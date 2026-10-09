// Package release creates reproducible native-binary archives and checksums.
package release

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

type Target struct{ OS, Arch string }

var Targets = []Target{{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}, {"windows", "amd64"}, {"windows", "arm64"}}
var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[a-z0-9.]+)?$`)

type Artifact struct {
	Name, SHA256 string
	Bytes        int
}

func Archive(directory, version string, target Target, binary, license, notices []byte) (Artifact, error) {
	var result Artifact
	valid := false
	for _, t := range Targets {
		valid = valid || t == target
	}
	if !valid || !versionPattern.MatchString(version) || len(binary) == 0 || len(binary) > 128<<20 || len(license) == 0 || len(notices) == 0 {
		return result, errors.New("invalid release version, target, binary, or license")
	}
	name := "nemalo"
	if target.OS == "windows" {
		name += ".exe"
	}
	usage := []byte(fmt.Sprintf("Nemalo %s\n\nFind knowledge. Care for it. Realize its potential.\n\nRun ./nemalo help (nemalo.exe help on Windows).\nThis is an early prerelease; see capability limits before handling a library.\nGuide: https://github.com/blisspixel/nemalo/blob/main/docs/DEVELOPMENT.md\nNo downloaded books or personal library data are included.\n", version))
	files := []struct {
		name string
		data []byte
		mode int64
	}{{name, binary, 0755}, {"LICENSE", license, 0644}, {"THIRD_PARTY_NOTICES.txt", notices, 0644}, {"USAGE.txt", usage, 0644}}
	var out bytes.Buffer
	stamp := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	ext := ".tar.gz"
	if target.OS == "windows" {
		ext = ".zip"
		z := zip.NewWriter(&out)
		for _, file := range files {
			header := &zip.FileHeader{Name: file.name, Method: zip.Deflate, Modified: stamp}
			header.SetMode(os.FileMode(file.mode))
			w, err := z.CreateHeader(header)
			if err != nil {
				return result, err
			}
			if _, err = w.Write(file.data); err != nil {
				return result, err
			}
		}
		if err := z.Close(); err != nil {
			return result, err
		}
	} else {
		g := gzip.NewWriter(&out)
		t := tar.NewWriter(g)
		for _, file := range files {
			if err := t.WriteHeader(&tar.Header{Name: file.name, Size: int64(len(file.data)), Mode: file.mode, ModTime: stamp, Typeflag: tar.TypeReg}); err != nil {
				return result, err
			}
			if _, err := t.Write(file.data); err != nil {
				return result, err
			}
		}
		if err := errors.Join(t.Close(), g.Close()); err != nil {
			return result, err
		}
	}
	result.Name = fmt.Sprintf("nemalo_%s_%s_%s%s", version, target.OS, target.Arch, ext)
	result.Bytes = out.Len()
	sum := sha256.Sum256(out.Bytes())
	result.SHA256 = hex.EncodeToString(sum[:])
	if err := WriteExclusive(filepath.Join(directory, result.Name), out.Bytes()); err != nil {
		return Artifact{}, err
	}
	return result, nil
}

func WriteExclusive(name string, data []byte) error {
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	n, writeErr := f.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	syncErr := f.Sync()
	return errors.Join(writeErr, syncErr, f.Close())
}
