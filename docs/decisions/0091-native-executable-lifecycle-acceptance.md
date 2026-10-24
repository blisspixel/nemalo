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
