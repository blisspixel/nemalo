# 0017: folder and ZIP ingest through the single-file import

Status: accepted and implemented, 2026-10-11. A production staging increment of
milestone 2, built on the managed import of
[decision 0009](0009-managed-epub-import.md).
Nested traversal extends this service in
[decision 0058](0058-bounded-nested-intake.md); the single-container extractor
described below remains the cleanup path and still preserves nested members.

## Decision

`library ingest LIBRARY FOLDER [--apply] [--scan]` (TUI: F8 import in State
with a plain folder as the source) imports a whole explicit folder. It does not
add a second import path: every candidate goes through the same single-file
import, assessment, journal, and crash recovery as `library import`.

- The folder is inventoried once with the existing bounded inventory (10,000
  entries, depth 32, no links followed). Every entry appears in the result with
  an outcome: `checked`, `review`, `not_imported` with a reason, `packet_part`,
  `preserved` (an archive), or `failed`.
- A directory holding `receipt.json` and one `content.epub`, `content.pdf`, or
  `content.mp3` is imported as an intake packet, so its receipt evidence applies.
  The folder itself can be a packet. Only the receipt and content file are
  packet parts; any other entry in or beside a packet folder is handled on its
  own like any other entry.
- Loose EPUB, PDF, and MP3 files, and M4B and M4A files since
  [decision 0039](0039-mp4-audio-samples.md), are imported as files. Other files, links,
  special files, tar, and compressed streams are listed and preserved. RAR and
  7z archives are too unless 7-Zip is installed
  ([decision 0043](0043-rar-and-7z-through-7-zip.md)).
- ZIP archives are extracted by `internal/staging` into a private temporary
  directory one at a time: an archive's members are staged, imported (or
  previewed), and removed before the next archive is opened, so staging never
  holds more than one archive's budget. Only members with supported suffixes are
  written, under index-prefixed flat names, so member paths, case collisions,
  and directory tricks never reach the filesystem. Directories, links, special
  entries, encrypted members, unknown compression methods, nested archives,
  unsafe names (absolute, parent, drive, backslash, malformed), empty members,
  members over 256 MiB or beyond a 1 GiB archive total, and members declaring
  more than 100:1 expansion are recorded and left in the archive. A repeated or
  overlong (over 1,024 bytes) member name is not extracted, so every label names
  one member. Copies are capped at the declared size plus one byte and the CRC
  is computed for every member, including those recording a CRC of zero, which
  Go's ZIP reader does not check, so a lying or corrupt member fails instead of
  being imported.
- The folder and its archives are never modified, moved, or deleted. Cleanup
  remains milestone 4.

## Archive member provenance and recovery

An archive member is imported from a staging copy that does not outlive the run.
Its provenance is therefore a label, `ABSOLUTE_ARCHIVE_PATH!/MEMBER`, recorded as
a holding source of kind `archive_member` and used as the journal intent's
source. The archive part of a label is compared by file identity, so a
differently cased path on a case-insensitive filesystem still matches; the
member part is compared exactly. An archive whose path contains `!/` is
preserved and reported failed, because its label could not be split. If an import is interrupted, the next ingest of the same folder resumes
that pending import first, matching it by label against a fresh extraction; any
other pending import stops ingest before it imports anything. Libraries with
`archive_member` sources cannot be opened by earlier prereleases.

## Scanning

With `--scan`, candidate files are first assessed and scanned in batches with
`assessment.CheckManyInRoot` (ClamAV loads signatures once per batch). Each
import then scans its own private snapshot through a cache that hashes that
snapshot and returns a batch result only for identical bytes, so every recorded
scan is still bound to exactly the bytes imported. Only definitive batch results
(no detections reported, or findings) are cached; cache misses, incomplete batch
results, and packets are scanned individually. A live scan takes precedence over a packet receipt scan.

## Validation

Synthetic tests cover preview without writes, apply with files, a packet, ZIP
members with archive provenance, unsupported files, idempotent re-ingest, batch
scans, resuming an interrupted member by its label, refusal of overlapping
folders, and hostile ZIPs (traversal, absolute and drive names, backslashes,
links, encryption, nested archives, empty members, corrupted CRC, a CRC recorded
as zero, repeated and overlong names, compression ratio, and size and count
budgets), entries beside packets and a top-level packet, one archive staged at a
time with nothing left behind, incomplete batch scans scanned again, and label
matching by archive identity. A live ingest of a folder with six Gutenberg
packets, four LibriVox packets, a failed packet, a ZIP of three EPUBs, a loose
MP3, and a text file imported 14 items (9 checked, 5 in review), listed 4, and
failed none; byte-identical ZIP members joined their existing holdings as
additional sources.
