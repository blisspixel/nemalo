# 0016: readers and players receive verified copies, never stored files

Status: accepted and implemented, 2026-10-11. The "initial local access" part of
the roadmap's access workstream: find a holding, then open or export it.

## Problem

A stored file is identified by its bytes. Some readers write into the files they
open: the calibre viewer stores highlights inside the book file, and stores EPUB
bookmarks there unless that is turned off. Others import or copy books (Thorium
copies them into its own folder; Apple Books imports them). Handing a reader the
stored file could silently change a checked holding.

## Decision

- `library open LIBRARY --asset ID [--app APPLICATION]` (TUI: `o` on a managed
  holding) copies the holding into the Nemalo handoff folder, `<state>/handoff`,
  then opens the copy. The copy is hashed while it is written, must equal the
  holding's size and SHA-256, is synced, and is made read-only. An identical
  earlier copy is reused. A copy that a reader changed is left in place and a
  fresh copy is made beside it, so the reader's annotations are not destroyed.
- `library export LIBRARY --asset ID --output DIRECTORY` makes the same verified
  copy, writable, in an explicit existing directory, and never overwrites.
- Copies are named from the declared title, reduced to letters, digits, and
  hyphens, plus a hash prefix. A title can never become a path.
- The stored file is opened only through the library's capability-bound root
  and only read. Bytes that no longer match the holding produce no copy and
  point to `library audit`.

## Launching

Without `--app`, Linux runs `xdg-open` and macOS runs `/usr/bin/open`, each with
an argument array, and Windows calls `ShellExecuteW` with no parameters on a
COM-initialized thread. `cmd /c start` is never used, because cmd.exe parses a
file name differently from argument quoting. `--app` starts the named
application with the copy's absolute path as its only argument. A successful
launch means the application started, not that it opened the file. Opening is
not recorded as reading or listening. If the launch fails, the result still
names the verified copy.

## Not covered

MCP tools stay read-only, so neither operation is exposed there. Handoff copies
are not tracked or cleaned up automatically; they are disposable copies in
Nemalo's own state folder. Sync with a reader's annotation store is out of scope.
