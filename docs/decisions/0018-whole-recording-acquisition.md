# 0018: whole-recording acquisition with track-level resume

Status: accepted and implemented, 2026-10-11. An increment of milestones 5 and
8, built on selected acquisition ([decision 0007](0007-selected-acquisition.md))
and folder ingest ([decision 0017](0017-folder-and-zip-ingest.md)).

## Context

A LibriVox reading on Archive is several MP3 files, usually offered in more than
one encoding. Single-file acquisition made a user fetch each track by hand and
lost the order. Archive file metadata declares the grouping: originals (often
`VBR MP3`) carry a `track` number, and derivatives (for example `64Kbps MP3`)
name their `original`. A live check of `art_of_war_librivox` showed both forms,
with durations as seconds (`507.1`) on originals and `MM:SS` on derivatives.

## Decision

- Evaluation reports `recordings`: the MP3 files of an item grouped by declared
  Archive format. Each track takes its order from its own declared track number,
  or from the original it names. Without distinct numbers for every track, the
  order falls back to file names and `order_source` says `file_name`. A recording
  is `acquirable` only when every track is a public file candidate with a usable
  size and a source checksum, and it has at most 1,000 tracks. Titles and
  durations are bounded, single-line declarations.
- `acquire archive:ITEM --recording FORMAT --output DIR` evaluates the item anew
  and selects the recording by its exact declared format. Every track must pass
  the same eligibility as a single selected file, and the declared total must fit
  `--max-recording-bytes` (default 4 GiB, at most 16 GiB).
- The output is a new directory holding `recording-intent.json`, synced before
  any request, then one ordinary intake packet per track named by position
  (`0001`, `0002`, ...), then `recording.json`, written last and only when every
  track has a receipt. Each track's intent and receipt carry its place
  (`recording: {source_format, position, tracks}`), so a track packet stays
  self-describing.
- Tracks go through the same transfer, checks, read-back hash, scan, and
  quarantine as single files; the packet code is shared, not copied. ClamAV
  scans groups of up to 32 tracks and 256 MiB in one invocation, and their
  receipts wait for that scan; batch results that cannot be attributed by
  position are incomplete. Defender scans files one at a time, so with it, or
  without a scan, each track's receipt is written before the next track is
  requested. A detection stops the recording. A cancellation before a group's
  receipts leaves those packets without receipts.
- `--resume` continues a directory this operation created. The fresh
  declarations must still list the same files, sizes, and checksums in the same
  order as the recording intent; otherwise nothing is requested. A track packet
  is reused only when its receipt completes that exact file at that position and
  its content still matches both the fresh declared size and checksum and the
  receipt's SHA-256, so an edited receipt cannot vouch for replaced bytes.
  Anything else, including a packet with transferred bytes but no receipt, is
  retained untouched and the track is fetched again under the next name
  (`0003-2`, up to nine attempts). A detection receipt stops the resume, and so
  does a `.pending` record left by an interrupted record write. A kept track
  keeps the scan result recorded when it was acquired. Retained packets are
  never adopted, removed, or overwritten.
- `library ingest` of a recording directory imports each completed track packet
  and records its place on the holding source. `library list` shows "Track N of
  M". A directory with an intent and content but no receipt is listed as not
  imported, so partial bytes never enter the library as a loose file. The
  recording records are listed as packet parts.

## Consequences

A whole LibriVox recording is one reviewed command in the CLI and one key (`a`)
in the TUI Evaluate list, and an interruption costs at most the track in flight, or one ClamAV scan group.
Track order is a source declaration; it is not checked against audio content.
Holdings record each track's declared place, but the library does not yet hold a
recording entity, narrator identity, or a completeness check across holdings.
Byte-range resume within one track (If-Range with a strong validator) remains
planned. Libraries with recording positions on sources cannot be opened by
earlier prereleases.

## Validation

Synthetic tests cover grouping and order through originals, duplicate and
missing track numbers, unusable tracks, restricted items, budget and format
refusals before any write (including an intent over 4 MiB), intent before
requests, receipt last, ClamAV-style batching versus per-track receipts for a
sequential scanner, unattributable batch results, cancellation during a group,
detections, resume after a failed and a cancelled track, changed declarations,
tampered content, an edited receipt, pending records, duplicate cleaned format
names, and ingest of a recording directory with a failed attempt. An
independent review found the edited-receipt, pending-record, and Defender
grouping defects before release; each has a regression test. Live: `art_of_war_librivox` reported both recordings in declared order;
the 7-track `64Kbps MP3` recording (34.7 MB) was acquired with Defender scans; an
acquisition interrupted after 12 seconds resumed and completed, retaining the
interrupted packets; ingest imported 7 tracks with positions and listed the 4
retained files as not imported.
