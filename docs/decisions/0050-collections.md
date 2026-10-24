# 0050: collections are declared selections resolved by holdings

Status: accepted and implemented, 2026-10-19. Stage 1 of the
[order of work to 1.0](../../ROADMAP.md#order-of-work-to-10), the collections
workstream: "declarative intent plus resolved edition/version/recording and
asset records. Repeated acquisition is idempotent; updates show a diff before
changes." Reuses selected acquisition ([decision 0007](0007-selected-acquisition.md))
and whole recordings ([decision 0018](0018-whole-recording-acquisition.md)).

## Context

Acquisition took one selection at a time. A person keeping a set of books,
recordings, and papers had no way to say what the set is, see what the library
already holds, or fetch only what is missing. The validation harness in
`internal/collection` downloads by URL into external intake for testing; it is
not a production path and is not reused for policy.

## Decision

- A collection is a JSON file the person writes: a schema version, a name, and
  at most 1,000 items. Each item is an exact selection: `archive:ITEM` with a
  `file` or a declared `recording` format, `gutenberg:NUMBER` with the EPUB name
  its record gives, or `arxiv:IDvN` with its PDF name, and an optional one-line
  note. Unknown fields, duplicates, paths, and other sources are refused. The
  file is read whole, at most 1 MiB, and its SHA-256 is reported.
- The library's holdings are the resolved record; Nemalo stores nothing else
  for a collection. `collection status COLLECTION LIBRARY` (and the read-only
  MCP tool `collection_status`) resolves each item offline by the exact source
  and file, or recording format, recorded at acquisition: held, held in review,
  recording incomplete (some declared positions held), or not held, with the
  holdings that resolve it. Titles and identifiers are never matched.
  Conflicting assets at one position or conflicting track totals keep a
  recording incomplete; every conflicting asset stays listed for review.
- `collection acquire COLLECTION LIBRARY --output INTAKE` previews the items not
  held and the packet each uses, `INTAKE/<source>-<digest>`, named from the
  selection so reordering the file changes nothing. With `--apply` each runs one
  at a time through the same service as `acquire`, with fresh evaluation,
  checked transfer, scan, and quarantine. A packet with a receipt waits for
  ingest, an interrupted one is resumed as `--resume` would, and any other
  existing packet is reported and left untouched. One item's failure does not
  stop the others; cancellation stops before the next item.
  Before offering resume or import, the packet's recorded source and exact
  file or recording format must match the collection item. The directory name
  is a lookup hint, not selection evidence; mismatched or absent declarations
  keep the packet retained without requests or writes to it.
- `--max-total-bytes` caps the bytes a run may transfer: each item's budget is
  the smaller of its own default and what remains, and no item starts once it is
  spent. The intake must be absolute and outside the library.
- Downloads are untrusted intake. `library ingest LIBRARY INTAKE --apply`
  assesses and imports them, as for any packet.

## Consequences

Running the same collection again fetches only what is still missing, and
status after ingest shows the set resolved. An Open Library edition is not an
item; its Archive item is. Explicit revision comparison is
[decision 0053](0053-collection-revision-previews.md): compare before/after user
files offline without changing the collection or removing holdings.
The TUI view and its separately bound acquisition review are implemented in
source under [decision 0089](0089-collection-acquisition-review.md), with their own
acceptance gate. Collection acquisition over MCP and reacquiring an item whose
source bytes changed remain later work.

## Validation

Offline tests: a collection of three Archive items with one retained attempt
previewed without requests, applied to fetch the other two, the retained one
left in place; after ingest, status showed two held in review and one not held;
a second run fetched only that one, and a third found it awaiting ingest; a
total budget smaller than an item fetched nothing; a relative intake and one
inside the library were refused. A recording was incomplete with one of two
tracks held and held with both. Collection files with unknown fields, both or
neither selection, duplicates, a path, a wrong Gutenberg name, an unversioned
arXiv ID, Gutenberg audio, another source, another schema, no items, and two
JSON values were refused.

An independent review then found, now fixed with tests: a recording already
receipted was resumed and reported as failed; a packet whose receipt records a
detection or review was shown as awaiting ingest; byte-identical tracks made a
recording look incomplete, and positions beyond the declared total were
counted; an interrupted packet that cannot resume (an arXiv transfer, or one
without a strong validator) was offered as a resume; a resume ignored the total
budget, and resumed bytes were counted twice; trailing brackets and repeated
keys in the file were accepted. The acquisition package now reads an existing
packet's state (`InspectPacket`, `InspectRecording`); a resume starts only when
what it may still transfer fits the remaining budget; `--max-recording-bytes`
sets a recording's budget; and tracks declaring different totals are reported.

Live on 2026-10-19: a collection of `gutenberg:84` (`84.epub.noimages`) and
`arxiv:1706.03762v1` previewed two items, acquired 356,586 and 2,128,686 bytes
with ClamAV reporting no detections, ingested both as checked, and then
reported every item held with nothing to acquire.
