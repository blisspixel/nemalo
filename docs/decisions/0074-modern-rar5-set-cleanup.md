# 0074: recoverable cleanup of completely covered modern RAR5 sets

Status: accepted, 2026-10-21 UTC. Implementation and verification tracked in
[issue 57](https://github.com/blisspixel/nemalo/issues/57), roadmap stage 2.
This does not complete the remaining RAR volume work or authorize a release.

## Prerequisite

Decision 0073's read-only framing, isolated/assembled reader binding and shared
budgets passed full local and native validation and all six hosted jobs at
`be042d6` in [run 39548731808](https://github.com/blisspixel/nemalo/actions/runs/39548731808).
[Receipt 6308956038](https://github.com/blisspixel/nemalo/issues/57#issuecomment-6308956038)
was posted and read back while issue 57 remained open, before this decision
and application edits. Every RAR set remains preserved at that prerequisite.

## Decision

Permit modern root RAR5 sets only after the existing checked private snapshots,
physical framing, exact isolated and assembled tables, complete extraction and
every currently verified held leaf pass. Request Coverage and RAR5SetCoverage
through the existing shared cleanup service. Require measured RARSetVersion=5,
Whole and Complete, no coverage gap, all expected source sizes/hashes and unchanged
final originals. Source-copy reads and final rehashes share the existing work
budgets. RAR4, old-named and nested volume sets remain preserved. No decoder,
module, executable, public option, network operation or format expansion is added.

Use a distinct rar5-set/1 proof for the complete outer set, including completely
covered standalone children under the existing tree rules. Select it before
generic nested-RAR authority; a child's version/count cannot authorize its outer
set. Preserve rar4-frames/1, rar5-frames/1 and archive-tree/1 and /2 meanings.
Neither tree ID may acquire RAR outer-set authority through expanded syntactic
layout validation. Bind canonical exact leaf chains, sizes, hashes and holdings
without changing their encoding or leaf-digest version.

A supported set has 2 to 1,000 modern numeric parts, starting at ordinal one,
in exact order with one portable family and one exact parent. Retain the existing
256 MiB per-source and 4 GiB total snapshot bounds and 64 GiB source-hash budget.
Preview and manifest represent the whole set as one entry. Never authorize an
individual part from its name, a successful listing, a child or partial facts.

Reuse reviewed cleanup copy/verify/link/remove journals and no-replace raw restore.
Fresh purge repeats the complete held-set and leaf check and binds the new proof,
all exact original indices and canonical leaf digest into the schema-2 intent.
Partial recovery uses only supported immutable original evidence plus fresh
remaining-byte identities and current verified holdings. Never reconstruct a
partly purged set, adopt a replacement, migrate a receipt or create a new plan
to resume it. Validate every known proof's complete shape and group binding
before any independent entry removes bytes. Unknown authority retains bytes.

An actual prerequisite executable must refuse or retain unfamiliar cleanup/purge
authority and preserve every source/held copy. Verify raw restore separately,
without an installed reader or replacing targets. Keep unsupported metadata,
hidden/service/recovery records, aliases, missing volumes/readers, unheld material,
changed sources/holdings and incomplete extraction preserved.

## Verification required

Use synthetic Go fixtures and injected readers/scanners, never installed tools or
network in tests. Reuse lifecycle, per-part cleanup/purge interruptions, restore,
immutable proof/leaf/index binding, independent groups, replacement, cancellation,
current holdings and stale-source assertions. Cover 2/3/more parts, direct and
nested leaves, empty directories, children that cannot lend older authority,
all known wrong proof IDs, unknown/missing evidence and malformed later groups
before independent removal. Preserve the previous IDs' exact outer-layout scope.

Separately run generated native sets through cleanup, exact raw restore and purge
with the installed maintained reader. Include compressed reference output and
retained hidden/unheld material, and exercise the actual preceding executable.
Keep reference content and reports outside the checkout. Require canonical
verifier, full native-cgo race, Windows cross-vet, docs checks and actual all-six
hosted CI on the exact final commit. Windows prerequisite library race took
525.477 seconds under the unchanged 600-second package timeout: measure added
recovery work and preserve all cases, assertions, coverage and durability checks.
Post/read acceptance while issue 57 remains open. Remaining volume subsets keep
stage 2 unfinished, and no release is included.

Native Linux validation with installed 7-Zip 26.04 passed six generated set
lifecycles: 2/3/7 stored parts, RAR4 and RAR5 children, and complete first/last
volume siblings plus an empty directory. Cleanup, exact raw restore and fresh
purge passed. The actual preceding be042d6 executable refused the new cleanup
and purge plans, preserved sources/held copies and restored raw originals.
Its SHA-256 is
`8f1aa6914e06ff39a7f352b09ddcf5469bfadadfbf558e17d99ec8c2bac78ee9`.
Hidden service records, unheld notes and unsupported compressed binary content
retained every part through preview and apply. The compressed reference also
returned its independently regenerated exact 1,200-byte pattern through read-only
coverage; that establishes no book eligibility. No downloaded code or installed
scanner was executed. These are bounded framing/reader and recovery checks,
not broad format conformance or a safety guarantee. Full acceptance remains
tracked in issue 57.
