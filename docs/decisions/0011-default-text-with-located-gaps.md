# 0011: default EPUB text locates omissions instead of refusing documents

Status: accepted and implemented, 2026-10-11. Resolves
[issue 5](https://github.com/blisspixel/nemalo/issues/5).

## Problem

`epub-text/1` refuses a whole reading-order document when its body contains any
`img`, `table`, `svg`, `math`, `audio`, `video`, or similar element. It also
refuses the whole source when assessment reports any warning, including missing
package language metadata. Illustrated Project Gutenberg EPUBs place images in
almost every chapter, so most of their prose was unreachable through the default
`content read`, even though `epub-structure/1` already extracts that prose and
locates each omitted element.

## Decision

Add `epub-text/2` and make it the default representation for `content units`
and `content read`. It is not a second extractor. It runs the shared structured
extractor (`nemalo.epub.structure/1` rules) and returns plain text plus located
gap descriptors instead of parts:

- Images, tables, MathML, SVG, embedded media, unsupported namespaces, and
  hidden content are omitted from the text. Each omission is a `gaps` entry with
  its element, reason, zero-width UTF-8 `offset` in the unit text, and the
  `epub-structure/1` part ID that locates the same element there. Image gaps keep
  source `alt` exactly: absent is `null`, distinct from an empty string. No
  description is generated.
- Tables are not flattened into prose. Row and column relationships are not
  established by this extractor, and flattened cell text with uncertain order
  would read as complete source text. Exact table markup stays available through
  `epub-source/1`.
- A document the extractor cannot represent (for example an SVG spine item) is
  an explicit `unsupported_document` unit. Other units stay readable. A
  continuation still stops at that unit; skipping it requires an explicit request.
- Like the structured representations, only container access restrictions
  (encryption declarations, scripted items, active content, repeated spine
  references) refuse the whole source. Other assessment warnings no longer do.
- Each gap is disclosed once across consecutive ranges: a zero-width gap belongs
  to the range that starts at its offset, or to the final range of its unit.

The text is byte-identical to `epub-structure/1` text, so offsets agree across
the two. The representation hash covers units, texts, and gap descriptors under
extractor `nemalo.epub.text/2`, so its references never validate against another
representation.

## Compatibility

`epub-text/1` is unchanged and remains selectable with
`--representation epub-text/1`. A continuation always keeps the representation
it was issued for: an existing `epub-text/1` cursor continues under
`nemalo.epub.text/1` without any flag, and a mismatched explicit representation
is a `stale_reference`, never a remapped position. `content capabilities`
reports `default_representation`. Callers that need the original behavior name
it explicitly; callers that read the default now receive more text, with each
omission located rather than hidden.

Human `content read` output now shows escaped text with bracketed display
markers such as `[image: The old sea-dog]` or `[table omitted]` at the located
offsets. Markers are presentation only. JSON `text` never contains them.

## Validation

Synthetic fixtures cover an illustrated chapter with captioned, alt-less, and
empty-alt images and a layout table; equality with structured text and part IDs;
chunked reads that disclose each gap exactly once; image-only units; per-unit
unsupported documents and stopped continuations; active-content refusal;
missing package metadata; default and continuation routing; and stale references
after a gap change. Live validation against the Gutenberg EPUBs named in the
issue is recorded in the changelog for the release that ships this change.
