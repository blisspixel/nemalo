# 0007: exclusive untrusted acquisition packets

Status: accepted and implemented, 2026-10-08.

## Decision

Deliver selected-file acquisition before the mutable library/catalog model is
complete. Acquire an exact Archive EPUB/PDF/MP3 through fresh provider evaluation
into an explicitly selected new packet directory. The shared application service
owns eligibility, budgets, evidence, assessment, and completion. CLI/JSON and TUI
call that service; TUI review freezes the exact write request.

Use standard-library streaming, checksums, HTTP, and exclusive file operations.
Relocate transfer counting/hash validation into `internal/transfer`, used by both
production intake and the curated collection harness. Both reuse the checked
file HTTP adapter in `internal/discovery` and checks in `internal/assessment`.
No new module or executable dependency is needed.

Sync intent before download and publish a synced receipt last. Keep incomplete
packets rather than deleting evidence, overwriting data, or attempting an unsafe
resume. Receipt publication requires filesystem hard links. These packets do not
extend the initialization journal or establish checked library holdings.

## Consequences

Search, evaluation, acquisition, local assessment, snapshots, audit, and EPUB access
can now participate in one deliberate workflow. Item/asset rights, exact edition
identity, antivirus coverage, and complete audio track sets remain explicit gaps.
Validator-bound resume, multi-file jobs, managed import recovery, and checked
publication remain required roadmap work. The
[operation contract](../ACQUISITION.md) records bounds and failure handling.
