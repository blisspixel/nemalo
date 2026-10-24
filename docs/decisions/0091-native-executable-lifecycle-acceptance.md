# 0091: native executable lifecycle acceptance

Status: recorded before implementation; implemented in source, 2026-10-24. Stage 5 acceptance tooling,
separate from production policy, host/plugin compatibility and a release.

Native CI already runs canonical verification, race, recovery fixtures and a
cgo-free executable smoke on Linux, macOS and Windows. Installer jobs exercise
actual published releases. Add a Go `cmd/nativecheck` harness to exercise the
current cgo-free executable through the same CLI contracts on each native runner.
Keep all existing checks, case identities, instrumentation, workers and deadlines.

Require the expected full commit, native OS/architecture, module and clean build
metadata before execution. Copy the explicitly selected executable through an
opened regular file into a fresh private workspace; verify the copy's hash and
build metadata. This is an identity check, not a signature or permission to run
downloaded content. Workspace output must be new and outside the checkout,
including resolved aliases and parent identities. Retain it on success or failure;
never adopt an existing directory or recursively clean user data.

Generate only small synthetic EPUB/ZIP fixtures. Invoke the exact private
executable with argument arrays, bounded stdout/stderr, per-command and overall
deadlines, explicit configuration, isolated temporary files and disabled external
tool discovery. Never repurpose the user's home. macOS default state paths are
computed by the CLI but unused by these selected commands. No provider, scanner,
decoder, external reader or model runs. Missing scanner coverage must report
unavailable. Holdings stay in review; no simulated scan or manufactured checked
publication is needed. Cleanup verifies held bytes independently of scan status
under [decision 0022](0022-recoverable-source-cleanup.md).

Check actual exit codes and JSON envelopes, import preview/apply and changed-plan
refusal, idempotence, source text/navigation/evidence/replay/citation, explicit
bookmarks and disabled-save refusal, verified export, ordinary ZIP ingest,
whole-library integrity, cleanup preview/apply and changed-plan refusal, raw
restore with a preserved conflict, fresh purge and wrong-plan refusal. A purged
copy cannot be silently reconstructed by restore. Compare bytes and modes around
read-only/refused operations, preserve original intake and uncertain neighbors,
and verify holdings and disabled bookmarks after the lifecycle.

Write a versioned receipt and bounded per-command diagnostics outside the checkout.
An error, timeout, output limit, malformed envelope or failed semantic assertion
stops further commands and fixture mutation. Partial state remains for review;
the harness never retries, repairs or substitutes a new cleanup/purge plan.

Test the harness with the real CLI/application services and injected external
adapters, plus process-output/cancellation and workspace/build-identity refusals.
Normal tests remain offline and invoke no installed tools. Then require local
canonical verification/native race and actual hosted execution of this additional
step on all three OSes. This adds no application option, journal schema, parser,
runtime dependency or alternative policy engine. Native installed-tool coverage,
interactive terminals, real host/plugin calls, other architectures and live
release installation remain distinct evidence.

## Hosted isolation correction

The first [main run at `74ab937`](https://github.com/blisspixel/nemalo/actions/runs/40071079730)
passed all 39 executable cases on Linux and macOS. Windows passed canonical
verification, race, all 32 boundary cases and the executable smoke, but its
missing-scanner case returned success rather than the required unavailable
refusal. The complete hosted gate failed; this increment was not accepted.
That log retained the diagnostic filename but not the failing command's JSON,
so a specific scanner or environment mechanism cannot be established from it.

Environment-construction regression tests reproduced inherited alternate Windows
program roots, including `ProgramW6432`, the common program directories and
`ALLUSERSPROFILE`. Redirect the complete group into the owned empty tool root.
[Microsoft documents custom child environments](https://learn.microsoft.com/en-us/windows/win32/procthread/changing-environment-variables)
and [program-root changes across process architectures](https://learn.microsoft.com/en-us/windows/win32/winprog64/wow64-implementation-details).
Those contracts motivate checking the actual child environment; they do not
prove that architecture translation caused this failure.

Add a native child-process regression that reports only selected tool-root
variables and resolves adapters without executing them. Run it before the full
canonical gate on every native runner. The executable lifecycle's existing
doctor case must report both scanners unavailable and no available external
tools before library initialization or a scan can run. Keep the same 39 cases,
expected exits and unavailable-scanner assertion. Failed receipts add a bounded
last-command excerpt, at most 16 KiB stdout and 4 KiB stderr with explicit
truncation, alongside the existing complete bounded files. No environment block
is published. Full final local and exact hosted acceptance remain required.
