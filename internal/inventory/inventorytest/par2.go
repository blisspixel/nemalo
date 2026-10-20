// Package inventorytest produces synthetic PAR2 packet fixtures without a
// recovery executable, protected content, or a dependency on the reader.
package inventorytest

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
)

// Packet serializes the public PAR 2.0 header and a four-byte-aligned body.
func Packet(set byte, kind string, body []byte) []byte {
	body = append([]byte{}, body...)
	for len(body)%4 != 0 {
		body = append(body, 0)
	}
	b := make([]byte, 64+len(body))
	copy(b, "PAR2\x00PKT")
	binary.LittleEndian.PutUint64(b[8:], uint64(len(b)))
	copy(b[32:], bytes.Repeat([]byte{set}, 16))
	copy(b[48:], kind)
	copy(b[64:], body)
	sum := md5.Sum(b[32:])
	copy(b[16:], sum[:])
	return b
}

func Main(set byte, slice uint64, recoverable uint32, ids ...byte) []byte {
	body := make([]byte, 12)
	binary.LittleEndian.PutUint64(body, slice)
	binary.LittleEndian.PutUint32(body[8:], recoverable)
	for _, id := range ids {
		body = append(body, bytes.Repeat([]byte{id}, 16)...)
	}
	return Packet(set, "PAR 2.0\x00Main", body)
}

func Description(set, file byte, name []byte, size uint64) []byte {
	body := make([]byte, 56)
	copy(body, bytes.Repeat([]byte{file}, 16))
	copy(body[16:], bytes.Repeat([]byte{0x42}, 16))
	copy(body[32:], bytes.Repeat([]byte{0x24}, 16))
	binary.LittleEndian.PutUint64(body[48:], size)
	body = append(body, name...)
	return Packet(set, "PAR 2.0\x00FileDesc", body)
}

func Creator(set byte, text string) []byte {
	return Packet(set, "PAR 2.0\x00Creator", []byte(text))
}

func File(packets ...[]byte) []byte {
	return bytes.Join(packets, nil)
}
