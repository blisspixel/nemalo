# Terminal guide

Nemalo uses the same application services as the CLI. No provider request runs at
startup. Select an operation, fill its fields, and press Enter to run it.
Results and form drafts remain with each operation for the current session.

## Browse and inspect

Search and holdings show a selectable list, with an adjacent evidence pane in
terminals at least 100 columns wide. Narrower terminals show the list; Enter in
results opens the selected item's details. F5 switches to the complete report,
including identifiers, provenance, findings, and limitations.

F6 moves focus between the form and results. A `>` marker identifies focused
fields and the selected row without relying on color. Up/Down select results;
Home/End select the first/last item on the current page. PageUp/PageDown scroll
the evidence pane or report. Escape returns from details, then to the form.
While work runs, Escape cancels it. Ctrl+C quits from anywhere.

In Archive search results, `e` fills the evaluation form with the selected item
ID. This makes no request; Enter explicitly retrieves metadata. Evaluation does
not download content, determine file-level rights, or establish safety.
Evaluation also lists source files. Enter a new Intake directory with Ctrl+N,
focus a file with F6, then `d` reviews the exact acquisition before a confirming
Enter. Downloads remain untrusted; Check/Scan are staged afterward. See
[acquisition](ACQUISITION.md) for incomplete-packet handling and bounds.

## Commands and shortcuts

| Key | Action |
| --- | --- |
| Tab / Shift+Tab | Next / previous operation |
| Alt+1..9, Alt+0 | Jump to the numbered operation; 0 selects Read |
| Enter in form | Run the explicit operation |
| F6 | Focus results / return to editing |
| Up / Down, Home / End in results | Select an item |
| Enter in results | Toggle selected-item details |
| F5 | Toggle complete evidence report |
| PageUp / PageDown | Scroll detail/report |
| Ctrl+Left / Ctrl+Right | Previous / next search or holdings page |
| F2 in search | Switch Open Library / Internet Archive |
| F3 in inspect, snapshot, audit | Select bounded 1 GiB / 5 GiB read budget |
| F4 in holdings | Cycle EPUB / PDF / MP3 / all filename filters |
| F7 in State | Review initialization of an explicit existing folder |
| Ctrl+N in two-field forms | Switch between fields |
| Ctrl+A in snapshot | Toggle historical health metadata |
| e in Archive results | Prepare selected-item evaluation, without requesting it |
| d in Evaluate results | Review selected-file acquisition into a new intake directory |
| r in Holdings results | Stage source access; explicit root still required |
| Enter in Read results | Retrieve the selected source unit |
| n / r / u in Read results | Next range / repeat last request / list units |
| Escape | Back; cancel active work |
| Ctrl+C | Quit |

Edited queries require Enter before paging. Holdings start with EPUB filenames;
filename filtering does not validate content. Snapshot success fills empty
holdings/audit forms but does not automatically open or audit anything.

State (operation 9) reads local control identity/status on Enter. F7 reviews the
exact folder and control-file writes; a second Enter initializes it. Escape returns
without writing. Review freezes fields/navigation and supports PageUp/PageDown.
This establishes control state, not a checked content library; see
[the state contract](decisions/0006-library-control-state.md).

## Source access

Select a Holdings asset and press `r`. This stages Read without reading files.
Use Ctrl+N to enter the explicit source root, then Enter lists EPUB spine units.
F6 focuses the list; Enter retrieves up to 4096 structured-text bytes from the
selected unit. The report preserves lines and escapes terminal control bytes.
It reports extraction gaps and references outside the delivered range.

In focused Read results, `n` requests the returned continuation, `r` repeats the
last request, and `u` lists units again. Failed requests retain the previous next
reference; repeat retries the failed request. Changing catalog/root requires a
fresh units request before continuation. Switching operations preserves the
session's report and references. Nothing records reading or acknowledgement.
The CLI exposes note/source/resource references; the TUI does not yet navigate
notes, render images, or play audio. Unsupported units fail explicitly.

## Appearance and boundaries

The indigo and neutral palette adapts to light and dark terminal backgrounds.
Amber indicates active work; errors have explicit text. `NO_COLOR` and `TERM=dumb`
disable colored styling. Minimum size is 40 columns by 20 rows; state survives
the resize prompt. No image, animation, or color is required for navigation.

Full reports remain accessible when compact browsing truncates a title. Catalog
assessment is historical; audit current bytes before relying on identity. Scanning
is explicit and the installed scanner's cloud/sample policy applies. See
[operation and limits](DEVELOPMENT.md) for the full capability boundaries.

## Application-view captures

These are rendered terminal frames from the actual Go application model, using
the real external catalog and live Archive results, captured 2026-10-08. They are
not OS screenshots or mockups. Downloaded content and private paths are excluded.

![Multilingual EPUB holdings and selected evidence](images/tui-holdings.png)

![Internet Archive discovery and selected source evidence](images/tui-discovery.png)

![Bounded source reading from Alice's Adventures in Wonderland](images/tui-reading.png)
