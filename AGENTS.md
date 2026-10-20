# Nemalo development instructions

Use the product name `Nemalo`, CLI/integration identifiers `nemalo`, and tagline
"Find knowledge. Care for it. Realize its potential."

## Orient first

Read `README.md`, the relevant milestone in `ROADMAP.md`,
`docs/ARCHITECTURE.md`, and the applicable decision record before editing.
`docs/PRODUCT.md` is the product contract; keep the README a concise entry point.
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
  The only one is `scripts/install.sh`/`install.ps1` (decision 0013).
- Implement the bounded task requested. Research/documentation work does not
  authorize a rewrite, data cleanup, release, commit, push, or deployment.
- Topic watches, Markdown knowledge workspaces, source capture beyond the library, and
  optional model/provider/harness processing are post-1.0 and deferred until all
  pre-existing roadmap work is complete. Cited evidence and agent-led reading over the
  read-only MCP tools are 1.0 milestone 9 (decision 0048): Nemalo runs no model and
  stores no generated interpretation there. Follow the roadmap's order of work.
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

Before closing an issue manually or merging a PR that will auto-close it, comment
on the issue with what changed, code/commit/PR links, checks actually run and their
results, and the user-visible outcome against its acceptance criteria. Link longer
receipts; state skipped checks and material limitations. A merge alone does not
prove release or live acceptance. Keep unmet criteria open unless an agreed scope
change links durable follow-up work. Duplicate or not-planned closures need a
reason and a link to the canonical issue or decision. Documentation-only work may
cite link and consistency review without adding test requirements.

Shared operations belong in `internal/app`; CLI and TUI use those services and
`internal/present`. Provider networking is in `internal/discovery`; read-only
root-scoped inventory is in `internal/inventory`; configuration is in
`internal/config`. Verification logic is tested in `internal/verify`. Do not
move domain policy into a terminal update handler or duplicate it for MCP.
Inventory reads no bodies by default. `inspect --par2` (decision 0059) opts into
bounded metadata reads through the same file handle and actual budgets as hashes.
Set IDs and filenames remain declarations, packet MD5 is format evidence only,
and protected files are never opened from those names. Keep header coverage,
checksum evidence, hashes and recovery capability separate; recovery material stays.
`internal/mcpserver` is the MCP adapter (decision 0014): tools call `internal/app`,
return the CLI data in its envelope, and scope every path. Write tools need
`--allow-write`; long ones run as durable jobs in `internal/jobs` (decision 0045),
journaled in the state folder and owned through an OS lock. Network tools need `--allow-network`.
Plugin packages are `plugins/nemalo/` (decision 0019): they run `nemalo` from PATH,
bundle nothing, and share one portable skill; versions equal `internal/cli.Version`.
Provider adapters use the shared metadata HTTP client in `internal/discovery`;
preserve fixed hosts, checked direct destinations, bounded JSON, and quota handling.
Gutenberg search and catalog evaluation read a local `pg_catalog.csv` (decision 0028).
Do not query the Gutenberg website, and do not download ebook bodies from www.gutenberg.org.
`catalog fetch gutenberg` downloads that one CSV. `evaluate --files` and
`acquire gutenberg:NUMBER --file NAME` fetch one bibliographic record and one
pinned EPUB from gutenberg.pglaf.org only (decision 0029). Do not add a Gutenberg
MCP tool. Do not fall back to another host.
arXiv (decision 0047): explicit `arxiv:IDvN` only, `export.arxiv.org` and
`oaipmh.arxiv.org` only, one request every three seconds, no search or MCP tool,
and a latest-version license never applied to an older version.
Collections (decision 0050) are user files of exact selections; holdings are their
only resolved record, and `collection acquire` reuses the acquire service per item.
Source evaluation is declaration evidence, never file rights, safety, or write
authorization. See decision 0005 before changing access classification or networking.
The bounded collection harness is in `internal/collection` and `cmd/collection`;
its curated manifest is `collections/foundations.json`. Keep downloaded validation
content and local reports outside the checkout. Its intake is not checked publication.
When integrating production acquisition/assessment, reuse or deliberately relocate
these tested building blocks; do not grow a second transfer or publication policy.
Selected acquisition is in `internal/acquisition`, sharing `internal/transfer` and
the checked file HTTP adapter with the harness. Preserve fresh evaluation, exact
selection, exclusive packets, synced intent, retained failures, and untrusted status;
see decision 0007. Recordings reuse the same packet code per track (decision 0018):
resume only reuses a receipt-complete, rehashed track; never adopt or remove a
retained attempt. Byte resume (decision 0023) continues only the same strong-ETag
bytes and verifies the whole file; refusals leave the packet untouched.
Use `internal/safeio` for capability-bound roots and existing-file opens and
`internal/textsafe` for JSON/generated prose. Preserve parent aliases, opened
identity checks, lifetime compressed-reader budgets, and parse-equivalent escaping;
see decision 0008. Do not reopen a validated pathname for later sensitive use.
File/container checks are in `internal/assessment`, shared with the collection
harness; installed antivirus is in `internal/scanner`. Preserve measured facts
versus semantic completeness, explicit scanner opt-in and cloud-policy disclosure,
snapshot/hash binding, no remediation, and unsuccessful missing/incomplete scans.
Acquisition scans intake bytes and quarantines detections; `library rescan` batches
ClamAV through `assessment.CheckManyInRoot`. A batch result binds to its own
snapshot, and unattributable output is incomplete. Do not use `clamdscan`: its
daemon limits can report a partially scanned file as OK. See decision 0012.
Portable byte-identity snapshots, holdings, and audits belong in `internal/library`;
see decision 0004. Preserve explicit audit roots, exclusive outside-root outputs,
all duplicate locations, and historical evidence distinct from checked publication.
An incomplete traversal cannot establish missing files. A snapshot is not a journal.
Library identity/init/status and OS locks also belong in `internal/library`; see
decision 0006. The initialization journal stays two records. Managed operations
are separate intent/completed pairs (import, rescan); every writer completes a
trailing rescan intent before its own work. Preserve explicit roots,
retained corrupt state, canonical records, OS-held locks, and TUI write review.
User narration statements are `.nemalo/narrations.jsonl` (decision 0055): exact
held recording selections and `recording-bytes/1` identities, with one synced
declaration or withdrawal fact. Holdings remain the resolved record; provider
narration fields remain LibriVox-only. Keep user assertions, provider evidence,
and byte equality distinct, with unresolved states visible. Writes require a
preview plan and do not initialize a library, change holdings, or copy audio.
Archive extraction is `internal/staging` (decisions 0017, 0043, 0056): flat indexed staging
names, never member paths; RAR and 7z only through an installed 7-Zip, by exact name
to stdout; one archive staged at a time; account for every member and every entry
beside a packet; ingest reuses Import. RAR volume sets require isolated format,
role, ordinal, and size evidence plus matching split members across every
boundary and an error-free assembled listing. Preserve aliases, overlapping
families, unlinked boundaries, and unknown ordinals; never infer set identity
from names/counts alone. RAR cleanup remains refused. Split ZIPs are decision
0057: byte chunks join privately, true segments require consistent reader
evidence through every part. Segment cleanup is decision 0062: validate raw disk
records and complete header boundaries, then reuse shared framing over a view
that excludes only the initial marker and overlays only validated references.
Raw/view reads share one allowance; preserve unsupported bytes. Resolve
`.zNN` RAR/ZIP naming only from explicit sibling anchors; preserve ambiguity.
The shared `assessment.OpenZIP` bounds directory parsing, ZIP64 count-driven
preallocation, and lifetime compressed reads; retain EPUB's tighter budgets.
An extraction failure marks ingest incomplete while separately verified members
can still import; do not claim archive-wide publication atomicity.
Nested intake is `staging.ExtractTree` (decision 0058): shared budgets include
failed attempts, reader work, diagnostic stdout/stderr, subprocess calls, and
the outer deadline. Keep four archive levels, exact chain provenance distinct
from literal historical `!/` member names, and pending recovery first. Nested
sets stay preserved. Cleanup uses the same tree only with requested complete
coverage (decision 0063): one shared 16 MiB proof allowance, also charged to reader
work, and bounded 7z coverage probes through the shared runner. Default intake
does not request coverage. RAR at any depth preserves the whole outer source.
Every leaf must be held with verified stored bytes; container markers alone
confer no permission. Keep exact-chain refusals and parse-equivalent previews.
Bind every cleanup root snapshot to the plan's expected size/hash while copying.
Covered ordinary ZIPs use the same snapshot mechanism without an installed reader.
Charge actual copy-hash reads, including failures, to the existing 64 GiB source
hash budget. Before/final hashes alone cannot bind intervening extraction bytes.
`library check` (decision 0024) is the read-only whole-library report; it never repairs.
Source cleanup is `library/cleanup.go` and `restore.go` (decision 0022): move only held,
verified bytes into the holding area via verified copy, hard-link publish, then source
removal; resume from the manifest, never a new plan; never replace on restore. A 7z or
split 7z set moves only whole as 7-Zip lists it, header and packed sizes filling the
file, a set as one entry; a RAR never moves; a directory holding data is not a directory
(decision 0051).
Decision 0064 permits an independent bounded Go RAR framing accountant beside
the maintained reader. It is read-only evidence, not a decoder or cleanup policy.
Keep RAR cleanup disabled until that proof passes its complete verification,
then separately verify the versioned application and recovery integration.
Requested standalone RAR5 coverage binds every physical file record to one UTF-8
reader table before exact-name extraction; child RAR coverage remains refused.
Historical RAR-named archive entries and old continuations without a supported
proof cannot resume cleanup or purge, including pending purge intents. Preserve
their recorded bytes for restore; member holdings alone confer no permission.
Plain ZIP cleanup and purge share complete local/central/descriptor accounting
and a 16 MiB proof-read budget (decision 0060). Preserve comments, unknown metadata
and unsupported layouts. Pending historical purge plans cannot bypass the current
proof; `archive_not_accounted` keeps the bytes available to restore.
The shared proof is `staging.ZIPAccounted`. ZIP byte-chunk cleanup (decision 0061)
binds `zip-stream/1` to all exact parts and holdings. Disk segments bind
`zip-segments/1`, including the terminal `.zip`, under the same whole-set protocol.
The checked reader table must match raw central records and drive exact-name
extraction. No ordinary-ZIP proof can authorize a lone terminal segment.
Fresh purge checks the full
set and binds archive groups in a schema-2 manifest before removal; partial resume
checks remaining original hashes and current holdings against that supported proof.
Unknown evidence retains bytes, malformed binding requires review, and completed
removals never adopt replacement files. Earlier builds refuse these new manifests.
Nested moves bind `archive-tree/1` and exact `archive_leaves` to the manifest and
plan. Fresh purge repeats the entire proof; schema-2 groups bind `leaf_digest`
as well as proof and indices. Validate all bindings before any source write or
purge removal, once per entry/group rather than once per part. Unknown proofs
retain bytes; malformed bindings require review. Restore uses recorded source
bytes without re-extracting or replacing. Never reconstruct a partially purged tree.
`library/purge.go` (decision 0041) removes only held copies a current, verified holding
also has, through its own previewed plan and manifest; receipts and intents stay.
Readers and players get verified copies (`library open`/`export`), never stored
files; launch only through `internal/launch` argument arrays (decision 0016).
Never remove a lock file to release an active writer or treat initialization as
content validation or production import/cleanup recovery.
The terminal reader is `internal/reader` (layout and pacing, no I/O) under the
TUI and `nemalo read` (decision 0025); it stores no position, rate, or history.
Source-bound text retrieval belongs in `internal/content`, exposed by `internal/app`;
see `docs/CONTENT-ACCESS.md` and issue 1. Reuse assessment's container parser.
Preserve stateless offline reads, exact-byte/extractor-bound references, explicit
unsupported units, no silent continuation skips, and no inferred consumption.
The default is `epub-text/2` (decision 0011). Keep earlier representations
selectable and their continuations valid; changed rules need a new extractor ID.
Declared EPUB contents are `epub-nav/1` (decision 0026): nesting depth from the
navigation document or NCX, and a read stops at the next entry that is not a
descendant. Do not add a heading level to `epub-structure/1` part kinds.
Explicit bookmarks are `.nemalo/bookmarks.jsonl` (decision 0027), written only
by enable, save, note, delete, and import. The reader package still stores
nothing, and MCP has no bookmark tool.
Derive plain text from the structured extractor rather than adding a parser.
PDF parsing is `internal/pdftext`, exposed as `pdf-text/1` (decision 0021): use the
object parser only through its budgets and guards; unmapped codes are gaps, never guesses.
MP3 frame indexing is `internal/audio`, exposed as `mpeg-audio/1` (decision 0015).
M4B/M4A sample tables are `audio.ProbeMP4`, units-only `mp4-audio/1` (decision 0039):
read boxes at offsets, never load media data, and place every sample before checked.
Optional full decode is `internal/decode` over an installed FFmpeg (decision 0042): only
`check --decode`, file protocol and a fixed demuxer, never in import or the core path.
Keep probed and declared timing separate and never decode in the core path.
Re-validate range or preroll changes bit-exactly against a reference decoder. Test located gaps and range
coverage separately from source bytes, rendering, safety, and semantic completeness.

Test behavior and failure recovery, not just execution. Use synthetic fixtures;
never personal downloads. Tests never run an installed scanner or reach a network:
inject `app.Service.Scanner` and providers, since a nil scanner selects the real one. Give cleanup, parser boundaries, scanner skips, and
crash transitions adversarial tests. Fix root causes; do not weaken checks,
schemas, assertions, or coverage to obtain a pass. Self-review consequential edits.

Update canonical docs and milestone status when behavior changes. Keep temporary
research, diagnostics, and resumable scratch state in gitignored `.agents/`, without
secrets. Promote durable findings into docs, tests, decisions, or bounded tickets
when useful. Avoid parallel instruction files and unnecessary process.

Use concise professional writing. No emojis, em dashes, en dashes, AI authorship
attribution, generated-by text, or AI coauthor trailers in controlled project output.
Express product principles in plain technical language, without religious or cultural branding.
