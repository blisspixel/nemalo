# 0060: complete ZIP byte accounting before cleanup

Status: accepted and implemented, 2026-10-20. Stage 2, [issue 53](https://github.com/blisspixel/nemalo/issues/53).
Extends [decision 0051](0051-cleanup-of-7z-and-split-sets.md), without granting
cleanup permission to another archive layout.

## Evidence

A generated one-member ZIP remains readable by the pinned Go ZIP reader when
arbitrary bytes are inserted after its last central-directory record and the
declared directory size is enlarged. The earlier cleanup proof accepted the
declared size and proposed moving that archive. A regression reproduces this
before the fix. Ordinary decoder acceptance cannot establish whole-container
accounting for cleanup or purge.

## Decision

Keep the maintained Go ZIP parser and extractor. Independently check the framing
of the same opened bytes: exactly one ordinary end record, zero disk references,
matching counts, and central records that exactly fill their declared extent.
Cross-check each record against the maintained parser's member and its local
header, including names, flags, method, sizes and CRC. Local entries and their
checked 12- or 16-byte descriptors must fill the entire space before the directory.
Duplicate offsets, gaps, overlaps, inconsistent records and unsupported framing
preserve the source. Ambiguous descriptor signatures are refused.
Only stored or Deflate members using supported ZIP 2.0 flags and required-version
fields qualify; unsupported representations remain for review.

Archive and member comments stay for review. Extra fields must have complete
framing and a supported metadata representation: extended timestamps, NTFS times,
or the fixed Unix timestamp/identity fields handled by the Go reader. Unknown,
variable attached data, malformed metadata and ZIP64 remain preserved. This is
a conservative cleanup subset, not a new format-validity or metadata-completeness
claim. Intake keeps its existing broader supported behavior.

All proof reads, including the maintained reader and later offset queries, share
one 16 MiB allowance and cancellation. No payload decompression or new dependency
is added to the proof. Extraction still verifies members separately, and cleanup
still requires every member to be held with verified stored bytes, source rehashes,
the previewed plan, and the existing durable recovery manifest. Purge must use the
same strengthened proof, including for older held archives.
Purge reports `archive_not_accounted` for those retained ZIPs. A pending older
purge also rechecks the same opened, rehashed file before removal, and retains
unaccounted bytes as that outcome while completing independently held entries.
The existing manifest and journal schemas stay unchanged.

RAR, split ZIP and nested cleanup remain unchanged. Only after this proof passes
its own acceptance can a later change reuse it for another layout.

## Contracts and verification

Reviewed against [PKWARE APPNOTE 6.3.10](https://pkware.cachefly.net/webdocs/casestudies/APPNOTE.TXT)
and the established Go 1.27.2 reader in the local toolchain. The decoder remains
responsible for member interpretation; the extra check establishes the supported
container's byte coverage and refuses uncertainty.

Use synthetic Go regressions for hidden directory bytes, contradictory headers,
counts and disk references, descriptors, metadata, malformed lengths, bounded
reads and cancellation. Verify ordinary cleanup/restore/purge beside refusal,
including historical held-copy purge. Require canonical verification, native cgo
race checks, documentation checks and actual hosted CI before issue acceptance.

The new regressions pass for directory gaps and a descriptor size contradiction
that the Go reader otherwise accepts. Ordinary signed/unsigned/no-descriptor
ZIPs and differing local/directory order still qualify. Historical fixtures bind
the old accepted bytes to canonical cleanup journals and manifests; both a new
purge and a resumed older purge retain the archive and restore all its bytes.
A bounded Go fuzz run on the final implementation passed 99,985 executions
(an earlier run passed 135,224). A separate native Linux experiment with installed
7-Zip 26.04 used generated content outside the checkout: its ordinary ZIP with
NTFS timestamps qualified for cleanup, while inserted directory bytes kept the
otherwise readable archive. That experiment previewed only; automated tests cover
write/recovery transitions. Hosted acceptance is recorded separately on the issue
after the exact commit completes CI.
