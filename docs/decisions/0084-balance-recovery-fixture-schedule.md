# 0084: balance the existing recovery fixture schedule

## Revision: remaining serial archive fixtures and failed counter retention

The source accepted at `5091ce7` passed all six hosted jobs and Go/Actions CodeQL.
The subsequent documentation-only `cf8957f` failed Windows canonical verification
in [run 40021076060](https://github.com/blisspixel/nemalo/actions/runs/40021076060).
The ordinary serial phase ended at 9m33.180s, so several queued recovery groups
had only 27 seconds before the unchanged ten-minute allowance expired. The library
process failed at 603.659 seconds. Windows race, boundary and binary steps were
skipped; the other five jobs and CodeQL passed. This is failed evidence, not a
regression claim against the separately accepted stage 3 receipt.

A fresh Linux covered baseline retained the exact current fixtures and completed
in 52.664 seconds: serial phase 29.394 seconds, 43 scheduled groups and 91.103
seconds of aggregate scheduled work. Profiling identifies twelve substantial
ordinary archive roots: old-set restore, modern RAR4/RAR5 pending-purge and
independent-shape checks, RAR tree previews/root recovery/single-archive lifecycle,
resume bindings, independent groups, unknown bindings and old-set fresh purge.
Move those intact roots into the existing fixed queue with one exact root group
each. Do not split their lifecycles, alter their assertions or add workers. Keep
ordinary filtered execution and exact RUN/PASS consumption. All original leaf
identities must survive; new instrumentation tests are additional.

The failed log also shows attempted removal of temporary counter files while
timed-out descendants could still be exiting. This is a secondary failure, not
an explanation of the original latency. Enforce decision 0081's existing ordering:
retain owned private counter directories after child/merge/publication failure,
and retain the schedule and reader counter roots unless the entire test run and
required final counter publication succeed. Report retained paths and cleanup
errors. Remove successful owned scratch through the existing no-follow helper;
never apply this to production sources, stores or recovery fixture directories.

Keep all counters and their merges, real processes, instrumentation, the same
three-worker ceiling and reserved ordinary lane, child/package deadlines and
production durability. Require fresh exact-case comparison, canonical verifier,
native-cgo race, Windows cross-vet and actual all-six/CodeQL acceptance. Installed
player conformance and later evidence implementation do not follow from a local
scheduling result. CI repair precedes publication of issue 39's feature work.

Status: accepted design, 2026-10-23. Test infrastructure only, tracked in
[issue 60](https://github.com/blisspixel/nemalo/issues/60). No application behavior,
cleanup authority, fixture content, storage or durability change is authorized.

## Evidence

The packet-evidence increment at `c4fc1b5` passed canonical local verification,
full native-cgo race, native packet comparisons, Linux/macOS native checks,
all installers and Go/Actions CodeQL. In
[hosted run 39985939770](https://github.com/blisspixel/nemalo/actions/runs/39985939770),
native Windows canonical verification passed, with library elapsed time 602.063
seconds. Its subsequent library race suite hit the unchanged 600-second alarm,
reporting 602.531 seconds. Windows reparse and binary smoke steps were skipped.
That run is failed evidence, not acceptance. The preceding writer's successful
exact gate and receipt at `1a9e67b` remain separate historical evidence.

The timeout stack was in ordinary `TestFlatSevenVersionedSetRecovery/fresh_purge/missing`,
waiting for the real synthetic reader process, while the recovery roots remained
paused at the parallel barrier. It was not in MP4 packet indexing. The serial
phase had not released the queue's third lane. This trace alone does not establish
every worker's completed work or explain all host timing variance.

The full local race trace measured a 156.557-second serial phase within a
158.273-second library test body. Four existing serial roots took 25.860 seconds
for nested-volume restore/bindings, 15.640 for exact old-name RAR recovery bindings,
13.870 for nested-volume layout lifecycles and 5.370 for flat 7z set recovery.
They are independently generated fixture matrices already using the same
recovery helpers. Their current placement is avoidable serial imbalance.

## Decision

Route only these four unchanged roots through the existing fixed fixture registry
and recovery process queue. Preserve their original root, subgroup and leaf names.
Use the two existing outer groups for nested bindings, the two existing format
groups for old-name bindings, and one whole-root group each for layout lifecycles
and flat 7z recovery. Do not split or remove any acceptance case.

Keep the same three-worker ceiling, one reserved ordinary lane until that phase
finishes, immutable captured environments, same compiled test/reader binaries,
race and coverage instrumentation, real subprocess boundaries, private counters,
merge/publication checks and error propagation. Keep every fixture body, scanner
injection, assertion, process/child/package deadline and production filesystem
operation. Never bypass initialization/import, clone recovery stores, skip fsync,
change counter cleanup or remove application fixture data as an optimization.

Filtered, repeated, constrained, native reparse and fuzz invocations retain their
existing ordinary runner behavior. Unknown or unregistered selectors must still
fail. A successful queue command must prove its exact selected original test
group ran and passed. This changes work placement, not coverage or acceptance.

## Verification

Run the exact selectors and ordinary filtered groups, compare every original
library leaf identity before/after, and preserve all 1,612 preceding leaf cases.
Require the canonical verifier, full native-cgo race, cross-vet and document
checks, then actual all-six hosted success and Go/Actions CodeQL on the exact
new commit. Report native Windows timing without inferring it from Linux or
calling differing hosts a controlled benchmark. Keep issue 60 open for the
remaining public audio contract, initialization, timing and player work.

## Local evidence

Ordinary filtered execution of the four original roots and selector checks
passed. Canonical verification passed at 86.6% aggregate coverage, including
pinned Staticcheck and govulncheck with no reported vulnerabilities. The library
covered suite took 41.633 seconds. Full native-cgo repository race passed; the
library body took 118.504 seconds and the complete process 126.317 seconds.
The preceding complete process took 171.692 seconds. Exact case comparison
retained all 1,612 original library leaf identities, with no missing cases.
Windows library cross-vet and document links/anchors/whitespace checks passed.
These observations are local evidence, not hosted acceptance or a controlled
performance benchmark. Actual exact-final all-six results remain required.

## Revision: deterministic bounded dispatch

The preceding scheduling increment passed all six hosted jobs and Go/Actions
CodeQL at `c95696e`. A later unchanged-library run at `a9e170c` failed Windows
canonical verification in [run 40000361799](https://github.com/blisspixel/nemalo/actions/runs/40000361799).
The nested-volume layout child stopped before completing its original cases,
and the package reached its unchanged ten-minute alarm while cleaning the
schedule workspace. Windows race, reparse and binary smokes were skipped. The
new content tests and the other five jobs passed. That failed gate is not
acceptance of the public packet increment.

Inspection found that one goroutine per group competes for worker tokens, so
the declared case list does not determine dispatch order. Successful native
Linux traces already show later declared jobs starting before earlier ones.
The failed trace does not record its child's actual start, and cannot establish
all Windows work timings. The whole nested-volume layout group also combines
17 independent leaf lifecycles behind one scheduling item.

Use one ordered queue and exactly three workers sharing the existing token
limit and reserved ordinary lane. Dispatch the existing larger nested/tree
groups before smaller standalone groups, with stable order within each class.
Split only the nested-volume layout scheduling item by its existing exact Go
leaf selectors; retain every original leaf body and name, including all seven
`zip` child layouts. Anchor every path component of a multi-component selector.
The receipt must still prove the exact selected leaf ran and passed; implicit
outer groups have no separate Go result header. Do not split a lifecycle or add
another process pool.
Include actual start/end timing in failed scheduled receipts as well as success.

Preserve all existing captured environments, cases, assertions, counter merging,
owned-scratch cleanup, real subprocesses, instrumentation, worker limits,
deadlines and production durability. Test reserved-lane behavior, exact queue
consumption, cancellation of unstarted work and unchanged selector identities.
Run ordinary filtered fixtures, the full verifier and native-cgo race without
reusing the old library result, compare all 1,612 preceding leaf identities,
then require a new exact all-six hosted gate and Go/Actions CodeQL. No decoder
implementation starts before this repair's acceptance; the separately approved
decoded-access design remains the next stage 3 increment.

The revision's ordinary layout fixtures and ten repetitions of queue/selector
tests passed under race detection. Canonical verification passed at 86.7%
aggregate coverage with no reported Go vulnerabilities; the library covered
suite took 49.123 seconds. The full native-cgo race command passed; the library
process reported 163.558 seconds. Exact comparison retained all 1,612 preceding
leaf identities and added three queue-test leaves, with none missing. Windows
cross-vet and document checks passed. These local observations do not establish
a speed improvement or Windows acceptance. A new exact hosted gate is required.
