# 0094: terminal keyboard and cursor access

Status: recorded before implementation, 2026-10-24. Stage 5 accessibility review,
first increment. It is not a screen-reader audit or certification.

## Findings

The review checked the TUI against its own stated rules (no meaning carried by
color alone, explicit text for every outcome, `NO_COLOR` and `TERM=dumb`
support, 40 by 20 minimum) and against three gaps those rules do not cover.

1. Function-key-only actions. Search provider (F2), read budget or result order
   (F3), format filter (F4), full report (F5), results focus (F6), library
   initialization, import and cleanup reviews (F7 to F9), Jobs (F10), the Read
   citation and evidence reports (F2, F4) and evidence replay (F3) had no other
   key. On a Mac the top row controls system features unless Fn is held
   ([Apple](https://support.apple.com/en-us/102439)). GNOME Terminal uses F10 as
   its menu accelerator by default
   ([GNOME Terminal help](https://git.stg.centos.org/source-git/gnome-terminal/blob/3bdf475c279f72459863c8f4ae3cce2f0b395501/f/help/C/pref-keyboard-access.page)).
   Compact keyboards may have no function row, and a held modifier plus a
   function key is a two-handed chord.
2. No real cursor. Every text input drew a virtual cursor cell and the terminal
   cursor stayed hidden. Screen magnifiers and screen readers that follow the
   text cursor had no position to follow.
3. Linear reading exists. `content read` without `--json` prints a located
   passage and an explicit next step as plain lines, which a screen reader can
   read in order, and the word reader starts paused. Its continuation token is
   long to copy by hand; that is recorded, not changed here.

## Decision

Keep every existing key. Add `Ctrl+O`, which opens an Actions list of exactly the
function-key and control-key actions the current view accepts, each named with
its key. Up and Down (or `j`/`k`) choose, Enter runs, Escape closes. Running an
item sends that same key through the normal handler, so availability, busy
guards and policy stay in one place. An item that opens a write review still
needs the review's own physical Enter; the list cannot confirm a review.
`Ctrl+O` is free in the text input keymap and in Nemalo's bindings, and Bubble
Tea's raw terminal mode disables the legacy `^O` discard character. The
evidence sidecar has no text input, so `r` replays there beside F3.

Text inputs use the terminal's real cursor at the edit position, through the
pinned Bubbles `Cursor` API, in the main form fields and the reader's find
field. The Actions list places the real cursor on the chosen item. Rendering
text, keys and policy are otherwise unchanged; no module or version changes.

## Verification

Model tests cover the list's per-mode contents, dispatch through the normal
handlers, refusal while busy or during a review, review confirmation still
requiring a physical Enter, and real cursor coordinates, including wide
characters. The native terminal harness adds six cases to decision 0092's list:
the terminal cursor visible right after typed text on the focused field, a
scroll and then `r` evidence replay (an identical screen sends no output), and
`Ctrl+O` opening Read's actions and reaching the citation report with no
function key, then returning. Full local and hosted gates apply. Screen reader,
magnifier and keyboard-only sessions by a person on each OS remain a separate,
required review.
