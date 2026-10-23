# 0082: recoverable cleanup of completely covered nested volume trees

Status: accepted, 2026-10-23 UTC. Implementation and verification tracked in
[issue 57](https://github.com/blisspixel/nemalo/issues/57), roadmap stage 2.
No release or real-user source cleanup is included.

## Prerequisite

Decisions 0080 and 0081 passed canonical local verification, full native-cgo race,
generated native checks and [all six hosted CI jobs](https://github.com/blisspixel/nemalo/actions/runs/39883474442)
at `1f6337424d1bafda2bb17ce7eb75d89586bf0f37`. Go and Actions CodeQL passed.
The [acceptance receipt](https://github.com/blisspixel/nemalo/issues/57#issuecomment-6355297444)
was posted and read back byte-for-byte while issue 57 remained open, before this
decision or writer implementation. Windows reparse cases passed without skips
and cgo-free native smokes passed. Windows library covered/race suites took
321.511/342.945 seconds under the unchanged 600-second package deadline.

The earlier `7a18a20` Windows timeout is not accepted evidence. Test-only counter
housekeeping retained all 1,384 preceding recovery leaf cases, instrumentation,
three-worker ceiling, deadlines and production durability calls. The accepted
prerequisite still left every application `NestedVolumeSets` flag false and
retained even completely covered nested sources with exact leaves already held.

## Decision

Permit only completely covered nested volume trees through the existing cleanup
and purge services. This explicitly revises nested-set retention for the bounded
decision 0080 layouts: modern/old RAR4/RAR5, split 7z, ZIP chunks and ZIP disk
segments. Keep every parser subset, adapter, part/group check, shared allowance
and preservation rule unchanged. Request `NestedVolumeSets` and the existing
selected-format coverage capabilities in application cleanup. Child old-name
coverage may be requested independently of the root naming layout; old-root
authority still requires its separate validated shape and measured version.

Select new `archive-tree/3` only from measured `CoveredVolumeSets > 0`, after
`Whole`, `Complete`, no coverage gap, every exact leaf currently held with verified
bytes, expected snapshot identities and final unchanged original-source hashes.
Validate root RAR standalone/modern/old format and layout facts independently
before selecting this authority. A child, suffix or count cannot supply those
root facts. Covered empty child sets beside a direct held book also need /3;
do not infer authority from leaf names or the presence of a nested leaf.

Keep all earlier proof identities and actual runtime scopes unchanged. Trees
without covered child sets retain their existing standalone, tree or root-set
proofs. A fresh purge must reproduce the original proof and exact leaf bindings;
old authority cannot acquire new semantics from the newly enabled traversal.
An old-name child and a standalone child can have the same `sample.rar` selection:
historical syntax alone cannot distinguish them. The checked physical tree and
versioned original proof supply that distinction, rather than invented naming
evidence or relabelling an earlier receipt.

Use a separate bounded leaf validator for /3. It retains canonical safe exact
member chains, 1..4 components, sizes, hashes, holdings and unique selections.
In addition to existing standalone archive names, intermediate components may
name a recognized first part of a supported numeric set or first ZIP disk segment.
Reject continuation ordinals and unsafe/ambiguous syntax. Do not broaden the
existing leaf validator or any earlier known proof's shape merely to accept a
new suffix. /3 may have only a direct leaf when a covered child has no file leaf.

Reuse the same validated outer single-file and whole-set layouts, one recoverable
entry, exact original file order and canonical leaf digest. Add no persisted
field, schema, migration, decoder, module, executable, public option or network
operation. The original outer sources already retain every encoded child part;
never publish or journal a private child snapshot as an original source.

Fresh purge repeats the full checked tree and holding checks and binds /3, all
original indices and the existing leaf digest into its durable schema-2 intent.
Partial recovery uses only supported immutable original evidence, fresh remaining
byte identities and current verified holdings. Never reconstruct a partly purged
tree, adopt a replacement, reset work limits or create a new recovery plan.
Validate all known shapes and bindings before any independent group can remove
bytes. Raw restore returns the exact recorded originals without an archive reader
and never replaces a target. Unknown authority retains bytes or refuses safely.

Enable the verified child-group intake opt-in through the existing `ingestArchive`
tree call, including recovery. Intake preserves sources and does not request
coverage or infer checked publication from a successful reader. Continue normal
assessment, explicit scanner policy and Import per leaf. Keep exact original
first-part provenance, canonical escaped source labels, the existing record-size
bound and the rule that an unavailable pending leaf stops before other imports.
Never import continuation bytes as book leaves. Defaults and direct extraction
retain their existing no-traversal semantics.

## Verification and acceptance

Preserve all existing tests, assertions, instrumentation, workers, deadlines and
durability operations. Use compositional verification: decision 0080 already
checks every child adapter and its failures. Exercise /3 selection and exact
leaves across every outer layout and child-name layout, then reuse the complete
per-part cleanup/purge/restore recovery assertions on representative three-part
outer layouts. Cover single-file interruptions separately. Avoid a redundant
Cartesian multiplication of every proven reader case by every recovery case.

Add empty-child/direct-leaf, interleaved groups, old-name syntax ambiguity,
unheld notes/books, malformed/unknown/earlier proof bindings, fresh wrong-proof
reproduction, changed sources/holdings, current limits and cancellation cases.
Verify default intake preview/apply/repeat and exact pending-leaf recovery, with
no continuation publication, new parser, real scanner or public network in tests.
Deliberately update the prerequisite's disabled-application assertions to require
only the distinct new authority, retaining all uncertainty and older-scope refusals.

Separately use generated native sources outside the checkout and installed 7-Zip
26.04 for cleanup, byte-exact restore and fresh purge. Use the actual accepted
preceding executable, SHA-256
`30c607c51910fe4ba7e21929b050d4cc502c05ef0c8209052660d7d538f84683`,
for unfamiliar cleanup/purge retention and raw no-reader restore. Old readers may
refuse chunk/segment leaf syntax while RAR-named chains complete with zero purged;
verify actual JSON and retained bytes. Close a refused pending intent with canonical
`purge --stop` before raw restore. Use independent constructed whole/partial durable
snapshots for current recovery; distinguish those from observed crash hooks.

Require self-review, canonical verifier, full native-cgo race, Windows cross-vet,
documentation checks and actual all-six hosted success on the final commit.
Post/read exact acceptance while issue 57 remains open. Audit its remaining stage 2
requirements before closure; unsupported metadata and uncertain sources remain
preserved. Later audio, evidence, interface and post-1.0 work follows roadmap order.
