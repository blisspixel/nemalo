# 0063: cleanup of completely accounted archive trees

Status: accepted and implemented, 2026-10-20. Acceptance evidence tracked in
[issue 56](https://github.com/blisspixel/nemalo/issues/56), stage 2.

## Decision

Reuse [0058](0058-bounded-nested-intake.md)'s `ExtractTree` with explicitly requested
coverage. Default intake keeps its existing behavior. Every traversed ZIP uses the
shared exact framing proof on the same opened bytes used for extraction; segment
sets retain [0062](0062-segmented-zip-cleanup.md)'s original disk checks and view.
Every 7z reuses [0051](0051-cleanup-of-7z-and-split-sets.md)'s complete header/packed
accounting through the shared bounded runner. RAR remains unaccounted and cannot
authorize cleanup at any depth. Unknown metadata, notes, unsupported or ambiguous
children, corrupt copies and incomplete traversal preserve the outer source/set.

All nodes share one 16 MiB proof-read allowance, in addition to the existing
four-level, 10,000-member, 1 GiB attempted-output, 8 GiB reader-work, 16 MiB
diagnostic and 1,000-process limits and outer deadline. Failed work stays charged.
Proof reads also consume the reader-work allowance. Container probes use that
same process/diagnostic runner. No decoder, dependency, executable, network call
or alternative import/transfer policy is added. Ordinary outer ZIPs remain Go-only;
nested ZIPs retain their maintained-reader extent check.

Library services require the whole traversal covered, every non-directory leaf
held with currently verified stored bytes, at least one held book and final
unchanged hashes for every original source part. Nested container markers confer
no permission by themselves. A refused member is identified by its exact chain
using parse-equivalent display escaping. A nested move exposes `archive_leaves`,
binding each exact member chain to its size, SHA-256 and holding ID in the canonical
manifest and reviewed digest. The outer source/set binds all encoded container
bytes. All parts of a nested set must share one exact parent.

Bind every root snapshot copy to the plan's expected source size and SHA-256.
Covered ordinary ZIPs use the same private snapshot mechanism without requiring
7-Zip for the outer container. Parsing, coverage and extraction then read that
exact private copy. Hashing the copy's actual bytes shares the application's
64 GiB source-hash allowance, including failed reads. This prevents a source
changed to different bytes during extraction and changed back before the final
rehash from authorizing cleanup of unchecked originals. Nested children already
come from checked private member copies. Default intake remains unchanged.

## Recovery and compatibility

Use `archive-tree/1` only for a complete nested tree. Existing non-nested proof
identifiers and legacy file/7z plans keep their meaning. A nested move reuses the
synced cleanup manifest, intent, verified copies, exclusive publication, removal
and per-entry journal protocol. Recovery validates supported proof, source layout
and exact leaf/holding bindings before source writes. Restore returns recorded
source bytes without replacing other files.

Extend [0061](0061-split-zip-stream-cleanup.md)'s schema-2 grouped purge to a nested
single file or supported ZIP/7z outer set. Fresh authorization repeats the complete
tree check on all held sources and current holdings, and binds proof, ordered
file indices and a `leaf_digest` of the canonical leaf bindings into the purge
manifest, reviewed digest and synced intent. This independently binds the leaf
evidence to that purge's original authorization, as well as the cleanup manifest.
Partial recovery uses only that supported immutable original-tree evidence plus
fresh remaining-part hashes and current verified holdings. It never reconstructs
a tree from a partially purged set. Only a validated supported group can authorize
the terminal ZIP segment. Missing or unknown evidence retains remaining bytes;
malformed bindings stop before any removal. Journaled removals never adopt
replacements.

Earlier executables reject unfamiliar canonical leaf fields or unsupported proof
and group shapes before removal. Keep their manifests readable where possible,
otherwise retain them for this version or newer. Initialization and operation
journal record shapes remain unchanged. No release or later roadmap stage is
part of this increment.

## Acceptance

Test mixed ZIP/7z trees and supported outer sets with synthetic Go fixtures and
injected readers/scanners. Cover every container/leaf outcome, exact chains and
literal separators, unknown bytes/metadata, RAR and nested sets, shared budgets
including failed attempts, unavailable tools, cancellation, stale plans and changed
holdings. Exercise cleanup/restore/purge interruption boundaries, unfamiliar and
malformed proof/leaf/group bindings, independent groups and replacement canaries.
Separately validate generated trees outside the checkout with installed 7-Zip.
Require canonical local verification, native-cgo race, docs checks and actual
hosted Linux/macOS/Windows jobs. Post and read back an acceptance receipt while the
issue is open before closing it.

Synthetic regressions exercise the existing interruption and recovery assertions
against nested ZIP chunks, true ZIP segments and split 7z sets, as well as single
ZIP/7z trees. They cover independent groups, malformed/unknown leaf/proof bindings,
changes to cleanup leaves beneath a pending purge intent, unsupported cleanup
resume, safe restore with an unknown proof and exact-chain refusal/review output.
Shared proof, subprocess and diagnostic tests establish that child boundaries do
not reset allowances. Skipped nested sets invalidate coverage.
An archive changed to other bytes for extraction and changed back before its final
rehash failed a regression before snapshot identity binding was added. Same-size
different-hash cases now cover all five outer layouts, and failed copy-hash work
remains charged. Default intake retains its previous snapshot behavior.

Native Linux validation with installed 7-Zip 26.04 and generated content outside
the checkout exercised mixed ZIP/7z children, directories and two held leaves in
all five outer layouts: ZIP, 7z, ZIP chunks, true ZIP segments and split 7z.
Cleanup, byte-exact restore and grouped purge passed, while a child with hidden
directory bytes preserved the outer archive. An actual earlier executable from
commit `43f5ecd` refused the unfamiliar manifest, retained the original held bytes,
and the current source then restored and purged them. These are limited reader
and recovery experiments, not broad archive conformance or safety certification.
Exact local and hosted acceptance is recorded on the issue before closure.
