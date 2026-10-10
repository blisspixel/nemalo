# Nemalo roadmap

This roadmap is a product and engineering plan, not a claim that the planned
features are implemented. The product is a local library and discovery system,
focused on books, ebooks, and audiobooks. It unifies discovery,
evaluation, acquisition, verification, ingest, organization, preservation, access,
and automation. Ebooks, research papers, audiobooks, MCP, Agent Plugins, structured
APIs, and TUI belong to the vision from the outset. Milestones stage delivery over
one Linux-first Go core, not a cleanup utility with discovery bolted on later.

## Version 1.0: library and discovery

The first stable release must make finding, collecting, and using books dependable.
Its acceptance centers on:

- Search legitimate sources; compare editions, languages, formats, rights, and
  availability; acquire selected ebooks and audiobook recordings reliably.
- Ingest messy folders and supported archives through the same assessment pipeline
  as downloads. Preserve uncertain material and make source cleanup recoverable.
- Maintain useful metadata, collections, exact duplicate handling, distinct editions
  and narrations, complete audiobook track sets, and traceable provenance.
- Check library integrity, recover interrupted operations, and preserve user data.
- Find holdings locally, retrieve references, and hand off actual ebook/audio assets
  to suitable readers or players. Built-in playback is not required.
- Provide native Go CLI, JSON, and scoped MCP/Agent Plugins interfaces over shared
  services, with the verification and platform evidence specified below.

Milestones deliver these workflows incrementally; the first end-to-end slice is
earlier than 1.0. Papers/textbooks and their provider plan remain in scope as library
expansion, but catalog breadth must not displace reliable book/audiobook workflows.
Additional capabilities below are not a blanket checklist for releasing 1.0.

Model-assisted analysis, generated Markdown knowledge workspaces, video/topic insight
watches, and inference/harness integrations are explicitly post-1.0. Existing MCP
library automation does not require them. Do not implement speculative analysis
infrastructure while the library release remains unfinished.

## First end-to-end slice

Build the smallest coherent lifecycle early: search a permitted source, evaluate
a specific edition/asset, acquire it, assess it through the same pipeline as an
existing local file, publish with provenance, retrieve/open it, and check integrity.
Expose typed CLI/JSON contracts and the initial MCP read tools over those services.

Milestones are dependency groups rather than a strict sequence. Provider search
and evaluation start alongside the foundation. Checked publication depends on
assessment; source deletion depends on recovery. Neither optional cleanup nor an
exhaustive source catalog is a prerequisite for discovery and acquisition.

The first production slice needs foundation, local intake, checked EPUB publication,
one real discovery/acquisition provider, preservation checks, local access, and
initial agent integration. Destructive cleanup remains unavailable until milestone 4
passes. Further providers, paper/audio policies, TUI, and richer content retrieval
broaden this same system rather than create separate applications. CLI and TUI
are first-class 1.0 interfaces; web/desktop interfaces are outside the current scope.

## Implementation status

Implemented in Go: CLI/TUI entry points over shared services, configuration,
Open Library/Internet Archive metadata search, Archive item/file evaluation,
bounded read-only inventory with optional hashing,
capability reporting, JSON envelopes, and pinned verification/native CI configuration.
Live source searches and Windows TUI search/inspection/holdings/evaluation/quit
smokes were exercised. TUI navigation, session drafts, adaptive no-color/light/dark
presentation, bounded budget selection, format filters, and result pagination exist.
Search/holdings include selection, adaptive detail panes, complete report access,
explicit focus, and staged Archive evaluation without automatic requests.
File-level `check` is implemented in CLI/TUI: bounded snapshots, signatures,
EPUB structure/text facts, expected size/hash comparison, and optional antivirus.
This is assessment evidence, not checked library publication or semantic validation.

Portable byte-identity catalog snapshots, literal holdings search, and explicit-root
preservation audits are implemented in CLI/TUI. Exact duplicates retain every
location; optional assessment records historical EPUB metadata and file health.
These snapshots do not implement checked publication or a mutable library journal.

Explicit initialization/status provide a stable library ID, nonblocking OS
reader/writer locks, and a synced two-record initialization journal. Valid
interrupted initialization resumes the same identity; corrupt state is retained.
See [decision 0006](docs/decisions/0006-library-control-state.md).
Single-asset import into that library is implemented: one EPUB, PDF, MP3, or
completed intake packet, with a separate operations/holdings journal, crash
resume, and managed audit. Checked status is limited to an EPUB whose checks
passed and whose scan reported no detections. See
[decision 0009](docs/decisions/0009-managed-epub-import.md).

Milestone 1 remains in progress. Managed holdings record EPUB package
identifier evidence for an explicit ISBN, DOI, or Open Library work or edition
key, and they do not merge files on that evidence. See
[decision 0010](docs/decisions/0010-bibliographic-identity.md). Recording and
track identities remain outstanding. Cleanup, archive extraction, and download
resume are also outstanding. Native archive/checksum packaging is
implemented; releases still require the actual verification and publication gates.
Milestone 2
has inventory and standalone signatures/private snapshots, without archive
extraction or production staging. Milestone 5 has
two discovery adapters, source-declared Archive file/access/rights evaluation,
and selected EPUB/PDF/MP3 acquisition into exclusive untrusted intake packets.
Validator-bound resume, complete track sets, and checked publication remain planned.
All remaining production
capabilities stay planned. A configured CI workflow is not itself a hosted pass.
The initial foundation passed hosted native CI on Linux, macOS, and Windows.
The [collection harness](docs/VALIDATION.md) separately exercises curated real assets
in untrusted intake; it does not complete production transfer or publication gates.
The 2026-10-08 Windows exercise acquired all 100 selected resources (208 content
assets) and recorded an external Defender scan reporting no threats. Its exact
checks and remaining validation gaps are recorded separately from production status.
Repeat validation preserved that selection and added four resources through three
content sources. The external intake now has 104 resources and 216 content assets;
all passed the current limited CLI checks, and an installed external diagnostic
decoded all 123 MP3 tracks. These are particular validation results, not production
audio support or publication gates. See [validation evidence](docs/VALIDATION.md).

## Product-wide acceptance

Every milestone must preserve the [product commitments](docs/PRODUCT.md#product-commitments).
Add checks at the relevant service boundary, not only prose or UI assurances:

- Verify unavailable/skipped checks prevent checked publication and default reader
  handoff; show uncertainty without absolute safety claims.
- Prove offline local operations make no network requests. Assert permitted hosts
  and request fields for provider operations; no library inventory, notes, or reading
  history in lookup payloads. No telemetry, hidden uploads, or remote crash reporting.
- Exercise CPU/worker, memory, disk, transfer, and spending limits, including prompt
  cancellation and restart reconciliation. Benchmark resource claims before publishing them.
- Check terminal output without color and in noninteractive mode, useful help and
  errors, and full CLI/JSON functionality; assess keyboard access when TUI arrives.
- Explain curation criteria and missing evidence. Use representative multilingual
  examples for usefulness review; no sponsored ranking or engagement objectives.
- Demonstrate that selected content can actually be opened/retrieved and referenced,
  not merely downloaded. Keep notes/bookmarks optional and local, and preserve their
  asset/version links through updates and recovery.

Mechanical tests establish paths, boundaries, state, and evidence. Relevance,
recommendation quality, and accessibility also need judgment and realistic review;
coverage or a fabricated score cannot certify them.

## Milestone 0: settle scope and language

Status: complete. Go confirmed by the user on 2026-10-08.

The prototype demonstrated eight imported EPUBs from loose downloads, one further
EPUB from the supplied collection, and four Gutenberg classics in different
languages. It also exposed incomplete archives, ordinary XHTML entity handling,
unknown language metadata, and the need to preserve non-EPUB books. These examples
are workflow evidence, not a performance benchmark or production certification.

Decisions:

- Go is the final implementation language, recorded in the language and stack decision.
- Linux CLI use is primary, with native macOS and Windows support.
- Go throughout application code, tests, and first-party executable tooling.
  Remove legacy prototype source, packaging, and CI in the Go foundation milestone
  after capturing useful requirements and synthetic fixture scenarios.
- Separate local intake, catalog discovery, and source cleanup in the product model.
- Keep EPUB as the default library format and retain other formats for review.
- Separate PDF paper and audiobook policies, preserving publication versions and recordings.
- Minimal justified dependencies; official Go MCP SDK when MCP implementation begins.
- Agent Plugins packaging, without embedding another agent runtime.

This phase records design and standing instructions; it does not implement the
Go application or change the user's collection.

## Milestone 1: portable CLI and durable state

Status: in progress. Depends on milestone 0; see implementation status above.

Delivered increment: versioned immutable byte-asset snapshots, duplicate locations,
holdings queries, and preservation audits. See the
[catalog decision](docs/decisions/0004-portable-library-snapshots.md).
Library identity, initialization/status, reader/writer locking, and initialization
recovery are implemented. Single-file managed import is a separate journal; see
[decision 0009](docs/decisions/0009-managed-epub-import.md). EPUB package
identifier evidence is recorded on those holdings; see
[decision 0010](docs/decisions/0010-bibliographic-identity.md). Recording, track,
and cross-file work relationships remain outstanding.

Deliver `doctor`, configuration loading, explicit library/review paths, a versioned
catalog schema, and a durable run journal. Use JSON metadata initially; the catalog
is rebuildable from assets and provenance, while the run journal records in-progress
operations and must survive interruption. Support one writer per library. Establish
the Go build, tests, and native CI before porting intake features.

Define discovery/evaluation/acquisition and content-access contracts alongside
intake, with a small provider vertical slice rather than a cleanup-only scaffold.

Acceptance:

- No hardcoded drive letters or implicit scans of home or download folders.
- Linux XDG locations and documented macOS/Windows equivalents.
- Stable terminal output, machine-readable JSON, exit codes, and signal handling.
- CLI flags override environment and config; configuration precedence is documented.
- Missing archive/scanner tools produce actionable capability reports.
- One native executable per release target, with checksums and build metadata.
- Synthetic fixture tests, without personal or copyrighted source dumps in the repository.
- Remove legacy Python source/tests/manifests/workflow and obsolete install instructions.
  No Python runtime, wrapper, or build/test dependency in the product.
- Pinned Go verification tools, enforced coverage, and native CI replace prototype checks.
- Establish work, edition, publication-version, recording, track, and asset identities.

## Milestone 2: inspect and stage local downloads

Status: partial. Inventory and standalone file checks exist; production staging
and archive extraction remain planned. Depends on milestone 1.

Deliver recursive inventory, file-signature checks, ZIP handling, additional archive
adapters, and private staging. Inventory includes links and unsupported entries as
explicit findings, without following them.

Acceptance:

- Exercise nested ZIP/RAR/7z fixtures, multipart sets, absent volumes, and password protection.
- Limit depth, entry count, expanded bytes, per-file bytes, memory, runtime,
  subprocess output, concurrency, and disk usage.
- Prevent traversal, symlink/hardlink escapes, Windows reparse-point escapes, and
  case or normalization collisions. Test filesystem replacement races where possible.
- Restrict writes to designated staging and destination roots.
- Resolve multipart relationships by container identity; unrelated volumes remain untouched.
- Preserve archive originals if any contained material has not been accounted for.
- Cancel and resume safely; incomplete extraction is never successful recovery.
- Inventory PAR2 sets and retain them. Do not silently repair or discard them.

## Milestone 3: assess and publish EPUBs

Status: partial. Shared EPUB checks, optional scanners, and single-asset managed
import exist. Work and edition identities, archive extraction, cleanup, and a
full checked-library policy remain. Depends on milestone 2.

Deliver EPUB package checks, content checks, scanner adapters, metadata handling,
exact duplicate detection, and verified library publication.

Acceptance:

- Validate container/package references, reading order, resource presence, and ZIP CRCs.
- Keep file size, trusted expected size/hash, text quantity, reading order, and
  declared pagination separate. Compare known edition evidence without arbitrary
  minimum novel lengths or converting words into supposedly measured pages.
- Add mature PDF page-tree and audio decoding/duration checks with malformed
  fixtures when those format policies ship; retain unknown results until then.
- Handle standard XHTML entities locally; reject unsafe custom entity resolution.
- Report active content, executables, external resources, and encrypted content.
- Distinguish font obfuscation from DRM; preserve uncertain files.
- Start with ClamAV on Linux/macOS and Defender on Windows; retain tool versions,
  scan logs, database information when available, and coverage/limit findings.
- Do not treat antivirus skips, truncation, or size limits as clean results.
- Unavailable or failed scanning prevents checked publication but not inventory.
- Normalize language tags without inventing missing metadata; support base-language filtering.
- Use exact hashes for automatic duplicate removal. Treat text similarity as a review signal,
  preserving translations, illustrations, annotations, and distinct editions.
- Verify destination checksums before committing catalog entries.
- Preserve source URLs, rights declarations, edition IDs, and original filenames.
- Provide an optional EPUBCheck adapter with reported availability and version.

## Milestone 4: recoverable cleanup and maintenance

Status: planned. Depends on milestone 3. Required for a production cleanup release.

Deliver plan-based source cleanup, review handling, library checking, and interruption
recovery. Cleanup defaults to a recoverable holding area; permanent purge is a later,
explicit action with a separate preview.

Acceptance:

- `cleanup --dry-run` lists exact dispositions and reasons for a completed import run.
- Applying a plan rechecks source identities and destination checksums, refusing changed inputs.
- Append durable intent before mutation and durable completion afterward.
- Test crashes before/after copying, verification, publication, catalog updates, and source moves.
- Cross-filesystem operations copy and verify before removing an original.
- Disk-full, permission-denied, name-conflict, scanner-failure, and concurrent-run cases preserve data.
- Review destinations cannot overwrite content or escape their configured root.
- `check` reports missing, modified, invalid, and unscanned entries without silently fixing them.
- Garbage collection never removes unaccounted-for archive contents or recovery files.

## Milestone 5: collection discovery and selected downloads

Status: partial. Open Library/Archive search, Archive source evaluation, and
selected-file intake acquisition exist in CLI/JSON/TUI. See the
[acquisition contract](docs/ACQUISITION.md). Resume, complete recording downloads,
Gutenberg production integration, and broader provider contracts remain
planned. Transfers and checked publication use milestones 2 and 3. Source cleanup
is independent and requires 4.

Complete Gutenberg and Archive acquisition over the existing Go provider core.
Use Gutenberg's permitted machine catalogs and acquisition routes rather than
porting automated website search unchanged. See [source research](docs/SOURCES.md).

Acceptance:

- Provider interfaces distinguish works, editions, assets, availability, and rights.
- Search is read-only and bounded, with pagination, caching, and provider rate limits.
- One provider's failure does not hide another's successful results.
- Downloads use bounded HTTPS requests, checked redirects, timeouts, and temporary files.
- Downloads enter the local assessment pipeline; provider identity does not imply safety.
- Open Library resolves selected editions to public Archive assets, avoiding arbitrary editions.
- Restricted, private, lending-only, and absent files get clear availability messages.
- Providers without language metadata do not silently satisfy a language filter.
- Multilingual starter collections are explicit selections, not unbounded site mirrors.
- Unit tests use synthetic contracts; small live checks run separately from offline CI.
- Include an actual search/evaluate/acquire/verify/access smoke workflow in the
  first production slice, alongside import of an existing download.
- Enforce provider request/download budgets; no unconfigured paid usage or silent
  fallback to another source or edition.

## Milestone 6: local agent integration

Status: planned. Read-only tools depend on milestone 1 and the relevant provider
slice from 5; inspection needs 2; import/acquisition need 3; source cleanup needs 4.

Deliver stdio MCP using the official Go SDK, typed tools over shared services,
and a portable Agent Plugins package. [Integration design](docs/AGENT-INTEGRATION.md)
records checked protocol/package versions; reverify stable releases before pinning.

Acceptance:

- Read-only defaults, explicit network/inspection capabilities, and configured roots.
- Structured bounded results, stable errors, stderr diagnostics, protocol-only stdout.
- Import/cleanup enforce the same scope, plan validation, and recovery as CLI.
- Test denied writes, stale plans, oversized messages, disconnects, and cancellation.
- Validate matching manifest/MCP schemas and fixed paths; no executable downloader,
  embedded secrets, or another runtime in the plugin package.
- Test a real host/protocol/platform compatibility matrix before claiming support.
- Durable application jobs use explicit IDs and journaled state, independent of
  connection lifetime. No queue service or daemon dependency.
- Include local asset retrieval and evidence references, with bounded content
  access added per format rather than limiting agents to catalog metadata forever.

## Access, preservation, and collection expansion

These are cross-cutting workstreams, not optional product add-ons:

Offline EPUB retrieval is implemented through shared Go services, CLI/JSON, and
TUI unit selection/repeat/continuation. [Content access](docs/CONTENT-ACCESS.md)
records exact locators and bounds. [Structured EPUB](docs/STRUCTURED-EPUB.md)
records implemented parts, language/direction, local notes/backlinks, source XML,
image-member identity, and per-range gaps from
[issue 1](https://github.com/blisspixel/nemalo/issues/1) and
[issue 2](https://github.com/blisspixel/nemalo/issues/2).
Table grids and rendered mathematics are explicitly unsupported, with original
markup available separately. MCP, PDF/OCR, decoded audio ranges, and external
caller validation remain open; neither full issue is complete.

- Initial local access: resolve a stable library ID to exact assets/provenance,
  export/open in an explicitly configured reader, and report changed/missing files.
- Content access: bounded format-specific extraction, full-text search, and locators
  for citations; unsupported extraction stays explicit. Derived indexes are rebuildable.
  Preserve actual source order and natural-unit evidence; distinguish result caps
  from end-of-unit/source. Bind references to bytes and extractor configuration.
  Retrieval never advances consumption state or substitutes summaries for sources.
- Collections: declarative intent plus resolved edition/version/recording and asset
  records. Repeated acquisition is idempotent; updates show a diff before changes.
- Preservation: manifests, backup/restore documentation, journal recovery, and
  optional matching reacquisition. Provider availability does not replace backups.
- TUI and structured APIs: shared services and contracts, preserving full CLI
  functionality and permission boundaries. Add interfaces without duplicated logic.
- Useful curation: user-controlled filters, saved searches, and explained selections;
  measure task usefulness rather than growth, clicks, or time spent in the application.
- Optional local notes, bookmarks, and reading lists with stable content references,
  export/recovery support, and explicit handling of edition-update conflicts.

## Milestone 7: research papers and PDF collections

Status: planned. Depends on shared assessment/publication and recovery milestones.

Deliver a PDF assessment policy, a separate paper collection, arXiv and Europe
PMC/PMC resolution, then OpenAlex discovery and Unpaywall DOI resolution. Enable
PDF ebooks/textbooks explicitly; PDF-to-EPUB conversion is unnecessary.

Acceptance:

- Select a maintained PDF parser/validator through the dependency policy. No
  header-only validation or keyword scan advertised as complete PDF assessment.
- Account for encryption, active actions, attachments, external references,
  malformed files, parser limits, and antivirus coverage. Uncertainty routes to review.
- Preserve DOI/PMCID/arXiv IDs and versions; display preprint, review, correction,
  and retraction evidence without treating availability as scientific endorsement.
- Export documented citation metadata, such as CSL-JSON, with provenance and
  unknown fields retained rather than invented.
- Define physical PDF page indexes separately from printed labels, and bind exact
  quotations to asset/version, source page/section, extractor, and extracted span.
  Disclose missing text, two-column order uncertainty, scanned/mixed pages, and
  located figure/table/math gaps. Optional OCR retains derivation/confidence evidence
  and never silently replaces exact original text. Use the shared access contract.
- Use approved retrieval channels, per-asset rights, quotas, and configured usage
  budgets. Retired Unpaywall search is not an implementation target.
- Add DOAB/OAPEN, OpenStax, and LibreTexts as a separate textbook/scholarly-book
  cohort after stable contracts and actual asset formats are verified.
- LibreTexts exposes offered web/PDF assets and book licenses without implying EPUB support.
- Test different paper versions, annotated files, poor scans, and metadata
  conflicts. Never merge on title, DOI, or extracted text alone.

## Milestone 8: audiobook recordings and tracks

Status: planned. Depends on shared assessment/publication and recovery milestones.
Can follow milestone 5 independently of paper-provider expansion.

Deliver local MP3/M4B intake and LibriVox acquisition, permitted Archive audio
resolution, recording/narrator identities, ordered tracks, and durable downloads.

Acceptance:

- Select bounded metadata/container assessment and optional decoding tools
  deliberately; disclose probe coverage and unavailable checks. No mandatory transcoding.
- Preserve chapter order, track IDs, declared counts/durations, narrator, language,
  and abridgment. Missing or failed tracks keep a recording incomplete.
- Provide a versioned source-bound recording manifest with ordered track hashes
  and chapter/range evidence. Separate declared, probed, and decoded timing; retain
  unknown durations, gaps, overlaps, and uncertain boundaries. Specify track-local
  timestamp units and, where verified, sample rate/channels and half-open sample
  ranges, including encoded versus presented/resampled timing.
- Retrieve actual bounded source ranges or explicitly authorized local references
  usable by an external player. A transcript or TTS version is a distinct derived
  asset. Probe/preparation/delivery never acknowledges playback; clients own that
  lifecycle. Test real range boundaries and distinct narrations independently.
- Preserve different narrations. Deduplicate alternate hosts using recording
  evidence and exact asset hashes, never book title or text identity alone.
- Interrupted multi-file downloads resume or restart with matching remote validators;
  never publish a partial recording as complete.
- Preserve source text, recording, cover, and contributor rights separately.
- Demonstrate output usable by a real audiobook reader/server. Any Audiobookshelf
  integration remains optional and separately verified.
- Playback, M4B generation, normalization, AI narration, and speech-to-text are later work.

## Additional planned capabilities

These capabilities remain in the vision, with delivery after the first end-to-end slice:

- PAR2 verification, then opt-in repair with originals retained and output revalidated.
- P2 source cohorts, generic permitted OPDS feeds, and reviewed collection manifests.
- Richer edition matching, metadata correction previews, and optional language inference.
- SQLite indexing if measured size, query time, or recovery complexity justifies it.
- Extend the first-class TUI alongside CLI/JSON as library operations arrive.
- Background watchers and scheduled library jobs. Cloud sync, web/desktop interfaces,
  and multi-user access are separate future proposals, outside the CLI/TUI 1.0 scope.
- Optional reader/server export, additional audio formats, and full-text indexing
  only after demonstrated use and separate resource/quality evaluation.

No PDF conversion, OCR, AI enrichment, scraper framework, or service infrastructure
is needed to prove the core workflow.

## Post-1.0: topic watches and optional knowledge processing

Status: explicitly post-1.0, deferred until all pre-existing roadmap work is complete,
including the additional planned capabilities and cross-cutting workstreams.
Conditional choices above remain conditional; this gate does not require adopting
a database or other option that evidence does not justify. This phase must not
become a dependency of 1.0 or displace the existing library product plan.

Implement these capabilities within Nemalo itself, through its shared Go services.
The goal is information gathering into a portable Markdown knowledge workspace,
with topic watches and optional insights that accumulate across updates. Processing
can use local inference, a model provider such as OpenRouter, or an agent harness
such as OpenCode. Provider and harness are distinct roles, not required products.
[Distillr](https://github.com/blisspixel/distillr) is a workflow research reference
only and will not be integrated. Design a fresh Go implementation; do not reuse,
port, embed, invoke, or depend on its code, prompts, configuration, tests, or packages. The
[research record](docs/RESEARCH.md#deferred-video-and-knowledge-processing-research)
distinguishes documented workflows from independently verified behavior.

### Discover and follow updates

- Search YouTube by topic, channel, language, and date through permitted interfaces.
  Preserve stable video/channel identifiers, original links, and discovery provenance.
- Follow explicitly selected channels, playlists, topics, and permitted feeds using
  existing collection and scheduling services. Offer refresh and catch-up previews
  and local digests of new, changed, unavailable, and already-seen items.
- Save topic watches with the user's question, sources, languages, cadence, lookback,
  and scope. Support links-only digests and explicitly enabled acquisition/insight
  policies. A configured policy can authorize recurring bounded runs without a new
  confirmation for every item; changing its scope or spending limits is explicit.
- Keep discovery, acquisition, and analysis independently selectable. A basic update
  check makes no model calls. An insight-enabled watch can acquire eligible evidence
  and process it within its configured policy, reporting partial results honestly.
- Persist pagination and reconciliation checkpoints. Handle overlap, late arrivals,
  exact-ID deduplication, partial failures, and interrupted jobs without silently
  advancing past failed work. Distinguish failed refreshes from successful empty ones.
- Respect provider quotas, credentials, refresh/deletion requirements, and resource
  limits. Recheck current provider policies before implementing retained snapshots.

### Capture eligible evidence

- Accept permitted captions/transcripts and user-provided material. Preserve language,
  timestamp locators, origin, and human/generated transcript status where known.
- Treat metadata-only results as links and discovery evidence, not full video content.
  Public availability does not establish download rights or caption access. The
  official [YouTube caption-download API](https://developers.google.com/youtube/v3/docs/captions/download)
  requires permission to edit the video.
- Consider optional local transcription only for authorized audio/video, with a
  separate dependency and resource decision. No mandatory media downloader or
  transcription runtime; no Python wrapper as a shortcut.
- Reuse intake, assessment, asset identity, rights, provenance, and preservation
  services. Apply platform retention/deletion rules to provider data rather than
  promising permanent copies of every remote record.
- Extend permitted capture to selected web articles, feeds, and user-supplied notes,
  not just videos. Distinguish fetched evidence from linked metadata and generated
  text. Keep original ebooks, PDFs, and audio alongside referenced text extractions;
  gathering into Markdown must not destructively convert the source library.

### Optional model providers and agent harnesses

- Processing is disabled by default. Support deliberately configured local inference
  and remote providers, with OpenRouter an initial candidate. Provider adapters make
  model requests; a harness coordinates reasoning and tool use. Support an optional
  external harness such as OpenCode without requiring it for direct model calls.
- Keep first-party orchestration, adapters, tests, and tooling in Go. Evaluate the
  provider's documented HTTP API before adding an SDK. Harnesses can consume Nemalo's
  bounded MCP tools or use a deliberately supported HTTP adapter. Avoid a dependency
  for every provider and do not assume nominally compatible APIs behave identically.
  External runtimes remain optional, declared integrations, not core dependencies.
- Verify the effective model/provider route, data destination, permissions, and cost
  before submitting selected evidence. A loopback agent endpoint does not prove that
  inference is local. Never silently fall back to cloud inference or a paid route.
- Scope processing to selected evidence. Enforce tool, filesystem, and network
  boundaries outside prompts; retrieved content cannot authorize shell commands,
  unrelated file access, acquisition, cleanup, or configuration changes.
- Reuse durable jobs with bounded context, concurrency, runtime, cancellation, and
  idempotency. Metered processing needs configured authorization and enforceable
  budget reservations across restarts and concurrent jobs. Reconcile unknown billing
  before retrying; do not promise a hard cap through an adapter that cannot enforce it.
  Do not auto-install models or expose provider credentials to source content.

### Persistent Markdown knowledge workspace

- Offer an optional workspace of source summaries, topic/concept pages, comparisons,
  and cited answers, with a navigable index and change history. Support incremental
  updates rather than regenerating the whole workspace on each refresh.
- Preserve originals, traceable text extractions, human notes, and generated pages
  as distinct layers. Generated pages cite original evidence, not merely each other.
  Markdown is an access and synthesis format, not a replacement for source assets.
- Keep workspace conventions portable and versioned, independent of any editor or
  harness. Workspace content cannot change Nemalo's permissions or load downloaded
  agent instructions. Obsidian, Git, embeddings, and a wiki server are optional.
- Stage page/index changes through the shared journal and publication services.
  Detect concurrent or user edits before replacement; preserve conflicts for review.
  A processing run cannot rewrite source evidence or erase human notes.
- Check broken links, missing references, and stale dependencies mechanically.
  Surface possible contradictions and knowledge gaps as review findings; semantic
  consistency cannot be established by link checks or model agreement alone.

### Cited insights and synthesis

- Offer summaries, topic briefs, comparisons, disagreements, cited questions and
  answers, and suggested next reading across selected books, papers, audio, and video.
- Topic-watch briefs explain what changed since the last successful run, which
  previous conclusions may need revision, and what remains unresolved. No new
  evidence is a valid result, not a reason to invent insights or repeat old claims.
- Keep captured evidence, extracted text, human notes, and generated interpretation
  distinct. Preserve originals and export portable Markdown/JSON; no mandatory vector
  database or hosted knowledge service. Generated wiki pages are an optional local view.
- Bind derived artifacts to input hashes, exact editions/versions, passage or timestamp
  locators, model identity, and processing settings. Record coverage gaps and uncertainty;
  abstain when evidence is insufficient.
- Track derivation dependencies. Changes, corrections, or withdrawals mark affected
  outputs stale and permit targeted recomputation with prior versions retained where
  permitted. Remote removal alone does not establish that a claim was false.
- Validate schemas, citation targets, quoted spans, and reproducible factual checks.
  These checks do not prove semantic truth; evaluate faithfulness, useful synthesis,
  and unresolved disagreement with representative evidence and review.

Acceptance:

- Demonstrate a permitted live discovery/update journey and offline fixtures for empty
  results, quota exhaustion, inaccessible captions, late arrivals, source changes,
  removals, pagination, concurrent refreshes, and interruption/recovery.
- Prove basic update checks make no inference calls and the existing product works
  with all processing integrations absent. Exercise a topic watch with configured
  insight processing, including empty updates and partial evidence. Test the actual
  supported provider/harness API versions, route restrictions, and budget exhaustion.
- Verify selected-evidence scope, local-mode network boundaries, no hidden upload,
  no paid fallback, cancellation, and resource limits at runtime.
- Show that malicious source instructions cannot authorize tools or wider access;
  malformed or unsupported model output cannot become canonical source facts.
- Demonstrate a selected video or library asset becoming a cited output, then a
  source correction invalidating only affected derived outputs.
- Demonstrate gather -> source capture -> Markdown pages -> cited query -> incremental
  update, including journal recovery, user-edit conflicts, and original-source citations.
- Evaluate multilingual citation accuracy, faithfulness, and usefulness; measure local
  resource costs. Retain native platform CI and the existing Go verification/coverage
  gates. Do not claim superiority to Distillr without comparative evidence.

## Release gates

Every production milestone must pass formatting, lint/static analysis, and relevant
tests: `gofmt`, `go vet`, pinned mature static analysis such as Staticcheck, `go test`,
supported-platform race tests, vulnerability checks, and at least 80% measured
coverage. Coverage supports meaningful recovery and failure tests; it cannot
replace them.

Run native CI on Linux, macOS, and Windows. Initially target Linux amd64/arm64,
macOS arm64/amd64, and Windows amd64. Cross-building alone does not establish
runtime support. Race instrumentation may require a native C toolchain in CI even
when release builds do not use cgo.

Release gates include packaged-binary smoke tests, hostile archive fixtures,
scanner error/skip cases, parser fuzzing, and cleanup fault injection. CI must enforce
formatting and coverage thresholds, not merely print them. Include all first-party
packages in the profile and test destructive paths individually. Publish checksums
and dependency requirements. Do not claim hosted CI passed without an actual run.

Current Go verification commands:

```sh
go run ./cmd/verify
go test -race ./...
```

The Go verification helper rejects formatting differences and enforces unrounded
coverage, merging identical blocks from cross-package tests. Staticcheck and
govulncheck are pinned development tools, not runtime dependencies. Native CI
also builds and smokes the executable with cgo disabled. Race checks require cgo.

## Remaining implementation choices

1. Select the first Linux distribution/environment for native integration testing.
2. Confirm holding-area retention before enabling permanent purge.
3. Decide whether EPUBCheck conformance is required or an optional assessment.

The language is settled. These choices can be refined in their milestones with
conservative defaults; they do not justify starting additional stacks or services.
