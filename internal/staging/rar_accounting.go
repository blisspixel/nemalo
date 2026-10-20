package staging

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrRAR5Coverage = errors.New("RAR5 framing is unsupported or unaccounted")

// RAR5File is a declared file record, not evidence of extracted integrity.
// HeaderOffset through DataOffset+Packed partitions the original container.
type RAR5File struct {
	Name                     string
	HeaderOffset, DataOffset int64
	Bytes, Packed            int64
	CRC32                    uint32
	HasCRC, Directory        bool
	Compression              uint64
	HostOS, Attributes       uint64
}

// RAR5Accounted measures a strict standalone framing subset. It does not read
// payloads, decompress, verify file contents, or authorize writes. A maintained
// reader must independently bind every returned record and check its output.
// Callers sharing tree work must also wrap source with Limits.proofReader.
func RAR5Accounted(ctx context.Context, source io.ReaderAt, size int64) ([]RAR5File, error) {
	if size < 8 || size > maxArchiveSnapshot {
		return nil, fmt.Errorf("%w: source size exceeds the supported bounds", ErrRAR5Coverage)
	}
	r := &coverageReaderAt{ctx: ctx, source: source, remaining: archiveProofBytes}
	signature := make([]byte, 8)
	if _, err := r.ReadAt(signature, 0); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRAR5Coverage, err)
	}
	if string(signature) != "Rar!\x1a\x07\x01\x00" {
		return nil, fmt.Errorf("%w: missing initial RAR5 signature", ErrRAR5Coverage)
	}
	files := []RAR5File{}
	main := false
	for off, blocks := int64(8), 0; off < size && blocks < 10002; blocks++ {
		h, next, err := readRAR5Header(r, off, size)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrRAR5Coverage, err)
		}
		kind, flags := h.number(), h.number()
		if !h.ok || (!main && kind != 1) {
			return nil, fmt.Errorf("%w: missing or malformed main header", ErrRAR5Coverage)
		}
		switch kind {
		case 1:
			archiveFlags := h.number()
			if main || flags != 0 || archiveFlags&^uint64(4) != 0 || !h.done() {
				return nil, fmt.Errorf("%w: unsupported main metadata or volume", ErrRAR5Coverage)
			}
			main = true
		case 2:
			if flags&^uint64(2) != 0 || len(files) >= 10000 {
				return nil, fmt.Errorf("%w: extra fields, split record, flags or member limit", ErrRAR5Coverage)
			}
			packed := uint64(0)
			if flags&2 != 0 {
				packed = h.number()
			}
			if packed > uint64(size-next) {
				return nil, fmt.Errorf("%w: data span exceeds the source", ErrRAR5Coverage)
			}
			f, err := readRAR5File(h)
			if err != nil {
				return nil, err
			}
			f.HeaderOffset, f.DataOffset, f.Packed = off, next, int64(packed)
			if f.Directory && (f.Bytes != 0 || f.Packed != 0 || f.CRC32 != 0) {
				return nil, fmt.Errorf("%w: directory contains data", ErrRAR5Coverage)
			}
			if f.Compression>>7&7 == 0 && f.Packed != f.Bytes {
				return nil, fmt.Errorf("%w: stored size disagrees with data span", ErrRAR5Coverage)
			}
			files = append(files, f)
			next += int64(packed)
		case 5:
			endFlags := h.number()
			if flags != 0 || endFlags != 0 || !h.done() || next != size {
				return nil, fmt.Errorf("%w: unsupported end marker or trailing bytes", ErrRAR5Coverage)
			}
			if !unambiguousRAR5Names(files) {
				return nil, fmt.Errorf("%w: duplicate or overlapping member selections", ErrRAR5Coverage)
			}
			return files, nil
		default:
			return nil, fmt.Errorf("%w: service, encryption or unknown block", ErrRAR5Coverage)
		}
		off = next
	}
	return nil, fmt.Errorf("%w: missing end marker or block limit", ErrRAR5Coverage)
}

// Header work shares the lifetime proof reader; no payload is read. RAR5's
// size field is at most three bytes, and the checked header is at most 2 MiB.
func readRAR5Header(r io.ReaderAt, off, size int64) (*rar5Fields, int64, error) {
	if off < 0 || off > size-7 {
		return nil, off, io.ErrUnexpectedEOF
	}
	prefix := make([]byte, 7)
	if _, err := r.ReadAt(prefix, off); err != nil {
		return nil, off, err
	}
	n, width := binary.Uvarint(prefix[4:])
	if width <= 0 || n < 2 || n > 2<<20 || int64(n)+int64(width)+4 > size-off {
		return nil, off, errors.New("invalid or excessive header size")
	}
	header := make([]byte, 4+width+int(n))
	copy(header, prefix)
	if len(header) > len(prefix) {
		if _, err := r.ReadAt(header[len(prefix):], off+int64(len(prefix))); err != nil {
			return nil, off, err
		}
	}
	if binary.LittleEndian.Uint32(header) != crc32.ChecksumIEEE(header[4:]) {
		return nil, off, errors.New("header checksum differs")
	}
	return &rar5Fields{data: header[4+width:], ok: true}, off + int64(len(header)), nil
}

type rar5Fields struct {
	data []byte
	ok   bool
}

func (h *rar5Fields) number() uint64 {
	v, n := binary.Uvarint(h.data)
	if n <= 0 {
		h.ok = false
		return 0
	}
	h.data = h.data[n:]
	return v
}

func (h *rar5Fields) fixed32() uint32 {
	if len(h.data) < 4 {
		h.ok = false
		return 0
	}
	v := binary.LittleEndian.Uint32(h.data)
	h.data = h.data[4:]
	return v
}

func (h *rar5Fields) done() bool { return h.ok && len(h.data) == 0 }

func readRAR5File(h *rar5Fields) (RAR5File, error) {
	f := RAR5File{}
	flags, bytes := h.number(), h.number()
	f.Attributes = h.number()
	if flags&2 != 0 {
		h.fixed32() // Known seconds-resolution mtime; no generated interpretation.
	}
	f.Directory, f.HasCRC = flags&1 != 0, flags&4 != 0
	if f.HasCRC {
		f.CRC32 = h.fixed32()
	}
	f.Compression, f.HostOS = h.number(), h.number()
	nameBytes := h.number()
	if !h.ok || flags&^uint64(7) != 0 || bytes > maxArchiveSnapshot || (!f.Directory && !f.HasCRC) ||
		nameBytes < 1 || nameBytes > 1024 || nameBytes != uint64(len(h.data)) || !supportedRAR5Compression(f.Compression) {
		return f, fmt.Errorf("%w: unknown size, checksum, compression or file metadata", ErrRAR5Coverage)
	}
	f.Bytes, f.Name = int64(bytes), string(h.data)
	if f.Name == "." || !utf8.ValidString(f.Name) || !fs.ValidPath(f.Name) || strings.ContainsAny(f.Name, "\\:\x00") ||
		strings.ContainsFunc(f.Name, func(r rune) bool { return unicode.IsControl(r) || unicode.In(r, unicode.Cf) || r == '\ufffe' }) {
		return f, fmt.Errorf("%w: unsupported member name", ErrRAR5Coverage)
	}
	switch f.HostOS {
	case 0:
		if f.Attributes&^uint64(0xb7) != 0 || (f.Attributes&16 != 0) != f.Directory {
			return f, fmt.Errorf("%w: unsupported Windows attributes", ErrRAR5Coverage)
		}
	case 1:
		mode := f.Attributes & 0170000
		if f.Attributes > 0177777 || (f.Directory && mode != 0 && mode != 0040000) || (!f.Directory && mode != 0 && mode != 0100000) {
			return f, fmt.Errorf("%w: unsupported Unix attributes", ErrRAR5Coverage)
		}
	default:
		return f, fmt.Errorf("%w: unsupported host", ErrRAR5Coverage)
	}
	return f, nil
}

func supportedRAR5Compression(c uint64) bool {
	version, dictionary := c&63, c>>10&31
	if version > 1 || c&^uint64(0x1fffff) != 0 || c>>7&7 > 5 || dictionary > 23 {
		return false
	}
	return version == 1 || (dictionary <= 15 && c&0x1f8000 == 0)
}

func unambiguousRAR5Names(files []RAR5File) bool {
	names := map[string]bool{}
	for _, f := range files {
		if _, exists := names[f.Name]; exists {
			return false
		}
		names[f.Name] = f.Directory
	}
	for _, f := range files {
		for parent := path.Dir(f.Name); parent != "."; parent = path.Dir(parent) {
			if dir, exists := names[parent]; exists && !dir {
				return false
			}
		}
	}
	return true
}
