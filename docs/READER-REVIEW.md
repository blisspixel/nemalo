# Reader and paced-reading review

Reviewed 2026-10-24 UTC in response to the request for a stronger reader and
speed-reading mode. Product observations below come from primary documentation,
not installed comparisons, accessibility certification or performance benchmarks.
There is no objective single best reader for every document, device and person.
The reference set covers mature reading, accessibility, navigation and pacing.
No competitor code is copied or added as a dependency.

## Useful reference patterns

| Reference | Documented strengths | Nemalo implication |
| --- | --- | --- |
| [calibre viewer](https://manual.calibre-ebook.com/viewer.html) | Paged/flow modes, in-book search, keyboard link navigation, contents, reference locations, annotations, image zoom and read aloud | Make navigation, finding text and returning to a precise source place easy. A terminal does not provide its rich renderer; verified-copy handoff remains useful. |
| [Thorium](https://www.edrlab.org/software/thorium-reader/) | Keyboard and assistive-reader support, configurable text/spacing/columns, declared page lists, full-text search, accessibility metadata and read aloud | Test keyboard/focus and visible labels, preserve declared structure and expose rendering limitations. Plain terminal output and no-color tests alone do not establish assistive-technology compatibility. |
| [KOReader guide](https://koreader.rocks/user_guide/) | Book map, skim/page browsers, contents tools, configurable layout and document search | Provide a structural overview and deliberate jumps. A useful structure map need not record which pages a person viewed or how long they spent there. |
| [Readest reading](https://readest.com/docs/reading) and [parallel views](https://readest.com/docs/parallel-read) | Contents/bookmark/search sidebar, annotations, note exports and independent parallel panes, with optional synchronized movement | Keep reference material accessible without losing the passage. Preserve edition/translation identities; matching relative scroll positions is not evidence that two passages correspond. Parallel rendering is a separate future interface decision. |
| [SwiftRead speed controls](https://swiftread.helpscoutdocs.com/article/67-change-reading-speed-and-chunk-size) and [micro-pauses](https://swiftread.helpscoutdocs.com/article/22-micro-pauses-controlling-how-swiftread-pauses-while-reading) | Small speed adjustments during playback, configurable word groups and pauses for punctuation, word length, numbers and paragraphs | Make pacing controls discoverable and keep pause/rewind immediate. Distinguish the nominal setting from elapsed presentation time. Optional pacing choices require their own verification. |
| [Spreeder controls](https://www.spreeder.com/app.html) and [advanced settings](https://helpdesk.ereflect.com/portal/en/kb/articles/advanced-settings) | Play/pause/restart controls, adjustable group size and speed variability; documented punctuation pauses apply to single-word groups | Check interactions between settings rather than counting controls. Preserve the source order, gap notices and exact place through pause, restart and mode changes. |
| [Outread](https://outreadapp.com/) | A guided highlight moving through the page alongside a separate flash mode, with adjustable chunk length and appearance | Keeping page context available is a useful design target. A guided-page presentation is an option to evaluate, not evidence that suppressing rereading improves understanding. |

The [Rayner et al. review](https://journals.sagepub.com/doi/10.1177/1529100615623267)
finds a speed/accuracy tradeoff and does not support general promises of doubling
or tripling speed while preserving detailed understanding. Nemalo should evaluate
successful reading and retrieval tasks, accurate place retention and recoverable
navigation. Vendor speed multipliers and training claims are not acceptance tests.
The studies and accessibility rationale already recorded in
[decision 0025](decisions/0025-terminal-reading.md#evidence) remain relevant.

## Source-grounded Nemalo baseline

The baseline is the Go implementation in `internal/reader`, `internal/tui/pager.go`,
the content services, and [decision 0025](decisions/0025-terminal-reading.md).
Implemented, tested and shipped remain separate states.

| Capability | Current source and gap |
| --- | --- |
| Reading layout | EPUB block-aware flow, preserved verse/PDF lines, grapheme/cell-aware widths, adjustable measure and terminal resizing. Rich typography, image/PDF rendering and universal bidirectional layout are not claimed. |
| Pacing | Paused start, 250 nominal WPM, adjustable 100-1000 by 25, fixed sentence/clause/block/long-word holds, explicit gap/heading dwell and a skimming label above 350. These are presentation controls, not measured comprehension. |
| Recovery and context | Sentence rewind/advance, paused surrounding text, page/word switching, late-result/timer rejection, explicit range/session continuation and visible extraction gaps. |
| Structural navigation | Declared EPUB contents/page lists in the Read view; full-screen c now adapts the same declared-entry service and physical PDF units. NAV/NCX and unresolved choices stay explicit, selection stops at the declared end, and closing the chooser preserves the paused passage. This increment requires its own full acceptance; in-reader search remains separate. |
| Finding text | Shared `content locate` supports bounded exact, case-sensitive EPUB/PDF matches with source-bound cursors. The full-screen reader currently has no search dialog. Reuse that service rather than introducing another parser or implicit index. |
| Notes and places | Source links/footnote detours preserve the passage; explicit local bookmark save/restore and notes exist. Opening, playing and closing do not save progress or change a bookmark. |
| Help during playback | A deterministic regression reproduced advancement while help hid the words. The repair pauses playback, clears waiting state and invalidates the old timer; leaving help requires an explicit play action. Native validation is recorded separately. |
| Page context and return | Native terminal validation reproduced a word-to-pages-to-word rewind from byte 11 to byte 0. The repair preserves the exact word through a page-context visit and resizing. Deliberate page navigation chooses a new place. The anchor exists only in session memory. |
| Other presentation modes | Guided page highlighting and multi-word groups are not implemented. Decision 0025 rejected multi-word mode on mixed evidence; this review does not silently reverse that decision or claim parity. |

First address correctness and access to existing capabilities: help must freeze
the place, navigation must be deliberate and source-bound, and search must state
its match/coverage limits. Then evaluate optional presentation controls with
multilingual fixtures and actual keyboard use. Preserve the current Go stack,
shared services, default pages, explicit bookmarks and absence of reading history.
Fonts, system speech, rich images and parallel graphical panes are distinct
capabilities; an existing reader can receive a verified copy without expanding
Nemalo's terminal renderer or introducing a second library policy.

## Local validation work

The requested machine validation should exercise complete workflows and refusals,
not infer native operation from a build. Use synthetic fixtures and owned roots
outside the checkout, with a clean exact-commit binary and bounded commands.
Record versions, selected inputs, commands, results and before/after inventories.

The initial Linux tmux 3.8 check on the candidate source confirmed both repairs:
help held unit 0, byte 11 through a 1.2-second dwell; returning from help stayed
paused; and page-context return stayed at byte 11. Library modes and bytes,
including disabled bookmarks, were unchanged. This is a candidate native result,
not exact committed, cross-platform or hosted acceptance.

- Reader pages/word mode: open the same EPUB/PDF through the CLI and TUI, resize,
  pause/play, change rate/measure, rewind, show help, follow/return from notes,
  change unit, reach gaps and close/reopen at the printed location. Verify source
  positions and unchanged bytes, including disabled bookmarks.
- Library lifecycle: initialize, inspect/import/ingest, compare holdings, audit,
  preview/apply authorized operations on fixture data and exercise interrupted
  recovery. Keep checked publication distinct from an initialized or byte-held
  library. Test cleanup/restore/purge only on owned fixtures with explicit plans.
- Discovery/acquisition: validate configured fixed-host searches/evaluations,
  selected intake, resumption and retained failures separately from library import.
  Live requests are explicit and bounded; offline tests use injected providers.
- Content/integration: retrieve text, navigation, PDF quotations, MP3/MP4 packets,
  opt-in decoded ranges, evidence replay and citations; exercise scoped MCP
  denial cases and the explicit Jobs view. Compare CLI/service data where relevant.
- Installed tools: record actual scanner, archive-reader, decoder and reader/player
  availability. A missing tool, skipped scan or failed check is an explicit coverage
  gap. Test doubles in offline tests do not establish installed-tool operation.

Keep an outcome for each feature family: native pass, expected refusal, incomplete,
unsupported, or not exercised. Preserve exact local/native/hosted receipts in the
appropriate validation record. This review authorizes no telemetry, generated
summaries, automatic progress saving, cloud synchronization or external uploads.
