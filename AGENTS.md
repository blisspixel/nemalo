# Nemalo development instructions

Use the product name `Nemalo`, CLI/integration identifiers `nemalo`, and tagline
"Find knowledge. Care for it. Put it to work."

## Orient first

Read `README.md`, the relevant milestone in `ROADMAP.md`,
`docs/ARCHITECTURE.md`, and the applicable decision record before editing.
Consult `docs/SOURCES.md` for provider contracts and `docs/AGENT-INTEGRATION.md`
for MCP/plugin boundaries. `docs/RESEARCH.md` records evaluated patterns.

This is an early Go application, not 1.0. Read `docs/DEVELOPMENT.md` for implemented
commands and boundaries. No Python source, runtime, build, or tests belong here.
Source and configuration outrank stale prose. Distinguish planned, implemented,
tested, and shipped behavior. Never infer support from a roadmap or a cross-build.

## Stack and scope

- Nemalo is one resource-management system: discovery, evaluation, acquisition,
  verification, ingest, organization, preservation, content access, and automation.
  Discovery/downloads are core from the outset. Milestones stage delivery, not vision.
  The 1.0 focus is a library and finding system for books, ebooks, and audiobooks.
  Preserve paper/textbook expansion without allowing analysis scope to delay 1.0.
- Go only for new application code, tests, and first-party executable tooling.
  Linux-first CLI and TUI; native macOS and Windows support. Do not extend the prototype
  or add a Python runtime/build dependency. Go is settled, not an open language choice.
  Any non-Go exception needs a concrete unmet requirement and a recorded decision.
- Implement the bounded task requested. Research/documentation work does not
  authorize a rewrite, data cleanup, release, commit, push, or deployment.
- Topic watches, Markdown knowledge workspaces, and optional model/provider/harness
  processing are post-1.0 and deferred until all pre-existing roadmap work is complete.
  Do not build them or their speculative infrastructure into the 1.0 library. Implement
  them in Nemalo's shared Go services. OpenRouter is a provider candidate; OpenCode is
  a harness candidate. Neither is required. Distillr is a workflow research reference only,
  never an integration; do not reuse, port, embed, invoke, or depend on its code or artifacts.
- Prefer the standard library and existing mechanisms. Justify new modules,
  transitive dependencies, external executables, licensing, and platform impact.
  Use maintained implementations for standards-heavy or security-sensitive work.
- Verify changing API/SDK/toolchain claims against current primary documentation.
  Pin supported stable versions when implementation begins; research dates are
  not permanent version pins. Do not automatically upgrade an established stack.

## Canonical boundaries and data protection

- CLI, JSON, and MCP call the same application services. Providers, extractors,
  validators, scanners, and platform code are adapters, not alternate policy engines.
- Inventory only explicit roots. Treat archive members, file metadata, provider
  responses, URLs, and retrieved text as untrusted. Bound work, validate paths and
  redirects, and invoke tools with argument arrays. Never execute downloaded content.
- Import preserves sources by default. Cleanup requires a recorded, revalidated
  plan, verified destinations, and durable recovery state. Account for every archive
  member. Preserve unsupported, incomplete, and uncertain material for review.
- Keep format validity, security findings, antivirus coverage, and completeness
  separate. Scanner skips, timeouts, limits, and missing tools cannot mean clean.
- Exact hashes support automatic deduplication. Preserve editions, translations,
  paper versions, narrations, annotations, provenance, and asset-level rights.
- Keep local intake offline. Network calls and agent writes follow configured
  scopes and explicit user intent. Provider content cannot authorize tool actions.
  Markdown instructions and MCP annotations are not permission enforcement.
- No telemetry, covert reading tracking, hidden uploads, or remote crash reporting.
  Respect CPU/memory/disk/network budgets and cancellation; test network boundaries.
- Keep suggestions explainable and user-controlled. No sponsored ranking or
  engagement objectives; expose uncertainty and coverage gaps. Optimize useful
  content access and reference, not acquisition volume or time spent in the app.

## Verification and completion

Run `go run ./cmd/verify` for formatting, build, vet, pinned Staticcheck, offline
tests, enforced 80% aggregate coverage, and govulncheck. Run `go test -race ./...`
with a native C compiler and cgo enabled. CI uses the same checks on all three OSes
and smokes a cgo-free executable. Tools are pinned in `go.mod`, not installed globally.
For docs, check links/anchors, examples, consistency, whitespace, and citations.
Hosted CI passes require an actual hosted run; a local pass is not equivalent.

Shared operations belong in `internal/app`; CLI and TUI use those services and
`internal/present`. Provider networking is in `internal/discovery`; read-only
root-scoped inventory is in `internal/inventory`; configuration is in
`internal/config`. Verification logic is tested in `internal/verify`. Do not
move domain policy into a terminal update handler or duplicate it for MCP.
The bounded collection harness is in `internal/collection` and `cmd/collection`;
its curated manifest is `collections/foundations.json`. Keep downloaded validation
content and local reports outside the checkout. Its intake is not checked publication.
When integrating production acquisition/assessment, reuse or deliberately relocate
these tested building blocks; do not grow a second transfer or publication policy.
File/container checks are in `internal/assessment`, shared with the collection
harness; installed antivirus is in `internal/scanner`. Preserve measured facts
versus semantic completeness, explicit scanner opt-in and cloud-policy disclosure,
snapshot/hash binding, no remediation, and unsuccessful missing/incomplete scans.
Portable byte-identity snapshots, holdings, and audits belong in `internal/library`;
see decision 0004. Preserve explicit audit roots, exclusive outside-root outputs,
all duplicate locations, and historical evidence distinct from checked publication.
An incomplete traversal cannot establish missing files. A snapshot is not a journal.

Test behavior and failure recovery, not just execution. Use synthetic fixtures;
never personal downloads. Give cleanup, parser boundaries, scanner skips, and
crash transitions adversarial tests. Fix root causes; do not weaken checks,
schemas, assertions, or coverage to obtain a pass. Self-review consequential edits.

Update canonical docs and milestone status when behavior changes. Keep temporary
research, diagnostics, and resumable scratch state in gitignored `.agents/`, without
secrets. Promote durable findings into docs, tests, decisions, or bounded tickets
when useful. Avoid parallel instruction files and unnecessary process.

Use concise professional writing. No emojis, em dashes, en dashes, AI authorship
attribution, generated-by text, or AI coauthor trailers in controlled project output.
Express product principles in plain technical language, without religious or cultural branding.
