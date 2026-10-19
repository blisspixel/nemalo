# 0044: narrations across recordings, and single-file recordings

Status: accepted and implemented, 2026-10-19. Extends the recording manifests
of [decision 0020](0020-recording-manifest.md) with the LibriVox evidence of
[decision 0035](0035-librivox-readers.md), toward the recording and narration
identity of milestones 1 and 8.

## Context

A recording was a source ID and declared format, so one reading held as MP3
tracks and as an M4B appeared as two unrelated recordings, and a single M4B
audiobook was not a recording at all. Milestone 8 asks to preserve different
narrations while relating alternate encodings and hosts of the same one, on
recording evidence and exact hashes, never on titles or text.

## Decision

- **Narration.** A narration is one reading of a text, named
  `librivox:PROJECT`. An audio source (MP3, M4B, or M4A from an intake packet)
  records it at import only when its acquisition linked a LibriVox project
  that names that same Archive item back
  (`librivox_project_links_back`), with the project's title. Different
  projects are different narrations even for the same text and title; a
  source without that evidence names none. Nothing is inferred from titles,
  creators, or durations.
- **Single-file recordings.** A held M4B from an intake packet with a source
  ID is a recording of one track, its format being the source file name and
  its kind `single_file`; track sets from `acquire --recording` are kind
  `tracks`. With `--source`, an M4B is probed from its sample tables at
  offsets and then rehashed against its holding, giving its duration and
  chapters.
- **Grouping.** A recording names a narration only when every held track
  names the same one; tracks naming different narrations, or some naming none,
  are a finding. `library recordings` (and MCP `library_recordings`) adds
  `narrations`: each narration with the recordings held of it and whether each
  is complete. Exact duplicates were already one asset by SHA-256.
- Existing holdings keep what they recorded: a source imported by an earlier
  version names no narration until imported again from its packet.

The MP4 reader of [decision 0039](0039-mp4-audio-samples.md) is now
`nemalo.mp4.samples/2`: only the audio track's samples must lie inside media
data. Some writers place chapter titles and cover art after the media data,
outside any box; such a sample of another track is noted
(`mp4_non_audio_samples_outside_media_data`), and still must lie inside the
file, since it does not change what plays and its bytes are still read.

## Consequences

One reading held in several encodings is seen as one narration with several
recordings, and two readings of one text stay apart. Narrations exist only
for LibriVox readings on Archive; other providers' recording evidence, and
merging across hosts by acoustic or declared evidence, remain later work. A
library holding a narration record needs this version or newer.

## Validation

A library test ingests a two-track MP3 recording and an M4B of one LibriVox
reading, an M4B of another reading, an M4B whose project names a different
item, and one with no LibriVox link: the first two share `librivox:7`, the
second is `librivox:8`, and the last two name none. A probed single-file
recording gives its sample-table duration. A track set whose tracks name two
readings names none and reports the finding. An MP4 test places chapter
titles after the media data and keeps them readable, while misplaced audio
stays a finding.

Live, on this machine: the seven LibriVox MP3 tracks of
`archive:art_of_war_librivox` and its M4B were grouped as `librivox:119`, read
by Moira Fogarty, and `archive:art_war_ps_librivox/ArtOfWar_32kb_librivox.m4b`
(55,296,087 bytes, acquired with `acquire --file`, scanned with no detections)
was its own narration, `librivox:1184` "Art of War (version 2)", probed at
1:53:45.920 with 13 chapters. That file stores its cover image and chapter
titles after the media data; before the `/2` rule it was held in review, and
FFprobe reads all 213,310 audio samples and 13 chapters from it as the reader
now does.
