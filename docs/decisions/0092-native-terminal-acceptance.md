# 0092: bounded native terminal acceptance

Status: recorded before implementation, 2026-10-24. Stage 5 acceptance tooling,
separate from graphical terminals, assistive technology, host/plugin calls and a
release. Decision [0091](0091-native-executable-lifecycle-acceptance.md) passed its
[exact native gate](https://github.com/blisspixel/nemalo/commit/d6ab8d86cc53f5e86a72bd3db3dfc8c0ae8757b0#commitcomment-206444963).
This extension passed its exact native gate at `6f17380`; see
[validation](../VALIDATION.md#native-terminal-acceptance).

## Requirement and adapters

The existing Linux tmux observations and native model tests do not establish
interactive reader/TUI operation on macOS and Windows. Extend the explicit Go
acceptance command with `--terminal`, after its unchanged 39 executable cases.
Reuse the checked executable copy, owned workspace, CLI services, scanner/tool
isolation, shared diagnostic/read allowances and five-minute overall deadline.
The default remains the 39 cases, with no terminal opened.

Use maintained terminal adapters and a screen-state emulator instead of writing
a terminal parser or Windows process launcher. Pins checked against primary
source at implementation start:

- [creack/pty v1.1.24](https://github.com/creack/pty/tree/v1.1.24), MIT, supplies
  native Unix PTYs over `os/exec`.
- [Charm conpty v0.2.0](https://github.com/charmbracelet/x/tree/conpty/v0.2.0/conpty),
  MIT, supplies Windows pseudoconsole creation, argument-array spawn, resize and
  close. Its x/sys minimum is below the established v0.48.0 pin. The adapter owns
  the returned process handle and closes it after obtaining its Go process.
- [Charm vt v0.1.0](https://github.com/charmbracelet/x/tree/vt/v0.1.0/vt), MIT,
  interprets the current screen. It reuses the existing ANSI/ultraviolet stack,
adding x/exp/ordered. Verify the resolved module graph
  and compatibility without automatically upgrading established pins.

These modules belong to the acceptance tool, not Nemalo's application import
graph. Check actual executable build metadata before claiming that separation.
They add no scanner, shell, downloaded executable, cgo requirement or non-Go
runtime. Preserve their license obligations; release notices still follow the
application executable's actual module list.

The evaluated go-pty v0.2.3 and xpty v0.1.4 command wrappers require additional
care around Windows context completion and returned process-handle ownership.
Use the lower-level maintained ConPTY spawn API with explicit ownership instead.
This does not copy an upstream implementation or change an OS/application policy.

## Bounded terminal contract

Run the exact private executable under a fixed `xterm-256color` profile, no color,
with an isolated environment. Keep HOME unchanged. Bound each session to one
minute, at most three sessions, 120 by 40 cells, 2 MiB raw output and 64 KiB input;
raw diagnostics also count toward the existing shared 8 MiB output allowance.
Read only current cells, clear exposed scrollback and bound saved frames. The
pinned emulator retains at most 10,000 internal lines per screen; history is
never used as the frame oracle. Raw-byte, geometry and time limits also apply.
Serialize emulator
mutation and snapshots. Handle terminal query replies without clipboard or
external OSC actions; screen-string searches must examine current cells rather
than all historical output.

Input, output and query response work remain separately drained. Cancellation
kills and reaps the owned child, closes the terminal and joins owned workers.
Report teardown errors/timeouts as failed acceptance and retain diagnostics.
Do not turn unavailable PTYs or an incomplete exit into passing skips. Follow
[Microsoft's channel and lifetime requirements](https://learn.microsoft.com/en-us/windows/console/creating-a-pseudoconsole-session).

Use the existing exact EPUB holding for the CLI reader and TUI reference view.
Prepare a separate two-page synthetic PDF library through the same CLI, recording
setup commands separately from the original 39 case identities and fixtures.
Check actual navigation, exact finding, page/word mode, play/pause, help dwell,
same-place return, resize, explicit citation/evidence/replay, normal continuation,
and real q/Ctrl+C exit with primary-screen restoration. Require the exact 49
terminal case identities and order, including reader return instructions with
the same explicit source position. Terminal-profile key encodings are part of these tests;
they do not prove support for every enhanced keyboard protocol.

Compare complete fixture/library modes and hashes before and after reading,
including disabled bookmarks and the absence of newly created bookmark state.
Do not infer consumption or save position, rate or history. A successful process
exit alone is insufficient: require the intended current screen and source place.
The optional report has its own `nemalo-terminal-check/1` schema with actual case
identities, frames, diagnostics and exits; the existing outer receipt and 39
command identities remain valid.

Test the emulator/transport boundaries, output/input budgets, cancellation,
process/handle lifetime, stale-screen refusal and preservation with owned Go
fixtures. Normal tests invoke no installed tools or network. Require final local
canonical/native-race checks and exact hosted terminal execution on all three
OSes, in addition to every existing verifier, recovery, boundary and installer
gate. Implementation or a cross-build is not native acceptance.
