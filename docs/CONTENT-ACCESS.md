# Source-bound content access

Implemented EPUB access, 2026-10-08; PDF and recording access, 2026-10-11. This
contract came from [issue 1](https://github.com/blisspixel/nemalo/issues/1) and
[issue 2](https://github.com/blisspixel/nemalo/issues/2), closed 2026-10-11 with
the remaining OCR, column, narrator, and M4B work moved to milestones 7 and 8. CLI/JSON and TUI use `internal/app.Service.Content` and
`internal/content`; the read-only MCP `content_*` tools call the same service
([decision 0014](decisions/0014-read-only-mcp-server.md)). MP3 tracks have a
frame index and decodable ranges (`mpeg-audio/1`, below), PDFs have pages,
printed labels, text, and located gaps (`pdf-text/1`,
[decision 0021](decisions/0021-pdf-pages-and-text.md)), exact quotations can be
located in EPUB or PDF text (`content locate`), and held recordings
have an ordered manifest across tracks (`recording-manifest/1`,
[decision 0020](decisions/0020-recording-manifest.md)). There is no Kilo adapter. This work does not bypass production import/publication gates.

The default representation is `epub-text/2`
([decision 0011](decisions/0011-default-text-with-located-gaps.md)): plain text
in which images, tables, mathematics, embedded media, and hidden content are
omitted and each omission is located as a gap. The original `epub-text/1`
contract below is unchanged and selectable with `--representation epub-text/1`.
A continuation always keeps the representation it was issued for. Opt into
`epub-structure/1` for source parts, note links, figures, and located gaps;
`epub-source/1` returns bounded original XML. `content capabilities --json` lists
implemented representations and the default. See
[structured EPUB access](STRUCTURED-EPUB.md) for commands, offsets, coverage,
resources, and the issue acceptance matrix.

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
known gap counts, and `supported_plain_text`, `supported_text_with_gaps`, or
`unsupported_document`. It does not invent labels from text. Nonlinear documents
remain explicit, in spine order. Human output shows escaped text with bracketed
display markers, such as `[image: alt text]` or `[table omitted]`, at each located
gap, and ends with the `--cursor` value when a continuation exists. Markers are
presentation only; JSON `text` never contains them.

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

In `epub-text/1`, image-only, table, embedded media, SVG/MathML, hidden, and other
unsupported documents remain manifest gaps in `unsupported_units`. Reading such a
unit fails explicitly. A continuation stops there instead of skipping ahead;
selecting a later supported unit requires an explicit caller request.
End-of-source still reports gaps. Malformed documents/container failures return
no excerpt or cursor. CSS visibility and typography are not applied, and
conformance is not established. Active/encrypted or otherwise warned containers
require review and are refused. Multiple package renditions are unsupported
rather than concatenated as one work.

## Version 2 text: the default

`epub-text/2` uses extractor `nemalo.epub.text/2`. Its text is byte-identical to
`epub-structure/1` text: the same parser, whitespace, line, and stanza rules.
Instead of refusing a document that contains an image, table, MathML, SVG, media,
unsupported namespace, or hidden content, it omits that element and reports a
gap in `gaps` with the element name, the reason (for example
`image_pixels_not_text` or `table_relationships_not_extracted`), the zero-width
UTF-8 `offset` in the unit text, and the `epub-structure/1` part ID of the same
element. Image gaps keep the source `alt` exactly; `null` means none was
declared, which is distinct from an empty string, and no description is
generated. Table cell text is not flattened into prose, because its row and
column relationships are not established; `epub-source/1` returns the exact
table markup. Each gap is reported once across consecutive ranges: a zero-width
gap belongs to the range that starts at its offset, or to the final range of its
unit. `coverage.text` is `known_extraction_gaps` when a range contains one.

Source whitespace stays in the text, so a hard-wrapped paragraph keeps its
line feeds. Each `epub-text/2` and `epub-nav/1` range lists the line feeds
that come from structure in `breaks`, each once, with its UTF-8 `offset` and
a `kind`: `block` ends a paragraph, heading, list item, or other block, and
`line` is an explicit `br` or a line of preformatted text. A `br` that ends
its block is that block's end. Any other line feed is XHTML whitespace and
reads as a space. Breaks describe layout over unchanged text: they move no
offset and are not part of the representation identity, so existing
continuations and bookmarks stay valid.

A document the extractor cannot represent, such as an SVG reading-order item,
is an `unsupported_document` unit in `unsupported_units`; other units stay
readable and continuations stop there as in version 1. Only container access
restrictions (encryption declarations, scripted items, active content, repeated
spine references) refuse the whole source; other assessment warnings do not.
The representation hash covers units, text, and gap descriptors, so a gap
change invalidates earlier references. On six illustrated Project Gutenberg
EPUBs from issue 5, version 1 read 28 of 54 units and version 2 read all 54,
locating 84 omissions.

## Declared contents: `epub-nav/1`

```sh
nemalo content navigation CATALOG --root DIRECTORY --asset sha256:HASH --json
nemalo content read CATALOG --root DIRECTORY --asset sha256:HASH --representation epub-nav/1 --unit 0 --json
nemalo content read CATALOG --root DIRECTORY --asset sha256:HASH --representation epub-nav/1 --unit 0 --navigation ncx --json
```

`units` lists spine documents, in the `epub-text/2` coordinate system, and every
EPUB 3 navigation document (`nav-doc`) and EPUB 2 NCX (`ncx`). A spine document
is a reading-order file. Several declared entries can share one file, and one
entry can span files. Labels are the declared text. Level is the `ol`/`li` or
`navPoint` depth, not an `h1` through `h6` level. `epub-structure/1` part kinds
are unchanged. See
[decision 0026](decisions/0026-epub-navigation-and-chapter-selection.md).

`read` takes `--unit` as the entry's order in the chosen source. `--text-offset`
is relative to that entry's start. The entry ends at the next entry that is not
nested inside it. `range_status` is `within_range` or `range_end`. `end_of_unit`
remains the spine fact. A cap, and the end of an intermediate spine file, leave
the range open and return a continuation that stays inside the entry. An
unresolved following entry fails with `unresolved_navigation` and does not
continue to the end of the file. When both sources were found, the read names
`--navigation nav-doc` or `--navigation ncx`. Listing does not require that
choice. `coverage.navigation` is `absent`, `nav-doc`, `ncx`, or `nav-doc+ncx`.
`navigation_conflict` means the resolved hierarchy or targets differ. Labels
alone do not set it.

Remote hrefs stay `external_not_fetched`. A `javascript:` link refuses the
source as active content. Nothing is fetched or executed. A navigation member
that is not in the spine does not change the `epub-text/2` representation id.
It does change the `epub-nav/1` id.

## MP3 tracks: `mpeg-audio/1`

```sh
nemalo content units LIBRARY --root LIBRARY --asset sha256:HASH --representation mpeg-audio/1 --json
nemalo content read LIBRARY --root LIBRARY --asset sha256:HASH --representation mpeg-audio/1 --sample 441000 --max-bytes 65536 --json
```

`units` returns one unit, the track: format, sample rate, channels, indexed
frames, encoded samples and duration from frame headers, and, kept separate,
what tags declare (info-frame counts, encoder delay and padding, ID3 title,
`TLEN`, and `CHAP` chapters in milliseconds). Tag regions and unindexed byte
regions are listed; nothing is skipped silently. Nothing is decoded.

`read` returns whole frames from the frame containing encoded sample N, as
base64 file bytes, with `audio_range` and a locator in `encoded_samples`
(per channel, zero-based, half-open) and a continuation to the next frame. The
bytes start with preroll frames so a Layer III decoder has its bit reservoir and
history; discard their output. A range never spans an unindexed region. The
byte and sample bounds also let a local player read the file directly. With
preroll, ranges from synthesized and LibriVox files decoded bit-exactly against
a whole-file FFmpeg decode. See
[decision 0015](decisions/0015-mpeg-audio-frame-index.md) for the preroll rule,
bounds, and evidence. One read covers one track. `library recordings LIBRARY
--source archive:ITEM` (MCP `library_recordings`) gives the declared track order,
exact assets, missing positions, conflicts, and probed per-track timing and
offsets across a recording ([decision 0020](decisions/0020-recording-manifest.md)).

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
`stale_reference`, `unavailable`, `navigation_choice_required`, `no_navigation`,
and `unresolved_navigation`. Failed operations return nonzero, with no
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
Explicit bookmarks are a separate opt-in journal,
[decision 0027](decisions/0027-explicit-local-bookmarks.md): `bookmark enable`
creates it, and only save, note, delete, and import write it. There is no
acknowledgement. A changed source is `changed_source` and is not remapped.
The terminal reader ([decision 0025](decisions/0025-terminal-reading.md)) is
such a caller: it requests ranges and continuations like any other and keeps
its position only on screen. Closing reports the unit and byte offset and does
not save a bookmark. MCP does not read or write bookmarks.

Implemented validation covers over 300 actual synthetic service calls across
multiple works, deterministic retries, source-order continuation, Unicode/stanzas,
serialized/restored references, explicit unsupported gaps, changed/missing assets,
incompatible references, malformed content, limits, cancellation, and CLI JSON.
It does not test caller-owned atomic acknowledgements or certify subjective experience.

Remaining work, without moving post-1.0 analysis ahead of library delivery:

- TUI unit browsing, bounded reading, repeat, and continuation expose this service.
  Future MCP must preserve its representation negotiation and failure facts.
- Milestone 7: done for PDF access: `pdf-text/1` gives the zero-based physical
  page index and the printed page label separately, text in content-stream order
  with located gaps (images, unmapped font codes, pages without a text layer),
  and `content locate` binds exact quotations to page, offsets, extractor, and
  representation ([decision 0021](decisions/0021-pdf-pages-and-text.md)). OCR,
  column reconstruction, and bibliographic provenance remain later work.
- Milestone 8: done for MP3 recordings: ordered track hashes, declared versus
  probed timing, bounded source ranges with preroll, explicit gaps, and
  completeness ([decision 0020](decisions/0020-recording-manifest.md)). Narrator
  identity, M4B, and decoded timing remain later work.
- External caller: Claude Code on Windows, through `nemalo mcp serve`, read,
  switched works, and resumed from a continuation at the exact stopping byte
  ([decision 0014](decisions/0014-read-only-mcp-server.md)). Other hosts and
  platforms remain untested.
- External Kilo Rust adapter: named bounded tool with explicit roots; no arbitrary
  host shell or private-state mounts. Atomic idempotent acknowledgements, failed/
  truncated transport handling, crash/restart and independent work continuity belong
  there. Validate its actual registered path and voluntary stop/switch/resume before
  claiming integration. Nemalo remains Go-only.

Reviewed primary contracts: [EPUB 3.3 spine](https://www.w3.org/TR/epub-33/#sec-spine-elem),
[Go XML decoder](https://pkg.go.dev/encoding/xml#Decoder), and
[existing HTML tokenizer](https://pkg.go.dev/golang.org/x/net/html#Tokenizer).
