# 0015: MP3 frame index and decodable audio ranges

Status: accepted and implemented, 2026-10-11. The single-file audio part of
[issue 1](https://github.com/blisspixel/nemalo/issues/1) and a first increment of
milestone 8. Multi-file recordings are not covered.

## Decision

`internal/audio` indexes MPEG audio frames (MPEG-1, MPEG-2, and MPEG-2.5,
Layers I through III) from their headers, in Go, without decoding. The content
service exposes it as representation `mpeg-audio/1` (extractor
`nemalo.mpeg.frames/1`) through the same request, locator, continuation,
stale-reference, and bounds as EPUB text, in CLI JSON, human output, and MCP.
It is selected explicitly; `content units` and `content read` on an MP3 with an
EPUB representation report `unsupported_extraction`.

`content units --representation mpeg-audio/1` returns one unit, the track, with:

- Probed facts: format, sample rate, channels, channel mode, constant or variable
  bitrate, indexed frame count, encoded samples (frames times samples per frame),
  encoded duration, and the byte range of the frames.
- Declared facts, kept separate and unverified: the Xing, Info, or VBRI frame
  count and byte count; the LAME or FFmpeg encoder name, delay, and padding;
  ID3v2.3 or v2.4 title, artist, album, track, language, `TLEN`, and `CHAP`
  chapters in milliseconds. Presented samples, encoded minus declared delay and
  padding, are reported as derived from that declaration. Disagreements between
  declared and indexed counts or lengths are findings.
- Tag regions (ID3v2, ID3v1, APEv2, info frame) and unindexed byte regions. A
  region between frames is reported because later sample positions assume it
  holds no audio; a trailer is labeled as one. Nothing is skipped silently.
- `decoded: not_decoded`. Encoded positions are not verified playback times.

`content read --representation mpeg-audio/1 --sample N` returns whole frames
starting at the frame containing encoded sample N, as base64 bytes of the file
range, up to `--max-bytes`, with a continuation to the next frame. Offsets are
encoded samples per channel, zero-based and half-open. A range never spans an
unindexed region. The locator and `audio_range` give the byte and sample
boundaries, so a local player can also read the file directly.

## Why no dependency

Indexing reads fixed 32-bit frame headers, three fixed info-frame layouts, and
ID3v2 frames. That is a few hundred lines of bounded Go against well-specified
formats. Decoding, which is where MP3 complexity and risk concentrate, is not
done at all. A Go MP3 decoder dependency would add code that is never run here,
and an external tool such as FFmpeg would make a core read path depend on an
installed executable. FFmpeg is used only as an independent reference during
validation. Decoded checks remain a separate, explicitly optional decision.

## Decoder preroll

Layer III frames borrow main data from earlier frames (the bit reservoir), and
a frame's output depends on decoder history. A range therefore starts with
preroll frames whose decoded output the caller discards. Preroll is the largest
of the requested frame's reservoir depth and, for each history frame k, k plus
that frame's reservoir depth. History is one frame for MPEG-1, whose frames hold
two granules, and two frames for MPEG-2 and 2.5, whose frames hold one: a
frame's output needs the previous granule's subband samples, which need that
granule's overlap from the granule before it. The stream start counts as
complete, because a whole-file decoder starts from the same empty state. A gap
before the requested frame makes `preroll_complete` false.

The two-frame MPEG-2 history was found by validation: with one frame of history,
MPEG-2 ranges differed from a whole-file decode in the first requested frame.

## Bounds

Sources stay within the existing 32 MiB snapshot and 256 MiB managed limits. The
index holds at most 2,000,000 frames and 1,024 unindexed regions; resynchronization
searches at most 64 KiB, and the first frame must appear within 1 MiB. A frame is
accepted only when the next header continues the same stream or the frame ends
the data. ID3 text is bounded to 2,000 bytes per value and 1,024 chapters.
Unsynchronised, compressed, or encrypted ID3 content, and ID3v2.2, are skipped
and named, never guessed. Free-format bitrates are unsupported.

## Validation

Synthetic frame tests cover every MPEG version and layer, CBR and VBR, ID3v2.4
text in each encoding, chapters, ID3v1 and APEv2 trailers, info frames with
encoder fields, gaps, truncation, stream changes, non-audio input, cancellation,
range tiling, reservoir and history preroll, and gap boundaries. A fuzz target
ran 44 million executions without a failure.

Against FFmpeg as an independent reference: four libmp3lame files (CBR, VBR at
48 kHz, MPEG-2 mono at 22.05 kHz, and ID3v2.3 chapters) and four LibriVox tracks
from Internet Archive (VBR, 128 kbps, and two 64 kbps MPEG-2) gave the same
sample rate, channels, chapters, and encoded duration as `ffprobe`, to the
millisecond. On the synthesized files, presented samples equal the source length
exactly. Seven ranges per file decoded bit-exactly against a whole-file decode
(56 of 56) when preroll was included and in none of the 56 without it.
`ffprobe` reported one or two fewer packets than its own duration implies on
some files; Nemalo's count agrees with that duration and with the encoders'
declared frame counts.

## Not covered

Recording identity, ordered track sets, completeness across files, M4B, decoded
duration, audio fingerprints, and playback remain milestone 8 work. A file is one
track; nothing here claims a recording is complete.
