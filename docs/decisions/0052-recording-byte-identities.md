# 0052: recording byte identities across source items

Status: accepted and implemented, 2026-10-20. An increment of stage 1 in the
[order of work to 1.0](../../ROADMAP.md#order-of-work-to-10), tracked in
[issue 41](https://github.com/blisspixel/nemalo/issues/41). Extends the manifests
of [decision 0020](0020-recording-manifest.md), independently of the LibriVox
narration declarations of [decision 0044](0044-narration-identity.md).

## Context

Holdings already deduplicate exact assets by SHA-256 and retain their sources.
A declared recording remains a source item and format, so two Archive items
offering identical tracks appeared unrelated unless both named one LibriVox
project. Titles, narrators' names, and durations cannot establish that two
recordings are the same reading.

## Decision

`library recordings` adds `byte_identities` to its shared service result. CLI
JSON and the read-only MCP `library_recordings` tool return that result. The
CLI and TUI report recordings sharing one byte identity together.

- Each complete, unambiguous declared recording contributes its asset IDs in
  position order. Every repeated position is retained, including two positions
  with identical bytes. Missing positions, conflicting assets at one position,
  and conflicting declared counts exclude the recording.
- Each result has schema `recording-bytes/1`, an `id`, `ordered_assets`, and
  `recordings`: the existing source ID, source format, kind, and completeness
  references. Singleton identities are returned too. Groups sort by identity;
  references sort by source, format, and kind. `--source` narrows the references
  without changing the digest of their asset sequence.
- The ID is `recording-sha256:` followed by the lowercase SHA-256 of UTF-8 text:
  `recording-bytes/1`, LF, then each full lowercase `sha256:HEX` asset ID
  followed by LF. No spaces, BOM, or other fields participate. Asset IDs have
  fixed validated syntax. The schema is domain separation; changed construction
  requires a new schema. For one asset whose 64 hex digits are all zero, the
  digest is `8825539a8913204d2467c41537d40b86c04835103d46f5513fbbcb1f1aef7b31`.
- Identity is evidence from the holdings journal. Listing performs no new
  integrity check. `--source` retains its separate per-track hash verification
  and probing, including reports of changed or missing bytes; it does not
  rewrite historical byte identities. Review status stays review.
- Byte identity never replaces source or narration declarations. Even
  contradictory LibriVox projects remain separate declarations when their
  recorded bytes match. No narrator or reading is inferred from byte identity.

## Consequences

Identical ordered assets from different source items relate without LibriVox
metadata or additional stored copies. There is no new on-disk state or migration;
existing holdings supply all evidence. `recording-manifest/1` is unchanged.
No extra network access, scanner invocation, or write is introduced.

Different encodings, tags, track divisions, and uncertain local folders do not
share this identity. Relating them requires explicit recording evidence in later
stage-1 work. No acoustic matching or title matching is implemented. An M4B is
still a single-file recording by the existing convention; an arbitrary M4A or
loose MP3 is not newly declared a whole audiobook.

## Validation

Synthetic library tests cover two source items without narration metadata,
asset deduplication, source filtering, reordered and changed tracks, repeated
positions, different counts, missing positions, conflicting claims and counts,
reimported provenance, stable ordering, and conflicting LibriVox declarations.
A fixed digest vector pins the construction. Missing stored bytes leave
journal-only identity unchanged and are reported when probing is requested.
CLI JSON and in-memory MCP tests exercise the exposed result; the MCP test also
rejects an outside-scope library and injects a scanner that fails if invoked.
Presentation tests check source-label escaping and shared-group output.
