// Package zipfixture generates synthetic disk-relative ZIP fixtures for tests.
// It is not a production reader or archive conversion service.
package zipfixture

import (
	"bytes"
	"encoding/binary"
	"sort"
	"testing"
)

type Set struct {
	Raw     []byte
	Parts   [][]byte
	Starts  []int64
	Central []int
	Local   []int
	Dir     int
}

// Split inserts the split marker and writes disk-relative references into a
// Go-generated ordinary ZIP32. Cuts are physical offsets including the marker.
// Deliberately invalid cuts are useful for testing header-boundary refusals.
func Split(t testing.TB, ordinary []byte, cuts ...int) Set {
	t.Helper()
	s := Set{Raw: append([]byte("PK\x07\x08"), ordinary...), Starts: []int64{0}}
	u16, u32 := binary.LittleEndian.Uint16, binary.LittleEndian.Uint32
	put16, put32 := binary.LittleEndian.PutUint16, binary.LittleEndian.PutUint32
	end := len(s.Raw) - 22
	if end < 0 || u32(s.Raw[end:]) != 0x06054b50 || !sort.IntsAreSorted(cuts) {
		t.Fatal("fixture requires an ordinary uncommented ZIP32 and sorted cuts")
	}
	for _, cut := range cuts {
		if cut <= int(s.Starts[len(s.Starts)-1]) || cut >= len(s.Raw) {
			t.Fatal("invalid fixture cut", cut)
		}
		s.Starts = append(s.Starts, int64(cut))
	}
	s.Starts = append(s.Starts, int64(len(s.Raw)))
	disk := func(off int) int {
		return sort.Search(len(s.Starts)-1, func(i int) bool { return s.Starts[i+1] > int64(off) })
	}
	s.Dir = int(u32(s.Raw[end+16:])) + 4
	pos, onLast := s.Dir, 0
	for i := 0; i < int(u16(s.Raw[end+10:])); i++ {
		if pos > end-46 || u32(s.Raw[pos:]) != 0x02014b50 {
			t.Fatal("fixture directory is invalid")
		}
		local := int(u32(s.Raw[pos+42:])) + 4
		s.Local, s.Central = append(s.Local, local), append(s.Central, pos)
		ld := disk(local)
		put16(s.Raw[pos+34:], uint16(ld))
		put32(s.Raw[pos+42:], uint32(int64(local)-s.Starts[ld]))
		if disk(pos) == len(s.Starts)-2 {
			onLast++
		}
		pos += 46 + int(u16(s.Raw[pos+28:])) + int(u16(s.Raw[pos+30:])) + int(u16(s.Raw[pos+32:]))
	}
	put16(s.Raw[end+4:], uint16(len(s.Starts)-2))
	put16(s.Raw[end+6:], uint16(disk(s.Dir)))
	put16(s.Raw[end+8:], uint16(onLast))
	put32(s.Raw[end+16:], uint32(int64(s.Dir)-s.Starts[disk(s.Dir)]))
	for i := 0; i < len(s.Starts)-1; i++ {
		s.Parts = append(s.Parts, bytes.Clone(s.Raw[s.Starts[i]:s.Starts[i+1]]))
	}
	return s
}
