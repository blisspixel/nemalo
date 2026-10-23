# 0088: non-mutating library job observations

Status: decision recorded before Go implementation, 2026-10-23. Not implemented,
stage accepted or released. This is the shared prerequisite in
[issue 59](https://github.com/blisspixel/nemalo/issues/59), after milestone 9's
[exact acceptance receipt](https://github.com/blisspixel/nemalo/issues/39#issuecomment-6376290156)
at `51bb249`. The optional Omarchy consumer remains separate work. This decision
adds no QML, new language exception, runtime, module, scanner or network request.

## Observe without opening a writer

Add `Service.JobSnapshot` and `nemalo jobs snapshot LIBRARY [--limit N] --json`.
The CLI supplies its configured state/jobs directory; the caller explicitly
selects an existing library. TUI and optional consumers use the same service.
No new MCP job tool is required. Keep existing jobs list/get/cancel behavior:
[decision 0045](0045-durable-mcp-jobs.md) intentionally permits initialization,
owner reconciliation and retention cleanup there. A passive snapshot never calls
`jobs.Open`, `Get`, `List`, `Cancel`, append or repair.

Return schema `nemalo-job-snapshot/1`, selected library ID and observation state,
observation start/end times, recorded job summaries, relationship evidence,
coverage, truncation, coarse problems and limits. Summaries include ID, known kind,
recorded status/timestamps, bounded progress, whether an error was recorded and
journal integrity. Omit raw requests, results, errors, paths and unrelated records.
Queued/running are recorded outcomes, never a claim of current work. Owner/liveness
is explicitly unobserved; observation acquires no job lock or write authority.
Age and expiry remain visible as recorded facts, without deleting history.

Missing directories stay missing. Do not create cancellation markers, prune
expired journals or orphaned control files, reconcile abandoned owners, repair a
tail, read stored asset bodies or change bookmarks/consumption state. Every file
is opened read-only through an existing capability, checked for regular type and
opened identity. Reject final links/reparse points and retain supported parent
aliases. Revalidate the opened entry after reading; replacements or changes are
incomplete observations. This is an observation interval, not an atomic global
snapshot or protection against noncooperating software.

## Library identity during an active writer

An applied rescan holds the library writer lock throughout its scan, so ordinary
LibraryStatus correctly returns busy. Do not weaken that API or read mutable
holdings through an active writer. Add a distinct library observation over the
same canonical two-record initialization journal from
[decision 0006](0006-library-identity-and-initialization.md). That journal is
immutable after successful initialization under Nemalo's protocol.

Open the explicit root/control capabilities, validate the existing bounded control
namespace, and read/revalidate the same initialization handle and identity.
Pending, corrupt, missing or unsupported identity never becomes initialized.
The observation can report an established identity while the writer is busy;
it does not establish catalog health or fresh stored bytes. If a nonblocking
shared lock is available, reuse canonical managed validation and complete journal
checks to obtain latest recorded holdings through that same control capability.
Otherwise report busy/holdings-unavailable and retain only identity-based job
relationships. Revalidate the identity before returning. Callback/close failures
invalidate scoped output. Normal status, import, cleanup, purge and recovery locks
retain their existing contracts.

Bound control enumeration to the seven accepted control names plus one refusal
entry, retaining every existing name/type rule. Reuse existing initialization,
operations (4 MiB), holdings (8 MiB), narration and record bounds, parsers and
consistency checks. No historical source/receipt path is opened. The complete
latest-holding view stays internal and supplies only association evidence.

## Canonical relationships, not folder guesses

Add optional immutable context to a job's queued record and public job data.
`jobs.Start` stays available; `StartWithContext` records at most 4 KiB of opaque
JSON context beside the unchanged request. Existing schema-1 records without
context remain readable; old readers ignore that optional field. Context is a
recorded declaration, never permission. The application owns
`nemalo-job-library/1`, containing only schema and an exact library ID.

New rescan starts obtain this context through the shared application service after
validating their explicitly selected library. Acquisition starts may take an
optional scoped `library` solely to record intended library context; it neither
imports nor grants library write authority. Omitted context preserves ordinary
intake behavior. Provider, scan, output, network/write and download policies stay
unchanged. The reader lock is released before starting asynchronous work.

The snapshot uses these distinct recorded relationships:

- A known context naming the selected library identifies a declared target.
  A contradictory recorded rescan result is uncertain, not adopted.
- A legacy completed rescan can identify the selected library through its
  canonical result's exact library ID. A request path alone cannot supply it.
- An acquisition result can relate to a current holding through its exact source
  selection, recorded bytes/SHA-256 and packet provenance. Single-file and
  per-track recording results use the same held packet matcher. Require the
  exact source ID/file, transfer identity and recorded packet parent; recording
  positions/formats stay distinct. Partial held recordings remain partial.

Do not infer library membership from a title, nearby intake directory, basename,
source count or byte equality alone. Do not open job-supplied or historical paths.
Unknown/malformed context never falls through to an older relationship rule.
Unbound legacy jobs have no invented target. Coverage explicitly concerns recorded
relationships, not all possible user intent; an empty result does not prove that
no unbound work exists. Busy holdings make acquisition relationship coverage
incomplete. Unrelated per-user requests/results never appear in public output.

## Bounded journal mechanics

Directory enumeration examines at most 4,096 entries in bounded batches. Read at
most 6 MiB per journal and 32 MiB of actual journal bytes per request, including
failed work. Fold at most 10,004 records per journal. The application operation
has a 15-second ceiling including library observation; a shorter caller deadline
wins. Filesystem calls can still stall; this is not an OS sandbox. Return at most
100 jobs (default 20), newest recorded creation time first with an ID tie-break,
and at most 1 MiB after safe JSON encoding. These are source contracts, not new
user-adjustable execution permissions.

Reuse one record-folding implementation. Snapshot validation additionally checks
the supported record/context shapes, duplicate starts, canonical complete lines,
bounded values and known transitions. A torn trailing line is not repaired or
folded as committed state; retain any valid scoped prefix with explicit incomplete
integrity. Unknown schemas/events or malformed complete records cannot masquerade
as complete history. Partial enumeration, read limits and result limits are
explicit. Results from an incomplete scan are newest only within observed records.
Cancellation and library identity/close failures discard all scoped jobs; isolated
bad job files may leave independently valid observations with coarse incomplete
coverage and no unrelated file details.

The shared journal reader must account for terminal JSON framing: a progress prefix
near the 4 MiB allowance plus a 1 MiB result and its metadata exceeds the old
5 MiB-plus-one reader. Use the same bounded 6 MiB file allowance and explicit
overrun refusal when factoring that reader, rather than silently turning a valid
terminal result into unfinished work. Preserve legacy folding/tail behavior
otherwise, writer limits, syncs, locks, queue bounds and retention policy.

## Verification and completion

Use synthetic fixtures and injected scanners/providers only. Test empty/mixed
stores, active and abandoned jobs, retained expired history/control files,
recorded versus live status, new contexts and legacy relationships, unrelated
libraries, same-path different identity, unknown/corrupt/torn records, valid near-
limit terminal results, all count/read/output/deadline bounds, capability swaps,
read-only permissions and missing roots. Compare complete inventories and bytes
before/after repeated reads, including bookmarks and job control files. Test an
active library writer without granting mutable-catalog evidence, plus CLI/service
parity and invalid flags before any writer store opens. Existing job writers and
MCP start/scope/permission tests remain required.

Run full canonical verification, native cgo race and actual all-six hosted checks,
including exact Windows boundary cases. Publish an issue receipt against the
implemented prerequisite. Keep issue 59 open until its separate consumer,
explicit handoff, race/generation, disabled-bookmark and native lifecycle criteria
are met. A passing prerequisite alone does not complete that issue or stage 5.
