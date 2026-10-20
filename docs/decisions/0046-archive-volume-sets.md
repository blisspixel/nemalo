# 0046: RAR and 7z volume sets

Status: accepted and implemented, 2026-10-19. Extends RAR and 7z ingest
([decision 0043](0043-rar-and-7z-through-7-zip.md)) to multi-part sets, for
milestone 2.

The RAR count-only gate below is superseded by
[decision 0056](0056-rar-volume-identity.md): old-style names, isolated
volume role/ordinal checks, and cross-volume split continuity. Split 7z
accounting remains unchanged.

## Context

Large downloads are often split into volumes: `NAME.7z.001`, `.002`, and so
on, a byte split of one 7z archive, or `NAME.part1.rar`, `.part2.rar`, a set of
RAR volumes read through the first. Read one volume at a time, such a set
fails or, worse, could be read in part. The roadmap requires multipart
relationships to be resolved by container identity, with unrelated volumes
left untouched and any unaccounted material preserved.

## Decision

`library ingest` groups the volumes of a folder by name: a set is every
`NAME.7z.NNN` or every `NAME.partN.rar` of one name in one folder.

- A set numbered from 1 without gaps, of two or more volumes, is one archive.
  Its first volume names its members' provenance (`NAME.7z.001!/MEMBER`), and
  every other volume is reported as `archive_volume`, read through the first.
  A set with a gap, or a lone `.7z.001`, is preserved, each volume reported
  as part of a set with a missing volume. A lone `NAME.part1.rar` is read as
  an ordinary RAR, which fails and is preserved if it is in fact a volume.
- A split 7z is joined in volume order into one private snapshot, each
  volume checked unchanged after it is copied, and read as one 7z by its
  signature, as decision 0043 reads any 7z, only when 7-Zip's archive block
  gives a physical size equal to the joined total, so a stray extra volume
  or a tail left from another split is never reported as read.
- A RAR set is copied volume by volume into a private snapshot under its own
  volume numbers, so 7-Zip finds each next volume by name. It is read only
  when 7-Zip's own archive block, listed without `-ba`, says
  `Multivolume = +` and names exactly as many volumes as were found; a set
  7-Zip counts differently, as a stray extra volume or a missing last one
  would make it, is refused and preserved. An archive comment is printed raw
  inside that block, so nothing at or after a `Comment` key is read, and a key
  that appears twice refuses the block.
- A set is at most 4 GiB and 1,000 volumes. Every member is then classified
  and extracted as in decision 0043, by exact name to standard output, with
  its listed size and CRC.

## Consequences

Split downloads become importable. Missing or incompatible volume evidence
refuses the set before member extraction. A later member extraction failure
makes ingest incomplete, while independently verified extracted members may
still be imported through the existing per-member service; publication is not
an archive-wide transaction. Old-style RAR names now follow
[decision 0056](0056-rar-volume-identity.md), and split ZIPs follow
[decision 0057](0057-split-zip-intake.md). Cleanup of 7z and split 7z follows
[decision 0051](0051-cleanup-of-7z-and-split-sets.md); all RAR and split ZIP
sources remain preserved.

## Validation

A stand-in 7-Zip tests a RAR set read when its header names two volumes and
refused when it names three, and sets with a gap, a volume of another set, or
one volume refused; a split 7z whose signature spans two volumes is joined
and read; and `VolumeOf` names sets and refuses plain archive names. A library
test ingests a split 7z and a RAR set with a gap with 7-Zip unavailable: the
first set's extra volume is a part, and both sets are preserved.

Live, on Linux amd64 with 7-Zip 26.04: a 3,356,279-byte 7z made by 7-Zip in
four 1 MiB volumes was ingested through its first volume, its EPUB and M4B
imported and a random file preserved; a two-volume RAR5 set built from the
RAR5 format description, with an EPUB split across both volumes, which
`7z t` reports as valid, imported both its books; a set with its second
volume missing was preserved; and a RAR set with a third, unrelated file
named as its next volume was refused because 7-Zip counted two volumes.

An independent adversarial review with the installed 7-Zip then found three
defects, each now fixed with a test: an archive comment holding
`Volumes = 3` overrode 7-Zip's count, so an unrelated archive named as a
third volume was reported as read; an extra `.7z.NNN` volume was reported as
read while 7-Zip ignored its bytes; and a set in a folder whose path holds
`!/` lost its first volume from the report. A volume of a set now says its
archive's outcome is the set's.
