# 0103: list columns, local filter, continuous paging and mouse

Status: recorded before implementation, 2026-10-27. Third increment of the TUI
design tracker [issue 65](https://github.com/blisspixel/nemalo/issues/65):
[issue 70](https://github.com/blisspixel/nemalo/issues/70). It builds on
decisions [0101](0101-task-sections-and-action-registry.md) and
[0102](0102-home-overview-and-detail-cards.md) and adds no service, format,
module or dependency.

## Context

Result rows were a number, a clipped title and a pipe-joined summary such as
`1.3 MiB | en | 2 location(s)`. Narrowing Holdings meant editing a field and
pressing Enter, paging was Ctrl+Left and Ctrl+Right only, the position read
`Selected 1 of 3 on this page | 3 matches, offset 0`, and there was no mouse.
The boundary to keep: no network request without an explicit key that names it.

## Decision

1. **Columns.** Each row has a numbered title clipped at a whole word with
   `...`, a right-aligned column (size, or format and size, or a job's recorded
   status) and a dim second line of languages, copies and status words. Rows
   carry no pipes. The panel title shows the absolute position, `12 of 340`.
2. **Filter.** With results focused, `/` opens a filter field above the list.
   Each keystroke narrows the loaded page to items whose title or summary
   contains the text, ignoring case, and accents the match. It reads nothing and
   requests nothing. Enter keeps the filter so the list keys act on the filtered
   selection; `/` edits it again; Escape restores the full page and the
   selection it had. New results or a step change drop the filter. The real
   cursor follows the field. The catalog-level Filter field is unchanged.
3. **Paging.** Down on the last item or Up on the first continues to the next or
   previous page. Holdings and a local Gutenberg catalog load it at once, since
   they read local files. A network provider (Open Library, Internet Archive,
   Open Library editions) shows a "Load the next page (network)" row instead,
   and only Enter on that row requests it; any other key withdraws it. Edited
   fields still need Enter before paging. Ctrl+Left and Ctrl+Right are unchanged.
4. **Mouse.** The panel layout enables cell-motion mouse reporting. A click
   selects a result row and focuses the results, or opens a section or step in
   the sidebar or header; the wheel moves the selection over the list and
   scrolls the report or details elsewhere; a second click on the same row
   within half a second is Enter. A click on the network row only selects it.
   Reviews, the palette, the filter field and the reader ignore the mouse. Plain
   mode does not enable mouse reporting, so the terminal keeps its own text
   selection; most terminals also select text with Shift held. The keyboard
   remains complete.

## Verification

Model tests cover narrowing on each keystroke without a request, Enter keeping
and Escape restoring the list and selection, the filter dropped across steps,
local pages loading past the end, network pages waiting for Enter on their own
row and withdrawn by other keys, absolute positions, clicks and the wheel in
sync with the keyboard, a double click as Enter, section clicks, plain mode
without mouse reporting, aligned columns at 80 and 120 columns and titles
clipped at words. The registry parity test covers `/`. Mouse behaviour in real
terminals (Ghostty, Kitty, WezTerm, Windows Terminal) is not established by
these synthetic events and is recorded separately when observed.
