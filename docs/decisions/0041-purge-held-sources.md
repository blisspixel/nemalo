# 0041: purge held cleanup copies the library holds

Status: accepted and implemented, 2026-10-19. The explicit purge that
[decision 0022](0022-recoverable-source-cleanup.md) left for later, with its own
preview and plan. Settles remaining implementation choice 2.

## Context

Cleanup moves a source the library holds into the library's holding area,
where it can be restored. Nothing removed held copies, so a cleaned folder's
space was never freed: the holding area kept a second copy of every held book
and recording until it was deleted by hand, without any check that the
library still held the same bytes.

## Decision

`library purge LIBRARY --cleanup ID` previews and `library purge LIBRARY
--cleanup ID --apply --plan DIGEST` applies. A purge is the only operation
that deletes held source bytes, and it never deletes bytes the library does
not also hold.

- **Plan.** Only a completed cleanup is purged, and only while no cleanup,
  restore, or purge is pending. Each held file of the cleanup, indexed as
  restore indexes it, gets one disposition. `purge`: the held copy has its
  planned bytes, and the holding it stands for is current with stored bytes
  that hash to its recorded SHA-256; for a ZIP, every member's holding. A
  packet's receipt and intent are `provenance_record` and stay. A missing held
  copy (never moved, restored, or purged) is `not_held`, a copy with other
  bytes `held_copy_changed`, and a copy whose holding is missing or no longer
  verifies `not_in_library`; each stays. The plan digest binds the cleanup ID
  and every file's index, path, size, SHA-256, disposition, and holdings.
- **Apply.** Under the writer lock, after completing any interrupted rescan,
  the plan is made again and must have the reviewed digest, or nothing is
  removed. The journal must have room for every removal first. A purge
  manifest of the plan is written and synced in the cleanup's folder, and its
  SHA-256 recorded in a `purge_started` line of `.nemalo/cleanup.jsonl`, before
  the first removal. Each file is removed only when every holding it stands
  for still verifies, its held copy hashes to the planned bytes through an
  opened handle, and its name still refers to that file with the same size and
  modification time. Every folder from the library root to a held copy is
  opened without following links, so a link planted in the holding area can
  never lead a removal into `assets/` or anywhere else. One `purge_entry` line
  per file records `purged`, `already_absent`, `held_copy_changed`,
  `not_in_library`, `not_readable`, or `not_removed`; a copy that cannot be
  read, verified, or removed stays, and only cancellation stops the purge.
  `purge_completed` closes the operation. A publishing partial that is a
  second name for a purged copy, left by a cleanup closed with `--stop`, goes
  with it. Folders left empty are removed.
- **Recovery.** Repeating the apply with the same digest resumes a pending
  purge from its manifest, never from a new plan. A file already gone was
  removed before the interruption journaled it and is `already_absent`. A
  manifest written before an interruption, with no journal line naming it, is
  replaced by the retry. While a purge is pending, cleanup and restore refuse
  to start and name the commands that finish or close it. `library purge
  LIBRARY --cleanup ID --stop` closes a purge that cannot finish, for example
  because its manifest was removed; removals already journaled stand, and
  every other held copy stays.
- **Restore** reports a purged file as `purged` instead of `not_held`, and still
  returns everything else the cleanup holds. Restore, cleanup, and check read
  held copies the same way, without following links in the holding area. `library check` verifies what
  remains, and its next step names purge.

## Consequences

A cleaned folder's space can be freed without losing any bytes the library
does not keep. A purged copy cannot be restored from the holding area; the
library's own copy, which `library export` gives back, is the remaining one.
Receipts and intents stay, so a cleanup's holding area keeps its provenance
records; delete them by hand only when they are no longer needed. A library
with a purge record cannot be opened by earlier prereleases. MCP has no purge
tool. The TUI previews a purge with Ctrl+P in State and applies only the
reviewed plan.

## Validation

Synthetic tests purge a cleanup of a loose EPUB, a packet, and a ZIP whose
member is held: the preview removes nothing, a wrong digest is refused, the
three held copies go, the receipt and intent stay, stored assets are
unchanged, `library check` stays healthy, a second purge finds nothing, and
restore reports three purged files and returns the other two. A held copy
whose library copy was damaged, and a held ZIP that changed, stay. Interrupted
purges after the manifest and after a removal resume with the same digest, and
while pending they block restore and cleanup and name their digest. A purge of
an unknown or pending cleanup is refused. A CLI run on this machine cleaned a
folder of a held M4B, purged its copy, and restore then reported it as purged.

An independent adversarial review then found two defects, each now with a
regression test. Replacing a cleanup's `files` folder with a link into
`assets/` made the library's stored asset look like the held copy, and a purge
removed it; restore's held-copy removal, from decision 0022, resolved paths the
same way. Both now refuse the link, and the asset stays. A purge whose manifest
was removed, or whose copies kept failing to read or remove, stayed pending
with no way out and blocked every cleanup and restore; per-file outcomes and
`--stop` now end it. Stored bytes must also have no other link, as `library
check` requires, and pending messages name the digest and `--stop`.
