# Nemalo architecture

Design updated 2026-10-08. The Go foundation is implemented as described in
[development and operation](DEVELOPMENT.md); the remaining production architecture
below is planned. The stack is Go throughout first-party application code, tests,
and executable tooling. Linux CLI use is primary; macOS and Windows are native
targets with platform-specific filesystem and scanner adapters.

## Product boundary

Nemalo owns discovery, evaluation, acquisition, verification, existing-download
intake, organization, preservation, content access, automation, and expansion.
These form one product contract. The [product contract](PRODUCT.md) is the canonical vision;
shipping order does not determine which capabilities belong to the product.

The 1.0 product centers on a local library and discovery for books, ebooks, and
audiobooks. Papers/textbooks extend the library; generated analysis and knowledge
workspaces are post-1.0. MCP library tools do not require inference or a model harness.
The [1.0 scope](../ROADMAP.md#version-10-library-and-discovery) defines the release boundary.

Use package-manager ideas where they fit: stable resource IDs, source catalogs,
resolved selections, integrity evidence, reproducible collections, explicit updates,
and recoverable operations. Content packages cannot execute installation scripts.
The apt/git analogy does not require implementing those tools inside Nemalo.

Existing readers and audiobook servers can use the collection. A browser UI,
playback server, cloud account, LLM, database server, and background daemon are
unnecessary for the first release. CLI and TUI are first-class interfaces now;
web/desktop interfaces are outside the current scope. Structured access and MCP
must use the same application services.

Keep the core usable offline. Local metadata stays local unless the user invokes
a provider lookup or another explicit network operation. Discovery queries expose
their query terms to the selected providers; local inventory does not.

## User trust, privacy, and useful curation

Preparation means checking capabilities, resource budgets, destination permissions,
and recovery state before starting work. Material must satisfy its format and
assessment policy before checked publication or default reader handoff. A failed
or missing check cannot be hidden behind a successful download. Inspection itself
must remain bounded and avoid rendering or executing content.

No telemetry, covert reading tracking, hidden uploads, or remote crash reporting.
Keep catalogs, notes, bookmarks, search indexes, and diagnostic logs local. Request
only the data needed for the explicitly selected network action; do not send a
library inventory to enrich one title. Show which providers receive query terms,
identifiers, or credentials. External readers and agent hosts have their own privacy
boundaries, which Nemalo's local-first design cannot guarantee on their behalf.
Reports are user-exported, redacted, and bounded; never auto-send them.

Set budgets for CPU/concurrency, memory, staging/storage, transfer volume, and
provider spending. Show estimated transfer/storage requirements where available,
distinguish estimates from measurements, and stop safely when limits are reached.
Cancellation should promptly stop work and reconcile partial state. Background
watchers and scheduling, if added, are explicitly enabled and inspectable.

CLI/TUI design prioritizes clear capabilities, predictable output, readable errors,
keyboard access, and useful help. Avoid decorative noise; do not equate minimal
visual design with usability. Do not rely on color, animation, a cover image, or
an interactive prompt to convey required information. Preserve noninteractive
automation and structured error details.

Recommendation and curation objectives follow the user's stated question, language,
format, access needs, and collection preferences. Expose the evidence and criteria
behind suggestions. No sponsored ordering, engagement optimization, artificial
urgency, or pressure to acquire more. Provider ordering, missing metadata, language
coverage, and editorial choices can introduce bias; make those limits visible
instead of promising perfect neutrality. Abstain when evidence is insufficient.

Begin with explicit filters, saved searches, and reviewed collection manifests.
Do not manufacture relevance or quality from arbitrary keyword scores. More
advanced semantic recommendations require evaluated usefulness and user control
before adoption. Recommendations cannot acquire content without an authorized
acquisition action or a configured collection policy.

Help users turn holdings into usable references: local reading/listening handoff,
exact-edition citations, chapter navigation, bounded retrieval, and optional local
notes, bookmarks, and reading lists. Preserve user-authored notes as distinct data
linked to asset identity and locators. Edition updates must retain those links or
surface conflicts rather than quietly attaching notes to different content.
Evaluate successful tasks and useful retrieval with synthetic fixtures or explicit
user feedback, not surveillance, session duration, or the number of files acquired.

## One application core

```text
CLI / TUI / JSON / planned MCP
              |
      application services
              |
 discover -> evaluate -> resolve -> transfer
                                      |
 explicit local folders --------------+-> plan -> stage -> assess -> publish
                                                                        |
                            access <- catalog/provenance <---------------+
                                          |
                         preserve / check / recover / optional cleanup

Shared infrastructure: durable journal, provider/archive/scanner/platform adapters
```

Adapters translate external contracts. Services own policy and state transitions;
CLI and MCP must not duplicate cleanup, rights, or validation logic. Start with
small packages organized around real boundaries, not a plugin framework or an
interface for every struct. Introduce interfaces where external behavior needs
substitution, especially filesystem mutations, processes, clocks, and providers.

## Identity and catalog model

| Entity | Purpose and identity |
| --- | --- |
| Work | Intellectual work; links translations, editions, and recordings without merging their files |
| Edition | Language, translator, publisher, date, ISBNs, illustrations, abridgment, series position |
| Publication version | Paper DOI/PMCID/arXiv ID and version; submitted, accepted, published, corrected, or withdrawn evidence |
| Recording | Narrator(s), language, source recording ID, abridgment, duration, edition relationship |
| Asset | Actual EPUB/PDF/audio file; byte size, SHA-256, media type, relative path, source and checks |
| Track | Ordered recording component; chapter label, source ID, duration, asset reference |
| Collection | Explicit selection or saved search, with language/subject/format criteria |
| Run and plan | Immutable inventory and proposed dispositions, applied state, recovery evidence |

Identifiers are namespaced and plural. A DOI is not a file checksum; an ISBN often
identifies an edition; an Archive item may contain multiple formats or recordings.
Represent uncertain relationships with evidence and confidence. Never collapse
two records merely because title and author strings match.

Keep bibliographic language separate from the language of a specific asset or
recording. Preserve raw provider values alongside normalized language tags. Unknown
language does not satisfy a requested language filter. Multilingual recordings and
collections can have several language values.

Each asset stores access mode (`direct_download`, `web_read`, `stream`,
`borrow_only`, `discovery_only`), raw and normalized license, license URL, rights
statement, jurisdiction when stated, source IDs, landing page, verified asset URL,
retrieval/check dates, attribution, and evidence origin. Metadata licenses and file
licenses are distinct. Time-limited URLs are not permanent identifiers; redact
credentials and signed query values from ordinary logs and exports.

Store download availability and permission to redistribute separately. Unknown
rights remain unknown. A public-domain text does not establish the status of a
translation, cover, narration, or all the files in an archive. Preserve preprint,
peer-review, correction, and retraction evidence without inventing a quality score.

## Format policies and output

Proposed collection layout, with configurable organization templates:

```text
library/
  ebooks/<language>/<author>/<title> [edition-id].epub
  papers/<year>/<title> [publication-id].pdf
  audiobooks/<language>/<author>/<title> [recording-id]/
    001 - Chapter title.mp3
    recording.json
  .nemalo/                 catalog and provenance
review/                   preserved alternate or uncertain material
quarantine/               detected or policy-rejected content
state/                    run journals, locks, cleanup holding area
```

Paths are presentation, not identifiers. Sanitize reserved names and control
characters, handle Unicode/case collisions, and use a stable suffix. Series order
may be fractional and must not replace edition identity. Filenames never control
shell syntax. Keep catalog/provenance exportable so readers do not require Nemalo.

| Policy | Eligible formats | Required assessment direction |
| --- | --- | --- |
| Default ebooks | EPUB | Bounded ZIP/package/resource checks, static findings, antivirus coverage |
| Optional PDF ebooks | PDF | PDF structure and active-content evidence, antivirus coverage; explicit enablement |
| Research papers | PDF initially | Same PDF checks, publication IDs/version/provenance; citation metadata |
| Audiobooks | MP3, M4B initially | Container/track metadata, chapter order and completeness evidence, antivirus coverage |
| Review | Unsupported, encrypted, incomplete, or insufficiently checked content | Preserve originals and explain the missing evidence |

PDF is important for papers and textbooks. It gets its own validation policy, rather
than an EPUB conversion requirement. Do not claim that checking a file header or
searching bytes for script names validates a PDF. Select a maintained validator or
parser through a dependency decision before enabling checked PDF publication.

Audiobooks publish as a recording with all expected tracks, not as unrelated songs.
Compare expected counts and declared durations where available; record uncertainty
for local folders without a manifest. A recording with missing or failed tracks
remains incomplete. Preserve multiple narrators, translations, and abridgments.
Repackaging MP3s into M4B, transcoding, loudness normalization, and speech-to-text
are optional later operations that create new derived assets.

Container metadata and successful probing do not prove full audio decodability.
Any decoder/probe must run with resource limits, no network access where containment
permits, and no interactive opening. Report probe coverage and failures. Exporting
to Audiobookshelf can follow later without making that server a dependency.

## Reproducible collections, updates, and access

A declarative collection describes intent, such as selected works or explicit
provider IDs plus languages/formats. A resolved collection record pins edition,
publication version or recording, asset IDs, hashes when known, provenance, and
rights evidence. Version both schemas. Do not treat a changing download URL as a pin.
This is an internal Go data contract, not a new package ecosystem or executable DSL.

Updates produce a preview showing changed versions, formats, rights, files, and
expected transfers. Acquire new assets alongside originals; do not overwrite
annotations, replace a translation, or change a narration implicitly. Repeating an
acquisition with the same resolved intent should be idempotent. A hash mismatch
means the source changed or transfer failed, not permission to silently accept new bytes.

Preservation includes periodic integrity checks, exportable manifests, and backup/
restore guidance for assets, provenance, and state. Reacquisition can repair a
missing asset only if the selected source still supplies matching content under
the recorded policy. Provider availability is not a backup guarantee. Recovery
does not erase changed or annotated copies without an explicit disposition.

Access covers real local content as well as catalog metadata. Start with exact
asset lookup, file export/open through an explicitly configured reader, and source
references. Invoke readers with argument arrays after an explicit open operation;
never render downloaded content during inventory. Rendering safety depends on the
reader, separately from Nemalo's checks.

An initial [source-bound EPUB text service](CONTENT-ACCESS.md) is implemented in
`internal/content`, using the canonical assessment container parser and shared
application boundary. It is offline and stateless; callers own acknowledgement.
Unsupported units are explicit and continuations never silently skip them.
CLI/JSON expose it; TUI content access, MCP, PDF/audio ranges, and indexes remain planned.

Expand bounded text extraction and search per supported format. Derived indexes key
on asset hash and extractor version; rebuild them without modifying originals.
Reference EPUB spine/fragment locations, PDF page locations, and audio track/time
locations where supported. Record missing text or OCR rather than pretending an
image-only PDF is searchable. Metadata search and full-text search are distinct
capabilities and must be labeled accordingly.

MCP/structured retrieval returns bounded content with asset identity, locators,
rights context, and provenance. Book text remains untrusted data, not instructions
to the agent. Unsupported extraction does not make an otherwise eligible asset
unavailable through ordinary file access. Semantic retrieval is optional and needs
evaluation beyond mechanically correct indexing.

### Deferred knowledge processing boundary

Topic insight watches, Markdown knowledge workspaces, and optional processing are
post-1.0, after existing roadmap work, as specified in the
[post-1.0 phase](../ROADMAP.md#post-10-topic-watches-and-optional-knowledge-processing).
Design adapters then, using shared acquisition, job, collection, and retrieval
services rather than introducing a second library or speculative processing framework.
First-party implementation remains Go; external inference/agent runtimes are optional.

Keep provider and harness roles separate. A provider adapter calls a local model or
remote service such as OpenRouter. A harness such as OpenCode coordinates reasoning
and tools, potentially using that provider. Direct model requests need no harness;
external harnesses get bounded application capabilities, not library ownership.
Use the shared HTTP/configuration/job mechanisms and validate each supported API.

Saved topic watches specify discovery scope and, optionally, acquisition/processing
policy, schedule, and resource/spending limits. Basic refresh stays model-free;
insight-enabled runs require a configured processing route and authorization.
Persist discovery and processing checkpoints separately so retries do not lose
unprocessed evidence or charge again for already completed derivations.

Pass only explicitly selected evidence to an approved processing route. Verify the
effective provider and enforce access outside model instructions. A local agent
server may use remote inference, so loopback alone is insufficient evidence of privacy.
Generated artifacts remain derived data, separate from source assets and human notes,
with input hashes, locators, processing identity, and dependency links for staleness.
Schema/citation validation cannot establish that an interpretation is true.

The optional knowledge workspace contains portable Markdown pages and an index,
with machine-readable provenance using the canonical metadata mechanism. Original
assets, extracted source text, generated interpretation, and human notes remain
distinct. Keep original-source citations resolvable through page revisions. Workspace
conventions are data, separate from development instructions and permission policy.
Publish staged page/index updates through the shared journal, checking expected
versions to preserve concurrent/user edits. Validate references and stale dependencies;
flag semantic disagreements for review rather than mechanically declaring truth.
Do not require an editor, Git installation, wiki service, or vector database.

## Assessment evidence

The initial implementation is `internal/assessment`, shared by `check FILE` and
the validation harness. Optional installed antivirus uses `internal/scanner`.
CLI/TUI dispatch through `internal/app` and share `internal/present`. See the
[file-health decision](decisions/0003-file-health-and-scanners.md) for implemented
limits and parser/scanner decisions. The production publication policy below is
still planned; file checks do not publish or delete anything.

Store independent results for structure, suspicious content, antivirus, metadata,
completeness, and duplicates. Each check records status, tool/version, policy/version,
asset hash, time, limits, and evidence. Distinguish pass, findings, unsupported,
unavailable, skipped, failed, cancelled, and incomplete.

Only assets satisfying the enabled policy can enter its checked collection.
Signature freshness is visible; expired evidence or changed files need reassessment.
No detected threats is an observation about completed scans, not a guarantee.

For EPUBs, check references and reading order without rendering or fetching remote
resources. Preserve originals rather than quietly rewriting active content. For
PDFs, account for encryption, scripts, attachments, launch actions, and external
references. Audio tags and cover images are also untrusted input.

Exact SHA-256 equality supports byte-level deduplication. Prefer references to an
existing verified asset over another stored copy while preserving all provenance.
Text similarity, filename matching, and catalog IDs are review signals. Different
paper versions and audiobook narrations must survive deduplication. Updating an
edition or annotated PDF creates a new asset; never overwrite user annotations.

## Untrusted folders and archives

Inventory only explicitly supplied roots. Do not follow filesystem links or
reparse points by default. Recognize archives by signature; password protection,
missing multipart volumes, malformed input, PAR2 files, and unknown types get
explicit dispositions. Detect traversal, absolute paths, device names, alternate
data streams, link entries, duplicate paths, and case/normalization collisions.

Enforce shared per-run budgets for nesting, entry count, compressed/expanded bytes,
per-file size, memory, time, process output, workers, and disk usage. Budgets span
nested archives, not just each individual container. Extraction failure preserves
the original. Validate actual writes, not just archive listings, and handle path
replacement races. Tools must be invoked as an executable and argument array.

Private staging and a subprocess are not sandboxes. Go's
[root-scoped filesystem APIs](https://go.dev/blog/osroot) are useful building blocks;
external extraction needs a separately tested containment strategy. Linux isolation
and Windows/macOS equivalents belong in adapter acceptance tests, with limitations
reported when equivalent isolation is unavailable. Never execute downloaded files.

## Durable operations

The implemented `internal/library` seam provides immutable byte-identity snapshots,
holdings queries, and explicit-root preservation audits. See
[decision 0004](decisions/0004-portable-library-snapshots.md). These snapshots
record all regular files and historical optional health evidence; they are not a
checked publication catalog, backup, authenticated manifest, or recovery journal.
Library identity/init/status and a synced initialization journal now share that
seam, with native reader/writer locks and resumable valid initialization. See
[decision 0006](decisions/0006-library-control-state.md). This does not import or
validate content. The mutable content operations below remain planned.

Use a versioned JSON catalog and per-asset provenance initially. Keep catalog
indexes rebuildable. Use a separate durable journal for in-progress operations;
catalog rebuilds cannot reconstruct whether an interrupted source deletion happened.
Keep one writer per library with tested OS-specific locking and stale-lock recovery.

1. Inventory and hash relevant source identities; write an immutable plan and digest.
2. Stage without source mutation; assess and classify all material.
3. Copy to temporary destination files; verify hashes; publish without overwriting.
4. Commit provenance and catalog state, with a durable journal recording intent
   before each mutation and completion afterward.
5. On explicit cleanup, recheck the plan, original identities, and verified outputs;
   move accounted-for originals to a recoverable holding area.
6. Permit permanent purge separately after its own preview and retention policy.

Recovery reconciles journal records with filesystem evidence and repeats idempotent
steps. Never assume cross-filesystem rename is atomic, an OS trash service is
available, or flushing a file alone makes its directory entry durable. Test native
replacement/flush behavior and document weaker filesystem guarantees.

An archive is removable only when every member has a verified disposition. That
can be publication, preservation in review/quarantine, or an explicitly approved
junk disposition. Unknown entries and missing volumes cannot disappear through
an archive-level success flag. Disk-full, cancellation, scanner failure, permissions,
and concurrent changes must preserve recoverable content.

## Providers and networking

The implemented metadata boundary supports explicit Open Library/Internet Archive
search and Archive item/file evaluation through the shared provider registry.
[Decision 0005](decisions/0005-provider-search-and-evaluation.md) records current
contracts, checked direct connections, limits, and unverified access/rights states.
The production transfer and wider provider behavior below remain planned.

Provider capabilities distinguish search, lookup, asset resolution, download,
web reading, streaming, and borrowing. Implement adapters in Go only as needed;
an external destination can be a catalog link without an ingestion adapter.
Generic OPDS can reduce duplication where publishers provide a permitted feed.

Use bounded HTTP bodies, decompression limits, pagination, caching, conditional
requests, cancellation, per-provider throttles, and capped retries with jitter.
Honor `Retry-After`; terminal authorization/rights errors are not retry loops.
Record partial results and provider errors separately. Validate content type and
response schema even after HTTP 200. Download resumes require matching validators
and lengths; otherwise restart safely. Checksums establish integrity, not rights.

Treat provider-returned URLs as untrusted. Revalidate every redirect and DNS/IP
destination; block loopback, private/link-local networks, unsafe schemes, credentials
in URLs, and unintended ports by default. Check the connected destination to resist
DNS rebinding. Never forward an API key to another origin. Explicit private-server
configuration, if introduced, must have a separate host/scope policy.

Do not fetch arbitrary links embedded in books or accept an MCP argument as an
unrestricted download URL. Resolve selected provider IDs through provider policy.
Collection manifests are declarative data, not executable plugin definitions.

## Dependency policy

First-party application code, tests, and executable tooling are 100% Go. A non-Go
exception requires a concrete unmet requirement and a recorded decision before
adoption. External archive/scanner/validator executables are separately declared
capabilities, not an excuse to add a second application or build runtime.

Minimal dependencies means small, justified, maintained dependencies. It does not
justify writing our own protocol stack, cryptography, or complex document parser.

| Need | Default choice | Addition trigger |
| --- | --- | --- |
| CLI/config/logging | `flag`, JSON, `log/slog` | Measured usability need for completion/help beyond a small CLI |
| HTTP/metadata/hash | Go standard library | A concrete unsupported contract |
| ZIP/TAR/compression | Go standard library | Additional formats through a maintained external tool |
| MCP | Official Go SDK | Needed when MCP milestone begins; review transitive graph |
| Agent Plugins | Manifest JSON and skill Markdown | No plugin runtime or package manager required |
| Catalog | Versioned JSON plus rebuildable indexes | SQLite only after measured query/scale/recovery need |
| OS locking/Unicode | Native APIs; assess small Go modules if needed | Correctness that standard APIs cannot provide portably |
| PDF/audio parsing | Select a maintained Go library or explicit validator process | Format milestone, with malformed-input fixtures and coverage requirements |
| Terminal UI | Bubble Tea v2 and Bubbles v2 | Unicode editing, bounded viewport, resize/input handling; shared Go services |
| Search/AI | Explicit catalog queries and filters | Embedded search, embeddings, or LLM only for demonstrated use; analysis post-1.0 |

Before accepting a dependency, record the need, alternatives, maintainer activity,
license, vulnerabilities, transitive additions, cgo/build impact, supported platforms,
update policy, and removal strategy. Pin versions and review changes to `go.sum`.
Do not add provider SDKs when a small standard-library HTTP adapter suffices.

External executables and signature databases count in the dependency inventory.
`doctor` reports required versus optional capabilities for the selected policy.
Do not auto-install software or download executable plugins as part of book intake.
Go release builds should avoid cgo; race instrumentation may need a C toolchain in CI.

## Verification and performance

Use offline synthetic fixtures for normal tests, Go fuzzing for parsers and paths,
fault injection for journal transitions, and native OS tests for filesystem behavior.
Live provider checks are small opt-in contract checks, separate from offline CI.
Keep proprietary downloads and personal metadata out of fixtures and logs.

Enforce at least 80% aggregate Go statement coverage and review destructive paths
individually. Include scan skips, malformed provider responses, redirect attacks,
multipart failures, Unicode paths, partial audiobook sets, paper version identity,
stale plans, and interrupted cross-filesystem copies. The race detector complements
tests; it does not establish freedom from all races.

Benchmark inventory and catalog operations on representative synthetic collections;
record peak memory, bytes processed, throughput, and cancellation latency. Stream
large files, use bounded workers, and avoid retaining archive contents in memory.
Choose database or parallelism changes from measurements. Large public catalogs
are optional local caches, separate from the user's acquired-assets catalog.
