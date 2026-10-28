# Real-resource validation collection

The [foundations manifest](../collections/foundations.json) selects 100 resources:
80 EPUB editions, 10 LibriVox recordings, and 10 versioned arXiv papers. It covers
13 languages. The recordings contain 118 exposed tracks, not 10 sample chapters.
Books and their audio versions remain distinct resources, linked by their sources.

The collection supports broad study of literature, perspective, inquiry, ethics,
freedom, society, mathematics, science, and listening. It is an editorial starting
point, not a universal curriculum, a consciousness test, or an endorsement of each
author's claims. English and European traditions remain overrepresented. Historical
prejudice and obsolete science need context; prominence is not evidence of truth.
Each selection records its rationale, language, source identity, and rights caveat.
The [Gutenberg catalog subset](../collections/gutenberg-catalog.csv) records the
selected IDs and catalog types from the official weekly CSV, retrieved 2026-10-08.
Its line endings and dash typography are normalized. An offline test requires all
80 ebook selections to be cataloged `Text`: an audio catalog page can itself have
an EPUB wrapper, so file extension and container validity are insufficient evidence
that the selected resource is a full text edition.

## Native workflow review on 2026-10-24

The Linux review used a clean binary of commit `3a6c793` (SHA-256
`f95b9abb8310794dac6f2076fe7cd0d8bd67d1d7b4c8ab5a4865643bae61af09`),
owned synthetic fixtures outside the checkout, explicit roots and bounded CLI
commands. Full local JSON responses, argument arrays and receipts stay beside
the fixtures. These observations establish the selected workflows on this host,
not every format, platform, accessibility configuration or release.

| Workflow | Observed outcome |
| --- | --- |
| Library lifecycle, 40 actual CLI calls | Initialization/status, import preview/apply, installed ClamAV rescan preview/apply, list, audit, whole-library check, export, cleanup preview/apply, raw restore and fresh purge passed. An unscanned PDF stayed in review and made audit return nonzero. Integrity health and checked status remained separate. |
| Source preservation and refusals | Read-only calls preserved library modes and bytes. Original EPUB/PDF sources stayed unchanged. Restore reproduced the owned source tree. An unsupported neighbor stayed through cleanup and purge. Disabled bookmark save failed without changing state. |
| Content and explicit bookmarks | EPUB text, declared contents/page lists, exact quotation lookup and its cursor, original markup, structured text, PDF text, evidence and citation export returned results. Bookmark enable/save/note/resume/export/delete/import/disable passed. These explicit actions were separate from ordinary reading. |
| Installed archive reader | 7-Zip 26.04 created and ingested an owned single-book 7z archive. Extraction and ClamAV assessment returned a checked EPUB while preserving the original archive hash. This does not replace the separate nested-volume and recovery matrix. |
| Audio, 18 commands including fixture generation and reference decoding | Installed FFmpeg/FFprobe checked synthetic MP3/M4B files; MP3 frame ranges, MP4 units and original packets returned. Opt-in PCM matched an independent full sequential FFmpeg reference over all 88,200 mono samples at 44,100 Hz. Returned and continued samples matched their exact intervals. A WAV was saved explicitly; audible playback was not exercised. |
| MCP, two actual stdio processes and 12 calls | Scope violations and unavailable write tools were refused. Decoder tools appeared only with the independent opt-in; ordinary content tools still refused decoding. The native decoded resource matched CLI data. Library modes and bytes stayed unchanged. |
| Verified-copy handoff | Two actual `library open --app` launches passed an owned Go probe a read-only copy with the exact holding hash, reused that verified copy and preserved library modes and bytes. This validates launch arguments and copy behavior, not graphical reader operation or consumption. |
| Inventory, catalogs and collections, 11 CLI calls | Default/hash/PAR2-requested inventories, an outside-root snapshot, snapshot audit/list and managed relationships passed on owned fixtures. A one-item collection resolved the exact acquired selection as held; diff, acquisition preview and apply skipped a new transfer. Preview left missing intake missing, and source/library modes and hashes stayed unchanged. No protected-file verification or repair was inferred from the PAR2 request. |
| Bounded discovery checks | One-result Open Library and Archive searches passed. Gutenberg search and row evaluation used the local catalog subset. Explicit `arxiv:1706.03762v7` evaluation passed. These metadata checks did not acquire bodies or establish rights, completeness or checked publication. |
| Selected live acquisition, seven CLI calls | Explicit `gutenberg:1342`, file `1342.epub.noimages`, was evaluated and acquired from `gutenberg.pglaf.org` only. The transfer and installed scan completed as untrusted intake. Preview/apply imported its bound scan as checked; a source-bound evidence read and whole-library check passed. The original intake hash stayed unchanged. This selected result does not establish every provider, interruption/resume path or semantic completeness. |

Doctor found installed ClamAV with signatures updated on 2026-10-23, 7-Zip,
FFmpeg and FFprobe. EpubCheck was unavailable; no EpubCheck validation is claimed.
Defender was unavailable on Linux. The scanner reported no detections for the
selected synthetic EPUB, which is coverage evidence rather than a safety guarantee.
A separate positive check of the harmless standard
[EICAR fixture](https://www.eicar.org/download-anti-malware-testfile/) returned
scanner findings and an unsuccessful CLI result, preserving its exact 68 bytes.
The fixture was never executed; no remediation, quarantine or scanner allowlist
change was requested. This checks detection reporting, not detection of every threat.
Installed FFplay n9.0.2 also accepted the returned mono 44,100 Hz WAV through
SDL dummy audio/video backends, using documented
[headless and exit controls](https://ffmpeg.org/ffplay.html) and the
[audio-driver selection](https://wiki.libsdl.org/SDL2/SDL_HINT_AUDIODRIVER).
It exited successfully and left the WAV hash unchanged. This checks a player
accepting the range; physical audio hardware and audible playback remain untested.
Four additional CLI checks at `78efce2` exercised missing-tool behavior.
The actual absent EPUBCheck, and scanner/decoder hidden only from each child
process's PATH, returned explicit unavailable errors. Missing decoding returned
no resource or continuation. With 7-Zip hidden the folder inventory completed,
but its archive was explicitly `not_imported`, with zero imported/checked items
and a preserved-source explanation. Inventory completion is not archive coverage.
Fixture modes and hashes stayed unchanged; installed tools and global settings
were not changed.
Native Windows/macOS tool installations, actual graphical reader/player interaction,
assistive-technology compatibility, power-loss recovery and all provider/download
combinations were not exercised by this review. Existing offline tests retain
their injected scanners/providers and adversarial recovery fixtures.

Candidate reader checks in an actual Linux tmux 3.8 terminal reproduced and
repaired hidden advancement behind help and loss of the exact word after viewing
page context. Both fixes passed focused native-cgo race tests. The terminal check
held byte 11 through help and returned to the same paused word after pages, with
disabled bookmarks and stored bytes unchanged. Exact committed hosted acceptance
remains separate. See the [reader comparison and next gaps](READER-REVIEW.md).

The next contents increment passed focused native-cgo race tests and actual
Linux tmux 3.8 interaction on synthetic EPUB/PDF holdings. The CLI reader chose
a physical PDF page, selected NAV and NCX entries independently, refused an
unresolved anchor, returned to the unchanged passage and exited on q. A selected
entry did not include the following entry's text. Complete library modes and
bytes, including disabled bookmarks, stayed unchanged. Full local canonical
verification then passed at 87.1% aggregate coverage with no reported dependency
vulnerabilities, followed by full native-cgo race, Windows cross-vet and documentation
checks. Exact committed native and hosted acceptance remain separate gates.

The contents increment subsequently passed exact committed native checks and
hosted acceptance at `b40b0d1`: [all six CI jobs](https://github.com/blisspixel/nemalo/actions/runs/40050021868)
and [Go/Actions CodeQL](https://github.com/blisspixel/nemalo/actions/runs/40050021573).
The successful Windows log matched all 32 required native boundary case identities,
with no skips or failures and the actual sharing-violation branch observed.
Its library covered/race suites took 591.023/587.120 seconds, close to the unchanged
600-second package deadline. This pass establishes that run, not a broad performance
margin or a release. No case, assertion, instrumentation, worker or deadline was reduced.

A later exact-search candidate passed focused native-cgo race tests and actual
Linux terminal interaction over the same owned synthetic holdings. It found text
outside the active declared-entry boundary, opened the exact returned match,
kept a cancelled query at its original passage, treated q as query text, and
preserved a pasted Unicode format character while displaying its escape.
Multiline paste was explicitly refused. Library modes and bytes, including disabled
bookmarks, stayed unchanged. Full canonical verification then passed at 87.1%
aggregate coverage with no reported dependency vulnerabilities, followed by full
native-cgo race tests, Windows cross-vet and documentation checks. Exact committed
native and hosted acceptance of that search increment remain separate gates.

Exact search subsequently passed clean committed native terminal checks and
first-attempt [all-six CI](https://github.com/blisspixel/nemalo/actions/runs/40054015318)
and [Go/Actions CodeQL](https://github.com/blisspixel/nemalo/actions/runs/40054014812)
at `ec0f64b`. The Windows log matched all 32 boundary identities, with no skips or
failures and the actual sharing-violation branch observed. Its library covered/race
suites took 489.448/523.698 seconds; the deadline stayed 600 seconds. Ten existing
fuzz targets also passed 20-second, two-worker runs totaling 5,772,663 executions,
covering cursor decoding, EPUB navigation/plain/structured extraction, PDF parsing,
MP3/MP4 indexes, ZIP cleanup accounting and standalone RAR4/RAR5 accounting.
The [exact receipt](https://github.com/blisspixel/nemalo/commit/ec0f64b8363c561b8b503435ea05c406e6cea0f2#commitcomment-206425547)
separates this bounded evidence from universal conformance or release.

A later collection-interface candidate passed actual Linux tmux interaction over
an owned library with disabled bookmarks. It browsed offline status and selected
details, showed the full revision hash, scrolled acquisition review, cancelled
without creating intake, and refused apply after the collection bytes changed.
A fresh deliberate confirmation acquired only `gutenberg:1342`,
`1342.epub.noimages`, through the established `gutenberg.pglaf.org` service.
The 561,102-byte result had SHA-256
`e59e587472723023b21bf380b8327858e7540b25e57affa3a68935b291e370dc`, with the
installed scanner reporting no detections. It remained untrusted intake and was
not implicitly imported. A later preview showed awaiting ingest, preserving all
packet modes and bytes. Revision comparison, a 40-column frame, restored drafts,
Alt+0 and actual Ctrl+C exit passed. Library modes and bytes, disabled bookmarks
and missing isolated configuration/state/cache folders stayed unchanged.
This candidate evidence is separate from full canonical checks and exact clean
committed native/hosted acceptance. No new provider contract, rights conclusion,
semantic completeness or release is inferred.

The collection source then passed full canonical verification at 87.4% aggregate
coverage, pinned static analysis and govulncheck with no reported vulnerabilities,
full native-cgo repository race tests, Windows cross-vet and documentation checks.
Self-review first reproduced a collision between distinct raw non-UTF-8 intake
paths in JSON-based preview intent; lossless path-byte binding fixed it with an
application regression. Exact clean committed native and hosted acceptance remain
separate from these candidate checks.

The first clean committed collection candidate later exposed a native rendering
failure during rapid narrow/wide mode changes, so it was not accepted for
publication. A passive probe reproduced it immediately without acquisition or
scanning. Terminal-stream analysis identified an old 24-row scrolling region
before a 36-row repaint; clearing alone did not repair it. Resetting the physical
region before repaint passed 20 rapid cycles. Model geometry tests preserve frame
bounds for several message orders. The [research record](RESEARCH.md#native-terminal-resize-and-scrolling-regions)
separates this native finding from general terminal support. This repair needs its
own complete final local, clean committed native and hosted checks.

The corrected source passed canonical verification again at 87.4% aggregate
coverage, with no reported dependency vulnerabilities, full native-cgo repository
race, Windows cross-vet and documentation checks. Exact committed native and
hosted acceptance remain the next gates.

Commit `6c05115` then passed [all six hosted CI jobs](https://github.com/blisspixel/nemalo/actions/runs/40059525502)
and [Go/Actions CodeQL](https://github.com/blisspixel/nemalo/actions/runs/40059525422).
All 32 Windows boundary cases passed without skips or failures, with the actual
sharing-violation branch observed. Windows library covered/race suites took
383.892/400.211 seconds; bounds and production durability were unchanged.
An additional adversarial input check subsequently found that a text commit
spelling `enter` could be mistaken for confirmation. Synthetic cases reproduced
it across collection, file/recording acquisition and library write reviews. An
enhanced keyboard-protocol event also reproduced unintended intake creation in
an owned native fixture before physical Enter. Review handlers now match physical
keystrokes through the pinned library API, preserving ordinary key controls.
This correction has its own final-source verification gate; the earlier hosted
pass does not establish it.

The confirmation correction then passed the complete verifier again at 87.4%
aggregate coverage, no reported dependency vulnerabilities, full native-cgo race,
Windows cross-vet and documentation checks. The native associated-text event now
preserved missing intake and the active review; a later physical Enter performed
the same selected acquisition and installed scan. Source acceptance still requires
its own clean committed native and hosted checks.

### Accepted collections and expanded Linux review

The collection, resize and physical-confirmation increment was subsequently
accepted at `8d7d356`: [all six CI jobs](https://github.com/blisspixel/nemalo/actions/runs/40061329811)
and [Go/Actions CodeQL](https://github.com/blisspixel/nemalo/actions/runs/40061329271)
passed on their first attempts. All 32 Windows boundary identities passed without
skips or failures, including the actual sharing-violation branch. The
[read-back verification receipt](https://github.com/blisspixel/nemalo/commit/8d7d3561aaaf1036cbee820b0fc0a43198b5d73c#commitcomment-206433228)
records exact native terminal checks, selected acquisition and source preservation.
Windows library covered/race durations were 592.904/572.579 seconds against the
unchanged 600-second package deadline. The earlier 383.892/400.211-second run
shows substantial variance; the pass does not establish a comfortable performance
margin. No case, assertion, instrumentation, worker, deadline or production
durability was reduced.

A fresh expanded review on the same date used the exact clean `8d7d356` binary
(SHA-256 `a3a73f1d91023223e636db04dc9818bd7040cef0020b6c6228120617f77269ca`),
with new synthetic fixture roots outside the checkout and isolated configuration,
state and cache. It performed 70 CLI invocations, three additional FFmpeg fixture
and reference commands, two MCP server processes and 12 real MCP calls:

| Feature family | Fresh observation |
| --- | --- |
| Library/content/bookmark lifecycle, 40 CLI calls | Import preview/apply, installed rescan, source-bound EPUB/PDF reads, navigation, exact finding, citation, explicit bookmark lifecycle, export, cleanup, raw restore, fresh purge and installed 7z ingest passed. Unsupported neighbors and all original sources stayed preserved. An unscanned PDF remained in review. |
| Audio, 15 CLI calls plus three FFmpeg commands | MP3 frames, MP4 units/original packets and explicit decoded ranges passed. Full PCM and selected/continued samples matched a separate sequential FFmpeg reference: 88,200 mono samples at 44,100 Hz. Ordinary calls required explicit decode permission and source bytes stayed unchanged. |
| Two actual stdio MCP sessions, 12 calls | Default and separately enabled decode tool sets, scope denials, absent write/network/bookmark tools and exact CLI/MCP PCM equality passed. Library modes and bytes stayed unchanged. |
| Missing tools, four CLI calls | Actual missing EPUBCheck and isolated child-PATH scanner, decoder and archive-reader refusals remained explicit, without derived payloads, continuations or changed fixture bytes/modes. No global environment or installation changed. |
| Verified-copy handoff, two CLI launches | A controlled Go child observed exact read-only bytes outside the managed store; repeat handoff reused the copy. Library modes and bytes stayed unchanged. This establishes process handoff, not graphical rendering or consumption. |
| Evidence, citations and detections, nine CLI calls | Exact bounded replay matched its original response; changed selection and malformed token returned no payload or continuation. Citation and evidence metadata digests agreed. Installed ClamAV detected a locally generated harmless EICAR marker both directly and embedded in a valid EPUB. Import preserved the EPUB source and held it only in review with `scan_findings`; integrity checking reported zero checked assets. Acquisition quarantine was not exercised by this probe. |
| Actual Linux terminal reader | Contents and physical PDF page choice, NAV/NCX boundaries, exact whole-book finding, Unicode input, paste refusal, help/pause, word/page context, resizing, disabled bookmarks and actual exit passed. Modes and stored bytes stayed unchanged. |
| Recovery under native cgo race instrumentation | Actual child-process termination released the writer lock and initialization resumed. Import crash recovery and every selected cleanup/purge interruption passed, with 15 passing root/subtest observations. These are synthetic filesystem/process tests, separate from CLI workflow counts or a power-loss test. |

Installed tools were ClamAV 1.5.4 with local daily signatures 28162, 7-Zip 26.04,
FFmpeg/FFprobe n9.0.2 and tmux 3.8. The plain EICAR text remained an unsupported
assessment format while its antivirus result separately reported `findings`.
Full canonical verification, native-cgo repository race and Windows cross-vet
passed again. This review made no live provider requests and is not a new security
audit. Native macOS/Windows installed-tool workflows, graphical readers, audible
playback, assistive technologies, power-loss recovery, every hostile format and
all provider/download combinations remain outside this fresh Linux observation.

### Terminal citation and evidence

Stage 5's adapter is implemented under
[decision 0090](decisions/0090-terminal-cited-evidence.md). Synthetic native-cgo
race checks cover EPUB, PDF, declared entries and MP3 ranges, exact replay,
malformed/changed references, torn metadata, snapshot refusal, citation without
stored content, units/failed-read/scope refusals, cancellation and late replies.
Complete library modes and bytes, including disabled bookmarks, stay unchanged;
an injected scanner and decoder fail the test if invoked. MP4 stays units-only.
Shared presentation retains the complete portable sidecar and inert untrusted
text, with the actual quotation before the long token and metadata.

Actual Linux tmux 3.8 interaction passed F2 citation and its sidecar, F4 evidence,
physical F3 replay, inert associated-text controls, rapid 40/80/116-column
resizing, return to the unchanged excerpt, exact continuation from bytes 4096 to
8192, cancellation/close and actual Ctrl+C exit. It made no network or scanner
request and preserved owned source/library bytes and modes. The broader existing
terminal reader probe also passed on that candidate binary. Two new application
view captures use a public-domain validation copy, with no private paths.

Canonical verification passed at 87.5% aggregate coverage with no reported
dependency vulnerabilities, followed by full native-cgo repository race,
Windows cross-vet and local link/anchor checks. Exact clean committed native
and hosted acceptance are separate from these candidate observations, native
macOS/Windows installed-tool workflows and the broader host/accessibility matrix.

### Native executable lifecycle acceptance

The Go harness in [decision 0091](decisions/0091-native-executable-lifecycle-acceptance.md)
adds 39 cases against an explicitly selected clean cgo-free executable. It checks
native target/build identity and a verified private executable copy, creates only
fresh outside-checkout fixtures, bounds command time and output, and retains a
versioned receipt, fixture hashes and diagnostics. Existing output and checkout
aliases are refused. Module-root discovery also protects invocation from a source
subdirectory. A directory capability is kept separate from the public path used
by child processes, with a workspace identity check before each launch.

The candidate harness passed all 39 real CLI commands on Linux against a clean
`3220d7f` baseline binary: import preview and changed-plan refusal, idempotence,
text/navigation/evidence/replay/citation, explicit and disabled bookmarks,
verified export, ordinary ZIP ingest, integrity, cleanup preview and changed-plan
refusal, conflict-preserving raw restore, fresh purge and wrong-plan refusal.
Restore reported purged copies instead of reconstructing them. Both holdings
stayed unscanned and in review; integrity verified their bytes without claiming
checked publication. Original intake, uncertain neighbors and disabled bookmarks
remained intact. External tool discovery was disabled, and a requested scan
reported unavailable. No simulated scanner was used by the executable harness.

Offline tests drive the same scenario through real CLI/application services with
injected external adapters and verify failure stops later commands and mutation.
Other tests cover build identity, workspace aliases, malformed envelopes, fresh
result decoding, output limits, cancellation and retained failure reports. The
focused native-cgo race checks and Windows cross-vet passed. Full canonical
verification then passed with 87.3% aggregate coverage and no reported dependency
vulnerabilities, followed by native repository race, Windows cross-vet, local
links/anchors and whitespace checks. Actual execution of the new step on all three
native hosted OSes and exact committed acceptance remain separate gates. Existing recovery/boundary
tests, actual installed-tool evidence, host/plugin calls, interactive terminals
and release installation are not replaced by this harness.

The first hosted execution at `74ab937` passed the new 39 cases on Linux and
macOS, but Windows's missing-scanner case returned success rather than the
required refusal. Windows canonical verification, race, smoke and all 32 boundary
cases passed. This is a failed whole-wave gate, not native lifecycle acceptance.
The [isolation correction](decisions/0091-native-executable-lifecycle-acceptance.md#hosted-isolation-correction)
keeps every case and assertion and requires separate final verification.
Its final local canonical verifier passed with 87.3% aggregate coverage and no
reported dependency vulnerabilities, followed by full native-cgo race, Windows
cross-vet and link/anchor checks. The candidate harness also passed all 39 actual
commands against the clean `74ab937` Linux executable. A new committed executable
and the complete exact hosted gate remain separate from those observations.

The exact clean `74ab937` Linux binary also passed a new machine review in fresh
synthetic roots: 70 CLI calls, three FFmpeg fixture/reference commands, two stdio
servers with 12 MCP calls and two actual tmux terminal probes. Networking was
disabled and only owned output writable. Installed ClamAV 1.5.4, signatures 28162,
7-Zip 26.04 and FFmpeg/FFprobe n9.0.2 exercised rescan, archive intake, all 88,200
reference samples, exact returned/continued intervals, evidence replay, missing
tools and verified-copy handoff. Harmless direct/embedded EICAR detections stayed
findings; the detected EPUB remained in review with zero checked assets and an
unchanged source. Reader help and page/word return preserved unit 0, byte 11;
citation/evidence controls, resize, continuation, actual exit and unchanged stored
bytes/disabled bookmarks passed under tmux 3.8. These local observations do not
substitute for the failed Windows gate, installed tools on other platforms,
graphical readers, audible playback, assistive technologies or real host calls.

### Native terminal acceptance

The optional `--terminal` extension to the executable harness is implemented
under [decision 0092](decisions/0092-native-terminal-acceptance.md). It retains
the original 39 CLI cases and adds three separate PDF setup commands and 49
current-screen observations across EPUB, PDF and TUI sessions. Real native
transport tests cover terminal handles, query responses, resize, normal and
unsuccessful exit, cancellation, worker joins and exhausted input/output limits.
The oracle reads current cells rather than historical terminal output.

The first Linux probe against the accepted `d6ab8d8` executable passed all 39
CLI cases, PDF setup and the first 13 EPUB terminal cases, then exposed clipped
source coordinates at 40 columns in speedreader mode. A regression test failed
before the presentation fix: narrow word mode now reserves a separate row for
pacing status, leaving exact source coordinates and notices visible. Focused
native-cgo race tests passed after that correction. Exact committed terminal
execution and the complete local and hosted gate remain required; this candidate
does not establish macOS/Windows terminal acceptance or a release.

The exact clean `97ab04a` Linux executable then exposed a harness defect, not an
application defect: the TUI reference probe pressed Tab, which changes modes,
where Ctrl+N changes the field. The corrected harness passed all 39 CLI cases,
three PDF setup commands and 49 terminal observations against that executable.
At `6f17380`, full canonical verification (including the enforced coverage floor,
pinned Staticcheck and govulncheck with no reported vulnerabilities) and full
native-cgo race passed locally. Hosted
[CI 40099532896](https://github.com/blisspixel/nemalo/actions/runs/40099532896)
passed all six jobs. Each native job ran the 39 cases and all 49 terminal frames,
through `unix-pty` on Linux and macOS and `windows-conpty` on Windows, with no
failed or skipped test; the Windows job also passed all 32 boundary tests. The
PTY/ConPTY and emulator modules are absent from the `nemalo` executable's build
metadata. This accepts the fixed terminal profile on the three hosted OSes. It is
not graphical terminal, assistive technology, real host/plugin or release
acceptance.

### Claude Code host acceptance

On 2026-10-24, Claude Code 2.1.310 on Linux ran the `plugins/nemalo/` package
against a clean `1ee0051` executable (SHA-256
`01dd1806579f42358a259ced5830ce446ef1c32d4bffb3e6186bc28b0a708bcd`) and a fresh
synthetic library from the native harness, whose 39 cases passed. The child PATH
put that executable first and excluded an older installed release. Each session
had every built-in host tool disabled and allowed only the plugin's MCP tools.

- The host's `claude plugin validate` passed for the plugin and marketplace, with
  advisory notes only (no icon, no README install line).
- With `--plugin-dir` alone in print mode, the plugin loaded at version
  `0.1.0-alpha.17` but its server did not start, and the debug log gave no reason:
  the required `scope` option was unset. Supplying `pluginConfigs` for
  `nemalo@inline` through `--settings` started `plugin:nemalo:nemalo` and loaded
  the shared skill. That route works but is not documented by the host.
- The host exposed exactly the 19 default read-only tools. Import, rescan and job
  tools (`--allow-write`), catalog and acquisition tools (`--allow-network`) and
  decode tools (`--allow-decode`) were absent.
- Two identical sessions made six actual calls each: capabilities reported
  `0.1.0-alpha.17`; status, list and `content_read` returned the fixture holding
  and a bounded `epub-text/2` range with a continuation; `file_check` on an
  unscoped fixture folder and on `/etc/hostname` returned tool errors stating the
  path is not inside the configured scopes. Results were read from the recorded
  tool payloads, not the model's summary.
- A complete before/after listing of fixture modes, sizes and hashes was
  unchanged. A first session showed host MCP logs inside the fixture because
  `XDG_CACHE_HOME` pointed there; the repeated session kept host cache outside.
- In an isolated `CLAUDE_CONFIG_DIR`, the documented
  `claude plugin marketplace add blisspixel/nemalo` cloned `1ee0051`, and
  `claude plugin install nemalo@nemalo --config scope=...` installed version
  `0.1.0-alpha.17`, stored the folder under `pluginConfigs` and produced files
  identical to the repository package apart from a host marker. The user's own
  host configuration was not changed.

This is one host on one OS with a synthetic library. The Agent Plugins package,
other hosts, macOS and Windows hosts, and interactive install prompts remain
unverified.

### Installed-tool acceptance

The `--installed-tools` mode in
[decision 0093](decisions/0093-native-installed-tool-acceptance.md) adds 13 cases
after the original 39, using only explicitly named executables. On 2026-10-24 the
candidate harness passed all 39 and all 13 on Linux against the clean `1ee0051`
executable with ClamAV 1.5.4 (signatures 28163), 7-Zip 26.04 and FFmpeg/FFprobe
n9.0.2. The child PATH was `/usr/bin` alone; there, `7z` is a 37-byte wrapper, so
its recorded hash is the wrapper's. The clean EPUB scan reported no detections
and the 68-byte EICAR file returned findings with unchanged bytes. Without the
tools, the 7z archive was listed as not imported and the decode check reported
FFmpeg unavailable; with them, the member imported as checked and decoding
passed. `mp4-pcm/1` reported 88,200 samples whose full hash equaled the harness's
own reference decode, and both 500-sample ranges matched it exactly. Archive,
source and library bytes stayed unchanged across reads and refusals.

The same review found that doctor reported 7-Zip unavailable when only `7zz`
was installed, as with Homebrew's `sevenzip` formula, although intake used it.
Doctor now uses intake's discovery; the regression test failed before the fix.
Offline tests cover selection refusals, shadowed executables, Defender roots,
case order and failures that stop later work.

Dispatched
[installed-tools run 40131545308](https://github.com/blisspixel/nemalo/actions/runs/40131545308)
passed all 39 and all 13 cases on all three hosted OSes against clean `392f21f`
executables, with ordinary
[CI 40131539453](https://github.com/blisspixel/nemalo/actions/runs/40131539453)
(all six jobs) and
[CodeQL 40131539333](https://github.com/blisspixel/nemalo/actions/runs/40131539333)
passing at the same head:

| OS | Scanner | 7-Zip | FFmpeg | Decoded samples |
| --- | --- | --- | --- | --- |
| Linux amd64 | ClamAV 1.5.4, signatures 28163 | 23.01 | 6.1.1 (Ubuntu) | 89,088 |
| macOS arm64 | ClamAV 1.5.4, signatures 28163 | 26.04 `7zz` (Homebrew) | 9.0.2 | 88,200 |
| Windows amd64 | Defender platform 4.18.26090.9-0, signatures 1.461.186.0 | 26.04 | 9.0.2 (Chocolatey) | 88,200 |

The macOS run is the first with only `7zz`: doctor reported 7-Zip available and
intake used it. On Windows the Defender `Platform` folder held a version folder
and a `CurrentVersion` junction to it. Nemalo's resolution selected the junction,
as Microsoft's documented name-descending folder choice also would, so the same
platform binary ran. Ubuntu's older FFmpeg decoded the same synthetic tone to a
different sample count than 9.0.2; each run matched its own reference decode
exactly. Decoded sample counts therefore describe one installed decoder, not a
portable fact about the file. An earlier dispatch at `b49bdaa` passed Linux and
macOS; its Windows job failed in the workflow's own folder sort, before any
case, because of that junction. Tool versions follow the runner images and are
recorded, not pinned. This is selected installed-tool behavior, not detection
coverage, every archive layout or codec, or a release.

### Terminal keyboard and cursor access

The first accessibility increment
([decision 0094](decisions/0094-terminal-keyboard-and-cursor-access.md)) found
that actions in several views had only a function key (F2 to F10) and that
text fields hid the terminal cursor. `Ctrl+O` now lists each view's function-
and control-key actions and runs one through its normal handler; fields place
the real cursor. New model tests cover per-mode contents, dispatch, refusal
while busy or in a review, physical review confirmation and wide characters.
The first native run also caught the harness's own wrong field label and an
identical-screen replay step, both corrected in the harness. On 2026-10-24 a clean exact Linux executable passed all 55 native
terminal cases, including the real cursor visible at column 84 of the focused
field and `Ctrl+O` reaching the citation report without a function key. Hosted
[CI 40135336281](https://github.com/blisspixel/nemalo/actions/runs/40135336281)
and the `main` push run
[40136786114](https://github.com/blisspixel/nemalo/actions/runs/40136786114)
passed all six jobs at `dbd0470`, so all 55 terminal cases passed under a real
macOS PTY and Windows ConPTY too. A person's screen reader, magnifier and
keyboard-only review remains required.

### Covers in real Kitty and Ghostty windows

On 2026-10-27 the `0.1.0-alpha.22` executable (`8b550bf`) was run in real
terminal windows on Linux 7.2 under Hyprland 0.56.2, against a library holding
one synthetic EPUB whose package declares a 600 by 900 PNG `cover-image`
([decision 0105](decisions/0105-local-epub-covers.md)). This was an agent-run
session, not a person's review. A small PTY relay inside each window passed
Nemalo's output to the terminal unchanged, sized the PTY to the window and sent
scripted keys (Alt+6, Enter, F6, Enter, Escape, Ctrl+C) while ignoring local
input; screenshots were taken after each key.

| Terminal | Details open | After Escape | Exit |
|---|---|---|---|
| Ghostty 1.3.1 | Cover drawn in 13 by 10 cells at the image's aspect ratio, `Cover  declared PNG, 600 x 900` | Image gone, list and side panel redrawn with no residue | Alternate screen left, terminal restored |
| Kitty 0.49.2 (official build) | Same | Same | Same |

The output contained one transmission
(`a=T,f=100,t=d,i=16,U=1,c=13,r=10,q=2`) and one deletion (`a=d,d=I,i=16`)
in each terminal. In the split layout at about 120 columns the side Details
panel is narrower than the 60-column minimum and shows no cover; the cover
appears when Enter opens details at full width, as designed. A window resize
while a cover is shown, WezTerm and mouse input were not observed here.

### Native runs on every shipped architecture

[Decision 0099](decisions/0099-native-ci-for-every-shipped-architecture.md) added
Linux arm64, macOS Intel and Windows arm64 to native CI and the installer jobs.
The first run at `2c0b459` passed all twelve jobs; the new runners' harness
executables reported `linux/arm64`, `darwin/amd64` and `windows/arm64`, each
passed all 55 terminal cases, and race passed except on Windows arm64, which Go
does not support. The installers there installed the published `0.1.0-alpha.18`.

### Parser fuzzing

The 24 fuzz targets cover EPUB inspection and extraction, cursors, navigation
lists, PDF text, MP3 and MP4 probing, catalog, edition and Gutenberg RDF
records, PAR2 metadata, ZIP extraction and cleanup proofs, ZIP segments, and
RAR4/RAR5 headers, extras, time metadata and volume accounting. Ordinary tests
replay only their seed inputs. On 2026-10-24 each target was fuzzed for 60
seconds on a 16-thread Linux machine at `62d7abb`: about 170 million executions
in total, from 0.2 million (ZIP extraction) to 17.9 million (MP4 probing), with
no failing input, crash or hang. The dispatched `fuzz.yml` workflow repeats
this on a hosted runner. Fuzzing finds inputs the targets can reach; it does
not prove the parsers correct or bound every resource.

The first hosted run,
[fuzz 40175568819](https://github.com/blisspixel/nemalo/actions/runs/40175568819)
at `de8cebe` with two minutes per target, passed 23 targets and found a real
defect in `FuzzNavigationLists` after 1.2 million executions: a 92-byte EPUB
navigation document made `parseNavList` panic with a copied non-empty
`strings.Builder`. Its frames stored label text in a Builder by value, and
growing the frame stack after label text copied it. A crafted EPUB could
therefore crash the CLI, TUI or MCP server instead of being refused. The NCX
parser had the same latent pattern without a fuzz target. Both now keep label
text as copyable bytes. The failing input is a committed corpus case, a
deterministic test panicked before the fix and passes after it, and the fuzz
target now also exercises the NCX parser; three further minutes, 8.9 million
executions, found nothing else.

## Cited evidence and citation export

On 2026-10-23, local verification of the
[evidence/citation contract](CONTENT-ACCESS.md#evidence-and-citation-export)
passed formatting, build, vet, pinned Staticcheck, offline tests, 86.9% aggregate
statement coverage and govulncheck with no reported vulnerabilities. Full native
cgo race tests and Windows cross-vet passed. Focused synthetic checks cover exact
EPUB/PDF ranges, gaps, legacy result parity, original audio clocks and payload
hashes, complete decoded-output identity, canonical replay and different requested
bounds, metadata conflicts/withdrawal/limits, shared-reader writer exclusion,
cancellation, failure discard, scopes and independent decoder permission.

The compiled CLI and an actual stdio MCP process returned identical data for
four permitted fixture selections and their exact replays: Pride and Prejudice
declared contents, Attention Is All You Need version 7 PDF page index 7, The Art
of War MP3 bytes, and a cached LibriVox MP4 decoded range. The last explicitly
invoked FFmpeg n9.0.2, returning 500 mono samples at decoder-output positions
10000..10500 in a 1044-byte WAV, with complete sequential output identity. It
does not establish original-packet, chapter or presentation mapping. File
inventories and hashes of the copied validation libraries remained unchanged.
This installed-tool/process evidence is Linux-specific; all native hosted
platform acceptance is still required.

Actual CLI citation item arrays for Pride and Prejudice, Vindication, Montaigne
and arXiv `1706.03762v7`/`v1` passed the official CSL draft-07 input schema pinned in
[decision 0087](decisions/0087-source-bound-evidence-and-citations.md), using the
existing pinned Go JSON Schema validator. Unknown bibliographic fields remained
outside the standard item array. Version 1 retained its unknown-license statement
rather than borrowing version 7's declaration. No provider, scanner or metadata
network request ran during those native calls. This run used fixture copies
outside the checkout and kept full local reports alongside them. The
[actual responses and selected semantic review](CITED-EVIDENCE-REVIEW.md) cover
all five roadmap cases, including two corrected draft presentation mistakes.
All 17 selected native CLI results replayed exactly. This same-session review
and these mechanical checks are distinct from the exact hosted acceptance
receipt on [issue 39](https://github.com/blisspixel/nemalo/issues/39).

The first candidate `66d75fb` failed Linux's hosted race job in
[run 40031395975](https://github.com/blisspixel/nemalo/actions/runs/40031395975),
in the existing nested-volume active-deadline test. Its fake lookup resumed when
the caller's Done channel closed, before the derived tree context necessarily
observed cancellation. The pinned Go 1.27.3 source closes that channel before
walking child contexts; a separate race-instrumented reproducer observed pending
children after Done and none after synchronized cancellation returned. The test
now completes caller cancellation before unblocking its fake lookup, retaining
the ten-second deadline, fixture and every original assertion. Three focused
native race repetitions passed after the change. No production archive policy,
cleanup authority, worker count, instrumentation or timeout was changed. The
repaired candidate still requires its own exact full hosted gate.

## Original MP4 packet access

On 2026-10-23, the actual CLI JSON `mp4-audio/2` path returned 829 original
packets across 22 bounded responses from five independently generated profiles:
AAC 44.1 kHz stereo with fast-start metadata, AAC 22.05 kHz mono, ALAC 48 kHz
stereo, MP3 in MP4 at 44.1 kHz stereo, and Opus in MP4 at 48 kHz mono. Source
spans, original sample-entry bytes, separate initialization/payload hashes,
decimal tick bounds and exact continuations matched the source and FFprobe
9.0.2. Edit lists were disabled for that independent raw-table comparison.
One terminal MP3 packet's codec duration differed from the table interval;
the packet contract preserves table evidence rather than equating those clocks.

For each profile, mpv 0.41.0 consumed the separate canonical read-only
`library export` copy into an owned PCM output, with scripts, saved position,
interactive input and network protocols disabled. Source, exported copy and
every library file remained byte-identical. This demonstrates whole-file player
handoff separately from packet retrieval. It does not establish a playable
packet-range container, edited timing, decoder preroll, audible quality or
whole-file PCM equivalence. Full stage 3 acceptance remains in
[issue 60](https://github.com/blisspixel/nemalo/issues/60); the
[AAC state research](RESEARCH.md#aac-range-boundaries-and-decoder-state) records
why short preroll cannot promise that equivalence.

Normal Go tests use synthetic bytes and no installed decoder, player, scanner or
network. Tests cover changed/reverted original bytes, failed and canceled private
copies, disk refusal, scratch/handle lifetime, initialization bounds, packet and
byte stops, large tick precision, forged continuations and CLI/MCP selector and
scope boundaries ([decision 0085](decisions/0085-verified-mp4-packet-access.md)).
Native fixture evidence is separate from the required exact hosted CI gate.

## Explicit decoded MP4 access

On 2026-10-23, the actual `mp4-pcm/1 --decode` CLI measured units and 48 sample
selections across eight generated AAC/ALAC/MP3/Opus profiles, including AAC noise
substitution enabled/disabled and a tone. Eight additional cursors resumed at
the exact next sample. Whole-output hashes/counts and selected PCM matched a
separate full sequential FFmpeg 9.0.2 reference. Forty nonempty public WAV
responses played through mpv 0.41.0 to identical raw PCM; past-end requests
returned no usable data. All sources and every synthetic library file stayed
unchanged.

The already cached LibriVox `archive:art_of_war_librivox` file
`ArtOfWar-64kb_librivox.m4b` was exercised through the actual CLI/player, without
another download or scanner. Its 18,084,624 source bytes had SHA-256
`8797750c9f8d86f8f013c251bbc9da5570ce5c82a2d3a5abe3da47a2c45c9b01`.
Complete output measured 95,659,008 samples at 22,050 Hz, with PCM hash
`e8a4c381e8174d214a219068177b043a856386ade0ceb4efb0ddcdbace8cd672`.
Four bounded WAV ranges, including exact continuation, a nonaligned middle
selection and the tail, matched a separately streamed full reference and actual
mpv output byte-for-byte. The reference retained only those bounded intervals
while hashing the full stream, without a whole-waveform allocation. Source and
library files stayed unchanged. Existing provenance and unresolved file-level
rights/safety caveats remain; this is not a new rights determination.

These tests establish native Linux output for the installed tools and exercised
profiles. They do not establish universal codec/player conformance, another tool
version's output, exact original-packet/chapter/edit mapping, semantic/audible
completeness or a release. Actual complete output binds
the representation so changed measurements refuse stale cursors.

Normal tests use synthetic bytes and injected adapters or test-binary children,
never installed decoders, players, scanners or a network. Tests cover whole-stream
and range hashes, WAV/sample boundaries, raw 64-bit declarations, late failures,
odd/partial output, quotas/cancellation, missing tools, stale references, copied
source binding and explicit CLI/MCP permission/scope boundaries. The canonical
verifier passed with 86.8% aggregate coverage and no reported Go vulnerabilities;
the complete native-cgo race suite and Windows cross-vet passed. Link/anchor and
whitespace checks passed. Native public CLI/reference/player checks were repeated
on the final implementation.

The accepted head `5091ce7` passed
[all six hosted jobs](https://github.com/blisspixel/nemalo/actions/runs/40017090664)
and [Go/Actions CodeQL](https://github.com/blisspixel/nemalo/actions/runs/40017090072).
Native canonical verification, race and cgo-free binary smokes passed on Linux,
macOS and Windows, as did all three installers against the published prerelease.
The full Windows log passed exact RUN/PASS checks for 32 boundary cases: all
11 reparse roots and descendants, both private-snapshot roots and seven failure
cases, without skips. Windows explicitly denied replacing the open private file;
the adapter's proposed payload was discarded and private bytes remained unchanged.
A separate same-bytes/different-file fixture exercised named/retained identity
refusal on every platform. The earlier Windows fixture-assumption failure remains
recorded in [decision 0086](decisions/0086-explicit-decoded-mp4-access.md#native-verification-correction).
The [stage receipt](https://github.com/blisspixel/nemalo/issues/60#issuecomment-6374231710)
was read back exactly while issue 60 was open, before closure. This accepts the
bounded stage 3 source contracts, separately from release/platform-player gates.

## Acquisition harness

`cmd/collection` is Go validation tooling over `internal/collection`, not the
production acquisition manager. Keep the actual collection outside the Git checkout.
Only the manifest, code, tests, and documentation belong in Git. No public provider
is contacted by unit tests or CI.

For an explicitly chosen external directory, with its report parent already present:

```sh
go run ./cmd/collection --manifest collections/foundations.json \
  --destination /srv/nemalo-validation/intake \
  --report /srv/nemalo-validation/acquisition-001.json \
  --index /srv/nemalo-validation/reading-list-001.md
```

Choose a new report filename for each run. The harness never overwrites reports.
The optional reading list links acquired assets and records selection context;
generating it does not open a reader or promote intake to checked library status.
The run has one transfer at a time, a 5 GiB transfer budget, a 256 MiB per-file cap,
five-minute HTTP deadlines, identified requests, a three-second interval between
asset requests, fixed provider hosts, and checked HTTPS redirects. It uses an official
Gutenberg mirror rather than automating ordinary search pages. Most selected EPUBs
omit optional illustrations to reduce transfer and image processing. Image-dependent
editions need a separate selection. Existing completed assets are rehashed and
reinspected without downloading them again. Changed or unreceipted files are
preserved and rejected, not overwritten.
Archive delivery redirects admit only the observed numbered `ia`/`dn` host patterns
under its US/Canadian domains. Other hosts fail closed pending source verification.

Each asset gets a SHA-256 receipt with the selected and final URLs, measured size,
acquisition time, and separate assessment fields. This hash identifies the acquired
bytes; it is not a provider signature or proof that the source was uncompromised.
The selected source metadata is preserved beside the local collection when running
the real validation exercise. It is not a second production catalog.

## What the checks establish

- EPUB: ZIP signature, bounded member names/count/expanded size, CRC and lengths,
  required mimetype placement, container/package XML, manifest member existence,
  and spine references. Remote manifest resources are flagged and never fetched.
- PDF: header and end marker only. No complete PDF parser or active-content audit.
- MP3: candidate signature only. No complete frame decoding, playback, or audio
  quality review. All tracks exposed by each selected recording record are required.
- All types: measured SHA-256, transfer accounting, and recheck of existing bytes.

EPUB limits are 10,000 members, 32 MiB per expanded member, 128 MiB total expansion,
1 MiB container XML, and 2 MiB package XML. CRC passes do not imply malware safety.
EPUB conformance, content quality, antivirus, paper peer review, and semantic/audio
completeness remain separate evidence. The acquisition report says `not_scanned`.
Any later external antivirus run gets its own dated report; it does not rewrite
historical acquisition receipts or guarantee safety. Scanner privacy follows its
own configuration and must be disclosed separately from Nemalo's network behavior.

One LibriVox record declares 13 sections but exposes 14 tracks. The manifest retains
all 14 and the discrepancy. Papers are repository versions; metadata does not prove
peer review. Eight current OAI records expose license URIs, retained as evidence;
two older records expose no license. A current-record declaration is not proof of
rights for every pinned version. Local reading is not permission to redistribute.
Gutenberg and LibriVox declarations use US copyright boundaries,
not a global public-domain assertion. Embedded notices and attribution remain intact.

## Recovery and production boundary

The harness uses root-scoped files and an exclusive writer lock. Per-file writes
are synced before rename, followed by a synced receipt. A process or power failure
between these steps can leave an unreceipted asset or partial file. A stale lock
requires inspection after confirming the writer is gone. Preserve ambiguous files
for review; do not remove them or manufacture a receipt to make a retry pass.
The immutable report is finalized on normal completion, including failed assets;
a forced termination may leave an empty report. Receipts remain the per-asset facts.

No HTTP range resume, durable production journal, automatic retry, metadata refresh,
archive extraction, checked publication, reader handoff, or source deletion is
implemented here. These remain roadmap work through the shared application core
and CLI/TUI. Synthetic tests exercise failure boundaries; real provider runs prove
only the particular bytes and checks recorded at that time. Public CI success
does not validate live provider availability or certify this collection.

## Recorded exercise: 2026-10-08

The Windows/amd64 run acquired all 100 selected resources: 80 EPUBs, 118 MP3
tracks across 10 recordings, and 10 PDFs. The 208 content assets total
1,046,965,564 bytes. All passed the limited checks above; completed assets were
rehashed on retry. Three Archive HTTP 500 failures succeeded in a subsequent run
without redownloading completed files. No partial files or writer lock remained.

An external Windows Defender custom scan of the intake completed with exit code 0
and output reporting no threats. Remediation was disabled, and existing machine
cloud/sample-consent settings were unchanged. This is scanner evidence for that
local run, not evidence that Nemalo performed that initial scan or a safety certification.
The later shared `check FILE --scan` adapter was separately smoke-tested against
one EPUB with Defender and reported `no_detections_reported`. Historical
acquisition receipts remain unchanged.
EPUB conformance, full PDF validation, MP3 decoding, and editorial review remain
unperformed. No downloaded content, personal paths, or local scanner logs are in Git.

The local reading list, selected source snapshots, asset receipts, failed-attempt
reports, successful acquisition report, and scanner report live with the external
collection. Gutenberg files can be regenerated; a URL-based manifest selects
resources but cannot reproduce historical bytes without preserved assets/receipts.
Manifest SHA-256 for this exercise:
`623c8101bacb698cb6864e336852244a7a2c562ab9b7eddeb649a316e2ea01cc`.

A subsequent CLI file-health pass checked all 208 content assets using the shared
assessment service. Every result was `limited_checks_passed`, with no active-content
indicators reported by these limited checks. The 80 EPUBs contained measured HTML
body text, from 22,162 to 2,657,494 non-whitespace characters per EPUB, totaling
40,824,129. These are measurements, not expected-length or completeness verdicts.
EPUB pages, PDF page trees, media decoding, and semantic completeness remain unknown.
The health report is local to the collection and does not overwrite scan evidence.

The shared library snapshot command subsequently cataloged the external intake:
416 unique byte assets/locations, comprising 208 content files and 208 acquisition
receipts. Optional assessment recorded health for all 208 content assets, including
titles/languages for all 80 EPUBs. Snapshot source reads totaled 2,094,080,564 bytes
across inventory hashing and assessment copies. A fresh explicit-root preservation
audit found all 416 file locations unchanged with no findings. A literal title
query retrieved the Italian edition of Dante's *Divina Commedia*. Catalog/audit
reports remain outside Git. No new antivirus scan, reader handoff, content mutation,
or checked publication was performed by these operations.

## Repeat validation and discovery expansion: 2026-10-08

The original 100-resource selection was rechecked against its receipts with zero
transferred bytes. The previous 416-location catalog audit was complete and found
no changes before expansion. No original asset or receipt was replaced.

The [discovery validation selection](../collections/discovery-validation.json)
then added four distinct resources: *Little Women*, the Danish novel *Tine*,
*The Happy Prince and Other Tales, Version 2*, and the versioned preprint
*Coded MapReduce*. Its eight assets comprise two EPUBs, five MP3 tracks, and one
PDF, totaling 51,625,325 bytes. The external collection now contains 104 resources:
82 EPUBs, 11 recordings with 123 tracks, and 11 papers, across 14 declared languages.
The 216 content assets total 1,098,590,889 bytes. Downloads remain outside Git.

The exercise used the following distinct source boundaries:

| Source | Exercised behavior | Product boundary |
| --- | --- | --- |
| Open Library | Native CLI search for Alcott and book metadata | Discovery only |
| Internet Archive | Native search, item evaluation, distinct-page checks, offered audio files | Expansion used the harness; selected-file CLI/TUI acquisition is now implemented |
| Project Gutenberg | Official offline CSV selection, per-book RDF, mirror EPUB acquisition | Native provider search remains planned |
| arXiv | Official API title search and pinned-version PDF acquisition | Native provider search remains planned |
| LibriVox | Three title-search probes returned not-found errors | Not a successful search integration; new audio selected from Archive's exposed LibriVox item |

Real pagination testing found and fixed an Archive adapter defect: `start` echoed
the requested offset while repeating the first documents. Document-ID comparison
now proves disjoint aligned pages and correct unaligned offsets using `page`/`rows`.
Offline tests cover adjacent-page consistency, duplicate IDs, and changing totals.
Failed probes and the pre-fix response remain local evidence, not successful checks.

Every content asset was checked through the actual CLI with its receipt's expected
bytes and SHA-256. All 216 reports were `limited_checks_passed`, with no findings.
All five new audio files also matched Archive's declared size and SHA-1. These
source checksums are integrity declarations, not cryptographic authentication.
Explicit Nemalo `--scan` checks of one EPUB, one MP3, and one PDF returned
`no_detections_reported`, bound to their private snapshots. A separate fresh Defender
directory scan completed with exit code 0 and no threats reported, with remediation
disabled and host cloud/sample policy unchanged. Receipts were not rewritten.

An already-installed FFmpeg/ffprobe diagnostic independently probed and decoded
all 123 audio tracks to a null output, sequentially with bounded processes. All
decoded without reported errors; probed durations sum to 36.56 hours. This is
additional local validation evidence, not a Nemalo decoder feature, a dependency,
listening review, or proof that every recording is semantically complete. PDF
parsing and EPUB conformance remain unperformed. No content was executed or
handed to a reader/player.

A fresh assessed catalog and audit accounted for 432 unchanged file locations:
216 content files and 216 receipts. The expanded collection exceeds the default
1 GiB inventory budget, so the explicit 5 GiB option was exercised. Local holdings
retrieved *Tine* with language `da`; exact format filtering returned 82 EPUB assets,
and MP3 pagination exposed all 123 tracks, including the final 23 after offset 100.
A receipt containing `.epub` in its name is no longer included by the EPUB format
filter. The filename filter remains separate from health evidence.
Negative CLI exercises rejected a wrong expected hash, insufficient hash budget,
and an attempt to overwrite the saved catalog. Auditing the old baseline after
expansion reported exactly 16 added locations and no changed or missing originals.

The Windows native TUI smoke exercised direct mode shortcuts, real catalog queries,
source evaluation, result scrolling, restored inputs/results, and clean quit.
Responsive light/dark/no-color and pagination behavior have synthetic tests.
The browsing refinement additionally exercised the real 82-EPUB holdings view,
live Archive results, native Windows focus/selection/detail/report scrolling, and
quit. Synthetic tests cover odd/even widths and long Japanese text; wrapped reports
retain their original evidence and allow scrolling to its end. No new assets
were acquired or rescanned for this presentation change.
The README and [terminal guide](TUI.md) images are rendered application-view frames from the real catalog and
live source results, not operating-system screenshots. Only public metadata
and relative asset paths appear. Raw reports, scanner details, copied catalogs,
and content remain outside tracked project state.

## Library control-state validation on 2026-10-08

Native Windows CLI and TUI exercises initialized a separate
`G:\Nemalo Library\Validation\managed-state-smoke` directory, outside the intake
collection and Git. Repeated initialization preserved the library ID and two
journal records. Status, the TUI's exact-root write review, cancellation, and
confirmed initialization were exercised. These checks wrote control metadata
only; they did not import, assess, open, or delete library content.

Synthetic tests cover shared readers, exclusive writers, process termination,
resuming a synced initialization intent, corrupt journals, and unsafe control
entries. This is evidence for initialization recovery only. Content publication,
import recovery, cleanup recovery, and power-loss durability remain unvalidated.

## Source-bound EPUB retrieval on 2026-10-08

Native Windows CLI/JSON listed reading-order units for five existing Gutenberg
EPUBs: *西遊記*, *The Upanishads*, *Alice's Adventures in Wonderland*, *Thus Spake
Zarathustra*, and *Introduction to Mathematical Philosophy*. Each exposed supported
text plus one or two explicitly unsupported cover/layout units. The first supported
unit was selected explicitly. Each book returned a 256-byte-bounded excerpt,
byte-identical retry, and adjacent continuation with the same source-bound identity.
Before/after SHA-256 checks confirmed unchanged originals. Reports and excerpt text
remain outside Git in the validation report directory.

The synthetic suite executes 1,365 actual retrieval calls across three fixture works
with retries, interleaved work selection, serialized/restored references, Unicode,
stanza breaks, and exact source-order assertions. Separate cases cover unsupported
gaps without silent skipping, malformed content, changed/missing sources, stale
references, budgets, and cancellation. A short local extraction fuzz run completed
12,650 inputs without a reported failure. This is bounded evidence, not exhaustive
fuzzing, conformance, safe rendering, or proof that content was read. Paper/audio
retrieval, caller-owned acknowledgement/crash handling, MCP, and the
actual external Kilo tool path remain unimplemented and unvalidated here.

## Structured EPUB access on 2026-10-08

The new representation was exercised against the same five real Gutenberg EPUBs.
All listed their complete declared spine manifests (31, 4, 14, 152, and 27 units,
respectively). Native CLI probes selected a text-bearing unit explicitly, verified
byte-identical retries and adjacent bounded continuation, retrieved original XML
through a returned part reference, and read 64 cover-image bytes through a returned
local-resource reference. Before/after source SHA-256 checks matched. Reports and
source excerpts remain outside Git; no source file or catalog was overwritten.

The initial 4096-part whole-book budget rejected Nietzsche and Russell explicitly.
The bounded implementation now permits 16384 parts, retaining the 1024-part
per-unit, 32 MiB representation-input, 1 MiB response, and time limits. All five
passed after this adjustment. This does not establish coverage for every EPUB.

The structured synthetic suite adds 758 actual retrieval/retry calls across two
works with exact concatenated text and unchanged originals. Fixtures cover linked
notes/return, whole-document and nested-anchor cycles, language/direction, poetry,
source captions/alt versus absent alt, remote unfetched images, oversized/missing
resources, resource reassembly/hash identity, table spans and MathML source markup,
known zero-width/end-of-document gaps, and partially delivered reference targets.
TUI tests exercise explicit-root staging, unit selection, repeat, switching modes,
missing-file recovery, and preventing a failed request from rebinding an old
continuation to an edited root. Terminal-control bytes remain escaped.

A short structured-parser fuzz run executed 122202 inputs without a reported
failure. This is bounded evidence, not exhaustive parser validation or safe
rendering. The terminal guide includes a real application-view read capture.
PDF/OCR, decoded audio ranges, MCP, and external caller acknowledgement/transport
integration still require their planned format/client validation.

## Selected acquisition and boundary revalidation on 2026-10-08

The current Windows CLI rechecked all 216 existing content assets against their
receipts' sizes and SHA-256 values. All returned `limited_checks_passed`. A fresh
preservation audit found all 432 locations unchanged, with no findings. These
operations were offline and did not request another antivirus scan.

Live Archive search and fresh evaluation then selected two additional files into
separate external intake packets: a Chinese *Art of War* LibriVox track (2,316,800
bytes) and the illustrated Gutenberg EPUB *Songs From Alice in Wonderland and
Through the Looking-Glass* (4,101,787 bytes). Both matched source-declared checksums,
passed the current limited format checks and read-back SHA-256 verification, and
received provenance receipts with `untrusted_intake` and `not_scanned` status.
The audio is one track, not another complete audiobook recording. The original
104-resource collection and its historical receipts were preserved.

The acquired EPUB was checked, cataloged, listed, retrieved through structured
EPUB access, and audited through the actual CLI. Its three declared reading-order
documents were exposed. These smokes establish the exercised workflow for these
particular files, not universal source availability, content completeness, rights,
safe rendering, or managed-library publication. Reports and content remain outside
Git. Synthetic regression tests separately cover directory/file replacement,
nonblocking special-file acquisition on Unix, overlapping compressed members,
lifetime parser work budgets, and parse-equivalent terminal-control escaping.

## Stage 2 archive intake and recoverable cleanup

This audit covers the bounded stage 2 contracts, including
[nested volume trees](decisions/0082-nested-volume-tree-cleanup.md). It does not
establish broad archive conformance, safety, a release, or native installed-reader
behavior on every platform. Exact-final hosted results and issue closure are
recorded in [issue 57](https://github.com/blisspixel/nemalo/issues/57).

| Acceptance item | Evidence and boundary |
| --- | --- |
| Old-style and modern RAR, split ZIP/7z, nested archives | Shared canonical grouping and checked adapters; isolated roles/ordinals, split continuity, raw supported framing and exact reader/output binding. Missing, encrypted, ambiguous and unsupported material stays preserved. See staging's [nested-set tests](../internal/staging/nested_volume_test.go) and [physical/reader failures](../internal/staging/nested_volume_binding_test.go). |
| Depth, work, output and cancellation | Shared four-level, member, expanded-byte, per-part, read/proof, process, diagnostic and deadline allowances, including failed work. [Budget tests](../internal/staging/nested_volume_budget_test.go) cover charged failures and active cancellation; sources remain unchanged. External decoder memory is not an OS sandbox guarantee. |
| Paths and filesystem confinement | Flat generated staging names and capability-bound opens; exact original names remain provenance. Existing replacement, alias and native Windows junction/symbolic-link tests exercise boundaries. Hosted reparse cases must pass without skips. |
| Complete member accounting and source preservation | Intake handles all seven child layouts, preview/apply/repeat and exact pending-leaf recovery. Incomplete groups retain their parts while independently verified leaves may import. [Intake tests](../internal/library/nested_volume_ingest_test.go) also require changed pending bytes to stop before other imports. |
| Recoverable cleanup and purge | Distinct proofs bind current holdings and original byte identities. [Layout tests](../internal/library/nested_volume_cleanup_test.go) cover every outer layout and child selection syntax. [Recovery tests](../internal/library/nested_volume_recovery_test.go) reuse per-part interruption, current-holding, replacement and immutable binding checks under the same three-worker and timeout gates. Fresh purge repeats the complete tree; partial recovery never reconstructs it. |
| Multiple groups and metadata-only children | [Interleaved group tests](../internal/library/nested_volume_groups_test.go) require every leaf held and bind repeated holdings to distinct selections. A [covered empty directory child](../internal/library/nested_volume_empty_test.go) beside a direct leaf still needs the new measured authority. A zero-entry ZIP remains outside the existing ZIP proof. |
| PAR2 inventory and retention | [Metadata tests](../internal/inventory/par2_test.go) and [library retention tests](../internal/library/par2_test.go) keep packet checksums, declarations and protected-file verification separate. No repair or protected-file opens are inferred from names. |

Generated native Linux fixtures outside the checkout passed 39 cleanup,
byte-exact raw-restore and fresh-purge lifecycles through installed 7-Zip 26.04,
covering all eleven outer layouts and seven child layouts compositionally, plus
a child with no file leaves. Eight hidden-service, missing-part, corrupt-header
or unheld-leaf cases retained sources. The actual accepted preceding executable
retained unfamiliar cleanup/purge plans and restored exact original bytes with
no archive reader available.

Sixteen independent constructed durable snapshots cover preceding/current
consumers, whole/partial purges, ZIP-chunk/old-RAR outer sets and RAR/chunk child
chains. The preceding executable completed with zero purged for unfamiliar
RAR-named chains and refused unfamiliar chunk syntax. Refused intents were
closed through canonical stop before raw restore; missing parts were reported
honestly. Separate current snapshots completed using immutable original evidence
without a reader or reconstruction. These constructed snapshots are distinct
from the unit tests' observed interruption hooks, and neither simulates power loss.

All normal tests use synthetic Go fixtures and injected readers/scanners. Real
user libraries, personal downloads, installed scanners and public providers were
not used for this archive validation. Installed-reader native evidence here is
Linux-specific. Native Go CI covers Linux/macOS/Windows, including race and
cgo-free smokes, with exact-final writer acceptance required in issue 57. Broader
host/reader validation and production release gates remain separate roadmap work.

## Source contracts reviewed on 2026-10-08

- Gutenberg [robot policy](https://www.gutenberg.org/policy/robot_access.html),
  [catalogs](https://www.gutenberg.org/ebooks/offline_catalogs.html), and
  [official mirrors](https://www.gutenberg.org/dirs/MIRRORS.ALL).
- LibriVox [API fields and limits](https://librivox.org/api/info) and
  [US public-domain declaration](https://librivox.org/pages/public-domain/).
- arXiv [programmatic retrieval](https://info.arxiv.org/help/bulk_data.html),
  [API terms](https://info.arxiv.org/help/api/tou.html), and
  [licenses](https://info.arxiv.org/help/license/index.html).
