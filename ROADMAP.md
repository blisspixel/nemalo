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
- Return cited evidence from holdings, bound to exact assets, versions, extractors,
  and locators with stated coverage, so a person's own agent can explain a work
  or answer a question from it. Nemalo runs no model for this.

Milestones deliver these workflows incrementally; the first end-to-end slice is
earlier than 1.0. Papers/textbooks and their provider plan remain in scope as library
expansion, but catalog breadth must not displace reliable book/audiobook workflows.
Additional capabilities below are not a blanket checklist for releasing 1.0.

Model processing inside Nemalo, generated Markdown knowledge workspaces, video,
podcast, feed, and web capture, topic watches, and inference/harness integrations
are post-1.0. Cited evidence and agent-led reading over the existing MCP tools are
1.0 work (milestone 9), because 1.0 freezes the reference contracts that later
analysis depends on ([decision 0048](docs/decisions/0048-cited-evidence-before-1-0.md)).
Do not build model, workspace, or capture infrastructure while the library release
remains unfinished.

## First end-to-end slice

Status: complete. Search, evaluate, acquire, assess and scan, import as checked
with provenance, open or export a verified copy, and `library check` run end to end
for Archive, Gutenberg, and arXiv sources through CLI, JSON, TUI, and MCP.

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

## Order of work to 1.0

Each stage depends on the ones before it; work inside a stage can proceed in any
order. A stage is finished when its milestone acceptance items are met and
verified, not when code exists. No stage has a time estimate.

1. **Identity and collections** (milestone 1, collections workstream). Recording
   identity beyond LibriVox, alternate hosts of one recording deduplicated by
   recording evidence and exact hashes, and relationships between distinct
   editions and versions of one work recorded from explicit identifier evidence,
   never merged. Declarative collections: intent plus resolved edition, version,
   recording, and asset records, idempotent repeat acquisition, and a diff before
   any update. First, because evidence, collections, and stored records cite
   these identities, and changing them after 1.0 breaks libraries. Progress:
   source-declared edition and work identifiers and `library related`
   ([decision 0049](docs/decisions/0049-declared-identifiers-and-related-holdings.md)),
   and collections resolved by holdings with idempotent acquisition
   ([decision 0050](docs/decisions/0050-collections.md)), with offline revision
   previews of exact selection, note, name, and order changes
   ([decision 0053](docs/decisions/0053-collection-revision-previews.md)). Complete recordings
   with identical ordered asset hashes now share a byte identity across source
   items, including without LibriVox evidence
   ([decision 0052](docs/decisions/0052-recording-byte-identities.md)). Explicit
   user declarations now relate different encodings and track divisions beyond
   LibriVox, bound to held selections and ordered asset identities, with visible
   unresolved evidence and withdrawal
   ([decision 0055](docs/decisions/0055-user-narration-declarations.md)).
   Status: implemented and verified for this stage's declared-evidence and
   collection contracts. The acceptance audit below records the evidence and
   remaining release/platform limits; registry confirmation and broader provider
   coverage do not replace unknown evidence with inferred identities.
2. **Intake completeness** (milestones 2 and 4). Status: implemented and
   verified for the bounded contracts, with
   [issue 57](https://github.com/blisspixel/nemalo/issues/57) closed after its
   [acceptance receipt](https://github.com/blisspixel/nemalo/issues/57#issuecomment-6368967562)
   was read back while open. The writer passed all six hosted jobs and
   Go/Actions CodeQL at `1a9e67b`.
   Supported modern/old-name RAR4/RAR5 volume sets, ZIP byte chunks and disk
   segments, split 7z and bounded nested trees share canonical intake and
   complete member accounting. PAR2 metadata remains declaration/checksum
   evidence without recovery or cleanup authority; recovery material stays.
   Native Windows reparse-point boundaries have a separate required test gate.
   Import preserves sources. Cleanup needs complete physical/output coverage,
   verified held leaves, exact source hashes, versioned proof bindings, fresh
   whole-source purge and immutable partial recovery. Unknown, incomplete,
   ambiguous and unsupported material stays preserved. Earlier proof IDs keep
   their scopes; later nested-volume authority is `archive-tree/3`
   ([decision 0082](docs/decisions/0082-nested-volume-tree-cleanup.md)).
   The [acceptance audit](docs/VALIDATION.md#stage-2-archive-intake-and-recoverable-cleanup),
   [archive operations](docs/DEVELOPMENT.md) and linked decisions record the
   supported layouts, earlier read-only prerequisites, tests and limitations.
   This source acceptance is separate from a release and broader host/reader
   conformance.
3. **Audio access** (milestone 8). M4B and M4A byte ranges and decoded timing in
   content access, bounded ranges usable by an external player, and output
   demonstrated with a real audiobook reader or server. Status: implemented and
   verified for the bounded contracts at `5091ce7`, with all six hosted jobs and
   Go/Actions CodeQL passing. [Issue 60](https://github.com/blisspixel/nemalo/issues/60)
   closed after its [acceptance receipt](https://github.com/blisspixel/nemalo/issues/60#issuecomment-6374231710)
   was read back exactly while open. The
   [AAC range research](docs/RESEARCH.md#aac-range-boundaries-and-decoder-state)
   records the decoder-state constraint addressed by full sequential decoding.
   Internal exact packet evidence now reuses the checked MP4 tables under
   [decision 0083](docs/decisions/0083-source-bound-mp4-packet-evidence.md).
   Explicit public `mp4-audio/2` adds bounded original packets and sample-entry
   evidence through shared CLI/JSON/MCP services, backed by a verified private
   copy ([decision 0085](docs/decisions/0085-verified-mp4-packet-access.md)).
   The original-packet increment and its test-only CI repair passed all six
   hosted jobs and Go/Actions CodeQL at `9e248a8`; its
   [acceptance receipt](https://github.com/blisspixel/nemalo/issues/60#issuecomment-6372689487)
   was read back while issue 60 remained open. The separately approved
   [explicit decoded-access design](docs/decisions/0086-explicit-decoded-mp4-access.md)
   is implemented as `mp4-pcm/1`, with synthetic CLI/MCP tests and native
   full-reference/player evidence including AAC noise substitution and a cached
   LibriVox audiobook. Native installed-tool/player evidence is Linux-specific;
   all three OSes passed synthetic verification and race checks. This is source
   acceptance, not a release or universal codec/player conformance.
   `mp4-audio/1` stays units-only;
   decoder samples do not infer original-packet/chapter/presentation mapping.
4. **Cited evidence and agent reading** (milestone 9). A versioned evidence
   result over the content services, documented citation export, and agent
   guidance for reading a work from its own structure. Depends on stages 1 and
   3, since evidence names identities and audio ranges. Selected declared metadata,
   citation export, opt-in evidence/replay and the
   [selected reading review](docs/CITED-EVIDENCE-REVIEW.md) are implemented.
   [Issue 39](https://github.com/blisspixel/nemalo/issues/39) records the exact
   acceptance head and hosted receipt; its full gate precedes stage 5.
5. **Interfaces and platforms** (milestone 6 and the TUI workstream). TUI views
   for jobs, collections, and evidence; the MCP host and platform matrix; native
   macOS and Windows runs of destructive and recovery paths, and of the installers.
   The shared passive `jobs snapshot` prerequisite for
   [issue 59](https://github.com/blisspixel/nemalo/issues/59) is implemented in
   source under [decision 0088](docs/decisions/0088-non-mutating-library-job-snapshot.md).
   The issue records verification; its optional panel, explicit reader handoff
   and native lifecycle acceptance remain open. This does not complete stage 5.
   State's explicit F10 Jobs view consumes the same passive service. Collections
   now has offline status, revision comparison and a bound acquisition review in
   source ([decision 0089](docs/decisions/0089-collection-acquisition-review.md));
   its [exact local/native/hosted acceptance](https://github.com/blisspixel/nemalo/commit/8d7d3561aaaf1036cbee820b0fc0a43198b5d73c#commitcomment-206433228)
   passed at `8d7d356`. Read's explicit citation/evidence reports and bounded
   replay are in source under [decision 0090](docs/decisions/0090-terminal-cited-evidence.md);
   their [exact acceptance](https://github.com/blisspixel/nemalo/commit/3220d7f0bcc8cd9da4e014c2ee6305daeb00ec83#commitcomment-206437123)
   passed at `3220d7f`. A portable native executable lifecycle harness is in source
   under [decision 0091](docs/decisions/0091-native-executable-lifecycle-acceptance.md),
   with [exact native acceptance](https://github.com/blisspixel/nemalo/commit/d6ab8d86cc53f5e86a72bd3db3dfc8c0ae8757b0#commitcomment-206444963)
   at `d6ab8d8`. Its optional PTY/ConPTY terminal extension is implemented under
   [decision 0092](docs/decisions/0092-native-terminal-acceptance.md), with
   [exact native acceptance](docs/VALIDATION.md#native-terminal-acceptance) on all
   three hosted OSes at `6f17380`. Broader real host/plugin and accessibility
   acceptance remains; see [next steps](#current-position-and-next-steps).
6. **1.0 release candidate.** Freeze and document the on-disk formats (journals,
   holdings, bookmarks, packets), JSON envelopes, extractor IDs, and the evidence
   contract; upgrade alpha libraries in place or refuse them with a clear path;
   pass every [release gate](#release-gates); review the documentation against
   behavior.

Not 1.0 gates, and able to proceed alongside once their dependencies exist:
remaining paper providers and DOI resolution in milestone 7, the textbook cohort,
OCR and column reconstruction, and the
[additional planned capabilities](#additional-planned-capabilities). The
[post-1.0 phase](#post-10-topic-watches-and-optional-knowledge-processing) follows
all of these.

## Current position and next steps

As of 2026-10-24, stages 1 to 4 are accepted and stage 5 is in progress. `main`
is the only branch and passes hosted CI on Linux, macOS and Windows. No wave is in
flight. Open issues are [59](https://github.com/blisspixel/nemalo/issues/59)
(optional desktop panel) and the post-1.0 proposals
[37](https://github.com/blisspixel/nemalo/issues/37) and
[38](https://github.com/blisspixel/nemalo/issues/38).

Remaining stage 5 work, in order:

1. Real host and plugin acceptance. Claude Code on Linux is
   [accepted](docs/VALIDATION.md#claude-code-host-acceptance): marketplace
   install, scoped read-only calls, out-of-scope refusals, absent gated tools and
   version parity. Remaining: the Agent Plugins package in a host that consumes
   it ([named launch clients](https://vercel.com/changelog/introducing-agent-plugins-1-0-0)
   include Codex, Cursor, GitHub Copilot, Kiro and VS Code; Codex is installed
   locally), and Claude Code on macOS and Windows.
2. Installed-tool evidence beyond Linux is
   [accepted](docs/VALIDATION.md#installed-tool-acceptance) on all three hosted
   OSes at `392f21f` through `nativecheck --installed-tools`
   ([decision 0093](docs/decisions/0093-native-installed-tool-acceptance.md)).
   Re-dispatch `installed-tools.yml` after changes to scanning, archive intake
   or decoding.
3. Accessibility review of the TUI and reader: keyboard-only operation, no-color
   and light/dark output, and at least one screen reader per OS, with limitations
   recorded rather than inferred from the fixed terminal profile.
4. Issue 59 panel. Its shared snapshot is accepted. The panel needs a recorded
   decision on a thin QML adapter before any implementation; it is optional and
   does not gate 1.0.

Then stage 6: freeze and document the on-disk formats, envelopes, extractor IDs
and evidence contract, decide alpha library upgrade or refusal, and run every
release gate. Start each session with the workspace check in `AGENTS.md`.

## Implementation status

Implemented in Go: CLI/TUI entry points over shared services, configuration,
Open Library/Internet Archive metadata search, Archive item/file evaluation,
bounded read-only inventory with optional hashing,
capability reporting, JSON envelopes, and pinned verification/native CI configuration.
Live source searches and Windows TUI search/inspection/holdings/evaluation/quit
smokes were exercised. Desktop and terminal compatibility were reviewed on
Omarchy 4 (Arch Linux x86_64 with Hyprland and foot; Ghostty, Alacritty, and
Kitty preserve truecolor, and desktop shortcuts avoid Nemalo's keys).
TUI navigation, session drafts, adaptive no-color/light/dark
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
`doctor` reports configuration paths, tool availability, the directory descriptor
bridge (`/proc/self/fd` on Linux, `/dev/fd` on macOS), and scanner recommendations
without executing scanners.
Single-asset import into that library is implemented: one EPUB, PDF, MP3, M4B,
M4A, or completed intake packet, with a separate operations/holdings journal,
crash resume, and managed audit. Checked status is limited to an EPUB, a PDF
whose page tree and active content were fully examined and clean, an MP3 whose
frames indexed cleanly, or an M4B or M4A whose samples were all located, and
whose scan reported no detections. See
[decision 0009](docs/decisions/0009-managed-epub-import.md),
[decision 0032](docs/decisions/0032-pdf-checked-policy.md),
[decision 0033](docs/decisions/0033-mp3-checked-policy.md), and
[decision 0039](docs/decisions/0039-mp4-audio-samples.md).
Import preview and recovery also bind normalized provenance and the full
proposed holding, including retained declarations and disposition
([decision 0054](docs/decisions/0054-provenance-bound-import-plans.md)).

Milestone 1 remains in progress. Managed holdings record EPUB package
identifier evidence for an explicit ISBN, DOI, or Open Library work or edition
key, and they do not merge files on that evidence. See
[decision 0010](docs/decisions/0010-bibliographic-identity.md). Recordings of
one LibriVox reading are related by narration
([decision 0044](docs/decisions/0044-narration-identity.md)). Explicit user
declarations relate other held recordings with their own evidence origin,
independently of provider metadata
([decision 0055](docs/decisions/0055-user-narration-declarations.md)). A scan is recommended for a file the user
did not produce. The default is Microsoft Defender on Windows and installed
ClamAV on Linux and macOS. `doctor` names that default. A supplied publisher
SHA-256 confirms the published bytes. Nemalo does not install or update a
scanner, and a no-detection result or a checksum match is not proof of safety.
Selected downloads are scanned on acquisition; a detection is quarantined (or
deleted by configuration), never imported, and other files from the item are
offered. A clean acquisition scan bound to the same bytes lets a packet import
as checked, and `library rescan` scans review holdings in ClamAV batches. See
[decision 0012](docs/decisions/0012-scan-on-acquire-quarantine-and-rescan.md).
Recoverable source cleanup (decision 0022) and validator-bound download resume
(decision 0023) are implemented. Native
archive/checksum packaging and checksum-verifying install scripts are
implemented; releases still require the actual verification and publication gates.
Milestone 2
has inventory, standalone signatures/private snapshots, and folder ingest with
ZIP extraction into private staging. Milestone 5 has
two discovery adapters, source-declared Archive file/access/rights evaluation,
and selected EPUB/PDF/MP3 acquisition into exclusive untrusted intake packets.
Whole recordings are acquired as ordered track packets, interrupted transfers
resume against a strong validator, and held recordings have ordered manifests.
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

Status: implemented and verified for the Go foundation and initial identity
contracts. Depends on milestone 0. This is not a 1.0 release signoff.

Delivered increment: versioned immutable byte-asset snapshots, duplicate locations,
holdings queries, and preservation audits. See the
[catalog decision](docs/decisions/0004-portable-library-snapshots.md).
Library identity, initialization/status, reader/writer locking, and initialization
recovery are implemented. Single-file managed import is a separate journal; see
[decision 0009](docs/decisions/0009-managed-epub-import.md). EPUB package
identifier evidence is recorded on those holdings; see
[decision 0010](docs/decisions/0010-bibliographic-identity.md). Recording, track,
and cross-file relationships now include declared positions, related holdings,
LibriVox narrations, exact ordered recording byte identities, and explicit user
declarations across encodings and track divisions (decisions 0020, 0044, 0049,
0052, and 0055). The user relationship journal preserves provider provenance and
holds only exact recording references and ordered-byte identities.

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
- Missing archive/scanner tools produce actionable capability reports; `doctor`
  reports the directory descriptor bridge and scanner recommendations.
- One native executable per release target, with checksums and build metadata.
- Synthetic fixture tests, without personal or copyrighted source dumps in the repository.
- Remove legacy Python source/tests/manifests/workflow and obsolete install instructions.
  No Python runtime, wrapper, or build/test dependency in the product.
- Pinned Go verification tools, enforced coverage, and native CI replace prototype checks.
- Establish work, edition, publication-version, recording, track, and asset identities.

Acceptance audit, 2026-10-20: configuration path/precedence and CLI envelope,
usage, cancellation, initialization/locking/recovery, and identity tests exercise
the implemented contracts over synthetic fixtures. `library related` preserves
distinct editions and paper versions under declared work/version evidence;
recording manifests, exact ordered byte identities, and explicit user statements
preserve assets and distinguish readings. Collection tests cover exact resolution,
partial/conflicting recordings, retained attempts, idempotent acquisition, revision
diffs, and preservation of earlier holdings and bookmark notes after an update.
See decisions 0004, 0006, 0010, 0018, 0044, 0049, 0050, and 0052 through 0055.

At `ea94da3`, [hosted CI](https://github.com/blisspixel/nemalo/actions/runs/39451688512)
passed the canonical verifier, race detector, and cgo-free executable smokes on
Linux, macOS, and Windows, plus installers on each OS. Local packaging of that
clean revision produced all six target archives, checked embedded target/revision
metadata, and independently verified every SHA-256 in `SHA256SUMS`. These are
cross-built artifacts, not runtime proof for every architecture. The native host
matrix, final format freeze, and 1.0 release acceptance remain stages 5 and 6;
published prereleases may lag the source. No Python source, runtime, build, or
test dependency remains in the application.

## Milestone 2: inspect and stage local downloads

Status: implemented within the bounded contracts below. Inventory, standalone file checks, and folder ingest with ZIP
extraction into private staging exist
([decision 0017](docs/decisions/0017-folder-and-zip-ingest.md)). RAR and 7z
members are extracted through an installed 7-Zip, by exact name to standard
output, with the same accounting and CRC checks
([decision 0043](docs/decisions/0043-rar-and-7z-through-7-zip.md)); the pure-Go
readers checked on 2026-10-11 lack verifiable checksums or memory bounds (see the
[research record](docs/RESEARCH.md#version-sensitive-research-record)).
Split 7z and multi-volume RAR sets are read through their first volume
([decision 0046](docs/decisions/0046-archive-volume-sets.md)). RAR sets include
old-style `.rar`/`.r00` names; each volume's format, role, ordinal, and size is
checked in isolation, adjacent split-member evidence must agree, and the
assembled listing must account for every volume without errors or unresolved
splits ([decision 0056](docs/decisions/0056-rar-volume-identity.md)). Gaps,
ambiguous names, overlapping families, and unlinked boundaries preserve the
set. Split ZIP byte streams (`.zip.001`) are joined and read through the Go ZIP
extractor after a maintained-reader extent check. True `.z01`/`.zip` segments
must present compatible sizes, ordinals, counts, and member tables through every
part before exact-name extraction through 7-Zip
([decision 0057](docs/decisions/0057-split-zip-intake.md)). Complete byte chunks now
qualify for recoverable whole-set cleanup
([decision 0061](docs/decisions/0061-split-zip-stream-cleanup.md)). True segments
also require independent raw disk references, header boundaries, directory counts
and full-byte coverage before whole-set cleanup
([decision 0062](docs/decisions/0062-segmented-zip-cleanup.md)). Nested standalone
ZIP/RAR/7z archives now traverse through a shared four-level archive-tree budget
and exact member-chain provenance
([decision 0058](docs/decisions/0058-bounded-nested-intake.md)). Cleanup now reuses
that tree with requested per-container coverage, a shared 16 MiB proof allowance
and exact held-leaf bindings. Covered standalone RAR children use the distinct
tree proof in [decision 0072](docs/decisions/0072-nested-rar-cleanup.md); covered
nested volume sets use separate `archive-tree/3`
([decision 0082](docs/decisions/0082-nested-volume-tree-cleanup.md)). Every unproven tree remains preserved
([decision 0063](docs/decisions/0063-complete-archive-tree-cleanup.md)). `inspect --par2`
now inventories bounded packet metadata and declared sets without verifying or
repairing protected files. Recovery material stays preserved by ingest and
cleanup ([decision 0059](docs/decisions/0059-par2-metadata-inventory.md)). Native
Windows junction/symbolic-link tests now run without privilege-based skips
([decision 0008](docs/decisions/0008-filesystem-and-display-boundaries.md)). Full
milestone acceptance is recorded in issue 57, with
[bounded evidence and limits](docs/VALIDATION.md#stage-2-archive-intake-and-recoverable-cleanup).
Depends on milestone 1.

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

Status: partial. Shared EPUB checks, Defender and ClamAV adapters with batched
ClamAV scans, and single-asset managed import and rescan exist. EPUB package
identifiers can be recorded as evidence. `doctor` names the default scanner,
its setup command, and signature age. ZIP extraction is in milestone 2 ingest.
PDFs whose page tree and active content were fully examined and clean can be
checked ([decision 0032](docs/decisions/0032-pdf-checked-policy.md)). An MP3 whose frames
index without damage or truncation can be checked
([decision 0033](docs/decisions/0033-mp3-checked-policy.md)), as can an M4B
or M4A whose every sample was located in its media data
([decision 0039](docs/decisions/0039-mp4-audio-samples.md)). EPUB font
obfuscation is a recorded fact, not DRM, while other encryption and DRM
licenses stay restricted ([decision 0036](docs/decisions/0036-epub-font-obfuscation.md)).
Holdings filter by declared base language without guessing missing tags
([decision 0037](docs/decisions/0037-base-language-filter.md)). `check
--epubcheck` runs an installed EPUBCheck as optional conformance evidence
([decision 0038](docs/decisions/0038-optional-epubcheck.md)).
Recording, track, and cross-file work relationships now use the stage-1 contracts
above. Full intake/publication acceptance still depends on milestone 2.

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

Status: implemented within the bounded contracts below. Plan-based source cleanup into a recoverable holding area,
journaled crash recovery, and restore are implemented in CLI, JSON, and TUI
([decision 0022](docs/decisions/0022-recoverable-source-cleanup.md)). `library
purge` removes a completed cleanup's held copies whose bytes the library holds
and verifies, after its own preview and plan, and resumes from its manifest
([decision 0041](docs/decisions/0041-purge-held-sources.md)). 7z archives
and split 7z sets move only when whole as 7-Zip lists them; unsupported RAR layouts stay
([decision 0051](docs/decisions/0051-cleanup-of-7z-and-split-sets.md)). `library check` reports missing, changed,
untracked, and unscanned entries, unfinished operations, and held copies without
changing anything ([decision 0024](docs/decisions/0024-library-check.md)).
ZIP byte streams move as one set only with complete byte coverage and every
member held. Purge binds the full set and its current proof to a durable intent,
then verifies each remaining part independently during partial recovery
([decision 0061](docs/decisions/0061-split-zip-stream-cleanup.md)).
True disk segments use `zip-segments/1`, including the terminal `.zip`, under
the same recovery protocol. Raw disk/header checks precede shared framing checks
through a read-only view; the installed reader's table is bound to central records
before extraction ([decision 0062](docs/decisions/0062-segmented-zip-cleanup.md)).
Complete nested ZIP/7z trees bind exact member chains, byte identities and holdings
in `archive_leaves`. Fresh purge repeats the whole-tree proof; its schema-2 group
also binds the canonical `leaf_digest`. Recovery never adopts changed leaf evidence
or tries to reconstruct a partially purged tree
([decision 0063](docs/decisions/0063-complete-archive-tree-cleanup.md)).
Flat 7z archives and sets now bind the same proof and exact leaves. Historical
unversioned 7z entries retain every remaining part through cleanup/purge recovery
for restore and a new checked cleanup
([decision 0065](docs/decisions/0065-versioned-7z-coverage.md)).
Standalone RAR5 book leaves use `rar5-frames/1`, a source identity and exact direct
leaf selections under the same recoverable operations. Fresh purge repeats complete
framing/reader/holding checks and binds the single source and leaf digest. This
proof excludes RAR4, sets and nested RAR layouts
([decision 0066](docs/decisions/0066-standalone-rar5-cleanup.md)).
Standalone RAR4 has its separate `rar4-frames/1` proof, and covered trees with
standalone RAR nodes use `archive-tree/2`
([decision 0070](docs/decisions/0070-standalone-rar4-cleanup.md),
[decision 0072](docs/decisions/0072-nested-rar-cleanup.md)). Modern root RAR4/RAR5
sets use distinct `rar4-set/1` and `rar5-set/1` contracts
([decision 0076](docs/decisions/0076-modern-rar4-set-cleanup.md)). Completely
covered old-name root sets use separate `rar4-old-set/1` and `rar5-old-set/1`
with the same whole-set preservation and recovery protocol
([decision 0079](docs/decisions/0079-old-name-rar-set-cleanup.md)). Covered nested
volume trees use `archive-tree/3` under the same recovery protocol
([decision 0082](docs/decisions/0082-nested-volume-tree-cleanup.md)); unsupported
or uncertain sources remain preserved. Exact-final writer acceptance is recorded
in issue 57 before closure. See the
[acceptance audit](docs/VALIDATION.md#stage-2-archive-intake-and-recoverable-cleanup).
Disk-full and scanner-failure faults are injected in tests: a full disk
during an import copy, a journal line, a cleanup copy, and an export, and a
scanner that is unavailable, incomplete, failing, or timing out. Each leaves
sources intact and the library recoverable. A torn last journal line holds
no record, is reported by `library check`, and is removed by a subsequent
authorized append to that journal.
Writes of known size refuse up front when the disk lacks room. While one
writer holds a library, import, ingest, cleanup, and rescan are refused as busy
before writing; racing imports all finish consistently when retried; an
unwritable store or control folder refuses an import, keeps the source, and
recovers once writable; an existing different file at an asset, export, or
restore name is never replaced. Depends on milestone 3.
Required for a production cleanup release.

Deliver plan-based source cleanup, review handling, library checking, and interruption
recovery. Cleanup defaults to a recoverable holding area; permanent purge is a later,
explicit action with a separate preview.

Acceptance:

- `library cleanup LIBRARY FOLDER` previews exact dispositions and reasons without
  `--apply`; applying requires that recorded plan.
- Applying a plan rechecks source identities and destination checksums, refusing changed inputs.
- Append durable intent before mutation and durable completion afterward.
- Test crashes before/after copying, verification, publication, catalog updates, and source moves.
- Cross-filesystem operations copy and verify before removing an original.
- Disk-full, permission-denied, name-conflict, scanner-failure, and concurrent-run cases preserve data.
- Review destinations cannot overwrite content or escape their configured root.
- `check` reports missing, modified, invalid, and unscanned entries without silently fixing them.
- Garbage collection never removes unaccounted-for archive contents or recovery files.

## Milestone 5: collection discovery and selected downloads

Status: partial. Open Library/Archive search with explicit result order, Archive
source evaluation, and selected-file intake acquisition with a post-download
scan and quarantine exist in CLI/JSON/TUI. See the
[acquisition contract](docs/ACQUISITION.md). Whole declared recordings are
acquired as ordered track packets with track-level resume
([decision 0018](docs/decisions/0018-whole-recording-acquisition.md)), and an
interrupted transfer resumes byte-wise against a strong validator
([decision 0023](docs/decisions/0023-validator-bound-download-resume.md)).
Gutenberg search and evaluation read a local copy of the official CSV, and
`catalog fetch gutenberg` downloads that file
([decision 0028](docs/decisions/0028-gutenberg-local-catalog.md)).
`evaluate gutenberg:NUMBER --files` and
`acquire gutenberg:NUMBER --file NAME` fetch one bibliographic record from
gutenberg.pglaf.org and can download one named EPUB into the existing intake
packet ([decision 0029](docs/decisions/0029-gutenberg-selected-epub.md)).
`search --source all` searches every source at once, each with its own
status, so one provider's failure does not hide another's results. Website
search, formats other than those three EPUBs, and broader provider contracts
remain planned. Transfers and checked publication use milestones 2
and 3. Source cleanup is independent and requires 4.

An Open Library work lists editions, and one chosen edition can name an
Archive item through its `ocaid`
([decision 0030](docs/decisions/0030-openlibrary-selected-edition.md)).
The first work-level Archive identifier is not a selection. Keep using
Gutenberg's permitted catalog and the pinned mirror. Do not automate
website search. See [source research](docs/SOURCES.md).

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

Status: partial. A read-only stdio MCP server exposes library status, holdings,
audits, file checks, and EPUB retrieval over the shared services, with catalog
tools behind `--allow-network` and paths confined to configured scopes; see
[decision 0014](docs/decisions/0014-read-only-mcp-server.md). Claude Code and
Agent Plugins packages share one skill and were validated in Claude Code on
Windows ([decision 0019](docs/decisions/0019-agent-plugin-packages.md)).
`--allow-write` adds import tools that store only a previewed plan
([decision 0034](docs/decisions/0034-mcp-import-tools.md)). Acquisition and
rescan run as durable jobs with explicit IDs, journaled state, progress,
cancellation, and interrupted-job reconciliation, outliving the connection
([decision 0045](docs/decisions/0045-durable-mcp-jobs.md)). A broader host and
platform matrix remains. Read-only tools depend on milestone 1 and the relevant provider
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
TUI unit selection/repeat/continuation. The default `epub-text/2` keeps
illustrated documents readable and locates each omitted element
([decision 0011](docs/decisions/0011-default-text-with-located-gaps.md)). [Content access](docs/CONTENT-ACCESS.md)
records exact locators and bounds. [Structured EPUB](docs/STRUCTURED-EPUB.md)
records implemented parts, language/direction, local notes/backlinks, source XML,
image-member identity, and per-range gaps from
[issue 1](https://github.com/blisspixel/nemalo/issues/1) and
[issue 2](https://github.com/blisspixel/nemalo/issues/2).
Table grids and rendered mathematics are explicitly unsupported, with original
markup available separately. The read-only MCP `content_*` tools expose the same
contract. MP3 tracks have probed timing and decodable frame ranges
([decision 0015](docs/decisions/0015-mpeg-audio-frame-index.md)), recordings have
ordered manifests ([decision 0020](docs/decisions/0020-recording-manifest.md)), and
PDFs have page indexes, printed labels, text with located gaps, and exact
quotation spans ([decision 0021](docs/decisions/0021-pdf-pages-and-text.md)).
A terminal reader shows EPUB and PDF text as pages or one word at a time over
the same ranges, breaking EPUB text only at structural line feeds. It opens at
the last excerpt, follows source notes and returns, and closing names the place
and writes nothing ([decision 0025](docs/decisions/0025-terminal-reading.md)).
The [reader review](docs/READER-REVIEW.md) compares primary documentation for
mature readers and pacing tools. Exact place retention and pause correctness
precede further controls. Full-screen contents now reuses declared-entry reading
and physical PDF units, with exact hosted acceptance at `b40b0d1`. In-reader exact
search adapts the existing source-bound lookup, with exact hosted acceptance and
[native verification](https://github.com/blisspixel/nemalo/commit/ec0f64b8363c561b8b503435ea05c406e6cea0f2#commitcomment-206425547)
at `ec0f64b`.
Broader normalized search and assistive-technology validation remain distinct
work. Source implementation and release remain separate. The
[native workflow review](docs/VALIDATION.md#native-workflow-review-on-2026-10-24)
records selected installed-tool and CLI/MCP evidence with explicit coverage gaps.
Source-declared EPUB 3 nav and EPUB 2 NCX hierarchies are `epub-nav/1`: a read
follows one entry until the next entry that is not nested inside it, and a
spine boundary is not that end
([decision 0026](docs/decisions/0026-epub-navigation-and-chapter-selection.md)).
Declared EPUB 3 page labels are `epub-page-list/1`, read from one marker by
reference or exact label ([decision 0031](docs/decisions/0031-epub-page-list.md)).
Claude Code validated the MCP contract as an external caller. Both issues are
closed; OCR, column reconstruction, and decoded audio continue in milestones 7 and 8.
M4B and M4A list their sample tables and chapters as `mp4-audio/1`
([decision 0039](docs/decisions/0039-mp4-audio-samples.md)).

- Initial local access: resolve a stable library ID to exact assets/provenance,
  export/open in an explicitly configured reader, and report changed/missing files.
  Implemented for managed holdings: list and search, and open or export a
  verified copy ([decision 0016](docs/decisions/0016-reader-handoff-copies.md)).
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
- Explicit local bookmarks, one saved location per asset plus an optional note,
  with export, import, and a conflict when the bytes or representation change
  ([decision 0027](docs/decisions/0027-explicit-local-bookmarks.md)). Reading
  lists remain planned. Bookmarks stay optional and local. MCP does not read
  or write them.

## Milestone 7: research papers and PDF collections

Status: partial. A PDF parser is selected and wrapped with budgets; `check` walks
page trees, and `pdf-text/1` gives physical page indexes, printed labels, text in
content-stream order with located gaps, and exact quotation spans
([decision 0021](docs/decisions/0021-pdf-pages-and-text.md)). Active content,
attachments, automatic actions, form submission, and remote references are
reported, and a clean PDF can be checked
([decision 0032](docs/decisions/0032-pdf-checked-policy.md)). One explicit
arXiv paper version can be evaluated and acquired, with its latest-version
license applied only to that version
([decision 0047](docs/decisions/0047-arxiv-selected-versions.md)). OCR, column
reconstruction, other providers, and citation export remain. Depends on shared assessment/publication and recovery milestones.

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
  unknown fields retained rather than invented. Delivered with milestone 9.
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

Status: partial. Single MP3 files have a frame index with probed versus
declared timing, ID3 chapters, and decodable frame-aligned ranges through the
shared content contract; see
[decision 0015](docs/decisions/0015-mpeg-audio-frame-index.md). Whole Archive
recordings are acquired in declared track order with track-level resume, and
ingest records each track's declared place
([decision 0018](docs/decisions/0018-whole-recording-acquisition.md)).
`recording-manifest/1` reports held tracks in order with exact hashes, missing
positions, conflicts, and probed timing and offsets
([decision 0020](docs/decisions/0020-recording-manifest.md)). Readers come from
the LibriVox project an item names, linked only when it names the item back
and credited per track only when sections match tracks by count and length
([decision 0035](docs/decisions/0035-librivox-readers.md)), and recordings of
one reading in different formats share a narration while different readings
stay apart ([decision 0044](docs/decisions/0044-narration-identity.md)). Complete
recordings with identical ordered asset hashes share a byte identity across
source items without changing their narration declarations
([decision 0052](docs/decisions/0052-recording-byte-identities.md)). User-declared
readings across different encodings and track divisions use exact held recording
selections and those byte identities, with unresolved or conflicting statements
reported separately from provider evidence
([decision 0055](docs/decisions/0055-user-narration-declarations.md)). M4B and M4A
files are assessed from their sample tables without decoding: truncation,
misplaced samples, fragments, and protected audio are findings, chapters come
from the QuickTime chapter track or a Nero list, and `mp4-audio/1` lists them
([decision 0039](docs/decisions/0039-mp4-audio-samples.md)). An M4B or M4A may
be 2 GiB; scans and transfers allow time by size, a stalled download continues
like a dropped one, and a ClamAV limit alert is an incomplete scan
([decision 0040](docs/decisions/0040-large-mp4-audio-and-scan-limits.md)).
`check --decode` decodes an MP3, M4B, or M4A with an installed FFmpeg and
reports decoded duration and decoder errors apart from probed timing
([decision 0042](docs/decisions/0042-optional-ffmpeg-decode.md)). M4B byte
ranges and decoded timing in content access remain. Depends on shared assessment/publication and recovery milestones.
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

## Milestone 9: cited evidence and agent reading

Status: implemented, with the exact hosted acceptance ledger in
[issue 39](https://github.com/blisspixel/nemalo/issues/39). Selected journal metadata,
CSL-JSON citation export and opt-in managed evidence/replay use shared services,
CLI JSON and scoped MCP. Synthetic checks cover exact ranges, replay, permission
separation, conflicts and failure discard; full local/native checks and the
[five selected semantic cases](docs/CITED-EVIDENCE-REVIEW.md) are recorded
separately from hosted acceptance. This is source work, not a release.
The pre-1.0 part of
[issue 37](https://github.com/blisspixel/nemalo/issues/37) and
[issue 38](https://github.com/blisspixel/nemalo/issues/38)
([decision 0048](docs/decisions/0048-cited-evidence-before-1-0.md)). Depends on
content access, milestone 1 identities, and milestone 8 audio ranges.

Deliver one versioned evidence result through `internal/app`, CLI JSON, and the
read-only MCP tools: the exact asset, work, edition or version, and recording
identities; the extractor and representation; passages, pages, sections, or time
ranges with their locators and exact text; and the coverage of the request, which
units were read, which were unsupported or had gaps, and where a bound stopped it.
Add documented citation metadata export, such as CSL-JSON, with provenance and
unknown fields retained. Extend the shared agent skill so an agent reads a work
from its question and the work's own structure, cites every location it relies on,
and reports what it did not read.

Acceptance:

- Quotation and source statement stay distinct from anything a caller generates;
  Nemalo stores no generated interpretation and runs no model.
- A request bounded by size or count reports the bound and a continuation, never
  a complete result. Unsupported units and gaps are listed, not skipped.
- An evidence reference resolves again to the same bytes and extractor, or fails
  visibly when the asset changed or was removed.
- Retrieval stays offline and read-only and records no reading or consumption.
- Using permitted public-domain works and an arXiv paper, review an agent's
  actual outputs for: an early idea developed or challenged later, cited at both
  places; two defensible readings of one passage kept apart; a result whose
  method and limitations survive the explanation; two sources that agree in
  summary but differ in definitions; and a changed question that changes what is
  read. Mechanical checks of citations support this review and do not replace it.

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

1. Settle Linux environments: Ubuntu is the hosted native CI runner; Omarchy 4
   (Arch Linux x86_64) was reviewed for desktop terminal and descriptor bridge
   compatibility.
2. Holding-area retention: held sources are kept until `library purge`
   removes the copies the library holds, after its own preview
   ([decision 0041](docs/decisions/0041-purge-held-sources.md)).
3. EPUBCheck conformance is an optional assessment, not part of the checked
   policy ([decision 0038](docs/decisions/0038-optional-epubcheck.md)).

The language is settled. These choices can be refined in their milestones with
conservative defaults; they do not justify starting additional stacks or services.

## Additional planned capabilities

These capabilities remain in the vision. They are not 1.0 gates and can be
delivered before or after it as their dependencies allow:

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
Cited evidence and agent-led reading over MCP are milestone 9, before 1.0; this
phase adds processing inside Nemalo, persistence of generated work, and capture
of sources beyond the library, all built on that evidence contract.
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
