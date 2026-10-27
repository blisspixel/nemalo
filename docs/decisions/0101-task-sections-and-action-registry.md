# 0101: task sections, panels and one action registry

Status: recorded before implementation, 2026-10-27. First increment of the TUI
design tracker [issue 65](https://github.com/blisspixel/nemalo/issues/65):
[issue 66](https://github.com/blisspixel/nemalo/issues/66) (frame) and
[issue 69](https://github.com/blisspixel/nemalo/issues/69) (actions). It builds on
[decision 0094](0094-terminal-keyboard-and-cursor-access.md) and keeps every
guarantee recorded there. It is a presentation change only: no service, policy,
format, module or dependency changes.

## Context

Navigation mirrored the twelve internal operations. The tab row showed numbers
for all but the selected one even at 120 columns, every screen stacked a form,
status, position line and dashed rule above a ` | ` list and detail split, and
actions were described in four unconnected places: hand-built footers, the
`Ctrl+O` list, per-mode empty help and the guide. Issue 65 proposed task
sections, titled panels and one registry, with four open choices.

## Decision

1. **Sections.** Steps are grouped under Discover (Search, Evaluate), Library
   (Holdings, Read), Collections (Collections, Revisions), Check files (Inspect,
   Check, Scan, Snapshot, Audit), Manage (Status) and Jobs. The names match the
   README and `nemalo help` groups (issue 66 option a). Jobs becomes its own step,
   reading the same passive snapshot as F10 in Status
   ([decision 0088](0088-non-mutating-library-job-snapshot.md)). Home is the next
   increment ([issue 67](https://github.com/blisspixel/nemalo/issues/67)). Tab and
   Shift+Tab move through steps in section order; Alt+1..9, Alt+0, Alt+A and Alt+B
   keep their operations. Jobs has no Alt key and is reached by Tab or the palette.
2. **Frame.** One header row. At 100 columns and wider a sections sidebar lists
   every section, with the current section's steps under it; narrower terminals
   show the section names on the header row (wrapping whole names below 80
   columns) and the current section's steps in the panel title. The current
   section and step are bracketed, so location never depends on colour. Panels
   have rounded borders with a title and state in the top border. The focused
   panel uses a heavy border, and the `>` marker stays on the focused field or
   row. Forms are compact rows inside the panel they feed. The status line and a
   one-line footer of at most six keys follow. No ` | ` divider or dashed rule
   remains in this layout.
3. **Plain mode.** `nemalo tui --plain`, and `TERM=dumb`, keep the linear layout:
   one location line naming the section and its steps, the context line, the
   fields, status, position, an ASCII rule, content and the footer. It draws no
   borders or sidebar, for screen readers and magnifiers. `NO_COLOR` only removes
   colour; it does not select plain mode.
4. **Registry.** One typed list of actions (key, name, steps, focus scope,
   effect, availability with a reason, footer priority) drives the footer, the
   `Ctrl+O` palette, the Space item menu and `?` help. The existing handlers stay
   the authority: running an entry sends its key through them. A model test
   presses every candidate key in every step and focus and fails when a key that
   changes state is missing from the registry, or a listed, available key does
   nothing.
5. **Palette.** `Ctrl+O` lists every action in every section, searchable as you
   type, with the current step's available actions first. Unavailable entries are
   dim and state why. Network actions carry the word `network`, writes `review`.
   Choosing an action that runs on Enter, uses the network or writes without a
   review only moves to its step and names the key to press; review actions open
   their existing review, which still needs its own physical Enter. Because typing
   now filters, Up and Down (not `j` and `k`) choose inside the palette. The real
   terminal cursor stays on the chosen row. The palette is refused while work runs.
6. **List-only keys.** Space (item actions), `?` (help) and `:` (palette, as in
   k9s and vim) act only when results have focus, so typing in a field is never
   taken over.
7. **Unchanged.** No request or write at startup; review confirmation needs a
   physical Enter; untrusted text is sanitized; 40 by 20 minimum; colour roles
   are the terminal's ANSI colours, with cyan added for network. Motion and the
   remaining issue 65 choices (configured defaults, covers) belong to their own
   increments.

## Verification

Model tests cover registry and handler parity, key round trips, palette search,
availability reasons, preparation instead of execution for Enter, network and
unreviewed writes, review entry through the palette with a separate physical
Enter, the item menu and help, list-only keys while typing, the real cursor in
fields and on the palette selection, 40x20 to 120x36 layouts without overflow,
at least 18 holdings rows at 80x24, section names at 80, 100 and 120 columns, and
plain and no-colour rendering. The native terminal harness's cases are updated to
the new frame. Full local and hosted gates apply. Real screen reader, magnifier
and keyboard-only sessions remain the person-run review in issue 62.
