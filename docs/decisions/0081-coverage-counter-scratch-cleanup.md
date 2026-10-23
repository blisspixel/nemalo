# 0081: remove private coverage scratch without recursive handle churn

Status: accepted, 2026-10-23 UTC. Test infrastructure only, tracked in
[issue 57](https://github.com/blisspixel/nemalo/issues/57). This is not a nested
volume writer decision and grants no application authority.

## Evidence

Decision 0080's implementation at `7a18a20` passed canonical local verification,
full native-cgo race, native generated checks, Linux/macOS native CI, all three
installers and Go/Actions CodeQL. Its Windows covered library suite hit the
unchanged 600-second package timeout in
[run 39881429356](https://github.com/blisspixel/nemalo/actions/runs/39881429356),
job `120179468663`, at 602.833 seconds. Staging's covered suite passed there.
Windows race, reparse and native smoke steps were skipped. This is a failed gate,
not accepted hosted evidence; no nested writer decision or implementation follows it.

At the timeout, both scheduled recovery workers were inside `os.RemoveAll` of
their private coverage scratch, through Windows descriptor-relative deletion.
The ordinary test lane was still exercising production `FlushFileBuffers`.
The pinned Go 1.27.3 sources show recursive `RemoveAll` opening directory handles
and calling `removefileat`; Windows `os.Remove` uses `DeleteFile` or
`RemoveDirectory` directly. These observations identify avoidable housekeeping
work to measure, not a proven explanation of every second of the timeout.

## Decision

After the same instrumented reader and test processes have exited and all required
counters have been merged and published, remove only their caller-owned temporary
coverage directories with a small Go test helper. Walk without following links,
remove each file or link with `os.Remove`, then remove directories in reverse
order. Retain cleanup errors. Keep all counter bytes until their existing merge,
identity, canonical-name and publication checks finish. Do not replace cleanup
of application sources, stores, staged archives or recovery fixtures.
An unsuccessful child or counter-processing operation remains a failure; later
scratch removal cannot convert that outcome into a pass.

Keep the same compiled reader/test binaries, race and coverage instrumentation,
all original fixture bodies and assertions, real subprocesses, private counters,
three-worker ceiling, per-child and package deadlines, full canonical verifier,
native-cgo race and actual hosted checks. Do not change production filesystem or
durability calls. No new dependency, executable, network operation or public API.

Check removal semantics, sibling preservation and failure reporting with synthetic
scratch. Compare unchanged recovery case identities and coverage before/after.
Record native hosted timing; do not infer a Windows performance pass from Linux.
Decision 0080 still needs a successful exact-final all-six run and exact issue
acceptance readback before any separate nested-volume writer decision.

## Local verification

Canonical verification passed at 86.5% aggregate coverage, with library/staging
covered suites at 28.434/16.782 seconds. Full native-cgo race passed; the library
test body took 91.230 seconds and its complete process took 100.132 seconds.
Comparing the earlier scheduling baseline retained all 1,171 original leaf cases
among the current 1,386, with no missing cases. New synthetic tests cover nested
counter removal, absent scratch, failure propagation, sibling preservation and
symbolic/hard-linked targets. A separate strict native Windows case requires real
junctions and symbolic links; it has only been cross-vetted locally so far.
Documentation links/anchors and whitespace checks passed. Hosted Windows timing
and the exact-final all-six gate remain unverified until a new run succeeds.
