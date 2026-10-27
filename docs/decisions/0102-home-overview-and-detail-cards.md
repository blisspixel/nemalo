# 0102: Home overview, configured defaults and detail cards

Status: recorded before implementation, 2026-10-27. Second increment of the TUI
design tracker [issue 65](https://github.com/blisspixel/nemalo/issues/65):
[issue 67](https://github.com/blisspixel/nemalo/issues/67) (Home) and
[issue 68](https://github.com/blisspixel/nemalo/issues/68) (detail cards). It
builds on [decision 0101](0101-task-sections-and-action-registry.md). It adds no
service, format, module or dependency.

## Context

The TUI opened on Search with an empty form, did not read `config.json`, and
gave no view of the library, its jobs or saved bookmarks. Detail panes were
report text that repeated sizes, paths and the full 71-character hash, with
caveats taking as many rows as facts, and no way to copy an identifier.

## Decision

1. **Configured defaults.** `nemalo tui` passes the library resolved with the
   CLI's precedence (`--library`, then `NEMALO_LIBRARY`, then `config.json`) to
   the terminal (issue 67 option a). It prefills Home, Holdings, Read (catalog
   and root), Status, Jobs, Audit and the Collections library field. Fields stay
   editable. The Gutenberg catalog default is unchanged.
2. **Home** is a new first section and the start view. Its one field is a
   library folder. With a configured library, Home reads an overview once at
   startup; otherwise Enter reads it. The overview uses existing read-only
   services: library status, holding counts by status (one-row pages), the
   bookmark list and the passive job snapshot ([decision 0088](0088-non-mutating-library-job-snapshot.md)),
   under a 15-second deadline, cancellable with Escape. It reads nothing when
   status is not `initialized`, and it creates, repairs or writes nothing.
   Scanner availability comes from the same report as `nemalo doctor`, which
   looks up tools and reads signature headers without running a scanner.
3. **Home entries.** Explicit bookmarks appear as Continue entries; Enter opens
   the saved location in Read through the existing resume service. Nothing else
   about reading is shown or inferred. Each section follows with a one-line use
   and a count where one exists; Enter goes there, and Library lists holdings.
   Without a library, three first-run choices lead to Discover, Check files and
   Manage. Home names `--json` and `nemalo mcp serve` for scripts and agents.
4. **Detail cards** for holdings (catalog and managed), search results,
   evaluation files and jobs share one shape: title, one line of facts, status
   chips, a shortened identity (`scheme:` plus the first 12 and last 4
   characters), aligned lists and one dim caveat line. Each fact appears once.
   Chips come only from typed status values and never claim more than the value
   does: a missing or partial scan is never clean, and `no detections reported`
   is not called safe. The F5 report keeps every recorded field and limitation;
   for catalog holdings it now includes each asset's recorded assessment.
5. **Copy.** `y` with results focused sends the selected item's full identity to
   the terminal clipboard with OSC 52 through Bubble Tea. A terminal can ignore
   that request without telling the program, so the status says the request was
   sent and that F5 shows the full value; an item without an identity says so.
   Nemalo still never reads the clipboard.

## Verification

Model tests cover first-run choices, configured prefill, an overview that leaves
every library byte unchanged, missing and uninitialized folders without created
state, Continue entries only for explicit bookmarks, Library listing holdings,
cards with each fact once and a full F5 report, short and copied identities,
copy without an identity, `y` typed into a field, and chips from typed values.
The registry parity test covers Home's keys. The native terminal harness's
startup case observes Home's first-run view. Full local and hosted gates apply.
