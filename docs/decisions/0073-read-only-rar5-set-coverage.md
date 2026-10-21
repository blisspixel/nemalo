# 0073: opt-in read-only RAR5 volume-set coverage

Status: accepted, 2026-10-21 UTC. Implementation and verification tracked in
[issue 57](https://github.com/blisspixel/nemalo/issues/57), roadmap stage 2.
This decision grants no new cleanup or purge authority.

## Prerequisite and evidence

The nested standalone writer passed all six hosted jobs at `c193024` in
[run 39538904741](https://github.com/blisspixel/nemalo/actions/runs/39538904741).
[Receipt 6307636974](https://github.com/blisspixel/nemalo/issues/57#issuecomment-6307636974)
was read back while 57 remained open, before this increment.

The [RAR5 format](https://www.rarlab.com/technote.htm) distinguishes a nonfinal
split part's packed-data CRC from the final part's whole unpacked-file CRC.
The [7-Zip 26.04 handler](https://github.com/ip7z/7zip/blob/26.04/CPP/7zip/Archive/Rar/Rar5Handler.cpp)
sums linked packed sizes and reports the final complete record's size and CRC.
These contracts were checked on 2026-10-21. They are references, not incorporated
code. Keep the installed maintained reader; do not add a decoder or dependency.

Native Linux prerequisites with installed 7-Zip 26.04 used a generated stored
three-volume PDF and a three-volume reframing of an unchanged, independently
known compressed payload. The latter returned the exact regenerated 1,200-byte
pattern from the external reference recorded in decision 0064. The default shared
intake returned Complete=true and Whole=false for both. No downloaded code was
executed and no book eligibility, general conformance or cleanup was established.

## Decision

Add an explicit internal RAR5SetCoverage opt-in alongside Coverage for modern
root RAR5 sets. Both default false. RAR4 sets, old naming and every nested set
remain outside this coverage contract; their existing intake remains unchanged.
The application keeps its early RAR-set cleanup refusal and leaves the new flag
false. No public option, network action, new module or runtime executable is added.

Share the strict standalone header, metadata, CRC, name and EOF mechanisms.
RAR5Accounted must continue to reject volume and split flags. A separate
RAR5VolumeAccounted returns physical file spans, ordinal, split roles, end role
and compatible declaration metadata. Each volume must start at its initial
signature and reach exact EOF through known records. Preserve unsupported
flags/metadata, service/recovery/encryption blocks, unknown sizes, directory
data, aliases, gaps and tails. Per-volume facts alone do not establish a set.

Require the existing checked snapshots and isolated reader format, role, ordinal,
size and boundary checks before exposing siblings. Bind every physical record
to its exact UTF-8 isolated table, including packed size, CRC kind and split
flags. Require consistent metadata through each adjacent continuation, all
continuations closed, and unique complete member selections. Nonfinal packed
CRCs need not equal the final whole CRC. Declarations do not measure payload
integrity. Sum packed sizes with checked bounds; stored complete chains must
match their whole size. Reject missing, duplicate, orphan or unlinked parts.

Rename only those same private snapshots into the assembled set. Require the
existing strict error-free archive block with exact format, first/total physical
sizes and volume count. Bind all logical facts to the same complete UTF-8 table
that drives exact-name stdout extraction. Do not fetch a replacement table.
Preserve output size/CRC/hash checks, eligibility and Complete independently of
physical Whole evidence. A separate internal, JSON-omitted RARSetVersion must
not populate the standalone RARVersion or RARContainers facts.

Share proof/read/member/output/process/diagnostic/deadline budgets across all
volumes, including failed work. Direct set calls also need one shared budget;
never reset the 16 MiB proof allowance or reader work per volume. Snapshots total
at most 4 GiB and a set has 2 to 1,000 parts. A shared work budget alone must
not enable nested traversal in direct set calls; only ExtractTree traverses.
Preserve expected-source hash binding and cancellation in framing, name and
continuation aggregation.

Existing rar4-frames/1, rar5-frames/1 and archive-tree/1 and /2 retain their
meanings. A later writer requires a distinct set proof, a recorded decision
AFTER this read-only capability's full acceptance, fresh whole-set grouped
purge, immutable partial recovery without joining/adoption, actual preceding
binary retention/raw-restore evidence and its own full gate. Keep issue 57 and
roadmap stage 2 open. This does not authorize a release.

## Verification

Use synthetic Go fixtures and injected readers/scanners, never installed tools
or public network in tests. Cover 2/3/more parts, complete siblings and empty
directories, stored/compressed declarations, explicit/default flags, exact
physical/logical tables, unchanged standalone refusal and application retention.
Refuse hidden records, contradictory ordinals/end/split/metadata/checksum roles,
unknown/duplicate/overlapping members, truncated or extra bytes, source/reader
mismatch and incomplete output. Test shared limits across successful and failed
work, cancellation and unsupported nested/old/RAR4 sets. Separately validate
stored and independently known compressed native sets, with every source kept.
Require canonical verifier, full native-cgo race, Windows cross-vet, docs checks
and actual all-six hosted CI on the exact final commit. Post/read acceptance
while issue 57 is open before any later application change.

Native Linux validation with installed 7-Zip 26.04 returns exact generated stored
and independently regenerated compressed output through the new opt-in. A mixed
set also returns complete siblings on the first/last volumes and an empty
directory, with exact starting-volume bindings. A checksummed service record inserted
in a middle volume is silently omitted by the maintained reader's assembled
listing, but independent framing refuses it before extracted output. All source
bytes remain unchanged. These are bounded reader/framing checks, not broad format
conformance, file eligibility or cleanup acceptance.
