// Package audiotest provides synthetic MP4 bytes for adapter contract tests.
package audiotest

import (
	"bytes"
	"encoding/binary"
	"slices"
)

func mp4Box(typ string, parts ...[]byte) []byte {
	body := bytes.Join(parts, nil)
	return append(append(binary.BigEndian.AppendUint32(nil, uint32(8+len(body))), typ...), body...)
}

func be32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }

// MP4 returns an M4B of n AAC samples of 200 bytes, each 1024 units at
// 44,100 Hz, with two Nero chapters.
func MP4(n int) []byte {
	ftyp := mp4Box("ftyp", []byte("M4B "), be32(0))
	entry := mp4Box("mp4a", make([]byte, 6), []byte{0, 1}, make([]byte, 8), []byte{0, 1, 0, 16}, make([]byte, 4), be32(44100<<16),
		mp4Box("esds", make([]byte, 4), []byte{0x03, 22, 0, 1, 0, 0x04, 17, 0x40, 0x15}, make([]byte, 11), []byte{0x05, 2, 0x12, 0x08}))
	chpl := mp4Box("chpl", []byte{1, 0, 0, 0}, make([]byte, 4), []byte{2}, make([]byte, 8), []byte{3}, []byte("One"), binary.BigEndian.AppendUint64(nil, 5_000_000), []byte{3}, []byte("Two"))
	moov := func(offset uint32) []byte {
		return mp4Box("moov",
			mp4Box("mvhd", make([]byte, 12), be32(1000), be32(uint32(n*1024*1000/44100)), make([]byte, 80)),
			mp4Box("trak", mp4Box("tkhd", make([]byte, 12), be32(1), make([]byte, 64)), mp4Box("mdia",
				mp4Box("mdhd", make([]byte, 12), be32(44100), be32(uint32(n*1024)), make([]byte, 4)),
				mp4Box("hdlr", make([]byte, 8), []byte("soun"), make([]byte, 12)),
				mp4Box("minf", mp4Box("stbl",
					mp4Box("stsd", make([]byte, 4), be32(1), entry),
					mp4Box("stts", make([]byte, 4), be32(1), be32(uint32(n)), be32(1024)),
					mp4Box("stsc", make([]byte, 4), be32(1), be32(1), be32(uint32(n)), be32(1)),
					mp4Box("stsz", make([]byte, 4), be32(200), be32(uint32(n))),
					mp4Box("stco", make([]byte, 4), be32(1), be32(offset)))))),
			mp4Box("udta", chpl))
	}
	offset := uint32(len(ftyp) + len(moov(0)) + 8)
	return slices.Concat(ftyp, moov(offset), mp4Box("mdat", bytes.Repeat([]byte{0x5a}, 200*n)))
}
