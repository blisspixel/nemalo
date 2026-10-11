# 0021: PDF page index, printed labels, text, and quotation spans

Status: accepted and implemented, 2026-10-11. Completes the PDF parts of
[issue 1](https://github.com/blisspixel/nemalo/issues/1) and
[issue 2](https://github.com/blisspixel/nemalo/issues/2), and starts milestone 7
with a parser decision.

## Dependency

Checked against the repositories and the Go vulnerability database on
2026-10-11:

- `github.com/pdfcpu/pdfcpu` v0.16.1 (Apache-2.0) has strong resource limits but
  loads every object eagerly from an `io.ReadSeeker`, brings 12 modules, had
  seven advisories fixed in the two weeks before this check, and exposes no page
  labels, ToUnicode maps, or text.
- `rsc.io/pdf` is archived and affected by GO-2026-5781; `ledongthuc/pdf` is
  named in GO-2026-6115; `unidoc/unipdf` is commercially licensed; a PDFium
  binding would embed a non-Go engine.
- `github.com/digitorus/pdf` v0.3.0 (BSD-3-Clause, no dependencies, released
  2026-09-21) reads objects lazily through an `io.ReaderAt`, supports standard
  encryption, recovers its own panics, and caps object-stream nesting.

Nemalo uses `digitorus/pdf` for cross-references, objects, and stream decoding
only, inside `internal/pdftext`, which adds what the parser lacks: a guard that
follows the cross-reference chain from the final `startxref` through `/Prev` and
`/XRefStm` with PDF tokenization and refuses any subsection, `/Index` range, or
`/Size` beyond half the file size or one million before the parser sizes its
table (a 200-byte file otherwise allocated 256 MiB); a read wrapper that checks
cancellation and bounds total bytes read to 64 times the file plus 64 MiB; a
limit on every stream Nemalo decodes (object and cross-reference streams decoded
inside the parser are bounded only by that read budget and the deadline); and
its own `recover`. The parser's page, font, and text
helpers are not used. The license is retained in release notices.

## Decision

`internal/pdftext` (extractor `nemalo.pdf.text/1`) walks the page tree
iteratively with a visited set and does not trust `/Count`; expands the
`/PageLabels` number tree (decimal, Roman, and letter styles, prefixes, and
starts); and interprets content streams in order: text showing, positioning for
line and word breaks, form XObjects within a depth limit and without re-entry,
and inline images. Fonts map codes through ToUnicode CMaps (ranges kept as
ranges), predefined UCS-2 CMaps, WinAnsiEncoding, and Differences glyph names;
StandardEncoding and MacRoman map ASCII only. Anything else is a located
`unmapped_text` gap naming the font and reason, never a guess.

- The content service exposes `pdf-text/1`: one unit per page, where the unit
  index is the zero-based physical page index and `page_label` is the printed
  label the document declares. Reads, continuations, stale references, and the
  once-only gap disclosure rule are the EPUB ones. Gaps locate images, unmapped
  codes, forms not read, undecodable content, and pages with images and no text
  (`no_text_layer`, which may be scanned). There is no OCR, and coverage states
  that column reading order and layout are not established.
- `content locate` (MCP `content_locate`) finds exact, case-sensitive
  occurrences of a quotation in `pdf-text/1` or `epub-text/2`, returning unit
  (page index and label for PDF, member for EPUB), byte offsets in the same
  representation as reads, and a cursor that reads the quotation back. At most
  100 matches; more are reported as truncated.
- `check` and import assessment walk a PDF's page tree and labels without
  reading page content and record the page count and first and last labels. A
  tree that cannot be walked or was walked only partly (a repeated, cyclic, or
  too deep node), an encrypted tree, or one over budget is a warning that keeps
  the file in review. The parser does not repair broken cross-reference tables,
  so such a PDF is in review too. A file with only header and end markers no
  longer passes limited checks.
- TUI Read stages PDF holdings as `pdf-text/1` and lists pages by index and
  label.

## Consequences

Papers and PDF books can be read, cited, and quoted by exact page and offset
through CLI, JSON, TUI, and MCP. Remaining milestone 7 work: active content,
attachments, and external references assessment; OCR as a separately labelled
derivation; column reconstruction; and the paper providers and citation export.

## Validation

Synthetic tests cover labels in every style, unlabelled documents, WinAnsi and
octal escapes, TJ word spacing, Tm and Td line breaks, Flate streams, ToUnicode
bfchar and bfrange (both forms, including a surrogate pair), a UCS-2 CMap, an
Identity-H font without a map, Differences with ligature and `uniXXXX` names, a
Type3 font, image and inline image gaps, scanned pages, self-referencing forms,
a cyclic page tree, operator, stream, page, and gap budgets, cancellation,
malformed files, the high object number guard, and the content, CLI, MCP, and
TUI paths. An independent review found ten defects before release, each now
with a regression test: guard bypasses through delimiters, comments, name
escapes, long `/Index` arrays, and `/Prev`; quadratic line breaks; slow
ToUnicode lookups (lookups now count against the operator budget and check
cancellation); inline fonts sharing a cache entry; gap offsets inside a
character after trimming; `TJ` replaying an earlier array; unbounded and
unreported page tree findings; matches and units leaking from failed or
truncated locate results; and pages over 8 MiB, which are now a budget error so
every continuation stays valid. A fuzz target ran 98 million executions over ten minutes without failure. Live: two arXiv
papers (1706.03762 and the two-column 1512.03385) produced readable text in
stream order with ligatures preserved and TeX symbol-font codes reported as
located gaps; "residual learning" was located 7 times on 5 pages and each cursor read it back exactly; `check` recorded 15 pages for 1706.03762. Measuring
TeX output showed justified word spaces near -190 thousandths of an em, so the
word-space threshold is 0.12 em.
