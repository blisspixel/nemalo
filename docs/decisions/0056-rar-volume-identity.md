# 0056: old-style RAR sets and checked volume roles

Status: accepted and implemented, 2026-10-20. Stage 2 of the
[ordered roadmap](../../ROADMAP.md#order-of-work-to-10), tracked in
[issue 48](https://github.com/blisspixel/nemalo/issues/48). Extends
[decision 0046](0046-archive-volume-sets.md); the RAR cleanup refusal in
[decision 0051](0051-cleanup-of-7z-and-split-sets.md) remains.

## Evidence and decision

A synthetic, stored RAR4 set was tested with installed 7-Zip 26.04 on Linux.
Replacing its second volume with an unrelated standalone RAR still produced a
successful listing with `Multivolume = +` and `Volumes = 2`. One member remained
split and the unrelated member appeared in the listing. Names and counts alone
do not establish a usable volume set.

Use the maintained installed 7-Zip reader, not a new RAR parser or dependency.
Later [decision 0064](0064-bounded-rar-framing-accounting.md) permits a separate,
bounded Go framing accountant beside that reader. It does not replace the
decoder or enable RAR cleanup before its own verification and integration gates.
The format and naming evidence was checked against the
[RAR format description](https://www.rarlab.com/technote.htm) and 7-Zip 26.04's
[RAR reader properties](https://github.com/ip7z/7zip/blob/26.04/CPP/7zip/Archive/Rar/RarHandler.cpp)
and [volume naming](https://github.com/ip7z/7zip/blob/26.04/CPP/7zip/Archive/Rar/RarVol.h).
The [RAR5 reader](https://github.com/ip7z/7zip/blob/26.04/CPP/7zip/Archive/Rar/Rar5Handler.cpp)
also explains its isolated split-continuation diagnostic.
These are reference contracts, not code incorporated into Nemalo. Do not upgrade
the installed executable. Insufficient reader evidence preserves the set.
Decision [0077](0077-old-rar-isolated-probe-names.md) adds only exact private
old-name fallback diagnostics for isolated probes, retaining every other
identity, continuity and clean-assembly requirement and no cleanup authority.

- Shared ingest/cleanup grouping recognizes an actual `NAME.rar` only when
  siblings `NAME.r00`, `.r01`, through `.r99`, `.s00`, through `.z99` are present.
  This bounded old naming family is separate from `NAME.partN.rar` and split
  7z. Case aliases, duplicate ordinals, overlapping families, and gaps preserve
  every involved file. A plain RAR with no old continuation stays standalone.
- Copy each regular volume into a separate private snapshot directory first.
  Fix its handler from its signature and require all volumes to agree. List
  each snapshot in isolation. Require its exact physical size, multivolume
  status, zero-based ordinal, and one visible volume. RAR4 must also declare
  the correct first-volume and old/new naming flags. Missing RAR4 ordinals are
  uncertainty, including older archives without end-volume numbering.
- An isolated nonterminal volume may exit with code 2 for the exact generated
  next name reported as missing in its archive block. RAR5 also reports exactly
  `ERRORS:` / `Headers Error` for a continuation's isolated `Split Before`
  member. That diagnostic is allowed only after the first volume, with matching
  split continuity and a subsequent error-free assembled listing. This is an
  identity probe, not successful extraction or validation. Reject other error,
  warning, comment, offset, tail, contradictory, or ambiguous header evidence.
  All identity probes together have two minutes and 16 MiB of listing output.
- After every probe passes, assemble only those private snapshots under fixed
  generated names. Require a successful full-set listing, the exact count and
  total physical size, and no unresolved split members before extracting any
  member. Existing exact-name stdout extraction, member size/CRC checks, and
  staging/runtime limits still apply. A set remains at most 1,000 volumes and
  4 GiB; the old letter naming range ends earlier at 901 volumes.
- Every boundary must have a split member with the same name and declared size
  on both sides, with compatible before/after flags. Missing, contradictory, or
  unlinked boundaries preserve the entire set, including otherwise valid sets
  whose members all end at volume boundaries. Ordinals alone cannot distinguish
  such a set from an accidental mix of different archives.
- These roles and ordinals are declared container structure, not a global
  cryptographic set identifier. RAR4 has no such identifier here. Inconclusive
  or incompatible structure is refused; member checks still bind extracted
  bytes. Neither a successful list nor a CRC establishes semantic completeness,
  rights, scanner coverage, or safety.
- Sources are never renamed or removed. Original first-volume/member labels
  remain provenance. Ingest still uses Import and its recovery/idempotence
  behavior. Cleanup uses the same grouping but never moves any RAR or RAR set;
  unlisted service/unknown blocks remain unaccounted for.

## Validation

Automated tests use injected Go archivers and synthetic files, never an installed
tool, network, personal content, or scanner. They cover old letter rollover,
separate numbering families, missing/duplicate/overlapping volumes, per-volume
format/role/ordinal/size and naming evidence, unrelated members with the correct
ordinal, unlinked or malformed boundaries, the RAR5 probe diagnostic without
masking assembled errors, shared output/byte/count budgets, cancellation, and
two- and three-volume sets. Library tests cover preview/apply, repeated ingest,
original member provenance, retained unsupported members, unchanged source
bytes, cleanup refusal, and recovery after a member's import intent when its
temporary staging copy has disappeared.

Native validation, Linux amd64 with 7-Zip 26.04 on 2026-10-20, used only synthetic
content outside the checkout. An old-style stored RAR4 set extracted two PDFs,
one split across volumes, with the expected SHA-256 bytes. New-style RAR4 with
mixed original number padding also passed. An unrelated standalone continuation
was refused on role evidence, and an unrelated multivolume continuation with
the correct ordinal was refused on split continuity, both before extraction.
Preview, apply, and repeated ingest succeeded; cleanup kept both source volumes.
The existing synthetic RAR5 set also extracted its EPUB and M4B with the expected
SHA-256 bytes after the isolated-continuation diagnostic was handled. This proves
the tested reader path, not every RAR variant or architecture. Canonical local
and actual hosted verification are recorded in the issue's acceptance receipt.
