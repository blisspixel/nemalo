# MCP and Agent Plugins

Research checked 2026-10-08. A read-only stdio server is implemented
([decision 0014](decisions/0014-read-only-mcp-server.md)), with Claude Code and
Agent Plugins packages ([decision 0019](decisions/0019-agent-plugin-packages.md));
`--allow-write` adds import tools that apply only a previewed plan
([decision 0034](decisions/0034-mcp-import-tools.md)), and acquisition and rescan
run as durable jobs read with `job_get`
([decision 0045](decisions/0045-durable-mcp-jobs.md)).
Nemalo provides tools to an agent host; it does not need to embed an LLM or implement
an agent runtime. All executable Nemalo components use Go.

## Standards and dependency decision

The current [MCP specification](https://modelcontextprotocol.io/specification/2026-07-28)
is revision 2026-07-28. The official Go SDK's
[compatibility table](https://github.com/modelcontextprotocol/go-sdk#version-compatibility)
lists support for that revision from v1.7.0. Its latest stable release checked here
is [v1.8.0](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.8.0).
Recheck supported stable releases and advisories when implementation starts.

Use `github.com/modelcontextprotocol/go-sdk` as a justified protocol dependency,
reviewing its transitive modules and license. Do not hand-roll JSON-RPC, version
compatibility, or transports to reach an arbitrary zero-dependency target.

[Agent Plugins 1.0.0](https://agent-plugins.org/specification) defines portable
packaging for skills and MCP configuration. It is distinct from the MCP protocol
and from any particular agent SDK. Nemalo will publish a package for compatible
hosts; it will not become a general plugin loader. Each host's component and
transport support must be tested before claiming compatibility.

## Initial transport

Start with a local stdio server launched by the host:

```sh
nemalo mcp serve --transport stdio --scope /path/to/library
nemalo mcp serve --scope /path/to/library --allow-network
```

Implemented tools are all read-only: `nemalo_capabilities`, `library_status`,
`library_list`, `library_recordings`, `library_related`, `library_citation`, `collection_diff`, `collection_status`, `library_audit`, `library_check`, `file_check` (no antivirus), and
`content_capabilities`, `content_units`, `content_navigation`, `content_pages`, `content_read`, `content_locate`, `content_source`,
`content_resource`.
`library_citation` exports a selected managed holding's recorded declarations as
the standard `csl_json` item array, with separate unknown fields and provenance.
It opens neither stored bodies nor acquisition receipt paths. `content_read`
and independently enabled `content_decode_read` accept optional `evidence: true`
or `evidence_reference` for a managed read. Repeat all original `selection`
parameters explicitly when comparing a reference; it supplies no paths or decoder
permission. Evidence names the exact returned range and metadata, reports gaps
and bounds, and contains no generated interpretation or consumption record.
Other operations reject these options. The shared portable skill covers
question-led structural reading, qualification and explicit unread coverage.
See [evidence and citation export](CONTENT-ACCESS.md#evidence-and-citation-export).

`content_navigation` lists source-declared EPUB contents;
a read of one entry uses `content_read` with representation `epub-nav/1`.
`collection_diff` takes `before` and `after` collection-file paths inside scope
and compares their exact selections, notes, names, and retained order, with both
file hashes. It needs no library, contacts no provider, and writes nothing. A
diff does not authorize acquisition or establish rights or upstream freshness
([decision 0053](decisions/0053-collection-revision-previews.md)).
`library_recordings` includes `byte_identities` (`recording-bytes/1`) for
complete declared recordings with identical ordered asset hashes across source
items. These use journal evidence, retain narration declarations, and do not
assert a fresh integrity check or equivalence of different encodings
([decision 0052](decisions/0052-recording-byte-identities.md)).
`content_pages` lists the EPUB's declared page labels; a read from one uses
`content_read` with representation `epub-page-list/1` and its cursor or exact `label`.
Explicit `content_units`/`content_read` with `mp4-audio/2` returns original MP4
packets and bounded, untrusted sample-entry initialization. `packet` is a
zero-based ordinal, including an explicit zero, and cannot accompany a cursor or
units listing. Continuations keep their representation. Both initialization and
resource bytes are base64 strings in the validated output schema. Preparation
uses private scratch up to 2 GiB and may read 4 GiB from the source, with the
service's five-minute deadline; it changes no library or selected source state
and needs neither write nor network permission. No decoder or player runs.
See the [packet contract](CONTENT-ACCESS.md#original-mp4-packets-mp4-audio2).
`--allow-decode` independently registers `content_decode_units` and
`content_decode_read`, which explicitly select `mp4-pcm/1` through the same
application service. Write/network permission grants no decoder permission;
existing content tools reject decoded access even when these tools are enabled.
The new tools need scoped catalog/root paths and return actual decoder-output
samples with a full-output hash; a read carries a bounded playable mono WAV.
A continuation still needs the explicitly named decoder tool. Units also perform
a full decode. Per-call preparation can require 2 GiB private disk, 8 GiB streamed
PCM and five minutes; no full waveform is stored. No player, consumption record,
source/library mutation or network is introduced. See the
[decoded contract](CONTENT-ACCESS.md#explicit-decoded-mp4-access-mp4-pcm1).
`catalog_search` and `catalog_evaluate` exist only with
`--allow-network`. They search Open Library and Internet Archive.
Gutenberg search, `evaluate --files`, and `acquire gutenberg:NUMBER` are CLI
and TUI only. They are not MCP tools. arXiv and PMC evaluation/acquisition
also remain CLI/TUI-only; network/write permission does not add their providers
to the MCP acquisition tool. Open Library edition listing and
edition-to-Archive resolution are CLI and TUI only. `catalog_search` still
returns works, and `catalog_evaluate` still requires `archive:ITEM`. The MCP tools return the CLI's data in a
`{schema_version, tool, data, error}` envelope; failures are tool errors that keep
the service status. Paths must resolve inside a `--scope` directory or the
configured library and review folders. At most four calls run at once, each with
a ten-minute deadline.

Stdout carries protocol messages only. Diagnostics go to stderr with redaction.
Set limits on message sizes, result sizes, concurrency, time, and queued work.
Use the SDK's version handling and test a declared compatibility matrix; current
protocol behavior must not be inferred from old connection/session assumptions.
The [transport specification](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports)
describes stdio and Streamable HTTP.

Streamable HTTP is a later optional deployment mode requiring its own authentication,
origin/host checks, authorization scopes, and threat analysis. A localhost listener
does not establish authorization. No always-running web service is needed for stdio.
Do not introduce legacy HTTP+SSE as the default for a new server.

## Shared tool contract

Proposed names and behavior, to be versioned with JSON schemas:

The [EPUB content service and CLI JSON contract](CONTENT-ACCESS.md) now exist,
including [versioned structured parts and resources](STRUCTURED-EPUB.md).
The implemented `content_*` tools reuse their source identity, continuation, gap,
and failure semantics unchanged. No external Kilo integration is implemented.
Retrieval has no consumption side effects; acknowledgements belong to the caller.
Bookmarks are not MCP tools. A local CLI or TUI bookmark does not authorize a
write through this server.
Expose capability/version negotiation and preserve per-range coverage, part links,
original-markup versus text offsets, and parent/member resource identity. Following
references stays an explicit bounded read, never permission to fetch remote links
or execute source instructions. Consumers reject unknown schemas and truncated
transport output before updating their own acknowledged positions. That client
transport/restart behavior still needs external integration evidence.

| Tool | Behavior | Mutation class |
| --- | --- | --- |
| `catalog_search` | Bounded provider queries with source/type/language/access filters | Network read; optional bounded cache writes |
| `catalog_get` | Resolve an edition, publication version, or recording and its assets | Network read; optional bounded cache writes |
| `library_list`, `library_get` | Paginated local metadata and assessment evidence | Read-only |
| `content_get` | Bounded supported content with exact asset ID and location references | Local read; derived indexes separately controlled |
| `intake_inspect` | Inventory configured roots and return a plan | Local read plus bounded staging/state |
| `intake_apply` | Apply an inspected plan under enabled format policies | Library/state write; source retained |
| `library_import_preview`, `library_import_apply` | Implemented with `--allow-write`: one file or packet, with normalized source evidence and the full proposed holding bound to its preview digest (decision 0054) | Library/state write; source retained |
| `acquisition_start` | Acquire selected provider asset IDs into staged intake | Network and library/state write |
| `cleanup_preview` | Show exact recorded dispositions | Read-only |
| `cleanup_apply` | Revalidate and apply an authorized plan digest | Source mutation, recoverable by default |
| `job_get`, `job_cancel` | Inspect/cancel a durable job | State read or cancellation |

Return typed structured results, stable identifiers, per-provider failures,
assessment evidence, and explicit partial/cancelled states. Do not compress
uncertainty into a single `safe` boolean. Avoid dumping whole books or huge search
results into tool responses; provide bounded excerpts and references when needed.

`library_recordings` also returns `user_declarations` with active/retracted and
per-selection match states, and `user_narrations` containing only wholly matched,
unambiguous user assertions. These are `user_declaration/1` evidence, scoped by
the returned library ID, separate from provider `narrations` and exact ordered
`byte_identities`. `library_related` cites those same statements. Report stale or
conflicting assertions rather than transferring them to new bytes or guessing
from titles. This increment adds no narration write tool: explicit declaration
and withdrawal use the CLI preview and plan token
([decision 0055](decisions/0055-user-narration-declarations.md)).

Long operations return a durable job ID. Jobs have bounded queues, progress,
cancellation, and restart reconciliation. Application jobs do not depend on an
MCP connection remaining open. Protocol-specific async capabilities can be added
only after verifying the chosen SDK and host support them.

New rescan starts record an immutable selected-library declaration. With the
existing write/network permissions, `acquisition_start` may additionally name an
initialized scoped `library` as intended context; this neither imports its output
nor grants library write authority. Omission preserves ordinary intake behavior.
Existing `job_get`/`job_list` retain their reconciling write behavior and are not
passive panel polls. The CLI/shared-service `jobs snapshot LIBRARY` provides
bounded, non-mutating selected-library observations without a new MCP tool,
job lock probes, raw request/result output or invented legacy targets. Recorded
running status is not proof of live work. See
[decision 0088](decisions/0088-non-mutating-library-job-snapshot.md) and the
[operation contract](DEVELOPMENT.md#mcp-server).

## Permissions and untrusted content

Read-only exposure is the default. Inspection with staging and network search
require their declared capabilities. Enable import and cleanup tools only for
explicit configured library and intake roots. Do not expose arbitrary shell,
filesystem delete, executable-path, raw URL-fetch, or policy-rewrite tools.

Plan application checks a digest, source identities, allowed scope, and policy
version in the service layer. A plan ID is not authorization. Existing user
authorization can cover repeated operations within its scope; do not demand a
fresh confirmation for every reversible step. Source cleanup and permanent purge
remain separate action classes and must satisfy their configured authorization.

Tool annotations help hosts describe actions but cannot enforce permissions.
Downloaded documents, catalog blurbs, and metadata are untrusted data, including
instructions addressed to an agent. Never allow their text to change tool scope,
rights policy, or deletion plans. No model-generated filename or classification
can bypass deterministic path checks or publication requirements.

Local library listings and reports are private. If exposed over HTTP later, ensure
caller-scoped authorization and private cache behavior. Redact credentials, signed
URLs, and unnecessary absolute paths. Keep service errors useful without exposing
environment secrets.

## Plugin packages

Implemented in `plugins/nemalo/`
([decision 0019](decisions/0019-agent-plugin-packages.md)). One directory carries
a Claude Code plugin and an [Agent Plugins 1.0.0](https://agent-plugins.org/specification)
package, sharing one skill:

```text
plugins/nemalo/
  .claude-plugin/plugin.json   Claude Code manifest, with a folder option
  .mcp.json                    Claude Code MCP server: --scope from that option
  plugin.json                  Agent Plugins manifest
  mcp.json                     Agent Plugins MCP server: configured folders only
  skills/nemalo/SKILL.md       Agent Skills frontmatter fields only
```

The repository root holds `.claude-plugin/marketplace.json`. In Claude Code:

```text
/plugin marketplace add blisspixel/nemalo
/plugin install nemalo@nemalo
```

Claude Code asks for one library folder at install and passes it as `--scope`;
the configured library and review folders are added. Without an interactive
prompt, supply it with `claude plugin install nemalo@nemalo --config
scope=FOLDER`. A plugin loaded only with `--plugin-dir` has no stored folder, and
Claude Code then omits the Nemalo server without reporting why; see the
[host acceptance](VALIDATION.md#claude-code-host-acceptance). Agent Plugins has no user
configuration, so that package runs `nemalo mcp serve` with Nemalo's configured
folders, and the server refuses to start without one. Both run the installed
`nemalo` executable from PATH. They bundle no binaries, run no installer, fetch
nothing, and leave network catalog tools off.

Manifest versions equal the executable version and change with each release.
Use fixed component paths and keep both MCP files on matching schema versions.
`PLUGIN_ROOT` and `PLUGIN_DATA` are not used; plugin data is not an intake root
or library. The skill explains tools, scopes, cursors, and evidence, and routes
changes to CLI commands the user runs. It is guidance, not permission
enforcement; the server's scope check and read-only tools are.

## Post-1.0 processing and knowledge workspaces

Before 1.0, milestone 9 adds a versioned evidence result and reading guidance to
the existing read-only tools, so the host's own agent explains a work from cited
passages; Nemalo runs no model there
([decision 0048](decisions/0048-cited-evidence-before-1-0.md)).

After 1.0 and all existing roadmap work, an optional harness can help maintain Nemalo's
Markdown knowledge workspace and produce topic-watch insights. OpenCode is one
candidate, not a required host. OpenRouter is a model-provider candidate, not the
harness itself; Nemalo can call a provider directly from Go or expose bounded tools
to a separately configured harness. Neither route integrates Distillr.

Use existing retrieval, durable jobs, and scoped writes. A harness proposes derived
page/index changes with input identities and expected output versions; Nemalo's shared
services validate scope, citations, conflicts, and publication. It cannot rewrite
source assets or human notes. Retrieved Markdown and workspace conventions do not
grant permissions or become trusted agent instructions.

Insight-enabled watches require configured source, acquisition, processing, network,
and spending policies. Check the effective provider even when the harness runs locally.
Expose unavailable checks and unenforceable external cost limits; an agent prompt is
not a budget or sandbox. Keep all processing absent from basic discovery and intake.
These features are outside the 1.0 library/MCP release. See the
[post-1.0 phase](../ROADMAP.md#post-10-topic-watches-and-optional-knowledge-processing)
for staged delivery and acceptance evidence.

## Acceptance

Validate package examples against pinned local copies of the matching schemas;
do not fetch schemas at plugin load time. Test stdio framing, stderr separation,
schema errors, bounded results, protocol compatibility, disconnection, and job
recovery. Prove that read-only mode rejects writes and that stale/unauthorized
cleanup plans fail in both CLI and MCP. Test the package in a real compatible host
on each supported OS before advertising host support.
