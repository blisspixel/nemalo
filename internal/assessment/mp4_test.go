package assessment

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
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

// mp4Head returns the boxes of an MP4 audio file of n AAC samples of 200
// bytes, each 1024 units at 44,100 Hz, up to its media data: the movie box,
// then the media data box header. mp4Audio adds the samples.
func mp4Head(n int) []byte {
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
	return slices.Concat(ftyp, moov(offset), be32(uint32(8+200*n)), []byte("mdat"))
}

func mp4Audio(n int) []byte {
	return append(mp4Head(n), bytes.Repeat([]byte{0x5a}, 200*n)...)
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

// sparse serves real bytes for a prefix and zeros after it, so a file larger
// than memory budgets can be inspected without existing.
type sparse struct {
	prefix []byte
	size   int64
}

func (s sparse) ReadAt(p []byte, off int64) (int, error) {
	if off >= s.size {
		return 0, io.EOF
	}
	n := int(min(int64(len(p)), s.size-off))
	for i := range n {
		p[i] = 0
		if at := off + int64(i); at < int64(len(s.prefix)) {
			p[i] = s.prefix[at]
		}
	}
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// An M4B over the parsed-format limit is inspected through its tables without
// reading its media data; an EPUB of that size is refused.
func TestLargeMP4IsInspectedAtOffsets(t *testing.T) {
	const n = 1 << 20 // 200 MiB of samples
	head := mp4Head(n)
	size := int64(len(head)) + 300<<20
	// Grow the media data box past the parsed-format limit; its samples fit.
	binary.BigEndian.PutUint32(head[len(head)-8:], uint32(size-int64(len(head))+8))
	c, err := InspectContext(context.Background(), sparse{head, size}, size, "m4b")
	if err != nil || c.MP4 == nil || c.MP4.Samples != n || len(c.Warnings) != 0 || size <= MaxParsedBytes {
		t.Fatal(c.Warnings, c.MP4, err)
	}
	if _, err := InspectContext(context.Background(), sparse{head, size}, size, "epub"); !errors.Is(err, ErrLimit) {
		t.Fatal("an oversized EPUB was inspected", err)
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
