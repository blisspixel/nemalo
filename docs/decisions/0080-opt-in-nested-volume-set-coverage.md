# 0080: opt-in nested volume intake and read-only coverage

Status: accepted, 2026-10-23 UTC. Implementation and verification tracked in
[issue 57](https://github.com/blisspixel/nemalo/issues/57), roadmap stage 2.
This decision grants no new cleanup or purge authority.

## Prerequisite

Decision 0079's old-name root writer passed canonical local verification, native
cgo race, seventeen generated native lifecycles, separate preceding/current
recovery checks and [all six hosted CI jobs](https://github.com/blisspixel/nemalo/actions/runs/39874514249)
at `3929f20b2e9ac48c67151a6b64c0f1d9aca9d348`. Go and Actions CodeQL passed.
Its [acceptance receipt](https://github.com/blisspixel/nemalo/issues/57#issuecomment-6354289672)
was posted and read back exactly while issue 57 remained open, before this
decision or nested-volume implementation. Windows reparse tests passed without
skips and cgo-free native smokes passed. Windows library race took 574.754 seconds
under the unchanged 600-second package timeout; preserve that gate and measure
later test growth.

## Decision

Add an internal `NestedVolumeSets` opt-in for `ExtractTree`. Defaults and all
application callers keep it false during this increment. Direct extraction
without tree traversal does not gain nested behavior. With the opt-in, intake
may read a complete nested group through the existing set adapter. Requested
`Coverage` additionally requires that adapter's complete proof and every existing
format capability: RAR4 needs `RAR4Coverage` and `RAR4SetCoverage`, RAR5 needs
`RAR5SetCoverage`, and old names additionally need `OldRARSetCoverage`.
`NestedRARCoverage` retains its separate standalone-child meaning.

Cover only the currently supported root set layouts within their existing bounds:
modern RAR4/RAR5, closed old-name RAR4/RAR5, split 7z, ZIP byte chunks and true
ZIP disk segments. Reuse their snapshot, format, role, ordinal, continuity,
assembly, raw framing and output-integrity checks without expanding a parser or
decoder. Incomplete, ambiguous, unsupported and uncertain groups stay preserved.
No runtime, dependency, executable, network operation or public option is added.

Deliberately relocate the pure volume-family grouping used by library ingest and
cleanup into `internal/staging`, with a thin library projection preserving
existing callers. Keep grouping order, exact parents, ordinal/alias rules,
explicit anchors, old RAR/ZIP letter overlap and every earlier refusal unchanged.
Do not create a second policy engine for archive members. Group the complete
member table; never resolve a safe-looking subset that silently discards a peer.

Every prospective part must pass the existing member checks for safe exact name,
regular type, supported encoding, encryption, CRC, size, ratio and duplicate or
folder-selection ambiguity. Extract through existing checked copy functions into
flat indexed staging names. Read a group only after every expected part is
uniquely extracted with its size and CRC verified. Original archive member names
remain provenance and grouping data, never filesystem paths to create or open.

Assemble each group in a fresh checked private directory using fixed generated
names derived only from the validated layout and ordinal. Move only its own flat
staged copies into that directory. Pass those copies to the existing `ExtractSet`
adapter, which still checks isolated physical volumes before exposing siblings
and requires a clean assembled table. Handle arbitrary original member order,
multiple independent groups and an original first volume appearing last. A failed
part prevents reading the entire group; never import a continuation or reinterpret
its first part as a standalone archive. Remove private staged parts after use or
failure, and retain every original source.

Report every original part separately, including those represented as group
bookkeeping. A leaf chain uses the exact original first-member name as its group
component, followed by the existing child chain. Generated private names never
enter source provenance. Requested but failed groups are explicitly incomplete;
do not silently skip a continuation, a missing reader or a coverage failure.

Measure completely covered child sets separately as JSON-omitted
`CoveredVolumeSets`. Count only requested, completed physical evidence, never
names, intake success or caller declarations. Modern/old root-set version fields
remain root-only; standalone facts keep their existing meanings. Counts can
survive an independent output failure and grant no write permission. Outer
coverage remains the conjunction of its own evidence and every visited child;
a covered child cannot repair an unsupported ancestor.

Share the existing four-level depth, member, expanded-byte, per-part, snapshot,
source/read/proof, process, diagnostic, cancellation and deadline limits across
all groups and failed work. Do not reset a budget at a child. Part copies already
derive from checked outer snapshots; original expected hashes still bind only
the application's selected roots. Charge private child snapshot and join reads
to the same tree allowance. In particular, `joinVolumes` must honor explicitly
requested snapshot read accounting for these new child joins. Renaming a private
staged copy does not create a second extraction allowance.

Keep the application cleanup opt-in closed. Measured nested-set evidence does
not expand `archive-tree/1`, `/2`, standalone RAR proofs, modern set proofs or
old-name set proofs. Existing consumers must retain these sources even if every
leaf is already held. Add no persisted proof or schema during this increment.

## Verification and later authority

Use synthetic Go fixtures and injected readers/scanners. Preserve every earlier
root grouping, extraction, proof, recovery and unsupported-source case. Exercise
each supported layout, outer archive kinds, standalone children, deeper nesting,
multiple groups, unordered tables and exact original source chains. Cover
duplicates, unsafe names/types, folder aliases, overlapping families, missing or
failed parts, CRC/size failures, role/ordinal/continuity/table mismatches, hidden
records, cancellation, active deadlines and all shared limits including charged
failures. Test framing evidence separately from output integrity and eligibility.
Keep earlier capabilities insufficient, root facts strict and application cleanup
preview/apply preservation explicit, including already-held exact leaves.

Separately run generated native fixtures outside the checkout against installed
7-Zip 26.04, using independently expected output and unchanged source bytes.
Include hidden/unheld/uncertain cases and actual application retention. No real
user library cleanup, scanner, network or downloaded-code execution is required.
Require canonical verifier, full native cgo race, docs checks and actual all-six
hosted CI on the final commit. Retain checks, assertions, instrumentation, worker
limits and deadlines. Self-review consequential changes and record exact evidence.

Post and read back acceptance while issue 57 remains open before a separate
nested-volume writer decision. That decision needs distinct authority, fresh
whole-tree purge, immutable partial recovery and actual preceding-executable
retention/raw restore. Stage 2 and this issue remain unfinished; no release or
later roadmap stage is included.

## Measured local implementation evidence

The implementation relocates canonical grouping without changing its library
projection, stages only verified flat parts under fixed private names, and reuses
the existing set adapters. All application callers keep the new opt-in false;
cleanup additionally rejects measured child-set evidence under existing proofs.
No decoder, module, executable, public option or persisted schema was added.

Synthetic offline tests cover both RAR formats and naming layouts, the three
other set layouts, arbitrary tables, independent interleaved groups, ancestor
coverage and depth, selected capabilities, unsafe/type/encryption/method/folder
refusals, missing readers, physical and reader-table failures, output integrity,
shared allowances including failed copies, active cancellation and deadlines,
JSON omission and application preview/apply retention with exact held leaves.
Earlier root grouping, parser, recovery and refusal suites still pass. The final
patch was self-reviewed for path provenance, private-part removal, root-only
facts, capability selection and shared read accounting.

Generated native fixtures outside the checkout passed 32 combinations with exact
independently generated held leaves: seven child layouts under ZIP, 7z, RAR4
and RAR5, plus nested old-name RAR5 under modern/old RAR4/RAR5 root sets. Eight
additional cases cover hidden service records, missing middle parts, physical
CRC corruption and unheld leaves for both formats. All 40 application cleanup
previews and applies moved zero and retained byte-identical original sources.
The primary 28-case book was 6,189 bytes, SHA-256
`fc8e68a3b62ad0ad40d25885e08c194afa187e9baa0b24b5889087b487fef0fd`.
The installed reader reported 7-Zip 26.04; its executable SHA-256 was
`f802383f13b8331aabaf779def00a356c8d2b8148255170b75b03028f4d23ad4`.
These checks establish neither broad format conformance nor a safety guarantee.

Canonical local verification passed at 86.5% aggregate statement coverage;
library/staging covered suites took 28.495/17.164 seconds. Full native-cgo race
passed, including library/staging at 91.663/34.539 seconds. Windows cross-vet,
local documentation links/anchors and whitespace checks passed. Exact-final
hosted acceptance remains tracked in issue 57 and must precede a writer decision.
