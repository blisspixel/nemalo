package audio

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"sort"
	"strings"
)

// MP4Extractor identifies the MP4 sample-table rules: which boxes are read,
// how samples are located, and what counts as a finding. Changing them
// requires a new identifier.
const MP4Extractor = "nemalo.mp4.samples/2"

const (
	maxTopBoxes    = 10_000
	maxBoxes       = 200_000
	maxBoxDepth    = 16
	maxMovie       = 64 << 20
	maxSamples     = 8_000_000
	maxChunks      = 8_000_000
	maxTracks      = 64
	maxChapterText = 4096
	maxEntries     = 16
)

// ErrNotMP4 means no readable ISO base media movie with an audio track was
// found: no file type box, no movie box, or no audio track.
var ErrNotMP4 = errors.New("no MP4 movie with an audio track found")

// MP4 is the sample-table summary of an MP4 audio file, such as an M4B
// audiobook or an M4A track. Every sample of every track is located against
// the file's bytes without decoding audio. Encoded timing is the sum of the
// audio track's sample durations; declared timing is what the movie and media
// headers state. Edit lists and encoder priming are recorded, not applied.
type MP4 struct {
	Brand             string      `json:"brand"`
	Compatible        []string    `json:"compatible_brands"`
	Codec             string      `json:"codec"`
	SampleEntry       string      `json:"sample_entry"`
	ObjectType        int         `json:"object_type,omitempty"`
	AudioObjectType   int         `json:"audio_object_type,omitempty"`
	SampleRate        int         `json:"sample_rate"`
	Channels          int         `json:"channels"`
	AverageBitrate    int         `json:"average_bitrate,omitempty"`
	Timescale         uint32      `json:"timescale"`
	Samples           int         `json:"samples"`
	Chunks            int         `json:"chunks"`
	EncodedDurationMs int64       `json:"encoded_duration_ms"`
	MediaDurationMs   int64       `json:"declared_media_duration_ms"`
	MovieDurationMs   int64       `json:"declared_movie_duration_ms"`
	EditList          bool        `json:"edit_list"`
	Language          string      `json:"language,omitempty"`
	Tracks            int         `json:"tracks"`
	MediaData         []Region    `json:"media_data"`
	SampleBytes       int64       `json:"sample_bytes"`
	UnreferencedBytes int64       `json:"unreferenced_media_bytes"`
	Metadata          MP4Metadata `json:"metadata"`
	ChapterSource     string      `json:"chapter_source,omitempty"`
	Chapters          []Chapter   `json:"chapters"`
	Findings          []string    `json:"findings"`
	Notes             []string    `json:"notes"`
	Decoded           string      `json:"decoded"`
}

// MP4Metadata is iTunes-style item text as declared. Missing values stay empty.
type MP4Metadata struct {
	Title    string `json:"title,omitempty"`
	Artist   string `json:"artist,omitempty"`
	Album    string `json:"album,omitempty"`
	Narrator string `json:"narrator,omitempty"`
	Composer string `json:"composer,omitempty"`
	Year     string `json:"year,omitempty"`
}

// MP4 findings keep a file in review. Notes are recorded facts.
const (
	MP4SamplesOutsideFile      = "mp4_samples_outside_file"
	MP4SamplesOutsideMediaData = "mp4_samples_outside_media_data"
	MP4SamplesOverlap          = "mp4_samples_overlap"
	MP4BoxPastFile             = "mp4_box_extends_past_file"
	MP4Fragmented              = "mp4_fragments_not_indexed"
	MP4Encrypted               = "mp4_audio_encrypted"
	MP4AudioTracksNotOne       = "mp4_audio_tracks_not_one"
	MP4CodecUnrecognized       = "mp4_audio_codec_unrecognized"
	MP4NoAudioSamples          = "mp4_no_audio_samples"
	MP4TablesInconsistent      = "mp4_sample_tables_inconsistent"
	MP4ChaptersUnreadable      = "mp4_chapters_unreadable"
	MP4DescriptionsDiffer      = "mp4_sample_descriptions_differ"

	MP4TrailingRegion       = "mp4_unindexed_trailing_region"
	MP4DeclaredDiffers      = "mp4_declared_duration_differs"
	MP4UnreferencedMedia    = "mp4_unreferenced_media_data"
	MP4OtherTracks          = "mp4_non_audio_tracks"
	MP4OtherOutsideMedia    = "mp4_non_audio_samples_outside_media_data"
	MP4ChapterSourcesDiffer = "mp4_chapter_sources_differ"
)

// Encrypted reports protected audio samples, such as FairPlay or Audible
// encryption. Nemalo does not remove protection.
func (m MP4) Encrypted() bool { return slicesHas(m.Findings, MP4Encrypted) }

type mp4Location struct{ offset, size int64 }

type mp4Track struct {
	id         uint32
	handler    string
	timescale  uint32
	duration   uint64
	language   string
	entry      string
	codec      string
	objectType int
	aot        int
	rate       int
	channels   int
	bitrate    int
	encrypted  bool
	editList   bool
	chapters   []uint32
	count      int
	constSize  uint32
	sizes      []uint32
	stts       [][2]uint32
	stsc       [][3]uint32
	offsets    []int64
	haveTables int
	failed     bool
	// descriptions are the sample description entries, read once the whole
	// track is known, since its handler may follow its sample table.
	descriptions []byte
	stsdVersion  byte
	entries      int
	mixed        bool
}

type mp4Parser struct {
	ctx   context.Context
	boxes int
}

func be16(b []byte) uint16 { return binary.BigEndian.Uint16(b) }
func be32(b []byte) uint32 { return binary.BigEndian.Uint32(b) }
func be64(b []byte) uint64 { return binary.BigEndian.Uint64(b) }

var errMP4Malformed = errors.New("malformed MP4 box structure")

// walk visits each box in data. A box that does not fit its parent is
// malformed; nothing past it is read.
func (p *mp4Parser) walk(data []byte, depth int, visit func(typ string, body []byte) error) error {
	if depth > maxBoxDepth {
		return ErrBudget
	}
	for len(data) > 0 {
		p.boxes++
		if p.boxes > maxBoxes {
			return ErrBudget
		}
		if p.boxes%4096 == 0 {
			if err := p.ctx.Err(); err != nil {
				return err
			}
		}
		if len(data) < 8 {
			return errMP4Malformed
		}
		size, header := uint64(be32(data)), uint64(8)
		switch size {
		case 0:
			size = uint64(len(data))
		case 1:
			if len(data) < 16 {
				return errMP4Malformed
			}
			size, header = be64(data[8:]), 16
		}
		if size < header || size > uint64(len(data)) {
			return errMP4Malformed
		}
		if err := visit(string(data[4:8]), data[header:size]); err != nil {
			return err
		}
		data = data[size:]
	}
	return nil
}

// ProbeMP4 reads the box structure and sample tables of an MP4 audio file
// through r without reading its media data, and locates every sample.
func ProbeMP4(ctx context.Context, r interface {
	ReadAt([]byte, int64) (int, error)
}, size int64) (MP4, error) {
	m := MP4{Compatible: []string{}, MediaData: []Region{}, Chapters: []Chapter{}, Findings: []string{}, Notes: []string{}, Decoded: "not_decoded"}
	p := &mp4Parser{ctx: ctx}
	var movie []byte
	declaredMedia := []Region{}
	fragmented := false
	read := func(off, n int64) ([]byte, error) {
		b := make([]byte, n)
		got, err := r.ReadAt(b, off)
		if int64(got) == n {
			return b, nil
		}
		if err == nil {
			err = errors.New("short read")
		}
		return nil, err
	}
	for off, i := int64(0), 0; off < size; i++ {
		if i >= maxTopBoxes {
			return m, ErrBudget
		}
		if err := ctx.Err(); err != nil {
			return m, err
		}
		if size-off < 8 {
			m.Notes = append(m.Notes, MP4TrailingRegion)
			break
		}
		h, err := read(off, min(16, size-off))
		if err != nil {
			return m, err
		}
		boxSize, header := int64(be32(h)), int64(8)
		typ := string(h[4:8])
		switch {
		case boxSize == 0:
			boxSize = size - off
		case boxSize == 1:
			if len(h) < 16 || be64(h[8:]) > math.MaxInt64 {
				return m, errMP4Malformed
			}
			boxSize, header = int64(be64(h[8:])), 16
		}
		if i == 0 && typ != "ftyp" {
			return m, ErrNotMP4
		}
		if boxSize < header || !printableType(typ) {
			if i == 0 {
				return m, ErrNotMP4
			}
			// Bytes that do not form a box end the structure; they are
			// reported, not interpreted.
			m.Notes = append(m.Notes, MP4TrailingRegion)
			break
		}
		past := boxSize > size-off
		if past {
			m.Findings = appendOnce(m.Findings, MP4BoxPastFile)
		}
		switch typ {
		case "ftyp":
			if i != 0 || past || boxSize-header < 8 || boxSize > 4096 {
				return m, ErrNotMP4
			}
			b, err := read(off+header, boxSize-header)
			if err != nil {
				return m, err
			}
			m.Brand = brand(b[0:4])
			for j := 8; j+4 <= len(b) && len(m.Compatible) < 32; j += 4 {
				m.Compatible = append(m.Compatible, brand(b[j:j+4]))
			}
		case "moov":
			if movie != nil {
				return m, errors.New("more than one MP4 movie box")
			}
			if past {
				return m, errors.New("MP4 movie box extends past the file")
			}
			if boxSize-header > maxMovie {
				return m, ErrBudget
			}
			if movie, err = read(off+header, boxSize-header); err != nil {
				return m, err
			}
		case "mdat":
			// A declared size past the file is kept, without overflowing.
			end := off + min(boxSize, math.MaxInt64-off)
			declaredMedia = append(declaredMedia, Region{"mdat", off + header, end})
			m.MediaData = append(m.MediaData, Region{"mdat", off + header, min(end, size)})
		case "moof", "mfra":
			fragmented = true
		}
		if past {
			break
		}
		off += boxSize
	}
	if movie == nil {
		return m, ErrNotMP4
	}
	tracks, err := p.movie(movie, &m)
	if err != nil {
		return m, err
	}
	if fragmented {
		m.Findings = appendOnce(m.Findings, MP4Fragmented)
	}
	m.Tracks = len(tracks)
	var audio []*mp4Track
	for _, t := range tracks {
		if t.handler == "soun" {
			audio = append(audio, t)
		}
	}
	if len(audio) == 0 {
		return m, ErrNotMP4
	}
	if len(audio) != 1 {
		m.Findings = appendOnce(m.Findings, MP4AudioTracksNotOne)
	}
	a := audio[0]
	m.SampleEntry, m.Codec, m.ObjectType, m.AudioObjectType = a.entry, a.codec, a.objectType, a.aot
	m.SampleRate, m.Channels, m.AverageBitrate, m.Timescale = a.rate, a.channels, a.bitrate, a.timescale
	m.Samples, m.Chunks, m.EditList, m.Language = a.count, len(a.offsets), a.editList, a.language
	if m.SampleRate == 0 {
		m.SampleRate = int(a.timescale)
	}
	m.MediaDurationMs = toMs(a.duration, a.timescale)
	if a.encrypted {
		m.Findings = appendOnce(m.Findings, MP4Encrypted)
	}
	if a.codec == "unrecognized" {
		m.Findings = appendOnce(m.Findings, MP4CodecUnrecognized)
	}
	// A chapter track is part of an audiobook; any other track is noted.
	for _, t := range tracks {
		if t.handler != "soun" && !isChapterTrack(t, a) {
			m.Notes = appendOnce(m.Notes, MP4OtherTracks)
		}
	}
	// Every track's samples must lie in the file and not overlap another's: a
	// cut-short download places samples past the end. Audio samples must also
	// lie inside media data. Some writers put cover art and chapter titles
	// after the media data, outside any box; such a sample of another track is
	// noted, since it does not change what plays and its bytes are still read.
	type extent struct {
		mp4Location
		audio bool
	}
	var extents []extent
	for _, t := range tracks {
		if t.failed {
			m.Findings = appendOnce(m.Findings, MP4TablesInconsistent)
			continue
		}
		chunks, _, err := t.layout(ctx, false)
		if err != nil {
			if errors.Is(err, ErrBudget) || ctx.Err() != nil {
				return m, err
			}
			m.Findings = appendOnce(m.Findings, MP4TablesInconsistent)
			continue
		}
		if t == a {
			var total uint64
			for _, e := range t.stts {
				total += uint64(e[0]) * uint64(e[1])
			}
			m.EncodedDurationMs = toMs(total, t.timescale)
			for _, c := range chunks {
				m.SampleBytes += c.size
			}
		}
		for _, c := range chunks {
			extents = append(extents, extent{c, t == a})
		}
	}
	// Samples that hold no bytes carry no audio, wherever they are placed.
	if m.Samples == 0 || m.SampleBytes == 0 {
		m.Findings = appendOnce(m.Findings, MP4NoAudioSamples)
	}
	if a.mixed {
		m.Findings = appendOnce(m.Findings, MP4DescriptionsDiffer)
	}
	sort.Slice(extents, func(i, j int) bool { return extents[i].offset < extents[j].offset })
	var covered int64
	var end int64 = -1
	for _, e := range extents {
		// An empty chunk is still placed: its offset must lie in the file
		// and in media data.
		if e.offset < 0 || e.offset > size || e.size > size-e.offset {
			m.Findings = appendOnce(m.Findings, MP4SamplesOutsideFile)
		}
		inMedia := within(declaredMedia, e.mp4Location)
		switch {
		case !inMedia && e.audio:
			m.Findings = appendOnce(m.Findings, MP4SamplesOutsideMediaData)
		case !inMedia:
			m.Notes = appendOnce(m.Notes, MP4OtherOutsideMedia)
		default:
			covered += e.size
		}
		if e.size > 0 && e.offset < end {
			m.Findings = appendOnce(m.Findings, MP4SamplesOverlap)
		}
		end = max(end, e.offset+e.size)
	}
	var media int64
	for _, d := range m.MediaData {
		media += d.End - d.Start
	}
	if media > covered && !slicesHas(m.Findings, MP4SamplesOverlap) {
		m.UnreferencedBytes = media - covered
		m.Notes = appendOnce(m.Notes, MP4UnreferencedMedia)
	}
	if d := m.MediaDurationMs - m.EncodedDurationMs; d > 1000 || d < -1000 {
		m.Notes = appendOnce(m.Notes, MP4DeclaredDiffers)
	}
	p.chapters(r, size, tracks, a, &m)
	return m, nil
}

func (p *mp4Parser) movie(data []byte, m *MP4) ([]*mp4Track, error) {
	var tracks []*mp4Track
	var nero []Chapter
	neroFound := false
	var movieScale uint32
	var movieDuration uint64
	err := p.walk(data, 1, func(typ string, body []byte) error {
		switch typ {
		case "mvhd":
			scale, duration, err := timing(body, 12, 16)
			if err != nil {
				return err
			}
			movieScale, movieDuration = scale, duration
		case "trak":
			if len(tracks) >= maxTracks {
				return ErrBudget
			}
			t := &mp4Track{}
			err := p.track(body, t)
			if err == nil {
				err = p.sampleEntries(t)
			}
			if err != nil {
				if !errors.Is(err, errMP4Malformed) && !errors.Is(err, errMP4Table) {
					return err
				}
				t.failed = true
			}
			tracks = append(tracks, t)
		case "mvex":
			m.Findings = appendOnce(m.Findings, MP4Fragmented)
		case "udta":
			return p.walk(body, 2, func(typ string, body []byte) error {
				switch typ {
				case "chpl":
					neroFound = true
					nero = neroChapters(body)
				case "meta":
					p.meta(body, &m.Metadata)
				}
				return nil
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	m.MovieDurationMs = toMs(movieDuration, movieScale)
	if neroFound {
		m.ChapterSource = "nero_chpl"
		m.Chapters = nero
		if nero == nil {
			m.Chapters = []Chapter{}
			m.Findings = appendOnce(m.Findings, MP4ChaptersUnreadable)
		}
		closeChapters(m.Chapters, m.MovieDurationMs)
	}
	return tracks, nil
}

var errMP4Table = errors.New("MP4 sample table outside limits")

// timing reads the timescale and duration of a version 0 or 1 full box, at
// offsets for version 0 (v1 adds eight bytes of 64-bit times before them).
func timing(body []byte, scale0, duration0 int) (uint32, uint64, error) {
	if len(body) < 4 {
		return 0, 0, errMP4Malformed
	}
	switch body[0] {
	case 0:
		if len(body) < duration0+4 {
			return 0, 0, errMP4Malformed
		}
		d := uint64(be32(body[duration0:]))
		if d == math.MaxUint32 {
			d = 0
		}
		return be32(body[scale0:]), d, nil
	case 1:
		if len(body) < duration0+8+8 {
			return 0, 0, errMP4Malformed
		}
		d := be64(body[duration0+8:])
		if d == math.MaxUint64 {
			d = 0
		}
		return be32(body[scale0+8:]), d, nil
	}
	return 0, 0, errMP4Malformed
}

func (p *mp4Parser) track(data []byte, t *mp4Track) error {
	return p.walk(data, 2, func(typ string, body []byte) error {
		switch typ {
		case "tkhd":
			if len(body) < 24 {
				return errMP4Malformed
			}
			if body[0] == 1 {
				t.id = be32(body[20:])
			} else {
				t.id = be32(body[12:])
			}
		case "tref":
			return p.walk(body, 3, func(typ string, body []byte) error {
				if typ == "chap" {
					for i := 0; i+4 <= len(body) && len(t.chapters) < 16; i += 4 {
						t.chapters = append(t.chapters, be32(body[i:]))
					}
				}
				return nil
			})
		case "edts":
			t.editList = true
		case "mdia":
			return p.walk(body, 3, func(typ string, body []byte) error {
				switch typ {
				case "mdhd":
					scale, duration, err := timing(body, 12, 16)
					if err != nil {
						return err
					}
					t.timescale, t.duration = scale, duration
					lang := 20
					if body[0] == 1 {
						lang = 32
					}
					if len(body) >= lang+2 {
						t.language = packedLanguage(be16(body[lang:]))
					}
				case "hdlr":
					if len(body) < 12 {
						return errMP4Malformed
					}
					t.handler = string(body[8:12])
				case "minf":
					return p.walk(body, 4, func(typ string, body []byte) error {
						if typ == "stbl" {
							return p.sampleTable(body, t)
						}
						return nil
					})
				}
				return nil
			})
		}
		return nil
	})
}

func (p *mp4Parser) sampleTable(data []byte, t *mp4Track) error {
	return p.walk(data, 5, func(typ string, body []byte) error {
		if len(body) < 8 && typ != "stsd" {
			if typ == "stts" || typ == "stsc" || typ == "stsz" || typ == "stco" || typ == "co64" || typ == "stz2" {
				return errMP4Table
			}
			return nil
		}
		switch typ {
		case "stsd":
			n := be32(body[4:])
			if len(body) < 8 || n == 0 || n > maxEntries || t.haveTables&16 != 0 {
				return errMP4Table
			}
			t.stsdVersion, t.entries, t.descriptions = body[0], int(n), body[8:]
			t.haveTables |= 16
		case "stts":
			n := int(be32(body[4:]))
			if n > maxSamples || len(body) < 8+8*n || t.haveTables&1 != 0 {
				return errMP4Table
			}
			t.stts = make([][2]uint32, n)
			for i := range n {
				t.stts[i] = [2]uint32{be32(body[8+8*i:]), be32(body[12+8*i:])}
			}
			t.haveTables |= 1
		case "stsc":
			n := int(be32(body[4:]))
			if n > maxChunks || len(body) < 8+12*n || t.haveTables&2 != 0 {
				return errMP4Table
			}
			t.stsc = make([][3]uint32, n)
			for i := range n {
				t.stsc[i] = [3]uint32{be32(body[8+12*i:]), be32(body[12+12*i:]), be32(body[16+12*i:])}
			}
			t.haveTables |= 2
		case "stsz":
			// One size table per track: a second would leave the first's
			// sizes beside the second's count.
			if len(body) < 12 || t.haveTables&4 != 0 {
				return errMP4Table
			}
			t.constSize = be32(body[4:])
			n := int(be32(body[8:]))
			if n > maxSamples {
				return ErrBudget
			}
			t.count = n
			if t.constSize == 0 {
				if len(body) < 12+4*n {
					return errMP4Table
				}
				t.sizes = make([]uint32, n)
				for i := range n {
					t.sizes[i] = be32(body[12+4*i:])
				}
			}
			t.haveTables |= 4
		case "stz2":
			if len(body) < 12 || t.haveTables&4 != 0 {
				return errMP4Table
			}
			field, n := int(body[7]), int(be32(body[8:]))
			if n > maxSamples {
				return ErrBudget
			}
			if (field != 4 && field != 8 && field != 16) || len(body) < 12+(n*field+7)/8 {
				return errMP4Table
			}
			t.count, t.sizes = n, make([]uint32, n)
			for i := range n {
				switch field {
				case 4:
					b := body[12+i/2]
					if i%2 == 0 {
						b >>= 4
					}
					t.sizes[i] = uint32(b & 0x0f)
				case 8:
					t.sizes[i] = uint32(body[12+i])
				case 16:
					t.sizes[i] = uint32(be16(body[12+2*i:]))
				}
			}
			t.haveTables |= 4
		case "stco", "co64":
			n := int(be32(body[4:]))
			width := 4
			if typ == "co64" {
				width = 8
			}
			if n > maxChunks || len(body) < 8+width*n || t.haveTables&8 != 0 {
				return errMP4Table
			}
			t.offsets = make([]int64, n)
			for i := range n {
				if width == 4 {
					t.offsets[i] = int64(be32(body[8+4*i:]))
				} else {
					o := be64(body[8+8*i:])
					if o > math.MaxInt64 {
						return errMP4Table
					}
					t.offsets[i] = int64(o)
				}
			}
			t.haveTables |= 8
		}
		return nil
	})
}

// sampleEntries reads every sample description of an audio track. The first
// names the codec, rate, and channels; any protected entry marks the track
// protected, and entries of different codecs are a finding.
func (p *mp4Parser) sampleEntries(t *mp4Track) error {
	if t.handler != "soun" {
		return nil
	}
	if t.descriptions == nil {
		t.codec = "unrecognized"
		return nil
	}
	data := t.descriptions
	for k := range t.entries {
		if len(data) < 8 {
			return errMP4Table
		}
		size := int(be32(data))
		if size < 8 || size > len(data) {
			return errMP4Table
		}
		e := &mp4Track{handler: t.handler, stsdVersion: t.stsdVersion}
		if err := p.sampleEntry(data[:size], e); err != nil {
			return err
		}
		if k == 0 {
			t.entry, t.codec, t.objectType, t.aot = e.entry, e.codec, e.objectType, e.aot
			t.rate, t.channels, t.bitrate = e.rate, e.channels, e.bitrate
		} else if e.codec != t.codec {
			t.mixed = true
		}
		t.encrypted = t.encrypted || e.encrypted
		data = data[size:]
	}
	if t.encrypted {
		t.codec = "encrypted"
	}
	return nil
}

// sampleEntry reads one sample description: the codec, its rate and
// channels, and whether the samples are protected.
func (p *mp4Parser) sampleEntry(data []byte, t *mp4Track) error {
	t.entry = string(data[4:8])
	body := data[8:]
	t.codec = "unrecognized"
	switch t.entry {
	case "enca", "drms", "aavd":
		t.encrypted = true
	}
	if len(body) < 28 {
		return errMP4Table
	}
	children := 28
	t.channels, t.rate = int(be16(body[16:])), int(be32(body[24:])>>16)
	switch be16(body[8:]) {
	case 0:
	case 1:
		// QuickTime sound description version 1 adds four fields. An ISO
		// AudioSampleEntryV1, in a version 1 sample description box, does not.
		if t.stsdVersion != 1 {
			children = 44
		}
	case 2:
		children = 64
		if len(body) < 64 {
			return errMP4Table
		}
		rate := math.Float64frombits(be64(body[32:]))
		if rate > 0 && rate < 1e7 {
			t.rate = int(rate)
		}
		t.channels = int(be32(body[40:]))
	default:
		return errMP4Table
	}
	if len(body) < children {
		return errMP4Table
	}
	var visit func(data []byte, depth int) error
	visit = func(data []byte, depth int) error {
		return p.walk(data, depth, func(typ string, body []byte) error {
			switch typ {
			case "esds":
				esds(body, t)
			case "sinf":
				t.encrypted = true
			case "wave":
				// QuickTime wraps the decoder configuration in a wave box.
				return visit(body, depth+1)
			}
			return nil
		})
	}
	if err := visit(body[children:], 7); err != nil {
		return err
	}
	switch {
	case t.encrypted:
		t.codec = "encrypted"
	case t.entry == "mp4a" && t.objectType == 0x40:
		t.codec = "aac"
	case t.entry == "mp4a" && (t.objectType == 0x6b || t.objectType == 0x69):
		t.codec = "mp3"
	case t.entry == "alac":
		t.codec = "alac"
	case t.entry == "Opus":
		t.codec = "opus"
	case t.entry == "fLaC":
		t.codec = "flac"
	case t.entry == "ac-3" || t.entry == "ec-3":
		t.codec = strings.ReplaceAll(t.entry, "-", "")
	}
	return nil
}

// esds reads the decoder configuration of an MPEG-4 elementary stream
// descriptor. Anything it cannot read leaves the codec unrecognized.
func esds(body []byte, t *mp4Track) {
	if len(body) < 4 {
		return
	}
	b := body[4:]
	descriptor := func(want byte) []byte {
		if len(b) < 2 || b[0] != want {
			return nil
		}
		n, i := 0, 1
		for ; i < 5 && i < len(b); i++ {
			n = n<<7 | int(b[i]&0x7f)
			if b[i]&0x80 == 0 {
				i++
				break
			}
		}
		if n > len(b)-i {
			return nil
		}
		return b[i : i+n]
	}
	es := descriptor(0x03)
	if len(es) < 3 {
		return
	}
	flags, skip := es[2], 3
	if flags&0x80 != 0 {
		skip += 2
	}
	if flags&0x40 != 0 {
		if len(es) <= skip {
			return
		}
		skip += 1 + int(es[skip])
	}
	if flags&0x20 != 0 {
		skip += 2
	}
	if len(es) < skip {
		return
	}
	b = es[skip:]
	dc := descriptor(0x04)
	if len(dc) < 13 {
		return
	}
	t.objectType = int(dc[0])
	t.bitrate = int(be32(dc[9:]))
	b = dc[13:]
	if asc := descriptor(0x05); len(asc) >= 1 {
		t.aot = int(asc[0] >> 3)
		if t.aot == 31 && len(asc) >= 2 {
			t.aot = 32 + int((asc[0]&0x07)<<3|asc[1]>>5)
		}
	}
}

// layout returns each chunk's byte extent and, when samples is set, each
// sample's. The tables must agree on the number of samples.
func (t *mp4Track) layout(ctx context.Context, samples bool) ([]mp4Location, []mp4Location, error) {
	if t.haveTables != 31 {
		return nil, nil, errMP4Table
	}
	var total uint64
	for _, e := range t.stts {
		total += uint64(e[0])
	}
	if total != uint64(t.count) {
		return nil, nil, errMP4Table
	}
	if t.count == 0 {
		return nil, nil, nil
	}
	if len(t.stsc) == 0 || t.stsc[0][0] != 1 || len(t.offsets) == 0 {
		return nil, nil, errMP4Table
	}
	for i := range t.stsc {
		// Each run names a sample description the track has.
		if t.stsc[i][2] < 1 || int(t.stsc[i][2]) > t.entries {
			return nil, nil, errMP4Table
		}
		if i > 0 && (t.stsc[i][0] <= t.stsc[i-1][0] || int64(t.stsc[i][0]) > int64(len(t.offsets))) {
			return nil, nil, errMP4Table
		}
	}
	chunks := make([]mp4Location, 0, len(t.offsets))
	var each []mp4Location
	if samples {
		each = make([]mp4Location, 0, t.count)
	}
	s, entry := 0, 0
	for c := range t.offsets {
		if c%65536 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
		}
		if entry+1 < len(t.stsc) && uint32(c+1) >= t.stsc[entry+1][0] {
			entry++
		}
		per := int(t.stsc[entry][1])
		if per == 0 || per > t.count-s {
			return nil, nil, errMP4Table
		}
		offset, size := t.offsets[c], int64(0)
		for k := range per {
			n := int64(t.constSize)
			if t.sizes != nil {
				n = int64(t.sizes[s+k])
			}
			if samples {
				each = append(each, mp4Location{offset + size, n})
			}
			size += n
		}
		chunks = append(chunks, mp4Location{offset, size})
		s += per
	}
	if s != t.count {
		return nil, nil, errMP4Table
	}
	return chunks, each, nil
}

func isChapterTrack(t, audio *mp4Track) bool {
	for _, id := range audio.chapters {
		if id == t.id && t != audio {
			return true
		}
	}
	return false
}

// chapters reads a QuickTime chapter track that the audio track references:
// each text sample names the chapter that starts at its time. When the file
// also has Nero chapters, the chapter track is used and a difference noted.
func (p *mp4Parser) chapters(r interface {
	ReadAt([]byte, int64) (int, error)
}, size int64, tracks []*mp4Track, audio *mp4Track, m *MP4) {
	var track *mp4Track
	for _, t := range tracks {
		if isChapterTrack(t, audio) && (t.handler == "text" || t.handler == "sbtl") {
			track = t
			break
		}
	}
	if track == nil {
		return
	}
	nero := m.Chapters
	// The count is checked before any sample is placed, so a small file
	// cannot make the chapter list allocate millions of entries.
	if track.count > maxChapters || track.failed || track.timescale == 0 {
		m.Findings = appendOnce(m.Findings, MP4ChaptersUnreadable)
		return
	}
	_, samples, err := track.layout(p.ctx, true)
	if err != nil {
		m.Findings = appendOnce(m.Findings, MP4ChaptersUnreadable)
		return
	}
	chapters := make([]Chapter, 0, len(samples))
	var at uint64
	i := 0
	for _, e := range track.stts {
		for range e[0] {
			s := samples[i]
			if s.size < 2 || s.size > maxChapterText || s.offset < 0 || s.offset > size-s.size {
				m.Findings = appendOnce(m.Findings, MP4ChaptersUnreadable)
				return
			}
			b := make([]byte, s.size)
			if n, _ := r.ReadAt(b, s.offset); int64(n) != s.size {
				m.Findings = appendOnce(m.Findings, MP4ChaptersUnreadable)
				return
			}
			n := int(be16(b))
			if n > len(b)-2 {
				m.Findings = appendOnce(m.Findings, MP4ChaptersUnreadable)
				return
			}
			title := b[2 : 2+n]
			enc := byte(3)
			if len(title) >= 2 && title[0] == 0xfe && title[1] == 0xff {
				enc, title = 2, title[2:]
			}
			chapters = append(chapters, Chapter{ID: fmt.Sprintf("chapter%d", i+1), Title: text(append([]byte{enc}, title...)), StartMs: toMs(at, track.timescale)})
			at += uint64(e[1])
			i++
		}
	}
	closeChapters(chapters, m.MovieDurationMs)
	if len(chapters) == 0 && len(nero) > 0 {
		// An empty chapter track, as in a fragmented file whose samples are
		// in fragments, does not replace a readable Nero list.
		return
	}
	if m.ChapterSource == "nero_chpl" && !sameChapters(nero, chapters) {
		m.Notes = appendOnce(m.Notes, MP4ChapterSourcesDiffer)
	}
	m.ChapterSource, m.Chapters = "quicktime_chapter_track", chapters
}

// neroChapters reads a Nero chapter list: start times in 100 ns units with a
// title each. It returns nil when the list does not fit its box.
func neroChapters(body []byte) []Chapter {
	if len(body) < 5 {
		return nil
	}
	i := 4
	if body[0] == 1 {
		i += 4
	}
	if len(body) <= i {
		return nil
	}
	n := int(body[i])
	i++
	chapters := make([]Chapter, 0, n)
	for k := range n {
		if len(body) < i+9 {
			return nil
		}
		start := be64(body[i:])
		l := int(body[i+8])
		i += 9
		if len(body) < i+l || start > math.MaxInt64/2 {
			return nil
		}
		chapters = append(chapters, Chapter{ID: fmt.Sprintf("chapter%d", k+1), Title: text(append([]byte{3}, body[i:i+l]...)), StartMs: int64(start / 10_000)})
		i += l
	}
	for k := 1; k < len(chapters); k++ {
		if chapters[k].StartMs < chapters[k-1].StartMs {
			return nil
		}
	}
	return chapters
}

// closeChapters ends each chapter where the next begins and the last at the
// movie's declared end, when that is later.
func closeChapters(c []Chapter, end int64) {
	for i := range c {
		c[i].EndMs = c[i].StartMs
		if i+1 < len(c) {
			c[i].EndMs = c[i+1].StartMs
		} else if end > c[i].StartMs {
			c[i].EndMs = end
		}
	}
}

func sameChapters(a, b []Chapter) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if d := a[i].StartMs - b[i].StartMs; d > 1000 || d < -1000 {
			return false
		}
	}
	return true
}

// meta reads iTunes-style item text from a metadata box.
func (p *mp4Parser) meta(body []byte, md *MP4Metadata) {
	// A full box in MP4; QuickTime writers may omit the version and flags.
	if len(body) >= 4 && be32(body) == 0 {
		body = body[4:]
	}
	_ = p.walk(body, 3, func(typ string, body []byte) error {
		if typ != "ilst" {
			return nil
		}
		return p.walk(body, 4, func(item string, body []byte) error {
			var field *string
			switch item {
			case "\xa9nam":
				field = &md.Title
			case "\xa9ART":
				field = &md.Artist
			case "\xa9alb":
				field = &md.Album
			case "\xa9nrt":
				field = &md.Narrator
			case "\xa9wrt":
				field = &md.Composer
			case "\xa9day":
				field = &md.Year
			default:
				return nil
			}
			return p.walk(body, 5, func(typ string, body []byte) error {
				// A data box of UTF-8 text: type 1 after a zero byte.
				if typ == "data" && len(body) >= 8 && be32(body) == 1 && *field == "" {
					*field = text(append([]byte{3}, body[8:]...))
				}
				return nil
			})
		})
	})
}

func packedLanguage(v uint16) string {
	if v == 0 || v == 0x7fff || v < 0x400 {
		return ""
	}
	b := []byte{byte(v>>10&0x1f) + 0x60, byte(v>>5&0x1f) + 0x60, byte(v&0x1f) + 0x60}
	for _, c := range b {
		if c < 'a' || c > 'z' {
			return ""
		}
	}
	if s := string(b); s != "und" {
		return s
	}
	return ""
}

func toMs(units uint64, scale uint32) int64 {
	if scale == 0 {
		return 0
	}
	hi, lo := bits.Mul64(units, 1000)
	if hi >= uint64(scale) {
		return math.MaxInt64
	}
	q, _ := bits.Div64(hi, lo, uint64(scale))
	if q > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(q)
}

func within(regions []Region, e mp4Location) bool {
	for _, r := range regions {
		if e.offset >= r.Start && e.offset <= r.End && e.size <= r.End-e.offset {
			return true
		}
	}
	return false
}

func printableType(t string) bool {
	for i := 0; i < len(t); i++ {
		if c := t[i]; (c < 0x20 || c > 0x7e) && c != 0xa9 {
			return false
		}
	}
	return true
}

// brand returns a four-character brand without its padding, which writers
// fill with spaces or NUL bytes.
func brand(b []byte) string {
	s := strings.TrimRight(string(b), " \x00")
	if !printableType(s) {
		return ""
	}
	return s
}

func appendOnce(list []string, s string) []string {
	if slicesHas(list, s) {
		return list
	}
	return append(list, s)
}

func slicesHas(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
