package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
	"unicode/utf16"
)

type spec struct {
	version, layer, bitrate, rate int // version bits 0/2/3, layer bits 1..3 (1=L3), indexes
	pad, mono, crc                bool
	reservoir                     int
}

// mp3 is MPEG-1 Layer III, 128 kbps, 44.1 kHz, joint stereo: 417-byte frames.
var mp3 = spec{version: 3, layer: 1, bitrate: 9, rate: 0}

func frame(s spec) []byte {
	b1 := byte(0xE0 | s.version<<3 | s.layer<<1)
	if !s.crc {
		b1 |= 1
	}
	b2 := byte(s.bitrate<<4 | s.rate<<2)
	if s.pad {
		b2 |= 2
	}
	b3 := byte(1 << 6) // joint stereo
	if s.mono {
		b3 = 3 << 6
	}
	h, ok := parseHeader([]byte{0xFF, b1, b2, b3})
	if !ok {
		panic("invalid test header")
	}
	f := make([]byte, h.length)
	copy(f, []byte{0xFF, b1, b2, b3})
	p := 4
	if s.crc {
		p += 2
	}
	if h.layer == 3 && h.version == 1 {
		f[p], f[p+1] = byte(s.reservoir>>1), byte(s.reservoir&1)<<7
	} else if h.layer == 3 {
		f[p] = byte(s.reservoir)
	}
	return f
}

func stream(s spec, n int) []byte {
	var b bytes.Buffer
	for range n {
		b.Write(frame(s))
	}
	return b.Bytes()
}

func probe(t *testing.T, data []byte) Track {
	t.Helper()
	tr, err := Probe(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestConstantBitrateStream(t *testing.T) {
	tr := probe(t, stream(mp3, 100))
	if tr.Format != "mpeg1_layer3" || tr.SampleRate != 44100 || tr.Channels != 2 || tr.ChannelMode != "joint_stereo" || tr.SamplesPerFrame != 1152 {
		t.Fatal(tr)
	}
	if tr.Frames != 100 || tr.EncodedSamples != 115200 || tr.EncodedDurationMs != 2612 || tr.Bitrate != "constant_128_kbps" || tr.AudioStart != 0 || tr.AudioEnd != 41700 {
		t.Fatal(tr.Frames, tr.EncodedSamples, tr.EncodedDurationMs, tr.Bitrate, tr.AudioEnd)
	}
	if len(tr.Gaps) != 0 || len(tr.Findings) != 0 || tr.Decoded != "not_decoded" || tr.PresentedSamples != nil {
		t.Fatal(tr.Gaps, tr.Findings)
	}
}

func TestFormatsAndVariableBitrate(t *testing.T) {
	for _, tc := range []struct {
		s       spec
		format  string
		rate    int
		samples int
	}{
		{spec{version: 2, layer: 1, bitrate: 8, rate: 0, mono: true}, "mpeg2_layer3", 22050, 576},
		{spec{version: 0, layer: 1, bitrate: 8, rate: 2}, "mpeg2_5_layer3", 8000, 576},
		{spec{version: 3, layer: 2, bitrate: 10, rate: 1}, "mpeg1_layer2", 48000, 1152},
		{spec{version: 3, layer: 3, bitrate: 4, rate: 2, crc: true}, "mpeg1_layer1", 32000, 384},
	} {
		tr := probe(t, stream(tc.s, 10))
		if tr.Format != tc.format || tr.SampleRate != tc.rate || tr.SamplesPerFrame != tc.samples || tr.Frames != 10 {
			t.Fatal(tc.format, tr.Format, tr.SampleRate, tr.SamplesPerFrame, tr.Frames)
		}
	}
	vbr := append(stream(mp3, 5), stream(spec{version: 3, layer: 1, bitrate: 11, rate: 0, pad: true}, 5)...)
	if tr := probe(t, vbr); tr.Bitrate != "variable" || tr.Frames != 10 {
		t.Fatal(tr.Bitrate, tr.Frames)
	}
}

func id3Frame(id string, v4 bool, body []byte) []byte {
	b := []byte(id)
	size := make([]byte, 4)
	if v4 {
		n := len(body)
		size = []byte{byte(n >> 21 & 0x7F), byte(n >> 14 & 0x7F), byte(n >> 7 & 0x7F), byte(n & 0x7F)}
	} else {
		binary.BigEndian.PutUint32(size, uint32(len(body)))
	}
	b = append(b, size...)
	b = append(b, 0, 0)
	return append(b, body...)
}

func id3Tag(major byte, flags byte, body []byte) []byte {
	n := len(body)
	return append([]byte{'I', 'D', '3', major, 0, flags, byte(n >> 21 & 0x7F), byte(n >> 14 & 0x7F), byte(n >> 7 & 0x7F), byte(n & 0x7F)}, body...)
}

func utf16Text(s string) []byte {
	b := []byte{1, 0xFF, 0xFE}
	for _, u := range utf16.Encode([]rune(s)) {
		b = binary.LittleEndian.AppendUint16(b, u)
	}
	return b
}

func TestID3TagsChaptersAndTrailers(t *testing.T) {
	sub := id3Frame("TIT2", true, append([]byte{3}, "Chapter One"...))
	chap := append([]byte("ch1\x00"), make([]byte, 16)...)
	binary.BigEndian.PutUint32(chap[4:], 0)
	binary.BigEndian.PutUint32(chap[8:], 1500)
	chap = append(chap, sub...)
	body := bytes.Join([][]byte{
		id3Frame("TIT2", true, append([]byte{3}, "Le Petit Prince"...)),
		id3Frame("TPE1", true, utf16Text("Antoine de Saint-Exupéry")),
		id3Frame("TALB", true, append([]byte{0}, "Caf\xe9"...)),
		id3Frame("TLEN", true, append([]byte{3}, "60000"...)),
		id3Frame("CHAP", true, chap),
		make([]byte, 20), // padding
	}, nil)
	tag := id3Tag(4, 0, body)
	ape := make([]byte, 64)
	copy(ape[32:], "APETAGEX")
	binary.LittleEndian.PutUint32(ape[32+12:], 64)
	binary.LittleEndian.PutUint32(ape[32+20:], 1<<31)
	v1 := append([]byte("TAG"), make([]byte, 125)...)
	data := bytes.Join([][]byte{tag, stream(mp3, 20), ape[32:], v1}, nil)
	// The APE size above counts header and items; use a footer-only size.
	binary.LittleEndian.PutUint32(data[len(data)-128-32+12:], 32)
	binary.LittleEndian.PutUint32(data[len(data)-128-32+20:], 0)
	tr := probe(t, data)
	m := tr.Metadata
	if m.ID3Version != "2.4" || m.Title != "Le Petit Prince" || m.Artist != "Antoine de Saint-Exupéry" || m.Album != "Café" || m.Skipped != "" {
		t.Fatalf("%+v", m)
	}
	if len(tr.Chapters) != 1 || tr.Chapters[0].ID != "ch1" || tr.Chapters[0].Title != "Chapter One" || tr.Chapters[0].EndMs != 1500 {
		t.Fatal(tr.Chapters)
	}
	if tr.Declared.LengthMs == nil || *tr.Declared.LengthMs != 60000 || len(tr.Findings) != 1 || !strings.Contains(tr.Findings[0], "ID3 length declares 60000 ms") {
		t.Fatal("declared length disagreement not reported", tr.Findings)
	}
	kinds := []string{}
	for _, r := range tr.Tags {
		kinds = append(kinds, r.Kind)
	}
	if strings.Join(kinds, ",") != "id3v2,id3v1,apev2" || tr.AudioStart != int64(len(tag)) || tr.Frames != 20 || len(tr.Gaps) != 0 {
		t.Fatal(kinds, tr.AudioStart, tr.Frames, tr.Gaps)
	}
}

func TestID3UnsupportedFormsAreSkippedNotGuessed(t *testing.T) {
	for _, tc := range []struct {
		tag  []byte
		want string
	}{
		{id3Tag(2, 0, make([]byte, 10)), "unsupported_id3_version"},
		{id3Tag(3, 0x80, make([]byte, 10)), "unsynchronised_tag_not_read"},
		{id3Tag(3, 0, append(append(id3Frame("TIT2", false, []byte{0, 'x'})[:8], 0, 0x80), 0, 'x')), "encoded_frame_not_read"},
		{id3Tag(4, 0, []byte("TIT2\x00\x00\x00\xff\x00\x00x")), "malformed_frame"},
		{id3Tag(3, 0, []byte("ti!2\x00\x00\x00\x01\x00\x00x")), "malformed_frame"},
	} {
		tr := probe(t, append(tc.tag, stream(mp3, 3)...))
		if tr.Metadata.Skipped != tc.want || tr.Metadata.Title != "" || tr.Frames != 3 {
			t.Fatal(tc.want, tr.Metadata)
		}
	}
	if s := text([]byte{1, 0x00, 0x41}); s != "" {
		t.Fatal("UTF-16 without a byte order mark accepted", s)
	}
	if s := text([]byte{3, 0xff, 0xfe}); s != "" {
		t.Fatal("invalid UTF-8 accepted", s)
	}
	if s := text(append([]byte{3}, strings.Repeat("é", 2000)...)); len(s) > maxText || !strings.HasSuffix(s, "é") {
		t.Fatal("text not bounded at a rune boundary", len(s))
	}
}

// infoFrame builds a LAME-style Info frame with every Xing field present.
func infoFrame(tag string, frames, delay, padding int, encoder string) []byte {
	f := frame(mp3)
	x := 4 + 32
	copy(f[x:], tag)
	binary.BigEndian.PutUint32(f[x+4:], 0xF)
	binary.BigEndian.PutUint32(f[x+8:], uint32(frames))
	binary.BigEndian.PutUint32(f[x+12:], uint32(len(f)*(frames+1)))
	copy(f[x+120:], encoder)
	f[x+141] = byte(delay >> 4)
	f[x+142] = byte(delay&0xF)<<4 | byte(padding>>8)
	f[x+143] = byte(padding)
	return f
}

func TestInfoFrameDeclarationsStaySeparateFromIndexedFrames(t *testing.T) {
	tr := probe(t, append(infoFrame("Info", 50, 576, 1200, "LAME3.100"), stream(mp3, 50)...))
	d := tr.Declared
	if d.InfoFrame != "Info" || d.Encoder != "LAME3.100" || *d.Frames != 50 || *d.EncoderDelay != 576 || *d.EncoderPadding != 1200 {
		t.Fatalf("%+v", d)
	}
	if tr.Frames != 50 || tr.AudioStart != 417 || tr.PresentedSamples == nil || *tr.PresentedSamples != 50*1152-576-1200 || len(tr.Findings) != 0 {
		t.Fatal(tr.Frames, tr.AudioStart, tr.PresentedSamples, tr.Findings)
	}
	tr = probe(t, append(infoFrame("Xing", 99, 0, 0, "unknown!!"), stream(mp3, 50)...))
	if tr.Declared.Encoder != "" || tr.Declared.EncoderDelay != nil || tr.PresentedSamples != nil || len(tr.Findings) != 1 || !strings.Contains(tr.Findings[0], "declares 99 frames") {
		t.Fatal("unknown encoder fields or frame disagreement", tr.Declared, tr.Findings)
	}
	vbri := frame(mp3)
	copy(vbri[36:], "VBRI")
	binary.BigEndian.PutUint32(vbri[36+14:], 7)
	if tr := probe(t, append(vbri, stream(mp3, 7)...)); tr.Declared.InfoFrame != "VBRI" || *tr.Declared.Frames != 7 || tr.Frames != 7 {
		t.Fatal(tr.Declared)
	}
}

func TestGapsAreReportedNeverSilentlySkipped(t *testing.T) {
	junk := bytes.Repeat([]byte{0x55}, 300)
	data := bytes.Join([][]byte{junk, stream(mp3, 10), junk, stream(mp3, 10), frame(mp3)[:200]}, nil)
	tr := probe(t, data)
	if tr.Frames != 20 || len(tr.Gaps) != 3 {
		t.Fatal(tr.Frames, tr.Gaps)
	}
	if tr.Gaps[0] != (Region{"unsynchronized_bytes", 0, 300}) || tr.Gaps[1].Kind != "unsynchronized_bytes" || tr.Gaps[2].Kind != "trailing_truncated_frame" || tr.Gaps[2].End != int64(len(data)) {
		t.Fatal(tr.Gaps)
	}
	// Only the gap between frames shifts later sample positions.
	if len(tr.Findings) != 2 || !strings.Contains(tr.Findings[0], "1 unindexed byte region(s) between frames") || !strings.Contains(tr.Findings[1], "2 unindexed region(s) outside") {
		t.Fatal(tr.Findings)
	}
	// A different stream after the first is not indexed as the same track.
	mixed := append(stream(mp3, 5), stream(spec{version: 3, layer: 1, bitrate: 9, rate: 1}, 5)...)
	if tr := probe(t, mixed); tr.Frames != 5 || len(tr.Gaps) != 1 {
		t.Fatal("stream change indexed", tr.Frames, tr.Gaps)
	}
}

func TestNonAudioAndBounds(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("not audio at all"), append([]byte{0xFF, 0xFB, 0x90, 0x00}, make([]byte, 50)...), bytes.Repeat([]byte{0xFF}, 5000)} {
		if _, err := Probe(context.Background(), data); !errors.Is(err, ErrNotMPEG) {
			t.Fatal("non-audio indexed", err)
		}
	}
	if _, err := Probe(context.Background(), id3Tag(4, 0, make([]byte, 100))[:50]); err == nil {
		t.Fatal("truncated ID3 tag accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Probe(ctx, stream(mp3, 10)); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
	for _, h := range [][]byte{{0xFF, 0xE8, 0x90, 0}, {0xFF, 0xF9, 0x90, 0}, {0xFF, 0xFB, 0x00, 0}, {0xFF, 0xFB, 0xF0, 0}, {0xFF, 0xFB, 0x9C, 0}, {0xFF, 0xFB, 0x90, 2}} {
		if _, ok := parseHeader(h); ok {
			t.Fatal("reserved or free-format header accepted", h)
		}
	}
}

func TestRangesAreFrameAlignedContiguousAndPrerolled(t *testing.T) {
	frames := make([][]byte, 30)
	for i := range frames {
		s := mp3
		if i == 10 {
			s.reservoir = 500 // borrows from two earlier frames (381 bytes each)
		}
		frames[i] = frame(s)
	}
	data := bytes.Join(frames, nil)
	tr := probe(t, data)
	r, err := tr.Range(data, 0, 4096)
	if err != nil || r.PrerollFrames != 0 || !r.PrerollComplete || r.Frames != 9 || r.SampleStart != 0 || r.SampleEnd != 9*1152 || r.NextFrame != 9 || r.ByteEnd != 9*417 {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = tr.Range(data, 5, 4096)
	if err != nil || r.PrerollFrames != 1 || r.ByteStart != 4*417 || r.PrerollStart != 4*1152 || r.SampleStart != 5*1152 {
		t.Fatalf("overlap preroll %+v %v", r, err)
	}
	r, err = tr.Range(data, 10, 4096)
	if err != nil || r.PrerollFrames != 2 || !r.PrerollComplete || r.ByteStart != 8*417 {
		t.Fatalf("reservoir preroll %+v %v", r, err)
	}
	if r, err := tr.Range(data, 10, 1000); !errors.Is(err, ErrRangeTooSmall) || r.MinimumMaxBytes != 3*417 {
		t.Fatal("undersized range", r, err)
	}
	r, err = tr.Range(data, 29, 1<<20)
	if err != nil || !r.EndOfTrack || r.NextFrame != 0 || r.SampleEnd != tr.EncodedSamples {
		t.Fatal("end of track", r, err)
	}
	if _, err := tr.Range(data, 30, 4096); err == nil {
		t.Fatal("frame past the end accepted")
	}
	if f, err := tr.FrameForSample(1152*7 + 3); err != nil || f != 7 {
		t.Fatal(f, err)
	}
	for _, s := range []int64{-1, tr.EncodedSamples} {
		if _, err := tr.FrameForSample(s); err == nil {
			t.Fatal("sample outside the track accepted", s)
		}
	}
}

func TestRangesStopAtGapsAndReportIncompletePreroll(t *testing.T) {
	data := bytes.Join([][]byte{stream(mp3, 5), bytes.Repeat([]byte{0x55}, 100), stream(mp3, 5)}, nil)
	tr := probe(t, data)
	r, err := tr.Range(data, 2, 1<<20)
	if err != nil || r.Frames != 3 || !r.StoppedAtGap || r.NextFrame != 5 || r.ByteEnd != 5*417 {
		t.Fatalf("range crossed a gap %+v %v", r, err)
	}
	r, err = tr.Range(data, 5, 1<<20)
	if err != nil || r.PrerollFrames != 0 || r.PrerollComplete || r.ByteStart != 5*417+100 {
		t.Fatalf("preroll across a gap %+v %v", r, err)
	}
	layer2 := stream(spec{version: 3, layer: 2, bitrate: 10, rate: 1}, 10)
	tr = probe(t, layer2)
	if r, err := tr.Range(layer2, 5, 1<<20); err != nil || r.PrerollFrames != 0 || !r.PrerollComplete {
		t.Fatal("Layer II preroll", r, err)
	}
}

func FuzzProbe(f *testing.F) {
	f.Add(stream(mp3, 3))
	f.Add(append(infoFrame("Info", 2, 576, 100, "LAME3.100"), stream(mp3, 2)...))
	f.Add(append(id3Tag(4, 0x40, append([]byte{0, 0, 0, 6, 1, 0}, id3Frame("TIT2", true, []byte{3, 'a'})...)), stream(mp3, 2)...))
	f.Add([]byte("ID3\x04\x00\x10\x00\x00\x00\x00"))
	f.Fuzz(func(t *testing.T, data []byte) {
		tr, err := Probe(context.Background(), data)
		if err != nil {
			return
		}
		prev := int64(-1)
		for i, o := range tr.offsets {
			if o <= prev || o+int64(tr.headers[i].length) > int64(len(data)) {
				t.Fatal("frame index out of order or bounds")
			}
			prev = o
		}
		if r, err := tr.Range(data, tr.Frames-1, 1<<20); err == nil && (r.ByteStart < 0 || r.ByteEnd > int64(len(data)) || r.ByteStart >= r.ByteEnd) {
			t.Fatal("range outside the file", r)
		}
	})
}

// MPEG-2 Layer III frames hold one granule, so two earlier frames are decoder
// history; MPEG-1 frames hold two, so one is (found by bit-exact comparison
// against a reference decoder).
func TestPrerollHistoryDependsOnGranulesPerFrame(t *testing.T) {
	lsf := spec{version: 2, layer: 1, bitrate: 8, rate: 0, mono: true}
	data := stream(lsf, 10)
	tr := probe(t, data)
	if r, err := tr.Range(data, 5, 1<<20); err != nil || r.PrerollFrames != 2 || !r.PrerollComplete {
		t.Fatalf("MPEG-2 history %+v %v", r, err)
	}
	// At the stream start a whole-file decoder has the same empty history.
	if r, err := tr.Range(data, 1, 1<<20); err != nil || r.PrerollFrames != 1 || !r.PrerollComplete {
		t.Fatalf("stream start %+v %v", r, err)
	}
	mpeg1 := stream(mp3, 10)
	tr = probe(t, mpeg1)
	if r, err := tr.Range(mpeg1, 5, 1<<20); err != nil || r.PrerollFrames != 1 {
		t.Fatalf("MPEG-1 history %+v %v", r, err)
	}
	// A reservoir reaching before the first frame matches a whole-file decode.
	first := frame(spec{version: 3, layer: 1, bitrate: 9, rate: 0, reservoir: 300})
	data = append(append(first, frame(mp3)...), stream(mp3, 3)...)
	tr = probe(t, data)
	if r, err := tr.Range(data, 1, 1<<20); err != nil || r.PrerollFrames != 1 || !r.PrerollComplete {
		t.Fatalf("reservoir at stream start %+v %v", r, err)
	}
}
