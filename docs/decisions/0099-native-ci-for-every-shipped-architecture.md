# 0099: native CI for every shipped architecture

Status: accepted, 2026-10-25. Stage 6 release gate. Test infrastructure only.

## Requirement

Every release ships six archives: Linux, macOS and Windows on amd64 and arm64.
Native CI ran only Linux amd64, macOS arm64 and Windows amd64, so the other
three were cross-built and never executed, although the release gates state
that cross-building alone does not establish runtime support.

## Decision

Run the native job and the installer job on all six, using GitHub's standard
public runners ([runner reference](https://docs.github.com/en/actions/reference/runners/github-hosted-runners)):
`ubuntu-latest` and `ubuntu-24.04-arm`, `macos-latest` and `macos-15-intel`,
`windows-latest` and `windows-11-arm`. Each native job runs the canonical
verifier, the native harness with all terminal cases, the executable smoke and,
on Windows, the reparse-point and decoder snapshot boundaries. The race step is
skipped only on `windows/arm64`, which Go's race detector does not support
([Go race detector requirements](https://go.dev/doc/articles/race_detector));
that platform therefore has no race coverage, rather than borrowed coverage.

## Evidence

The first run, [CI 40215309420](https://github.com/blisspixel/nemalo/actions/runs/40215309420)
at `2c0b459`, passed all twelve jobs. On the three new runners the harness
executables reported `linux/arm64`, `darwin/amd64` and `windows/arm64`, each
passed all 55 terminal cases with no failed or skipped test, and race passed on
Linux arm64 and macOS Intel. Covered library suites took 46, 369 and 562
seconds, within decision 0095's budget. The installer jobs installed, pinned,
replaced and uninstalled the published `0.1.0-alpha.18` there. Windows arm64
can emulate x64 programs, so that installer run alone does not prove the arm64
archive was the one executed; the native job does prove the arm64 build runs.
