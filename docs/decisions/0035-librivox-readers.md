# 0035: LibriVox readers linked both ways

Status: accepted and implemented, 2026-10-19. Narrator identity for
milestone 8, over the recording acquisition of
[decision 0018](0018-whole-recording-acquisition.md) and the manifest of
[decision 0020](0020-recording-manifest.md), using the LibriVox API listed in
[SOURCES.md](../SOURCES.md) under the provider rules of
[decision 0005](0005-provider-search-and-evaluation.md).

## Context

Archive items for LibriVox recordings name their readers only in description
prose ("Read by Moira Fogarty."), and per-file `artist` is empty. The LibriVox
API declares readers per section with stable reader IDs, and a different
reading of the same text is a different project. It cannot be searched by
Archive identifier. An Archive LibriVox item's `call_number`, however, is its
LibriVox project ID, and that project's `url_iarchive` names the item: on
2026-10-19 this held both ways for `art_of_war_librivox` (119),
`art_war_ps_librivox` (1184), `artofwar3_pc_librivox` (7318), and three 2024
items (20390, 20829, 20422).

## Decision

Evaluating an Archive item that has MP3 recordings, is in the `librivoxaudio`
collection, and has a numeric `call_number` reads that one LibriVox project
from `https://librivox.org/api/feed/audiobooks?id=N&format=json&extended=1`
with the shared metadata client: a fixed host, checked direct dial, bounded
JSON, spacing, and retry of transient failures. The evaluation's `librivox`
field records the project ID and a status:

- `linked`: the project's `url_iarchive` names this item. The project, its
  sections, and their readers are recorded with the record's SHA-256.
- `not_linked`: it names another item or none. No reader is credited.
- `unavailable`: the record could not be read or was outside the contract
  (another project, duplicate section numbers, a reader without an ID or
  name, more than 2,000 sections or 50 readers in one). The Archive
  evaluation continues.
- `not_checked`: no LibriVox client was configured, as in offline tests.

Readers are credited per track only when a linked project has as many
sections as the recording has tracks and, in section order, every track's
Archive-declared length is within 3 seconds of its section's declared
playtime. Then each track carries its section's readers and the recording's
`reader_evidence` is `librivox_sections_matched`; otherwise no track is
credited and it is `librivox_sections_not_matched`. Titles and names are never
used to match.

Acquisition records a track's readers in its intent and receipt, beside its
declared title and length; ingest stores them in the holding's source record.
`recording-manifest/1` lists them per track and gives `narrators` as the
distinct readers held tracks were credited with, in first-credited order,
from journal evidence alone, so `library recordings` stays offline. The
manifest schema keeps its version: `narrators` changes from always empty to
reader objects (`librivox_reader_id`, `name`). A library that holds a track
with readers needs this version or newer, because an earlier version reads
the unknown field in that journal line as invalid.

Readers are LibriVox catalog declarations. They are not a check of who is
heard in the audio, a rights statement, or evidence that two projects are the
same reading.

## Validation

Discovery tests link a project that names the item back, and refuse to link
one that names another item, a failing catalog (the Archive evaluation
continues), and no configured client; a non-LibriVox item with a call number
makes no request. Records with another project, none, duplicate sections, or
readers without an ID or name are refused. Matching credits both formats of a
fixture whose lengths agree, withholds credit from one track set whose second
track is four minutes off, and from a project with a different section count.
Live on 2026-10-19: `archive:art_of_war_librivox` linked project 119, matched
all 7 tracks of the 64 kbps recording to Moira Fogarty (reader 209), and after
acquisition and ingest with ClamAV the manifest named her as the narrator.
