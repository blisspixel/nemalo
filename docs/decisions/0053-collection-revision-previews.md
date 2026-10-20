# 0053: compare explicit collection revisions

Status: accepted and implemented, 2026-10-20. Extends
[decision 0050](0050-collections.md) for stage 1 of the
[order of work to 1.0](../../ROADMAP.md#order-of-work-to-10), tracked in
[issue 45](https://github.com/blisspixel/nemalo/issues/45).

## Decision

`collection diff BEFORE AFTER` compares two explicit collection files through
`internal/library` and `internal/app`. CLI JSON and the read-only MCP tool
`collection_diff` return the same `schema_version: 1` result. No initialized
library is needed. Each MCP input must resolve inside a configured scope.

- Both inputs use the existing strict collection parser: a regular file of at
  most 1 MiB and 1,000 exact selections, with duplicate keys, duplicate selections,
  unknown fields, trailing data, and unsupported schema versions refused. Reads
  bind to opened regular files through `safeio`, permit parent directory aliases,
  and check cancellation. A final file link is refused by the service; MCP can
  resolve an explicitly selected link only within its configured scopes.
- A successful result has `complete: true`, `before` and `after` revisions
  (absolute path, SHA-256 of the bytes read, name, item count), `changed`,
  `name_changed`, `added`, `removed`, `notes_changed`, and `retained_order`.
  Positions are one-based. Added entries use after positions; removed entries
  use before positions. Notes name the after item and its before note, with
  both positions. No source lookup or fuzzy matching is performed.
- Selection identity is the complete source/file/recording tuple, excluding
  notes and positions. A new paper version, encoding, or source is an explicit
  removal and addition, not an inferred update of a work or narration. Packet
  directory digests are not comparison keys.
- `retained_order` is empty unless the relative order of retained selections
  changes. When it changes, every retained selection is listed in after order
  with its before and after positions and after item. An insertion or removal
  shifting positions alone does not constitute reordering.
- Arrays keep declaration order: additions and note changes in after order,
  removals in before order. A formatting-only change can alter the hashes
  without making `changed` true. A failed or cancelled comparison returns an
  error with `complete: false` and no partial diff or revision evidence.

## Boundaries

This is a comparison of declared files, not an upstream refresh, rights check,
resolved asset comparison, transfer estimate, or authorization to acquire. The
result describes the bytes read; it does not freeze later edits. Before applying
an updated collection, review this diff and its ordinary acquisition preview.
Acquisition still evaluates each source freshly and acquires only missing exact
selections. Removed selections never remove earlier holdings, notes, or sources.

No additional on-disk state, collection catalog, journal, dependency, network
request, scanner invocation, or write is introduced. Collections remain user
files; holdings remain their resolved record. A TUI collection view stays in
the interfaces stage.

## Validation

Synthetic tests cover additions/removals, explicit paper versions and encoding
changes, renamed collections, added/removed notes, reordered retained items,
position shifts from insertions/removals, formatting-only changes, identical
inputs, stable output, both byte hashes, cancellation, and strict invalid inputs.
A fixed pair of selections sharing a legacy packet-directory digest remains a
removal and addition. Parent aliases work; final links and, on Unix, a FIFO are
refused without blocking. CLI JSON and in-memory MCP agree with the shared
service; MCP checks both scopes and link targets. Terminal output escapes source
text. An application test compares revisions, acquires the new selection,
preserves the earlier asset and explicit bookmark note, then repeats acquisition
without fetching anything again.
