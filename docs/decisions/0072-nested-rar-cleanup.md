# 0072: recoverable cleanup of covered trees containing standalone RARs

Status: accepted, 2026-10-20. Implementation and verification tracked in
[issue 57](https://github.com/blisspixel/nemalo/issues/57), roadmap stage 2.
This does not complete volume work or authorize a release.

## Prerequisite

The explicit read-only nested capability, its shared work/child/reader boundaries
and its application retention guard must pass full local, native and actual
all-six hosted acceptance. Read back its issue receipt while 57 remains open,
then record this decision before application edits. The writer increment requires
its own full exact-commit acceptance afterward. Keep volume cleanup separate.
The read-only gate passed at `12bea6e` in all-six hosted
[run 39531484354](https://github.com/blisspixel/nemalo/actions/runs/39531484354).
[Receipt 6306463093](https://github.com/blisspixel/nemalo/issues/57#issuecomment-6306463093)
was read back while issue 57 remained open, before this application increment.

## Decision

Permit only completely accounted trees within existing bounds, with standalone
RAR4/RAR5 nodes inside their already verified framing subsets and every leaf held
with currently verified stored bytes. Reuse the same checked snapshots, strict
child preflight, exact reader table/output binding, source hashes and shared limits.
No decoder, module, executable, public option, network or archive format expansion.

Return an internal JSON-omitted count of individually covered RAR nodes, measured
from the private node signature and successful physical coverage, including empty
RAR children. Keep RARVersion outer-only. Application traversal may request both
RAR4Coverage and NestedRARCoverage only alongside Coverage and the new authority.
A tree is eligible only with whole coverage, complete output, every held leaf and
unchanged final originals; partial facts alone confer no permission.

Bind nested trees containing a measured RAR node to new archive-tree/2. Direct-only
RAR roots retain rar4-frames/1 or rar5-frames/1. ZIP/7z-only trees retain /1. Never
infer /2 from a filename or a leaf chain, since a covered empty RAR child may
appear only as a container marker beside a direct held book. Never extend /1 or
either standalone RAR proof. Keep original proof identities in old manifests.

Use the existing single ZIP/7z/RAR roots and supported ZIP/7z outer sets. RAR volume
sets and all nested volume sets remain preserved. Intermediate .rar selections
may be valid only under supported /2 authority; /1 continues to exclude RAR roots
and intermediate RARs. Preserve canonical exact chain/size/hash/holding bindings,
limits, parse-equivalent escaping and stable leaf digest encoding. Separate safe
syntactic leaf validation from known proof semantics so unknown proofs retain
bytes without gaining any permission. Validate every KNOWN proof's complete
layout/leaf shape before any independent group may remove a file. Known proof
mismatches are errors; unknown authority retains bytes.

Reuse reviewed cleanup manifests, copy/link/remove and per-entry journals. Fresh
purge repeats the whole tree check and binds /2, all exact original file indices
and the canonical leaf digest into the schema-2 intent. Partial recovery uses
only that supported immutable original evidence with current remaining identities
and holdings; never reconstruct partial sets or adopt replacements. Restore
returns recorded original bytes without a reader or replacing any target.

Actual preceding executables must retain/refuse unfamiliar /2 authority before
removal. Test raw restore compatibility separately. No old receipt is migrated or
relabelled. Keep unsupported metadata/Unicode, hidden service/subblocks, directory
data, aliases, gaps/tails, missing readers and incomplete traversal preserved.

## Verification required

Use shared synthetic recovery assertions for all five ZIP/7z outer layouts and
both RAR root versions, exact mixed leaf chains, all-held/unheld material, strict
refusals, current holdings and final originals. Add the empty-RAR-with-direct-book
regression, measured count/default/partial/JSON checks, and wrong known proof
versions in fresh and pending operations. Exercise every cleanup/restore/purge
mutation interruption, partial removals, independent groups, missing/unknown/
malformed bindings, replacements, cancellation and source/holding changes. Assert
old /1 and standalone IDs cannot borrow new authority. Explicitly update the
read-only no-move guard to require ONLY /2, while preserving volume/malformed
refusals and adding older-proof rejection.

Separately run native generated trees through cleanup, byte-exact restore and
purge, including ZIP/7z/RAR roots and supported outer sets, retained hidden/
unheld material, and an actual preceding binary. References remain outside the
checkout, tests use injected readers/scanners and no network/installed tools.
Require verifier, native-cgo full race, Windows cross-vet, docs and exact all-six
hosted CI; post/read acceptance while 57 is open. Volume work remains unfinished.

Native Linux validation with installed 7-Zip 26.04 passed sixteen generated
cleanup, byte-exact restore and purge cases: both child RAR versions under all
five ZIP/7z layouts, both RAR root versions and empty-RAR-with-direct-book trees.
Four hidden-record or unheld-notes cases retained every original byte. The actual
preceding `12bea6e` executable refused the new cleanup and purge plans, preserved
all source/held bytes and restored the recorded raw originals. Its SHA-256 is
`f653053502035701a64b7fc70d9c883fabff30212df60dbf8918aff94e5f331f`.
These are bounded recovery/reader checks, not broad format conformance or safety
claims. No downloaded content was executed and no installed scanner was used.
