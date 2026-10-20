# 0054: import proposals bind provenance and recovery

Status: accepted and implemented, 2026-10-20. Corrects
[decision 0034](0034-mcp-import-tools.md)'s preview contract and extends the
managed import of [decision 0009](0009-managed-epub-import.md), tracked in
[issue 46](https://github.com/blisspixel/nemalo/issues/46).

## Context

Changing a packet's declared source after preview could leave its token unchanged
and import the changed declaration. Preview also assessed the incoming file alone,
while apply could retain an existing holding's status and merge its provenance.
A crash intent retained byte and disposition facts, but no metadata binding.

## Decision

- Preview and apply build the same validated proposal in `internal/library`.
  The result exposes `source_evidence`, the normalized incoming source declaration,
  and `holding`, the full resulting record including retained sources and status.
  A no-op requires that full record to match, including extracted titles and
  languages; a changed proposal is journaled even when its bytes already exist.
  Retained scan evidence remains historical; preview does not report a new scan
  merely because the library already holds checked bytes.
- The `nemalo-import-plan/2` token binds library identity, source as named (the
  archive-member label for staging), scan choice, and SHA-256 values of both the
  normalized source evidence and canonical proposed holding. Each hash uses
  canonical struct JSON without a line feed. Metadata deliberately
  omitted by the source normalizer has no effect. A changed proposal refuses an
  old token and withholds its replacement. Earlier transient tokens need a fresh
  preview. The token remains a comparison, not write authorization.
- Preview reads current managed state under the reader lock. A pending rescan's
  recorded disposition is projected without writing, matching the writer's
  required roll-forward before its own work. A stale import refusal starts no
  import write; completing an earlier rescan remains separate recovery work.
  Results distinguish `apply_requested`, `applied`, and `recovered_rescan`, so
  an interrupted apply is not described as a preview that wrote nothing.
- New import intent/completed pairs carry `import_source_sha256` and
  `import_holding_sha256` together. Both must be valid hashes, are forbidden on
  rescan records, and remain identical across the pair. Recovery verifies fresh
  source evidence and the reconstructed holding against them before touching
  stored bytes or journals. No full duplicate holding or receipt is added to an
  operation record. Existing record and journal budgets stay unchanged.
- The holding hash also checks a current holding against the latest bound import
  operation; a later rescan retains its existing disposition-only contract.
  Canonical validation, intent/completed sequencing, OS locks, source preservation,
  byte verification, and retained failures remain required.

## Compatibility and recovery

Completed older operation pairs remain readable without rewriting. New optional
hash fields are omitted on legacy records; partial or malformed bindings fail
closed. Older executables refuse records carrying the new fields.

An unfinished older import intent cannot establish which metadata was originally
reviewed. This version retains it and refuses to finish it. Finish that operation
with the earlier executable and the original source before upgrading, or retain
the library for explicit review if the original evidence is unavailable. No intent
is discarded, reinterpreted, or silently rebound to a current receipt. Initialization
still has exactly two records; no new control file or dependency is introduced.

CLI, JSON, TUI, and MCP continue using the same application import service.
MCP still needs its configured scope, `--allow-write`, and the preview token.
Source declarations do not establish rights, file validity, safety, or authority.

## Verification

Synthetic library regressions change source/file identity, URL, rights, licenses,
recording position/count/readers, narration, and declared identifiers between
preview and apply. Refusals leave the full library tree unchanged and withhold
new tokens. Fresh apply stores the exact proposed record; repeat apply is a
no-op. Omitted raw fields leave the plan unchanged. Incoming and retained source
records remain distinct, including a later scan detection and older receipt.

Interruption tests cover intent, partial copy, published link, and holding append.
Changed evidence is refused with or without the previous token; restored evidence
resumes the original proposal. Legacy completed and pending state, malformed or
partial binding pairs, changed holdings, stable archive labels, pending rescan
projection/recovery, and changed extracted metadata are covered. CLI and MCP
regressions use the shared service and verify refusal before new import writes;
human output tests cover all source fields and terminal escaping.

`go run ./cmd/verify`, including the enforced coverage gate and govulncheck,
passes locally with 85.2% aggregate coverage. `CGO_ENABLED=1 go test -race ./...`
also passes with the native C compiler. Hosted native results are recorded in
the issue closeout after the actual run.
