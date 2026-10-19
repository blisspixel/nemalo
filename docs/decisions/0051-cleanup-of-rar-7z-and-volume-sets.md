# 0051: cleanup of RAR, 7z, and volume sets

Status: accepted and implemented, 2026-10-19. Stage 2 of the
[order of work to 1.0](../../ROADMAP.md#order-of-work-to-10). Extends source
cleanup ([decision 0022](0022-recoverable-source-cleanup.md)) to the archives
ingest reads through 7-Zip ([decision 0043](0043-rar-and-7z-through-7-zip.md))
and their volume sets ([decision 0046](0046-archive-volume-sets.md)).

## Context

Ingest imported RAR and 7z members and read split 7z and RAR volume sets, but
cleanup moved only ZIPs, so a folder of RAR downloads could never be cleared
after import. Moving a source out of the folder is only safe when the archive
holds nothing beyond the members that were checked. A ZIP is checked for data
before its first entry and for a comment. 7-Zip 26.04, checked 2026-10-19 on
real files, lists an archive with bytes after its end with a `WARNINGS` heading
and a `Tail Size` key, and a RAR volume set with `Total Physical Size` equal to
the sum of its volumes.

## Decision

- Cleanup plans a RAR or 7z as it plans a ZIP: the archive's bytes are hashed,
  its members are extracted through the same staging code as ingest, every
  member must be a directory or bytes the library holds with verified stored
  bytes, at least one book must be held, and the archive must be unchanged
  after the check.
- A RAR or 7z must also be whole as 7-Zip lists it: its archive block, read
  line by line to the member listing, has only keys, each once, no `Comment`,
  `Offset`, or `Tail Size`, and a physical size equal to the bytes read. Any
  other line, such as a `WARNINGS` heading, refuses it.
- A complete volume set is one entry through its first volume, grouped by the
  same rule ingest uses, now one shared function. It moves only as a whole,
  with every volume among the entry's files: a split 7z must be whole across
  its joined volumes, and a RAR set's `Total Physical Size` must equal their
  sum. When the set stays, each other volume is its own kept entry naming the
  set. A set with a missing volume keeps every volume.
- Without 7-Zip a RAR or 7z stays, with that reason.
- Restore and purge already handle entries of several files, so a moved set is
  restored and purged volume by volume under the existing plans and journals.

## Consequences

A folder of RAR and 7z downloads can be cleared after import with the same
recovery guarantees as ZIPs. An archive with an unsupported member, a comment,
or extra bytes stays. Old-style RAR volume names (`.r00`) and split ZIPs are not
yet read by ingest, so cleanup keeps them too.

## Validation

Offline tests through a stand-in 7-Zip (`internal/staging/stagingtest`): a RAR
with a tail, a comment, or a physical size one byte short stays; one with an
unheld member stays; a whole one with only a held book moves; without 7-Zip a
7z stays; a split 7z set moves as one entry with both volumes, is restored
volume by volume, and a set with a missing volume keeps both. The block parser
is tested on blocks as 7-Zip 26.04 prints them.

Live with 7-Zip 26.04 on 2026-10-19: after ingest, `books.7z` (an EPUB and an
M4B) and a seven-volume `split.7z` set moved, while the same archive with five
bytes appended stayed; the purge preview listed the archive and all seven
volumes. A synthetic RAR and a four-volume split set each holding a member the
library cannot import stayed.
