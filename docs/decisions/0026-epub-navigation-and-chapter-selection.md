# 0026: EPUB navigation hierarchy and bounded chapter selection

Status: accepted and implemented, 2026-10-12. Follows closed issues 1 and 2.
Spine documents stay the `content units` coordinate system. This does not infer
chapters from headings or word counts.

## Decision

`epub-nav/1` (extractor `nemalo.epub.nav/1`) lists source-declared EPUB 3
navigation documents and EPUB 2 NCX documents, and reads one declared entry
until that entry's declared end.

A navigation document is a manifest item whose `properties` include the `nav`
token. An NCX document has media type `application/x-dtbncx+xml`, or is the
spine `toc` item and does not have the `nav` property. An item that has both
is `nav-doc`. These members are often outside the spine. Reading them does not
change `check` or `Inspect`. Remote hrefs are recorded as `external_not_fetched`
and are not fetched. A member over 2 MiB is `over_budget` and does not fail
inspection. A `toc` id that is missing from the manifest is
`toc_attribute_missing_item`.

Only `epub:type="toc"` in the EPUB namespace `http://www.idpf.org/2007/ops`
is a table of contents. `page-list` and `landmarks` are ignored. More than one
toc nav in one file fails the navigation read with `budget_exceeded`. EPUB 3
level is the `ol`/`li` depth. NCX level is the `navPoint` depth. Neither level
changes `epub-structure/1`: `h1` through `h6` stay part kind `heading`, with no
level field, so existing text continuations keep their representation ids.

Labels are the declared text with Unicode spaces collapsed. A label over 256
bytes fails the whole navigation document; labels are not clipped. Nesting
deeper than 64, more than 1024 entries, or an href over 2048 bytes fails the
same way. Document order is the hierarchy order. `playOrder` is evidence and
does not reorder entries; a value that is not a canonical non-negative integer
fails the navigation read. Duplicate labels stay duplicate.

Each entry resolves a local href to a spine unit and a UTF-8 offset in the
`epub-text/2` text of that unit. An empty fragment is offset 0. A fragment
matches `id:` plus the path-unescaped id. A missing id is `unresolved_anchor`.
An absolute path, backslash, scheme, host, or query is `invalid_reference` or
`external_not_fetched` and is never fetched. A `javascript:` href is active
content: the source is refused with `unsupported_extraction` before navigation
is returned. No script runs.

An entry's range starts at its target and ends at the next entry, in document
order, that is not nested inside it. A parent's range includes its children.
The end is exclusive. Two chapters in one spine file: the first ends at the
second's fragment, `end_of_unit` stays false, and the continuation does not
enter the next chapter. One chapter across files: the end of an intermediate
spine file sets `end_of_unit` and leaves `range_status` at `within_range`; the
continuation stays inside the chapter. A max-bytes cap is `within_range` with
a continuation. Reaching the declared end sets `range_status` to `range_end`
and returns no continuation. `end_of_source` is set only when that end is also
the end of the last spine unit. If the following entry does not resolve, or
its target is not strictly after the start, the read fails with
`unresolved_navigation` and does not continue to the end of the file.

`units` returns the spine units in the `epub-text/2` coordinate system plus
every navigation document. It does not require a source choice. When both a
nav document and an NCX were found, `read` requires `--navigation nav-doc` or
`--navigation ncx` (`navigation_choice_required`), including when their targets
agree. `coverage.navigation_conflict` is set when the resolved levels, parents,
statuses, units, or offsets differ, or when one side has a problem or a
different length. Label-only differences are not a conflict, and the source
flag is still required. Several documents of one kind, including a remote one,
clear that kind's entries and record `multiple_nav_documents` or
`multiple_ncx_documents` when no earlier problem applies.

On a read, `--unit` is the entry's order inside the chosen source, and
`--text-offset` is relative to that entry's start and must stay in the start
unit. The locator's unit and offset are absolute spine coordinates. The
continuation's `part` is `nav-doc:ORDER` or `ncx:ORDER`. A read result carries
the one selected entry and `range_end`. `coverage.text` is
`bounded_selected_range`, or `known_extraction_gaps` when the range contains a
gap. An unsupported document inside the range stops the read; it is not skipped.

The representation id hashes the extractor, the spine units, the text, the
gaps, and the navigation documents. A navigation member that is not a spine
item does not change the `epub-text/2` representation id. A changed table of
contents does change the `epub-nav/1` id, so an old cursor is `stale_reference`.

CLI: `content navigation CATALOG --root DIRECTORY --asset sha256:HASH`.
`content read` with `--representation epub-nav/1`. MCP: read-only
`content_navigation`. TUI Read: `c` lists entries; Enter, `p`, or `s` on a
resolved entry pages that entry and stops at its declared end. `]` and `[`
do not leave the entry.

Full-screen reading also uses `c` to choose declared entries through the same
application service. NAV and NCX remain separate choices, even when their labels
or targets agree; unresolved entries remain visible and cannot open. Selecting
one reuses the same declared-entry reader, including its bounded end. PDF choices
are physical source units and their declared labels, not inferred contents.
The transient chooser pauses playback, invalidates its timer, bounds display to
2048 choices with explicit truncation, and retrieves only on explicit open with
a 15-second deadline. Escape/c returns to the unchanged paused passage. Requests
are cancelled on close/replacement and session/request fences reject late results.
The CLI reader-only exit mode and in-session rate survive a declared selection.
Closing names the explicitly selected navigation source's entry; another source
cannot relabel it. Control and format characters in that label stay visible
escapes in the closing line, without changing source metadata or representation IDs.
No new extractor, parser, network access, model, bookmark write or reading record.

## Evidence

- EPUB 3.3 navigation document: <https://www.w3.org/TR/epub-33/#sec-nav>
- EPUB 3.3 NCX: <https://www.w3.org/TR/epub-33/#sec-ncx>
