# 0089: bind collection acquisition to its reviewed local preview

Status: implemented in source, 2026-10-24. Stage 5 interface work over decisions
[0050](0050-collections.md) and [0053](0053-collection-revision-previews.md).
Complete exact native/hosted acceptance and release remain separate.

## Decision

The shared application acquisition preview returns a `plan_digest`. Its
`collection-acquire/1` preimage binds the initialized library identity and path,
collection path and exact file hash, absolute intake, total and normalized
recording byte budgets, scanner/detection choices, and ordered preview steps.
Steps bind exact selections, packet paths, measured actions and remaining bytes.
Filesystem paths and diagnostic bytes are encoded losslessly in the internal
preimage; JSON's replacement of invalid UTF-8 cannot collapse different paths.
This is an ephemeral intent comparison, not a durable plan or byte snapshot.

An optional expected plan is checked after the ordinary fresh offline preview
and before creating intake, evaluating providers or transferring bytes. A changed
collection, library identity, missing selection, packet action or budget requires
a fresh review. The transfer still revalidates each source and packet through the
existing acquisition services. Matching preview facts cannot replace those checks
or freeze concurrent edits after observation. The checked in-memory selections,
not a later reopening of the collection file, drive the run.

CLI `collection acquire --apply --plan DIGEST` enables the same guard. Existing
explicit CLI apply without a plan retains its behavior. A digest alone grants no
write or network authority. No collection catalog, journal, store schema,
dependency, executable, parser or MCP write tool is added.

## Terminal workflow

Collections and revision comparison use ordinary session-scoped forms and the
same application status/diff services. Status, comparison and acquisition preview
remain offline and do not initialize a library or create missing intake. Declared
selections, holdings resolution, assessment status and source rights stay distinct.
Removed selections do not remove holdings or notes.

Acquisition requires explicit collection, initialized library and outside intake.
Review freezes the exact request and expected plan. A second deliberate Enter
permits fresh provider evaluation, bounded transfer and ordinary scanner policy.
The TUI uses an explicit total byte cap, initially 1 GiB with a deliberate 5 GiB
choice; it does not select unbounded acquisition. Downloads remain untrusted
intake. Cancellation retains started packets; ingest remains a separate action.
Confirmation matches the key event, not an IME's committed text that happens to
spell a control name. This same distinction applies to existing download and
library write-review dialogs.

Preserve original operation shortcuts, per-operation drafts, full reports,
keyboard navigation, visible non-colour outcomes and late-result fences. Add no
polling, automatic acquisition, implicit source selection, saved reading state
or model processing.

## Verification required

Test stale collection/library/holdings/packet/budget refusal before requests or
writes, matching guarded application, cancellation and ordinary unguarded CLI
compatibility. Test offline status/diff, exact row identities, disabled bookmarks,
missing intake preservation, frozen review, cancellation and late replies, narrow
and wide frames, retained failures and bounded transfer through injected providers
and scanners. Run canonical verification, native-cgo race, Windows cross-vet and
documentation checks, then exact native terminal and hosted acceptance. Real
installed-tool or provider evidence is separate from synthetic tests and release.
