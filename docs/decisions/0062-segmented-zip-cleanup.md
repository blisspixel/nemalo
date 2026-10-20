# 0062: complete disk-segmented ZIP cleanup

Status: accepted and implemented, 2026-10-20. Acceptance evidence is tracked in
[issue 55](https://github.com/blisspixel/nemalo/issues/55), stage 2.

## Decision

Extend decision [0057](0057-split-zip-intake.md)'s exact private segment snapshots,
installed-reader evidence and bounded extraction. Before cleanup, establish
physical byte coverage independently of listing sizes and extraction success.
Library services retain the held-member and write policy.

The primary [PKWARE APPNOTE 6.3.10](https://pkware.cachefly.net/webdocs/casestudies/APPNOTE.TXT)
was checked on 2026-10-20, particularly sections 4.4 and 8.5. Local and central
headers must fit one segment, while a directory and payload can span segments.
Disk-relative offsets, the last disk, directory start disk and counts must agree
with the supplied bytes. Only the initial four-byte split marker is excluded
from ordinary framing. Unknown records, comments, gaps, tails, ZIP64 and unsupported
metadata retain the sources.

Use a read-only view of the private snapshots, with at most one opened part at a
time. Prevalidate the original disk references, counts, complete header boundaries
and end record. Overlay only these validated fixed-width disk/count/offset fields
to expose global offsets without the initial marker. No payload or other metadata
is modified or omitted. Reuse the maintained Go ZIP parser and the shared exact
ordinary-ZIP coverage check on this view. All original bytes except the verified
marker remain represented exactly once. Raw preflight and view reads share one
16 MiB lifetime allowance, including failed work, and the existing preparation
deadline. No additional joined archive is written.

Bind the installed reader's complete member table to the raw central records,
including names, sizes, CRCs, disk offsets and directory types. Extract from the
same checked snapshots with that validated table, using the existing exact-name
stdout path and per-member size/CRC checks. Keep the existing 2 to 1,000 parts,
4 GiB source, member/output/ratio, hash, space, deadline and cancellation limits.
No decoder, module, executable or network dependency is added.

The reader represents a directory without its final slash and can omit its CRC.
Permit only that single-slash name representation with the matching raw directory
type, unique membership, zero size/packed size and zero raw CRC. File CRC evidence
remains required. No other name normalization or inferred payload is introduced.

## Recovery

Use decision [0061](0061-split-zip-stream-cleanup.md)'s whole-set cleanup and
schema-2 grouped purge protocol with the distinct proof identifier
`zip-segments/1`. Require one exact parent and the contiguous segment family,
including its terminal `.zip`. The canonical cleanup manifest binds ordered part
identities, hashes and verified holdings. A fresh purge rechecks the entire held
set. Partial recovery uses only the supported, immutable, intent-bound original
set proof, fresh remaining-part hashes and current verified holdings.

Only that validated group can authorize the terminal `.zip` as a segment; ordinary
ZIP purge still requires its own current coverage proof. Missing or unsupported
group evidence preserves all remaining parts, including the terminal segment.
Malformed bindings stop before any removal. Earlier builds reject source removal
with the new cleanup proof and cannot silently reinterpret its purge group as
`zip-stream/1`. Restore continues to return recorded bytes without replacement.
No journal record shape, ordinary-file recovery or initialization protocol changes.

## Acceptance

Use synthetic Go fixtures and injected readers/scanners for cross-segment payloads,
directories spanning segments, record boundaries, corrupt disk/count/offset fields,
unaccounted bytes, unknown metadata, extraction CRC failures, budgets, cancellation,
stale plans and every per-part recovery transition. Test proof/version/membership
failures and terminal-segment replacement canaries. Separately validate generated
archives outside the checkout with the installed reader. Require canonical local
verification, native-cgo race, documentation checks and actual hosted checks on all
three operating systems before recording implementation acceptance.

Synthetic tests cover cross-disk stored/deflated payloads and descriptors, directory
spanning, raw header/count/offset disagreements, gaps, unsupported metadata,
reader-table disagreement, cancellation, budgets and private file identity changes.
The same cleanup/restore/purge recovery assertions now run against streams and
segments, including every per-part interruption, stale plans, missing tools,
unknown/malformed evidence, changed holdings and replacement canaries. A separate
regression confirms stream evidence cannot authorize a segment group's terminal
`.zip`. Interrupted partial restore preserves conflicting replacements and returns
all original parts after the conflict is removed. Framing-focused bounded fuzzing
passed 333,697 executions. The canonical verifier and native-cgo race suite passed
locally with 85.9% aggregate coverage and no known Go vulnerabilities. Synchronous
fake-archiver children retain race instrumentation and reports while removing the
documented per-process exit sleep; the expanded library race suite took 28 seconds.
Exact hosted verification is recorded on the issue before closure.

Separate native Linux experiments with installed 7-Zip 26.04 and generated content
outside the checkout passed complete cleanup, byte-exact restore and grouped purge
for three-part payload-crossing and directory-spanning layouts, including an empty
directory and a member beginning on a later disk. All parts of the layout with
hidden directory bytes stayed unchanged. This is limited installed-reader evidence,
not broad ZIP conformance or semantic-completeness certification.
