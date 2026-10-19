# 0022: recoverable source cleanup through a holding area

Status: accepted and implemented, 2026-10-11. The first increment of milestone 4,
over folder ingest ([decision 0017](0017-folder-and-zip-ingest.md)) and managed
holdings ([decision 0009](0009-managed-epub-import.md)). Settles remaining
implementation choice 2: held sources are retained until the user removes them;
there is no purge.

## Decision

`library cleanup LIBRARY FOLDER` (TUI: F9 in State) previews and
`library cleanup LIBRARY FOLDER --apply --plan DIGEST` applies; `library cleanup
LIBRARY --stop` closes a cleanup that cannot finish; `library restore LIBRARY
--cleanup ID [--apply]` undoes. Nothing is ever deleted outright:
cleanup moves a source into `<library>/cleanup/<id>/files/<folder-relative path>`.

- **Plan.** The folder and the library must not overlap, by path and by
  identity: both are compared as given and as resolved through links,
  junctions, and short names, case-insensitively on Windows and macOS, and every
  existing ancestor of each is compared with the other. A folder containing any
  `.nemalo` control directory is refused. A complete
  inventory of the folder (an incomplete one plans nothing) gives one entry per
  file or intake packet. A loose EPUB, PDF, or MP3 moves only when a current
  holding has its exact SHA-256 and that holding's stored bytes verify. An intake
  packet moves as its content, receipt, and intent only when its content is held
  and its receipt is a completed Nemalo receipt for exactly that content; a
  packet without a receipt stays for review, as in ingest. A ZIP moves only when
  it starts with an entry, has no comment, holds at least one held book, every
  other member is a directory, and its bytes are unchanged after the check. A
  file over its format's limit (256 MiB, or 2 GiB for an M4B or M4A,
  [decision 0040](0040-large-mp4-audio-and-scan-limits.md)), and names that
  differ only by case, stay. Everything else
  stays with a reason. The plan digest binds the folder path and every entry's
  disposition and file identities.
- **Apply.** Under the writer lock, after completing any interrupted rescan, the
  plan is made again and must have the reviewed digest, or nothing moves. Hard
  links are probed in the holding area and in the folder first, since moves and
  restores publish by link; a filesystem without them is refused before anything
  starts, and so is an operation the cleanup journal could not record in full
  (this cleanup and one restore of it), so a full journal can never leave a
  cleanup pending. A manifest of the plan is written and synced
  in the holding area, and its SHA-256 recorded in a `cleanup_started` line of
  `.nemalo/cleanup.jsonl`, before the first move. Each file is copied through an
  opened handle into a `.partial` name, verified against its planned size and
  SHA-256, synced, and published by hard link, which never replaces an existing
  name. The source is removed only afterwards, and only if its name still refers
  to the file that was read with the same size and modification time. On Windows
  a read-only attribute is cleared only when set and put back if removal still
  fails. An entry stops at its first file that is not held; its held parts stay
  restorable. The same steps hold within and across filesystems.
  One `cleanup_entry` line per entry records `held`, `source_changed`,
  `source_missing`, or `source_not_removed` (for example a protected or open
  file, whose held copy is kept); `cleanup_completed` closes the operation.
- **Recovery.** Repeating the apply with the same digest resumes a pending
  cleanup from its manifest, never from a new plan. Each step checks what an
  interrupted attempt left: a `.partial` is this operation's own and is redone,
  a held copy with the planned bytes is kept, and a source already removed after
  its copy was held is complete. A held copy with other bytes means the holding
  area was changed outside Nemalo and stops the cleanup for review. A folder
  removed meanwhile counts every remaining source as missing. A cleanup that
  cannot finish is closed with `--stop`, which records the rest as stopped. While
  a cleanup or restore is pending, no other cleanup or restore starts.
- **Restore.** Every held file of the cleanup, whatever its entry's outcome,
  returns to its original path through a randomly named partial and a link. An
  existing file with the same bytes counts as already present; different bytes
  are a conflict, and the held copy stays. A held copy is removed only after a
  separate folder copy is verified, never when the two are the same file. A
  leftover partial is removed only when it is the same file as the restored
  file; a user's file with a similar name stays. Each file is journaled as it
  finishes, so restore resumes and can be repeated. A path that cannot take a
  file (a file where its folder was, a link) blocks only that file, whose held
  copy stays; `library restore --stop` closes a restore that cannot finish.

## Consequences

A downloads folder can be emptied of everything the library holds and returned
exactly. Uncertain, unsupported, unheld, and partly held material is never
moved. Holding areas grow until removed by hand; a later explicit purge needs its
own preview and decision. Libraries with a cleanup journal cannot be opened by
earlier prereleases. Hard links are required in the library and in the folder
for restore, as for acquisition receipts. Directory-entry durability across power
loss depends on the filesystem.

## Validation

Synthetic tests cover the plan for loose, unheld, packet, archive, mixed archive,
and unsupported entries with no writes; a wrong digest; a source changed after
the preview; damaged stored bytes keeping a source; interruption after the
manifest, after a partial copy, after publishing a held copy, and after an
entry's move, each resumed with every planned file in the folder or the holding
area with its exact bytes and no partial left; a second folder refused while a
cleanup is pending; a tampered held copy stopping for review; a source that
cannot be removed; restore, repeated restore, and a conflict that is never
replaced; overlap and incomplete inventory refusals; and the CLI and TUI review
flows. An independent adversarial review then found eleven defects, each now
with a regression test: overlap through a link, junction, or short name moving
the library's own files; restore removing the only copy when the folder path and
the held copy were the same file; restore removing a user's file named like a
partial; a partly moved packet never restored; empty, prefixed, or commented
archives and foreign receipts moving; a stuck cleanup with no way out, including
on filesystems without hard links; a kept source's read-only setting changed;
one large file stopping the plan; a garbled pending message; partial files left
after an interruption; and cleanup skipping an interrupted rescan. A second
review found three more, also fixed with tests: a link to a directory below the
library root, or a library named through a link into the folder, still counted
as no overlap; a full cleanup journal could leave a cleanup pending with files
already moved; and a restore blocked by one path could neither finish nor be
closed. On filesystems with coarse timestamps (FAT, exFAT, some network shares)
a same-size rewrite in the moment between verification and removal is not
detectable; the source's identity, size, and time are the last checks made.
