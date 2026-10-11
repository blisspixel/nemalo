package audio

import (
	"errors"
	"fmt"
)

// Range is a contiguous, frame-aligned slice of the file for a decoder. Sample
// positions are encoded samples per channel, zero-based and half-open, counted
// over indexed frames only (gaps and tags hold no samples). Preroll frames come
// before the requested frames so a Layer III decoder has the bit reservoir and
// filterbank history it needs; a decoder should discard their output.
type Range struct {
	FirstFrame       int    `json:"first_frame"`
	Frames           int    `json:"frames"`
	PrerollFrames    int    `json:"preroll_frames"`
	PrerollComplete  bool   `json:"preroll_complete"`
	ByteStart        int64  `json:"byte_start"`
	ByteEnd          int64  `json:"byte_end"`
	SampleStart      int64  `json:"sample_start"`
	SampleEnd        int64  `json:"sample_end"`
	PrerollStart     int64  `json:"preroll_sample_start"`
	EndOfTrack       bool   `json:"end_of_track"`
	NextFrame        int    `json:"next_frame,omitempty"`
	StoppedAtGap     bool   `json:"stopped_at_gap"`
	MinimumMaxBytes  int    `json:"minimum_max_bytes,omitempty"`
	SampleRate       int    `json:"sample_rate"`
	Channels         int    `json:"channels"`
	SamplesPerFrame  int    `json:"samples_per_frame"`
	PresentationNote string `json:"presentation_note"`
}

// ErrRangeTooSmall means maxBytes cannot hold the first frame and its preroll.
var ErrRangeTooSmall = errors.New("max bytes cannot hold one frame and its decoder preroll")

// FrameForSample returns the frame containing an encoded sample position.
func (t Track) FrameForSample(sample int64) (int, error) {
	if sample < 0 || t.SamplesPerFrame == 0 || sample >= t.EncodedSamples {
		return 0, fmt.Errorf("sample must be 0 through %d", max(0, t.EncodedSamples-1))
	}
	return int(sample / int64(t.SamplesPerFrame)), nil
}

// Range returns up to maxBytes of whole, byte-contiguous frames starting at
// frame first, preceded by preroll. A range never spans an unindexed gap: it
// stops before one, and the next range starts after it.
func (t Track) Range(data []byte, first, maxBytes int) (Range, error) {
	r := Range{SampleRate: t.SampleRate, Channels: t.Channels, SamplesPerFrame: t.SamplesPerFrame, PresentationNote: "Encoded samples. A decoder adds its own delay; the declared encoder delay and padding are not applied or verified."}
	if first < 0 || first >= len(t.offsets) {
		return r, fmt.Errorf("frame must be 0 through %d", len(t.offsets)-1)
	}
	contiguous := func(i int) bool { // frame i ends where frame i+1 begins
		return t.offsets[i]+int64(t.headers[i].length) == t.offsets[i+1]
	}
	// depth counts the contiguous frames before frame i whose main data covers
	// the bytes i borrows from the bit reservoir.
	depth := func(i int) (int, bool) {
		need, have, n := t.reservoir(data, i), 0, 0
		for j := i; have < need; j-- {
			if j == 0 {
				// The stream start: a whole-file decoder lacks the same bytes,
				// so the range reproduces its output.
				return n, true
			}
			if !contiguous(j-1) || n >= maxPrerollFrame {
				return n, false
			}
			h := t.headers[j-1]
			have += h.length - h.dataStart()
			n++
		}
		return n, true
	}
	// Layer III output for a frame needs the previous granule's subband
	// samples, which need that granule's own IMDCT overlap from the granule
	// before it. MPEG-1 frames hold two granules, so one earlier frame is
	// history; MPEG-2 and 2.5 frames hold one, so two earlier frames are. Each
	// history frame also needs its own bit reservoir.
	pre, complete := 0, true
	if h := t.headers[first]; h.layer == 3 && first > 0 {
		history := 1
		if h.version != 1 {
			history = 2
		}
		own, ok := depth(first)
		pre, complete = own, ok
		for k := 1; k <= history && first-k >= 0; k++ {
			if !contiguous(first - k) {
				complete = false
				break
			}
			d, ok := depth(first - k)
			pre, complete = max(pre, d+k), complete && ok
		}
		if first-pre < 0 {
			pre, complete = first, false
		}
	}
	r.PrerollComplete = complete
	start := first - pre
	r.FirstFrame, r.PrerollFrames, r.ByteStart = first, pre, t.offsets[start]
	size := 0
	for i := start; i < first; i++ {
		size += t.headers[i].length
	}
	last := first
	for {
		size += t.headers[last].length
		if last+1 >= len(t.offsets) || !contiguous(last) || size+t.headers[last+1].length > maxBytes {
			break
		}
		last++
	}
	if size > maxBytes {
		r.MinimumMaxBytes = size
		return r, ErrRangeTooSmall
	}
	r.Frames = last - first + 1
	r.ByteEnd = t.offsets[last] + int64(t.headers[last].length)
	spf := int64(t.SamplesPerFrame)
	r.SampleStart, r.SampleEnd, r.PrerollStart = int64(first)*spf, int64(last+1)*spf, int64(start)*spf
	r.EndOfTrack = last == len(t.offsets)-1
	if !r.EndOfTrack {
		r.NextFrame = last + 1
		r.StoppedAtGap = !contiguous(last)
	}
	return r, nil
}

// reservoir returns how many main-data bytes frame i borrows from earlier
// frames (Layer III main_data_begin), or 0 when it cannot be read.
func (t Track) reservoir(data []byte, i int) int {
	h := t.headers[i]
	if h.layer != 3 {
		return 0
	}
	p := int(t.offsets[i]) + 4
	if h.protected {
		p += 2
	}
	if p+2 > len(data) {
		return 0
	}
	if h.version == 1 {
		return int(data[p])<<1 | int(data[p+1])>>7
	}
	return int(data[p])
}
