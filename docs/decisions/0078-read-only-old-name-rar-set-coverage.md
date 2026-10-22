# 0078: opt-in read-only old-name RAR set coverage

Status: accepted, 2026-10-22. Implementation and verification are tracked in
[issue 57](https://github.com/blisspixel/nemalo/issues/57), roadmap stage 2.
This decision grants no old-name cleanup or purge authority.

## Prerequisite and measured contracts

The intake correction in decision 0077 passed all six hosted jobs at
`f6c9e8fb6cc11c256319cbd9535a86b8e948695a` in
[run 39865238135](https://github.com/blisspixel/nemalo/actions/runs/39865238135).
Its [acceptance receipt](https://github.com/blisspixel/nemalo/issues/57#issuecomment-6352964566)
was posted and read back exactly while issue 57 remained open, before this
decision or framing implementation. Windows reparse cases passed without skips;
the cgo-free executable passed native smokes. The three-worker test scheduling
repair preserved every original fixture body and deadline.

Fresh generated native RAR4/RAR5 sets at 2, 3, 7, 103 and 901 parts completed
intake with installed 7-Zip 26.04 and exact independently generated PDF bytes.
Application previews accounted for all volumes and retained library/source
bytes. Intake kept `Whole=false` and set version 0; cleanup preview and apply
moved zero. Extraction success does not establish complete encoded-block coverage.

The maintained reader's [RAR4 definitions](https://github.com/ip7z/7zip/blob/26.04/CPP/7zip/Archive/Rar/RarHeader.h),
rechecked on 2026-10-22, distinguish the volume, modern naming and first-volume
bits and end ordinals. Old naming does not mean every historical RAR generation:
the first-volume bit is a RAR 3.0-and-later field. The
[RAR5 format](https://www.rarlab.com/technote.htm) and
[pinned handler](https://github.com/ip7z/7zip/blob/26.04/CPP/7zip/Archive/Rar/Rar5Handler.cpp)
separate physical volume fields from external filename layout. No reader or
decoder source is incorporated.

## Decision

Add an internal `OldRARSetCoverage` opt-in alongside `Coverage` and the selected
format's existing set capability. RAR4 additionally requires `RAR4Coverage` and
`RAR4SetCoverage`; RAR5 requires `RAR5SetCoverage`. All application callers keep
the new flag false. The existing modern capabilities alone continue to refuse
old naming. Defaults retain the current intake behavior.

Permit only root sets of 2..901 original names in the existing closed family:
one `.rar`, then `.r00` through `.z99`, in exact ordinal order and one checked
parent without aliases or gaps. Keep the 4 GiB total bound and existing source
identity checks. Every nested volume set remains refused, even with the opt-in.

Reuse the shared physical framing and continuation/table matcher. Keep public
`RAR4VolumeAccounted` strict for modern naming. A private explicit old-layout
mode requires the volume bit and absence of the modern-name bit, with the same
mandatory first role, exact end ordinal, supported header/metadata subset,
checksums and EOF checks. Its ordinal must fit the old naming range. Standalone
RAR4 still refuses every volume or split declaration. Unsupported older framing,
volume CRC declarations, unknown blocks/flags, comments, recovery, encryption,
directory data, gaps, tails and uncertain metadata remain refused.

RAR5 physical parsing is unchanged. The checked source layout selects the
explicit capability; it does not enlarge the RAR5 block or metadata subset.
Before exposing siblings, bind every physical file record to its isolated UTF-8
reader table and each split role/ordinal. Bind the complete logical facts to the
same clean assembled table used for exact-name stdout extraction. Preserve
packed-part CRCs versus final whole-output CRCs and every existing comparison.

Report old root evidence separately as internal, JSON-omitted
`OldRARSetVersion=4` or `5`. Old sets keep `RARSetVersion`, `RARVersion` and
`RARContainers` zero. Root tree results may retain this old-set fact; children
cannot provide it. Modern results keep their existing fields and meanings.
`Whole`, successful output, `Complete`, member eligibility and write permission
remain independent. Framing facts cannot authorize an existing modern proof.

Share expected snapshot sizes/hashes and all existing source-copy/hash, proof,
member, output, process, diagnostic, cancellation and deadline budgets across
every part, including failed work. Direct covered calls get the same budget
without enabling traversal. Keep decision 0077's exact diagnostic exceptions
and all later continuity/assembly checks. Add no decoder, module, executable,
network operation, public option, persisted proof or schema change.

## Verification and later authority

Use synthetic Go fixtures and injected readers/scanners. Cover both formats,
every capability combination, later positions and letter rollover, source byte
preservation and exact output. Keep modern and standalone refusals strict.
Exercise header/CRC/ordinal/role/metadata mutations, hidden records, incorrect
isolated and assembled tables, aliases, missing/reordered parts, shared limits
including failed work, cancellation and changed expected source identities.
Physical framing must not read compressed payloads. Test source framing and
output integrity separately, including failures after a valid framing proof.

Separately check generated native stored sets through 901 parts and compressed
references with independently expected output against installed 7-Zip 26.04.
Include hidden-record refusals and explicit application cleanup preview/apply
retention even when every book leaf is held. Unit tests invoke no installed
reader, scanner or public network. Keep native content and reports outside the
checkout.

Require canonical local verification, native cgo race, documentation checks and
actual hosted Linux/macOS/Windows success on the final commit. Post and read back
its acceptance while issue 57 is open before a separate old-name writer decision.
That later decision needs distinct proof IDs, fresh whole-set purge, immutable
partial recovery and preceding-executable retention/raw-restore evidence. No
existing proof expands here. Stage 2, nested-volume work and a release remain
unfinished.
