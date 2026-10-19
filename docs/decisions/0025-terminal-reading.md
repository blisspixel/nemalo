# 0025: terminal pages and speed reading

Status: accepted and implemented, 2026-10-12. Adds ways for a person to read
held EPUB and PDF text in the terminal, over the existing content service.

## Decision

`nemalo read CATALOG|LIBRARY --root DIRECTORY --asset sha256:HASH` and `p` or
`s` in TUI Read open a full-screen reader over `epub-text/2` or `pdf-text/1`.
`internal/reader` lays out delivered ranges; it reads no files and stores
nothing. Closing prints the unit and byte and does not save a bookmark.
An explicit save is the library journal in
[decision 0027](0027-explicit-local-bookmarks.md). The reader requests the same bounded ranges and continuations as
`content read`, 64 KiB at a time, ahead of the reader, within 4 MiB a session
(more than a very long novel; at most about 40 MiB of memory). At that limit,
`]` continues in a new session from exactly where the session stopped: a word
cut off by the last range, and any omission located at it, belong to the new
session, so nothing is lost or repeated.

- **Pages** fill the terminal at a fixed measure: 66 columns for EPUB prose,
  100 for PDF text, adjustable from 24 to 120 and never wider than the
  terminal. EPUB blocks are separated by a blank row, at the block ends a
  range's `breaks` mark; a source line feed inside a block, as in a
  hard-wrapped Project Gutenberg paragraph, flows as a space. An explicit
  line break keeps its line, and a wrapped row after one hangs, as verse
  does; two in a row leave a blank row. PDF text keeps the
  extractor's lines; no paragraphs are inferred, and a wrapped source line
  continues with a hanging indent. Every omitted image, table, or unmapped run
  is shown where it was omitted, with its alternative text when the source
  declares one. Each unit opens with a heading: a reading-order document, or a
  physical PDF page with its printed label.
- **Speed reading** shows one word at a time with a fixed focus column between
  fixed guide marks. It starts paused, at 250 words per minute nominal, and
  changes by 25 between 100 and 1000. A sentence end holds twice the base time,
  a clause 1.5 times, a block end 2.5 times, and a word over eight characters
  1.3 times; omissions and headings hold at least 750 ms. The end of an EPUB
  line break, such as a line of verse, holds as a clause. Above 350 the footer
  says the rate is skimming. Paused, the rows around the word are shown with the
  word marked. Left and Right move by sentence. Unspaced text, such as CJK,
  is shown in chunks of at most 12 cells.
- An unreadable unit stops continuation. Everything already delivered is still
  shown, including a word the last range cut off and any omission it located.
  The reader says so when it reaches that point, not when read-ahead meets it,
  naming the byte where reading stopped within a partly read unit, and `]`
  then opens the next unit as an explicit choice that the next notice
  discloses. Nothing is skipped silently.
- Each session refuses a range that does not start where it was opened or
  continue where it ends, and fetches and timers are numbered across sessions,
  so a closed session's late result can never appear in a new one.
- Source text shown on one row (page labels, alternative text, omission
  details) has control characters escaped and line breaks and tabs folded to
  spaces.
- From TUI Read results, the reader opens at the selected listing entry, else
  where the last successful excerpt of that asset and scope began (issue 20).
  A declared-entry excerpt reopens its entry at that place with the entry's
  own cursor, keeping its declared end. No other token or representation
  identity moves between representations.
- `f` opens source links as a detour (issue 21): links in the shown passage
  come from `epub-structure/1` parts, a declared `noteref` is a note and any
  other link stays a link, and following one reads only its bounded target
  part, or the part a short marker anchor sits in, disclosed. External,
  missing, and off-spine targets are listed and never followed. Detours nest
  at most four deep over an untouched passage session; Escape returns, late
  results are ignored, and nothing is recorded.
- Position is shown as unit and byte. Closing reports it, and `nemalo read`
  prints the `--unit` and `--text-offset` that return there. No position,
  history, rate, or reading time is recorded.
- MCP gains no tool. A host shows a person one page at a time with
  `content_read` and its continuation; the plugin skill says how. Timed
  presentation belongs to a terminal the person controls, not to an agent.

Cell widths and grapheme boundaries come from `github.com/rivo/uniseg` v0.4.7
(MIT), already in the module graph through the terminal libraries and now a
direct requirement; no module is added.

## Evidence

Speed reading trades comprehension for rate; no technique removes that cost,
and rereading repairs understanding, which one-word presentation prevents
(Rayner, Schotter, Masson, Potter, and Treiman, 2016, *Psychological Science in
the Public Interest* 17(1), [doi:10.1177/1529100615623267](https://doi.org/10.1177/1529100615623267)).
Preventing regressions lowered sentence comprehension (Schotter, Tran, and
Rayner, 2014, *Psychological Science* 25(6),
[doi:10.1177/0956797614531148](https://doi.org/10.1177/0956797614531148)), so
the reader keeps pause and sentence rewind always available. With 209 readers,
one-word presentation matched normal reading on inferential questions at 250 to
350 words per minute and was worse at 400 and 450 (Di Nocera, Ricciardi, and
Juola, 2018, *International Journal of Human Factors and Ergonomics* 5(4),
[doi:10.1504/IJHFE.2018.096118](https://doi.org/10.1504/IJHFE.2018.096118));
hence the 250 default and the skimming label above 350. Literal comprehension
was lower than page reading at about 250 (Benedetto et al., 2015, *Computers in
Human Behavior* 45, [doi:10.1016/j.chb.2014.12.043](https://doi.org/10.1016/j.chb.2014.12.043)),
so pages remain the default mode. Pauses between sentences improved
comprehension (Masson, 1983, *Memory and Cognition* 11(3),
[doi:10.3758/BF03196973](https://doi.org/10.3758/BF03196973)). The clause,
block, and long-word extensions and the focus column follow existing readers;
no study found the focus position better than a centered word.

Screen reading costs little against paper overall, more for expository text
(Clinton, 2019, *Journal of Research in Reading* 42(2),
[doi:10.1111/1467-9817.12269](https://doi.org/10.1111/1467-9817.12269)).
Paging was at least as good as scrolling and supported going back (Haverkamp,
Bråten, Latini, and Salmerón, 2022, *Reading and Writing* 36,
[doi:10.1007/s11145-022-10328-9](https://doi.org/10.1007/s11145-022-10328-9)).
About 55 characters per line gave the best comprehension (Dyson and
Haselgrove, 2001, *International Journal of Human-Computer Studies* 54(4),
[doi:10.1006/ijhc.2001.0458](https://doi.org/10.1006/ijhc.2001.0458)), and
[WCAG 1.4.8](https://www.w3.org/TR/WCAG22/#visual-presentation) asks for a way
to keep lines within 80 characters.

Moving content must be pausable and its rate controllable
([WCAG 2.2.2](https://www.w3.org/WAI/WCAG22/Understanding/pause-stop-hide.html)),
so speed reading never starts by itself. Only the word changes: no blank frame
between words, no inverse video, and emphasis by weight and underline rather
than color, keeping well clear of
[WCAG 2.3.1](https://www.w3.org/WAI/WCAG22/Understanding/three-flashes-or-below-threshold.html).

Rejected: bolded word beginnings, which a registered report with 90 readers
found no faster and no better understood than plain text (Parker, Wang, Lee,
and Joseph, 2026, *Royal Society Open Science* 13,
[doi:10.1098/rsos.250177](https://doi.org/10.1098/rsos.250177)); remembered
positions by default, which existing terminal readers keep but Nemalo does
not; and a multi-word chunk mode, whose evidence is mixed.

## Validation

`internal/reader` tests deliver the same text in every range size from 1 to 99
bytes and require identical words, gaps, and positions; refuse ranges that skip,
repeat, or start elsewhere; place a cut word and pending omissions when a
session finishes, and leave them to the next session when it stops at its
limit; escape source control characters and keep labels on one row; keep PDF
lines and hanging indents within the width; extend layouts incrementally to the
same rows as a fresh layout; and check the pace rules and sentence navigation.
TUI tests page through a three-range document and require every word exactly
once in order, also across a session that reached its limit and continued; the
image where it was omitted; a deferred and explicit unreadable unit; place kept
across resizes; a paused start; pacing by the rule; stale, paused, and earlier
sessions' ticks and results ignored; waiting for and resuming at the next
range, including after a rate change; and no file changed. An independent
adversarial review found the cross-session results, lost trailing omissions, a
stalled wait, multi-row labels, and a hanging row two cells too wide; each is
fixed with a regression test.
Issue 23 added hard-wrapped and verse fixtures: delivered in every range size,
a wrapped paragraph flows with no orphan rows, a block end alone holds, and a
wrapped verse line hangs. Live, the issue's Gutenberg Pride and Prejudice
read at 100 by 30 and 40 by 20 in tmux on Linux with filled paragraphs.
CLI tests cover the printed return position and usage errors. Live: an arXiv
PDF read in tmux on Linux, paged, narrowed, speed read, closed, and reopened at
the printed position, with no file written.
