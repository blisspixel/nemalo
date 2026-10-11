// Package audio indexes MPEG audio (MP3) frames without decoding them. It
// reports encoded timing measured from frame headers separately from timing
// that tags declare, and computes frame-aligned byte ranges a decoder can use.
// Input is untrusted; every count, offset, and string is bounded.
package audio

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Extractor identifies these indexing rules. Changing what is indexed, how
// frames are accepted, or how ranges are cut requires a new identifier.
const Extractor = "nemalo.mpeg.frames/1"

const (
	maxFrames       = 2_000_000
	maxGaps         = 1024
	maxResync       = 64 << 10
	maxFirstSearch  = 1 << 20
	maxChapters     = 1024
	maxText         = 2000
	maxPrerollFrame = 64
)

// ErrNotMPEG means no confirmed MPEG audio frame sequence was found.
var ErrNotMPEG = errors.New("no MPEG audio frame sequence found")

// ErrBudget means the input exceeds the indexing bounds.
var ErrBudget = errors.New("MPEG audio indexing budget exceeded")

// Declared holds timing and encoder facts stated by tags or info frames. None
// of it is verified by decoding.
type Declared struct {
	InfoFrame      string `json:"info_frame,omitempty"`
	Frames         *int64 `json:"frames,omitempty"`
	Bytes          *int64 `json:"bytes,omitempty"`
	Encoder        string `json:"encoder,omitempty"`
	EncoderDelay   *int   `json:"encoder_delay_samples,omitempty"`
	EncoderPadding *int   `json:"encoder_padding_samples,omitempty"`
	LengthMs       *int64 `json:"id3_length_ms,omitempty"`
}

// Metadata is ID3 text as declared. Missing values stay empty, not guessed.
type Metadata struct {
	ID3Version string `json:"id3_version,omitempty"`
	Title      string `json:"title,omitempty"`
	Artist     string `json:"artist,omitempty"`
	Album      string `json:"album,omitempty"`
	Track      string `json:"track,omitempty"`
	Language   string `json:"language,omitempty"`
	Skipped    string `json:"skipped,omitempty"`
}

// Chapter is an ID3 CHAP declaration in milliseconds, as stated.
type Chapter struct {
	ID      string `json:"id"`
	Title   string `json:"title,omitempty"`
	StartMs int64  `json:"start_ms"`
	EndMs   int64  `json:"end_ms"`
}

// Region is a half-open byte range [Start, End) of the file.
type Region struct {
	Kind  string `json:"kind"`
	Start int64  `json:"start"`
	End   int64  `json:"end"`
}

// Track is the frame index summary of one MP3 file. Sample counts are encoded
// samples per channel: frames times samples per frame. Presented samples
// subtract the encoder's declared delay and padding and are only as reliable as
// that declaration; no output was decoded.
type Track struct {
	Format            string    `json:"format"`
	SampleRate        int       `json:"sample_rate"`
	Channels          int       `json:"channels"`
	ChannelMode       string    `json:"channel_mode"`
	SamplesPerFrame   int       `json:"samples_per_frame"`
	Bitrate           string    `json:"bitrate"`
	AudioStart        int64     `json:"audio_start"`
	AudioEnd          int64     `json:"audio_end"`
	Frames            int       `json:"frames"`
	EncodedSamples    int64     `json:"encoded_samples"`
	EncodedDurationMs int64     `json:"encoded_duration_ms"`
	PresentedSamples  *int64    `json:"presented_samples,omitempty"`
	Declared          Declared  `json:"declared"`
	Metadata          Metadata  `json:"metadata"`
	Chapters          []Chapter `json:"chapters"`
	Tags              []Region  `json:"tags"`
	Gaps              []Region  `json:"gaps"`
	Findings          []string  `json:"findings"`
	Decoded           string    `json:"decoded"`

	// stream holds the parameters every indexed frame shares; frames holds the
	// small per-frame facts, so the index stays near 16 bytes per frame.
	stream header
	frames []frameRec
}

type frameRec struct {
	offset    int64
	length    uint16
	bitrate   uint16
	protected bool
}

func (t Track) frameLen(i int) int { return int(t.frames[i].length) }

// dataStart is the offset of frame i's main data after header, CRC, and side
// information.
func (t Track) dataStart(i int) int {
	n := 4 + t.stream.sideInfo()
	if t.frames[i].protected {
		n += 2
	}
	return n
}

// Offsets returns the byte offset of each indexed audio frame.
func (t Track) Offsets() []int64 {
	out := make([]int64, len(t.frames))
	for i, f := range t.frames {
		out[i] = f.offset
	}
	return out
}

type header struct {
	version, layer        int // version 1, 2, or 25 (MPEG-2.5)
	protected             bool
	bitrate, rate, length int
	padding, channels     int
	mode, samples         int
}

var bitrates = map[[2]int][15]int{
	{1, 1}:  {0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448},
	{1, 2}:  {0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384},
	{1, 3}:  {0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320},
	{2, 1}:  {0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256},
	{2, 2}:  {0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160},
	{2, 3}:  {0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160},
	{25, 1}: {0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256},
	{25, 2}: {0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160},
	{25, 3}: {0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160},
}

var sampleRates = map[int][3]int{1: {44100, 48000, 32000}, 2: {22050, 24000, 16000}, 25: {11025, 12000, 8000}}

// parseHeader decodes a 4-byte MPEG audio frame header. Free-format bitrates,
// reserved fields, and the reserved emphasis value are rejected.
func parseHeader(b []byte) (header, bool) {
	if len(b) < 4 || b[0] != 0xFF || b[1]&0xE0 != 0xE0 {
		return header{}, false
	}
	h := header{}
	switch (b[1] >> 3) & 3 {
	case 0:
		h.version = 25
	case 2:
		h.version = 2
	case 3:
		h.version = 1
	default:
		return header{}, false
	}
	switch (b[1] >> 1) & 3 {
	case 1:
		h.layer = 3
	case 2:
		h.layer = 2
	case 3:
		h.layer = 1
	default:
		return header{}, false
	}
	h.protected = b[1]&1 == 0
	bi, si := int(b[2]>>4), int((b[2]>>2)&3)
	if bi == 0 || bi == 15 || si == 3 || b[3]&3 == 2 {
		return header{}, false
	}
	h.bitrate = bitrates[[2]int{h.version, h.layer}][bi]
	h.rate = sampleRates[h.version][si]
	h.padding = int((b[2] >> 1) & 1)
	h.mode = int(b[3] >> 6)
	h.channels = 2
	if h.mode == 3 {
		h.channels = 1
	}
	switch {
	case h.layer == 1:
		h.samples = 384
		h.length = (12*h.bitrate*1000/h.rate + h.padding) * 4
	case h.layer == 2 || h.version == 1:
		h.samples = 1152
		h.length = 144*h.bitrate*1000/h.rate + h.padding
	default:
		h.samples = 576
		h.length = 72*h.bitrate*1000/h.rate + h.padding
	}
	return h, h.length > 4
}

// sameStream reports whether two headers belong to one elementary stream.
// Bitrate and padding may vary (VBR); format, rate, and channels may not.
func sameStream(a, b header) bool {
	return a.version == b.version && a.layer == b.layer && a.rate == b.rate && a.channels == b.channels
}

// sideInfo is the Layer III side information length after header and CRC.
func (h header) sideInfo() int {
	if h.layer != 3 {
		return 0
	}
	if h.version == 1 {
		if h.channels == 1 {
			return 17
		}
		return 32
	}
	if h.channels == 1 {
		return 9
	}
	return 17
}

func (h header) dataStart() int {
	n := 4 + h.sideInfo()
	if h.protected {
		n += 2
	}
	return n
}

func (h header) format() string {
	v := map[int]string{1: "mpeg1", 2: "mpeg2", 25: "mpeg2_5"}[h.version]
	return fmt.Sprintf("%s_layer%d", v, h.layer)
}

func modeName(mode int) string {
	return [...]string{"stereo", "joint_stereo", "dual_channel", "mono"}[mode]
}

// Probe indexes every frame of an MP3 file held in memory. It never decodes
// audio. Unparseable stretches between frames are reported as gaps rather than
// skipped silently, and tag regions are reported with their byte ranges.
func Probe(ctx context.Context, data []byte) (Track, error) {
	t := Track{Chapters: []Chapter{}, Tags: []Region{}, Gaps: []Region{}, Findings: []string{}, Decoded: "not_decoded"}
	start := 0
	if n, ok := id3v2Size(data); ok {
		if n > len(data) {
			return t, errors.New("ID3v2 tag extends beyond the file")
		}
		t.Tags = append(t.Tags, Region{"id3v2", 0, int64(n)})
		t.Metadata, t.Chapters, t.Declared.LengthMs = parseID3v2(data[:n])
		start = n
	}
	end := trailingTags(data, start, &t)
	first := -1
	for o := start; o+4 <= end && o-start <= maxFirstSearch; o++ {
		if confirmed(data, o, end) {
			first = o
			break
		}
	}
	if first < 0 {
		return t, ErrNotMPEG
	}
	if first > start {
		t.Gaps = append(t.Gaps, Region{"unsynchronized_bytes", int64(start), int64(first)})
	}
	infoChecked := false
	for o := first; o < end; {
		if len(t.frames)%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return t, err
			}
		}
		h, ok := parseHeader(data[o:min(o+4, end)])
		if ok && len(t.frames) > 0 && !sameStream(t.stream, h) {
			ok = false
		}
		if ok && o+h.length <= end {
			// Only the first frame may be a Xing, Info, or VBRI frame. It carries
			// metadata rather than audio, so it is reported and not indexed.
			if !infoChecked {
				infoChecked = true
				if parseInfoFrame(data[o:o+h.length], h, &t.Declared) {
					t.Tags = append(t.Tags, Region{"info_frame", int64(o), int64(o + h.length)})
					o += h.length
					continue
				}
			}
			if len(t.frames) >= maxFrames {
				return t, ErrBudget
			}
			if len(t.frames) == 0 {
				t.stream = h
			}
			// Frame lengths are at most 1,729 bytes and bitrates at most 448 kbps.
			t.frames = append(t.frames, frameRec{offset: int64(o), length: uint16(h.length), bitrate: uint16(h.bitrate), protected: h.protected})
			o += h.length
			continue
		}
		next := -1
		for p := o + 1; p+4 <= end && p-o <= maxResync; p++ {
			if c, ok := parseHeader(data[p:]); ok && (len(t.frames) == 0 || sameStream(t.stream, c)) && confirmed(data, p, end) {
				next = p
				break
			}
		}
		kind := "unsynchronized_bytes"
		if ok && o+h.length > end {
			kind = "truncated_frame"
		}
		if next < 0 {
			t.Gaps = append(t.Gaps, Region{kind, int64(o), int64(end)})
			break
		}
		if len(t.Gaps) >= maxGaps {
			return t, ErrBudget
		}
		t.Gaps = append(t.Gaps, Region{kind, int64(o), int64(next)})
		o = next
	}
	if len(t.frames) == 0 {
		return t, ErrNotMPEG
	}
	h := t.stream
	t.Format, t.SampleRate, t.Channels, t.ChannelMode, t.SamplesPerFrame = h.format(), h.rate, h.channels, modeName(h.mode), h.samples
	t.Frames = len(t.frames)
	t.AudioStart = t.frames[0].offset
	last := len(t.frames) - 1
	t.AudioEnd = t.frames[last].offset + int64(t.frameLen(last))
	t.EncodedSamples = int64(t.Frames) * int64(h.samples)
	t.EncodedDurationMs = t.EncodedSamples * 1000 / int64(h.rate)
	t.Bitrate = fmt.Sprintf("constant_%d_kbps", h.bitrate)
	for _, x := range t.frames {
		if int(x.bitrate) != h.bitrate {
			t.Bitrate = "variable"
			break
		}
	}
	if d, p := t.Declared.EncoderDelay, t.Declared.EncoderPadding; d != nil && p != nil && int64(*d+*p) < t.EncodedSamples {
		presented := t.EncodedSamples - int64(*d) - int64(*p)
		t.PresentedSamples = &presented
	}
	if f := t.Declared.Frames; f != nil && *f != int64(t.Frames) {
		t.Findings = append(t.Findings, fmt.Sprintf("info frame declares %d frames; %d were indexed", *f, t.Frames))
	}
	if l := t.Declared.LengthMs; l != nil {
		if diff := *l - t.EncodedDurationMs; diff > 1000 || diff < -1000 {
			t.Findings = append(t.Findings, fmt.Sprintf("ID3 length declares %d ms; indexed frames encode %d ms", *l, t.EncodedDurationMs))
		}
	}
	// A region after the last indexed frame is a trailer; one before an indexed
	// frame shifts every later sample position if it held audio.
	inside := 0
	for i := range t.Gaps {
		if t.Gaps[i].Start >= t.AudioEnd {
			t.Gaps[i].Kind = "trailing_" + t.Gaps[i].Kind
		} else if t.Gaps[i].End > t.AudioStart {
			inside++
		}
	}
	if inside > 0 {
		t.Findings = append(t.Findings, fmt.Sprintf("%d unindexed byte region(s) between frames; later sample positions assume they hold no audio", inside))
	}
	if trailing := len(t.Gaps) - inside; trailing > 0 {
		t.Findings = append(t.Findings, fmt.Sprintf("%d unindexed region(s) outside the indexed frames, such as a nonstandard tag", trailing))
	}
	return t, nil
}

// confirmed requires a valid header at o whose successor is also a valid header
// of the same stream, or whose frame ends exactly at end. One header alone is
// not evidence: four random bytes match the sync pattern too often.
func confirmed(data []byte, o, end int) bool {
	h, ok := parseHeader(data[o:min(o+4, end)])
	if !ok || o+h.length > end {
		return false
	}
	n := o + h.length
	if n == end {
		return true
	}
	next, ok := parseHeader(data[n:min(n+4, end)])
	return ok && sameStream(h, next)
}

// parseInfoFrame recognizes a Xing/Info or VBRI frame and records what it
// declares, including the LAME or FFmpeg encoder delay and padding fields.
func parseInfoFrame(frame []byte, h header, d *Declared) bool {
	if x := h.dataStart(); h.layer == 3 && x+8 <= len(frame) {
		tag := string(frame[x : x+4])
		if tag == "Xing" || tag == "Info" {
			d.InfoFrame = tag
			flags := binary.BigEndian.Uint32(frame[x+4:])
			p := x + 8
			if flags&1 != 0 && p+4 <= len(frame) {
				n := int64(binary.BigEndian.Uint32(frame[p:]))
				d.Frames = &n
				p += 4
			}
			if flags&2 != 0 && p+4 <= len(frame) {
				n := int64(binary.BigEndian.Uint32(frame[p:]))
				d.Bytes = &n
			}
			// The LAME extension follows only when every Xing field is present.
			if flags&0xF == 0xF && x+144 <= len(frame) {
				enc := frame[x+120 : x+129]
				if printable(enc) && (strings.HasPrefix(string(enc), "LAME") || strings.HasPrefix(string(enc), "Lavc") || strings.HasPrefix(string(enc), "Lavf")) {
					d.Encoder = strings.TrimRight(string(enc), " \x00")
					delay := int(frame[x+141])<<4 | int(frame[x+142])>>4
					padding := int(frame[x+142]&0x0F)<<8 | int(frame[x+143])
					d.EncoderDelay, d.EncoderPadding = &delay, &padding
				}
			}
			return true
		}
	}
	if 36+18 <= len(frame) && string(frame[36:40]) == "VBRI" {
		d.InfoFrame = "VBRI"
		n := int64(binary.BigEndian.Uint32(frame[36+10:]))
		frames := int64(binary.BigEndian.Uint32(frame[36+14:]))
		d.Bytes, d.Frames = &n, &frames
		return true
	}
	return false
}

func printable(b []byte) bool {
	for _, c := range b {
		if (c < 0x20 || c > 0x7E) && c != 0 {
			return false
		}
	}
	return true
}

// trailingTags excludes an ID3v1 tag and an APEv2 tag (with footer) from the
// end of the audio region and reports them.
func trailingTags(data []byte, start int, t *Track) int {
	end := len(data)
	if end-start >= 128 && string(data[end-128:end-125]) == "TAG" {
		t.Tags = append(t.Tags, Region{"id3v1", int64(end - 128), int64(end)})
		end -= 128
	}
	if end-start >= 32 && string(data[end-32:end-24]) == "APETAGEX" {
		size := int(binary.LittleEndian.Uint32(data[end-20:]))
		flags := binary.LittleEndian.Uint32(data[end-12:])
		total := size
		if flags&(1<<31) != 0 {
			total += 32
		}
		if size >= 32 && total <= end-start {
			t.Tags = append(t.Tags, Region{"apev2", int64(end - total), int64(end)})
			end -= total
		}
	}
	return end
}

func syncsafe(b []byte) (int, bool) {
	if len(b) < 4 || b[0]|b[1]|b[2]|b[3] >= 0x80 {
		return 0, false
	}
	return int(b[0])<<21 | int(b[1])<<14 | int(b[2])<<7 | int(b[3]), true
}

// id3v2Size returns the total size of a leading ID3v2 tag, including a footer.
func id3v2Size(data []byte) (int, bool) {
	if len(data) < 10 || string(data[:3]) != "ID3" || data[3] == 0xFF || data[4] == 0xFF {
		return 0, false
	}
	size, ok := syncsafe(data[6:10])
	if !ok {
		return 0, false
	}
	size += 10
	if data[5]&0x10 != 0 {
		size += 10
	}
	return size, true
}

// parseID3v2 reads declared text, TLEN, and CHAP frames from ID3v2.3 or v2.4.
// Unsynchronised, compressed, or encrypted content is skipped and reported,
// never guessed at.
func parseID3v2(tag []byte) (Metadata, []Chapter, *int64) {
	m := Metadata{ID3Version: fmt.Sprintf("2.%d", tag[3])}
	chapters := []Chapter{}
	var length *int64
	major, flags := tag[3], tag[5]
	if major != 3 && major != 4 {
		m.Skipped = "unsupported_id3_version"
		return m, chapters, length
	}
	if flags&0x80 != 0 {
		m.Skipped = "unsynchronised_tag_not_read"
		return m, chapters, length
	}
	body := tag[10:]
	if flags&0x10 != 0 && len(body) >= 10 {
		body = body[:len(body)-10]
	}
	if flags&0x40 != 0 {
		if len(body) < 4 {
			m.Skipped = "invalid_extended_header"
			return m, chapters, length
		}
		n := int(binary.BigEndian.Uint32(body))
		if major == 3 {
			n += 4
		} else if s, ok := syncsafe(body); ok {
			n = s
		} else {
			n = -1
		}
		if n < 4 || n > len(body) {
			m.Skipped = "invalid_extended_header"
			return m, chapters, length
		}
		body = body[n:]
	}
	frames(body, major, func(id string, data []byte) {
		switch id {
		case "TIT2":
			m.Title = text(data)
		case "TPE1":
			m.Artist = text(data)
		case "TALB":
			m.Album = text(data)
		case "TRCK":
			m.Track = text(data)
		case "TLAN":
			m.Language = text(data)
		case "TLEN":
			if v, err := strconv.ParseInt(strings.TrimSpace(text(data)), 10, 64); err == nil && v > 0 {
				length = &v
			}
		case "CHAP":
			if c, ok := chapter(data, major); ok && len(chapters) < maxChapters {
				chapters = append(chapters, c)
			}
		}
	}, &m)
	return m, chapters, length
}

// frames walks ID3v2 frames, stopping at padding or the first malformed frame.
func frames(body []byte, major byte, visit func(string, []byte), m *Metadata) {
	for p := 0; p+10 <= len(body); {
		id := string(body[p : p+4])
		if body[p] == 0 {
			return
		}
		for _, c := range id {
			if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
				m.Skipped = "malformed_frame"
				return
			}
		}
		size := int(binary.BigEndian.Uint32(body[p+4:]))
		if major == 4 {
			s, ok := syncsafe(body[p+4 : p+8])
			if !ok {
				m.Skipped = "malformed_frame"
				return
			}
			size = s
		}
		fl := binary.BigEndian.Uint16(body[p+8:])
		if size < 0 || p+10+size > len(body) {
			m.Skipped = "malformed_frame"
			return
		}
		data := body[p+10 : p+10+size]
		// Compression, encryption, and grouping (v2.3) or grouping,
		// compression, encryption, unsynchronisation, and data length (v2.4)
		// change the frame body layout; such frames are named, not read.
		unsupported := (major == 3 && fl&0x00E0 != 0) || (major == 4 && fl&0x004F != 0)
		if unsupported {
			m.Skipped = "encoded_frame_not_read"
		} else {
			visit(id, data)
		}
		p += 10 + size
	}
}

func chapter(data []byte, major byte) (Chapter, bool) {
	i := 0
	for i < len(data) && data[i] != 0 && i < 256 {
		i++
	}
	// The element ID must end in a NUL within its 256-byte bound.
	if i >= len(data) || data[i] != 0 || i == 0 || i+17 > len(data) {
		return Chapter{}, false
	}
	c := Chapter{ID: string(data[:i])}
	if !utf8.ValidString(c.ID) {
		return Chapter{}, false
	}
	p := i + 1
	c.StartMs, c.EndMs = int64(binary.BigEndian.Uint32(data[p:])), int64(binary.BigEndian.Uint32(data[p+4:]))
	if c.EndMs < c.StartMs {
		return Chapter{}, false
	}
	var meta Metadata
	frames(data[p+16:], major, func(id string, sub []byte) {
		if id == "TIT2" {
			c.Title = text(sub)
		}
	}, &meta)
	return c, true
}

// text decodes an ID3 text frame. Only the first of several values is kept,
// and the result is bounded. Invalid encodings return an empty string.
func text(data []byte) string {
	if len(data) < 1 {
		return ""
	}
	enc, b := data[0], data[1:]
	// Decode only what can survive truncation: at most 4 input bytes per
	// output byte kept. A frame may be hundreds of MiB; decoding all of it
	// first would allocate several times its size.
	if len(b) > 4*maxText {
		b = b[:4*maxText]
		if enc == 3 {
			// Drop a UTF-8 sequence split by the cut, at most three bytes.
			for i := 0; i < 3 && len(b) > 0 && !utf8.Valid(b); i++ {
				b = b[:len(b)-1]
			}
		}
	}
	var s string
	switch enc {
	case 0:
		runes := make([]rune, 0, len(b))
		for _, c := range b {
			if c == 0 {
				break
			}
			runes = append(runes, rune(c))
		}
		s = string(runes)
	case 1, 2:
		bigEndian := enc == 2
		if enc == 1 && len(b) >= 2 {
			switch {
			case b[0] == 0xFE && b[1] == 0xFF:
				bigEndian, b = true, b[2:]
			case b[0] == 0xFF && b[1] == 0xFE:
				b = b[2:]
			default:
				return ""
			}
		}
		units := []uint16{}
		for i := 0; i+1 < len(b); i += 2 {
			u := binary.LittleEndian.Uint16(b[i:])
			if bigEndian {
				u = binary.BigEndian.Uint16(b[i:])
			}
			if u == 0 {
				break
			}
			units = append(units, u)
		}
		s = string(utf16.Decode(units))
	case 3:
		if i := strings.IndexByte(string(b), 0); i >= 0 {
			b = b[:i]
		}
		if !utf8.Valid(b) {
			return ""
		}
		s = string(b)
	default:
		return ""
	}
	if len(s) > maxText {
		end := maxText
		for end > 0 && !utf8.RuneStart(s[end]) {
			end--
		}
		s = s[:end]
	}
	return s
}
