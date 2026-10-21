# 0075: opt-in read-only modern RAR4 volume-set coverage

Status: accepted, 2026-10-21 UTC. Implementation and verification tracked in
[issue 57](https://github.com/blisspixel/nemalo/issues/57), roadmap stage 2.
This decision grants no RAR4 set cleanup or purge authority.

## Prerequisite and measured contracts

Modern RAR5 set cleanup passed all six hosted jobs at `f4f1adc` in
[run 39552741780](https://github.com/blisspixel/nemalo/actions/runs/39552741780).
[Receipt 6309566200](https://github.com/blisspixel/nemalo/issues/57#issuecomment-6309566200)
was read back while issue 57 remained open, before this decision and edits.

The maintained [7-Zip 26.04 definitions](https://github.com/ip7z/7zip/blob/26.04/CPP/7zip/Archive/Rar/RarHeader.h)
and [handler](https://github.com/ip7z/7zip/blob/26.04/CPP/7zip/Archive/Rar/RarHandler.cpp),
checked on 2026-10-21, distinguish main volume/first/naming flags, split file
roles and end-record continuation/ordinal fields. A reserved end area has seven
bytes. A separate optional volume CRC precedes the ordinal. Nonfinal file CRCs
describe packed parts; the final record provides whole size and output CRC.
The reader sums packed spans and exposes the starting volume of an assembled
member. Its ordinary listing still omits non-file blocks. These are format
references, not incorporated source or a decoder.

Native Linux prerequisites with installed 7-Zip 26.04 passed generated stored
three-part PDFs with ordinary and zero-reserved end records. Reframing the same
490 compressed bytes from decision 0067 returned the independently regenerated
64-byte pattern, SHA-256
`353d5f7a0789034186922e5834f666f36fa1f98e18f53fb4140b336ba090a923`.
Default intake reported Complete=true, Whole=false and no measured set version;
application cleanup retained every original. No downloaded code was executed,
and no book eligibility or broad conformance was established.

## Decision

Add RAR4SetCoverage as an internal explicit opt-in requiring both Coverage and
RAR4Coverage. All default false. Keep application RAR4SetCoverage false and its
required root-set version 5 unchanged. RAR5SetCoverage cannot request RAR4 facts,
and RAR4 flags cannot request RAR5 facts. With both version-specific set flags
enabled, select only from each checked private snapshot's measured signature.
Old naming and every nested volume set remain outside this coverage contract.
No public option, decoder, module, cgo or new executable is added.

Share the existing standalone RAR4 header/CRC/name/metadata/EOF implementation.
RAR4Accounted must continue to refuse every volume/split declaration. Separate
RAR4VolumeAccounted returns physical spans, main first/solid roles, end ordinal
and continuation role, split flags and comparable declaration metadata. Require
modern main volume/naming flags and an explicit end ordinal consistent with
the first-volume bit. The end may contain exactly seven reserved zero bytes when
declared. Refuse unknown flags, nonzero reserved bytes, truncated/extra fields,
unknown whole sizes, directory data, hidden service/comment/recovery/encryption
records, gaps and tails. Volume-wide CRC declarations remain refused: their
protected byte range has not been independently verified for this contract.
Keep the existing bounded ASCII names, attributes, compression declarations,
zero high sizes and time metadata; this adds no new metadata subset.

Use a shared format-neutral continuation aggregator and exact reader-table
matcher for both RAR versions. Preserve every RAR5 condition and result. Each
format adapts its own facts and declaration fingerprint, never converts RAR4
into RAR5 authority. Compare exact names, known whole sizes, compression/version,
attributes, times and other supported metadata across adjacent continuations.
Keep nonfinal packed CRCs distinct from the final whole CRC. Close every split
chain, check packed sums and complete stored sizes, preserve complete siblings
and empty directories, and refuse duplicate or parent-conflicting selections.

Reuse the same private snapshot, expected-source and shared-work mechanisms.
Before exposing any siblings, frame each opened physical volume and bind every
file record to its isolated UTF-8 table, exact split roles and ordinal. Bind the
complete logical facts and starting-volume indices to the same strict assembled
table used for exact-name stdout extraction. Keep Whole, output integrity,
Complete and member eligibility independent. RARSetVersion=4 is root-set evidence
omitted from JSON; it cannot populate standalone RARVersion/RARContainers.

Proof/read, source-copy/hash, member/output, process/diagnostic and deadline
budgets remain shared across all parts, including failed attempts. Direct calls
get the same work budget without silently enabling traversal. Retain the exact
parent/alias rules, 2..1000 parts, 4 GiB total snapshots and cancellation checks.
Keep default intake and every existing cleanup proof's meaning unchanged.

## Verification required

Use synthetic Go fixtures and injected readers/scanners without installed tools
or network in tests. Cover 2/3/more parts, stored/compressed declarations,
optional reserved end fields, known high-size/time metadata, complete siblings,
empty directories and chains ending/starting in one volume. Exercise each
truncation, corrupt CRC, hidden block, unknown flag, ordinal/end/split/metadata
contradiction, unknown size, malformed table, output mismatch, alias, duplicate,
limit including failed work and cancellation boundary. Verify explicit flag
combinations, unchanged RAR5 evidence and standalone refusals, old/nested set
refusal, and an all-held RAR4 set still preserved by application cleanup.

Separately verify generated stored and independently known compressed native
sets and hidden material outside the checkout, with every source unchanged.
Require canonical verifier, full native-cgo race, Windows cross-vet, docs checks
and actual all-six hosted CI on the exact final commit. Post/read acceptance
while issue 57 remains open before any later writer decision or application edit.
Such a writer needs a distinct proof, whole-set fresh purge, immutable partial
recovery without reconstruction/adoption, actual preceding-binary retention and
raw-restore evidence and its own full gate. Keep stage 2 and issue 57 open.
This does not authorize a release.

Native Linux validation through the opt-in returns the exact stored PDF and
independently regenerated compressed reference bytes, including a zero-reserved
end variant. A mixed set returns complete first/last-volume siblings and an
empty directory with exact starting-volume bindings. A valid middle-volume
service record is silently omitted by the reader, while independent framing
refuses it before output. Default intake and application retention remain
unchanged, including all-held valid sets; every original is byte-identical.
These observations establish bounded framing/reader behavior, not eligibility,
broad conformance, safety or cleanup acceptance. Full acceptance remains tracked
in issue 57.
