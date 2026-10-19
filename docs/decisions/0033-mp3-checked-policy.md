# 0033: checked MP3 tracks

Status: accepted and implemented, 2026-10-19. Completes the checked policy of
[decision 0009](0009-managed-epub-import.md) for the three held formats, after
[decision 0032](0032-pdf-checked-policy.md) for PDF, over the MPEG frame index
of [decision 0015](0015-mpeg-audio-frame-index.md).

## Decision

`check`, import, ingest, and rescan index an MP3's MPEG frames with the shared
frame index, without decoding audio. A track whose frames index has signature
`mp3_frames_indexed` and records its format, sample rate, channels, frame
count, and encoded duration. Two measurements are findings that keep it in
review:

- `mp3_declared_frames_differ`: an Xing, Info, or VBRI frame declares a frame
  count the file does not hold, as when a download was cut short.
- `mp3_unindexed_regions_between_frames`: bytes between frames that are not
  frames, as when a file is damaged.

Bytes that index no frames are `mp3_frames_not_indexed`. Two measurements are
notes, not findings, because they do not change what plays: an unindexed
region after the last frame, such as an APE or Lyrics3 tag
(`mp3_unindexed_trailing_region`), and an ID3 length more than a second from
the indexed duration (`mp3_declared_length_differs`).

An MP3 is `checked`, with reason `checks_and_scan_recorded`, when its frames
were indexed with no finding and the installed scanner reported no detections.
That is evidence about its structure and scan, not that the audio decodes or
that a recording is complete; a recording's completeness stays the manifest of
[decision 0020](0020-recording-manifest.md). `library rescan` assesses MP3
holdings in review for lack of a scan, including those an earlier version held
as `format_not_checked_for_publication`. A library that holds a checked MP3
needs this version or newer.

## Validation

Assessment tests index 40 clean frames, and find junk between frames, an info
frame declaring 99 frames over 41, and bytes with no frames; a trailing tag is
a note only. A library test imports a clean track as checked and a damaged one
in review. Live: a LibriVox track from Archive, `art_of_war_01-02_sun_tzu_64kb.mp3`
(4,055,167 bytes, MD5 equal to Archive's published value), indexed 19,403
MPEG-2 Layer III frames at 22,050 Hz mono for 506,853 ms with no finding, and
imported as checked after a ClamAV scan reported no detections.
