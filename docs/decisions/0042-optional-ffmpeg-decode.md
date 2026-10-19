# 0042: optional full audio decode with FFmpeg

Status: accepted and implemented, 2026-10-19. Adds decoded timing to milestone
8 beside the probed timing of [decision 0015](0015-mpeg-audio-frame-index.md)
and [decision 0039](0039-mp4-audio-samples.md), as an optional adapter like
EPUBCheck in [decision 0038](0038-optional-epubcheck.md).

## Context

Frame and sample indexes find truncation, misplaced data, and damage between
frames, but not damage inside a frame: a corrupted AAC or MP3 frame indexes
cleanly and fails only when decoded. Milestone 8 asks for decoded timing kept
apart from declared and probed timing, from deliberately selected optional
decoding tools. Writing audio decoders in Go is out of scope; FFmpeg is the
maintained reference decoder for these formats and is often installed.

## Decision

`check FILE --decode` decodes an MP3, M4B, or M4A snapshot in full with the
installed FFmpeg and FFprobe. It is never required: it does not run unless
asked, Nemalo never installs or downloads FFmpeg, and `doctor` reports both
tools.

- **Containment.** Both tools read the private snapshot as `file:PATH` with
  `-protocol_whitelist file` and a demuxer fixed by format (`mp3`, or `mov`
  for M4B and M4A), so the file is never probed as a playlist or another kind
  of input, and no network or other protocol can be opened. The `file`
  protocol alone would allow any local path, so confinement to the snapshot
  rests on the `mov` demuxer's defaults of not following data references
  (`enable_drefs`) or absolute paths (`use_absolute_path`), which are never
  enabled. Arguments are an array; the
  output is bounded to 64 KiB of messages and counted, never stored.
- **Measurement.** FFprobe reports the decoder's output sample rate, which can
  differ from the container's. FFmpeg then decodes the first audio stream to
  one 16-bit channel at that rate, and Nemalo counts the bytes: decoded
  samples per channel and a decoded duration, after FFmpeg's own trimming of
  encoder delay and padding. Every line FFmpeg prints at error level is a
  decoder error, and a folded "Last message repeated N times" counts N.
- **Results.** `passed` is a clean exit with no error line. `errors_reported`
  has error lines or a failed exit after some audio. `unavailable` means
  either tool is missing. `incomplete` means no audio stream, no decoded
  audio, a partial sample, or the deadline: two minutes and a minute per
  64 MiB, at most an hour.
- **Findings.** Decoder errors, a requested decode that did not pass, and a
  decoded duration more than a second from the encoded duration keep the file
  in review. The decode is bound to the snapshot like a scan: the snapshot is
  rehashed afterwards. It is evidence in the check report, not part of the
  checked policy, and import does not decode. A `--decode` or `--epubcheck`
  that does not apply to the assessed format, or a batched check, is named in
  the report's limitations instead of passing silently.

The check's five-minute bound now covers only the snapshot and inspection; a
scan, EPUBCheck, and decode run under the caller's context with their own
deadlines by size, as batch rescans already did, so a large file is not cut
short.

## Consequences

Damage inside frames, and a stream that stops decoding early, are found when
asked. FFmpeg parses untrusted input and has a long history of parser
vulnerabilities; the adapter contains its inputs but not the decoder itself,
so `--decode` is for files the user means to keep, and its help says so.
Decoded duration describes FFmpeg's output, not playback in another player.
Container-declared channels can differ from the decoder's; both are reported.

## Validation

Tests run the test binary as a stand-in FFmpeg and FFprobe that refuses any
call without the file protocol whitelist, the fixed demuxer, and a `file:`
input: a clean decode counts its samples, error lines and a repeated-message
count are errors, a partial sample, a failed open, and no audio stream are
incomplete, a missing FFprobe is unavailable, and a hanging decode stops at the
caller's deadline. Assessment tests turn each result into its finding.

Live, on Linux amd64 with FFmpeg 9.0.2: a text playlist named `.m4b` that
points at a local URL was refused as invalid data, with nothing fetched. The
LibriVox M4Bs of decision 0039 decoded with no errors to exactly their encoded
durations: 4,338,277 ms for `art_of_war_librivox` in about a second and
47,202,057 ms for the 13-hour `cambridgemodernhistory5_2307_librivox` part in
40 seconds. A LibriVox MP3 track decoded to 506,775 ms against 506,853 ms
encoded, the difference being its trimmed encoder delay and padding. A copy of
an M4B with 2,000 bytes of its media data overwritten indexed cleanly but
decoded with 24 errors to 9,721 of 10,023 ms, and was held in review; a
cut-short copy decoded with 3 errors beside its sample-table findings.
