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

The pinned Go toolchain reports a junction as `ModeIrregular`; inventory therefore
reports it as `special`/`not_opened`, while symbolic links are `link`/`not_followed`.
Both are excluded from traversal and strict directory roots. Explicitly selected
Windows aliases resolve through `GetFinalPathNameByHandleW` on an attributes-only
target handle, because `filepath.EvalSymlinks` leaves junctions unresolved. The
resolved root must still match the identity captured before resolution. No mutable
pathname fallback is used if the native query fails or exceeds its fixed buffer.
This follows Microsoft's [final-path handle contract](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-getfinalpathnamebyhandlew).

These controls do not freeze directory locations, defeat hostile mounts, sandbox
same-user processes, or make stalled storage cancellable. User-facing source paths
are retained separately from descriptor bridge names.

Windows boundary fixtures create local directory junctions through the existing
`golang.org/x/sys/windows` adapter and symbolic links through Go, without a shell
or installed tool. The fixture verifies each native reparse tag before testing it.
`TestWindowsReparse` cases require actual creation, with no privilege-based skip.
Native Windows CI runs them verbosely as well as in the verifier and race suite.
They cover strict versus explicitly resolved roots, opened file/directory identity,
inventory hash/PAR2 reads, ingest, stored assets, cleanup, restore, purge, and a
pending cleanup whose recovery parent was replaced. Outside canaries and retained
recovery copies must stay unchanged. Ordinary cleanup/restore/purge is exercised
beside link refusals. These tests establish the tested local filesystem and tags;
they do not establish every Windows filesystem, remote share or reparse filter.

Fixture format references (reviewed 2026-10-20): Microsoft's
[mount-point buffer specification](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-fscc/ca069dad-ed16-42aa-b057-b6b207f447cc),
[reparse operation](https://learn.microsoft.com/en-us/windows/win32/api/winioctl/ni-winioctl-fsctl_set_reparse_point),
and [junction overview](https://learn.microsoft.com/en-us/windows/win32/fileio/hard-links-and-junctions).

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
