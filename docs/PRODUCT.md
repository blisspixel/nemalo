# Product vision and commitments

This document describes the intended product. For implemented behavior, see [development and operation](DEVELOPMENT.md).

## What Nemalo should do

**1.0 focuses on the library and finding things:** dependable discovery and
acquisition, messy-download intake, verified organization, preservation, and access
for ebooks and audiobooks. CLI, JSON, and MCP expose those library capabilities.
Model-assisted analysis, generated knowledge workspaces, and video/topic insight
workflows are post-1.0 work and cannot become prerequisites for the library release.
See the [1.0 scope](../ROADMAP.md#version-10-library-and-discovery).

The full lifecycle defines the product from the outset, even when capabilities
ship in different milestones:

| Operation | Product contract |
| --- | --- |
| Discover | Unified search across legitimate book, paper, textbook, and audio sources |
| Evaluate | Compare editions, translations, formats, languages, quality evidence, rights, availability, and DRM restrictions |
| Acquire | Resolve selected assets; manage bounded, resumable transfers and multi-source collections |
| Verify | Assess integrity, structure, malicious-content indicators, provenance, and metadata without safety guarantees |
| Ingest | Apply the same checks to existing folders, subfolders, and supported archives |
| Organize | Maintain searchable metadata, versions, editions, recordings, and careful duplicate handling |
| Preserve | Detect missing/changed assets, recover interrupted operations, and make cleanup reversible |
| Access | Open, retrieve, search, cite, and use actual local content through portable formats |
| Automate | Expose first-class CLI/TUI, JSON/structured APIs, MCP, and agent packaging over shared services |
| Expand | Add providers, formats, metadata standards, and declarative collections through deliberate extension seams |

Initial implementation requirements include:

- Accept explicit download folders, including subfolders and supported nested archives.
- Identify content using signatures, metadata, and stable identifiers.
- Assess format structure, suspicious content, and quality indicators separately.
- Run local antivirus checks and retain their results as separate evidence.
- Organize EPUBs, paper PDFs, and audiobook recordings under distinct policies.
- Detect exact duplicates and preserve potentially different editions for review.
- Preserve alternate formats, uncertain metadata, and incomplete material in review.
- Clean source folders only after their contents have a verified disposition.
- Search public catalogs and send selected downloads through the same intake pipeline.
- Preserve edition, translation, recording, chapter, license, and source details.
- Expose bounded, typed tools for agents without requiring an AI service.

EPUB remains the default ebook format. Non-EPUB ebooks go to review unless the
user enables a PDF policy. Research papers use a separate PDF policy; audiobooks
start with MP3 and M4B. Those policies arrive in staged releases, as described in
the roadmap. Conversion, OCR, playback, and AI narration are separate capabilities.

## Product commitments

- Prepare before use: inspect untrusted material, report scanner coverage and
  missing checks, and preserve uncertain content without making safety guarantees.
- Respect the user's machine: bounded CPU, memory, disk, and network work; visible
  costs and progress; prompt cancellation; no surprise background activity.
- Protect local ownership and privacy: no telemetry, covert reading tracking,
  hidden uploads, or remote crash reporting. Network actions have explicit purposes.
- Keep interfaces direct and accessible: useful help, quiet defaults, clear errors,
  keyboard-friendly workflows, and complete structured output. Simplicity must
  preserve useful capabilities and accessibility.
- Make evidence and recommendations honest: show provenance, uncertainty, selection
  criteria, and coverage gaps. No sponsored ranking, engagement optimization, or
  pressure to acquire more. Users control sources and curation preferences.
- Help people use what they collect: retrieve passages, cite exact editions,
  navigate chapters, and maintain optional local notes, bookmarks, and reading lists.
  A useful collection serves the user's questions and learning goals; download
  counts and collection size are not measures of success.

These are requirements for the product. The architecture and roadmap turn them
into explicit boundaries and acceptance checks; they are not claims of implemented behavior.

After 1.0 and the existing library roadmap priorities, the roadmap adds topic watches
and a local Markdown knowledge workspace: gather permitted information, retain sources,
and optionally build linked summaries, insights, and briefs that stay current.
Processing can use local models, a provider such as OpenRouter, or an agent harness
such as OpenCode. These are native Nemalo capabilities, implemented independently in Go.
Distillr is only a research reference, with no integration, code reuse, or runtime
dependency. See the
[post-1.0 phase](../ROADMAP.md#post-10-topic-watches-and-optional-knowledge-processing).

## Intended workflow

```text
Discover -> evaluate -> resolve -> acquire
                                      |
Existing folders and archives ---------+
                                      |
                    Plan -> stage -> verify -> organize
                                      |
                  Local collection + provenance + content access
                                      |
                      Check / preserve / recover
                                      |
                   Optional recoverable source cleanup
```

Inspection must be useful without changing downloads. Importing books must leave
sources intact by default. Cleanup is a separate, explicit action against the
recorded intake plan, and must refuse stale or changed inputs.

## Proposed command interface

These examples describe the full production design. See the
[current command reference](DEVELOPMENT.md#current-operations) for implemented operations;
language/type/provider filters and the other commands below are planned:

```sh
nemalo doctor
nemalo search "Jules Verne" --language fr
nemalo add gutenberg 4791
nemalo search "linear algebra" --type article --source arxiv
nemalo search "Don Quixote" --type audiobook --language es
nemalo add librivox RECORDING_ID
nemalo inspect ~/Downloads/books --json
nemalo organize ~/Downloads/books --library ~/Books
nemalo list --language fr
nemalo show ITEM_ID --json
nemalo open ITEM_ID
nemalo check
nemalo cleanup --run RUN_ID --dry-run
nemalo cleanup --run RUN_ID --apply
nemalo jobs list --json
nemalo mcp serve --transport stdio
```

The planned `doctor` reports archive tools, scanners, signatures, paths, and permissions.
Today's `doctor` reports configuration paths, tool availability, the directory
descriptor bridge, the recommended scanner for this operating system, and how to
compare a publisher SHA-256. It does not execute scanners.
`inspect` reports what is present without modifying source or library files; any
temporary staging it needs is bounded and reported. `organize` stages and imports
eligible assets under enabled format policies. `cleanup` shows or applies recorded
dispositions from a completed run. Current `check FILE` assesses one file;
library-wide preservation checks remain planned. An
agent must use the same validated plans and configured filesystem scope.

## Catalogs and collection growth

| Collection | Initial source direction |
| --- | --- |
| Classics and multilingual ebooks | Project Gutenberg, Internet Archive, Open Library; then Standard Ebooks |
| Open textbooks and scholarly books | OpenStax, LibreTexts, DOAB, OAPEN |
| Research papers | arXiv, Europe PMC/PMC; OpenAlex search and Unpaywall DOI resolution |
| Audiobooks | LibriVox and permitted Internet Archive audio assets |
| Broader discovery | Regional libraries, children's collections, institutional repositories, OPDS |

Search, downloadable format, and permission to reuse are distinct pieces of
information. A catalog entry is not a promise of a downloadable EPUB. Nemalo should
show the selected edition, language, format, rights information, and source URL
before adding it. It should preserve this provenance in the library catalog.

The [source directory](SOURCES.md) separates researched integrations from
candidates and external reading/borrowing destinations. A large directory does not
mean every site needs a custom adapter. Use documented APIs, allowed feeds, and
publisher-provided assets; preserve rights at the individual asset level.

## What checked means

Each asset needs separate results for format validity, static content findings,
antivirus scanning, metadata quality, and duplicate status. Scanner outcomes must
distinguish no detected threats, detections, failure, unavailability, skipped files,
and scans stopped by limits. A scan exit code alone is insufficient evidence that
every file was examined.

A valid EPUB can have missing text; a valid PDF can contain poor scans; an audio
file can play while chapters are missing. Format checks and antivirus address
different questions. Nemalo reports evidence and uncertainty, not a safety guarantee.

Unscanned or rejected files remain in review and do not enter the default checked
library. Static inspection can still run when a scanner is missing. A temporary
directory and a subprocess are not security sandboxes.

## Platform and dependency direction

The application, tests, and first-party executable tooling will use Go. Start with
the standard library; justify every module by a concrete need, including its
transitive dependencies. The official Go MCP SDK is the planned standards adapter.
Agent Plugins packaging uses manifests and skill text, not another runtime.

ClamAV, Defender, 7-Zip, EPUBCheck, and media/PDF validators are separate, declared
capabilities. Only require a tool for a policy that needs it. Missing tools cannot
silently turn an unchecked file into a checked one. PAR2 is recovery data and stays
accounted for until verification or repair is explicitly supported.

Library and review directories are user-selected. Linux configuration, state, and
cache locations follow XDG conventions; macOS and Windows use appropriate platform
directories. No fixed drive letter, Downloads folder, or home-directory scan is
part of the production default.
