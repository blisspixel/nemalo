package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// MP4PacketExtractor identifies original packet byte and media-decode-tick
// selections. It establishes neither presentation timing nor decoder history.
const MP4PacketExtractor = "nemalo.mp4.packets/1"

const maxMP4RangePackets = 128
const maxMP4RangeBytes = 64 << 10
const maxMP4PacketSourceBytes = 2 << 30

var ErrMP4PacketUnsupported = errors.New("MP4 packet selection is unsupported")
var ErrMP4PacketRangeTooSmall = errors.New("byte allowance cannot hold the first MP4 packet")

// SampleDescription copies the one complete original sample entry and reports
// its stsd version. It does not interpret or verify decoder initialization.
func (index MP4PacketIndex) SampleDescription() ([]byte, byte, error) {
	if index.track == nil || index.track.entries != 1 || len(index.track.descriptions) < 8 {
		return nil, 0, ErrMP4PacketUnsupported
	}
	b := index.track.descriptions
	if len(b) > 16<<10 {
		return nil, 0, ErrBudget
	}
	if int64(be32(b)) != int64(len(b)) {
		return nil, 0, ErrMP4PacketUnsupported
	}
	return append([]byte(nil), b...), index.track.stsdVersion, nil
}

// MP4PacketIndex retains the existing parser's checked tables and open reader.
// It owns no file. The caller keeps that reader alive, binds it to an exact
// source identity and rechecks the source before publishing any returned bytes.
// Summary is informational; editing it cannot change the private index.
type MP4PacketIndex struct {
	Summary MP4
	source  io.ReaderAt
	track   *mp4Track
	size    int64
}

// MP4Packet describes one original encoded packet. Decode ticks are in the
// track's declared timescale, not PCM samples or edited presentation time.
type MP4Packet struct {
	Index       int   `json:"index"`
	ByteStart   int64 `json:"byte_start"`
	ByteEnd     int64 `json:"byte_end"`
	DecodeStart int64 `json:"decode_start"`
	DecodeEnd   int64 `json:"decode_end"`
}

// MP4PacketRange contains concatenated original packets, with their individual
// source positions. Data is not a playable container or a decoded waveform.
// Stop identifies a bound or unsupported next boundary; NextPacket never skips it.
type MP4PacketRange struct {
	Extractor     string      `json:"extractor"`
	Timescale     uint32      `json:"timescale"`
	Timeline      string      `json:"timeline"`
	FirstPacket   int         `json:"first_packet"`
	Packets       []MP4Packet `json:"packets"`
	Data          []byte      `json:"data"`
	NextPacket    int         `json:"next_packet"`
	EndOfTrack    bool        `json:"end_of_track"`
	Stop          string      `json:"stop"`
	RequiredBytes int64       `json:"required_bytes,omitempty"`
}

// IndexMP4 uses the same parser and placement checks as ProbeMP4 without reading
// audio bodies or allocating a second per-packet index. Any finding or missing
// media timescale refuses packet delivery while retaining its summary.
func IndexMP4(ctx context.Context, r io.ReaderAt, size int64) (MP4PacketIndex, error) {
	var index MP4PacketIndex
	if err := ctx.Err(); err != nil {
		return index, err
	}
	if r == nil || size <= 0 || size > maxMP4PacketSourceBytes {
		return index, ErrMP4PacketUnsupported
	}
	m, err := probeMP4Tables(ctx, r, size, &index)
	index.Summary = m
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		index.source, index.track = nil, nil
		return index, err
	}
	if len(m.Findings) != 0 || m.Timescale == 0 || index.track == nil || index.track.entries != 1 {
		index.source, index.track = nil, nil
		return index, ErrMP4PacketUnsupported
	}
	return index, nil
}

// ReadRange selects whole packets starting at first. It reads at most maxBytes
// of audio and returns at most 128 packets. Cancellation or a body-read failure
// returns no deliverable payload. The reader must remain the index's open source.
func (index MP4PacketIndex) ReadRange(ctx context.Context, first, maxBytes int) (MP4PacketRange, error) {
	out := MP4PacketRange{Extractor: MP4PacketExtractor, Timeline: "media_decode_ticks", FirstPacket: first, NextPacket: first}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if index.track == nil || index.source == nil {
		return out, ErrMP4PacketUnsupported
	}
	out.Timescale = index.track.timescale
	if first < 0 || first >= index.track.count || maxBytes < 1 || maxBytes > maxMP4RangeBytes {
		return out, errors.New("MP4 packet range requires an indexed packet ordinal and 1-65536 bytes")
	}
	cursor, err := index.cursor(ctx, first)
	if err != nil {
		return out, err
	}
	for cursor.packet < index.track.count {
		if err := ctx.Err(); err != nil {
			return failedMP4Range(out), err
		}
		n := cursor.packetSize()
		if n == 0 {
			out.Stop = "unsupported_empty_packet"
			if len(out.Packets) == 0 {
				return out, ErrMP4PacketUnsupported
			}
			return out, nil
		}
		if n > int64(maxBytes-len(out.Data)) {
			out.Stop = "byte_limit"
			if len(out.Packets) == 0 {
				out.RequiredBytes = n
				return out, ErrMP4PacketRangeTooSmall
			}
			return out, nil
		}
		if len(out.Packets) == maxMP4RangePackets {
			out.Stop = "packet_limit"
			return out, nil
		}
		if cursor.offset < 0 || cursor.offset > index.size || n > index.size-cursor.offset {
			return failedMP4Range(out), ErrMP4PacketUnsupported
		}
		if out.Data == nil {
			out.Data = make([]byte, 0, maxBytes)
		}
		start := len(out.Data)
		out.Data = out.Data[:start+int(n)]
		got, err := index.source.ReadAt(out.Data[start:], cursor.offset)
		if got != int(n) || err != nil && !errors.Is(err, io.EOF) {
			if err == nil {
				err = io.ErrUnexpectedEOF
			}
			return failedMP4Range(out), err
		}
		if err := ctx.Err(); err != nil {
			return failedMP4Range(out), err
		}
		duration := int64(index.track.stts[cursor.timeRun][1])
		out.Packets = append(out.Packets, MP4Packet{cursor.packet, cursor.offset, cursor.offset + n, cursor.tick, cursor.tick + duration})
		if err := cursor.advance(ctx); err != nil {
			return failedMP4Range(out), err
		}
		out.NextPacket = cursor.packet
	}
	out.EndOfTrack, out.Stop = true, "end_of_track"
	return out, nil
}

func failedMP4Range(out MP4PacketRange) MP4PacketRange {
	out.Data, out.Packets, out.NextPacket, out.Stop = nil, nil, out.FirstPacket, "failed"
	return out
}

type mp4PacketCursor struct {
	track              *mp4Track
	packet, chunk      int
	chunkEnd, chunkRun int
	timeRun, timeLeft  int
	offset, tick       int64
}

func (index MP4PacketIndex) cursor(ctx context.Context, first int) (mp4PacketCursor, error) {
	t := index.track
	c := mp4PacketCursor{track: t, packet: first}
	work := 0
	check := func() error {
		work++
		if work%4096 == 0 {
			return ctx.Err()
		}
		return nil
	}
	base := 0
	for chunk, offset := range t.offsets {
		if err := check(); err != nil {
			return c, err
		}
		for c.chunkRun+1 < len(t.stsc) && uint32(chunk+1) >= t.stsc[c.chunkRun+1][0] {
			c.chunkRun++
		}
		end := base + int(t.stsc[c.chunkRun][1])
		if first >= end {
			base = end
			continue
		}
		c.chunk, c.chunkEnd, c.offset = chunk, end, offset
		if t.constSize != 0 {
			c.offset += int64(first-base) * int64(t.constSize)
		} else {
			for i := base; i < first; i++ {
				if err := check(); err != nil {
					return c, err
				}
				c.offset += t.packetSize(i)
			}
		}
		break
	}
	remaining := first
	for run, entry := range t.stts {
		if err := check(); err != nil {
			return c, err
		}
		if remaining >= int(entry[0]) {
			remaining -= int(entry[0])
			c.tick += int64(entry[0]) * int64(entry[1])
			continue
		}
		c.timeRun, c.timeLeft = run, int(entry[0])-remaining
		c.tick += int64(remaining) * int64(entry[1])
		return c, nil
	}
	return c, fmt.Errorf("%w: missing packet timing", ErrMP4PacketUnsupported)
}

func (t *mp4Track) packetSize(packet int) int64 {
	if t.sizes != nil {
		return int64(t.sizes[packet])
	}
	return int64(t.constSize)
}

func (c *mp4PacketCursor) packetSize() int64 { return c.track.packetSize(c.packet) }

func (c *mp4PacketCursor) advance(ctx context.Context) error {
	c.offset += c.packetSize()
	c.tick += int64(c.track.stts[c.timeRun][1])
	c.packet++
	if c.packet == c.track.count {
		return nil
	}
	if c.packet == c.chunkEnd {
		c.chunk++
		if c.chunkRun+1 < len(c.track.stsc) && uint32(c.chunk+1) >= c.track.stsc[c.chunkRun+1][0] {
			c.chunkRun++
		}
		c.chunkEnd += int(c.track.stsc[c.chunkRun][1])
		c.offset = c.track.offsets[c.chunk]
	}
	c.timeLeft--
	for c.timeLeft == 0 {
		if c.timeRun%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		c.timeRun++
		c.timeLeft = int(c.track.stts[c.timeRun][0])
	}
	return nil
}
