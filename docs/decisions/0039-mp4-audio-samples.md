# 0039: M4B and M4A sample tables

Status: accepted and implemented, 2026-10-19. Extends the checked policy of
[decision 0033](0033-mp3-checked-policy.md) to MP4 audio, beside the MPEG frame
index of [decision 0015](0015-mpeg-audio-frame-index.md).

## Context

Audiobooks are often one M4B file: AAC audio in an MP4 container with a
chapter list. A download cut short, a damaged rewrite, or protected audio
cannot be told apart from a good file by its signature or size alone. Unlike
an MP3, the samples of an MP4 are not self-delimiting: their places and sizes
are stated only in the movie's sample tables, so a cut-short file can still
start with a complete movie box. No maintained Go module reads MP4 sample
tables within this project's dependency policy, and the box structure needed
is small and fully specified (ISO/IEC 14496-12 and -14, and Apple's QuickTime
file format for chapter tracks and sound description versions).

## Decision

`internal/audio` reads MP4 boxes through a reader at offsets, never loading
media data, as extractor `nemalo.mp4.samples/1`. It requires a leading file
type box and one movie box, reads at most 64 MiB of movie box, 200,000 boxes,
16 levels of nesting, 64 tracks, and 8,000,000 samples or chunks per table,
and checks every table length against its box before allocating. From the
sample-to-chunk, chunk offset (`stco` or `co64`), and sample size (`stsz` or
`stz2`) tables it places every sample of every track. These are findings that
keep a file in review:

- `mp4_samples_outside_file`: a sample extends past the end, as when a
  download was cut short.
- `mp4_box_extends_past_file`: a top-level box declares more bytes than the
  file holds.
- `mp4_samples_outside_media_data`, `mp4_samples_overlap`: a sample is not
  inside a media data box, or two samples share bytes.
- `mp4_sample_tables_inconsistent`: the tables disagree on the sample count,
  or a table is missing or out of its box.
- `mp4_fragments_not_indexed`: a fragmented file, whose samples are in movie
  fragments that this extractor does not index.
- `mp4_audio_encrypted`: an `enca`, `drms`, or `aavd` sample entry or a
  protection scheme box. It is also an access restriction; Nemalo does not
  remove protection.
- `mp4_audio_tracks_not_one`, `mp4_audio_codec_unrecognized`,
  `mp4_no_audio_samples`, `mp4_chapters_unreadable`.

A file with no movie box, no audio track, or a malformed box structure is
`mp4_samples_not_indexed`. Notes do not change what plays: bytes after the
last box (`mp4_unindexed_trailing_region`), media data no sample references
(`mp4_unreferenced_media_data`), a declared media duration more than a second
from the sum of sample durations (`mp4_declared_duration_differs`), a track
that is neither audio nor its chapter track (`mp4_non_audio_tracks`), and
QuickTime and Nero chapter lists that differ (`mp4_chapter_sources_differ`).

Codecs are named from the sample entry and the elementary stream descriptor:
AAC with its audio object type, MP3 in MP4, ALAC, Opus, FLAC, AC-3, and E-AC-3.
Encoded timing is the sum of the audio track's sample durations over its
timescale; the media and movie headers' durations are kept separately as
declarations, and an edit list is recorded, not applied. Chapters come from
the QuickTime chapter track that the audio track references, otherwise from a
Nero `chpl` list; each ends where the next begins and the last at the movie's
declared end. iTunes item text (title, artist, album, narrator, composer,
year) and the media language are declarations. Nothing is decoded.

`check`, import, ingest, rescan, and acquisition accept `.m4b` and `.m4a`.
Both are the same container; the extension names an audiobook or a track, and
a file of this container with another name is assessed as M4A and reported as
an extension mismatch. An M4B or M4A is `checked`, with reason
`checks_and_scan_recorded`, when every sample was located with no finding and
the installed scanner reported no detections. That is evidence about its
structure and scan, not that the audio decodes or the recording is complete.
A library that holds an M4B or M4A needs this version or newer.

Content access adds `mp4-audio/1`, units only: the sample table summary,
timing, chapters, and item text. MP4 samples cannot be played apart from their
container, so no byte ranges are served; a player gets a verified copy through
`library open` or `export`. The TUI lists the summary and each chapter.

## Consequences

Truncation, misplaced or overlapping samples, fragments, and protection are
found without decoding or a new dependency. An M4B or M4A may be 2 GiB, beyond
the 256 MiB limit of the formats parsed in memory
([decision 0040](0040-large-mp4-audio-and-scan-limits.md)). Decoded timing, encoder priming,
multi-file M4B recordings, and chapter-to-text alignment remain later work.

## Validation

Unit tests build synthetic files: AAC with a QuickTime chapter track, Nero
chapters, and item text; Nero chapters alone and differing; a cut-short file;
samples placed in the movie box; two audio tracks sharing samples; a sample
count the time table does not match; protected, unknown, and fragmented
entries; 64-bit box sizes with `co64` and `stz2` tables; trailing bytes;
missing file type, movie, or audio; nesting, table-size, and cancellation
bounds; and padded brands. A fuzz target found no panic or slow input in
about ten million runs. Library tests import a clean M4B as checked, a cut-short
M4A in review, and an M4B intake packet; content and TUI tests list chapters
and refuse a range.

Live, on this machine: files written by FFmpeg 9.0 (AAC with chapters, moved
movie box, fragmented, ALAC, MP3 in MP4, Opus, two tracks, QuickTime) gave the
sample counts and durations FFprobe reports, and cut copies were found. A
LibriVox M4B from Archive, `art_of_war_librivox/ArtOfWar-64kb_librivox.m4b`
(18,084,624 bytes, MD5 equal to Archive's published value), was acquired with
`acquire --file`. It located 93,417 AAC samples at 22,050 Hz in 4,250 chunks for
4,338,277 ms and 7 chapters, the same counts, duration, and chapter starts
FFprobe reports, with no finding, and imported as checked after a ClamAV scan
reported no detections. A second, `aesop_fables_volume_one_librivox/AesopsFables64kbps_librivox.m4b`
(93,029,739 bytes, MD5 equal to Archive's), located 488,183 samples for
22,671,174 ms and 285 chapters; all 285 chapter starts equal FFprobe's to the
millisecond.
