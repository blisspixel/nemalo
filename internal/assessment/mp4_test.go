package assessment

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func mp4Box(typ string, parts ...[]byte) []byte {
	body := bytes.Join(parts, nil)
	return append(append(binary.BigEndian.AppendUint32(nil, uint32(8+len(body))), typ...), body...)
}

func be32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }

// mp4Audio returns an MP4 audio file of n AAC samples of 200 bytes, each 1024
// units at 44,100 Hz, with the movie box before the media data.
func mp4Audio(n int) []byte {
	ftyp := mp4Box("ftyp", []byte("M4B "), be32(0), []byte("M4B isom"))
	entry := mp4Box("mp4a", make([]byte, 6), []byte{0, 1}, make([]byte, 8), []byte{0, 1, 0, 16}, make([]byte, 4), be32(44100<<16),
		mp4Box("esds", make([]byte, 4), []byte{0x03, 22, 0, 1, 0, 0x04, 17, 0x40, 0x15}, make([]byte, 11), []byte{0x05, 2, 0x12, 0x08}))
	moov := func(offset uint32) []byte {
		sizes := [][]byte{make([]byte, 4), be32(200), be32(uint32(n))}
		return mp4Box("moov",
			mp4Box("mvhd", make([]byte, 12), be32(1000), be32(uint32(n*1024*1000/44100)), make([]byte, 80)),
			mp4Box("trak", mp4Box("tkhd", make([]byte, 12), be32(1), make([]byte, 64)), mp4Box("mdia",
				mp4Box("mdhd", make([]byte, 12), be32(44100), be32(uint32(n*1024)), make([]byte, 4)),
				mp4Box("hdlr", make([]byte, 8), []byte("soun"), make([]byte, 12)),
				mp4Box("minf", mp4Box("stbl",
					mp4Box("stsd", make([]byte, 4), be32(1), entry),
					mp4Box("stts", make([]byte, 4), be32(1), be32(uint32(n)), be32(1024)),
					mp4Box("stsc", make([]byte, 4), be32(1), be32(1), be32(uint32(n)), be32(1)),
					mp4Box("stsz", sizes...),
					mp4Box("stco", make([]byte, 4), be32(1), be32(offset)))))))
	}
	offset := uint32(len(ftyp) + len(moov(0)) + 8)
	return slices.Concat(ftyp, moov(offset), mp4Box("mdat", bytes.Repeat([]byte{0x5a}, 200*n)))
}

func TestMP4ChecksLocateSamplesAndFindTruncation(t *testing.T) {
	ctx := context.Background()
	inspect := func(data []byte, format string) Checks {
		t.Helper()
		c, err := InspectContext(ctx, bytes.NewReader(data), int64(len(data)), format)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	clean := inspect(mp4Audio(50), "m4b")
	if clean.Signature != "mp4_samples_indexed" || clean.MP4 == nil || clean.MP4.Samples != 50 || clean.MP4.Codec != "aac" || clean.MP4.EncodedDurationMs != 1160 || len(clean.Warnings) != 0 {
		t.Fatalf("%+v %+v", clean, clean.MP4)
	}
	data := mp4Audio(50)
	cut := inspect(data[:len(data)-500], "m4b")
	if !slices.Contains(cut.Warnings, "mp4_samples_outside_file") || !slices.Contains(cut.Warnings, "mp4_box_extends_past_file") {
		t.Fatal(cut.Warnings)
	}
	// Protected samples are a finding and an access restriction.
	enc := bytes.Replace(mp4Audio(5), []byte("mp4a"), []byte("drms"), 1)
	if c := inspect(enc, "m4a"); !slices.Contains(c.Warnings, "mp4_audio_encrypted") || !slices.Contains(c.AccessRestrictions, "mp4_audio_encrypted") {
		t.Fatal(c.Warnings, c.AccessRestrictions)
	}
	// A file type box without a movie indexes nothing.
	if c := inspect(slices.Concat(mp4Box("ftyp", []byte("M4A "), be32(0)), mp4Box("mdat", make([]byte, 40))), "m4a"); !slices.Equal(c.Warnings, []string{"mp4_samples_not_indexed"}) || c.MP4 != nil {
		t.Fatal(c.Warnings)
	}
	if _, err := InspectContext(ctx, bytes.NewReader(mp3Frames(4)), 417*4, "m4b"); err == nil {
		t.Fatal("an MP3 was inspected as MP4")
	}
}

func TestCheckNamesMP4AudioByExtension(t *testing.T) {
	dir := t.TempDir()
	for name, want := range map[string]string{"book.m4b": "m4b", "track.m4a": "m4a", "clip.mp4": "m4a"} {
		if err := os.WriteFile(filepath.Join(dir, name), mp4Audio(10), 0o600); err != nil {
			t.Fatal(err)
		}
		// A finding is returned as an error as well as recorded.
		r, err := Check(context.Background(), filepath.Join(dir, name), Options{}, nil)
		mismatch := slices.Contains(r.Findings, `extension "mp4" differs from assessed format "m4a"`)
		if (err != nil) != mismatch || r.DetectedFormat != want || r.Checks.MP4 == nil {
			t.Fatalf("%s: %s %v %v", name, r.DetectedFormat, r.Findings, err)
		}
		if mismatch != (name == "clip.mp4") || (r.Status == "limited_checks_passed") == mismatch {
			t.Fatalf("%s: %s %v", name, r.Status, r.Findings)
		}
	}
}
