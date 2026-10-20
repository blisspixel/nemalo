# 0065: versioned coverage for every 7z cleanup

Status: accepted, 2026-10-20. Implementation and verification in progress in
[issue 58](https://github.com/blisspixel/nemalo/issues/58), roadmap stage 2.
Extends [decision 0051](0051-cleanup-of-7z-and-split-sets.md) through the existing
[tree proof and grouped recovery](0063-complete-archive-tree-cleanup.md).

## Evidence

A flat 7z or split 7z cleanup records member holdings but no versioned coverage
proof. Earlier cleanup could accept matching physical size without accounting
for all encoded bytes. Fresh purge trusts those historical holdings, and a
pending schema-1 purge intent can remove every part without a fresh archive
check. Cleanup resume likewise accepts that unversioned authority.

Synthetic old-manifest regressions reproduce cleanup resume, fresh purge and
pending purge for one file and a three-part set. Their canonical receipts bind
all original bytes, including generated unheld material. With the reader adapter
explicitly unavailable, the pending purges remove those synthetic copies.
Current member hashes cannot establish coverage of an older encoded source.

## Decision

Record `archive-tree/1` and exact `archive_leaves` for every new 7z cleanup,
including a standalone root with only direct book leaves and a flat split set.
This reuses that proof's existing ZIP/7z root and one-to-four-component leaf
contract; it does not change the accepted framing subset or create another
policy. Require one exact parent for all parts and the existing shared source,
proof, reader, output, process and deadline budgets.

Fresh purge repeats the complete check against current verified holdings,
then binds the proof, exact indices and leaf digest to the existing schema-2
manifest before any removal. Partial recovery validates that immutable supported
proof and each remaining source identity and current holding. Never reconstruct
a partly purged set, adopt replacements or downgrade to unversioned authority.

Treat every 7z-named archive entry and recognized 7z chunk as requiring a
supported proof. Historical unversioned entries stop cleanup resume before
writes; fresh and pending purge retain all files of the entry for restore.
Do not rewrite an old manifest or infer its coverage from recorded member hashes.
A user can stop an interrupted operation, restore remaining original bytes,
and preview a new cleanup under the current complete accounting. Restore keeps
its recorded-byte, no-replacement contract and needs no installed reader.

Old executables refuse new canonical leaf fields. Use this source version or
newer for the recorded cleanup and grouped purge recovery. Unknown proofs retain
bytes; malformed bindings stop for review. RAR cleanup remains disabled under
[decision 0064](0064-bounded-rar-framing-accounting.md).

## Verification

Use synthetic Go fixtures and injected readers/scanners. Preserve old single
and multipart bytes through cleanup refusal, fresh/pending purge and byte-exact
restore. Exercise valid new flat archives and sets through cleanup/restore/purge,
every crash boundary, unavailable readers, changed source or holdings, independent
groups and malformed/unsupported evidence. Run generated native experiments
outside the checkout and verify compatibility with an actual earlier executable.
Require canonical local verification, native-cgo race, docs checks and all six
hosted jobs on the final commit. This decision alone establishes no acceptance,
release or completion of the remaining RAR work.
