# Structured EPUB access

Implemented EPUB scope of [issue 1](https://github.com/blisspixel/nemalo/issues/1)
and [issue 2](https://github.com/blisspixel/nemalo/issues/2), 2026-10-08.
This extends [source-bound access](CONTENT-ACCESS.md), without changing its default
plain-text representation or introducing consumption state.

## Negotiate and retrieve

```sh
nemalo content capabilities --json
nemalo content units /path/catalog.json --root /path/library --asset sha256:HASH --representation epub-structure/1 --json
nemalo content read /path/catalog.json --root /path/library --asset sha256:HASH --representation epub-structure/1 --unit 1 --max-bytes 4096 --json
nemalo content read /path/catalog.json --root /path/library --asset sha256:HASH --representation epub-structure/1 --cursor TOKEN --json
nemalo content source /path/catalog.json --root /path/library --asset sha256:HASH --cursor SOURCE_TOKEN --json
nemalo content resource /path/catalog.json --root /path/library --asset sha256:HASH --cursor IMAGE_TOKEN --max-bytes 4096 --json
```

`HASH` and tokens are returned identifiers, not literal values. Unknown versions
fail with `unsupported_representation`. The unchanged default is `epub-text/1`,
extractor `nemalo.epub.text/1`. Both new representations use
`nemalo.epub.structure/1` and schema version 1. References carry the representation;
using one with another representation fails instead of remapping positions.

All operations use the same explicit root, catalog lookup, immutable parent bytes,
SHA-256 verification, and canonical bounded assessment container parser. No network,
writes, rendering, antivirus, automatic link traversal, or reading records occur.
Remote static references remain unfetched gaps. Active/scripted/encrypted content
and repeated spine references fail. This distinction does not remove assessment
warnings or make static content safe.

## Parts and references

Structured text retains decoded source whitespace, combining characters, explicit
line breaks and stanza separation. Common block boundaries add a missing LF.
It does not apply CSS, normalize typography, or infer chapter/poem boundaries.
Parts identify declared paragraphs, headings, preformatted text, sections, stanzas,
notes, references, figures, and captions, with inherited language/direction or
`unknown`. Source IDs are prefixed `id:`; otherwise IDs are deterministic `part:N`
within the source unit. Duplicate represented source IDs fail.
Unit listings include represented-part and known-gap counts; zero text bytes
does not establish an empty source document or a complete text extraction.

Each part has parent, decoded-text byte range, original-member byte range, and
source-bound locators/references. `reference` retrieves the selected text part;
`source_reference` retrieves its exact XML span. A caller can also select
`--part id:SOURCE_ID --unit N`. Part continuation stops at that part's end.
Descriptors may span beyond the delivered range; they do not claim those bytes
were delivered. Known skipped subtrees are represented by their enclosing gap,
not an invented structure for their descendants.

Local hyperlinks resolve only to spine documents and represented source fragments.
Resolved links provide `link.reference`. Missing fragments, invalid references,
off-spine destinations, and external URLs have explicit statuses and no usable
target reference. Cycles are marked without following them. To follow a note,
request its reference explicitly; retain the originating request/reference to
return. Neither action advances another request or creates reading progress.

## Figures, tables, and mathematics

Figures/captions retain their source relationship. Image descriptors retain source
URL, exact alt text when present, and `alt: null` when absent; empty alt text remains
distinct. Pixels are always a known text gap. No generated description replaces
missing alt text. Eligible local manifest image members expose member path, byte
length, SHA-256, and a resource reference bound to the parent EPUB and part.

Resource reads return bounded base64 bytes and `resource_bytes` offsets. They
revalidate the entire parent and selected member on every request. Remote, missing,
unsupported, oversized, or unrepresentable resource references return explicit
statuses without a usable resource token. Returned media types are source
declarations, not proof of image validity or security. Bytes are never decoded,
rendered, executed, or written by retrieval.

Tables are `table_relationships_not_extracted` gaps, not flattened prose. The
source operation retains rows, cells, header attributes, and row/column spans as
original XML; it does not reconstruct a semantic table grid. MathML is a
`mathematical_layout_not_extracted` gap, with exact source XML available separately.
Image equations remain images. No OCR, equation transcription, or rendered-math
equivalence is claimed. Unsupported namespaces, hidden content, and embedded media
also receive located gaps.

## Coverage, identity, and bounds

`coverage.text` distinguishes a complete selected text range, bounded range, known
extraction gaps, and non-text resources. `known_gaps` identifies returned gap parts;
`referenced_outside_range` includes unresolved links and targets not wholly within
the delivered range. `unknown` records unestablished layout/accessibility/semantic
coverage. These fields describe this response, not the whole work or consumption.
Raw source delivery does not claim omitted XML; gap descriptors still explain
which semantics the structured-text extractor cannot provide.

Text offsets use `utf8_bytes`; original XML uses `member_utf8_bytes`. Source XML
is untrusted data, including potentially unsafe markup. Parent SHA-256 plus extractor
version and representation hash bind every reference. The representation hash covers
ordered units, structured text, parts, and resolved declarations before attaching
derived cursor strings. Source byte ranges and resource hashes are included.
Original XML and resources remain additionally bound to the exact parent bytes.

Existing 32 MiB parent, 512-unit, 8 MiB total-text, 4..65536-byte chunk, 2048-byte
cursor, 1 MiB result, and cooperative 15-second limits remain. Structured access
adds 8 MiB per XHTML member/resource, depth 256, 1024 parts per unit, 16384 total
parts, and 32 MiB encoded representation-hash input. An oversized descriptor result
fails without returning partial text. References are checked against encoded size
including JSON escaping and maximum continuation offset before issuance.

## Acceptance and remaining work

| Requirement | Evidence or remaining boundary |
| --- | --- |
| Exact resumable EPUB delivery | Existing plain-text tests plus structured multi-work retrieval/retry cycles, source-order equality, unchanged source bytes |
| Poetry, language/direction, notes | Unicode/stanza, inherited declarations, bounded note follow/return, missing-fragment and cyclic-link fixtures |
| Figures and local resources | Source alt versus absent alt, caption parent, remote unfetched URL, chunk reassembly and exact member hash |
| Table/math accuracy | Located unavailable semantics; exact table spans and MathML retained through source references |
| Per-range coverage | Partial-target disclosure, bounded parts, terminal zero-width gaps, raw-markup delivery distinguished from text omissions |
| CLI/TUI | Negotiation/source commands; explicit-root staging, unit selection, repeat, retained failed continuation, session switching |
| PDF/OCR and paper citations | Milestone 7; physical page index versus printed label, evidence regions, OCR confidence/gaps, citations/version provenance |
| Audio | Milestone 8; ordered recording/track identity, declared versus decoded timing, actual range boundaries and source-bound related text |
| MCP and external caller | MCP milestone and external adapter; actual registered tool path, transport truncation, restart/acknowledgement and voluntary switching need independent validation |

Neither issue is closed by EPUB delivery alone. No external caller code, paper
parser, audio decoder, reading telemetry, or post-1.0 analysis is introduced.

Primary contracts reviewed: [EPUB XHTML](https://www.w3.org/TR/epub-33/#sec-xhtml),
[EPUB structural semantics](https://www.w3.org/TR/epub-ssv-11/),
[HTML table model](https://html.spec.whatwg.org/multipage/tables.html#the-table-element),
and [Go XML byte offsets](https://pkg.go.dev/encoding/xml#Decoder.InputOffset).
