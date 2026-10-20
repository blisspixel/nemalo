# 0064: bounded RAR framing accounting beside the maintained reader

Status: accepted, 2026-10-20. Implementation and verification in progress in
[issue 57](https://github.com/blisspixel/nemalo/issues/57), roadmap stage 2.
No RAR cleanup capability is established by this decision alone.

## Evidence and scope

The [RARLAB format reference](https://www.rarlab.com/technote.htm) distinguishes
file data from service records and optional metadata. The installed reader's
[26.04 RAR5 handler](https://github.com/ip7z/7zip/blob/26.04/CPP/7zip/Archive/Rar/Rar5Handler.cpp)
omits non-stream service records from ordinary file references. A generated
archive containing such a record produced the same member listing and extracted
book as an archive without it, while its full listing reported a matching
physical size, no warning and a successful archive test. Native experiment
details and hashes are recorded on the issue. File listing or test success
cannot establish complete source coverage.

The ordinary `rardecode/v2` v2.4.5 file API also skips non-file records. UnRAR's
normal DLL searches for file headers; its technical CLI can show services but
does not supply a complete physical-span and extra-field inventory. Neither
examined API closes this gap by replacing the existing adapter.

## Decision

Retain installed 7-Zip for format parsing, decompression, exact member selection,
CRC and output-size checks. Narrow decision 0056's restriction against a new
parser to permit an independent, read-only Go framing accountant. It is not a
decoder or an alternative extraction, import or cleanup policy. Prefer the
standard library and shared proof-reader mechanism; add no module, executable,
cgo requirement or non-Go tooling.

First implement a strict standalone RAR5 subset. Measure checked header and data
spans from the initial signature through an exact end marker, bound work and
allocation, and return the declared file facts. Refuse service/comment/recovery
records, encryption, volumes and split members, unknown flags or extra records,
unsupported metadata, directory payloads, gaps, tails, malformed framing and
ambiguous names. Never infer coverage from a successful member extraction.
The supported field subset and limits belong in code and adversarial fixtures.
The result establishes framing facts only, not eligibility, integrity, safety,
completeness or permission to change a source.

Keep existing RAR preservation in force while that read-only implementation is
verified. Before any subset can authorize cleanup, bind every physical file
record to the maintained reader's exact selection, size, packed bytes, directory
state and CRC, using the same private snapshot. Bind the snapshot to expected
source size and SHA-256. Require every leaf held with currently verified stored
bytes and unchanged final originals. Share existing tree proof/read/member/output,
diagnostic/process and deadline allowances, including failed work.

An enabled subset needs its own explicit versioned proof and the reviewed,
recoverable cleanup/restore and grouped-purge contracts of decisions 0022, 0041
and 0063. Fresh purge repeats complete accounting. Partial recovery binds the
original supported immutable proof plus fresh remaining bytes and holdings;
it never reconstructs a partly purged set or adopts replacements. Earlier
executables must retain or refuse unfamiliar evidence before source writes.
Do not broaden `archive-tree/1` to include RAR implicitly.

RAR4, supported volume layouts and nested RAR coverage need separately specified
subsets and tests. A standalone RAR5 proof does not complete stage 2 or authorize
any of them. Unsupported and uncertain inputs preserve the whole source/set.

## Verification before enabling cleanup

Use synthetic Go fixtures and injected readers/scanners. Exercise malformed
lengths and integers, header checksums, record/reader mismatches, every unsupported
block and extra field, directory data, aliases, limits including failed reads,
cancellation, source changes and recovery bindings. Fuzz the framing boundary.
Separately test generated archives outside the checkout with an installed
maintained reader, including records its ordinary listing omits.

Require the canonical verifier, native-cgo race, docs checks and actual hosted
Linux/macOS/Windows jobs for the read-only implementation before enabling a
write-capable subset. That integration then needs its own complete verification
and hosted acceptance on the exact commit. Keep planned, implemented, tested
and accepted behavior distinct. No release or later roadmap stage is included.
