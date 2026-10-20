package staging

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"github.com/blisspixel/nemalo/internal/assessment"
	"io"
	"math/bits"
	"sort"
)

const zipProofBytes = 16 << 20

// ZIPAccounted reports byte coverage, not member eligibility or write permission.
func ZIPAccounted(ctx context.Context, source io.ReaderAt, size int64) (bool, string) {
	r := &zipProofReader{ctx: ctx, source: source, remaining: zipProofBytes}
	head := make([]byte, 4)
	if _, err := r.ReadAt(head, 0); err != nil || string(head) != "PK\x03\x04" {
		return false, "the archive has unreadable or leading data"
	}
	z, err := assessment.OpenZIP(ctx, r, size, zipProofBytes, 8<<30, 10000)
	if err != nil {
		return false, "the archive could not be read within its proof limits"
	}
	if z.Comment != "" {
		return false, "the archive carries a comment"
	}
	if !zipContiguous(r, size, z) {
		return false, "the archive has bytes outside its entries and directory, inconsistent records, or unsupported metadata"
	}
	return true, ""
}

// This lifetime allowance includes decoder initialization, DataOffset calls,
// framing reads and failed attempts. The decoder's bounded end-record search
// can include neighboring payload bytes; the proof never decompresses them.
type zipProofReader struct {
	ctx       context.Context
	source    io.ReaderAt
	remaining int64
}

func (r *zipProofReader) ReadAt(p []byte, off int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > r.remaining {
		return 0, assessment.ErrLimit
	}
	n, err := r.source.ReadAt(p, off)
	r.remaining -= int64(n)
	if err == nil {
		err = r.ctx.Err()
	}
	return n, err
}

type zipLocalRecord struct {
	file         *zip.File
	header, data int64
}

// zipContiguous establishes ordinary ZIP32 framing, not conformance or safety.
// The maintained reader still interprets and checks each extracted member.
func zipContiguous(r io.ReaderAt, size int64, z *zip.Reader) bool {
	if size < 22 || len(z.File) == 0 || len(z.File) > 10000 {
		return false
	}
	end := make([]byte, 22)
	if _, err := r.ReadAt(end, size-22); err != nil || binary.LittleEndian.Uint32(end) != 0x06054b50 {
		return false
	}
	u16 := binary.LittleEndian.Uint16
	u32 := binary.LittleEndian.Uint32
	count := int(u16(end[10:]))
	dirSize, dirOffset := int64(u32(end[12:])), int64(u32(end[16:]))
	if u16(end[4:]) != 0 || u16(end[6:]) != 0 || int(u16(end[8:])) != count || count != len(z.File) ||
		u16(end[20:]) != 0 || dirOffset == 0xFFFFFFFF || dirSize == 0xFFFFFFFF || dirOffset+dirSize != size-22 {
		return false
	}
	pos := dirOffset
	locals := make([]zipLocalRecord, 0, count)
	central := make([]byte, 46)
	for _, f := range z.File {
		if pos < 0 || pos > size-22-46 {
			return false
		}
		if _, err := r.ReadAt(central, pos); err != nil || u32(central) != 0x02014b50 {
			return false
		}
		nameLen, extraLen, commentLen := int(u16(central[28:])), int(u16(central[30:])), int(u16(central[32:]))
		if nameLen > 1024 || commentLen != 0 || f.Comment != "" || u16(central[34:]) != 0 ||
			u32(central[42:]) == 0xFFFFFFFF || f.CompressedSize64 >= 0xFFFFFFFF || f.UncompressedSize64 >= 0xFFFFFFFF ||
			(f.ReaderVersion != 10 && f.ReaderVersion != 20) || (f.Method == zip.Deflate && f.ReaderVersion != 20) ||
			f.Flags&^uint16(0x080e) != 0 || (f.Method != zip.Store && f.Method != zip.Deflate) ||
			u16(central[4:]) != f.CreatorVersion || u32(central[38:]) != f.ExternalAttrs ||
			u16(central[6:]) != f.ReaderVersion || u16(central[8:]) != f.Flags || u16(central[10:]) != f.Method ||
			u16(central[12:]) != f.ModifiedTime || u16(central[14:]) != f.ModifiedDate ||
			u32(central[16:]) != f.CRC32 || uint64(u32(central[20:])) != f.CompressedSize64 || uint64(u32(central[24:])) != f.UncompressedSize64 {
			return false
		}
		pos += 46
		if int64(nameLen+extraLen) > size-22-pos {
			return false
		}
		metadata := make([]byte, nameLen+extraLen)
		if _, err := r.ReadAt(metadata, pos); err != nil || string(metadata[:nameLen]) != f.Name ||
			!bytes.Equal(metadata[nameLen:], f.Extra) || !zipMetadataExtra(metadata[nameLen:], false) {
			return false
		}
		pos += int64(len(metadata))
		data, err := f.DataOffset()
		if err != nil {
			return false
		}
		locals = append(locals, zipLocalRecord{file: f, header: int64(u32(central[42:])), data: data})
	}
	if pos != size-22 {
		return false
	}
	sort.Slice(locals, func(i, j int) bool { return locals[i].header < locals[j].header })
	pos = 0
	local := make([]byte, 30)
	for _, record := range locals {
		f := record.file
		if record.header != pos || pos > dirOffset-30 {
			return false
		}
		if _, err := r.ReadAt(local, pos); err != nil || u32(local) != 0x04034b50 ||
			u16(local[4:]) != f.ReaderVersion || u16(local[6:]) != f.Flags || u16(local[8:]) != f.Method ||
			u16(local[10:]) != f.ModifiedTime || u16(local[12:]) != f.ModifiedDate {
			return false
		}
		nameLen, extraLen := int(u16(local[26:])), int(u16(local[28:]))
		data := pos + 30 + int64(nameLen+extraLen)
		if nameLen != len(f.Name) || data != record.data || data > dirOffset || int64(f.CompressedSize64) > dirOffset-data {
			return false
		}
		metadata := make([]byte, nameLen+extraLen)
		if _, err := r.ReadAt(metadata, pos+30); err != nil || string(metadata[:nameLen]) != f.Name || !zipMetadataExtra(metadata[nameLen:], true) {
			return false
		}
		for _, field := range []struct{ got, want uint32 }{{u32(local[14:]), f.CRC32}, {u32(local[18:]), uint32(f.CompressedSize64)}, {u32(local[22:]), uint32(f.UncompressedSize64)}} {
			if field.got != field.want && !(f.Flags&8 != 0 && field.got == 0) {
				return false
			}
		}
		pos = data + int64(f.CompressedSize64)
		if f.Flags&8 != 0 {
			if f.CRC32 == 0x08074b50 || pos > dirOffset-12 {
				return false
			}
			descriptor := make([]byte, 16)
			if _, err := r.ReadAt(descriptor[:12], pos); err != nil {
				return false
			}
			length, offset := 12, 0
			if u32(descriptor) == 0x08074b50 {
				if pos > dirOffset-16 {
					return false
				}
				if _, err := r.ReadAt(descriptor[12:], pos+12); err != nil {
					return false
				}
				length, offset = 16, 4
			}
			if u32(descriptor[offset:]) != f.CRC32 || uint64(u32(descriptor[offset+4:])) != f.CompressedSize64 || uint64(u32(descriptor[offset+8:])) != f.UncompressedSize64 {
				return false
			}
			pos += int64(length)
		}
	}
	return pos == dirOffset
}

// Keep unknown or attached data; only bounded metadata handled by Go's ZIP
// reader qualifies. Extra records can differ between local and central headers.
func zipMetadataExtra(extra []byte, local bool) bool {
	seen := map[uint16]bool{}
	for len(extra) != 0 {
		if len(extra) < 4 {
			return false
		}
		tag, length := binary.LittleEndian.Uint16(extra), int(binary.LittleEndian.Uint16(extra[2:]))
		extra = extra[4:]
		if length > len(extra) || seen[tag] {
			return false
		}
		seen[tag] = true
		body := extra[:length]
		extra = extra[length:]
		switch tag {
		case 0x5455: // Extended timestamps: central headers only carry mtime.
			if len(body) == 0 || body[0]&^byte(7) != 0 {
				return false
			}
			flags := body[0]
			if !local {
				flags &= 1
			}
			if len(body) != 1+4*bits.OnesCount8(flags) {
				return false
			}
		case 0x000a: // One NTFS timestamp attribute, no other attributes.
			if len(body) != 32 || binary.LittleEndian.Uint32(body) != 0 || binary.LittleEndian.Uint16(body[4:]) != 1 || binary.LittleEndian.Uint16(body[6:]) != 24 {
				return false
			}
		case 0x000d: // Fixed Unix times, UID and GID; no attached link/data.
			if len(body) != 12 {
				return false
			}
		case 0x5855: // Info-ZIP Unix timestamps and optional fixed UID/GID.
			if len(body) != 8 && len(body) != 12 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
