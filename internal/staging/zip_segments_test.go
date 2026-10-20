package staging

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/blisspixel/nemalo/internal/safeio"
	"github.com/blisspixel/nemalo/internal/staging/zipfixture"
)

func segmentProofFixture(t testing.TB, directorySpans bool) (zipfixture.Set, []sevenEntry) {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for _, name := range []string{"one.epub", "two.epub"} {
		f, err := w.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write(bytes.Repeat([]byte(name), 10000)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	raw := b.Bytes()
	dir := int(binary.LittleEndian.Uint32(raw[len(raw)-6:])) + 4
	cuts := []int{65536, 131072}
	if directorySpans {
		cuts = []int{65536, dir + 46 + len("one.epub")}
	}
	s := zipfixture.Split(t, raw, cuts...)
	return s, segmentEntries(s)
}

func segmentEntries(s zipfixture.Set) []sevenEntry {
	u16, u32 := binary.LittleEndian.Uint16, binary.LittleEndian.Uint32
	var entries []sevenEntry
	for _, pos := range s.Central {
		name := string(s.Raw[pos+46 : pos+46+int(u16(s.Raw[pos+28:]))])
		entries = append(entries, sevenEntry{raw: name, name: name, valid: true, sizeKnown: true, packedKnown: true, crcKnown: true, volumeKnown: true, offsetKnown: true,
			size: int64(u32(s.Raw[pos+24:])), packed: int64(u32(s.Raw[pos+20:])), crc: u32(s.Raw[pos+16:]), volume: int64(u16(s.Raw[pos+34:])), offset: int64(u32(s.Raw[pos+42:]))})
	}
	return entries
}

func TestZIPSegmentProofAccountsPayloadAndDirectoryAcrossDisks(t *testing.T) {
	for _, spanning := range []bool{false, true} {
		t.Run(fmt.Sprint(spanning), func(t *testing.T) {
			s, entries := segmentProofFixture(t, spanning)
			before := bytes.Clone(s.Raw)
			if ok, why := zipSegmentProof(context.Background(), bytes.NewReader(s.Raw), s.Starts, entries); !ok {
				t.Fatal(why)
			}
			if !bytes.Equal(before, s.Raw) {
				t.Fatal("proof changed original bytes")
			}
			if ok, _ := ZIPAccounted(context.Background(), bytes.NewReader(s.Raw), int64(len(s.Raw))); ok {
				t.Fatal("disk segments were misrepresented as an ordinary ZIP")
			}
		})
	}
}

func TestZIPSegmentProofRefusesContradictionsAndHiddenBytes(t *testing.T) {
	for _, kind := range []string{"marker", "last_disk", "directory_disk", "directory_offset", "directory_size", "last_count", "total_count", "comment", "zip64_offset", "local_disk", "local_offset", "central_signature", "local_signature", "central_name", "local_name", "descriptor", "unknown_extra", "member_comment", "gap", "tail", "first_gap", "local_header_crosses", "central_header_crosses", "end_crosses", "extra_part", "missing_part", "listing_omission", "listing_crc", "listing_size", "listing_packed", "listing_disk", "listing_offset", "listing_folder", "listing_link", "listing_duplicate", "listing_unknown"} {
		t.Run(kind, func(t *testing.T) {
			s, entries := segmentProofFixture(t, true)
			end, c, l := len(s.Raw)-22, s.Central[0], s.Local[0]
			p16, p32 := binary.LittleEndian.PutUint16, binary.LittleEndian.PutUint32
			switch kind {
			case "marker":
				s.Raw[0] ^= 1
			case "last_disk":
				p16(s.Raw[end+4:], 1)
			case "directory_disk":
				p16(s.Raw[end+6:], 99)
			case "directory_offset":
				p32(s.Raw[end+16:], 0)
			case "directory_size":
				p32(s.Raw[end+12:], 0)
			case "last_count":
				p16(s.Raw[end+8:], 2)
			case "total_count":
				p16(s.Raw[end+10:], 3)
			case "comment":
				p16(s.Raw[end+20:], 1)
			case "zip64_offset":
				p32(s.Raw[c+42:], 0xffffffff)
			case "local_disk":
				p16(s.Raw[c+34:], 99)
			case "local_offset":
				p32(s.Raw[c+42:], 0)
			case "central_signature":
				s.Raw[c] ^= 1
			case "local_signature":
				s.Raw[l] ^= 1
			case "central_name":
				s.Raw[c+46] ^= 1
			case "local_name":
				s.Raw[l+30] ^= 1
			case "descriptor":
				s.Raw[s.Local[1]-1] ^= 1
			case "unknown_extra":
				p16(s.Raw[c+30:], 1)
			case "member_comment":
				p16(s.Raw[c+32:], 1)
			case "gap":
				// Consistent directory size/counts cannot excuse a hidden central record.
				s.Raw = append(s.Raw[:end:end], append([]byte("hidden"), s.Raw[end:]...)...)
				s.Starts[len(s.Starts)-1] += 6
				p32(s.Raw[end+6+12:], uint32(end-s.Dir+6))
			case "tail":
				s.Raw = append(s.Raw, 'x')
				s.Starts[len(s.Starts)-1]++
			case "first_gap":
				s.Raw[4] = 0
			case "local_header_crosses":
				s.Starts[1] = 20
			case "central_header_crosses":
				s.Starts[2] = int64(c + 20)
			case "end_crosses":
				s.Starts[2] = int64(end + 1)
			case "extra_part":
				s.Starts = append(s.Starts[:len(s.Starts)-1], int64(end), int64(len(s.Raw)))
			case "missing_part":
				s.Starts = []int64{0, int64(len(s.Raw))}
			case "listing_omission":
				entries = entries[:1]
			case "listing_crc":
				entries[0].crc++
			case "listing_size":
				entries[0].size++
			case "listing_packed":
				entries[0].packed++
			case "listing_disk":
				entries[0].volume++
			case "listing_offset":
				entries[0].offset++
			case "listing_folder":
				entries[0].folder = true
			case "listing_link":
				entries[0].link = true
			case "listing_duplicate":
				entries[1] = entries[0]
			case "listing_unknown":
				entries[0].crcKnown = false
			}
			if ok, why := zipSegmentProof(context.Background(), bytes.NewReader(s.Raw), s.Starts, entries); ok || why == "" {
				t.Fatal("unproven segments accepted", why)
			}
		})
	}
}

func TestZIPSegmentViewPreservesEveryOtherByte(t *testing.T) {
	raw := []byte("PK\x07\x08abcdefghijklmnopqrstuvwxyz")
	v := &zipSegmentView{source: bytes.NewReader(raw), patches: []zipPatch{{2, []byte("XY")}, {11, []byte("1234")}}}
	want := []byte("abXYefghijk1234pqrstuvwxyz")
	for start := 0; start <= len(want); start++ {
		for size := 0; size <= len(want)-start+1; size++ {
			p := make([]byte, size)
			n, err := v.ReadAt(p, int64(start))
			if n != min(size, len(want)-start) || !bytes.Equal(p[:n], want[start:start+n]) || (err != nil && !errors.Is(err, io.EOF)) {
				t.Fatal(start, size, n, string(p), err)
			}
		}
	}
	if _, err := v.ReadAt(make([]byte, 1), -1); err == nil {
		t.Fatal("negative offset accepted")
	}
}

func TestZIPSegmentProofUsesOneLifetimeReadBudgetAndCancellation(t *testing.T) {
	s, entries := segmentProofFixture(t, true)
	r := &zipProofReader{ctx: context.Background(), source: bytes.NewReader(s.Raw), remaining: 150}
	if ok, _ := zipSegmentProof(context.Background(), r, s.Starts, entries); ok || r.remaining == 150 {
		t.Fatal("physical reads were uncharged", r.remaining)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ok, _ := zipSegmentProof(ctx, bytes.NewReader(s.Raw), s.Starts, entries); ok {
		t.Fatal("cancelled proof accepted")
	}
}

func TestZIPSegmentPrivateReaderBoundsAndChangedIdentity(t *testing.T) {
	dir := t.TempDir()
	root, err := safeio.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	s := &snapshotSegments{root: root, names: []string{"a", "b"}, starts: []int64{0, 3, 7}}
	for i, body := range []string{"abc", "defg"} {
		if err := root.WriteFile(s.names[i], []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		info, err := root.Lstat(s.names[i])
		if err != nil {
			t.Fatal(err)
		}
		s.infos = append(s.infos, info)
	}
	var p [6]byte
	if n, err := s.ReadAt(p[:], 1); err != nil || n != 6 || string(p[:]) != "bcdefg" {
		t.Fatal(n, string(p[:]), err)
	}
	if n, err := s.ReadAt(p[:], 3); !errors.Is(err, io.EOF) || n != 4 {
		t.Fatal(n, err)
	}
	if _, err := s.ReadAt(p[:], -1); !errors.Is(err, fs.ErrInvalid) {
		t.Fatal(err)
	}
	if n, err := s.ReadAt(nil, 7); n != 0 || err != nil {
		t.Fatal(n, err)
	}
	if err := root.Rename("b", "retained"); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile("b", []byte("fake"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadAt(p[:1], 3); err == nil {
		t.Fatal("replacement adopted")
	}
	if err := root.Remove("b"); err != nil {
		t.Fatal(err)
	}
	if err := root.Rename("retained", "b"); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile("b", []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadAt(p[:1], 3); err == nil {
		t.Fatal("changed original adopted")
	}
	if ok, _ := zipSegmentsAccounted(context.Background(), root, []string{"absent"}, nil, Defaults()); ok {
		t.Fatal("missing private part accepted")
	}
	if err := os.Mkdir(filepath.Join(dir, "folder"), 0700); err != nil {
		t.Fatal(err)
	}
	if ok, _ := zipSegmentsAccounted(context.Background(), root, []string{"folder"}, nil, Defaults()); ok {
		t.Fatal("directory private part accepted")
	}
}

func TestZIPSegmentsReuseOrdinaryFramingForMethodsAndDescriptors(t *testing.T) {
	for _, method := range []uint16{zip.Store, zip.Deflate} {
		var b bytes.Buffer
		w := zip.NewWriter(&b)
		f, err := w.CreateHeader(&zip.FileHeader{Name: "book.epub", Method: method})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(bytes.Repeat([]byte("synthetic payload"), 1000)); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		z, err := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
		if err != nil {
			t.Fatal(err)
		}
		data, err := z.File[0].DataOffset()
		if err != nil {
			t.Fatal(err)
		}
		for _, cut := range []int{4 + int(data) + int(z.File[0].CompressedSize64)/2, 4 + int(data) + int(z.File[0].CompressedSize64) + 5} {
			s := zipfixture.Split(t, b.Bytes(), cut)
			if ok, why := zipSegmentProof(context.Background(), bytes.NewReader(s.Raw), s.Starts, segmentEntries(s)); !ok {
				t.Fatal(method, cut, why)
			}
		}
	}
}

func TestZIPSegmentProofEmptyDirectoryMayOmitListedCRC(t *testing.T) {
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	if _, err := w.CreateHeader(&zip.FileHeader{Name: "books/", Method: zip.Store}); err != nil {
		t.Fatal(err)
	}
	f, err := w.CreateHeader(&zip.FileHeader{Name: "books/book.epub", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(bytes.Repeat([]byte("synthetic"), 1000)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	s := zipfixture.Split(t, b.Bytes(), 5000)
	entries := segmentEntries(s)
	entries[0].folder, entries[0].crcKnown = true, false
	entries[0].name, entries[0].raw = "books", "books"
	if ok, why := zipSegmentProof(context.Background(), bytes.NewReader(s.Raw), s.Starts, entries); !ok {
		t.Fatal("known empty directory needs no listed CRC", why)
	}
	entries[1].crcKnown = false
	if ok, _ := zipSegmentProof(context.Background(), bytes.NewReader(s.Raw), s.Starts, entries); ok {
		t.Fatal("missing file CRC accepted")
	}
}

func TestZIPSegmentCoveragePrecedesAnyMemberExtraction(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(fmt.Sprint(corrupt), func(t *testing.T) {
			src, dst := sevenFixture(t, nil)
			s, entries := segmentProofFixture(t, true)
			if corrupt {
				s.Raw[s.Central[0]+34] = 99
				for i := range s.Parts {
					s.Parts[i] = bytes.Clone(s.Raw[s.Starts[i]:s.Starts[i+1]])
				}
			}
			t.Setenv("NEMALO_FAKE_7Z_TYPE", "-tzip")
			var listing bytes.Buffer
			for _, e := range entries {
				fmt.Fprintf(&listing, "Path = %s\nSize = %d\nPacked Size = %d\nCRC = %08X\nVolume Index = %d\nOffset = %d\n\n", e.name, e.size, e.packed, e.crc, e.volume, e.offset)
				body := bytes.Repeat([]byte(e.name), 10000)
				if crc32.ChecksumIEEE(body) != e.crc {
					t.Fatal("fixture CRC mismatch")
				}
				writeFakeRARFile(t, fmt.Sprintf("members/%x", e.name), string(body))
			}
			writeFakeRARFile(t, "listing.txt", listing.String())
			names, copies := []string{}, map[string][]byte{}
			for i, part := range s.Parts {
				name := fmt.Sprintf("archive.z%02d", i+1)
				if i+1 == len(s.Parts) {
					name = "archive.zip"
				}
				names = append(names, name)
				copies[name] = part
				if err := src.WriteFile(name, part, 0600); err != nil {
					t.Fatal(err)
				}
				writeFakeRARFile(t, "header-"+name, fmt.Sprintf("Type = zip\nPhysical Size = %d\nTotal Physical Size = %d\nEmbedded Stub Size = 4\nMultivolume = +\nVolume Index = %d\nVolumes = %d\n", len(part), len(s.Raw), i, len(s.Parts)))
			}
			zipSetCopies(t, copies)
			limits := Defaults()
			limits.Coverage = true
			x, err := ExtractSet(context.Background(), src, names, dst, books, limits)
			if corrupt {
				if !errors.Is(err, ErrZIPCoverage) || len(x.Members) != 0 {
					t.Fatal(x, err)
				}
				files, _ := os.ReadDir(dst.Name())
				if len(files) != 0 {
					t.Fatal("extracted before proof", files)
				}
			} else if err != nil || !x.Whole || !x.Complete || len(x.Members) != 2 || x.Members[0].Disposition != "extracted" {
				t.Fatal(x, err)
			}
		})
	}
}

func FuzzZIPSegmentProofStaysBounded(f *testing.F) {
	s, entries := segmentProofFixture(f, true)
	// Mutate framing directly without spending the fuzz budget on shrinking a
	// large, mostly irrelevant payload. Keep a real cross-segment seed intact.
	var locations []int
	for _, region := range [][2]int{{0, 8}, {s.Local[0], 42}, {s.Local[1] - 16, 58}, {s.Central[0], len(s.Raw) - s.Central[0]}} {
		for i := 0; i < region[1]; i++ {
			locations = append(locations, region[0]+i)
		}
	}
	f.Add([]byte{}, uint32(65536), uint32(s.Starts[2]))
	f.Add([]byte{0, 255}, uint32(65536), uint32(s.Starts[2]))
	f.Add([]byte{}, uint32(0), uint32(0))
	f.Fuzz(func(t *testing.T, changes []byte, first, second uint32) {
		if len(changes) > 4096 {
			t.Skip()
		}
		raw := bytes.Clone(s.Raw)
		for i := 0; i+2 < len(changes); i += 3 {
			at := int(binary.LittleEndian.Uint16(changes[i:])) % len(locations)
			raw[locations[at]] ^= changes[i+2]
		}
		zipSegmentProof(context.Background(), bytes.NewReader(raw), []int64{0, int64(first), int64(second), int64(len(raw))}, entries)
	})
}

func TestZIPSegmentViewCannotHideGapsBetweenPhysicalRecords(t *testing.T) {
	base := zipProofFixture(t, nil, "")
	u16, u32 := binary.LittleEndian.Uint16, binary.LittleEndian.Uint32
	dir := int(u32(base[len(base)-6:]))
	next := dir + 46 + int(u16(base[dir+28:]))
	for _, at := range []int{0, int(u32(base[next+42:])), dir} {
		b := append(append(append([]byte{}, base[:at]...), []byte("gap")...), base[at:]...)
		newDir := dir + 3
		binary.LittleEndian.PutUint32(b[len(b)-6:], uint32(newDir))
		for _, original := range []int{dir, next} {
			local := u32(base[original+42:])
			if int(local) >= at {
				local += 3
			}
			binary.LittleEndian.PutUint32(b[original+3+42:], local)
		}
		s := zipfixture.Split(t, b, 53)
		if ok, why := zipSegmentProof(context.Background(), bytes.NewReader(s.Raw), s.Starts, segmentEntries(s)); ok {
			t.Fatal("overlay concealed source bytes", at, why)
		}
	}
}
