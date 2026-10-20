# 0057: split ZIP intake through checked private snapshots

Status: accepted and implemented, 2026-10-20. Stage 2 of the
[ordered roadmap](../../ROADMAP.md#order-of-work-to-10), tracked in
[issue 49](https://github.com/blisspixel/nemalo/issues/49). Extends the shared
staging and volume grouping in decisions [0017](0017-folder-and-zip-ingest.md),
[0043](0043-rar-and-7z-through-7-zip.md), and [0056](0056-rar-volume-identity.md).

## Evidence and decision

There are two different layouts. Byte-split `NAME.zip.001`, `.002`, ... are
chunks of an ordinary ZIP stream. PKZIP segments `NAME.z01`, `.z02`, ...,
`NAME.zip` instead carry central-directory disk references, with the `.zip`
terminal segment last. Concatenating those segments does not produce an ordinary
ZIP. Go's existing ZIP reader handles the first layout after joining, not the
second. Keep both layouts distinct from ordinary ZIPs and other volume families.

The contracts were checked against
[PKWARE APPNOTE 6.3.10](https://pkware.cachefly.net/webdocs/casestudies/APPNOTE.TXT)
and the installed reader's tagged
[ZIP properties](https://github.com/ip7z/7zip/blob/26.04/CPP/7zip/Archive/Zip/ZipHandler.cpp)
and [multi-volume input](https://github.com/ip7z/7zip/blob/26.04/CPP/7zip/Archive/Zip/ZipIn.cpp).
Native synthetic two- and three-segment fixtures passed 7-Zip 26.04 on Linux.
Every segment could open the same member table, reporting its own physical size
and opened-volume ordinal. Removing the first segment still left the reported
count unchanged, but produced a missing-volume error and a short total size.
Count alone cannot establish a set. No reference code is incorporated.

- Shared ingest/cleanup grouping recognizes `.zip.NNN` or `.zip.NNNN` streams
  and `.zNN` through `.zNNNN` segments with their actual `.zip` terminal sibling.
  Each starts at one and is contiguous, with 2 to 1,000 volumes, at most 4 GiB
  in total. Case aliases, duplicate numbers, missing parts, or ambiguous families
  are preserved. A plain ZIP without segment siblings remains standalone.
  Old RAR letter rollover also uses `.zNN`: an actual `.rar` first sibling or
  earlier RAR letter versus a `.zip` terminal sibling selects the family.
  Conflicting or absent anchors preserve the ambiguous shared paths.
- Use existing capability-bound regular-file snapshots, unchanged-after-copy
  checks, fixed generated names, cancellation, and one archive at a time.
  All set preparation and container probes share two minutes; all probe output
  shares 16 MiB. Readers never open original sibling paths.
- Join byte chunks through the existing bounded snapshot copier. Require an
  ordinary ZIP signature and an error-free fixed `-tzip` technical listing with
  exactly the joined physical size, no multivolume interpretation, no leading
  stub/offset, tail, warning, or archive comment. The maintained installed reader
  supplies container extent facts absent from Go's public ZIP API, which can
  accept a leading stub or ignored bytes. Then use the existing Go ZIP extractor
  for member classification, limits, bounded copies, and unconditional CRC checks.
- Reuse assessment's bounded standard ZIP opener for intake and cleanup. Intake
  and cleanup allow 16 MiB of directory reads and an 8 GiB compressed-read
  lifetime budget; EPUB inspection retains its existing 2 MiB/1 GiB limits.
  Declared ZIP64 counts are bounded before the standard reader's fixed end-record
  read can cause decoder preallocation; tests check this contract against the
  pinned Go reader and retain bounded ZIP64 support. Go ZIP staging also shares
  the existing whole-archive deadline. Disabling the directory phase never resets
  the compressed-read budget.
- Snapshot true segments as `archive.z01`, `.z02`, ..., `archive.zip`. Require
  the standard split marker at the first segment's start. Probe the complete
  snapshot set through every part with fixed `-tzip`: each must succeed, report
  its exact physical size and ordinal, the same exact count/total size, and an
  identical member table. Require member disk/offset/packed-size evidence to fit
  within the supplied snapshots. Only the four-byte split marker may be a stub;
  ambiguous, contradictory, commented, warning, or error evidence preserves all
  sources before extraction. Reuse exact-name stdout extraction and size/CRC
  checks for eligible members through 7-Zip, never its extraction paths.
- These are container declarations and reader consistency checks, not a global
  cryptographic set identifier. Payload corruption still belongs to each member's
  bounded extraction checks. An extraction failure makes the ingest incomplete;
  independently verified extracted members may still be imported under the
  existing per-member service. Volume-evidence refusal happens before extraction.
  Interrupted publication resumes by the original first-volume/member label.
  An earlier standalone terminal `.zip` member also resumes under its original
  label, before any other member; regrouping never rewrites historical provenance.
- Every split ZIP source stays in cleanup. Exact listing sizes do not establish
  full byte accounting for deletion. No new module, decoder, executable, network
  operation, cleanup permission, or archive-wide publication transaction is added.
  Existing installed 7-Zip discovery is required for both layouts; it is not
  installed or upgraded. Insufficient reader capabilities preserve the sources.

## Limits and validation

The later [decision 0061](0061-split-zip-stream-cleanup.md) adds recoverable cleanup
and version-bound purge of completely accounted byte chunks. The retention rule
above remains in force for true disk segments and unproven sets.

Self-extracting, spanned removable-media, header-encrypted, ambiguous, or otherwise
unproven variants stay for review. In particular, raw archive comments cannot
authorize or supply reader header evidence. Files and unsupported members remain
preserved, with assessment, antivirus coverage, and completeness kept distinct.

Automated verification uses Go-generated fixtures and injected readers/scanners,
never a network or an installed executable. Tests cover both layouts, exact
snapshot copies, missing/aliased/overlapping names, RAR rollover ambiguity,
contradictory size/count/ordinal/member evidence, archive comments and diagnostics,
out-of-range member extents, missing tools, cancellation and aggregate probe
output, directory limits, and count-driven ZIP64 allocation. Library tests cover
preview/apply, repeat ingest, unchanged sources and cleanup refusal, original
member labels, lost staging after an import intent, legacy terminal-part recovery,
and refusal before other imports when the pending member is absent or damaged.
Member CRC failure remains incomplete while independently verified members import.

Native validation on Linux amd64 with installed 7-Zip 26.04 used only synthetic
content outside the checkout. Five byte chunks and three PKZIP segments extracted
two PDFs with their exact expected SHA-256 bytes. Preview, apply, repeat ingest,
and cleanup preservation passed. Corrupting a middle segment produced one failed
member, one independently imported member, and an incomplete result, without
changing damaged source bytes. The old standalone terminal ZIP path also reproduced
the recovery case: a wholly contained second member could extract independently.
This validates the tested reader paths, not every ZIP variant or installed reader
on every platform. Canonical local and actual hosted verification are recorded in
issue 49's acceptance receipt before closure.
