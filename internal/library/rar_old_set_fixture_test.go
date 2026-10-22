package library

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
)

func newOldRAR4SetFixture(t *testing.T, count int, build func([]byte) []byte) streamFixture {
	return makeOldRARSet(t, newRAR4SetCleanupFixture(t, count, build), 4)
}

func newOldRAR5SetFixture(t *testing.T, count int, build func([]byte) []byte) streamFixture {
	return makeOldRARSet(t, newRAR5SetCleanupFixture(t, count, build), 5)
}

func oldSetPartName(base string, index int) string {
	if index == 0 {
		return base + ".rar"
	}
	return fmt.Sprintf("%s.%c%02d", base, 'r'+(index-1)/100, (index-1)%100)
}

func makeOldRARSet(t *testing.T, f streamFixture, version int) streamFixture {
	t.Helper()
	a := &f.archives[len(f.archives)-1]
	f.archiveProof = fmt.Sprintf("rar%d-old-set/1", version)
	for i, name := range f.names {
		if err := os.Remove(filepath.Join(f.folder, filepath.FromSlash(name))); err != nil {
			t.Fatal(err)
		}
		if version == 4 {
			h := f.parts[i][7:20]
			binary.LittleEndian.PutUint16(h[3:], binary.LittleEndian.Uint16(h[3:])&^0x10)
			binary.LittleEndian.PutUint16(h, uint16(crc32.ChecksumIEEE(h[2:])))
		}
		f.names[i] = oldSetPartName("books/set", i)
		writeTree(t, f.folder, map[string][]byte{f.names[i]: f.parts[i]})
		a.Volumes[i].Name, a.Volumes[i].Body = oldSetPartName("archive", i), f.parts[i]
		header := strings.ReplaceAll(a.Volumes[i].Header, " NewVolName", "")
		if i+1 < len(f.names) {
			next := oldSetPartName("archive", i+1)
			if version == 4 && i >= 3 {
				next = oldSetPartName("archive", i) + ".r00"
			} else if version == 5 && i > 0 && i%100 == 0 {
				next = fmt.Sprintf("archive.%c100", 'r'+(i-1)/100)
			}
			header = strings.ReplaceAll(header, fmt.Sprintf("archive.part%04d.rar", i+2), next)
		}
		a.Volumes[i].Header = header
	}
	stagingtest.InstallMany(t, f.archives...)
	return f
}
