# Formats and compatibility

This is the consolidated list of everything Nemalo writes to disk or exchanges
with other programs, with its version and how it is read. It is the contract the
1.0 release freezes ([roadmap stage 6](../ROADMAP.md#order-of-work-to-10)). Until
then it describes current behavior and may still change by addition. The policy
is [decision 0096](decisions/0096-on-disk-compatibility-policy.md).

## Compatibility policy

- Every Nemalo-written record or file carries a version; current ones are all 1
  unless listed otherwise. A reader accepts only versions it knows and refuses
  others without changing bytes.
- Formats change by addition. A newer Nemalo opens older libraries in place,
  without migration: a library written by the `0.1.0-alpha.2` release is a
  committed test fixture that every build must open, verify and keep using.
- An older Nemalo refuses newer control files and record versions. From this
  release on, that refusal says a newer Nemalo may have written them; other
  malformed state keeps the plain review refusal.
- Identifiers below never change meaning. Changed rules get a new identifier,
  and earlier ones stay selectable with valid continuations.

## Library folder

| Path | Contents | Version | Read |
| --- | --- | --- | --- |
| `.nemalo/writer.lock` | Empty file held with an OS lock by the one writer | none | must exist; never remove it to release a writer |
| `.nemalo/journal.jsonl` | Two initialization records: library identity | 1 | strict ([0006](decisions/0006-library-control-state.md)) |
| `.nemalo/operations.jsonl` | Import and rescan intent/completed records | 1 | strict ([0009](decisions/0009-managed-epub-import.md)) |
| `.nemalo/holdings.jsonl` | Resolved holdings: hash, format, status, sources | 1 | strict |
| `.nemalo/cleanup.jsonl` | Cleanup, restore and purge records | 1 | strict ([0022](decisions/0022-recoverable-source-cleanup.md)) |
| `.nemalo/bookmarks.jsonl` | Explicit bookmark events; exists only once enabled | 1 | strict ([0027](decisions/0027-explicit-local-bookmarks.md)) |
| `.nemalo/narrations.jsonl` | User narration declarations and withdrawals | 1 | strict ([0055](decisions/0055-user-narration-declarations.md)) |
| `assets/sha256/<2 hex>/<hash>.<format>` | Stored bytes, named by SHA-256 | layout | verified by `library check` |
| `cleanup/<hex>/files/<path>` | Held source copies from a cleanup | layout | bound to the manifest |
| `cleanup/<hex>/manifest.json` | Cleanup entries and plan digest | 1 | canonical, version checked, hash bound to its journal record |
| `cleanup/<hex>/purge-<n>.json` | Purge plan; version 2 adds archive groups | 1 or 2 | canonical; 1 must have no groups, 2 must have them |

Strict means: canonical JSON lines only, no unknown or duplicate fields, exact
sequence and identity checks, and refusal with the library left for review. Any
other entry in `.nemalo` is refused. Temporary names beginning `.nemalo-` or
`nemalo-` are private working files.

## Intake packets and other files

| File | Contents | Version | Read |
| --- | --- | --- | --- |
| Packet `intent.json` | The exact acquisition request | 1 | other versions are not resumed |
| Packet `transfer.json` | Validator and progress for byte resume | 1 | other versions are not resumed |
| Packet `receipt.json` | Completed transfer, checks and scan | 1 | above 1 refused as possibly newer; missing refused as not Nemalo intake |
| Packet `content.<format>` | The downloaded bytes | none | hashed against the receipt |
| `recording-intent.json`, `recording.json` | Whole-recording intent and result | 1 | other versions are retained, not imported |
| Quarantine `<UTC>-<hash>/<name>.quarantined` | A detected download, never executed | none | not read by Nemalo |
| `config.json` | `schema_version` (optional), `library`, `review`, `gutenberg_catalog`, `on_detection`, `covers` | 1 (absent means 1) | higher refused as possibly newer; unknown fields refused; written only by the user |
| Catalog snapshot (user-named) | Byte identities of an explicit root | 1 | strict ([0004](decisions/0004-portable-library-snapshots.md)) |
| Collection file (user-written) | Exact selections | 1 | strict ([0050](decisions/0050-collections.md)) |
| Bookmark export | `kind` `nemalo-bookmarks/1` | 1 | unknown fields refused |
| Narration declaration input | `schema` `narration-declaration/1` | 1 | strict |
| Job journals `<state>/jobs/<id>.jsonl` with `.lock`, `.cancel` | Durable MCP jobs | 1 | strict for writer and passive snapshot; a torn final line is tolerated ([0045](decisions/0045-durable-mcp-jobs.md), [0098](decisions/0098-settle-remaining-format-items.md)) |
| Job library context | `nemalo-job-library/1` | 1 | jobs without it are omitted from snapshots ([0088](decisions/0088-non-mutating-library-job-snapshot.md)) |

Intake reads take only the fields they need, so the version, not unknown-field
refusal, gates compatibility there.

## Envelopes

- CLI `--json`: `{"schema_version":1,"command":...,"data":...,"error":...}`.
- MCP tools: `{"schema_version":1,"tool":...,"data":...,"error":...}`
  ([0014](decisions/0014-read-only-mcp-server.md)). Capabilities report the
  executable version, which plugin manifests repeat.
- Result payloads inside `data` carry their own `schema_version` 1. Within a
  version their fields are only added; the field contract test in
  `internal/contract` fails on any removed, renamed or retyped field
  ([decision 0097](decisions/0097-versioned-field-contract.md)).

## Identifiers

| Kind | Identifiers |
| --- | --- |
| Representations and extractors | `epub-text/1` (`nemalo.epub.text/1`), `epub-text/2` default (`nemalo.epub.text/2`), `epub-structure/1` and `epub-source/1` (`nemalo.epub.structure/1`), `epub-nav/1`, `epub-page-list/1`, `pdf-text/1`, `mpeg-audio/1` (`nemalo.mpeg.frames/1`), `mp4-audio/1` (`nemalo.mp4.samples/2`), `mp4-audio/2` (`nemalo.mp4.packets/1`), `mp4-pcm/1` (`nemalo.ffmpeg.pcm/1`). See [content access](CONTENT-ACCESS.md). |
| Continuations | Content cursor (base64url JSON, version 1; no `representation` means `epub-text/1`), `mp4pcm1:` decoded-audio cursor, `nemaloevidence1:` evidence reference |
| Result schemas | `nemalo-evidence/1`, `nemalo-citation/1`, `holding-metadata/1`, `recording-manifest/1`, `recording-bytes/1`, `nemalo-job-snapshot/1`, `user_declaration/1` |
| Plan digests | `nemalo-import-plan/2`, `nemalo-purge-plan/1` and `/2`, `nemalo-archive-leaves/1`, `narration-plan/1`, `collection-acquire/1`; the cleanup plan digest has no tag and its construction is frozen, because manifests record it |
| Cleanup proofs | `archive-tree/1`, `/2`, `/3`, `rar5-frames/1`, `rar4-frames/1`, `rar5-set/1`, `rar4-set/1`, `rar5-old-set/1`, `rar4-old-set/1`, `zip-stream/1`, `zip-segments/1`. Unknown proofs retain bytes. See [architecture](ARCHITECTURE.md). |
| Acceptance reports | `nemalo-native-check/1`, `nemalo-terminal-check/1`, `nemalo-installed-tools-check/1` (tooling only) |
| State IDs | `library:`, `operation:`, `cleanup:`, `bookmark:`, `job:` followed by 32 hex digits |
| Selected PMC sources | `pmc:PMCnumber.N` binds the explicitly selected distributed version; the file is `PMCnumber.N.pdf`. Existing receipt and holding source fields carry these values. Version objects can change, so the source ID is not byte identity. No new record schema or collection selection is introduced ([0100](decisions/0100-pmc-selected-article-versions.md)). |

## Freeze status

No item is left unsettled ([decision 0098](decisions/0098-settle-remaining-format-items.md)).
The 1.0 release candidate declares this document frozen; until then it may
change only by addition under the policy above.
