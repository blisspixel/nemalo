# Source-bound content access

Implemented EPUB access, 2026-10-08. Tracks
[issue 1](https://github.com/blisspixel/nemalo/issues/1) and
[issue 2](https://github.com/blisspixel/nemalo/issues/2); full cross-format/caller
acceptance remains open. CLI/JSON and TUI use `internal/app.Service.Content` and
`internal/content`. There is no MCP server, PDF extractor, audio-range service,
or Kilo adapter yet. This work does not bypass production import/publication gates.

The existing default `epub-text/1` contract below remains available. Opt into
`epub-structure/1` for source parts, note links, figures, and located gaps;
`epub-source/1` returns bounded original XML. `content capabilities --json` lists
implemented representations. See [structured EPUB access](STRUCTURED-EPUB.md)
for commands, offsets, coverage, resources, and the issue acceptance matrix.

## Usage

Use an existing portable snapshot and an explicit root. Its root hint never
authorizes filesystem access. Get the exact asset ID from `library list`.

```sh
nemalo content units /path/catalog.json --root /path/library --asset sha256:HASH --json
nemalo content read /path/catalog.json --root /path/library --asset sha256:HASH --unit 1 --max-bytes 4096 --json
nemalo content read /path/catalog.json --root /path/library --asset sha256:HASH --cursor TOKEN --json
```

`HASH` and `TOKEN` are placeholders for returned identifiers. Units are zero-based
EPUB spine documents, not inferred chapters or poems. The manifest includes source
package/member paths, spine order, `linear` declarations, extracted byte counts,
and `supported_plain_text` or `unsupported_document`. It does not invent labels
from text. Nonlinear documents remain explicit, in spine order.

An initialized library can be both arguments when they are the same absolute
non-link directory:

```sh
nemalo content read /path/library --root /path/library --asset sha256:HASH --json
```

The asset ID comes from `library import`. This reads that one managed holding
and rechecks its size and SHA-256. It does not search the library, and review
status does not become checked because the text was read. A managed holding may
be read up to the 256 MiB import cap. A different root is refused. Snapshot
catalogs remain files with their own `--root` and keep the 32 MiB source cap.

The first recorded location is selected deterministically and reported. Missing
or changed bytes do not trigger an undisclosed search of duplicate locations.
Every request reads a bounded immutable in-memory copy, checks size and SHA-256,
and checks file identity around the read. Paths are locations, never identity.
Final source symlinks and special files are refused; `os.Root` confines traversal.
Source reads write nothing and never execute content. Only an explicit resource
read returns local embedded bytes; it never fetches links or renders those bytes.

## Version 1 text and references

The shared request requires schema version 1; CLI commands select that version.
The result has its own `schema_version: 1` inside the existing CLI envelope.
The extractor/configuration identifier is `nemalo.epub.text/1`. Changing its
representation rules requires a new identifier, not silent continuation remapping.

The representation is decoded UTF-8 XHTML body text. XML numeric/predefined
entities are supported; external/custom entities are not resolved. Preserve
source whitespace and combining characters without Unicode normalization; change
CRLF/CR to LF. Common block boundaries add a missing LF; each explicit `br` adds
an LF, retaining consecutive breaks. Inline text stays adjacent. This is a
documented plain-text transformation, not original markup bytes or rendered layout.

Locators bind asset ID, extractor, representation SHA-256, unit, and half-open
`[start, end)` UTF-8 byte offsets. The representation hash covers ordered unit
descriptors and extracted text. Offsets are zero-based and must be rune boundaries;
chunks can split a word or grapheme, but never a UTF-8 encoding. `--text-offset`
allows an explicit offset; a continuation cannot be combined with unit/offset.

Continuations are canonical, versioned base64url JSON references carrying the same
identity and next position. They are validated references, not credentials or
permissions. Retrying an identical request returns the same result. An incompatible
asset/extractor/representation yields `stale_reference`; changed source bytes yield
`changed_source`. Neither silently resumes at a guessed nearby passage.

`partial_range` means the text cap stopped inside the selected document.
`complete_range` means its requested remaining span was delivered. `end_of_unit`
and `end_of_source` separately identify the end of that representation's document
and final spine document. They do not prove semantic completeness or that earlier
units were consumed. A cap is never treated as the end of a book.

Image-only, table, embedded media, SVG/MathML, hidden, and other unsupported
documents remain manifest gaps in `unsupported_units`. Reading such a unit fails
explicitly. A continuation stops there instead of skipping ahead; selecting a
later supported unit requires an explicit caller request. End-of-source still
reports gaps. Malformed documents/container failures return no excerpt or cursor.
CSS visibility and typography are not applied, and conformance is not established.
Active/encrypted or otherwise warned containers require review and are refused.
Multiple package renditions are unsupported rather than concatenated as one work.

## Bounds and failures

- Catalog: existing strict 16 MiB snapshot loader; explicit paths at most 4096 bytes.
- Source: nonempty regular asset, at most 32 MiB for a snapshot and 256 MiB for
  a managed holding; exact size/hash required.
- Container: shared assessment parser, 2 MiB ZIP metadata-read budget, 10,000
  members, 32 MiB/member, 128 MiB declared expansion, CRC/length checks.
- Extraction: at most 512 units, 8 MiB total extracted text, UTF-8 XHTML with XML
  structural checks, and bounded tokenizer buffers. No fallback conversion/OCR.
- Response: 4-65,536 text bytes per request (default 4096), at most 1 MiB result
  JSON, cursors at most 2048 bytes, and bounded escaped diagnostics.
- Work: cooperative cancellation and a 15-second service deadline. Bounded parser
  steps and filesystem reads may finish after cancellation; stalled devices are
  not a hard real-time or process sandbox guarantee.

Stable operation statuses include `units_available`, `complete_range`,
`partial_range`, `invalid_request`, `missing_asset`, `changed_source`,
`unsupported_extraction`, `malformed_content`, `budget_exceeded`, `cancelled`,
`stale_reference`, and `unavailable`. Failed operations return nonzero, with no
text, locator, or continuation. Unknown/duplicate cursor fields, future schemas,
invalid encodings, and incompatible references are refused. A truncated JSON
transport response cannot be treated as success by a caller.
`malformed_content` reports failure in the supported parser, not a universal EPUB
conformance verdict; unsupported XML entity/encoding forms can also cause it.

Rights/edition metadata remain unknown where the snapshot lacks them. A managed
EPUB holding can record package identifier evidence; that evidence is not a
retrieval locator and does not replace the asset hash. Retrieval
does not certify safety, grant redistribution rights, trigger a scan, or infer
reading/listening. Future provenance must extend the shared contract deliberately.

## Issue 1 ownership and remaining acceptance

Nemalo owns offline retrieval and evidence. A caller owns voluntary selection,
pause/switch/resume/stop, and explicit acknowledgement of what it actually processed.
Retrieval, retries, peeks, preparation, or delivery never create consumption state.
Optional bookmarks remain separately enabled, local, and exportable future work.

Implemented validation covers over 300 actual synthetic service calls across
multiple works, deterministic retries, source-order continuation, Unicode/stanzas,
serialized/restored references, explicit unsupported gaps, changed/missing assets,
incompatible references, malformed content, limits, cancellation, and CLI JSON.
It does not test caller-owned atomic acknowledgements or certify subjective experience.

Remaining work, without moving post-1.0 analysis ahead of library delivery:

- TUI unit browsing, bounded reading, repeat, and continuation expose this service.
  Future MCP must preserve its representation negotiation and failure facts.
- Milestone 7: physical PDF page index versus printed label, exact quotation spans,
  OCR/gaps, publication version and bibliographic provenance.
- Milestone 8: recording/narration identity, ordered track hashes, declared/probed/
  decoded timing, actual bounded source ranges, explicit gaps, and audio fixtures.
- External Kilo Rust adapter: named bounded tool with explicit roots; no arbitrary
  host shell or private-state mounts. Atomic idempotent acknowledgements, failed/
  truncated transport handling, crash/restart and independent work continuity belong
  there. Validate its actual registered path and voluntary stop/switch/resume before
  claiming integration. Nemalo remains Go-only.

Reviewed primary contracts: [EPUB 3.3 spine](https://www.w3.org/TR/epub-33/#sec-spine-elem),
[Go XML decoder](https://pkg.go.dev/encoding/xml#Decoder), and
[existing HTML tokenizer](https://pkg.go.dev/golang.org/x/net/html#Tokenizer).
