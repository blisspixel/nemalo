# 0061: recoverable cleanup of split ZIP byte streams

Status: accepted and implemented, 2026-10-20. Stage 2,
[issue 54](https://github.com/blisspixel/nemalo/issues/54).

## Decision

Reuse decision [0057](0057-split-zip-intake.md)'s private joining and installed
reader checks for `.zip.001` byte chunks. Relocate the exact ZIP coverage proof
and its parser-boundary tests from `internal/library` to `internal/staging`.
The shared proof returns bounded byte-coverage evidence only. Library services
still require every member held with verified stored bytes, unchanged hashes of
all original chunks, and the reviewed cleanup plan. True PKZIP disk segments,
RAR and nested material keep their existing retention policy.

Only chunks in the same exact parent directory can move together. A complete
set is one cleanup entry containing every original part in order. Its optional
`archive_proof: "zip-stream/1"` binds this coverage contract to the cleanup
manifest's exact file identities and holdings. The existing synced manifest,
copy, verify, publish, remove and per-entry recovery protocol handles the parts.
Unknown or missing proof identifiers refuse continued source removal for such a
set; restore can still return recorded bytes. Earlier executables reject the new
cleanup entry field when reading the canonical manifest, before a write.
An unreferenced manifest is not a durable intent; a retry before the journaled
start must recheck the complete plan. Identity hashing charges actual reads,
including failed reads and final archive rechecks, to the shared work allowance.

## Purge and partial recovery

A fresh purge must verify the entire held set again through the same joining,
reader, coverage, extraction and held-member services. Missing, changed or
unprovable parts prevent a new whole-set authorization; other independently held
entries can still qualify. No partial set is declared a complete archive.

The purge preview includes `archive_groups`, each with proof identifier
`zip-stream/1` and ordered file indices. Its plan digest binds those groups to the
file paths, hashes, sizes and holdings already in the preview. An apply rechecks
the complete set and persists a schema-2 purge manifest and synced intent before
the first removal. Earlier executables refuse that manifest version.

After one removal, the original archive cannot be reconstructed from remaining
chunks. Resume therefore uses the immutable, journal-hash-bound proof for that
exact original set, only while its identifier is supported. Each remaining part
still needs its own fresh hash, opened identity checks and currently verified
holdings. A changed part stays; an already removed part is reported absent;
recorded completed removals never adopt or remove replacement files. Independently
verified original parts can finish without repeatedly joining a partial archive.
This avoids quadratic rehashing or keeping a second permanent archive copy.

Missing or unsupported group evidence yields `archive_not_accounted`, preserving
remaining bytes for restore. Malformed membership or binding is retained as state
requiring review before any removal. Legacy schema-1 manifests for ordinary
files, ZIPs and 7z sets retain their existing digest and recovery contract. A
schema-1 intent cannot authorize ZIP byte chunks without the new bound evidence.
The initialization and cleanup journal record shapes stay unchanged.

## Bounds and verification

Keep 2 to 1,000 parts, at most 4 GiB joined, the existing per-file and 64 GiB
cleanup hash limits, temporary-space checks, two-minute preparation/probe budget,
16 MiB probe output, member/output/ratio limits, archive deadline and cancellation.
Coverage retains its independent 16 MiB lifetime read budget. Work is sequential;
no new dependency, decoder, executable or network operation is introduced.

Test complete and refused sets, changed inputs, aliases, unsupported bytes and
metadata, missing tools, every per-part cleanup/purge crash window, full and partial
restore, proof-version and membership failures, and unchanged outside/store bytes.
Use synthetic Go fixtures and injected tools/scanners. Separately label native
installed-reader experiments, with generated content outside the checkout.
Require canonical verification, native-cgo race, documentation checks and hosted
Linux/macOS/Windows acceptance before closing the issue.

Synthetic tests exercise every per-part copy/publication interruption, purge
removal before its completion record, stale plans, active cancellation, missing
tools, independent groups, unknown/absent/malformed proofs, changed parts and
holdings, and replacement files after a recorded removal. Existing ZIP coverage
regressions moved with the shared proof; bounded fuzzing passed 313,889 executions.
A separate native Linux experiment with installed 7-Zip 26.04 and generated
content outside the checkout previewed and moved a three-part set, restored all
three parts, then purged its held copies. All parts of a ZIP containing unaccounted
directory bytes stayed unchanged. This is tested reader evidence, not a claim
about all archive variants. Exact hosted acceptance is recorded on the issue.
