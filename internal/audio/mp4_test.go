package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"slices"
	"testing"
)

func box(typ string, parts ...[]byte) []byte {
	body := bytes.Join(parts, nil)
	out := binary.BigEndian.AppendUint32(nil, uint32(8+len(body)))
	return append(append(out, typ...), body...)
}

func u16(v uint16) []byte { return binary.BigEndian.AppendUint16(nil, v) }
func u32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }
func u64(v uint64) []byte { return binary.BigEndian.AppendUint64(nil, v) }
func zeros(n int) []byte  { return make([]byte, n) }

// mp4Spec describes a synthetic file: one AAC track of samples of the given
// sizes, each lasting 1024 units at 44,100 Hz, in chunks of perChunk.
type mp4Spec struct {
	sizes      []uint32
	perChunk   int
	entry      string
	sinf       bool
	chapters   []string
	nero       []uint64
	extraAudio bool
	mvex       bool
	sttsCount  uint32
	offsetBias int64
	co64       bool
	title      string
	// unreferenced keeps the text track but drops the audio track's
	// reference to it, so it is not a chapter track.
	unreferenced bool
}

func defaultSpec() mp4Spec {
	sizes := make([]uint32, 40)
	for i := range sizes {
		sizes[i] = uint32(100 + i)
	}
	return mp4Spec{sizes: sizes, perChunk: 8, entry: "mp4a"}
}

func audioEntry(s mp4Spec) []byte {
	esds := box("esds", zeros(4),
		[]byte{0x03, 22}, u16(1), []byte{0},
		[]byte{0x04, 17, 0x40, 0x15}, zeros(3), u32(70000), u32(64000),
		[]byte{0x05, 2, 0x12, 0x08})
	children := [][]byte{esds}
	if s.sinf {
		children = append(children, box("sinf", box("frma", []byte("mp4a"))))
	}
	return box(s.entry, append([][]byte{zeros(6), u16(1), zeros(8), u16(2), u16(16), zeros(4), u32(44100 << 16)}, children...)...)
}

func table(t *mp4Spec, sizes []uint32, perChunk int, offsets []int64, entry []byte, handler string, id uint32, scale uint32, delta uint32, chap uint32) []byte {
	count := uint32(len(sizes))
	stts := box("stts", zeros(4), u32(1), u32(count), u32(delta))
	if t != nil && t.sttsCount != 0 && handler == "soun" {
		stts = box("stts", zeros(4), u32(1), u32(t.sttsCount), u32(delta))
	}
	stsz := [][]byte{zeros(4), u32(0), u32(count)}
	for _, v := range sizes {
		stsz = append(stsz, u32(v))
	}
	var stco []byte
	if t != nil && t.co64 {
		parts := [][]byte{zeros(4), u32(uint32(len(offsets)))}
		for _, o := range offsets {
			parts = append(parts, u64(uint64(o)))
		}
		stco = box("co64", parts...)
	} else {
		parts := [][]byte{zeros(4), u32(uint32(len(offsets)))}
		for _, o := range offsets {
			parts = append(parts, u32(uint32(o)))
		}
		stco = box("stco", parts...)
	}
	stbl := box("stbl",
		box("stsd", zeros(4), u32(1), entry),
		stts,
		box("stsc", zeros(4), u32(1), u32(1), u32(uint32(perChunk)), u32(1)),
		box("stsz", stsz...),
		stco)
	duration := count * delta
	parts := [][]byte{box("tkhd", zeros(12), u32(id), zeros(64))}
	if chap != 0 {
		parts = append(parts, box("tref", box("chap", u32(chap))))
	}
	parts = append(parts, box("mdia",
		box("mdhd", zeros(12), u32(scale), u32(duration), u16(0x15c7), zeros(2)), // "eng"
		box("hdlr", zeros(8), []byte(handler), zeros(12)),
		box("minf", stbl)))
	return box("trak", parts...)
}

// chunkOffsets places chunks of perChunk samples contiguously from base.
func chunkOffsets(sizes []uint32, perChunk int, base int64) []int64 {
	var out []int64
	at := base
	for i, s := range sizes {
		if i%perChunk == 0 {
			out = append(out, at)
		}
		at += int64(s)
	}
	return out
}

func buildMP4(s mp4Spec) []byte {
	ftyp := box("ftyp", []byte("M4B "), u32(0), []byte("M4B isommp42"))
	var chapterSamples [][]byte
	for _, title := range s.chapters {
		chapterSamples = append(chapterSamples, append(u16(uint16(len(title))), title...))
	}
	moov := func(base int64) []byte {
		offsets := chunkOffsets(s.sizes, s.perChunk, base+s.offsetBias)
		var media int64
		for _, v := range s.sizes {
			media += int64(v)
		}
		chap := uint32(0)
		if len(s.chapters) > 0 && !s.unreferenced {
			chap = 2
		}
		parts := [][]byte{
			box("mvhd", zeros(12), u32(1000), u32(uint32(len(s.sizes))*1024*1000/44100), zeros(80)),
			table(&s, s.sizes, s.perChunk, offsets, audioEntry(s), "soun", 1, 44100, 1024, chap),
		}
		if len(s.chapters) > 0 {
			sizes := make([]uint32, len(s.chapters))
			for i, c := range chapterSamples {
				sizes[i] = uint32(len(c))
			}
			parts = append(parts, table(nil, sizes, len(sizes), chunkOffsets(sizes, len(sizes), base+media), box("text", zeros(6), u16(1)), "text", 2, 1000, 400, 0))
		}
		if s.extraAudio {
			parts = append(parts, table(nil, s.sizes, s.perChunk, offsets, audioEntry(s), "soun", 3, 44100, 1024, 0))
		}
		if s.mvex {
			parts = append(parts, box("mvex", box("trex", zeros(24))))
		}
		udta := [][]byte{}
		if len(s.nero) > 0 {
			chpl := [][]byte{{1, 0, 0, 0}, zeros(4), {byte(len(s.nero))}}
			for i, start := range s.nero {
				name := []byte{'N', byte('0' + i)}
				chpl = append(chpl, u64(start), []byte{byte(len(name))}, name)
			}
			udta = append(udta, box("chpl", chpl...))
		}
		if s.title != "" {
			udta = append(udta, box("meta", zeros(4), box("hdlr", zeros(8), []byte("mdir"), zeros(12)),
				box("ilst", box("\xa9nam", box("data", u32(1), zeros(4), []byte(s.title))))))
		}
		if len(udta) > 0 {
			parts = append(parts, box("udta", udta...))
		}
		return box("moov", parts...)
	}
	head := len(ftyp) + len(moov(0)) + 8
	body := [][]byte{}
	for _, v := range s.sizes {
		body = append(body, bytes.Repeat([]byte{0xa5}, int(v)))
	}
	body = append(body, chapterSamples...)
	return slices.Concat(ftyp, moov(int64(head)), box("mdat", body...))
}

func probeMP4(t *testing.T, data []byte) MP4 {
	t.Helper()
	m, err := ProbeMP4(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("ProbeMP4: %v", err)
	}
	return m
}

func TestMP4SamplesChaptersAndMetadata(t *testing.T) {
	s := defaultSpec()
	s.chapters = []string{"Opening", "Second"}
	s.nero = []uint64{0, 4_000_000}
	s.title = "Synthetic Book"
	m := probeMP4(t, buildMP4(s))
	if len(m.Findings) != 0 || m.Codec != "aac" || m.AudioObjectType != 2 || m.SampleRate != 44100 || m.Channels != 2 || m.Samples != 40 || m.Chunks != 5 || m.Brand != "M4B" {
		t.Fatalf("summary = %+v", m)
	}
	if m.EncodedDurationMs != 928 || m.MediaDurationMs != 928 || m.Language != "eng" || m.AverageBitrate != 64000 || m.Metadata.Title != "Synthetic Book" {
		t.Fatalf("timing or metadata = %+v", m)
	}
	if m.SampleBytes != 40*100+780 || m.UnreferencedBytes != 0 {
		t.Fatalf("bytes = %d unreferenced %d", m.SampleBytes, m.UnreferencedBytes)
	}
	want := []Chapter{{"chapter1", "Opening", 0, 400}, {"chapter2", "Second", 400, 928}}
	if m.ChapterSource != "quicktime_chapter_track" || !slices.Equal(m.Chapters, want) {
		t.Fatalf("chapters %s %+v", m.ChapterSource, m.Chapters)
	}
	if len(m.Notes) != 0 {
		t.Fatalf("notes = %v", m.Notes)
	}
}

func TestMP4NeroChaptersAloneAndDiffering(t *testing.T) {
	s := defaultSpec()
	s.nero = []uint64{0, 3_000_000}
	m := probeMP4(t, buildMP4(s))
	if m.ChapterSource != "nero_chpl" || len(m.Chapters) != 2 || m.Chapters[1].StartMs != 300 || m.Chapters[0].EndMs != 300 || m.Chapters[1].Title != "N1" {
		t.Fatalf("nero chapters %+v", m.Chapters)
	}
	s.chapters = []string{"Only"}
	m = probeMP4(t, buildMP4(s))
	if m.ChapterSource != "quicktime_chapter_track" || len(m.Chapters) != 1 || !slices.Contains(m.Notes, MP4ChapterSourcesDiffer) {
		t.Fatalf("differing chapter sources %s %+v %v", m.ChapterSource, m.Chapters, m.Notes)
	}
}

func TestMP4UnreferencedTextTrackIsNotChapters(t *testing.T) {
	s := defaultSpec()
	s.chapters, s.unreferenced = []string{"Caption"}, true
	m := probeMP4(t, buildMP4(s))
	if len(m.Chapters) != 0 || m.ChapterSource != "" || !slices.Equal(m.Notes, []string{MP4OtherTracks}) || len(m.Findings) != 0 || m.Tracks != 2 {
		t.Fatalf("unreferenced text track: %+v", m)
	}
}

func TestMP4TruncationAndMisplacedSamplesAreFindings(t *testing.T) {
	data := buildMP4(defaultSpec())
	m := probeMP4(t, data[:len(data)-50])
	if !slices.Contains(m.Findings, MP4SamplesOutsideFile) || !slices.Contains(m.Findings, MP4BoxPastFile) {
		t.Fatalf("truncated findings = %v", m.Findings)
	}
	s := defaultSpec()
	s.offsetBias = -200
	if m := probeMP4(t, buildMP4(s)); !slices.Contains(m.Findings, MP4SamplesOutsideMediaData) {
		t.Fatalf("misplaced findings = %v", m.Findings)
	}
	s = defaultSpec()
	s.extraAudio = true
	if m := probeMP4(t, buildMP4(s)); !slices.Contains(m.Findings, MP4AudioTracksNotOne) || !slices.Contains(m.Findings, MP4SamplesOverlap) {
		t.Fatalf("shared samples findings = %v", m.Findings)
	}
	s = defaultSpec()
	s.sttsCount = 39
	if m := probeMP4(t, buildMP4(s)); !slices.Contains(m.Findings, MP4TablesInconsistent) {
		t.Fatalf("inconsistent tables findings = %v", m.Findings)
	}
	// Moving the movie box past a cut leaves nothing to index.
	s = defaultSpec()
	moovLast := buildMP4(s)
	if _, err := ProbeMP4(context.Background(), bytes.NewReader(moovLast[:100]), 100); err == nil {
		t.Fatal("a file cut inside its movie box was indexed")
	}
}

func TestMP4ProtectionFragmentsAndCodecs(t *testing.T) {
	s := defaultSpec()
	s.entry, s.sinf = "enca", true
	if m := probeMP4(t, buildMP4(s)); !slices.Contains(m.Findings, MP4Encrypted) || m.Codec != "encrypted" {
		t.Fatalf("encrypted = %v %s", m.Findings, m.Codec)
	}
	s = defaultSpec()
	s.entry = "zzzz"
	if m := probeMP4(t, buildMP4(s)); !slices.Contains(m.Findings, MP4CodecUnrecognized) {
		t.Fatalf("unknown codec = %v", m.Findings)
	}
	s = defaultSpec()
	s.mvex = true
	if m := probeMP4(t, buildMP4(s)); !slices.Contains(m.Findings, MP4Fragmented) {
		t.Fatalf("fragmented = %v", m.Findings)
	}
	s = defaultSpec()
	s.co64 = true
	if m := probeMP4(t, buildMP4(s)); len(m.Findings) != 0 || m.Samples != 40 {
		t.Fatalf("co64 = %+v", m)
	}
}

func TestMP4BrandsDropTheirPadding(t *testing.T) {
	if brand([]byte("M4A\x00")) != "M4A" || brand([]byte("M4B ")) != "M4B" || brand([]byte("is\x01m")) != "" {
		t.Fatal("brand padding or control bytes")
	}
}

func TestMP4StructureBoundsAndTrailingBytes(t *testing.T) {
	data := buildMP4(defaultSpec())
	if m := probeMP4(t, append(slices.Clone(data), 1, 2, 3)); !slices.Contains(m.Notes, MP4TrailingRegion) || len(m.Findings) != 0 {
		t.Fatalf("trailing = %v %v", m.Notes, m.Findings)
	}
	for name, input := range map[string][]byte{
		"no file type": data[len(box("ftyp", []byte("M4B "), u32(0), []byte("M4B isommp42"))):],
		"no movie":     slices.Concat(box("ftyp", []byte("M4A "), u32(0)), box("mdat", zeros(10))),
		"no audio":     slices.Concat(box("ftyp", []byte("M4A "), u32(0)), box("moov", box("mvhd", zeros(12), u32(1000), u32(0), zeros(80)))),
		"empty":        {},
	} {
		if _, err := ProbeMP4(context.Background(), bytes.NewReader(input), int64(len(input))); !errors.Is(err, ErrNotMP4) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// Decoder configuration nested in wave boxes is followed only so deep.
	wave := box("free")
	for range maxBoxDepth {
		wave = box("wave", wave)
	}
	entry := box("mp4a", zeros(6), u16(1), zeros(8), u16(2), u16(16), zeros(4), u32(44100<<16), wave)
	nested := slices.Concat(box("ftyp", []byte("M4A "), u32(0)), box("moov", table(nil, []uint32{4}, 1, []int64{0}, entry, "soun", 1, 44100, 1024, 0)))
	if _, err := ProbeMP4(context.Background(), bytes.NewReader(nested), int64(len(nested))); !errors.Is(err, ErrBudget) {
		t.Fatalf("unbounded nesting err = %v", err)
	}
	huge := slices.Concat(box("ftyp", []byte("M4A "), u32(0)), box("moov", table(nil, nil, 1, nil, audioEntry(defaultSpec()), "soun", 1, 44100, 1024, 0)))
	// A sample count beyond the bounds is refused before allocating.
	at := bytes.Index(huge, []byte("stsz")) + 12
	binary.BigEndian.PutUint32(huge[at:], maxSamples+1)
	if _, err := ProbeMP4(context.Background(), bytes.NewReader(huge), int64(len(huge))); !errors.Is(err, ErrBudget) {
		t.Fatalf("oversized table err = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ProbeMP4(ctx, bytes.NewReader(data), int64(len(data))); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled err = %v", err)
	}
}

func TestMP4LargeSizesAndCompactSampleSizes(t *testing.T) {
	// A 64-bit box size and a compact sample size table index like their
	// ordinary forms.
	ftyp := box("ftyp", []byte("M4A "), u32(0))
	stbl := func(base int64) []byte {
		return box("trak", box("tkhd", zeros(12), u32(1), zeros(64)), box("mdia",
			box("mdhd", zeros(12), u32(8000), u32(3*160), zeros(4)),
			box("hdlr", zeros(8), []byte("soun"), zeros(12)),
			box("minf", box("stbl",
				box("stsd", zeros(4), u32(1), box("alac", zeros(6), u16(1), zeros(8), u16(1), u16(16), zeros(4), u32(8000<<16))),
				box("stts", zeros(4), u32(1), u32(3), u32(160)),
				box("stsc", zeros(4), u32(1), u32(1), u32(3), u32(1)),
				box("stz2", zeros(4), zeros(3), []byte{8}, u32(3), []byte{7, 9, 11}),
				box("stco", zeros(4), u32(1), u32(uint32(base)))))))
	}
	moov := box("moov", box("mvhd", zeros(12), u32(1000), u32(60), zeros(80)), stbl(0))
	base := int64(len(ftyp) + len(moov) + 16)
	mdat := slices.Concat(u32(1), []byte("mdat"), u64(uint64(16+27)), zeros(27))
	data := slices.Concat(ftyp, box("moov", box("mvhd", zeros(12), u32(1000), u32(60), zeros(80)), stbl(base)), mdat)
	m := probeMP4(t, data)
	if len(m.Findings) != 0 || m.Codec != "alac" || m.Samples != 3 || m.SampleBytes != 27 || m.EncodedDurationMs != 60 || m.Channels != 1 {
		t.Fatalf("large and compact = %+v", m)
	}
}

func FuzzProbeMP4(f *testing.F) {
	s := defaultSpec()
	s.chapters = []string{"A", "B"}
	s.nero = []uint64{0, 10}
	s.title = "T"
	f.Add(buildMP4(s))
	f.Add(buildMP4(defaultSpec())[:200])
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := ProbeMP4(context.Background(), bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return
		}
		if m.Samples < 0 || m.SampleBytes < 0 || m.UnreferencedBytes < 0 || len(m.Chapters) > maxChapters {
			t.Fatalf("out of range summary %+v", m)
		}
		for _, c := range m.Chapters {
			if c.EndMs < c.StartMs {
				t.Fatalf("chapter ends before it starts: %+v", c)
			}
		}
	})
}
