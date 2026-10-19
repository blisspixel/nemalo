package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/library"
)

func mp4Box(typ string, parts ...[]byte) []byte {
	body := bytes.Join(parts, nil)
	return append(append(binary.BigEndian.AppendUint32(nil, uint32(8+len(body))), typ...), body...)
}

func be32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }

// mp4Audio returns an M4B of n AAC samples of 200 bytes, each 1024 units at
// 44,100 Hz, with two Nero chapters.
func mp4Audio(n int) []byte {
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

func TestMP4UnitsListChaptersAndRefuseRanges(t *testing.T) {
	req := audioFixture(t, mp4Audio(60))
	req.Representation, req.UnitsOnly = "mp4-audio/1", true
	r, err := Get(context.Background(), req)
	if err != nil || r.Status != "units_available" || r.MP4 == nil || r.MP4.Samples != 60 || len(r.MP4.Chapters) != 2 || r.MP4.Chapters[1].StartMs != 500 || r.MP4.Chapters[1].EndMs != 1393 || len(r.Units) != 1 || r.Units[0].Kind != "mp4_audio_track" || !validRepresentationID(r.RepresentationID) {
		t.Fatal(r, err)
	}
	again, _ := Get(context.Background(), req)
	if again.RepresentationID != r.RepresentationID {
		t.Fatal("the same bytes gave another representation")
	}
	req.UnitsOnly = false
	if r, err := Get(context.Background(), req); err == nil || r.MP4 != nil {
		t.Fatal("mp4-audio/1 served a range", r.Status)
	}
	// An MP3 has no MP4 sample tables.
	req = audioFixture(t, mp3Frames(10))
	req.Representation, req.UnitsOnly = "mp4-audio/1", true
	if r, err := Get(context.Background(), req); err == nil || r.Status != "unsupported_extraction" || r.MP4 != nil {
		t.Fatal(r.Status, err)
	}
}

func validRepresentationID(id string) bool {
	return len(id) == 71 && id[:7] == "sha256:"
}

// A file rewritten while it is read at offsets, even with its size and time
// restored, is refused by the hash that follows the read.
func TestOffsetReadsRefuseAFileChangedWhileRead(t *testing.T) {
	data := mp4Audio(20)
	req := audioFixture(t, data)
	path := filepath.Join(req.Root, "track.mp3")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	asset := library.Asset{ID: req.AssetID, SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(data))}
	status, err := useAsset(context.Background(), req.Root, "track.mp3", asset, func(r io.ReaderAt, size int64) error {
		changed := bytes.Clone(data)
		changed[len(changed)-1] ^= 0xff
		if err := os.WriteFile(path, changed, 0o600); err != nil {
			return err
		}
		return os.Chtimes(path, info.ModTime(), info.ModTime())
	})
	if err == nil || status != "changed_source" {
		t.Fatal(status, err)
	}
	if status, err := useAsset(context.Background(), req.Root, "track.mp3", library.Asset{ID: req.AssetID, SHA256: asset.SHA256, Bytes: assessment.MaxMP4Bytes + 1}, func(io.ReaderAt, int64) error { return nil }); status != "budget_exceeded" || err == nil {
		t.Fatal(status, err)
	}
}
