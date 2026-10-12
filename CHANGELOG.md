# Changelog

## Unreleased

- `nemalo read` and `p` or `s` in TUI Read open a full-screen reader for EPUB
  and PDF text: pages sized to the terminal, or one word at a time with
  sentence pauses, starting paused at 250 words per minute. Omitted images and
  tables are shown where they were omitted, PDF pages keep their lines, and an
  unreadable unit stops reading until you choose the next one. Closing prints
  the position to return to; nothing is recorded
  ([decision 0025](docs/decisions/0025-terminal-reading.md)).

## 0.1.0-alpha.7

Seventh native prerelease. This remains an early library, not 1.0.

- `library check LIBRARY` (MCP `library_check`; TUI Audit with a library folder)
  verifies the whole library read-only: stored and held bytes, untracked and
  partial store files, unfinished operations, and review counts with next steps.
- TUI: `d` on an interrupted packet reviews resuming it, and after a cleanup F9
  previews restoring it.
- Resume an interrupted download: `acquire ... --file NAME --output PACKET
  --resume` requests only the missing bytes with `If-Range` on the strong ETag
  recorded before the body, accepts only exactly that range, and verifies the
  whole file as one transfer. Interrupted recording tracks resume in place.
- Recoverable source cleanup: `library cleanup LIBRARY FOLDER` previews every
  entry with a disposition, reason, and plan digest; `--apply --plan DIGEST`
  moves into `<library>/cleanup/<id>/` loose files whose exact bytes the library
  holds (stored bytes verified), intake packets whose receipt binds held content,
  and ZIPs whose every member is held, through verified copies and a journal that
  resumes after an interruption. `--stop` closes a cleanup that cannot finish.
  `library restore` returns every held file without replacing anything, and
  `library restore --stop` closes a restore that cannot finish. TUI: F9
  in State. Nothing is purged. Libraries with a cleanup journal cannot be opened
  by earlier prereleases.

## 0.1.0-alpha.6

Sixth native prerelease. This remains an early library, not 1.0. Closes issues 1 and 2.

- PDF pages and text: `--representation pdf-text/1` reads one unit per physical
  page with its printed page label, text in content-stream order, and located
  gaps for images, font codes without a Unicode mapping, and pages without a text
  layer, in CLI, JSON, TUI Read, and MCP. `check` walks the page tree and records
  page count and labels; a file with only PDF header and end markers now needs
  review. Uses `github.com/digitorus/pdf` for object parsing, behind budgets.
- `content locate --text` (MCP `content_locate`) finds exact quotations in EPUB
  or PDF text and returns page or unit, offsets, and a cursor that reads each back.
- Recording manifests: `library recordings LIBRARY [--source archive:ITEM]`,
  the MCP `library_recordings` tool, and `t` in TUI Holdings list each held
  recording's tracks in declared order with exact assets, missing positions, and
  conflicts. With a source, each track's stored bytes are verified and indexed
  for probed duration, chapters, and offset in the recording. Track titles and
  declared durations are now recorded at acquisition.

## 0.1.0-alpha.5

Fifth native prerelease. This remains an early library, not 1.0.

- List and search a managed library: `library list LIBRARY` with `--query`,
  `--format`, and `--status checked|review`, in the TUI Holdings operation, and
  through the MCP `library_list` tool.
- Open a holding in the default application or `--app` through a verified
  read-only copy in the Nemalo handoff folder (TUI: `o`), and export a verified
  copy with `library export`. Stored files are never handed to readers, because
  some readers write annotations into the files they open.
- Ingest a whole folder with `library ingest LIBRARY FOLDER [--apply] [--scan]`
  (TUI: F8 with a folder): loose EPUB, PDF, and MP3 files, intake packets, and
  ZIP members extracted into private staging with path, size, ratio, and CRC
  checks, through the same import and journal. Every entry is accounted for and
  the folder is never changed. Archive members are recorded as
  `ARCHIVE!/MEMBER`, which earlier prereleases cannot read. An intake packet
  without a receipt is listed, never imported as a loose file.
- Acquire a whole LibriVox recording with `acquire archive:ITEM --recording
  FORMAT --output DIRECTORY` (TUI: `a` in Evaluate). Evaluation lists each
  item's recordings in declared track order. Tracks become ordinary intake
  packets with shared checks, scans, and quarantine; ClamAV scans them in
  batches; `recording.json` is written last. `--resume` keeps completed tracks
  and refetches the rest under new names. Ingest records each track's place,
  shown as "Track N of M", which earlier prereleases cannot read.
- Install the read-only MCP tools as a Claude Code plugin
  (`/plugin marketplace add blisspixel/nemalo`, then
  `/plugin install nemalo@nemalo`), which asks for the library folder, or as an
  Agent Plugins 1.0 package. Both include a skill that explains the tools and
  how to read their evidence. The `nemalo` executable must be on PATH.

## 0.1.0-alpha.4

Fourth native prerelease. This remains an early library, not 1.0.

- Index MP3 frames with `--representation mpeg-audio/1`: probed sample rate,
  channels, frames, and encoded duration, kept separate from declared info-frame
  counts, encoder delay and padding, and ID3 text, length, and chapters. Reads
  return whole-frame byte ranges with decoder preroll and exact sample bounds,
  over CLI, JSON, MCP, and TUI Read. Nothing is decoded. Ranges decoded bit-exactly
  against a whole-file FFmpeg decode on synthesized and LibriVox files.
- Validated the MCP server with a real host: Claude Code on Windows read one
  book, switched to another, and resumed the first from its continuation at
  the exact byte where it stopped.
- Check MCP tool paths lexically against the scopes before any filesystem
  access, so a Windows UNC path cannot trigger a network connection, and
  return one error for missing and outside paths. Skip a configured folder
  that does not exist yet instead of refusing to start.
- Bound audio indexing memory: compact per-frame records and ID3 text capped
  before decoding.

## 0.1.0-alpha.3

Third native prerelease. This remains an early library, not 1.0. It resolves
issues 3 through 8.

- Accept Internet Archive downloads redirected to `eu` data nodes, beside `us`
  and `ca`. Arbitrary archive.org subdomains stay refused.
- Keep Archive search in Archive's relevance order instead of identifier order.
  `--sort identifier` pages repeatably and `--sort downloads` ranks by declared
  download count, a popularity signal. Each result names its order. TUI F3
  cycles the Archive order.
- Make `epub-text/2` the default text representation. Illustrated documents stay
  readable; each omitted image, table, MathML, SVG, or media element is located
  as a gap with its source alt text, and human output marks it in place.
  `epub-text/1` and its continuations are unchanged.
- Scan each download once during `acquire` with the installed scanner. A
  detection is quarantined under the Nemalo state directory (or deleted with
  `on_detection`), never imported, and the item's other files and a search are
  offered. `--no-scan` opts out. A clean acquisition scan bound to the same
  SHA-256 lets the packet import as checked without scanning again.
- Add `library rescan LIBRARY [--apply]` and TUI Ctrl+R in State. It scans review
  EPUBs that lack scan evidence, with ClamAV batches that load signatures once.
- Report signature age and the platform install command from `doctor`, and end
  unscanned `check`, `import`, and `acquire` output with one next step.
- Create missing parent folders for `acquire --output`, only after evaluation
  succeeds.
- Add `scripts/install.sh` and `scripts/install.ps1`, which verify release
  archives against `SHA256SUMS`.
- Add `nemalo mcp serve`, a read-only MCP server on stdio built on the official
  Go SDK v1.8.0. Its tools call the same services as the CLI and return the
  same data; catalog tools need `--allow-network`, and every path must resolve
  inside a configured scope.
- Fix the journal check that reported an interrupted scan-upgrade import of an
  already held EPUB as corrupt state.

Validation for this release: on the six Project Gutenberg EPUBs from issue 5,
`epub-text/1` read 28 of 54 units and `epub-text/2` read all 54, locating 84
omissions. Live acquisitions were scanned by Defender on Windows and by ClamAV
1.5.4 on Ubuntu; Treasure Island was delivered from an `eu` node. On Ubuntu,
six sequential `check --scan` runs took 59.5 seconds and one batched rescan of
seven files took 17.5 seconds, detecting an EICAR test member. Libraries with
rescan journal events cannot be opened by earlier prereleases. The Defender
detection pattern is covered by synthetic tests, not a live detection.

## 0.1.0-alpha.2

Second native prerelease. This remains an early library, not 1.0.

- Import one assessed EPUB, PDF, MP3, or completed intake packet into an
  initialized library. Preview writes nothing. `checked` requires a passed EPUB
  check and a recorded no-detection scan. Other assessed files stay in review.
- Record an explicit ISBN, DOI, or Open Library work or edition key from an
  EPUB package as evidence. Each file stays under its SHA-256.
- Report the directory descriptor bridge and the recommended scanner from
  `doctor`. Windows uses Microsoft Defender. Linux and macOS use ClamAV.
- Compare a publisher SHA-256 with `--expected-sha256` when one is supplied.
  Nemalo also records the SHA-256 it measured.

A scan is recommended for a file you did not produce. Nothing is installed
automatically. A matching checksum shows the bytes are the published bytes.
A no-detection scan and a matching checksum do not prove the file is free of
malware. Cleanup, archive extraction, download resume, recording and track
identities, PDF page trees, decoded audio, and MCP remain planned. Hosted CI
runs on Ubuntu, macOS, and Windows. It does not boot Omarchy.

## 0.1.0-alpha.1

First native prerelease. This is an early library/discovery foundation, not 1.0.

- Search Open Library and Archive, evaluate source assets, and acquire selected
  Archive EPUB/PDF/MP3 files into provenance-bearing untrusted intake packets.
- Inventory, assess with optional antivirus, snapshot, browse, audit, and initialize
  library control state through shared Go CLI/TUI services.
- Retrieve source-bound EPUB text, structured parts, notes, source XML, and local
  resource bytes with exact continuation and explicit extraction gaps.
- Bind filesystem validation to held capabilities, bound compressed parser work,
  and preserve safe, parse-equivalent multilingual terminal output.
- Package cgo-free Linux, macOS, and Windows archives with SHA-256 checksums.

Resumable downloads, archive extraction, checked publication, mutable managed
catalogs, complete audiobook management, and MCP remain planned. See the
[roadmap](ROADMAP.md) and [capability limits](docs/DEVELOPMENT.md).

## Development history

### Since 0.1.0-alpha.1

### Development: scanner and checksum recommendations

- Recommend Microsoft Defender on Windows and ClamAV `clamscan` on Linux and
  macOS for a file the user did not produce. `doctor` names that default.
- A publisher SHA-256 confirms the published bytes. Nemalo records its own
  SHA-256 either way. Neither a checksum nor a no-detection scan proves the
  file is safe. Nothing is installed automatically.

### Development: Omarchy desktop compatibility

- Report the Linux `/proc/self/fd` and macOS `/dev/fd` directory bridge from
  `doctor`. Import still hard-links only inside the library directory.
- Treat `foot`, Ghostty, Alacritty, and Kitty as truecolor. Leave tmux at
  256-color. Omarchy 4.0.4 does not bind Nemalo's Alt, F2 through F8, or Ctrl+C keys.
- The checked Linux build remains the cgo-free `linux/amd64` archive. Hosted CI
  runs that archive on Ubuntu. See [development notes](docs/DEVELOPMENT.md).

### Development: EPUB package identifier evidence

- Record an explicit ISBN, DOI, or Open Library work/edition key from an EPUB
  package as identity evidence. Preserve other package identifiers as unassigned.
- Keep each file under its SHA-256. The same identifier on two files does not
  merge them, and a title match assigns no work, recording, or track.
- Leave PDF and MP3 bibliographic identifiers unread. See
  [decision 0010](docs/decisions/0010-bibliographic-identity.md).

### Development: managed single-file import

- Copy one assessed EPUB, PDF, MP3, or completed intake packet into an initialized
  library through shared services, CLI, and a terminal review that does not scan.
- Sync import intent before the copy. Resume only one trailing intent for the same
  source. Retain corrupt journals. A later import without a scan does not downgrade
  a checked EPUB.
- Store the bytes beside `.nemalo` by SHA-256. Leave a conflicting file in place,
  reject an unexpected extra hard link, and reject a store parent that is not a
  real directory before writing a new intent.
- Record `checked` only when limited EPUB checks passed and the scan reported no
  detections. Name a scan-only gap with the scanner status. Managed reads use the
  256 MiB import cap; snapshot reads stay 32 MiB.
- Work and edition identity, archive extraction, download resume, folder cleanup,
  and a full checked-library policy remain outstanding. See
  [decision 0009](docs/decisions/0009-managed-epub-import.md).

### Before 0.1.0-alpha.1

### Development: structured EPUB access

- Address the EPUB scope of issues 1 and 2 with versioned capability negotiation,
  paragraphs/headings/stanzas, inherited language/direction, and source-bound parts.
- Resolve local note/backlink references and disclose cycles without auto-following.
  Expose source alt text, captions, and bounded image-member bytes with parent/member
  hashes. Remote resources remain unfetched; missing alt text stays explicit.
- Locate unsupported tables/math/hidden content as gaps instead of flattening them.
  Add bounded original XHTML/XML retrieval, preserving table spans and source math.
- Add TUI unit selection, exact repeat/continuation, explicit-root staging from
  Holdings, escaped source lines, and session navigation without consumption records.
- Keep the existing plain-text representation available. Add hostile-boundary,
  range-coverage, note/resource, CLI/TUI recovery, and repeated-retrieval tests.
  PDF/OCR, decoded audio ranges, MCP, and external caller validation remain open.

### Development: source-bound EPUB access

- Address the first Nemalo-owned slice of issue 1 with offline content units/read
  CLI/JSON over shared services and the existing bounded EPUB container parser.
- Revalidate exact source bytes; add versioned locators, deterministic continuation,
  explicit unsupported document gaps, bounded errors, and no reading-history writes.
- Test more than 300 actual retrieval cycles, retries, interleaved works, Unicode,
  stanza breaks, stale references, changed/missing content, limits, and cancellation.
  MCP, paper/audio ranges, and external caller integration remain open.

### Development: managed library initialization

- Add explicit library init/status in CLI/JSON/TUI, stable library identity,
  native reader/writer locks, synced initialization intent/completion, and
  idempotent completion/resume without modifying existing content.
- Retain/reject corrupt, oversized, linked, replaced, or unexpected control state;
  add cross-process crash tests and TUI write-scope review.
- Promote the existing x/sys module to direct use without new modules or versions.
  Content publication, mutable catalogs, and import/cleanup recovery remain planned.

### Development: terminal browsing and documentation

- Replace teal with an adaptive indigo/neutral palette; retain no-color focus markers.
- Add selectable search/holdings, wide-terminal evidence panes, focused details,
  complete reports, and staged Archive evaluation without automatic requests.
- Prevent wide-character report overflow with grapheme-aware wrapping and test
  odd/even terminal widths, focus isolation, selection, and evidence preservation.
- Shorten the README; move the full product contract and terminal guide into linked docs.

### Development: validation and terminal usability

- Revalidated the external collection and added four resources using Gutenberg,
  Archive, and arXiv; retained all source files and per-asset evidence outside Git.
- Fixed Archive pagination using documented page indexes, with actual document-ID
  tests and bounded adjacent-page handling for unaligned offsets.
- Added direct/reverse TUI navigation, per-operation drafts/results, search/holdings
  pagination, labeled forms, explicit read-budget selection, and adaptive color.
- Added CLI/TUI exact filename-format filters and title-first holdings; EPUB-only
  views exclude receipts without claiming filename validity.
- Added real-data rendered TUI images and updated validation limits/evidence.

### Development:

- Establish Nemalo as an Apache-2.0 Go CLI/TUI application, with no Python prototype
  in the repository. Use `nemalo` as the command/integration identifier.
- Add shared read-only inventory with explicit roots, link skipping, bounds,
  optional SHA-256 hashes, and incomplete-result reporting.
- Add bounded Open Library discovery with stable IDs, paging, cancellation,
  rate control, checked redirects, and clear discovery-only availability.
- Add strict configuration/path precedence, capability reporting, and schema-versioned
  JSON results. CLI and TUI use the same application services and safe formatting.
- Pin Go and verification tools; add offline tests and a native three-OS CI workflow
  enforcing formatting, compilation, vet, Staticcheck, 80% coverage, race checks,
  vulnerability checks, and cgo-free executable smokes.

- Add a Go-only real-resource collection harness with a 100-resource manifest,
  13 languages, complete exposed audiobook track sets, bounded container checks,
  per-asset receipts, and rehashing without overwriting changed files.
- Exercise all 100 selected resources outside the checkout, including 118 audiobook
  tracks. Record container/hash evidence and an external Windows Defender scan;
  retain incomplete-attempt evidence and distinguish unperformed checks.
- Add shared file health checks in CLI/TUI: actual signatures, EPUB structure,
  measured text/documents, expected size/hash comparisons, and review findings.
- Add explicit installed antivirus adapters with bounded private snapshots,
  no remediation, hash revalidation, and unsuccessful unknown/missing scan states.
- Add portable immutable byte-identity snapshots, exact duplicate locations,
  optional historical EPUB/file-health metadata, literal holdings search, and
  explicit-root preservation audits in CLI/TUI. Save complete catalogs exclusively
  outside source roots without replacing existing outputs or mutating content.
- Add Internet Archive discovery and item/file evaluation alongside Open Library,
  with source-declared rights, access restrictions, candidate assets, and explicit
  unverified DRM/file-rights states through CLI/TUI and structured output.
- Consolidate provider metadata HTTP handling with bounded JSON, checked direct
  destinations/redirects, cancellable pacing, and Retry-After handling.
- Update the tagline to "Find knowledge. Care for it. Realize its potential."

These entries record development history. The version sections above are the release notes. Current capability limits are documented in docs/DEVELOPMENT.md.
