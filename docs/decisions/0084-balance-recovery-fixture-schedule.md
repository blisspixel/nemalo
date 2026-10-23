# 0084: balance the existing recovery fixture schedule

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
