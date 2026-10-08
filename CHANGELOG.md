# Changelog

## Unreleased terminal browsing and documentation

- Replace teal with an adaptive indigo/neutral palette; retain no-color focus markers.
- Add selectable search/holdings, wide-terminal evidence panes, focused details,
  complete reports, and staged Archive evaluation without automatic requests.
- Prevent wide-character report overflow with grapheme-aware wrapping and test
  odd/even terminal widths, focus isolation, selection, and evidence preservation.
- Shorten the README; move the full product contract and terminal guide into linked docs.

## Unreleased validation and terminal usability

- Revalidated the external collection and added four resources using Gutenberg,
  Archive, and arXiv; retained all source files and per-asset evidence outside Git.
- Fixed Archive pagination using documented page indexes, with actual document-ID
  tests and bounded adjacent-page handling for unaligned offsets.
- Added direct/reverse TUI navigation, per-operation drafts/results, search/holdings
  pagination, labeled forms, explicit read-budget selection, and adaptive color.
- Added CLI/TUI exact filename-format filters and title-first holdings; EPUB-only
  views exclude receipts without claiming filename validity.
- Added real-data rendered TUI images and updated validation limits/evidence.

## Unreleased

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

Production downloads, full format validation, extraction, checked publication, durable
mutable library journals/recovery, cleanup, audiobook management, and MCP are not implemented yet.
Generated analysis/workspaces and model/harness integrations remain post-1.0.
