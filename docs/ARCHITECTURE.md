# Nemalo architecture

Design updated 2026-10-08. The Go foundation is implemented as described in
[development and operation](DEVELOPMENT.md); the remaining production architecture
below is planned. The stack is Go throughout first-party application code, tests,
and executable tooling. Linux CLI use is primary; macOS and Windows are native targets with platform-specific
filesystem and scanner adapters. Desktop and terminal compatibility were reviewed
on Omarchy 4 (Arch Linux x86_64).

## Product boundary

Nemalo owns discovery, evaluation, acquisition, verification, existing-download
intake, organization, preservation, content access, automation, and expansion.
These form one product contract. The [product contract](PRODUCT.md) is the canonical vision;
shipping order does not determine which capabilities belong to the product.

The 1.0 product centers on a local library and discovery for books, ebooks, and
audiobooks. Papers/textbooks extend the library. Cited evidence for agent-led reading is 1.0
work; generated analysis inside Nemalo and knowledge workspaces are post-1.0. MCP
library tools do not require inference or a model harness.
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
CLI / TUI / JSON / MCP
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

Selected acquisition is implemented through shared services into exclusive
untrusted packets; see [decision 0007](decisions/0007-selected-acquisition.md).
Acquisition scans intake bytes through the same scanner adapter and quarantines
detections; library rescans batch ClamAV through the shared assessment pipeline
([decision 0012](decisions/0012-scan-on-acquire-quarantine-and-rescan.md)).
Filesystem capability binding and display encoding use shared adapters described
in [decision 0008](decisions/0008-filesystem-and-display-boundaries.md).
Neither intake receipts nor portable snapshots are managed publication journals.

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

Managed holdings implement a narrow part of this model. [Decision 0010](decisions/0010-bibliographic-identity.md)
records stated EPUB package identifiers and leaves the asset SHA-256 as the
storage, duplicate, and retrieval identity. Recording, track, and cross-file
relationships are not created from titles or filenames. Snapshots do not carry
these identifiers.

Managed recording manifests also return `recording-bytes/1` identities for
complete declared sequences of asset hashes. Identical ordered assets relate
across source items without altering source or narration declarations. This is
journal evidence rather than a fresh integrity check, and neither encoding nor
track-division equivalence is inferred
([decision 0052](decisions/0052-recording-byte-identities.md)).

Explicit user narration statements name exact held recording selections and
their ordered-byte identities. The optional relationship journal stores no
resolved holdings or audio and preserves provider provenance. Withdrawals retain
history; stale, incomplete, and conflicting selections remain visible rather
than acquiring an inferred reading. CLI/JSON, related holdings, recording views,
and read-only MCP use the shared library/application service
([decision 0055](decisions/0055-user-narration-declarations.md)).

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
Explicit `mp4-audio/2` content requests prepare a verified private copy from the
opened selected asset before reading original packet bytes and bounded original
sample-entry evidence. This ephemeral preparation changes no library state and
creates no playable range or decoder authority; the shared content service owns
its budgets, source binding and cleanup
([decision 0085](decisions/0085-verified-mp4-packet-access.md)).
Separately authorized `mp4-pcm/1` reuses that source preparation and the existing
installed decoder adapter. Every request opts in; existing MCP content tools
never decode. A complete sequential output hash binds measured sample ranges,
returned as bounded derived WAV bytes without creating a holding or playback
history. Declared clocks, original packets and decoder-output samples stay
distinct ([decision 0086](decisions/0086-explicit-decoded-mp4-access.md)).
Any decoder/probe must run with resource limits, no network access where containment
permits, and no interactive opening. Report probe coverage and failures. Exporting
to Audiobookshelf can follow later without making that server a dependency.

## Reproducible collections, updates, and access

A declarative collection describes intent, such as selected works or explicit
provider IDs plus languages/formats. A resolved collection record pins edition,
publication version or recording, asset IDs, hashes when known, provenance, and
rights evidence. Version both schemas. Do not treat a changing download URL as a pin.
This is an internal Go data contract, not a new package ecosystem or executable DSL.

The implemented collection revision preview compares two explicit user files
offline, bound to their byte hashes: exact selection additions/removals and
name, note, and retained-order changes. It creates no stored collection record
and does not infer upstream rights, changed assets, or transfer estimates
([decision 0053](decisions/0053-collection-revision-previews.md)).

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
CLI/JSON and the TUI Read operation expose it. Explicit version negotiation adds
structured EPUB parts, local note references, source XML, and hash-bound image
member bytes through the same services and container parser. Located text gaps
are distinct from retained source markup and unknown rendered/semantic coverage.
Assessment warnings remain review evidence; typed access restrictions prohibit
active/encrypted/repeated-spine content, while remote static references are disclosed
without fetching them. No retrieved bytes are rendered or executed. See
[the structured contract](STRUCTURED-EPUB.md). The read-only MCP `content_*`
tools, PDF pages, and MP3 frame ranges use the same service; full-text indexes
remain planned. `internal/reader` lays out delivered ranges for a person as
terminal pages or one word at a time, under the TUI and `nemalo read`; it reads
no files and keeps no state ([decision 0025](decisions/0025-terminal-reading.md)).
Explicit bookmarks are a separate library journal, written only by bookmark
commands and the TUI save key
([decision 0027](decisions/0027-explicit-local-bookmarks.md)).
Declared EPUB contents are `epub-nav/1`
([decision 0026](decisions/0026-epub-navigation-and-chapter-selection.md)), and declared
page labels are `epub-page-list/1`
([decision 0031](decisions/0031-epub-page-list.md)).

Opt-in `nemalo-evidence/1` augments managed reads in `internal/app` using the same
content retrieval. A non-mutating library reader lock holds selected journal
metadata stable throughout the range retrieval. The portable declaration view is
bounded to 256 KiB; original stored bytes are independently revalidated. Exact
quotations/audio payload hashes, typed locators and single-range coverage remain
separate from caller interpretations. Versioned references compare complete
metadata context, retrieval and explicit selection without supplying paths or
permissions. Every failure discards the payload. Shared `library citation` and
`library_citation` export CSL-JSON separately from provenance and unknown fields,
without opening stored bodies or historical receipt paths. See
[decision 0087](decisions/0087-source-bound-evidence-and-citations.md) and the
[public contract](CONTENT-ACCESS.md#evidence-and-citation-export). Semantic-output
and hosted stage acceptance remain outstanding.

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
the validation harness. Installed antivirus uses `internal/scanner`, which owns
tool resolution, single and batched scans, detection recognition, setup hints,
and signature age; `CheckManyInRoot` binds each batched result to its snapshot.
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

Optional PAR2 metadata inventory lives in `internal/inventory` and uses the same
opened regular file and read counters as requested hashes. The default still reads
no file bodies. Header declarations, checked small-packet bodies, unchecked parity,
and protected-data verification remain distinct. Declared filenames never become
I/O paths or write authority, and conflicting metadata remains visible. No verifier,
repairer or cleanup permission follows from set inventory
([decision 0059](decisions/0059-par2-metadata-inventory.md)).

Ordinary ZIP cleanup uses an independent byte-coverage proof beside the maintained
decoder: central records, local headers and descriptors must agree and cover the
file exactly. Proof reads share 16 MiB. Unaccounted bytes, unknown metadata and
unsupported layouts stay preserved. Purge repeats this proof on the opened,
rehashed ZIP, including when resuming an older purge manifest; it never treats
historical cleanup permission as current accounting
([decision 0060](decisions/0060-complete-zip-cleanup-accounting.md)).

Enforce shared per-run budgets for nesting, entry count, compressed/expanded bytes,
per-file size, memory, time, process output, workers, and disk usage. Budgets span
nested archives, not just each individual container. Extraction failure preserves
the original. Validate actual writes, not just archive listings, and handle path
replacement races. Tools must be invoked as an executable and argument array.

Implemented RAR intake checks each private volume's format, role, ordinal,
and size through the installed 7-Zip reader, matches split-member evidence
across adjacent volumes, and requires an error-free assembled listing. Names
and counts alone cannot establish a set. Old-style letter volumes share this
adapter; inconclusive boundaries preserve all sources. Reader listing evidence
alone never authorizes volume cleanup because unlisted blocks are unaccounted for
([decision 0056](decisions/0056-rar-volume-identity.md)).

Requested standalone RAR5 coverage now measures bounded physical framing from
the opened private snapshot, then matches every raw file name, size, packed size,
directory state and CRC to one UTF-8 reader table before extraction. This is
read-only evidence, separate from application permission. Default intake is unchanged.
Child RAR coverage requires the separate opt-in below. Historical RAR-named archive entries, including old
continuations, cannot resume cleanup or purge without a supported versioned proof.
Their recorded bytes remain restorable
([decision 0064](decisions/0064-bounded-rar-framing-accounting.md)).

Split ZIP byte chunks and true disk segments share this staging service and
Import. Byte chunks are joined for the Go ZIP reader after a maintained-reader
extent check. True segments must expose consistent member tables and exact
sizes/ordinals through every private part. Both use the installed 7-Zip for
container evidence. Complete byte chunks can move as one recoverable cleanup
entry after the shared `staging.ZIPAccounted` proof and held-member checks.
True segments additionally validate raw disk references/counts, record boundaries
and the initial marker. A read-only view overlays only validated disk/offset fields,
then reuses the same maintained ZIP parser and full framing/metadata proof.
It preserves every other byte, supports a directory spanning segments and shares
one 16 MiB physical-read allowance across preflight and view checks. At most one
private part handle is open at a time; no converted archive is written. The checked
reader table must match central names, sizes, CRCs, types and disk-relative offsets,
and that exact table drives extraction
([decision 0062](decisions/0062-segmented-zip-cleanup.md)). The standard ZIP opener shares
assessment's directory and lifetime compressed-reader guards, with operation
limits selected by the caller. Source-set evidence, member-level failures, and
per-member journal recovery remain separate
([decision 0057](decisions/0057-split-zip-intake.md)).

Split ZIP cleanup entries bind `zip-stream/1` or `zip-segments/1` evidence to exact part identities
and holdings. A fresh purge repeats the complete check. Its schema-2 manifest
binds versioned archive groups into the reviewed digest and durable intent.
Partial recovery checks each remaining original part and current holdings against
that supported proof, without pretending missing chunks form a whole archive.
Unsupported evidence retains bytes; malformed bindings require review. Earlier
executables refuse the new cleanup field and purge manifest version
([decision 0061](decisions/0061-split-zip-stream-cleanup.md)).
Only a valid supported whole-set group authorizes a terminal `.zip` as a segment;
ordinary ZIP copies still need their own current coverage proof. Unsupported groups
retain every remaining part, including the terminal segment. Earlier builds refuse
the new segment proof instead of interpreting it as byte-stream evidence.

Nested standalone archives traverse through `staging.ExtractTree`, with one
shared depth/member/output/read/diagnostic/process/deadline budget. Child copies
are checked before traversal, and exact member-chain labels distinguish nested
selections from historical direct names. Ingest still calls Import for every
leaf; unavailable or changed pending selections stop new imports
([decision 0058](decisions/0058-bounded-nested-intake.md)). Cleanup requests complete
coverage through this same tree. ZIP proof reads use the checked opened bytes and
share one 16 MiB allowance across all nodes, also charged to the existing reader
budget. 7z whole-container probes use the shared process/diagnostic runner. Default
intake does not request this evidence. Untraversed containers cannot make a tree
covered, and unknown directory sizes remain unaccounted in cleanup. Refusals locate
the exact first container chain.

Every root copy is checked against the application's planned source size and
SHA-256 while it is written. Covered ordinary ZIPs use the same private snapshot
mechanism without requiring an installed reader for the outer container. Actual
copy-hash reads, including failures, share the 64 GiB source-hash allowance with
the initial and final source hashes. This binds extraction to the selected bytes
even if an original changes and changes back during planning.

Library services additionally require every leaf held with currently verified
stored bytes and unchanged final source hashes. A nested cleanup binds exact
chains, sizes, hashes and holding IDs in `archive_leaves` under `archive-tree/1`.
CLI and TUI reviews show these exact selections with parse-equivalent escaping.
Fresh purge repeats the complete tree check; its schema-2 archive group binds
the leaf digest as well as proof and file indices. Partial recovery checks that
immutable original-tree evidence, remaining part identities and current holdings.
It never reconstructs a tree after a part was removed. Unsupported proofs retain
bytes; malformed bindings stop before removal. Unaccounted RAR children and all unproven
trees stay. Earlier executables refuse unfamiliar canonical leaf fields; restore
with this source version or newer ([decision 0063](decisions/0063-complete-archive-tree-cleanup.md)).

Every new 7z move uses that same versioned tree proof and exact leaf records,
even for a flat standalone archive or split set. Fresh purge rechecks complete
coverage and binds its group and leaf digest before removal. Historical 7z-named
archive entries/chunks without supported evidence stop cleanup resume and retain
all copies through fresh or pending purge. Restore returns their recorded bytes;
stop, restore and preview a new cleanup to establish current coverage. Immutable
old manifests are never migrated
([decision 0065](decisions/0065-versioned-7z-coverage.md)).

Standalone RAR5 cleanup uses its own `rar5-frames/1`, one exact source and
one-component leaf selections. It reuses the opened-snapshot framing/reader
binding, verified current holdings, final source hashes, copy/link/remove journal
and raw-byte restore. Fresh purge repeats complete checks and binds the proof,
single exact index and leaf digest to its schema-2 intent. Recovery validates
every binding before removal and checks current remaining bytes/holdings without
reconstructing or adopting replacements. RAR4, volume sets, nested RARs and RAR
roots containing nested archives remain outside this subset. `archive-tree/1`
does not grant RAR authority
([decision 0066](decisions/0066-standalone-rar5-cleanup.md)).

The separate `staging.RAR4Accounted` measures a strict standalone RAR4 header/data
partition through the shared lifetime proof reader. It returns declared facts
without reading payloads and refuses unlisted subblocks and unsupported metadata.
Its facts alone grant no extraction coverage or application permission. Default
RAR4 intake remains unchanged
([decision 0067](decisions/0067-read-only-rar4-framing.md)).
Explicit internal `Coverage` plus `RAR4Coverage` permits standalone root reader
binding through the same opened snapshot, shared budgets and UTF-8 exact-table
matcher as RAR5. The common matcher checks every raw name, size, packed size,
directory and CRC before extraction. This capability alone cannot authorize the
existing RAR5 cleanup proof.
This standalone RAR4 opt-in alone grants no child or set coverage
([decision 0068](decisions/0068-opt-in-rar4-reader-binding.md)).
RAR4 framing also accounts for zero high-size words and exact modification,
creation and access time fields. Presence, width, selected subsecond range and
header exhaustion are checked; no time interpretation is generated. Unknown
time fields, nonzero high words and unsupported encodings stay refused. This
metadata extension alone grants no application permission
([decision 0069](decisions/0069-bounded-rar4-time-and-size-metadata.md)).

Standalone root RAR4 cleanup uses the separately verified `rar4-frames/1`.
The adapter measures the checked outer snapshot signature and propagates its
version only from the root, as internal evidence omitted from JSON. Application
standalone authority selects its own proof for version 4 and `rar5-frames/1` for
version 5. Unknown versions preserve the source.
Exact leaves, current holdings, final originals, reviewed manifests, fresh purge
and immutable recovery reuse the same protocol. Known proof mismatches stop
before removal; unfamiliar proofs retain bytes. Unicode names and unsupported
metadata stay preserved; volume and nested authority are specified separately
([decision 0070](decisions/0070-standalone-rar4-cleanup.md)).

Explicit internal `NestedRARCoverage` alongside `Coverage` extends the same
read-only standalone proof to checked private RAR children; RAR4 still requires
`RAR4Coverage`. Existing framing subsets and the strict reader preflight proving
one standalone child are unchanged. All descendants share proof/read, output,
member, diagnostic/process and deadline allowances, including failed attempts.
The outer-only measured RAR version never comes from a child. Whole framing,
output integrity, member eligibility and application permission remain separate.
This read-only prerequisite initially left the application nested opt-in false
([decision 0071](decisions/0071-opt-in-nested-rar-coverage.md)).

Covered trees containing standalone RAR nodes now use the distinct
`archive-tree/2`. A measured count of individually covered private RAR nodes,
including empty children, selects that authority only after the whole tree,
every leaf/current holding and final original bytes pass. The count is internal
and omitted from JSON; incomplete output or a partial tree still grants no move.
Direct RAR roots keep their standalone proof and ZIP/7z-only trees keep /1.
The original /1 continues to reject RAR roots and intermediate RAR selections.
Canonical leaf syntax permits safe RAR chains independently of proof semantics,
so unfamiliar authority retains its bytes. All known proof shapes and mismatched
bindings are validated before any independent purge group can remove bytes.
Fresh purge repeats complete tree checks; immutable partial recovery binds exact
indices/leaves, remaining original identities and current holdings without
reconstruction. Restore returns raw originals without replacing a target. RAR
outer sets and nested volume sets receive no authority from either tree proof
([decision 0072](decisions/0072-nested-rar-cleanup.md)).

Explicit internal `Coverage` plus `RAR5SetCoverage` adds read-only coverage for
modern root RAR5 sets. Each exact opened private volume is framed independently
and bound to its own UTF-8 isolated table before siblings become visible. Split
roles, ordinals, metadata and known whole sizes must agree; packed-part CRCs stay
distinct from the final whole-file CRC. Complete logical records bind the same
strict assembled table used for exact-name extraction. Direct calls share one
proof/read/process/output budget, including source copies and failed work.
`RARSetVersion` is separate from standalone version/count evidence and omitted
from JSON. Whole framing does not establish successful extraction or permission.
That read-only increment left this opt-in false and retained every RAR set. RAR4,
old naming and nested sets receive no new coverage. A later writer requires a
distinct proof and separate full recovery/verification gate
([decision 0073](decisions/0073-read-only-rar5-set-coverage.md)).

Completely covered modern root RAR5 sets now use distinct `rar5-set/1` application
authority. The cleanup service requests the verified read-only capability and
requires measured set version 5, complete framing/output/traversal, every current
verified held leaf, bound snapshots and unchanged final originals. Set authority
takes precedence over nested-RAR counts. Existing tree and standalone proof IDs
retain their scopes; neither tree ID accepts a RAR outer set. Ordered modern
parts share one exact parent/family and one recoverable entry. Fresh purge repeats
the whole held-set check and binds all indices and the canonical leaf digest.
Immutable partial recovery checks remaining identities/holdings without joining
missing parts or adopting replacements. Unknown authority retains bytes; known
malformed bindings stop before independent removals. Restore returns raw originals
without a reader or replacement. Old-named and nested volume sets remain
preserved. This requires its own acceptance and preceding-binary checks
([decision 0074](decisions/0074-modern-rar5-set-cleanup.md)).

Separate internal `Coverage`, `RAR4Coverage` and `RAR4SetCoverage` together permit
read-only modern root RAR4 set coverage. Standalone RAR4 framing still refuses
volume declarations. The volume accountant measures an explicit end ordinal,
first/continuation roles, supported metadata and optional seven reserved zero
bytes. Volume-wide CRC declarations remain unsupported. Each format feeds its
own declaration fingerprint and neutral facts into the same continuation and
exact-table matcher, preserving RAR5 conditions without converting RAR4 into
RAR5 authority. The same private per-volume and assembled tables, source-copy
and shared work limits, output checks and direct-call traversal boundary apply.
Root-set version 4 is internal evidence distinct from standalone facts. The
read-only increment left the new application flag false and required version 5
for its RAR5 set writer. It added no RAR4 cleanup right, public option or decoder; old
names and nested sets remain outside the contract
([decision 0075](decisions/0075-read-only-rar4-set-coverage.md)).

Application cleanup now requests that RAR4 capability under distinct `rar4-set/1`,
selected only by measured root version 4; version 5 retains `rar5-set/1`. Both reuse
the exact-parent modern set shape, verified held leaves, whole-set fresh purge
and immutable partial recovery. Shared persisted shape checks do not infer
physical format: fresh purge must reproduce the exact recorded proof, and partial
recovery binds the original proof, indices and leaf digest before any removal.
No existing standalone/tree proof expands, and old-named/nested volume sets stay
preserved. Separate full acceptance and actual preceding-binary retention/raw
restore remain required ([decision 0076](decisions/0076-modern-rar4-set-cleanup.md)).

Internal `NestedVolumeSets` adds opt-in tree intake and read-only coverage for
the existing modern/old RAR4/RAR5, split 7z and split ZIP adapters. Canonical
volume-family grouping is shared in `internal/staging`, with the library's
existing projection retained. Every original part must pass ordinary extraction
and integrity checks before fixed private names expose one complete group to
the set reader. Leaves retain the exact original first-member chain. All groups
share the existing tree allowances, including child snapshot and join reads;
failed work does not refund them. JSON-omitted `CoveredVolumeSets` measures
requested child-set physical evidence separately from root and standalone facts.
That read-only increment left application callers false and explicitly retained
measured child sets rather than expanding any existing proof. Its separate
hosted acceptance preceded writer authority
([decision 0080](decisions/0080-opt-in-nested-volume-set-coverage.md)).

After accepted read-only and counter-housekeeping gates at `1f63374`, application
intake explicitly requests checked child groups while retaining sources. Cleanup
requests full coverage and selects `archive-tree/3` only from measured child-set
evidence, complete output, every current verified held leaf and unchanged expected
original bytes. Root RAR format/layout facts remain independent prerequisites.
Only /3's leaf validator accepts safe exact first-part selections; previous IDs
and validators retain their scopes. A covered child with no file leaf still
requires /3 beside a direct held book. Old-name chains can share standalone syntax,
so fresh purge reproduces actual authority rather than guessing from suffixes.
The existing group intent binds /3, original indices and canonical leaf digest;
partial recovery checks immutable evidence and remaining bytes/holdings without
reconstruction. Restore returns raw originals without a reader. No schema field,
decoder, module or runtime was added. Writer acceptance is tracked in issue 57
([decision 0082](decisions/0082-nested-volume-tree-cleanup.md)).

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
[decision 0006](decisions/0006-library-control-state.md). Single-asset import
stores one assessed file beside `.nemalo` and records it in separate journals.
See [decision 0009](decisions/0009-managed-epub-import.md). Folder and ZIP
ingest ([decision 0017](decisions/0017-folder-and-zip-ingest.md)) and recoverable
source cleanup into a holding area with its own journal
([decision 0022](decisions/0022-recoverable-source-cleanup.md)) and
validator-bound download resume
([decision 0023](decisions/0023-validator-bound-download-resume.md)) are implemented.

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

Every write of known size first checks the free space of its folder, or its
nearest existing parent, through `safeio.EnsureSpace`: an acquisition (and
each continuation), a recording's remaining tracks, a catalog download, an
import before its journal intent, a ZIP member before extraction, a cleanup
copy, and an export or open copy. A write that would leave less than 64 MiB
free is refused with the needed and free sizes, before any folder, journal
entry, or request. A root opened through the descriptor bridge is measured
through its own descriptor (`fstatfs`), since on macOS its `/dev/fd` name is
not a path to the folder. Free space that cannot be read, or a pseudo
filesystem that reports no blocks, does not refuse a write, and the write
still fails cleanly if the disk fills. The check is an early refusal, not a
reservation; another process can still fill the disk.

## Providers and networking

The implemented metadata boundary supports explicit Open Library/Internet Archive
search, Open Library edition pages, and Archive item/file evaluation through the shared provider registry.
An edition's `ocaid` is the only Open Library path to an Archive item
([decision 0030](decisions/0030-openlibrary-selected-edition.md)).
[Decision 0005](decisions/0005-provider-search-and-evaluation.md) records current
contracts, checked direct connections, limits, and unverified access/rights states.
Project Gutenberg search and catalog evaluation read a local `pg_catalog.csv`.
`catalog fetch gutenberg` downloads that one CSV.
`evaluate gutenberg:NUMBER --files` and `acquire gutenberg:NUMBER --file NAME`
fetch one bibliographic record from gutenberg.pglaf.org and can download one
pinned EPUB from that host.
[Decision 0028](decisions/0028-gutenberg-local-catalog.md) records the catalog.
[Decision 0029](decisions/0029-gutenberg-selected-epub.md) records the EPUB.
Website search and the wider provider behavior below remain planned.

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
`doctor` reports tool availability, the directory descriptor bridge, the
recommended scanner for this operating system, and signature age read from the
ClamAV database header or Defender registry values. It does not execute scanners.
The release install scripts are the one non-Go first-party tooling exception
([decision 0013](decisions/0013-release-installer-scripts.md)): they only download,
verify, and place a published executable.
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
