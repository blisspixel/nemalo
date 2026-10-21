# 0069: bounded RAR4 time and zero-high-size metadata

Status: accepted and implemented, 2026-10-20. Acceptance evidence is tracked in
[issue 57](https://github.com/blisspixel/nemalo/issues/57), roadmap stage 2.
This extends read-only framing evidence and enables no RAR4 cleanup.

## Evidence

A native experiment adds only known header metadata to the independent compressed
reference from decision 0067: zero high packed/unpacked-size words, an extended
modification time, or both. The packed stream and independently regenerated
64-byte output stay identical. Installed 7-Zip 26.04 lists and extracts every
variant successfully, reporting the expected declared modification time. The
initial strict accountant correctly refuses these additional file flags.
The combined 579-byte variant has SHA-256
`4a5a73ac3e48dd3f1586fae54c5dba3baf8740dab695d63323bdbdfb9608b307`.
Its header remains at 20, data starts at 82 and the same 490 packed bytes return
the exact 64-byte pattern. Stored generated-book and directory variants also
pass the opted-in reader check while application cleanup still moves none.

The maintained [26.04 RAR4 handler](https://github.com/ip7z/7zip/blob/26.04/CPP/7zip/Archive/Rar/RarHandler.cpp)
places the high words before the name and extended times after it. Its bounded
field definitions and read order establish this extension's layout. Do not copy
or port its decoder, or treat the reader's permissive handling of missing fields
as complete accounting.

## Decision

Account for two additional known file flags in `RAR4Accounted`, before any RAR4
application proof exists. A high-size declaration requires both fixed high words
present and zero. This does not add large-file or larger-source support: all
existing source, output and work limits remain. Nonzero high words stay refused.

An extended-time declaration requires its complete two-byte mask. Account for
modification, creation and access time fields in the declared order. Modification
uses the existing fixed DOS time; creation/access each require a fixed DOS time
when present. A present field has its known optional odd-second bit and zero to
three fractional bytes. For this subset, accept only fractional representations
below one second, with the format's declared precision. Other representations
stay unsupported rather than being normalized or guessed.

Require presence before lower time bits, exact field widths and exact exhaustion
of the header metadata. Refuse the fourth time-mask nibble, truncated fields,
dangling bits, unknown metadata and trailing bytes. A missing extended-time area
never means default metadata. Return declared facts only, with no generated time
interpretation, payload reads, decoder or write authority.

Keep Unicode-name encodings, salts, comments, old versions, split/volume records,
service/subblocks and every other unsupported flag refused. Names remain the same
bounded ASCII subset, and directories still require zero data/CRC and consistent
type. Framing, reader-table and output checks remain independent. The explicitly
opted-in reader binding reuses these measured fields; application services keep
the RAR4 gate closed. Never broaden the RAR5 or tree cleanup proofs.

## Verification

Use synthetic Go fixtures for each accepted field order, time presence/precision,
zero high words, every truncated mask/field, nonzero high words, unsupported
fractions/nibbles and trailing metadata. Preserve CRC, size, alias, directory,
context and shared-budget refusals. Fuzz checksum-aware time metadata as well as
whole framing. Compare generated native stored books/directories and the compressed
reference variants outside the checkout; all unsupported sources stay unchanged.

Require canonical verifier, native-cgo race, docs checks and all-six hosted jobs
on the final commit. A later RAR4 application decision needs a distinct proof,
signature-bound selection and separately verified recovery. Volume and nested
RAR coverage remain unfinished, and this extension does not complete issue 57.
