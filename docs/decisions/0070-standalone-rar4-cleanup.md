# 0070: signature-bound recoverable standalone RAR4 cleanup

Status: accepted, 2026-10-20. Implementation and verification tracked in
[issue 57](https://github.com/blisspixel/nemalo/issues/57), roadmap stage 2.
This decision does not complete volume/nested work or authorize a release.

## Prerequisite

The user-authorized framing approach requires full verification before cleanup.
Decisions 0067 through 0069, including name-work cancellation and exact reader
binding, must pass their complete local, native and actual all-six hosted gates
before application policy enables RAR4. The application increment then needs its
own complete verification on the exact final commit.
The framing/cancellation and reader-binding gates passed at `df9f372` and
`23fee80`, in hosted runs
[39520758843](https://github.com/blisspixel/nemalo/actions/runs/39520758843) and
[39523091999](https://github.com/blisspixel/nemalo/actions/runs/39523091999).
Metadata acceptance passed at `ee52520` in all-six hosted run
[39526581039](https://github.com/blisspixel/nemalo/actions/runs/39526581039), after
the required Go 1.27.3 security patch. Its acceptance receipt was read back while
issue 57 remained open, before enabling the application's RAR4 opt-in.

## Decision

Permit only one standalone root RAR4 with held direct book leaves and empty
directories in the verified framing subset: bounded ASCII names, known attributes,
known methods/versions, zero high-size words and the selected exact time metadata.
Keep Unicode names, nonzero high words, unsupported metadata, services/subblocks,
comments/recovery, encryption, volumes, child RARs and RAR roots containing nested
archives preserved. No decoder, dependency, executable, cgo or network is added.

Return the measured RAR version from the adapter's checked private signature,
propagated only from the outer root through the shared tree result. This is
internal evidence, omitted from JSON. The application may enable its internal
RAR4 reader opt-in only alongside signature-bound selection: `rar4-frames/1`
for measured version 4 and the existing `rar5-frames/1` for version 5. A missing or
unknown measured version preserves the source. Filename extension alone never
selects a RAR proof, and `archive-tree/1` never grants RAR authority.

Reuse the same expected size/SHA-256 snapshot binding, bounded framing and single
UTF-8 reader table, exact-name output size/CRC checks, current verified holdings
and unchanged final originals. Bind one original source identity and every exact
one-component leaf to the reviewed digest and immutable cleanup manifest. Every
leaf must be held; no successful extraction or framing fact alone grants a move.

Use the existing copy, hard-link publication, source removal and journal protocol.
Validate supported proof/layout/leaf bindings before any recovery writes. Restore
returns recorded original bytes without replacing targets and needs no reader.

Fresh purge repeats complete signature/framing/reader/holding checks and binds
its distinct proof, single exact index and canonical leaf digest to the existing
schema-2 intent. Partial recovery uses that immutable supported evidence and
fresh remaining identities and holdings. It never reconstructs a partial source
or adopts replacements. Unknown proof versions retain bytes; malformed or
mismatched known bindings stop before removal. Historical unversioned RAR entries
retain their existing refusal and raw-byte restore contracts.

Earlier executables must retain or refuse the unfamiliar RAR4 authority before
source removal. Verify this with an actual preceding binary; raw-byte restore may
remain compatible. Do not relabel an old immutable receipt as current proof.

## Verification

Use Go-generated fixtures, injected readers/scanners and shared recovery tests.
Verify measured outer version, independent RAR4/RAR5 proof selection, exact and
multiple leaves, unheld material, all cleanup/restore/purge interruptions, current
holdings, changed sources, cancellation, missing readers, unknown/malformed proofs,
independent groups and replacement canaries. Wrong known proof versions cannot
borrow another format's authority, including through a pending purge.

Separately validate generated stored books/directories with known metadata through
cleanup, byte-exact restore and grouped purge using the installed maintained
reader outside the checkout. Keep unsupported native layouts unchanged. The
compressed reference establishes reader/output agreement, not book validity.
Require canonical verifier, native-cgo race, docs checks and actual all-six hosted
jobs on the final commit. Keep issue 57 open for remaining supported volume and
nested RAR layouts; this subset does not complete roadmap stage 2.
