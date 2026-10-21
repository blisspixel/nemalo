# 0066: recoverable cleanup of a bounded standalone RAR5 subset

Status: accepted and implemented, 2026-10-20. Acceptance evidence is tracked in
[issue 57](https://github.com/blisspixel/nemalo/issues/57), roadmap stage 2.
This decision does not complete that issue or authorize a release.
RAR4 has its own later [decision 0070](0070-standalone-rar4-cleanup.md); it never
borrows this RAR5 proof.

## Prerequisite

The user authorized the bounded Go framing proof in decision 0064, requiring
full verification before cleanup is enabled. Its framing, metadata and maintained
reader binding must each pass canonical local verification, native-cgo race,
synthetic and native experiments, documentation checks and actual all-six hosted
checks before changing the application's RAR source-removal policy. The enabled
subset then requires the same complete checks on its own final commit.
The read-only baseline, metadata and reader-binding gates passed on commits
`dabf83a`, `8d2ad15` and `4707628`, respectively. Their all-six hosted runs are
[39508244604](https://github.com/blisspixel/nemalo/actions/runs/39508244604),
[39509995330](https://github.com/blisspixel/nemalo/actions/runs/39509995330) and
[39514355386](https://github.com/blisspixel/nemalo/actions/runs/39514355386).

## Decision

Permit one standalone root RAR5 with only held book leaves and empty directories,
within the existing cleanup file and shared staging budgets. The opened private
snapshot must match the planned size and SHA-256. The bounded framing accountant
must cover it exactly and bind every physical file record to the maintained
reader's one UTF-8 technical table before exact-name extraction. Successful
extraction independently requires known sizes and CRCs; every leaf must have a
current holding with verified stored bytes, and every final original must still
hash to the plan. No framing or reader success alone authorizes removal.

Keep RAR4, RAR volume sets, child RARs, and a RAR root containing nested archives
preserved. Keep service/comment/recovery blocks, encryption, unknown metadata,
unsupported flags or attributes, split records, directory data, aliases, gaps,
tails and all other framing refusals outside the subset. Default intake remains
unchanged. Add no decoder, dependency, executable, cgo or network requirement.

Record the distinct `rar5-frames/1` proof, one exact original source identity,
and every held leaf's one-component selection, size, SHA-256 and holding ID in
`archive_leaves`. Bind them to the reviewed cleanup digest and immutable manifest.
Do not extend `archive-tree/1` to RAR. A RAR proof cannot authorize a ZIP/7z source,
a volume set or nested leaf; a ZIP/7z proof cannot authorize a RAR source.

Reuse the existing verified copy, hard-link publication, source removal and
journal transitions. Recovery validates all supported proof/layout/leaf bindings
before source writes, then uses the recorded manifest rather than a new plan.
Restore returns the recorded original bytes without replacing a target and
requires no reader or framing parser.

Fresh purge repeats the same complete framing, reader, source and current-holding
checks. Its existing schema-2 group binds `rar5-frames/1`, the single exact file
index and canonical leaf digest before removal. Partial recovery uses that durable
supported proof, remaining recorded source identity and current verified holdings;
it never reconstructs a partial source or adopts a replacement. Unknown evidence
retains bytes; malformed or mismatched known bindings stop for review. Historical
unversioned RAR entries and old continuations remain preserved for restore.

Earlier executables must retain or refuse the unfamiliar proof before cleanup
or purge removal. A reader that can safely restore recorded raw bytes may do so;
restore is not new coverage authority. Verify compatibility with an actual earlier
executable, rather than inferring it from a source review.

## Verification

Use synthetic Go RAR5 builders and injected readers/scanners for positive holding
checks, exact leaf/digest bindings, cleanup/restore/purge, every interruption,
unknown and malformed proofs, changed sources/holdings, missing readers, cancellation,
independent groups and replacement canaries. Retain RAR4, volumes, nested roots,
child RARs and unheld material. Preserve unsupported fixtures and source bytes.

Separately use an installed maintained reader with generated valid books outside
the checkout for cleanup, byte-exact restore, grouped purge and strict refused
layouts. The independently checked compressed synthetic pattern in decision 0064
establishes reader/framing agreement, not book validity or broad RAR conformance.
Record local and exact-commit hosted evidence on issue 57 and keep it open for the
remaining RAR layouts and their separately specified proofs.
