# 0031: EPUB page-list labels

Status: accepted and implemented, 2026-10-18. Issue 22. Follows decision 0026,
which limits `epub-nav/1` to the table of contents.

## Decision

`epub-page-list/1` (extractor `nemalo.epub.page-list/1`) lists the `page-list`
of an EPUB 3 navigation document: the book's own page markers, such as `iv` or
`42`. A marker is an assertion of this exact EPUB. It is not a terminal screen
page, a spine unit, a chapter number, or evidence that the text matches any
print edition or another download. Nothing is generated from character counts,
headings, or screenfuls.

`content pages` (MCP `content_pages`) lists every page list of the reading
package's navigation documents. Each has its path, whether the `nav` element is
`hidden` (common for page lists and still the source's declaration), and its
entries in document order. An entry keeps the declared label with whitespace
collapsed, the href, its order, and a status. A resolved entry has its spine
unit, its UTF-8 offset in `epub-text/2` text, the target part and member, and a
`reference`. A missing fragment is `unresolved_anchor` with no unit. Remote,
absolute, or scheme hrefs keep the statuses of decision 0026 and are never
fetched. Roman numerals, words, and duplicate labels stay as declared. Two page
lists in one document are `multiple_page_lists`, and two navigation documents
are `multiple_nav_documents`; neither list is chosen. A book without a page list
is `no_page_list`. NCX `pageList` is not read. Limits are those of `epub-nav/1`:
1024 entries, 256-byte labels, and 2048-byte hrefs fail the read.

The package's pagination source is reported with provenance. `declared` lists
`dc:source` values refined as `source-of` `pagination` and any `pageBreakSource`
(`a11y:pageBreakSource`) property; otherwise it is `unknown`. Nemalo does not
check a declaration against an edition.

A read starts at one marker: by its `reference` (`--cursor`), by exact `--label`
when one marker carries it, or by `--unit` as its order. A label that several
markers carry returns status `ambiguous_label` with every candidate and its
reference, and no text. The text, locator, gaps, breaks, and continuation are
`epub-text/2`'s from that position, in its identity, so the continuation reads
on through the book. `page` is the selected marker. `page_end` is the next
marker only when it resolves strictly after this one; otherwise no end is
claimed and coverage says so. A page range is never inferred.

The representation id hashes the extractor, the `epub-text/2` id, the page
lists, and the pagination source. A reference is bound to it: a changed page
list, changed text, or a moved marker is `stale_reference`, never remapped.
The page list is not part of `epub-nav/1`, so adding or changing one does not
change any table-of-contents id or cursor.

`g` in TUI Read lists the labels. Enter reads from one by its reference; `p`
and `s` open the reader at its exact position, paused for `s`. A resize changes
the layout, never the label or the location. Listing, reading, and paging
record nothing.

## Evidence

[EPUB 3.3, section 7.4.3](https://www.w3.org/TR/epub-33/) defines the page list
as navigation to static page boundaries, distinct from the table of contents and
optional. [EPUB Accessibility 1.1, section 3.4.1](https://www.w3.org/TR/epub-a11y-11/)
explains its use in mixed print and digital reading and why its pagination
source matters. Both support listing declared markers, not generating
pagination or claiming conformance.

## Validation

Content tests cover labels `iv` and `1` in one file, an empty page-break
marker, a cross-file target, a duplicate label and its candidates, a missing
fragment, a backward next marker that claims no end, a hidden list, two lists,
an absent list, more than 1024 entries, declared and unknown pagination
sources, a stale reference after the list changed, and an unchanged
`epub-nav/1` identity with and without a page list. MCP returns the same result
as the shared service. TUI tests list, read, continue, and page from a marker,
keep its location across a resize, and change no file. Representation ids of
`epub-text/2`, `epub-nav/1`, and `epub-structure/1` match 0.1.0-alpha.11 on a
real Project Gutenberg EPUB.
