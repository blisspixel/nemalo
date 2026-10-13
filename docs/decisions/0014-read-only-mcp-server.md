# 0014: read-only MCP server over stdio

Status: accepted and implemented, 2026-10-11. First increment of milestone 6.
Exposes the shared retrieval contract from
[issue 1](https://github.com/blisspixel/nemalo/issues/1) and
[issue 2](https://github.com/blisspixel/nemalo/issues/2) to agent hosts, as
designed in [agent integration](../AGENT-INTEGRATION.md).

## Decision

`nemalo mcp serve --scope DIRECTORY` runs a Model Context Protocol server on
stdio using the official Go SDK, `github.com/modelcontextprotocol/go-sdk`
v1.8.0, released 2026-09-14 and supporting protocol revision 2026-07-28. The
package `internal/mcpserver` is an adapter: each tool calls the same
`internal/app` service as the CLI and returns the CLI's data inside a versioned
envelope, `{schema_version, tool, data, error}`. A failed operation is a tool
error whose structured content still carries the service status, such as
`stale_reference` or `invalid_request`.

All tools are read-only and annotated as such:

| Tool | Service |
| --- | --- |
| `nemalo_capabilities` | Version, scopes, network setting, tools, content representations, limits |
| `library_status` | Library control identity and journal state |
| `library_list` | Holdings from a catalog snapshot or a managed library |
| `library_recordings` | Held recording manifests, and verified track timing when a source is named |
| `library_audit` | Managed holdings audit, or snapshot audit with an explicit root |
| `library_check` | Read-only whole-library report |
| `file_check` | Bounded file checks without antivirus |
| `content_capabilities`, `content_units`, `content_navigation`, `content_read`, `content_locate`, `content_source`, `content_resource` | Source-bound retrieval, including `epub-text/2` gaps, `epub-nav/1` declared contents, PDF pages, and structured references |
| `catalog_search`, `catalog_evaluate` | Provider search and Archive evaluation, registered only with `--allow-network` |

No tool imports, initializes, acquires, rescans, snapshots, scans, deletes, or
writes a bookmark. Local CLI and TUI bookmarks do not authorize an MCP write.
Write tools need the plan, authorization, and durable job design in the agent
integration document and are a later increment.

## Scope

Tool paths must be absolute and must resolve, through any links, to a configured
scope or below it. A path is first checked lexically, after cleaning, against
the scopes as given and as resolved; a path outside them is refused before any
filesystem access. This matters on Windows, where resolving a UNC path would
open an SMB connection and could expose credentials to the remote host. Only an
inside path is then resolved, and the resolved path must also be inside a
resolved scope. Missing and outside paths return the same error, so the server
does not reveal whether a path elsewhere exists. Scopes are the repeatable
`--scope` directories plus the configured `library` and `review` folders; a
configured folder that does not exist yet is reported on stderr and skipped. A
server with no scope refuses to start unless `--allow-network` is set. A link
inside a scope that points to a network location is still resolved. The check runs when a tool is called; the
services then open paths through their own capability-bound checks. This is a
policy boundary over the CLI's services, not an operating-system sandbox. The
deprecated MCP roots feature is not used; paths come from configuration and
tool parameters, as protocol revision 2026-07-28 recommends.

## Transport and limits

Stdout carries only newline-delimited JSON-RPC; diagnostics go to stderr. At
most four tool calls run at once, each with a ten-minute deadline in addition to
the service's own bounds (content results stay at most 1 MiB). End of input stops
the server with exit status 0. Streamable HTTP is not offered; it needs its own
authentication and threat review.

## Dependency

The SDK links `github.com/google/jsonschema-go` (MIT), `github.com/segmentio/encoding`
and `github.com/segmentio/asm` (MIT), `github.com/yosida95/uritemplate/v3`
(BSD-3-Clause), `golang.org/x/oauth2` and `golang.org/x/time` (BSD-3-Clause). The
SDK itself is moving from MIT to Apache-2.0. No existing pinned version changed,
builds stay cgo-free for every release target, and release notices are collected
from the module cache as for other modules. Hand-rolling JSON-RPC, version
negotiation, and schema validation would carry more risk than this dependency.

## Validation

In-memory protocol tests drive every tool with the official client: read-only
annotations, network tools absent by default, results equal to the shared service,
status-carrying tool errors, scope refusal for relative, missing, parent-escaping,
and link-escaping paths, and no library writes or scanner calls. A CLI test runs
`mcp serve` over pipes and requires every stdout line to be JSON-RPC. A live check
launched the built binary as a stdio subprocess with the official SDK client,
negotiated protocol 2026-07-28, and read a managed Project Gutenberg EPUB.

Real host, 2026-10-11: Claude Code 2.1.296 on Windows, in headless mode with a
project MCP configuration launching `nemalo mcp serve --scope LIBRARY` and only
`content_read` allowed, read 300 bytes of Treasure Island, switched to Alice's
Adventures in Wonderland, then resumed Treasure Island with the saved
continuation. The resumed locator started at byte 300 of the same unit, the end
of the first read, with no skip or replay. This shows one host on one platform;
other hosts and platforms are not claimed until tested. Acknowledging what was
actually read, and recovering from a truncated transport, remain the client's
responsibility.
