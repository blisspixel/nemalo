# Decision 0006: library identity and initialization journal

Status: accepted and implemented, 2026-10-08.

## Bounded increment

`library init DIRECTORY` establishes one library identity inside an existing,
explicitly selected folder. `library status DIRECTORY` reads that control state
without creating files. CLI, JSON, and TUI use `internal/app` and
`internal/library`; human output uses `internal/present`.

This is an initialization journal, not a mutable content catalog, general job
engine, or import/cleanup recovery system. Snapshot/list/audit keep their contracts.
Work, edition, recording, track, and asset relationships remain separate domain
work. Initialization does not inspect, import, assess, open, reorganize, or delete
any content.

## Control files and state

Only `.nemalo/` is created under the selected root, containing:

- `writer.lock`: a permanent empty file used for OS locks. Never unlink it to
  release an active lock.
- `journal.jsonl`: schema-version-1 canonical JSON records. The first assigns
  random namespaced library/operation IDs and records initialization intent.
  The second records completion, retaining those IDs.

Each record is written and `File.Sync` succeeds before proceeding. Repeating
completed initialization preserves its ID and journal bytes. A complete started
record can resume through an explicit `init`; `status` reports pending state and
exits unsuccessfully. Sequence determines order, independent of wall-clock skew.

Reads are bounded to 64 KiB and this schema permits exactly one or two records.
Canonical encoding, schema, sequence, IDs, timestamps, and transitions are
validated. Duplicate/unknown fields, trailing data, torn records, future versions,
and inconsistent IDs are rejected. Invalid state is never truncated, overwritten,
or silently repaired. A crash before the initial record is complete can leave an
empty/partial control namespace requiring review.

The directory must contain exactly the two expected regular files. Unexpected
artifacts, control symlinks/reparse points, hard-linked control files, and changed
file identities are rejected. Files open relative to `os.Root`; source content is
never opened. New control directories/files request Unix modes 0700/0600; Windows
access follows host ACL inheritance. The user must control library write access.
These checks are not a sandbox against a privileged or noncooperating actor
changing the filesystem concurrently.

## Locking and durability

Use nonblocking shared locks for status and exclusive locks for initialization.
Busy state fails explicitly without polling or PID-based stale-owner guesses.
Normal close unlocks before closing the descriptor. The OS releases locks after
process termination; a lock file's existence is not proof of a live writer.
Cross-process tests kill a writer after synced intent and resume the same ID.

Adapters use Linux/macOS `flock` and Windows `LockFileEx`/`UnlockFileEx` through
the existing `golang.org/x/sys` v0.48.0 module. It becomes direct with no new
module/version/checksum or runtime. This small OS seam keeps lock identity attached
to an opened file. Reviewed the
[Linux manual](https://man7.org/linux/man-pages/man2/flock.2.html),
[Apple manual](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/flock.2.html),
and [Microsoft contract](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-lockfileex).

The supported target is cooperating processes on a local filesystem. Network
filesystems and multi-host coordination are not validated.
[File.Sync](https://pkg.go.dev/os#File.Sync) flushes file contents; this increment
does not guarantee directory-entry persistence through power loss. Reports expose
that limitation. Process-crash tests do not establish disk-full, power-loss, or
hardware durability. Broader recovery and directory-flush evidence remain gates
before production publication or source cleanup.

## Terminal contract and validation

TUI operation 9 reads status on Enter. F7 displays the exact absolute root and
write scope before a second Enter initializes it. Fields/navigation freeze during
review; Escape returns without writing and review text can scroll. No provider
request, scan, or automatic initialization occurs.

Synthetic tests cover first/repeated/resumed initialization, cancellation,
reader/writer exclusion, process death, replaced descriptors, corrupt/oversized
journals, linked files, unknown artifacts, source preservation, CLI exit codes/JSON,
and TUI review/dispatch/resize. Native CI supplies separate platform evidence.
Future operations must reuse this state seam and deliberately version the journal
before introducing other records.
