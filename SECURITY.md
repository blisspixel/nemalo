# Security policy

Nemalo is an early Go library and discovery application, not 1.0. Include the
prerelease version or exact commit, operating system and affected command in a
report. Identify whether it concerns current source or a published executable;
they can differ. No long-term support or response-time commitment is established.

## Reporting a vulnerability

Use [GitHub private vulnerability reporting](https://github.com/blisspixel/nemalo/security/advisories/new)
for a suspected vulnerability. Include a minimal synthetic reproducer, the input
you control, the affected boundary, expected and observed behavior, and necessary
configuration or tool versions. Keep credentials, personal paths, private books
and unnecessary exploit detail out of reports. Ordinary feature requests and
non-sensitive defects belong in GitHub issues.

## System and trust boundaries

Review the shared Go services and their CLI, TUI, JSON and MCP adapters, local
state and recovery, provider HTTP clients, archive/content parsers, installed-tool
adapters, installers and release workflows. CLI and MCP reuse the same services.
MCP can be invoked by a separate agent host; configured filesystem, write and
network capabilities are enforced by code. Retrieved content cannot authorize
commands or change those capabilities.

Treat local and acquired files, archive records and names, metadata, URLs,
provider responses, retrieved text and subprocess output as untrusted. Important
assets include source bytes, managed holdings, provenance, journals, bookmarks,
configured credentials and the user's authority over files and network access.

## Required properties

- Inventory and access stay within explicitly selected roots. Preserve opened
  identity checks, alias/reparse protections and capability-bound file handles.
- Import preserves sources. Cleanup and purge require exact currently verified
  byte identities, complete supported container evidence, a revalidated recorded
  plan and durable recovery. Uncertain material stays available for review;
  restore never replaces an existing target.
- Parsing, decompression, transfer and subprocess work stay bounded, including
  failed attempts, cancellation and output. Downloaded content never executes.
- Local intake and source retrieval stay offline. Explicit provider operations
  enforce fixed hosts, checked destinations, redirects, pacing and transfer bounds.
- Format validity, security findings, antivirus coverage and completeness remain
  separate. Missing, skipped, limited or failed checks cannot establish a clean
  result. Positive active-content indicators require review.
- Content references bind exact bytes and extractor rules. Report changed,
  unsupported, missing and bounded material explicitly. Ordinary reads create no
  consumption history or bookmark writes.
- External reader handoff uses verified copies. Process arguments remain arrays;
  source strings are untrusted display data. Preserve independent source rights,
  edition, version, narration and provenance evidence.

## Assessing findings and limitations

Establish the attacker-controlled source, reachable operation, violated property,
preconditions and concrete consequence. Calibrate severity to that path rather
than a scanner label. A local malicious file can cross a supported boundary.
No finding classes or accepted-risk exclusions are established by this policy.

Limited container checks and no-detection scans provide evidence, not a safety or
semantic-completeness guarantee. Unsupported archive layouts remain preserved.
Installed readers, scanners, external applications and agent hosts have their own
behavior and privacy policies. CodeQL, known-vulnerability checks, coverage, race
checks and bounded fuzzing support review; they do not establish exhaustive
security assurance. See [architecture](docs/ARCHITECTURE.md),
[operation and limits](docs/DEVELOPMENT.md) and [agent boundaries](docs/AGENT-INTEGRATION.md).
