# Source-bound content access

Implemented EPUB access, 2026-10-08; PDF and recording access, 2026-10-11. This
contract came from [issue 1](https://github.com/blisspixel/nemalo/issues/1) and
[issue 2](https://github.com/blisspixel/nemalo/issues/2), closed 2026-10-11 with
the remaining OCR, column, narrator, and M4B work moved to milestones 7 and 8. CLI/JSON and TUI use `internal/app.Service.Content` and
`internal/content`; the read-only MCP `content_*` tools call the same service
([decision 0014](decisions/0014-read-only-mcp-server.md)). MP3 tracks have a
frame index and decodable ranges (`mpeg-audio/1`, below), M4B and M4A files
list their sample tables and chapters (`mp4-audio/1`) or return explicit original
packet selections (`mp4-audio/2`), PDFs have pages,
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

The terminal reader's `/` dialog adapts the same exact `content locate` operation.
It searches the whole book's supported extracted text, not rendered layout or an
inferred semantic passage, and opens a chosen result only through its returned
source-bound cursor. Terminal input is one line; the CLI retains the full existing
literal UTF-8 contract. Finding does not normalize case, Unicode or whitespace to
manufacture a match. See the [terminal controls](TUI.md).

The asset ID comes from `library import`. This reads that one managed holding
and rechecks its size and SHA-256. It does not search the library, and review
status does not become checked because the text was read. A managed holding may
be read up to its format's import limit: 256 MiB, or 2 GiB for an M4B or M4A,
whose `mp4-audio/1` reads at offsets and then hashes the whole file. A different root is refused. Snapshot
catalogs remain files with their own `--root` and keep the 32 MiB source cap.

The first recorded location is selected deterministically and reported. Missing
or changed bytes do not trigger an undisclosed search of duplicate locations.
Every request reads a bounded immutable in-memory copy, checks size and SHA-256,
and checks file identity around the read. Paths are locations, never identity.
Final source symlinks and special files are refused; `os.Root` confines traversal.
Source reads write nothing and never execute content. Only an explicit resource
read returns local embedded bytes; it never fetches links or renders those bytes.

## Evidence and citation export

Managed `content read` supports optional `--evidence` through the shared
application service. Ordinary reads retain their existing JSON; only opted-in
results add `evidence`, schema `nemalo-evidence/1`. See the
[selected semantic review](CITED-EVIDENCE-REVIEW.md) and the exact hosted
acceptance ledger in [issue 39](https://github.com/blisspixel/nemalo/issues/39).
Implementation and stage acceptance are distinct from a release.

```sh
nemalo content read /path/library --root /path/library --asset sha256:HASH --unit 1 --max-bytes 4096 --evidence --json
nemalo content read /path/library --root /path/library --asset sha256:HASH --unit 1 --max-bytes 4096 --evidence-reference REFERENCE --json
nemalo library citation /path/library --asset sha256:HASH --json
```

`REFERENCE` is the earlier result's `evidence.reference`, not its continuation.
Repeat the original `evidence.selection` explicitly, including representation,
range selector or original cursor, and max-bytes. A different requested bound
fails even when it would produce identical bytes. The token contains no paths,
selectors or decoder authority. It compares asset/library, the full selected
holding and portable metadata digests, extractor, representation ID and a
versioned digest of the actual result plus selection. It is not a signature or
authorization credential. Unsupported or ambiguous selections can return their
existing diagnostics without a quotation or evidence reference.

Evidence retains exact source `quotation` and its locator, or delivered audio
SHA-256/byte count and the original typed range. EPUB text offsets and PDF pages,
MP3 encoded samples, MP4 packet ordinals and decoder-output samples remain
different coordinate systems. PCM evidence retains the full-output identity in
the enclosing result; audio is not a transcript. The original result continues
to carry structured parts, printed page labels, declared navigation/page ends,
initialization evidence, clocks, gaps and stop facts. Evidence coverage names only
the returned range's unit, not every inventoried unit or the whole work.
An end-of-source marker does not prove earlier units were read.

Only an initialized managed library can supply evidence metadata. Recorded work,
edition/version, source and recording relationships are declarations, with their
origins, omissions, conflicts and user narration match/withdrawal states retained.
They are not fresh rights or safety checks. Metadata reads hold a shared reader
lock through retrieval and refuse torn journal tails without repairing them.
The portable metadata view omits private intake paths and transfer URLs and is
at most 256 KiB after safe JSON encoding; oversized views are refused instead of
dropping identities. The full safely encoded result stays within 1 MiB. A
five-minute outer deadline includes metadata work; existing shorter retrieval or
caller deadlines still apply. Nothing initializes or mutates the library or
records reading. Any metadata, retrieval, reference, encoding or close failure
discards all quotation, resource bytes, evidence and continuation.

MCP uses optional `evidence` and `evidence_reference` only on `content_read` and
explicitly enabled `content_decode_read`. Scopes and existing per-call decoder
permission still apply. CLI decoded evidence still requires `--decode`; a
reference never selects a decoder by itself. Units, locate, raw source, resources
and snapshot reads retain their earlier contracts. The TUI requests evidence
only through its explicit F4 report and replays it with F3 or `r`
([decision 0090](decisions/0090-terminal-cited-evidence.md)).

`library citation` and scoped read-only MCP `library_citation` export schema
`nemalo-citation/1`: a standard CSL-JSON item array in `csl_json`, separately from
Nemalo's `metadata`, field provenance/candidates, `metadata_sha256`, unknown fields
and limitations. Pass only that array to a CSL processor. Exact asset IDs identify
items; only unambiguous recorded titles, languages and classified ISBN/DOI values
are projected. Author, date, publisher, edition label and bibliographic type stay
unknown; `document` is an explicit generic fallback. A narrator is not an author,
and a different paper version supplies no missing metadata or license. Citation
export reads committed journals only, works when stored content is missing and
marks bytes `not_revalidated`. It never opens arbitrary receipt paths or contacts
providers. See [decision 0087](decisions/0087-source-bound-evidence-and-citations.md)
for the exact reference encoding and pinned CSL schema.

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
line feeds. Each `epub-text/2`, `epub-nav/1`, and `epub-structure/1` range lists the line feeds
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
restrictions (encryption beyond font obfuscation, DRM licenses, scripted items,
active content, repeated spine references; see
[decision 0036](decisions/0036-epub-font-obfuscation.md)) refuse the whole source; other assessment warnings do not.
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

## Declared page labels: `epub-page-list/1`

```sh
nemalo content pages CATALOG --root DIRECTORY --asset sha256:HASH --json
nemalo content read CATALOG --root DIRECTORY --asset sha256:HASH --representation epub-page-list/1 --label iv --json
```

An EPUB 3 `page-list` is listed with each declared label, its exact target in
`epub-text/2` coordinates, its status, and a reference bound to the page
list's identity, plus the package's stated pagination source or `unknown`. A
read starts at one marker by reference, exact label, or order; a label that
several markers carry returns `ambiguous_label` with each candidate. The text
and continuation are `epub-text/2`'s, and `page_end` is the next marker only
when it resolves after this one. Page labels are the EPUB's own markers, not
screen pages or a print-edition match, and none are generated. The table of
contents and its ids are unchanged
([decision 0031](decisions/0031-epub-page-list.md)).

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
The result also lists `recording-bytes/1` identities of complete declared
recordings with identical ordered assets across source items. Identity uses
journal hashes, independently of probing and narration declarations
([decision 0052](decisions/0052-recording-byte-identities.md)).

## M4B and M4A audio: `mp4-audio/1`

```sh
nemalo content units LIBRARY --root LIBRARY --asset sha256:HASH --representation mp4-audio/1 --json
```

`units` returns one unit and an `mp4` summary from the movie's sample tables:
codec, sample rate, channels, sample and chunk counts, sample bytes, encoded
duration (the sum of sample durations) beside the media and movie headers'
declared durations, whether an edit list exists, item text (title, artist,
album, narrator), language, chapters with start and end in milliseconds, and
their source (`quicktime_chapter_track` or `nero_chpl`). Findings and notes are
those of `check`. Nothing is decoded, and edit lists and encoder priming are
not applied. MP4 samples cannot be played apart from their container, so
there is no `read`: `library open` or `library export` gives a player a
verified copy. See [decision 0039](decisions/0039-mp4-audio-samples.md).

## Original MP4 packets: `mp4-audio/2`

```sh
nemalo content units LIBRARY --root LIBRARY --asset sha256:HASH --representation mp4-audio/2 --json
nemalo content read LIBRARY --root LIBRARY --asset sha256:HASH --representation mp4-audio/2 --packet 0 --max-bytes 65536 --json
```

This explicit representation reuses the checked packet index and extractor
`nemalo.mp4.packets/1` ([decision 0083](decisions/0083-source-bound-mp4-packet-evidence.md),
[decision 0085](decisions/0085-verified-mp4-packet-access.md)). It requires one
clear audio track, one sample description and no format findings. The earlier
`mp4-audio/1` and all earlier continuations keep their behavior.

`mp4_packets.initialization` contains at most 16 KiB of the exact original audio
sample entry, its `stsd` version, separate SHA-256 and base64 data. This untrusted
declaration is not a playable MP4 header or verified decoder configuration.
`resource` contains concatenated original whole packets, separately hashed and
base64 encoded; its `total_bytes` is the delivered payload length. Neither hash
replaces the whole asset identity. `mp4_packets.packets` gives each packet's
original file byte span and canonical decimal-string `decode_start`/`decode_end`
ticks at the declared `timescale`. Decimal strings preserve large values exactly.
These are media decode ticks, not decoded PCM samples or edited player times.
Decoder preroll and PCM equivalence remain `not_established`; presentation mapping
is `not_applied`. No decoder, player, scanner, network call or consumption record
runs in this operation.

The locator's `offset_unit` is `mp4_packets`, with zero-based, half-open ordinals
in unit 0. A read defaults to packet 0, or explicitly selects `--packet N`.
Even an explicit zero is rejected on another representation, units listing or
cursor request. Continue using the exact returned cursor, without a packet
selector; the cursor keeps its representation when `--representation` is omitted.
It binds the asset, extractor, initialization, representation and next ordinal.
At most 128 whole packets and 64 KiB of packet data are returned. A byte/packet
stop remains `partial_range`, distinct from `end_of_track`. An empty next packet
is a reported gap and is never skipped; an initial empty packet or a first packet
larger than the requested allowance fails without usable data or continuation.
An explicit later selection establishes no coverage of earlier packets.

Each call copies and hashes the exact opened source into owned private scratch
before parsing or packet reads. Metadata and payload use that retained verified
handle, with one shared 80 MiB read allowance. The original is revalidated before
success. Scratch is removed on success and failure; library state and selected
sources stay unchanged. Preparation may need 2 GiB temporary disk and up to 4 GiB
of original-source reads, even for a small response. Space refusal is
`budget_exceeded`. Only this representation has a five-minute deadline; shorter
caller cancellation wins. Small packet responses do not imply cheap preparation.

For ordinary playback, `library open` or `library export` gives a player a verified
whole-file copy. Original packet access does not establish decoded timing or
playable packet ranges. Separately authorized decoded access follows below;
full stage acceptance remains [issue 60](https://github.com/blisspixel/nemalo/issues/60).

## Explicit decoded MP4 access: `mp4-pcm/1`

```sh
nemalo content units LIBRARY --root LIBRARY --asset sha256:HASH --representation mp4-pcm/1 --decode --json
nemalo content read LIBRARY --root LIBRARY --asset sha256:HASH --representation mp4-pcm/1 --decode --pcm-sample 0 --max-bytes 65536 --json
```

This optional representation uses `nemalo.ffmpeg.pcm/1` and installed
FFmpeg/FFprobe, never downloaded or required. Every invocation, including units
and continuation, supplies `--decode`. Default representations, TUI, import and
acquisition never decode. MCP registers separate `content_decode_units` and
`content_decode_read` only with `--allow-decode`; ordinary content tools reject
decoder access even when these tools are enabled. Write or network permission
grants no decoder permission
([decision 0086](decisions/0086-explicit-decoded-mp4-access.md)).

The checked MP4 index requires one clear audio track, one complete bounded sample
description and no format findings. The source is copied and hashed before use.
Metadata and tools use that owned private copy; after decoding, Go rehashes its
retained handle and checks its filename identity. The original is also revalidated
before success. No selected source, holding, bookmark or job state changes.
Owned scratch is removed on every return.

Each call decodes the complete source sequentially from its beginning to mono
signed 16-bit little-endian output at the selected probed stream rate. This
preserves the full decoder state required by the measured AAC noise-substitution
case. It counts/hashes all output while retaining only the selected interval;
no full waveform file/allocation is created. Downmix, quantization, selected rate
and FFmpeg trimming/resampling are part of this derived representation. Its
sample timeline is not an independently established original-packet, chapter,
edit-list or audible player-position mapping.

`mp4_pcm` reports tool version, format/rate/channels, separately reported source
rate/channels and exact declared/encoded tick strings, full decoded sample count
and `full_pcm_sha256`. Header ticks remain declarations, including possible
unknown-duration sentinels. Output rate/count describe actual output, never a
duration ratio inferred from the container.

A read selects `--pcm-sample N`, a zero-based output sample distinct from MP3
encoded samples and MP4 packets. Even an explicit zero is rejected on another
representation, units or cursor request. `mp4_pcm.range` binds source, extractor
and representation to half-open `start_sample`/`end_sample` decimal strings in
`decoder_output_samples`. Legacy locators are not reinterpreted. `resource` is a
complete small mono PCM WAV, with a whole-WAV hash separate from
`range_pcm_sha256` and the complete output hash. A client can explicitly save
and play it outside the library. Nemalo launches no player.

Continue using the exact `mp4pcm1:` cursor, retaining `--decode` and omitting the
sample selector. The cursor keeps its representation when that option is omitted.
It binds source, extractor, tool version, transform/rate/count, whole PCM hash and
next sample. A changed whole-output hash, including outside the selected slice,
refuses a stale continuation. A late decoder error/failed exit, partial sample,
timeout, cancellation, work cap or changed identity discards every collected byte
and reference. Missing tools are `unavailable`; no alternate representation is
silently substituted. Selecting the actual end or beyond fails without an empty
playable file.

Preparation may need 2 GiB private disk, 4 GiB original-source reads, 2 GiB private
revalidation and 8 GiB streamed PCM even for a small result. Metadata retains its
shared 80 MiB allowance. At most three children use argument arrays, fixed
mov/file-only input, explicitly disabled external data references/absolute paths
and one decoder/filter thread. Version/probe/decode share 64 KiB of diagnostics;
failing observed chunks are charged and cancel work. PCM has a separate streamed
allowance. Responses are at most 64 KiB including the 44-byte WAV header, with
whole 16-bit samples; reads need at least 46 bytes. The service ceiling is five
minutes, with the shared decoder's size bound and shorter caller cancellation
still applying. Pipe/in-flight overshoot is bounded work, not an OS sandbox.
Available input is bounded; aggregate child memory and cumulative internal file
reads are not directly measured. Rights, safety, semantic completeness and
consumption remain unknown. See the
[validation audit](VALIDATION.md#explicit-decoded-mp4-access).

## Bounds and failures

- Catalog: existing strict 16 MiB snapshot loader; explicit paths at most 4096 bytes.
- Source: nonempty regular asset, at most 32 MiB for a snapshot and 256 MiB for
  a managed holding, or 2 GiB for `mp4-audio/1`, `mp4-audio/2` and `mp4-pcm/1`; exact
  size/hash required, and checked after an offset read.
- Container: shared assessment parser, 2 MiB ZIP metadata-read budget, 10,000
  members, 32 MiB/member, 128 MiB declared expansion, CRC/length checks.
- Extraction: at most 512 units, 8 MiB total extracted text, UTF-8 XHTML with XML
  structural checks, and bounded tokenizer buffers. No fallback conversion/OCR.
- Response: 4-65,536 text bytes per request (default 4096), at most 1 MiB result
  JSON, cursors at most 2048 bytes, and bounded escaped diagnostics.
- Work: cooperative cancellation and a 15-second service deadline, or five
  minutes for explicit `mp4-audio/2` preparation or `mp4-pcm/1`. Bounded parser
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
  MCP preserves its representation negotiation and failure facts.
- Milestone 7: done for PDF access: `pdf-text/1` gives the zero-based physical
  page index and the printed page label separately, text in content-stream order
  with located gaps (images, unmapped font codes, pages without a text layer),
  and `content locate` binds exact quotations to page, offsets, extractor, and
  representation ([decision 0021](decisions/0021-pdf-pages-and-text.md)). OCR,
  column reconstruction, and bibliographic provenance remain later work.
- Milestone 8: done for MP3 recordings: ordered track hashes, declared versus
  probed timing, bounded source ranges with preroll, explicit gaps, and
  completeness ([decision 0020](decisions/0020-recording-manifest.md)), with
  LibriVox narrators ([decision 0035](decisions/0035-librivox-readers.md)).
  M4B and M4A list sample tables and chapters as `mp4-audio/1`
  ([decision 0039](decisions/0039-mp4-audio-samples.md)); explicit `mp4-audio/2`
  adds bounded original packets and sample-entry evidence. Explicit `mp4-pcm/1`
  adds full-output-hash-bound decoded samples and small playable WAV ranges.
  Stage 3 acceptance closed issue 60 with its
  [receipt](https://github.com/blisspixel/nemalo/issues/60#issuecomment-6374231710).
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
