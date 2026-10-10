# Changelog

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

## Pre-release development history

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

These entries record development before the first prerelease. Current capability limits are documented above and in docs/DEVELOPMENT.md.
