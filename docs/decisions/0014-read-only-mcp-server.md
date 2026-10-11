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
| `library_list` | Holdings from a catalog snapshot |
| `library_audit` | Managed holdings audit, or snapshot audit with an explicit root |
| `file_check` | Bounded file checks without antivirus |
| `content_capabilities`, `content_units`, `content_read`, `content_source`, `content_resource` | Source-bound EPUB retrieval, including the default `epub-text/2` gaps and structured references |
| `catalog_search`, `catalog_evaluate` | Provider search and Archive evaluation, registered only with `--allow-network` |

No tool imports, initializes, acquires, rescans, snapshots, scans, or deletes.
Write tools need the plan, authorization, and durable job design in the agent
integration document and are a later increment.

## Scope

Tool paths must be absolute and must resolve, through any links, to a configured
scope or below it. Scopes are the repeatable `--scope` directories plus the
configured `library` and `review` folders. A server with no scope refuses to
start unless `--allow-network` is set. The check runs when a tool is called; the
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
negotiated protocol 2026-07-28, and read a managed Project Gutenberg EPUB. No
specific agent host is claimed as supported until it is tested on each platform.
