# 0104: terminal feedback, toasts and the message log

Status: recorded before implementation, 2026-10-27. Fourth increment of the TUI
design tracker [issue 65](https://github.com/blisspixel/nemalo/issues/65):
[issue 71](https://github.com/blisspixel/nemalo/issues/71). It builds on
decisions [0101](0101-task-sections-and-action-registry.md) to
[0103](0103-list-filter-paging-and-mouse.md) and supersedes the "Motion: none"
rule of the earlier design tracker for the panel layout only. It adds no
service, format, module or dependency.

## Context

Every finished operation said "Complete.", running work said "Working..." with
no time, phase or progress, and each message replaced the last. Acquisitions
already report a phase and bytes through a progress callback; imports, rescans,
audits and collection acquisitions do not.

## Decision

1. **Completion text** comes from the typed result: imports name the holding and
   its recorded status, checks name the health and antivirus status and add "not
   proof of safety" to a scan with no detections, and library checks, audits,
   inventories, rescans, cleanups, restores, purges, searches and evaluations
   give their own counts. An unknown result still says "Complete.".
2. **Running work** shows elapsed time in the panel title and content, the
   reported phase, and a bar only when the service counts bytes against a known
   total (single-file and recording acquisition). The bar stops at 99% until a
   result arrives. Other work shows elapsed time without a bar. Escape cancels
   as before.
3. **Motion.** A four-frame ASCII spinner turns every 125 ms while work runs;
   otherwise one tick a second expires toasts. Ticks never start work.
   `NEMALO_REDUCE_MOTION`, plain mode and `TERM=dumb` draw no spinner and no
   toast, and round elapsed time down to five seconds, so the busy line changes
   at most every five seconds. The bar and spinner are drawn with ASCII rather
   than the Bubbles progress component, whose default gradient uses truecolor
   values the TUI does not emit.
4. **Toasts.** A finished result puts its outcome word (Done, Needs attention,
   Failed) and text at the right of the footer, in the colour of the typed
   outcome. Others fade after five seconds; a failure stays until the next key.
   The status line still carries the same text.
5. **Message log.** Every status of the session is kept with its time, up to 500
   entries, in memory only. `L` with results focused, or "Show this session's
   messages" in the palette, shows it; Escape or `L` closes it. It is never
   written to disk.
6. **Native harness.** The PTY harness runs with reduced motion so its compared
   screens stay deterministic; model tests cover motion and toasts.

## Verification

Model tests cover elapsed time and spinner frames with and without reduced
motion, a bar only for counted bytes that stays below 100%, typed completion
text, toast colour from the outcome even when the text contains other words,
fading and persistent toasts, dismissal by a key, no toast with reduced motion,
the log's times, `L` typed into a field and the 500-entry bound. The registry
parity test covers `L`. Full local and hosted gates apply.
