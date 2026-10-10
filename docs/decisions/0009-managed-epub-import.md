# 0009: managed import of one assessed file

Status: accepted and implemented, 2026-10-09.

## Bounded increment

`library import LIBRARY SOURCE` assesses one explicit file, or one completed
intake packet, and can copy it into an initialized library. Without `--apply` it
only reports the plan. `--scan` may invoke the installed antivirus scanner.
CLI, JSON, and TUI use `internal/app` and `internal/library`. Human output uses
`internal/present`.

This stores one EPUB, PDF, or MP3. It does not extract archives, resume a
download, organize a folder, delete a source, or invent work, edition,
recording, or track identities. It is not the full checked-library policy.

The initialization journal from decision 0006 stays two records. Import uses
separate journals.

## Stored bytes and status

The library directory is the store. Bytes are written beside `.nemalo`, at
`assets/sha256/<2 hex>/<64 hex>.<ext>`, through a synced partial file and a
hard link. The filesystem must support a hard link inside that directory.
A matching stored file is kept. A different file at that path is left in place
and the import fails closed before a new intent is written. A stored file with
an unexpected extra hard link is rejected the same way. A partial file that is
hard-linked elsewhere is not adopted. Each store directory must be a real
directory. The source is not modified.

`checked` is recorded only for an EPUB whose limited checks passed, whose
recorded scan status is `no_detections_reported`, and which has no findings.
The reason is `checks_and_scan_recorded`. PDF, MP3, unscanned EPUBs, failed
checks, and scans that are not clean stay `review`, with a reason that names
the gap. When the EPUB checks passed and there is no other finding, a scan that
is not `no_detections_reported` uses `scan_` plus `unavailable`, `incomplete`,
`findings`, or `not_scanned`. Any other scanner status is `scan_unrecognized`.
Invalid or uncertain EPUB material can still be stored for review.
Review is not a safety or completeness guarantee.

A later import without `--scan` does not downgrade `checked`. With `--scan`,
new scan evidence can raise an EPUB to `checked` or return it to `review`.
The same hash is not stored twice. A new source path appends a holding line.
Rights and license strings are recorded declarations, not file rights.

## Journals, locks, and recovery

`.nemalo/operations.jsonl` (at most 4 MiB) and `.nemalo/holdings.jsonl` (at most
8 MiB) are optional regular files. Each record is at most 64 KiB, canonical
JSON, and synced before the next step. Missing files mean no managed import.
An empty file, unknown field, or broken pair is corrupt. Corrupt bytes are
kept and are not truncated or repaired. `library init` and `library status`
fail closed while those journals are corrupt.

Operations are pairs: `import_intent`, then `import_completed`. A single trailing
intent can resume only for the same source path. Another source is refused
until that import finishes. The intent is synced before the copy. A crash after
the intent, the partial, the link, or the holding can resume. If the source
bytes no longer match the intent, the journal is kept and the import fails.

Import takes the same exclusive writer lock as initialization. One import runs
at a time. Readers see busy state. File sync is required. Directory-entry
survival across power loss still depends on the filesystem.

`library audit LIBRARY`, when the argument is a non-link directory and no
snapshot `--root` or inventory limit is set, hashes managed holdings. It
reports missing, changed, over-budget, and review files, changes nothing, and
exits unsuccessfully when any finding remains. A snapshot catalog audit is
unchanged and still requires `--root`.

## Reading and the terminal

`content read LIBRARY --root LIBRARY --asset sha256:HASH` reads one managed
holding when both paths are the same absolute non-link library directory.
It rechecks size and SHA-256. It does not search the library. Retrieval does
not record reading and does not turn review into checked. A managed holding may
be read up to the 256 MiB import cap. Snapshot source reads stay 32 MiB.
Container member limits still apply.

TUI State keeps its place in the ten-operation list. Enter reads status. F7
reviews initialization. Ctrl+N sets one source, and F8 reviews import. A second
Enter applies it. Escape, F7, and F8 leave a review without a new write. The
terminal import does not scan, so it cannot newly mark a file checked.

## Validation

Synthetic fixtures cover preview, apply, a second source, scan upgrade,
no-downgrade, a named scan gap, PDF review, a completed packet, receipt
mismatch, crash resume, a busy writer, corrupt journals, conflicting bytes,
extra hard links, a non-directory store parent, managed audit, managed read,
and the TUI review. Personal downloads are not test fixtures.
