# 0043: RAR and 7z members through an installed 7-Zip

Status: accepted and implemented, 2026-10-19. Extends the ZIP ingest of
[decision 0017](0017-folder-and-zip-ingest.md) to RAR and 7z archives.

## Context

Download folders often hold RAR and 7z archives. The pure-Go RAR and 7z
readers checked on 2026-10-11 lacked verifiable checksums or memory bounds
([research record](../RESEARCH.md#version-sensitive-research-record)), and
writing either decoder is out of scope. 7-Zip is the maintained reader for
both formats, is free, runs on Linux, macOS, and Windows, and is often
installed. Its command line can list an archive's members with sizes, CRCs,
attributes, and encryption, and write one member to standard output.

## Decision

`library ingest` extracts the supported members of a RAR or 7z archive when an
installed 7-Zip (`7zz`, `7z`, or `7za`) is found, through the same staging,
accounting, assessment, journal, and provenance (`ARCHIVE!/MEMBER`) as a ZIP.
Without one, the archive is listed as `not_imported` with that reason and
preserved, as before. `doctor` reports it.

- **Snapshot.** The archive, at most 4 GiB, is copied to a private temporary
  folder, checked unchanged after copying, and 7-Zip reads only that copy.
  The folder and the original are never written.
- **Type.** 7-Zip opens whatever format a file's bytes name, so the handler is
  fixed with `-t7z`, `-tRar5`, or `-tRar` from the snapshot's signature, which
  must agree with the extension. A tar, cpio, disk image, or ZIP renamed `.rar`
  or `.7z` is refused and preserved, and only those three parsers ever see an
  archive.
- **Listing.** `7z l -slt -ba` lists every member. The listing must be blocks
  of `Key = Value` lines, at most 16 MiB; any other line, as a member name
  holding a line break would produce, refuses the whole archive. A listing
  that fails, as for a damaged, header-encrypted, or split archive, or a 7-Zip
  without RAR support, refuses it too. Every member is then classified as a
  ZIP member is: directory, link, other special file (from the Unix mode or
  Windows attributes; a hex attribute word is not read as letters), a link
  (also from a `Mode` key or a `Symbolic Link`, `Hard Link`, or `Copy Link`
  key), encrypted, unsafe name, nested archive, unsupported format, too large,
  empty, no stated CRC, a suspicious ratio when a packed size is stated, or a
  name that two members share or that is also a folder of other members,
  which is never extracted, since extraction by name would return them all.
- **Extraction.** Each eligible member is extracted alone with
  `7z e -so -spd -ssc -- SNAPSHOT NAME`: to standard output, with wildcards
  off and case kept, so a name such as `a*b?.epub` selects only itself. Its
  bytes go to a fresh index-prefixed flat staging name, capped at the listed
  size plus one byte, and must have exactly the listed size and CRC-32 and a
  zero exit status; otherwise the member is `failed` and its partial removed.
  No member name is ever a path on disk. A fixed dummy password and no input
  mean 7-Zip never waits for one. Each listing and member has a deadline by
  size, and the whole archive one more: ten minutes and a minute per 16 MiB,
  at most an hour, and at most 1,000 extraction runs. Each run of a solid
  archive decompresses its block from the start, so members past the budget
  fail and the archive is preserved.
- **Cleanup** keeps RAR and 7z archives in their folder; only a ZIP whose
  every member is held moves (decision 0022).

## Consequences

RAR and 7z downloads become importable without a new Go dependency or a
decoder of Nemalo's own. 7-Zip parses untrusted archives as an external
program, as ClamAV and FFmpeg do; its own path handling is never used, since
nothing is extracted to a path. A solid 7z archive is decompressed from the
start of its block for each member, so a large solid archive takes longer.
Multipart sets, nested archives, tar, and compressed streams stay listed and
preserved. Cleanup of RAR and 7z archives is later work.

## Validation

Tests run the test binary as a stand-in 7-Zip that refuses any extraction
without `-so`, `-spd`, `-ssc`, the dummy password, and `--`. A listing of
seventeen members gives each its disposition: a directory, a member named
with wildcard characters extracted by exact name, an unsupported text file,
links and a FIFO in the 7z and RAR attribute forms, an encrypted member, a
parent path, a shared name, a nested ZIP, a listed CRC that differs, a member
served one byte longer, a member that returns nothing, an extreme packed
ratio, and an oversized member; only the one eligible member is staged, with
its exact bytes. A listing line that is not `Key = Value`, a failed listing,
and a missing 7-Zip are refused, and so are a tar named `.rar` and a ZIP named
`.7z` by their signatures. A library test ingests a folder with a RAR and
a 7z and no 7-Zip, and the archives are preserved while the loose book
imports.

Live, on Linux amd64 with 7-Zip 26.04: a 7z made by 7-Zip itself with a
directory, an M4B, a text file, and an EPUB named `sub/a*b?.epub`, a 7z
holding a symbolic link, and a RAR5 archive built byte by byte from the RAR5
format description (which `7z t` reports as valid) were ingested into a new
library. The EPUB and M4B members of both archives were extracted with
matching CRCs and imported; the text files, the directory, and the link were
listed and preserved; and the same EPUB from the RAR and the 7z became one
holding with both sources. A tar renamed `.rar` was refused by its signature.

An independent adversarial review, with the installed 7-Zip, then found five
defects, each now with a regression test: 7-Zip read any format by its bytes,
so a tar or cpio named `.rar` was listed and extracted; links stated by a
`Mode` or link key were extracted as their target text; a hex attribute word
holding `D` made a book look like a folder and silently skipped it; a solid
archive could make thousands of runs each decompress a large block; and a name
that is also a folder selected that folder's members too, which the size check
caught. The review found the parser, name selection with trailing spaces,
leading `@` or `!`, and Unicode forms, password handling, and cleanup sound.
