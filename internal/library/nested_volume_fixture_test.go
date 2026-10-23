package library

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
	"github.com/blisspixel/nemalo/internal/staging/zipfixture"
)

func nestedVolumeChildFixture(t *testing.T, kind string, build func([]byte) []byte) streamFixture {
	t.Helper()
	switch kind {
	case "rar4":
		return newRAR4SetCleanupFixture(t, 3, build)
	case "rar5":
		return newRAR5SetCleanupFixture(t, 3, build)
	case "old4":
		return newOldRAR4SetFixture(t, 3, build)
	case "old5":
		return newOldRAR5SetFixture(t, 3, build)
	case "zip_chunks":
		return newStreamFixture(t, 3, build)
	case "zip_segments":
		return newSegmentFixture(t, 3, build)
	case "7z_chunks":
		return newFlatSevenFixture(t, 3, build)
	default:
		t.Fatal("unknown child layout", kind)
		return streamFixture{}
	}
}

// Reuse the existing independently checked child fixtures and their exact reader
// profiles. Outer selection changes only which original files recovery owns.
func newNestedVolumeCleanupFixture(t *testing.T, outer, inner string, count int, build func([]byte) []byte) streamFixture {
	t.Helper()
	child := nestedVolumeChildFixture(t, inner, build)
	var files []nestedFile
	for i := len(child.names) - 1; i >= 0; i-- {
		files = append(files, nestedFile{"shelf/" + filepath.Base(child.names[i]), child.parts[i]})
	}
	if strings.HasSuffix(outer, "_set") {
		factory := newRAR4SetCleanupWithChild
		if strings.Contains(outer, "5") {
			factory = newRAR5SetCleanupWithChild
		}
		f := factory(t, count, "inner.zip", func([]byte) ([]byte, []stagingtest.Archive) {
			body := nestedZIP(t, files...)
			return body, append(append([]stagingtest.Archive{}, child.archives...), stagingtest.Archive{Body: body, Kind: "-tzip"})
		})
		if strings.HasPrefix(outer, "old") {
			v := 4
			if strings.Contains(outer, "5") {
				v = 5
			}
			f = makeOldRARSet(t, f, v)
		}
		// The generated outer holds the child's exact book, not the unused book
		// its ordinary root fixture initialized before calling the builder.
		f.root, f.archiveProof = child.root, archiveVolumeTreeProof
		return f
	}
	for _, name := range child.names {
		if err := os.Remove(filepath.Join(child.folder, filepath.FromSlash(name))); err != nil {
			t.Fatal(err)
		}
	}
	f := child
	f.names, f.parts, f.archiveProof = nil, nil, archiveVolumeTreeProof
	body := nestedZIP(t, files...)
	profile := stagingtest.Archive{Body: body, Kind: "-tzip", Header: fmt.Sprintf("Type = zip\nPhysical Size = %d\n", len(body))}
	switch outer {
	case "7z", "7z_chunks":
		body, profile = cleanupTreeSeven("outer child volumes", files...)
	case "rar4", "rar5":
		v := 4
		if outer == "rar5" {
			v = 5
		}
		body, profile = cleanupTreeRAR(v, files...)
	}
	var roots []stagingtest.Archive
	switch outer {
	case "zip", "7z", "rar4", "rar5":
		ext := outer
		if strings.HasPrefix(outer, "rar") {
			ext = "rar"
		}
		f.names, f.parts = []string{"books/outer." + ext}, [][]byte{body}
		roots = append(roots, profile)
	case "zip_chunks", "7z_chunks":
		ext := "zip"
		if outer == "7z_chunks" {
			ext = "7z"
		}
		for i := range count {
			f.names = append(f.names, fmt.Sprintf("books/outer.%s.%03d", ext, i+1))
			f.parts = append(f.parts, bytes.Clone(body[i*len(body)/count:(i+1)*len(body)/count]))
		}
		profile.Volumes = []stagingtest.Volume{{Name: "archive." + ext, Body: body}}
		roots = append(roots, profile)
	case "zip_segments":
		if count != 3 {
			t.Fatal("this outer-segment fixture requires three parts")
		}
		s := zipfixture.Split(t, body, 100, 200)
		profile.Headers, profile.Fields, profile.Members = map[string]string{}, map[string]string{}, map[string][]byte{}
		for _, file := range files {
			profile.Members[file.name] = file.body
		}
		for _, c := range s.Central {
			name := string(s.Raw[c+46 : c+46+int(binary.LittleEndian.Uint16(s.Raw[c+28:]))])
			profile.Fields[name] = fmt.Sprintf("Volume Index = %d\nOffset = %d\n", binary.LittleEndian.Uint16(s.Raw[c+34:]), binary.LittleEndian.Uint32(s.Raw[c+42:]))
		}
		for i, part := range s.Parts {
			name := fmt.Sprintf("archive.z%02d", i+1)
			original := fmt.Sprintf("books/outer.z%02d", i+1)
			if i+1 == len(s.Parts) {
				name, original = "archive.zip", "books/outer.zip"
			}
			profile.Volumes = append(profile.Volumes, stagingtest.Volume{Name: name, Body: part})
			profile.Headers[name] = fmt.Sprintf("Type = zip\nPhysical Size = %d\nTotal Physical Size = %d\nEmbedded Stub Size = 4\nMultivolume = +\nVolume Index = %d\nVolumes = %d\n", len(part), len(s.Raw), i, len(s.Parts))
			f.names, f.parts = append(f.names, original), append(f.parts, part)
		}
		for _, part := range s.Parts {
			p := profile
			p.Body = part
			roots = append(roots, p)
		}
	default:
		t.Fatal("unknown outer layout", outer)
	}
	f.archives = append(append([]stagingtest.Archive{}, child.archives...), roots...)
	for i, name := range f.names {
		writeTree(t, f.folder, map[string][]byte{name: f.parts[i]})
	}
	stagingtest.InstallMany(t, f.archives...)
	return f
}
