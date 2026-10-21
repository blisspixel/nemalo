# 0071: opt-in read-only nested RAR coverage

Status: accepted, 2026-10-20. Implementation and verification tracked in
[issue 57](https://github.com/blisspixel/nemalo/issues/57), roadmap stage 2.
This decision grants no new cleanup or purge authority.

## Prerequisite and boundary

Standalone RAR4/RAR5 framing and exact maintained-reader binding have passed
separate local, native and hosted gates. Standalone application integration in
[decision 0070](0070-standalone-rar4-cleanup.md) passed all six hosted jobs at
`f008510` in [run 39528208063](https://github.com/blisspixel/nemalo/actions/runs/39528208063).
Its acceptance receipt was read back while issue 57 remained open.

Reuse these same strict standalone framing subsets for size/CRC-checked private
children under [0058](0058-bounded-nested-intake.md)'s existing traversal.
No format flags, metadata, encodings or volume layouts are added. No decoder,
module, executable, public option, cgo requirement or network operation is added.

## Decision

Require an explicit internal `Limits.NestedRARCoverage` alongside `Coverage`
before a RAR child may receive physical coverage evidence. RAR4 additionally
requires `RAR4Coverage`. Both remain false by default; application cleanup leaves
the new nested flag false. Merely setting a nested flag without `Coverage` keeps
ordinary intake behavior and cannot produce a covered tree.

Measure each child from the same checked opened private snapshot used for its
reader table and exact-name extraction. Require all existing header/data framing,
CRC, name/alias, known metadata and exact EOF checks; bind every physical file
record to one complete UTF-8 table before checked output. Retain the strict
maintained-reader child preflight establishing one non-volume container. Neither
file extension, physical size, successful extraction nor preflight alone proves
coverage. RAR volume/set declarations, hidden service/subblocks, unknown fields,
unsupported encodings, directory data, gaps and tails remain refused.

All descendants share the existing four-level traversal, member/output, proof/read,
diagnostic/process and outer-deadline allowances. Failed attempts stay charged;
no child resets a budget. Use generated node directories and flat staged names,
never member paths. Preserve exact leaf chains and return each container's refusal
at its exact chain. An unproven child cannot make its outer tree whole. The
measured `RARVersion` remains outer-only and omitted from JSON; a child cannot
supply its parent's signature fact.

Keep physical coverage, successful extraction, member eligibility, verified
holdings and source-removal permission separate. A physically accounted notes
file may be unsupported for import; it remains visible and preserved. A checked
frame/table can still have failed output integrity. Neither case authorizes cleanup.

## Application contract

The application does not set `NestedRARCoverage` in this increment. Existing
`archive-tree/1`, `rar4-frames/1` and `rar5-frames/1` retain their exact meanings.
Child RARs and RAR roots with nested archives still preserve their outer sources.
No historical receipt or manifest is migrated.

A later application increment needs a distinct versioned tree proof, measured
container evidence, exact leaves/current holdings, final originals and the full
reviewed cleanup/restore/grouped-purge recovery contract. It must follow this
read-only capability's complete local/native/exact-hosted acceptance, then pass
its own full gate. Volume accounting remains separate. This does not complete
issue 57 or roadmap stage 2 and does not authorize a release.

## Verification

Use synthetic Go fixtures and injected readers/scanners for both RAR versions,
mixed ZIP/7z/RAR trees, exact chains and bytes, root-only signature facts, explicit
opt-in and unchanged default/application preservation. Refuse hidden/unknown child
records, CRC/table/output mismatch, unsupported child volumes and ambiguous reader
preflight. Test shared proof/process/diagnostic/member/output work across siblings,
including failed work, cancellation and depth exhaustion. Separately validate
fresh generated trees outside the checkout through installed 7-Zip, preserving
every application source. Require canonical verifier, native-cgo full race,
Windows cross-vet, docs checks and actual all-six hosted jobs on the final commit.

Native Linux experiments with installed 7-Zip 26.04 returned the exact generated
book bytes from RAR4 and RAR5 children under ZIP, 7z, RAR4 and RAR5 roots.
Hidden child subblocks/service records were refused. Preview and apply of
application cleanup retained every original source byte in all ten cases.
Wrapping the independent compressed references from decisions
[0067](0067-read-only-rar4-framing.md) and
[0064](0064-bounded-rar-framing-accounting.md) in generated ZIPs also returned the
exact independently regenerated 64 and 1,200 bytes through the opted-in nested
adapter. These establish reader/output agreement, not book eligibility, general
format conformance or cleanup permission. References, source and licenses stay
outside the checkout; no downloaded decoder or source code was executed.
