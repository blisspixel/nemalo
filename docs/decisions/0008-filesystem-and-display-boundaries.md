# 0008: bound filesystem capabilities and portable display encoding

Status: accepted and implemented, 2026-10-08.

## Filesystem acquisition

Use `internal/safeio` for root acquisition and existing regular-file opens. Validate
opened identity/type before reading or writing. Inventory and optional assessment
retain the same source root; publication retains the validated output parent.
Physical containment checks bind resolved names to captured directory identities.
Explicit directory aliases remain supported for file-parent/content access;
inventory roots and control directories retain their non-link contract.

Linux/macOS acquire directories with directory/nonblocking flags and open `os.Root`
through a held descriptor. Linux requires `/proc/self/fd`; macOS requires `/dev/fd`.
Missing descriptor facilities fail explicitly, without a mutable-path fallback.
Windows captures path identity through an attributes-only handle before comparing
the opened directory capability. Regular-file opens are nonblocking on Unix and
validated immediately on every platform. No extra module or subprocess is needed.

These controls do not freeze directory locations, defeat hostile mounts, sandbox
same-user processes, or make stalled storage cancellable. User-facing source paths
are retained separately from descriptor bridge names.

## Parsing and presentation

The shared EPUB parser preflights compressed-data bounds and overlap before
decompression. Expanded limits, a lifetime 1 GiB compressed-read work ceiling, and
a five-minute assessment deadline are distinct limits. Reader-level cancellation
and work accounting remain active through member reopens and content visitors.
These are bounded parser checks, not full EPUB conformance or a safety guarantee.

`internal/textsafe` is the common output encoder for JSON and generated prose.
Terminal-sensitive control/format characters are represented visibly; JSON stays
parse-equivalent, including object keys and supplementary Unicode. Source data is
preserved. Ordinary multilingual text and deliberate prose whitespace remain.
