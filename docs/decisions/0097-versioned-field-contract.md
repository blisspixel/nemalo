# 0097: an enforced field contract for records and results

Status: accepted, 2026-10-24. Second stage 6 increment, applying decision 0096
to every field. Test infrastructure only; no runtime behavior changes.

## Requirement

Scripts, agents and older libraries depend on JSON field names, types and
`omitempty` options. Nothing stopped a later change from renaming or removing a
field of a persisted record or a CLI/MCP result within the same schema version.

## Decision

`internal/contract` holds a test that parses every non-test Go file under
`internal` with the standard library parser. Its roots are every struct with a
`schema_version` field (45 today, on disk and in results) and an explicit list
of user-facing structures without one: configuration, file checks, inventory,
discovery pages and evaluations, doctor, library status, audits, export,
bookmarks, jobs and the MCP envelope and capabilities. It expands nested,
embedded and cross-package struct fields and writes one line per field path
with its Go type and JSON options to `internal/contract/testdata/fields.txt`.

Within a schema version, fields are only added. A removed, renamed or retyped
field fails with a message that it needs a new schema version and a decision,
not a snapshot update. An added field also fails until it is reviewed and the
snapshot is regenerated with `go test ./internal/contract -update`, so every
addition is deliberate and visible in review. A second test requires known
nested and cross-package fields to be present, so an empty or truncated
snapshot cannot pass.

A deliberate mutation renaming `Holding.titles` was reported as a removal at
all three places the field appears, nested in import and ingest results, and as
one addition. The contract covers field names, types and options, not values,
ordering or the meaning of a field; those stay with behavior tests.
