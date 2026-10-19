# 0024: one read-only library check

Status: accepted and implemented, 2026-10-12. Completes the `check` acceptance
item of milestone 4: report missing, modified, invalid, and unscanned entries
without silently fixing them.

## Decision

`library check LIBRARY` (MCP `library_check`; TUI Audit with a library folder
and no root) takes the shared reader lock and writes nothing. It reports:

- every current holding's stored bytes, verified by size, SHA-256, and a single
  link (`missing`, `changed`), within a 64 GiB budget (`unverified` beyond it);
- files under `assets/` that no holding records (`untracked`), unfinished import
  copies (`partial`), and anything that is not a regular file (`unexpected`);
- unfinished operations: an import, rescan, cleanup, or restore;
- every cleanup's held copies against its manifest (`held_changed`), a manifest
  that no longer matches its journal record, and cleanup folders no journal
  names (`cleanup_orphan`);
- intact holdings that are not checked, counted by review reason, with the next
  step (for example `library rescan --apply` for missing scan evidence).

The library is healthy when there are no issues; review counts do not make it
unhealthy, since review is the expected state of PDFs, MP3s, and unscanned
books. The command exits 1 when the library needs attention. Files outside
`assets/` and `cleanup/` in the library folder are the user's and are not
examined. `library audit LIBRARY` remains as the narrower holdings comparison.

## Interrupted journal writes

Added 2026-10-19. A journal whose last line has no line feed was cut short by
a crash or a full disk while that line was written; it holds no record.
Readers use the complete lines, `library check` reports
`interrupted_journal_write` for that journal, and the next writer, under the
writer lock, removes the unterminated bytes before it appends. More than one
record's worth of unterminated bytes (64 KiB) is not a torn write: it stays
for review and nothing is cut. Tests fill the disk during each of an import's
three journal writes, then check, list, and finish the import.

## Validation

Synthetic tests cover a healthy library with review counts and its next step;
a changed and a missing stored file, an untracked file, and a leftover partial,
none of them changed by the check; a tampered held copy, an orphan cleanup
folder, and a pending cleanup; a pending import; an uninitialized folder; and
the CLI, JSON, and TUI paths.
