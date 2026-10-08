# Changelog

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
