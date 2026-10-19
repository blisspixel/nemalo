# 0034: MCP import tools bound to a previewed plan

Status: accepted and implemented, 2026-10-19. The first MCP write tools, for
milestone 6, over [decision 0014](0014-read-only-mcp-server.md) and the import
of [decision 0009](0009-managed-epub-import.md).

## Decision

`nemalo mcp serve --allow-write` registers two tools. Without the flag the
server is unchanged: read-only tools, `read_only: true` in capabilities.

- `library_import_preview` assesses one EPUB, PDF, MP3, M4B, or M4A file, or a completed
  intake packet, for an initialized library and writes nothing to it. With
  `scan`, the installed antivirus scans the file. It returns the status the
  import would record and a plan digest.
- `library_import_apply` stores that plan: a journaled, verified copy in the
  library. It requires the 64-character plan, and refuses, writing nothing,
  when the current source, its assessment, the scan choice, or the library no
  longer give it. A refusal withholds the new plan, so the caller previews
  again rather than applying a plan nobody was shown.

The plan is a SHA-256 over the library ID, the absolute source path, the source
kind, the asset ID and size, the format, the status and reason the assessment
gives, and the scan choice. The service layer computes and checks it, so the
CLI shares it: `library import` previews show `Plan:` and suggest an apply with
`--plan`, and `--apply --plan DIGEST` refuses a changed source the same way.
The digest binds an apply to what was shown; it is not authorization. The
person's decision to start the server with `--allow-write` and with these
scopes is the authorization, and the server instructions tell the agent to ask
before applying.

Both library and source must resolve inside the configured scopes. Import
copies: no tool changes, moves, or deletes a source, initializes a library,
ingests a folder, cleans up, acquires, or rescans. Apply is annotated as not
read-only, not destructive, and idempotent, because a repeated apply records
the holding already held. Annotations describe; the scope check and the plan
check enforce. Retrieved text cannot change either. The bundled plugins keep
starting the server read-only.

Import finishes within its 10-minute bound, so it runs within one call; durable
jobs remain for longer work such as acquisition and ingest of a folder.

## Validation

A library test previews twice with an identical plan, refuses a forged plan, a
different scan choice, and a source changed after the preview, each without an
operation line, then applies a fresh plan. An MCP test finds no import tools
without Write, checks the apply annotations and `read_only: false`, refuses an
apply with no plan, a forged plan, or a source outside the scopes without
writing, refuses a preview that carries a plan, then previews, applies, and
finds one holding and an unchanged source.
