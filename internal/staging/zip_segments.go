package staging

import (
	"archive/zip"
	"context"
	"encoding/binary"
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/blisspixel/nemalo/internal/safeio"
)

// snapshotSegments reads only the checked private copies. A read holds one
// file descriptor at a time, even when a set contains a thousand parts.
type snapshotSegments struct {
	root   *os.Root
	names  []string
	infos  []fs.FileInfo
	starts []int64 // Includes the end of the last part.
}

func (s *snapshotSegments) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fs.ErrInvalid
	}
	n := 0
	for len(p) != 0 {
		i := sort.Search(len(s.names), func(i int) bool { return s.starts[i+1] > off })
		if i == len(s.names) {
			return n, io.EOF
		}
		f, err := safeio.OpenRegular(s.root, s.names[i], s.infos[i], os.O_RDONLY)
		if err != nil {
			return n, err
		}
		before, err := f.Stat()
		if err != nil || before.Size() != s.infos[i].Size() || !before.ModTime().Equal(s.infos[i].ModTime()) {
			f.Close()
			return n, fs.ErrInvalid
		}
		want := min(int64(len(p)), s.starts[i+1]-off)
		got, err := f.ReadAt(p[:int(want)], off-s.starts[i])
		after, statErr := f.Stat()
		f.Close()
		n += got
		off += int64(got)
		p = p[got:]
		if err != nil {
			return n, err
		}
		if statErr != nil || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
			return n, fs.ErrInvalid
		}
		if int64(got) != want {
			return n, io.ErrUnexpectedEOF
		}
	}
	return n, nil
}

func zipSegmentsAccounted(ctx context.Context, root *os.Root, names []string, entries []sevenEntry) (bool, string) {
	s := &snapshotSegments{root: root, names: names, starts: []int64{0}}
	for _, name := range names {
		info, err := root.Lstat(name)
		if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxArchiveSnapshot-s.starts[len(s.starts)-1] {
			return false, "the private ZIP segments are unreadable or exceed their limits"
		}
		s.infos = append(s.infos, info)
		s.starts = append(s.starts, s.starts[len(s.starts)-1]+info.Size())
	}
	return zipSegmentProof(ctx, s, s.starts, entries)
}

type zipPatch struct {
	off  int64
	data []byte
}

// The view drops only the verified initial marker. Patches translate already
// validated references; all other original bytes remain in their exact order.
type zipSegmentView struct {
	source  io.ReaderAt
	patches []zipPatch
}

func (v *zipSegmentView) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off > (4<<30)-4 {
		return 0, fs.ErrInvalid
	}
	n, err := v.source.ReadAt(p, off+4)
	first := sort.Search(len(v.patches), func(i int) bool { return v.patches[i].off+int64(len(v.patches[i].data)) > off })
	for _, patch := range v.patches[first:] {
		if patch.off >= off+int64(n) {
			break
		}
		start, end := max(off, patch.off), min(off+int64(n), patch.off+int64(len(patch.data)))
		copy(p[start-off:end-off], patch.data[start-patch.off:end-patch.off])
	}
	return n, err
}

func zipSegmentProof(ctx context.Context, source io.ReaderAt, starts []int64, entries []sevenEntry) (bool, string) {
	refused := "the ZIP segments have inconsistent disk records, unaccounted bytes, unsupported metadata, or exhausted proof limits"
	parts := len(starts) - 1
	if parts < 2 || parts > 1000 || starts[0] != 0 || len(entries) < 1 || len(entries) > 10000 {
		return false, refused
	}
	for i := 0; i < parts; i++ {
		if starts[i+1] <= starts[i] || starts[i+1] > 4<<30 {
			return false, refused
		}
	}
	size := starts[parts]
	if starts[1] < 4 || size-starts[parts-1] < 22 {
		return false, refused
	}
	r := &zipProofReader{ctx: ctx, source: source, remaining: zipProofBytes}
	var marker [4]byte
	if _, err := r.ReadAt(marker[:], 0); err != nil || string(marker[:]) != "PK\x07\x08" {
		return false, refused
	}
	end := make([]byte, 22)
	if _, err := r.ReadAt(end, size-22); err != nil {
		return false, refused
	}
	u16, u32 := binary.LittleEndian.Uint16, binary.LittleEndian.Uint32
	count, dirDisk := int(u16(end[10:])), int(u16(end[6:]))
	dirSize, dirOff := int64(u32(end[12:])), int64(u32(end[16:]))
	if u32(end) != 0x06054b50 || int(u16(end[4:])) != parts-1 || dirDisk >= parts || count != len(entries) ||
		u16(end[20:]) != 0 || dirSize == 0xffffffff || dirOff == 0xffffffff || dirOff >= starts[dirDisk+1]-starts[dirDisk] {
		return false, refused
	}
	dir := starts[dirDisk] + dirOff
	if dir < 4 || dir+dirSize != size-22 {
		return false, refused
	}
	listed := make(map[string]sevenEntry, count)
	for _, e := range entries {
		if _, duplicate := listed[e.name]; duplicate || e.malformed || !e.valid {
			return false, refused
		}
		listed[e.name] = e
	}
	v := &zipSegmentView{source: r}
	patch := func(off int64, width int, value uint32) {
		data := make([]byte, width)
		if width == 2 {
			binary.LittleEndian.PutUint16(data, uint16(value))
		} else {
			binary.LittleEndian.PutUint32(data, value)
		}
		v.patches = append(v.patches, zipPatch{off: off - 4, data: data})
	}
	// Every header, including variable metadata, must fit its original disk.
	diskAt := func(pos int64) int { return sort.Search(parts, func(i int) bool { return starts[i+1] > pos }) }
	pos, onLast := dir, 0
	central, local := make([]byte, 46), make([]byte, 30)
	for i := 0; i < count; i++ {
		disk := diskAt(pos)
		if disk == parts || pos > starts[disk+1]-46 || pos > size-22-46 {
			return false, refused
		}
		if _, err := r.ReadAt(central, pos); err != nil || u32(central) != 0x02014b50 {
			return false, refused
		}
		nameLen := int64(u16(central[28:]))
		length := 46 + nameLen + int64(u16(central[30:])) + int64(u16(central[32:]))
		localDisk, localOff := int(u16(central[34:])), int64(u32(central[42:]))
		if nameLen > 1024 || length > starts[disk+1]-pos || length > size-22-pos || localDisk >= parts ||
			localOff == 0xffffffff || localOff > starts[localDisk+1]-starts[localDisk]-30 {
			return false, refused
		}
		name := make([]byte, int(nameLen))
		if _, err := r.ReadAt(name, pos+46); err != nil {
			return false, refused
		}
		header := zip.FileHeader{Name: string(name), CreatorVersion: u16(central[4:]), ExternalAttrs: u32(central[38:])}
		key := string(name)
		e, ok := listed[key]
		if !ok && header.Mode().IsDir() && strings.HasSuffix(key, "/") {
			// The maintained reader omits the final slash on directory names.
			// Permit precisely that representation, still requiring its raw
			// directory type and unique one-to-one membership below.
			key = strings.TrimSuffix(key, "/")
			e, ok = listed[key]
		}
		// 7-Zip can omit CRC for a directory. Only a raw zero-length,
		// zero-packed directory with zero CRC can supply that exact fact.
		crcKnown := e.crcKnown || (e.folder && e.size == 0 && e.packed == 0 && e.crc == 0 && u32(central[16:]) == 0)
		if !ok || !e.sizeKnown || !e.packedKnown || !crcKnown || !e.volumeKnown || !e.offsetKnown ||
			e.size != int64(u32(central[24:])) || e.packed != int64(u32(central[20:])) || e.crc != u32(central[16:]) ||
			e.volume != int64(localDisk) || e.offset != localOff || e.folder != header.Mode().IsDir() || e.link != (header.Mode()&fs.ModeSymlink != 0) {
			return false, refused
		}
		delete(listed, key)
		localPos := starts[localDisk] + localOff
		if localPos < 4 || localPos > dir-30 {
			return false, refused
		}
		if _, err := r.ReadAt(local, localPos); err != nil || u32(local) != 0x04034b50 ||
			30+int64(u16(local[26:]))+int64(u16(local[28:])) > starts[localDisk+1]-localPos {
			return false, refused
		}
		patch(pos+34, 2, 0)
		patch(pos+42, 4, uint32(localPos-4))
		if disk == parts-1 {
			onLast++
		}
		pos += length
	}
	if pos != size-22 || int(u16(end[8:])) != onLast || len(listed) != 0 {
		return false, refused
	}
	patch(size-22+4, 2, 0)
	patch(size-22+6, 2, 0)
	patch(size-22+8, 2, uint32(count))
	patch(size-22+16, 4, uint32(dir-4))
	// ZIPAccounted's reads flow through r, so raw preflight and the maintained
	// reader/framing check consume one lifetime physical-read allowance.
	if ok, _ := ZIPAccounted(ctx, v, size-4); !ok {
		return false, refused
	}
	return true, ""
}
