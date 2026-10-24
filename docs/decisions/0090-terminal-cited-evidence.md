# 0090: explicit terminal evidence and citation views

Status: recorded before implementation; implemented in source, 2026-10-24. Extends the
accepted shared evidence and citation services in
[decision 0087](0087-source-bound-evidence-and-citations.md) into stage 5's TUI.
This is an adapter increment, separate from its verification and a release.

In Read, F2 explicitly requests the selected managed holding's recorded citation
metadata. F4 explicitly repeats the last successful bounded excerpt with evidence.
Neither action runs automatically when selecting, listing, opening or reading.
Units, failed requests and cancelled requests cannot become an evidence selection.
A new excerpt or listing clears that selection; changed catalog, root or asset
requires another successful excerpt. Snapshot catalogs remain usable for ordinary
reading, but the canonical evidence/citation services require a managed library.

Show each response in a separate read-only full-screen report. Retain the ordinary
report, selection, continuation, location, form drafts and scroll positions.
Escape closes the report and cancels outstanding work; Ctrl+C exits. Block other
form and mode actions while it owns the screen. Use physical control keystrokes,
including for scrolling and replay, so committed text cannot invoke them.
Keep bounded layout at 40, 80 and 116 columns and reset scroll margins on resize.

F3 in an evidence report deliberately replays its same captured bounded request
and the returned reference. Never derive parameters or permissions from a token,
follow a continuation automatically, or enable a decoder. A failed replay discards
its response payload and earlier token instead of displaying stale evidence.
Generation, scope and screen-ownership checks reject late results. Citation work
uses a 15-second deadline; evidence uses the shared five-minute maximum with
caller cancellation and unchanged inner budgets.

Call only `app.Service.Content` and `app.Service.Citation`. Shared presentation
shows the human report and the complete portable evidence or citation sidecar,
including identities, declared metadata, unknown fields, gaps and limits. These
are source facts and integrity comparisons, never interpretation, consumption,
rights or safety guarantees. No parser, evidence schema, token format, model,
store, dependency, network request or MCP tool is added. TUI MP4 stays units-only;
the explicitly approved CLI and named MCP decoder routes keep their boundaries.

Verify successful text/audio reads, exact replay, malformed or changed bindings,
units/failure/scope refusals, cancellation and late replies using synthetic data
and injected scanners/providers. Compare complete library bytes and modes,
including disabled bookmarks. Follow with full local checks, native terminal
interaction on owned fixtures and the exact hosted acceptance gate. Broader
platform, host and assistive-technology acceptance remains separate.
