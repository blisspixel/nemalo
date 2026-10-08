# CLI and TUI interfaces

Date: 2026-10-08. Status: accepted.

CLI and TUI are first-class interfaces for 1.0. Linux is primary; macOS and Windows
need native checks. Web/desktop interfaces and a hosted service are outside the
current scope. JSON stays available for noninteractive use and future MCP adapters.

Use standard-library flags for the small CLI. Use maintained Go terminal libraries
for interactive rendering/input rather than writing our own terminal protocol,
Unicode editor, clipboard handling, or resize engine. Current stable pins:

- [Bubble Tea v2.0.10](https://github.com/charmbracelet/bubbletea/releases/tag/v2.0.10)
  provides the event loop, terminal lifecycle, rendering, and platform handling.
- [Bubbles v2.2.1](https://github.com/charmbracelet/bubbles/releases/tag/v2.2.1)
  supplies text editing and a scrollable viewport. Both projects use MIT licenses.

These add transitive Go modules for Unicode/terminal/platform support, recorded
in `go.sum`; they do not add a Python/Node runtime or require cgo for release builds.
Clipboard support may use separately installed platform utilities when invoked;
the current workflows do not require clipboard access. A zero-dependency custom
TUI would move substantial protocol and portability risk into Nemalo itself.
Do not add another UI framework or a provider SDK for convenience.

Models handle presentation and dispatch operations through `internal/app`. Keep
provider policy, filesystem access, and future mutation/recovery logic out of
update handlers. `internal/present` escapes data for both terminal interfaces.
No startup network calls. Visible modes, keyboard instructions, cancellation,
resize handling, and noninteractive CLI parity matter more than decorative styling.

Update-state tests exercise search/inspection dispatch, stale results, cancellation,
input, resize, and error rendering. A real terminal smoke is separate evidence;
model tests alone cannot prove accessibility or terminal compatibility. Native CI
must verify all three OSes, including race tests and cgo-free binary smokes.
