# 0027: explicit local bookmarks

Status: accepted and implemented, 2026-10-12. Optional local bookmarks from the
roadmap. Retrieval stays stateless ([content access](../CONTENT-ACCESS.md)).
The terminal reader still stores nothing
([decision 0025](0025-terminal-reading.md)).

## Decision

Bookmarks are explicit saved locations in an initialized library. They are not
reading acknowledgements. Retrieval, preview, retry, repeat, preparation, time
passing, and shutdown do not create, move, or confirm one. There is no
acknowledgement operation: delivery does not mean the text was read.

`bookmark enable LIBRARY` creates `.nemalo/bookmarks.jsonl` and is idempotent
when the journal is already enabled. `bookmark disable` keeps the records and
refuses save, resume, and import. List, export, note, and delete still work.
Until the file exists, every operation except enable fails with a message that
names `bookmark enable`. A library whose control directory contains this file
cannot be opened by an earlier prerelease. A library without the file stays
compatible. `library status` does not parse the file: a corrupt bookmark
journal fails bookmark operations and leaves holdings and status available.
`library check` does not walk `.nemalo/`.

The file is an append-only canonical JSON journal, the same durability class
as the other control journals: each record is synced; directory-entry
power-loss durability depends on the filesystem. The allowlist in
[decision 0006](0006-library-control-state.md) includes this name. The
initialization journal stays two records. Events are `enabled`, `disabled`,
`save`, `note`, and `delete`, with schema version, sequence, and library id.
The first event is `enabled`. A save while disabled fails replay. Location
fields are interpreted on save events. Unit and offset are always present,
including zero. Note is always present, including empty, so a cleared note is
visible. Cursor is omitted when empty.

One current location is kept per asset, the latest save. An identical save
(same representation, representation id, unit, offset, and cursor) appends
nothing. A new position gets a new id and copies the note onto that save.
`bookmark note` replaces the note, and an empty note clears it. A note needs
a current bookmark. `bookmark delete` appends a delete for the current id and
keeps the history; deleting an asset that has no bookmark writes nothing.
Note and delete name the current id; a journal where they do not is
`needs_review` and is not rewritten.

A note is at most 2000 bytes of UTF-8 without NUL. It is the reader's text.
It is never written into the book and is returned even when the location
conflicts. Representations are checked in the application service against
content capabilities: a representation must offer `read`, and `epub-source/1`
cannot be saved. `epub-nav/1` requires the cursor from
[decision 0026](0026-epub-navigation-and-chapter-selection.md). `mpeg-audio/1`
without a cursor is only the start (unit 0, offset 0). The caller passes the
representation id it observed. The save reads the bytes and refuses when that
id, unit, or offset does not match. It does not substitute the current id.

`bookmark resume` returns `restored` with text only when the asset, the
representation id, and the locator still match. `changed_source` means the
stored bytes changed. `unavailable` means the asset is missing. `conflict`
means the representation or cursor does not match, including an
`epub-nav/1` location without a cursor and a PDF representation saved against
an EPUB. `not_enabled` means the journal is disabled. Those results return no
source text and do not guess another passage by title or nearby bytes. Another
asset's bookmark still resumes. A later content read does not move a bookmark.

Export writes `nemalo-bookmarks/1` (schema version 1, library id, current
bookmarks sorted by asset id) to a new file outside `.nemalo`, mode 0600,
created exclusively, synced, with a trailing newline. Import reads at most
1 MiB, rejects unknown fields, and writes nothing unless the library id
matches and every record is valid. Without `--replace`, a different location
or note fails the whole import and writes nothing; an identical place and
note is idempotent; a new asset is appended. With `--replace`, a different
location is a save carrying the imported note, and the same place with a
different note is a note event. Assets left out of the file stay. Duplicate
assets in one file fail the import. Import refuses a disabled or missing
journal.

A reader ignores a trailing partial line. A later append may truncate only
that incomplete tail, and only when the complete prefix parses, before it
appends. A complete invalid line or a journal over 1 MiB is left unchanged
and fails closed. A torn tail may extend past the cap by one record; the size
check runs after that repair, so a refused append can drop the torn tail and
still keep the complete records.

CLI commands are `bookmark enable|disable|list|save|note|delete|export|import|resume`.
The TUI save key is `m` and the restore key is `'`, in Read and in the pager.
`m` does not enable bookmarks. A snapshot catalog, or any session whose
catalog and root differ, writes nothing. Closing the pager does not save.
Restore of `epub-text/2` or `pdf-text/1` opens the pager at the saved unit
and offset. Restore of `epub-nav/1` opens that declared entry at the saved
cursor. MCP stays read-only and has no bookmark tool. An external client may
keep its own continuation; Nemalo does not require it to use this journal.
