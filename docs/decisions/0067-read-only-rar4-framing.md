# 0067: read-only accounting of bounded standalone RAR4 framing

Status: accepted and verified, 2026-10-20. Evidence is tracked in
[issue 57](https://github.com/blisspixel/nemalo/issues/57), roadmap stage 2.
This decision enables no RAR4 cleanup or extraction coverage authority.
The separate [decision 0068](0068-opt-in-rar4-reader-binding.md) specifies the
explicitly opted-in reader binding, with the application gate still closed.

## Evidence

Generated RAR4 archives with and without new and old subblocks return the same
ordinary file listing and exact book through installed 7-Zip 26.04. Their full
physical sizes match, no warning appears and archive tests succeed. Every source
stays byte-identical and current cleanup moves none. The native experiment and
hashes are recorded in [issue comment 6304377957](https://github.com/blisspixel/nemalo/issues/57#issuecomment-6304377957).

The maintained [26.04 RAR4 handler](https://github.com/ip7z/7zip/blob/26.04/CPP/7zip/Archive/Rar/RarHandler.cpp)
and [header definitions](https://github.com/ip7z/7zip/blob/26.04/CPP/7zip/Archive/Rar/RarHeader.h)
distinguish ordinary file headers from comment, subblock, recovery and other
records. The reader skips non-file long-block data by its declared size. Its
file API and test success therefore cannot establish whole-container coverage.
Inspecting these primary implementations establishes format facts; do not copy
or port a decoder.

## Decision

Under the user-authorized framing approach of decision 0064, develop an independent
read-only RAR4 accountant beside the maintained reader. Add no decoder, dependency,
executable, cgo or network requirement. Return measured header/data spans and
declared file facts, never payload integrity, completeness or write permission.
Do not wire the result into application cleanup or staging coverage in this step.

Start with one exact signature at zero, a fixed 13-byte main header, bounded
ordinary file headers and an exact seven-byte end header at EOF. Verify the
format's truncated header CRC32 and every length before allocation or offset
advance. Refuse every non-file service/comment/subblock/recovery/authenticity
record, encrypted or volume declaration, split member, unsupported header/extra
field, gap, tail and ambiguous name. Known lock/solid flags and skip-if-unknown
on recognized blocks carry no additional data; unknown blocks remain refused.

The initial file subset uses 32-bit declared sizes, ASCII names with the shared
portable name and alias checks, supported DOS/Windows or Unix attributes, known
stored/compressed method values and the maintained reader's bounded RAR1/2/3
version range. Stored sizes must agree. Directories require consistent type,
zero packed/unpacked size and zero CRC. Unicode-name encodings, high-size fields,
salts, old-version declarations, comments and extended times remain refused until
an explicitly measured extension. Fixed declared timestamp bytes are metadata,
not generated interpretation.

Reuse the lifetime 16 MiB proof reader, maximum 4 GiB source, 10,000-file and
bounded-block limits. Check context cancellation during work. Failed reads stay
charged. Any future caller sharing tree work must additionally charge its existing
proof/read budgets. Source bytes remain untouched and payloads are never read.
Keep default intake, all RAR4 cleanup and requested RAR4 tree coverage unchanged.

## Independent compressed reference

Libarchive's generated [low-distance-reset fixture](https://github.com/libarchive/libarchive/blob/03b426003adf12c1eb4c34e72bedb4c2bf1ba8d3/libarchive/test/test_read_format_rar3_lowdist_reset.rar.uu)
is checked against its [test source](https://github.com/libarchive/libarchive/blob/03b426003adf12c1eb4c34e72bedb4c2bf1ba8d3/libarchive/test/test_read_format_rar.c),
retained outside the checkout with its license. The 566 source bytes have SHA-256
`45ca17d52b91a6f24c48fe5d79b04cb26052f7e1d732d8bf8e459c433540f916`.
The accountant measures the file header at 20, data at 69, 490 packed bytes,
64 declared output bytes, version 29, method 53 and CRC `6FF838DC`.
Installed 7-Zip 26.04 returns the exact independently regenerated 64-byte pattern,
SHA-256 `353d5f7a0789034186922e5834f666f36fa1f98e18f53fb4140b336ba090a923`.
These are framing and reader-output observations, not book validity, file-type
eligibility or cleanup authority. No upstream fixture or decoder is bundled;
first-party tests use synthetic Go builders. Requested RAR4 extraction coverage
still refuses this reference.

## Verification and later work

Use synthetic Go fixtures for exact spans, supported fields, every truncation,
header corruption, malformed sizes/flags/attributes/names, directory payloads,
non-file records, tails, bounds, cancellation during reads and final name/prefix
validation, and failed/shared reader work. A large valid name set must not return
framing evidence after cancellation during its final validation. The shared
RAR5 name checks obey the same cancellation contract.
Fuzz raw framing and checksum-aware headers. Compare independently generated
native archives with the maintained reader outside the checkout, including its
unlisted records. Keep installed tools and scanners out of core tests.

Require canonical verification, native-cgo race, docs checks and actual all-six
hosted jobs for this read-only implementation. Later reader binding and application
permission require their own complete verification, distinct supported proof and
reviewed recovery contract. Never extend `rar5-frames/1` or `archive-tree/1` to RAR4
implicitly. Volumes and nested RARs require separate measured subsets; this step
does not complete issue 57 or roadmap stage 2.
