# 0020: recording manifests across held tracks

Status: accepted and implemented, 2026-10-11. Completes the multi-file audio part
of [issue 1](https://github.com/blisspixel/nemalo/issues/1) for milestone 8,
over [decision 0015](0015-mpeg-audio-frame-index.md) (one track) and
[decision 0018](0018-whole-recording-acquisition.md) (acquisition and positions).

## Decision

`library recordings LIBRARY [--source archive:ITEM]`, the MCP tool
`library_recordings`, and `t` on a track in the TUI Holdings list return
`recording-manifest/1` for each declared recording held in a managed library:

- A recording is a source ID and declared format. Different formats or sources
  are different recordings, so different narrations never merge.
- Tracks appear in declared position order with their exact asset ID, size,
  source file, declared title and duration (recorded since this decision from
  the evaluation at acquisition), and holding status.
- `missing_positions` lists declared positions with no holding. A position
  claimed by more than one asset, or tracks declaring different counts, is a
  finding. `complete` means every declared position is held by exactly one asset
  and the counts agree; it is not a check of the audio.
- With `--source`, each held track's stored bytes are read, verified against the
  holding's size and SHA-256, and indexed with the shared MPEG frame index:
  sample rate, channels, encoded samples and duration, and ID3 chapters.
  `start_ms` is the probed offset of a track from the start of the recording,
  given only through an unbroken run of indexed tracks from position 1. The
  probed total appears only for a complete recording. Changed or missing stored
  bytes report `stored_bytes_changed` and are not indexed. One call indexes at
  most 4 GiB.
- Without `--source`, manifests come from the journal alone and nothing is read.
- `narrators` lists the readers held tracks were credited with by a LibriVox
  project that links back to the item and whose sections match its tracks
  ([decision 0035](0035-librivox-readers.md)); otherwise it stays empty rather
  than guessed. Track-local ranges and preroll come from `mpeg-audio/1`
  on each asset; the manifest gives their order and offsets.

## Consequences

Callers can play or process a recording in declared order, know exactly which
tracks are missing, and place track-local ranges on one timeline without
decoding. Narrator evidence is [decision 0035](0035-librivox-readers.md); narrations
across recordings and single-file M4B recordings are
[decision 0044](0044-narration-identity.md).

## Validation

Synthetic tests cover order and offsets over three tracks, the journal-only
listing, a source filter, a missing position (offsets stop at the gap and no
total is given), a conflicting asset for one position, stored bytes changed after
import, the CLI and JSON outputs, the MCP tool list, and the TUI action keeping
its list.
