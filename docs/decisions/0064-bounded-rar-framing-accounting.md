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
records, encryption, volumes and split members, unknown flags or unsupported extra records,
unsupported metadata, directory payloads, gaps, tails, malformed framing and
ambiguous names. Never infer coverage from a successful member extraction.
The supported field subset and limits belong in code and adversarial fixtures.
The result establishes framing facts only, not eligibility, integrity, safety,
completeness or permission to change a source.

The first compressed synthetic reference also carries routine metadata. A
read-only extension may account for the known skip-on-update common flag on
recognized block types, main locator records whose declared offsets are zero,
and file time records with exact flag/width/precision checks. Unknown blocks
remain refused even when marked skippable. Nonzero locators, archive metadata,
encryption, hashes, redirection, ownership and other extra records remain outside
this subset. Reject duplicate records, malformed sizes, unknown flags and invalid
nanoseconds. This extension must pass its own synthetic, native, fuzz and complete
local/hosted verification before cleanup can be enabled.

The compressed native reference is libarchive's generated `test.bin` pattern
[fixture](https://github.com/libarchive/libarchive/blob/03b426003adf12c1eb4c34e72bedb4c2bf1ba8d3/libarchive/test/test_read_format_rar5_compressed.rar.uu), checked against its
[test source](https://github.com/libarchive/libarchive/blob/03b426003adf12c1eb4c34e72bedb4c2bf1ba8d3/libarchive/test/test_read_format_rar5.c), retained outside
the checkout with its source and license. Its 436 source bytes have SHA-256
`68f9d7e2662cd5bc5607ba9c6bf1edbf121cf3b08011a7d8643091791750a7c8`.
Installed 7-Zip 26.04 returned the exact independently regenerated 1,200 bytes.
The extended accountant reports the file header at 24, data at 67, 361 packed
bytes and the same declared size and CRC. This is a framing/reader experiment,
not a claim of book validity or production cleanup. No upstream fixture or
decoder is bundled; first-party tests remain synthetic Go builders.

Keep existing RAR preservation in force while that read-only implementation is
verified. Before any subset can authorize cleanup, bind every physical file
record to the maintained reader's exact selection, size, packed bytes, directory
state and CRC, using the same private snapshot. Bind the snapshot to expected
source size and SHA-256. Require every leaf held with currently verified stored
bytes and unchanged final originals. Share existing tree proof/read/member/output,
diagnostic/process and deadline allowances, including failed work.

Verify the maintained-reader binding as a separate read-only step while the
library still refuses every RAR cleanup. Only a requested standalone root RAR5
may receive coverage evidence; child RARs remain refused. Probe the opened private
snapshot before listing, use one UTF-8 technical listing, and bind every unique
raw name to known size, packed size, directory state and file CRC. The complete
record set must agree, without normalized aliases, duplicate names, links,
special files, encryption or split records. Reader ordering confers no authority:
the existing reader selects members by exact name. Successful extraction and
physical coverage remain independent. Other formats and default intake keep
their existing paths. Require complete verification of this binding before a
later application decision enables source cleanup.

Preserve historical RAR entries too: a missing proof cannot authorize resuming
an earlier cleanup or purging its held archive copies. Guard every file of a
RAR archive entry, including old-style continuation names, through the same
versioned archive checks. Fresh previews retain these copies; pending unversioned
purge intents cannot remove them. A supported proof is required before any later
permission. Restore still returns recorded original bytes without replacing a
target. This restriction does not enable RAR cleanup or migrate old manifests.

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
