# MCP and Agent Plugins

Research checked 2026-10-08. This is a design, not a shipped server or plugin.
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
nemalo mcp serve --transport stdio
```

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

| Tool | Behavior | Mutation class |
| --- | --- | --- |
| `catalog_search` | Bounded provider queries with source/type/language/access filters | Network read; optional bounded cache writes |
| `catalog_get` | Resolve an edition, publication version, or recording and its assets | Network read; optional bounded cache writes |
| `library_list`, `library_get` | Paginated local metadata and assessment evidence | Read-only |
| `content_get` | Bounded supported content with exact asset ID and location references | Local read; derived indexes separately controlled |
| `intake_inspect` | Inventory configured roots and return a plan | Local read plus bounded staging/state |
| `intake_apply` | Apply an inspected plan under enabled format policies | Library/state write; source retained |
| `acquisition_start` | Acquire selected provider asset IDs into staged intake | Network and library/state write |
| `cleanup_preview` | Show exact recorded dispositions | Read-only |
| `cleanup_apply` | Revalidate and apply an authorized plan digest | Source mutation, recoverable by default |
| `job_get`, `job_cancel` | Inspect/cancel a durable job | State read or cancellation |

Return typed structured results, stable identifiers, per-provider failures,
assessment evidence, and explicit partial/cancelled states. Do not compress
uncertainty into a single `safe` boolean. Avoid dumping whole books or huge search
results into tool responses; provide bounded excerpts and references when needed.

Long operations return a durable job ID. Jobs have bounded queues, progress,
cancellation, and restart reconciliation. Application jobs do not depend on an
MCP connection remaining open. Protocol-specific async capabilities can be added
only after verifying the chosen SDK and host support them.

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

## Agent Plugins package

Proposed package layout:

```text
nemalo-plugin/
  plugin.json
  mcp.json
  skills/
    nemalo/
      SKILL.md
```

An illustrative manifest using the
[canonical plugin schema](https://agent-plugins.org/schemas/1.0.0/plugin.schema.json):

```json
{
  "$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json",
  "name": "nemalo",
  "description": "Inspect downloads, discover publications, and manage a local collection."
}
```

An illustrative MCP configuration using the
[canonical MCP schema](https://agent-plugins.org/schemas/1.0.0/mcp.schema.json):

```json
{
  "$schema": "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json",
  "mcpServers": {
    "nemalo": {
      "type": "stdio",
      "command": "nemalo",
      "args": ["mcp", "serve", "--transport", "stdio"]
    }
  }
}
```

The baseline package requires the native Nemalo binary installed on the host PATH.
It does not run an installer, fetch executables, or require Node/Python. If binaries
are later bundled, use platform-specific packages and plugin-relative commands.
Declare an actual package version when publishing and use the repository's Apache-2.0
license. The illustrative package is not a released MCP integration.

Keep `plugin.json` and root `mcp.json` on matching specification schema versions.
Use fixed component paths; MCP configuration is not inline in the manifest.
Commands are executable tokens with separate arguments. Hosts provide
`PLUGIN_ROOT` and persistent `PLUGIN_DATA`; configured environments cannot override
them or embed secrets. Use plugin data only for integration state, not as an
implicit intake root or user library. These are packaging constraints from the
[specification](https://agent-plugins.org/specification), not executable enforcement.

The future skill explains discovery, format policies, explicit scope, plan/apply
semantics, and evidence interpretation. It routes operations through the tools or
Go CLI rather than supplying scripts that duplicate library logic.

## Post-1.0 processing and knowledge workspaces

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
