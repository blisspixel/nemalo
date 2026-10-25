# 0095: a measured test package budget for hosted Windows

Status: accepted by the user, 2026-10-24. Test infrastructure only. It changes
the wall-clock limit of each test binary, not any case, assertion or production
behavior.

## Evidence

Go's default limit is ten minutes per test binary. On hosted Windows runners the
covered `internal/library` suite took 462 seconds at `392f21f` and 515 to 545
seconds in earlier receipts, and the race suite about 542 seconds. The same
covered suite takes about 28 seconds on a local 16-thread Linux machine.

At `df5b28f`, a documentation-only commit with code identical to `392f21f`,
[run 40133259216](https://github.com/blisspixel/nemalo/actions/runs/40133259216)
failed the Windows covered suite on its first attempt and on one same-head rerun,
on the same runner image (`windows-2025-vs2026` 20261021.278.1). The recovery
schedule reported its serial phase ending at 6m21s and children starting at
9m32s; each child took 22 to 28 seconds. The package limit, not a child
deadline or a hang, ended the run, and every test waiting on a child reported
`context deadline exceeded`. An earlier timeout at `3a6c793` and
[decision 0081](0081-coverage-counter-scratch-cleanup.md)'s
602.8-second failure are the same margin problem.

A temporary diagnostic run on the same image showed Microsoft Defender
real-time and on-access protection already off and 4 logical processors.
Real-time scanning is therefore not the cause. The remaining cost is four cores and
the durable file flushes that production keeps on purpose.

## Decision

Run the canonical verifier's covered tests and the CI race step with
`-timeout=25m`. Keep every case, assertion, instrumentation setting, the
three-worker recovery ceiling, the five-minute per-child recovery deadline,
production durability calls and decision 0081's scratch handling. The recovery
schedule already derives its overall limit from `-test.timeout`. Ordinary local
`go test` runs keep Go's default. The verifier's own test requires the flag.

The first hosted run with this budget,
[CI 40167463572](https://github.com/blisspixel/nemalo/actions/runs/40167463572)
at `7074251`, passed all six jobs. Its Windows covered library suite took 862.4
seconds and its race suite 771.9 seconds, both beyond the former ten-minute
limit, so the variance is larger than the earlier receipts showed.

This trades a later failure signal for a hung package for a stable signal on a
slow runner: a real hang still ends at its five-minute child deadline or the
25-minute package limit. Reducing Windows test time remains worthwhile, through
profiling rather than by removing cases.
