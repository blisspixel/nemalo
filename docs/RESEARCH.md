# Related-project research and design conclusions

Reviewed 2026-10-08. Observations come from primary project documentation, not
installed-app tests, security audits, or benchmarks. Nemalo choices are engineering
judgments. No competitor code or implementation is copied.

## Requested comparisons

| Project and primary source | Documented pattern | Nemalo implication |
| --- | --- | --- |
| [Shelfwright](https://github.com/Shelfwright/shelfwright) | Windows acquisition/organization app, format/language filters, persistent queue, existing-download indicators, series and device/cloud organization | Study selection and queue UX; its sources and browser workflow do not define Nemalo's policy or stack |
| [shelfctl](https://github.com/blackwell-systems/shelfctl) | Go CLI/TUI, JSON commands, metadata catalog, on-demand transfers, GitHub Release storage | Useful scriptability and metadata/file separation; remote hosting is not required |
| [FolioPort](https://github.com/0xdps/folioport) | Developer-portfolio generator using structured content and templates | Not a book manager; declarative input/export is the limited relevant pattern |
| [ShelfKin](https://shelfkin.blog/about/) | Reading guides and explained recommendations | Useful collection-selection rationale, not an archive-handling design |
| [SourceLoom AI](https://www.sourceloomai.com/) | Document retrieval, background processing, citations, indexing state, source permissions | Useful evidence/job-state presentation without importing a hosted RAG stack |

FolioPort has multiple namesakes; the repository matches the supplied portfolio
description. ShelfKin and SourceLoom were reviewed through their own websites,
without assuming a matching open-source repository.

## Selection and acquisition

Shelfwright exposes language/format filters and already-held indicators. Nemalo
should show available editions and why a result matches. A persistent queue is a
useful workflow; Nemalo's durable jobs must additionally reconcile interrupted
transfers and verify before publication. Similar titles cannot establish identity:
translations, study guides, boxed sets, adaptations, and abridgments stay distinct.
Popularity is not proof of completeness, safety, or quality.

## One complete application interface

shelfctl documents both CLI and TUI workflows. Nemalo's first-class CLI/TUI and
JSON/MCP interfaces call shared services. None should grow a second import/cleanup engine.
Its [architecture](https://github.com/blackwell-systems/shelfctl/blob/main/docs/reference/architecture.md)
separates metadata and document storage. Nemalo can do that locally with provenance
and ordinary files, without a cloud account. Optional exports must preserve user
annotations and versions rather than replacing originals silently.

## Declarative collections and explained curation

FolioPort illustrates a structured-content boundary. A reviewed Nemalo collection
manifest can select source IDs, languages, and editions without executing scripts.
That does not justify multiple config formats or a template engine.

ShelfKin explains recommendations. Nemalo can describe a multilingual starter
collection's scope and rationale, keeping editorial judgment separate from verified
bibliographic facts. AI personalization is unnecessary to prove collection building.

## Access to content and evidence

SourceLoom describes citations, processing state, and access-scoped retrieval.
Nemalo should expose original sources, exact assessment evidence, and durable job
state. References must resolve to the selected local edition/version and authorized
asset, not a similarly named work.

This supports the knowledge-infrastructure vision without requiring embeddings,
hosted identity, a vector database, or an LLM. Begin with catalog search, local file
access, and supported content references. Full-text indexing and semantic retrieval
need separate resource budgets and quality evaluation.

## Pattern decisions

| Pattern | Decision and reason |
| --- | --- |
| Persistent acquisition queue | Adopt durable jobs for restart/cancellation and chapter sets |
| Existing-library markers | Adopt with evidence; preserve alternate editions |
| Language/format filtering | Adopt; central to evaluation and collection building |
| Series/narrator/version metadata | Adopt; prevent destructive semantic deduplication |
| Portable metadata and files | Adopt; local ownership, access, and recovery |
| Explicit collection manifests | Plan bounded, reviewed multilingual selections |
| Permitted OPDS | Plan standards-based source expansion |
| Reader/server export | Plan access through existing software |
| TUI | First-class interface alongside CLI; Bubble Tea/Bubbles v2 over shared services |
| Cloud/device sync | Later; credentials and conflicts need separate design |
| Browser-driven acquisition | Exclude initially; prefer documented APIs and allowed feeds |
| Hosted RAG or embedded LLM | Not required for core discovery/acquisition/access or MCP |
| Executable plugin loader | Not required to package Nemalo as an Agent Plugin |

## Deferred video and knowledge-processing research

Reviewed 2026-10-08 for the [post-1.0 roadmap phase](../ROADMAP.md#post-10-topic-watches-and-optional-knowledge-processing),
after all pre-existing planned work. Analysis is outside the 1.0 library/discovery
release. These are documentation observations, not
installed integration tests or a claim that Nemalo already supports these workflows.

| Primary reference | Documented workflow | Independent Nemalo design direction |
| --- | --- | --- |
| [Distillr overview](https://github.com/blisspixel/distillr) | Research goals, multi-source capture, cited questions, corpus audits, and recurring topic work | Extend Nemalo's existing resource lifecycle with optional processing, not a second library or mandatory model |
| [Distillr usage](https://github.com/blisspixel/distillr/blob/main/docs/usage.md) | Discovery previews, channel watches, catch-up runs, and refresh profiles | Separate update discovery from acquisition and inference; reuse collections, scheduling, and durable jobs |
| [Distillr outputs](https://github.com/blisspixel/distillr/blob/main/docs/outputs.md) | Source material, derived insights, topic synthesis, reports, and verification evidence | Keep sources and interpretation distinct, cite exact inputs, and preserve portable derived artifacts |

Nemalo will implement these capabilities itself in Go. Distillr will not be connected
through its CLI, MCP server, API, or any other integration.
No Distillr code, prompts, configuration, tests, or packages will be copied, ported,
embedded, invoked, or made dependencies. Its implementation language and provider
stack do not alter Nemalo's Go decision. Do not inherit every feature or quality claim:
editorial publishing, arbitrary relevance scores, and additional model integrations
need their own demonstrated Nemalo requirement. Correction propagation and selective
revision are Nemalo acceptance goals, not independently validated Distillr behavior.

### Integration constraints from primary documentation

- [Karpathy's LLM Wiki idea](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f)
  describes retained sources, interlinked Markdown maintained incrementally, query
  results saved as pages, an index/history, and maintenance checks. It is a conceptual
  pattern, not a package to integrate. Nemalo will adapt it as an optional derived
  workspace with source citations, protected human notes, checked writes, and recovery.
  Do not inherit claims that models always maintain consistency or that upkeep is free.
- OpenRouter's [HTTP quickstart](https://openrouter.ai/docs/quickstart) supports direct
  API calls without a language SDK. Evaluate a Go standard-library client first.
  Its [routing controls](https://openrouter.ai/docs/guides/routing/provider-selection)
  and [data-retention controls](https://openrouter.ai/docs/guides/features/zdr) require
  explicit configuration and verification. Record the actual route and usage; do not
  assume compatible endpoints imply identical behavior, privacy, or enforceable costs.
- OpenRouter documents an [OpenCode integration](https://openrouter.ai/docs/cookbook/coding-agents/opencode-integration).
  These are different roles: OpenRouter provides model access; OpenCode is a harness
  that can use it. Neither is required for local Markdown storage or basic topic updates.
- YouTube's [channel resource](https://developers.google.com/youtube/v3/docs/channels)
  exposes an uploads playlist, retrievable with
  [playlistItems.list](https://developers.google.com/youtube/v3/docs/playlistItems/list).
  Evaluate this path for known-channel updates instead of repeatedly searching the
  entire catalog. Quotas, pagination, changes, and removals still require reconciliation.
- [Captions.list](https://developers.google.com/youtube/v3/docs/captions/list) requires
  authorization and returns track information, not caption text.
  [Captions.download](https://developers.google.com/youtube/v3/docs/captions/download)
  requires permission to edit the video. Public videos therefore cannot be assumed
  to provide transcripts through this API. Preserve links when permitted evidence
  is unavailable; do not claim analysis of content that was never captured.
- Review YouTube's [developer policies](https://developers.google.com/youtube/terms/developer-policies)
  for permitted acquisition and provider-data storage, refresh, and deletion rules.
  A permanent local preservation policy cannot override those requirements.
- OpenCode documents a [headless HTTP server and OpenAPI interface](https://opencode.ai/docs/server/).
  This makes a Go HTTP adapter a candidate without adopting its JavaScript SDK.
  Alternatively, an agent host can consume Nemalo's MCP tools. Select and test the
  integration when the deferred phase begins, without installing another runtime now.
- OpenCode's [provider configuration](https://opencode.ai/docs/providers/) includes
  local and remote routes. A local server address does not prove local inference.
  Verify the effective route and data destinations before processing private content.
- OpenCode's [V2 permissions documentation](https://opencode.ai/v2/docs/permissions)
  explicitly differs from V1 field/action names. Pin a supported stable version,
  inspect its schema, and test effective restrictions; do not paste a version-agnostic
  permissions example into Nemalo's standing instructions. Agent settings alone are
  insufficient evidence of filesystem or network isolation.

The improvement target is measurable: reliable refresh/recovery, bounded processing,
accurate citations, visible uncertainty, useful synthesis, and targeted invalidation
after source changes. No superiority or performance claim is established by this
research. Models, transcription tools, and adapters remain undecided until this phase.

## Version-sensitive research record

These are observations, not installation commands or permanent pins. Recheck stable
patches, advisories, compatibility, and contracts when implementation begins.

| Surface | Observation and primary reference |
| --- | --- |
| Go | [Official downloads](https://go.dev/dl/?mode=json) list stable 1.27.2; `go.mod` pins it. The Go launcher selected/downloaded that toolchain without replacing the system installation. |
| TUI | [Bubble Tea v2.0.10](https://github.com/charmbracelet/bubbletea/releases/tag/v2.0.10) and Bubbles v2.2.1 pinned after reviewing current module versions; see the [terminal decision](decisions/0002-terminal-interfaces.md) |
| MCP | Current [2026-07-28 specification](https://modelcontextprotocol.io/specification/2026-07-28) |
| Go MCP SDK | Official [v1.8.0 stable release](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.8.0), supporting that revision |
| Agent Plugins | [1.0.0 specification](https://agent-plugins.org/specification), distinct from MCP wire behavior |
| Static analysis | [Staticcheck](https://staticcheck.dev/docs/), pinned development tool `honnef.co/go/tools` v0.8.1 |
| Vulnerability checks | Official [govulncheck guidance](https://go.dev/security/vuln/); `golang.org/x/vuln` v1.8.0 pinned as a development tool |
| Providers | [Source directory](SOURCES.md) records current API/access caveats and research limits |

## Terminal interaction reviewed on 2026-10-08

The [Charm v2 release](https://charm.land/blog/v2/) and the installed, pinned
Bubble Tea/Bubbles/Lip Gloss APIs informed the terminal increment. Use existing
keyboard/input/viewport primitives, terminal background/color capability messages,
and the [NO_COLOR convention](https://no-color.org/). Lip Gloss and colorprofile
were already transitive modules; direct use changed no module version or checksum.
There is no additional terminal framework, renderer, or application runtime.

Nemalo's concrete choices are visible direct/reverse navigation, session-scoped
forms/results, explicit network and scanner context, result pagination, exact
filename-format filters, bounded local read budgets, and responsive layouts.
Synthetic tests cover 40x20 through 160x45, long errors, no-color/light/dark states,
busy guards, and edited-query paging. A native Windows terminal exercise and
real-data rendered frames supply separate evidence. This does not establish a
screen-reader audit, every terminal emulator, or every user's accessibility needs.

The subsequent browsing refinement also consulted the
[CLI guidelines](https://clig.dev/#saying-just-enough) for concise output and
discoverable help. That guide explicitly excludes full-screen interface design;
the selectable list, detail pane, and indigo palette are Nemalo design choices,
not a claimed terminal standard. No dependency or version changed.
The pinned viewport's soft wrapping can split wide characters and rewrap them
past its height. Nemalo pre-wraps original reports/details with the existing
Lip Gloss grapheme-aware wrapper, retaining complete evidence independently of
layout. Tests cover odd/even widths, long Japanese titles, reports, and focus.

## Evidence still required

No competitor runtime comparison, complete source audit, Go benchmark, or plugin-host
test was performed. The implemented foundation has offline Go tests, native Windows
race checks, live Open Library/Archive searches, Archive item evaluation, and Windows
TUI search/inspection/evaluation/quit smokes. The provider increment is recorded in
[decision 0005](decisions/0005-provider-search-and-evaluation.md). Native CI
configuration covers Linux/macOS/Windows; actual run results are
separate evidence. Neither foundation coverage nor the old prototype certifies
the planned archive, scanner, recovery, or acquisition behavior.
