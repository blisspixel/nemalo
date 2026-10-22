# 0079: recoverable cleanup of completely covered old-name RAR sets

Status: accepted, 2026-10-22. Implementation and verification tracked in
[issue 57](https://github.com/blisspixel/nemalo/issues/57), roadmap stage 2.
Nested volume work remains unfinished. No release is included.

## Prerequisite

Decision 0078's separate read-only old-name root RAR4/RAR5 coverage passed
canonical local verification, native cgo race, generated native checks and
[all six hosted CI jobs](https://github.com/blisspixel/nemalo/actions/runs/39869814650)
at `1e927a289dd59cf2b3c869f696825c785c3e89f5`. Go and Actions CodeQL passed.
Its [acceptance receipt](https://github.com/blisspixel/nemalo/issues/57#issuecomment-6353603662)
was posted and read back exactly while issue 57 remained open, before this
decision or writer implementation. Windows reparse tests passed without skips;
cgo-free native executable smokes passed. Old-name cleanup stayed disabled at
that accepted prerequisite, including when every exact leaf was already held.

## Decision

Permit only completely covered old-name root RAR4/RAR5 sets through the existing
cleanup service. This explicitly revises old-name source retention for this
bounded subset after verified decision 0078 evidence. Require checked private
snapshots, complete physical framing, matching isolated and assembled reader
tables, complete verified output, every currently verified held leaf, expected
source identities and final unchanged original hashes. Require `Whole`,
`Complete` and no coverage gap. Unsupported or uncertain material stays.

Request `OldRARSetCoverage` only for a separately validated old root layout,
alongside `Coverage` and the selected format's existing set capabilities.
Select distinct `rar4-old-set/1` or `rar5-old-set/1` only from measured
`OldRARSetVersion=4` or `5`, with modern root-set evidence zero. Names and child
versions do not supply physical format or authority. Keep all earlier modern,
standalone and tree proof IDs and parser subsets unchanged. Add no decoder,
module, executable, public option, network operation or persisted schema change.

A supported old layout has 2..901 exact ordered names: one `.rar`, then `.r00`
through `.z99`, one checked portable family and one exact parent. Keep 256 MiB
per source, 4 GiB total snapshots, the shared 64 GiB source-hash allowance and
all proof, member, output, reader, process, diagnostic, cancellation and deadline
budgets, including failed work. Missing parts, aliases, overlapping families,
unlinked boundaries and unsupported framing remain preserved. Old RAR4 naming
does not imply support for RAR generations lacking mandatory role/ordinal fields.

Use a separate old-layout shape validator. Do not expand the modern numeric
set validator or the meaning of earlier proofs. Generic immutable archive-group
handling may recognize the closed old shape for retention; recognizing names
does not grant a known proof. `.zNN` names can overlap ZIP segments, so explicit
anchors and existing ambiguity checks still apply. Completely covered standalone
children reuse existing tree checks. Nested volume sets remain refused, and
neither `archive-tree/1` nor `/2` gains an outer RAR-set scope.

Represent a complete source set as one recoverable entry with exact ordered
files and canonical leaf chains, sizes, hashes and holdings. Reuse verified
copy, hard-link publication, source removal and durable cleanup recovery. Raw
restore replaces nothing and needs no archive reader. Fresh purge rechecks the
entire held set and exact leaves against its original measured format and proof.
Its existing schema-2 intent binds the distinct proof, all original file indices
and canonical leaf digest. Validate every known shape and group binding before
any independent removal. Unknown authority retains bytes.

Partial purge recovery uses only supported immutable original evidence plus
fresh identities of remaining held bytes and current verified holdings. Never
join missing parts, reopen the reader to reconstruct a partly purged set, adopt
replacements, migrate receipts or create a new plan for recovery. Whole and
partial recovery fixtures must be independent from preceding-executable tests.

The actual accepted predecessor may refuse an unfamiliar old-layout pending
group while leaving its intent pending. Verify its JSON outcomes and retained
bytes rather than assuming the modern-layout zero-purged completion behavior.
If it refuses, close that pending operation through canonical `purge --stop`
before testing raw no-reader restore. Report absent parts honestly. Distinguish
constructed durable snapshots from observed interruption hooks or power loss.

## Verification required

Use synthetic Go fixtures and injected readers/scanners. Reuse existing complete
set lifecycle, per-part cleanup/purge and restore interruption tests, immutable
proof/leaf/index bindings, independent groups, replacements, cancellation,
source-hash limits and current-holding checks for both formats. Keep every prior
format/layout refusal and recovery case. Deliberately revise the prerequisite's
disabled-writer assertion into distinct old-name authority assertions; modern
proofs must still reject old layouts and old proofs must reject modern layouts.
Renaming modern RAR4 framing does not change its physical naming flags.

Cover 2/3/more parts, later positions and letter rollover, direct and nested
standalone leaves, known metadata, complete siblings and empty directories.
Exercise fresh cross-format and cross-layout proof substitution in both
directions, immutable known-proof mismatches and malformed later groups before
independent removal. Keep missing/unknown evidence, hidden records, missing
readers/volumes, ambiguous families and unheld material preserved.

Separately use generated native stored sets, compressed references with
independently expected output, hidden/unheld refusals, cleanup, exact raw restore
and fresh purge against installed 7-Zip 26.04. Exercise the actual clean accepted
preceding executable at `1e927a2`, SHA-256
`375bdc6cf0a02637fe99d766dc8578ce040f5286e9db27528138e56050f68450`.
Keep generated content and reports outside the checkout. No real user library
cleanup, installed scanner, network or downloaded-code execution is required.

Require canonical verifier, full native cgo race, documentation checks and actual
all-six hosted CI at the final commit. Measure test growth within the existing
three-worker schedule and deadlines; preserve assertions, instrumentation,
coverage thresholds and real durability operations. Self-review consequential
changes. Post and read back acceptance while issue 57 is open. Nested volumes
keep stage 2 unfinished, and no later stage or release is included.
