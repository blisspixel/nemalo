package staging

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"strings"
)

var ErrRAR4Coverage = errors.New("RAR4 framing is unsupported or unaccounted")

// RAR4File contains declared facts, never evidence of extracted integrity.
// HeaderOffset through DataOffset+Packed partitions the original container.
type RAR4File struct {
	Name                                     string
	HeaderOffset, DataOffset                 int64
	Bytes, Packed                            int64
	CRC32, Attributes, ModifiedDOS           uint32
	Directory                                bool
	HostOS, UnpackVersion, CompressionMethod byte
}

// RAR4Accounted measures a strict standalone framing subset without reading
// payloads, decompressing, checking file contents or granting write permission.
// A future shared-tree caller must also wrap source with Limits.proofReader.
func RAR4Accounted(ctx context.Context, source io.ReaderAt, size int64) ([]RAR4File, error) {
	fail := func(reason string) ([]RAR4File, error) { return nil, fmt.Errorf("%w: %s", ErrRAR4Coverage, reason) }
	if size < 7 || size > maxArchiveSnapshot {
		return fail("source size exceeds supported bounds")
	}
	r := &coverageReaderAt{ctx: ctx, source: source, remaining: archiveProofBytes}
	signature := make([]byte, 7)
	if _, err := r.ReadAt(signature, 0); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRAR4Coverage, err)
	}
	if !bytes.Equal(signature, []byte("Rar!\x1a\x07\x00")) {
		return fail("missing initial signature")
	}
	files := []RAR4File{}
	names := map[string]bool{}
	main := false
	for off, blocks := int64(7), 0; off < size && blocks < 10002; blocks++ {
		if size-off < 7 {
			return fail("truncated block prefix")
		}
		prefix := make([]byte, 7)
		if _, err := r.ReadAt(prefix, off); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrRAR4Coverage, err)
		}
		headerSize := int(binary.LittleEndian.Uint16(prefix[5:7]))
		if headerSize < 7 || int64(headerSize) > size-off {
			return fail("invalid header size")
		}
		h := make([]byte, headerSize)
		copy(h, prefix)
		if headerSize > 7 {
			if _, err := r.ReadAt(h[7:], off+7); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrRAR4Coverage, err)
			}
		}
		if binary.LittleEndian.Uint16(h[:2]) != uint16(crc32.ChecksumIEEE(h[2:])) {
			return fail("header checksum mismatch")
		}
		kind, flags := h[2], binary.LittleEndian.Uint16(h[3:5])
		if !main && kind != 0x73 {
			return fail("missing main header")
		}
		next := off + int64(headerSize)
		switch kind {
		case 0x73:
			// Lock, solid and skip-if-unknown have no additional data here.
			if main || headerSize != 13 || flags&^uint16(0x400c) != 0 || !bytes.Equal(h[7:], make([]byte, 6)) {
				return fail("unsupported main metadata or volume")
			}
			main = true
		case 0x74:
			// Known framing flags plus zero high sizes and bounded time metadata.
			if len(files) >= 10000 || headerSize < 33 || flags&^uint16(0xd1f0) != 0 || flags&0x8000 == 0 {
				return fail("unsupported file flags or member count")
			}
			nameOffset := 32
			if flags&0x100 != 0 {
				if headerSize < 40 || binary.LittleEndian.Uint32(h[32:36]) != 0 || binary.LittleEndian.Uint32(h[36:40]) != 0 {
					return fail("missing or unsupported high-size fields")
				}
				nameOffset = 40
			}
			nameSize := int(binary.LittleEndian.Uint16(h[26:28]))
			if nameSize < 1 || nameSize > 1024 || nameOffset+nameSize > headerSize {
				return fail("unsupported file metadata or name length")
			}
			nameEnd := nameOffset + nameSize
			extra := h[nameEnd:]
			if flags&0x1000 != 0 {
				if !rar4TimeMetadata(extra) {
					return fail("missing or unsupported extended-time metadata")
				}
			} else if len(extra) != 0 {
				return fail("unsupported trailing file metadata")
			}
			f := RAR4File{
				Name: string(h[nameOffset:nameEnd]), HeaderOffset: off, DataOffset: next,
				Packed:            int64(binary.LittleEndian.Uint32(h[7:11])),
				Bytes:             int64(binary.LittleEndian.Uint32(h[11:15])),
				HostOS:            h[15],
				CRC32:             binary.LittleEndian.Uint32(h[16:20]),
				ModifiedDOS:       binary.LittleEndian.Uint32(h[20:24]),
				UnpackVersion:     h[24],
				CompressionMethod: h[25],
				Attributes:        binary.LittleEndian.Uint32(h[28:32]),
				Directory:         flags&0xe0 == 0xe0,
			}
			if !validRARName(f.Name) || strings.ContainsFunc(f.Name, func(r rune) bool { return r >= 0x80 }) {
				return fail("unsupported ASCII member name")
			}
			if _, exists := names[f.Name]; exists {
				return fail("duplicate member selection")
			}
			if f.UnpackVersion < 15 || f.UnpackVersion > 40 || f.CompressionMethod < 0x30 || f.CompressionMethod > 0x35 {
				return fail("unsupported compression declaration")
			}
			switch f.HostOS {
			case 0, 2:
				if f.Attributes&^uint32(0xb7) != 0 || (f.Attributes&16 != 0) != f.Directory {
					return fail("unsupported DOS or Windows attributes")
				}
			case 3:
				mode := f.Attributes & 0170000
				if f.Attributes > 0177777 || (f.Directory && mode != 0 && mode != 0040000) || (!f.Directory && mode != 0 && mode != 0100000) {
					return fail("unsupported Unix attributes")
				}
			default:
				return fail("unsupported host")
			}
			if f.Packed > size-next {
				return fail("data span exceeds source")
			}
			if f.Directory && (f.Packed != 0 || f.Bytes != 0 || f.CRC32 != 0 || f.CompressionMethod != 0x30) {
				return fail("directory carries data or unsupported declarations")
			}
			if f.CompressionMethod == 0x30 && f.Packed != f.Bytes {
				return fail("stored sizes disagree")
			}
			files = append(files, f)
			names[f.Name] = f.Directory
			next += f.Packed
		case 0x7b:
			if headerSize != 7 || flags&^uint16(0x4000) != 0 || next != size {
				return fail("unsupported end metadata or trailing data")
			}
			if err := unambiguousRARParents(ctx, names); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrRAR4Coverage, err)
			}
			return files, nil
		default:
			return fail("non-file or unknown block")
		}
		off = next
	}
	return fail("missing end marker or block limit")
}

// Measure only the known mtime/ctime/atime layout. DOS time words stay declared
// metadata; fractional values are limited to the selected subsecond subset.
func rar4TimeMetadata(data []byte) bool {
	if len(data) < 2 {
		return false
	}
	mask := binary.LittleEndian.Uint16(data[:2])
	if mask&15 != 0 { // The fourth time field is outside this subset.
		return false
	}
	data = data[2:]
	for i, shift := range []uint{12, 8, 4} {
		field := byte(mask>>shift) & 15
		if field&8 == 0 {
			if field != 0 {
				return false
			}
			continue
		}
		if i != 0 {
			if len(data) < 4 {
				return false
			}
			data = data[4:]
		}
		n := int(field & 3)
		if len(data) < n {
			return false
		}
		var fraction uint32
		for j := 0; j < n; j++ {
			fraction |= uint32(data[j]) << uint(j*8)
		}
		if fraction<<uint((3-n)*8) >= 10_000_000 {
			return false
		}
		data = data[n:]
	}
	return len(data) == 0
}
