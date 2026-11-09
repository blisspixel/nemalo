# 0110: next action and pane layout

Status: accepted with this implementation, 2026-11-08. A presentation change
in the TUI design wave ([issue 65](https://github.com/blisspixel/nemalo/issues/65)).
It builds on [decision 0101](0101-task-sections-and-action-registry.md) and adds
no service, policy, format, module or dependency. It does not close issue 65,
[70](https://github.com/blisspixel/nemalo/issues/70) or
[72](https://github.com/blisspixel/nemalo/issues/72). Decision 0109 is the
OAPEN Library handle and is unchanged.

## Context

Search, evaluation, download, import, reading and one-word reading were already
separate steps, each with its own review. Once a screen held results, the footer
showed movement keys and the status said the outcome, so the next step was easy
to miss. The sections sidebar and the side details appeared only from the
terminal width. There was no key to hide them.

The constraints that stay: no request or write without its existing review and
a physical Enter; plain mode stays linear for screen readers; Holdings keeps at
least 18 content rows at 80 by 24; a status of more than two lines at 40 columns
is replaced by a truncation notice, so a hint must not cause that.

## Decision

1. **Next action.** The first visit to Home, Search, Evaluate, Holdings, Read
   and Manage names the next action in the status line. A finished search,
   holdings list or import keeps its own summary and adds the next key when the
   combined line still fits at 40 columns. A longer one of those summaries is
   left unchanged. An evaluation whose summary plus the next key would exceed
   two lines uses the file count and the next key instead, so the truncation
   notice does not hide both; the report still lists the files and says the
   statements are declarations. An OpenAlex location list and an Open Library
   edition list name `e`, not a download. `e` prepares Evaluate and does not
   download. `d` reviews one download into a new intake folder. The review
   footer says Enter download. After import, the status points at Holdings and
   `r`. In Holdings, `r` reads here and `o` opens a verified copy. In Read, `p`
   is pages and `s` is one word at a time, paused at 250 words per minute.
   Those keys are labeled on the footer when the list has focus. The selected
   row repeats the next key on the blank line under that row, including F6
   while the form still has focus, so a long result summary cannot hide it. A
   download review, a finished download and an applied import add one sentence
   naming the following screen. One-word reading opens paused and says that
   Space plays and that closing saves nothing. The empty views and the
   first-run welcome say the same path.
2. **Panes.** `Ctrl+\` cycles four layouts: follow the width, hide the sections
   sidebar, hide the side details, hide both. Below 100 columns the sidebar is
   already absent, and hiding it keeps the section names on the header. Hiding
   the side details leaves Enter as the way to open a book. Plain mode does not
   cycle; the status says it stays linear. The key does nothing during running
   work, a review, the palette, help or the message log. The choice is for the
   session. Width still decides the default.
3. **Unchanged.** Reviews still need their own Enter. Downloads stay untrusted
   until import. The reader still stores no position, rate or history. Covers,
   mouse behavior and the plain layout are unchanged.

## Verification

Model tests cover the arrival text at 40 columns, including an OpenAlex
location list and an edition list, the result hints and the case where a long
import title keeps its summary, the selected-row cue for search, evaluation,
holdings, audio and reading, the download and import follow-on sentences, the
paused speed-reader line, the Holdings to Read handoff, the footer labels, the
four pane layouts at 120 columns, and refusal while busy, in a review and in
plain mode. The registry round trip includes `Ctrl+\`. The existing 18-row
Holdings check and the registry parity test still apply. The native terminal
units observation looks for the parts-listed status.
