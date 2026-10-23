package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"reflect"
	"runtime"
	"slices"
	"testing"
)

type packetFixture struct {
	data    []byte
	packets []MP4Packet
}

// This writer declares independent chunk placements and timing, including
// physical chunk order unlike track order. Expected intervals come from those
// declarations, not from the parser or packet cursor under test.
func makePacketFixture(sizes []uint32, per []int, order []int, times [][2]uint32, co64, constant bool, compact int) packetFixture {
	var chunks [][]byte
	var packets []MP4Packet
	p, tick, run, left := 0, int64(0), 0, int(times[0][0])
	for _, count := range per {
		var chunk []byte
		for range count {
			for left == 0 {
				run++
				left = int(times[run][0])
			}
			start := len(chunk)
			chunk = append(chunk, bytes.Repeat([]byte{byte(p + 1)}, int(sizes[p]))...)
			end := tick + int64(times[run][1])
			packets = append(packets, MP4Packet{p, int64(start), int64(len(chunk)), tick, end})
			tick, p, left = end, p+1, left-1
		}
		chunks = append(chunks, chunk)
	}
	var media []byte
	offsets := make([]int64, len(chunks))
	for _, chunk := range order {
		media = append(media, bytes.Repeat([]byte{0xee}, 13)...)
		offsets[chunk] = int64(len(media))
		media = append(media, chunks[chunk]...)
	}
	ftyp := box("ftyp", []byte("M4A "), u32(0), []byte("isom"))
	movie := func(base int64) []byte {
		stts, stsc := [][]byte{zeros(4), u32(uint32(len(times)))}, [][]byte{zeros(4), u32(uint32(len(per)))}
		for _, e := range times {
			stts = append(stts, u32(e[0]), u32(e[1]))
		}
		for i, count := range per {
			stsc = append(stsc, u32(uint32(i+1)), u32(uint32(count)), u32(1))
		}
		constantSize := uint32(0)
		if constant {
			constantSize = sizes[0]
		}
		stsz := [][]byte{zeros(4), u32(constantSize), u32(uint32(len(sizes)))}
		if !constant {
			for _, n := range sizes {
				stsz = append(stsz, u32(n))
			}
		}
		sizeBox := box("stsz", stsz...)
		if compact != 0 {
			values := []byte{}
			for i, n := range sizes {
				switch compact {
				case 4:
					if i%2 == 0 {
						values = append(values, byte(n<<4))
					} else {
						values[len(values)-1] |= byte(n)
					}
				case 8:
					values = append(values, byte(n))
				case 16:
					values = append(values, u16(uint16(n))...)
				}
			}
			sizeBox = box("stz2", zeros(4), zeros(3), []byte{byte(compact)}, u32(uint32(len(sizes))), values)
		}
		stco := [][]byte{zeros(4), u32(uint32(len(offsets)))}
		for _, offset := range offsets {
			if co64 {
				stco = append(stco, u64(uint64(base+offset)))
			} else {
				stco = append(stco, u32(uint32(base+offset)))
			}
		}
		kind := "stco"
		if co64 {
			kind = "co64"
		}
		return box("moov", box("mvhd", zeros(12), u32(1000), u32(uint32(tick)), zeros(80)),
			box("trak", box("tkhd", zeros(12), u32(1), zeros(64)), box("mdia",
				box("mdhd", zeros(12), u32(1000), u32(uint32(tick)), zeros(4)),
				box("hdlr", zeros(8), []byte("soun"), zeros(12)), box("minf", box("stbl",
					box("stsd", zeros(4), u32(1), audioEntry(defaultSpec())), box("stts", stts...),
					box("stsc", stsc...), sizeBox, box(kind, stco...))))))
	}
	base := int64(len(ftyp) + len(movie(0)) + 8)
	p = 0
	for chunk, count := range per {
		for range count {
			packets[p].ByteStart += base + offsets[chunk]
			packets[p].ByteEnd += base + offsets[chunk]
			p++
		}
	}
	return packetFixture{slices.Concat(ftyp, movie(base), box("mdat", media)), packets}
}

type packetReader struct {
	*bytes.Reader
	read func([]byte, int64) (int, error)
}

func (r *packetReader) ReadAt(p []byte, off int64) (int, error) {
	if r.read != nil {
		return r.read(p, off)
	}
	return r.Reader.ReadAt(p, off)
}

func TestMP4PacketRangesKeepOriginalSpansAndDecodeTicks(t *testing.T) {
	for _, variant := range []string{"variable", "constant", "co64", "compact4", "compact8", "compact16"} {
		t.Run(variant, func(t *testing.T) {
			sizes := []uint32{3, 5, 9, 7, 1, 8}
			if variant == "constant" {
				sizes = []uint32{4, 4, 4, 4, 4, 4}
			}
			compact := map[string]int{"compact4": 4, "compact8": 8, "compact16": 16}[variant]
			if compact == 16 {
				sizes[0], sizes[2] = 300, 513
			}
			f := makePacketFixture(sizes, []int{2, 1, 3}, []int{2, 0, 1}, [][2]uint32{{1, 9}, {0, 99}, {2, 23}, {3, 0}}, variant == "co64", variant == "constant", compact)
			r := &packetReader{Reader: bytes.NewReader(f.data)}
			media := probeMP4(t, f.data).MediaData[0]
			r.read = func(p []byte, off int64) (int, error) {
				if off < media.End && off+int64(len(p)) > media.Start {
					t.Fatal("indexing read an audio body", off, len(p))
				}
				return r.Reader.ReadAt(p, off)
			}
			index, err := IndexMP4(context.Background(), r, int64(len(f.data)))
			if err != nil || !reflect.DeepEqual(index.Summary, probeMP4(t, f.data)) {
				t.Fatal("packet index changed the old summary", index.Summary, err)
			}
			r.read = nil
			for first := range sizes {
				out, err := index.ReadRange(context.Background(), first, 65536)
				var want []byte
				for _, p := range f.packets[first:] {
					want = append(want, f.data[p.ByteStart:p.ByteEnd]...)
				}
				if err != nil || out.Extractor != MP4PacketExtractor || out.Timescale != 1000 || out.Timeline != "media_decode_ticks" || !out.EndOfTrack || out.NextPacket != len(sizes) || !reflect.DeepEqual(out.Packets, f.packets[first:]) || !bytes.Equal(out.Data, want) {
					t.Fatal("source selection or non-PCM timeline changed", first, out, err)
				}
			}
			index.Summary.Timescale, index.Summary.Samples = 0, 0
			out, err := index.ReadRange(context.Background(), 1, int(sizes[1]))
			if err != nil || len(out.Packets) != 1 || out.Packets[0] != f.packets[1] || out.NextPacket != 2 || out.Stop != "byte_limit" || out.EndOfTrack {
				t.Fatal("public summary mutated private evidence or byte cap skipped a packet", out, err)
			}
			out, err = index.ReadRange(context.Background(), 1, int(sizes[1])-1)
			if !errors.Is(err, ErrMP4PacketRangeTooSmall) || out.RequiredBytes != int64(sizes[1]) || len(out.Data) != 0 || len(out.Packets) != 0 || out.NextPacket != 1 {
				t.Fatal("small cap read or skipped the first packet", out, err)
			}
		})
	}
}

func TestMP4PacketRangeLimitsAndEmptyBoundary(t *testing.T) {
	s := defaultSpec()
	s.sizes, s.perChunk = make([]uint32, 260), 10
	for i := range s.sizes {
		s.sizes[i] = 1
	}
	data := buildMP4(s)
	index, err := IndexMP4(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for first := 0; first < len(s.sizes); {
		out, err := index.ReadRange(context.Background(), first, 65536)
		if err != nil || len(out.Packets) != min(128, len(s.sizes)-first) || len(out.Data) != len(out.Packets) || out.NextPacket != first+len(out.Packets) || out.EndOfTrack != (out.NextPacket == len(s.sizes)) {
			t.Fatal("packet limit or continuation", out, err)
		}
		first = out.NextPacket
	}
	for _, request := range [][2]int{{-1, 4}, {260, 4}, {0, 0}, {0, 65537}} {
		if out, err := index.ReadRange(context.Background(), request[0], request[1]); err == nil || len(out.Data) != 0 {
			t.Fatal("invalid request delivered bytes", request, out, err)
		}
	}
	f := makePacketFixture([]uint32{3, 0, 2}, []int{3}, []int{0}, [][2]uint32{{3, 7}}, false, false, 0)
	index, err = IndexMP4(context.Background(), bytes.NewReader(f.data), int64(len(f.data)))
	if err != nil {
		t.Fatal(err)
	}
	out, err := index.ReadRange(context.Background(), 0, 64)
	if err != nil || len(out.Packets) != 1 || out.NextPacket != 1 || out.Stop != "unsupported_empty_packet" || out.EndOfTrack {
		t.Fatal("empty packet skipped", out, err)
	}
	if out, err := index.ReadRange(context.Background(), 1, 64); !errors.Is(err, ErrMP4PacketUnsupported) || len(out.Data) != 0 || out.NextPacket != 1 {
		t.Fatal("empty boundary became delivery", out, err)
	}
}

func TestMP4PacketFailuresDiscardAllPayloadAndDoNotAdvance(t *testing.T) {
	for _, failure := range []string{"short", "error", "canceled"} {
		t.Run(failure, func(t *testing.T) {
			data := buildMP4(defaultSpec())
			r := &packetReader{Reader: bytes.NewReader(data)}
			index, err := IndexMP4(context.Background(), r, int64(len(data)))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls, charged := 0, 0
			r.read = func(p []byte, off int64) (int, error) {
				calls++
				charged += len(p)
				if calls == 2 {
					switch failure {
					case "short":
						return len(p) - 1, nil
					case "error":
						return 0, io.ErrClosedPipe
					case "canceled":
						cancel()
					}
				}
				return r.Reader.ReadAt(p, off)
			}
			out, err := index.ReadRange(ctx, 3, 65536)
			if err == nil || len(out.Data) != 0 || len(out.Packets) != 0 || out.NextPacket != 3 || out.Stop != "failed" || calls != 2 || charged > 65536 {
				t.Fatal("failed work delivered payload, advanced, or exceeded reads", out, err, calls, charged)
			}
			if failure == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation", err)
			}
		})
	}
}

func TestMP4PacketIndexRefusesFindingsAndInvalidSources(t *testing.T) {
	for _, kind := range []string{"fragmented", "encrypted", "unknown", "two tracks", "misplaced", "inconsistent", "cut", "zero timescale"} {
		t.Run(kind, func(t *testing.T) {
			s := defaultSpec()
			switch kind {
			case "fragmented":
				s.mvex = true
			case "encrypted":
				s.sinf = true
			case "unknown":
				s.entry = "zzzz"
			case "two tracks":
				s.extraAudio = true
			case "misplaced":
				s.offsetBias = -100
			case "inconsistent":
				s.sttsCount = 3
			}
			data := buildMP4(s)
			if kind == "cut" {
				data = data[:len(data)-4]
			}
			if kind == "zero timescale" {
				at := bytes.Index(data, []byte("mdhd"))
				binary.BigEndian.PutUint32(data[at+16:], 0)
			}
			index, err := IndexMP4(context.Background(), bytes.NewReader(data), int64(len(data)))
			if !errors.Is(err, ErrMP4PacketUnsupported) {
				t.Fatal("unsupported source indexed", index.Summary, err)
			}
			index.Summary.Findings = nil
			if out, err := index.ReadRange(context.Background(), 0, 65536); !errors.Is(err, ErrMP4PacketUnsupported) || len(out.Data) != 0 {
				t.Fatal("summary supplied permission", out, err)
			}
		})
	}
	if _, err := IndexMP4(context.Background(), nil, 1); !errors.Is(err, ErrMP4PacketUnsupported) {
		t.Fatal(err)
	}
	if _, err := IndexMP4(context.Background(), bytes.NewReader(nil), 2<<30+1); !errors.Is(err, ErrMP4PacketUnsupported) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := IndexMP4(ctx, bytes.NewReader(nil), 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if out, err := (MP4PacketIndex{}).ReadRange(ctx, 0, 1); !errors.Is(err, context.Canceled) || len(out.Data) != 0 {
		t.Fatal(out, err)
	}
}

func TestMP4PacketIndexDoesNotAllocatePerPacket(t *testing.T) {
	// Two million one-byte packets occupy one chunk and one timing run. The
	// metadata is tiny; a second per-packet location index would exceed 30 MiB.
	const count = 2_000_000
	ftyp := box("ftyp", []byte("M4A "), u32(0))
	movie := func(base int64) []byte {
		return box("moov", box("mvhd", zeros(12), u32(1000), u32(1000), zeros(80)), box("trak",
			box("tkhd", zeros(12), u32(1), zeros(64)), box("mdia", box("mdhd", zeros(12), u32(1000), u32(count), zeros(4)),
				box("hdlr", zeros(8), []byte("soun"), zeros(12)), box("minf", box("stbl",
					box("stsd", zeros(4), u32(1), audioEntry(defaultSpec())), box("stts", zeros(4), u32(1), u32(count), u32(1)),
					box("stsc", zeros(4), u32(1), u32(1), u32(count), u32(1)), box("stsz", zeros(4), u32(1), u32(count)),
					box("stco", zeros(4), u32(1), u32(uint32(base))))))))
	}
	base := int64(len(ftyp) + len(movie(0)) + 8)
	data := slices.Concat(ftyp, movie(base), box("mdat", make([]byte, count)))
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	index, err := IndexMP4(context.Background(), bytes.NewReader(data), int64(len(data)))
	runtime.ReadMemStats(&after)
	if err != nil || after.TotalAlloc-before.TotalAlloc > 4<<20 {
		t.Fatal("packet-proportional index allocation", after.TotalAlloc-before.TotalAlloc, err)
	}
	out, err := index.ReadRange(context.Background(), count-2, 2)
	if err != nil || len(out.Packets) != 2 || !out.EndOfTrack || out.Packets[0].DecodeStart != count-2 {
		t.Fatal(out, err)
	}
}

type packetCancelContext struct {
	context.Context
	cancel    context.CancelFunc
	calls, at int
}

func (c *packetCancelContext) Err() error {
	c.calls++
	if c.calls == c.at {
		c.cancel()
	}
	return c.Context.Err()
}

func TestMP4PacketMetadataWalkCancellationPrecedesBodyReads(t *testing.T) {
	sizes := make([]uint32, 12000)
	for i := range sizes {
		sizes[i] = 1
	}
	f := makePacketFixture(sizes, []int{len(sizes)}, []int{0}, [][2]uint32{{uint32(len(sizes)), 1}}, false, false, 0)
	r := &packetReader{Reader: bytes.NewReader(f.data)}
	index, err := IndexMP4(context.Background(), r, int64(len(f.data)))
	if err != nil {
		t.Fatal(err)
	}
	r.read = func([]byte, int64) (int, error) { t.Fatal("canceled metadata walk read audio"); return 0, nil }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stepped := &packetCancelContext{Context: ctx, cancel: cancel, at: 3}
	if out, err := index.ReadRange(stepped, 11000, 64); !errors.Is(err, context.Canceled) || len(out.Data) != 0 || out.NextPacket != 11000 || stepped.calls != 3 {
		t.Fatal("metadata walk ignored active cancellation", out, err, stepped.calls)
	}
}

func TestMP4PacketOversizeAndMetadataFailureNeverDeliver(t *testing.T) {
	s := defaultSpec()
	s.sizes, s.perChunk = []uint32{65537}, 1
	data := buildMP4(s)
	r := &packetReader{Reader: bytes.NewReader(data)}
	index, err := IndexMP4(context.Background(), r, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	r.read = func([]byte, int64) (int, error) { t.Fatal("oversize body read"); return 0, nil }
	if out, err := index.ReadRange(context.Background(), 0, 65536); !errors.Is(err, ErrMP4PacketRangeTooSmall) || out.RequiredBytes != 65537 || len(out.Data) != 0 {
		t.Fatal(out, err)
	}
	r.read = func([]byte, int64) (int, error) { return 0, io.ErrClosedPipe }
	index, err = IndexMP4(context.Background(), r, int64(len(data)))
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal("metadata failure lost", err)
	}
	if out, err := index.ReadRange(context.Background(), 0, 64); !errors.Is(err, ErrMP4PacketUnsupported) || len(out.Data) != 0 {
		t.Fatal(out, err)
	}
}

func TestMP4PacketIndexRefusesOverlappingAndMixedDescriptions(t *testing.T) {
	for _, kind := range []string{"overlap", "mixed", "equivalent descriptions"} {
		t.Run(kind, func(t *testing.T) {
			data := buildMP4(defaultSpec())
			if kind == "overlap" {
				at := bytes.Index(data, []byte("stco"))
				copy(data[at+16:at+20], data[at+12:at+16])
			} else {
				// Replace the one description with two different supported codecs,
				// updating every enclosing size and chunk offset independently.
				at := bytes.Index(data, []byte("stsd")) - 4
				oldSize := int(binary.BigEndian.Uint32(data[at:]))
				entry := box("alac", zeros(6), u16(1), zeros(8), u16(2), u16(16), zeros(4), u32(44100<<16))
				if kind == "equivalent descriptions" {
					entry = append([]byte{}, data[at+16:at+oldSize]...)
				}
				replacement := box("stsd", zeros(4), u32(2), data[at+16:at+oldSize], entry)
				delta := len(replacement) - oldSize
				for _, typ := range []string{"moov", "trak", "mdia", "minf", "stbl"} {
					p := bytes.Index(data, []byte(typ)) - 4
					binary.BigEndian.PutUint32(data[p:], binary.BigEndian.Uint32(data[p:])+uint32(delta))
				}
				data = slices.Concat(data[:at], replacement, data[at+oldSize:])
				p := bytes.Index(data, []byte("stco"))
				for i := 0; i < 5; i++ {
					q := p + 12 + 4*i
					binary.BigEndian.PutUint32(data[q:], binary.BigEndian.Uint32(data[q:])+uint32(delta))
				}
			}
			index, err := IndexMP4(context.Background(), bytes.NewReader(data), int64(len(data)))
			finding := MP4SamplesOverlap
			if kind == "mixed" {
				finding = MP4DescriptionsDiffer
			}
			if !errors.Is(err, ErrMP4PacketUnsupported) || kind != "equivalent descriptions" && !slices.Contains(index.Summary.Findings, finding) {
				t.Fatal("wrong refusal", index.Summary, err)
			}
			if kind == "equivalent descriptions" && len(index.Summary.Findings) != 0 {
				t.Fatal("new packet opt-in changed old assessment", index.Summary)
			}
		})
	}
}

func TestMP4PacketSampleDescriptionIsBoundedOriginalCopy(t *testing.T) {
	entry := audioEntry(defaultSpec())
	index := MP4PacketIndex{track: &mp4Track{entries: 1, descriptions: bytes.Clone(entry), stsdVersion: 1}}
	first, version, err := index.SampleDescription()
	if err != nil || version != 1 || !bytes.Equal(first, entry) {
		t.Fatal(first, version, err)
	}
	first[0] ^= 255
	again, _, err := index.SampleDescription()
	if err != nil || !bytes.Equal(again, entry) {
		t.Fatal("caller modified index initialization", err)
	}
	index.Summary.Codec = "changed"
	if b, _, err := index.SampleDescription(); err != nil || !bytes.Equal(b, entry) {
		t.Fatal("summary changed original bytes", err)
	}
	for _, tc := range []struct {
		name  string
		index MP4PacketIndex
		want  error
	}{
		{"zero", MP4PacketIndex{}, ErrMP4PacketUnsupported},
		{"multiple", MP4PacketIndex{track: &mp4Track{entries: 2, descriptions: entry}}, ErrMP4PacketUnsupported},
		{"short", MP4PacketIndex{track: &mp4Track{entries: 1, descriptions: []byte{1}}}, ErrMP4PacketUnsupported},
		{"tail", MP4PacketIndex{track: &mp4Track{entries: 1, descriptions: append(bytes.Clone(entry), 1)}}, ErrMP4PacketUnsupported},
		{"oversize", MP4PacketIndex{track: &mp4Track{entries: 1, descriptions: make([]byte, 16<<10+1)}}, ErrBudget},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, v, err := tc.index.SampleDescription()
			if !errors.Is(err, tc.want) || b != nil || v != 0 {
				t.Fatal(b, v, err)
			}
		})
	}
}

func TestMP4ClockRetainsExactVersionOneDeclarations(t *testing.T) {
	const mediaTicks uint64 = 1<<56 + 77
	const movieTicks uint64 = 1<<55 + 123
	s := defaultSpec()
	ftyp := box("ftyp", []byte("M4A "), u32(0))
	movie := func(base int64) []byte {
		track := table(&s, []uint32{3, 5}, 2, []int64{base}, audioEntry(s), "soun", 1, 44100, 1024, 0)
		old := box("mdhd", zeros(12), u32(44100), u32(2048), u16(0x15c7), zeros(2))
		replacement := box("mdhd", []byte{1, 0, 0, 0}, zeros(16), u32(44100), u64(mediaTicks), u16(0x15c7), zeros(2))
		if bytes.Count(track, old) != 1 {
			t.Fatal("fixture media header")
		}
		track = bytes.Replace(track, old, replacement, 1)
		binary.BigEndian.PutUint32(track, uint32(len(track)))
		parent := bytes.Index(track, []byte("mdia")) - 4
		binary.BigEndian.PutUint32(track[parent:], binary.BigEndian.Uint32(track[parent:])+12)
		return box("moov", box("mvhd", []byte{1, 0, 0, 0}, zeros(16), u32(1000), u64(movieTicks), zeros(80)), track)
	}
	base := int64(len(ftyp) + len(movie(0)) + 8)
	data := slices.Concat(ftyp, movie(base), box("mdat", []byte{1, 2, 3, 4, 5, 6, 7, 8}))
	index, err := IndexMP4(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(index.Summary, err)
	}
	clock, err := index.Clock()
	if err != nil || clock.DeclaredMediaTicks != mediaTicks || clock.DeclaredMovieTicks != movieTicks || clock.EncodedMediaTicks != 2048 || clock.MediaTimescale != 44100 || clock.MovieTimescale != 1000 || clock.EditList {
		t.Fatal("declarations rounded, saturated or borrowed", clock, err)
	}
	index.Summary.MovieDurationMs, index.Summary.MediaDurationMs, index.Summary.EncodedDurationMs = 0, 0, 0
	again, err := index.Clock()
	if err != nil || again != clock {
		t.Fatal("mutable summary changed clock evidence", again, err)
	}
	if _, err := (MP4PacketIndex{}).Clock(); !errors.Is(err, ErrMP4PacketUnsupported) {
		t.Fatal("empty index acquired clock evidence", err)
	}
}
