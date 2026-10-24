# Nemalo

**Knowledge is power.**

Nemalo is an open-source, local-first library and discovery system for books,
ebooks and audiobooks. Find resources, evaluate sources, acquire selected files,
and care for a portable library under your control. Papers and textbooks extend
the same collection.

Built in Go, with CLI, TUI, JSON and MCP interfaces. Linux, macOS and Windows.
No telemetry or required AI service.

![Nemalo browsing library holdings](docs/images/tui-holdings.png)

## Status

**Early development, not 1.0.** Current source capabilities include:

- Discover and evaluate books through Open Library, Internet Archive and a local
  Gutenberg catalog; acquire selected ebooks, papers and audiobook recordings.
- Inspect downloads and supported archives, import into a managed library, audit
  stored bytes, and recover interrupted operations. Sources stay intact on import.
- Browse holdings and collections, observe scoped recorded jobs, open verified
  copies in external readers, and read EPUBs and PDFs with contents, exact-text
  finding and explicit bookmarks.
- Retrieve bounded, source-bound text, MP3 ranges and original MP4 packets through
  shared CLI, JSON and MCP services, with coverage gaps reported. Explicitly
  selected installed FFmpeg access returns decoded MP4 ranges as small WAV files.
- Return exact cited evidence and recorded citation metadata, including explicit
  TUI reports, with source identities, range bounds, gaps and unknown fields retained.

Published prereleases may lag the source tree. Checks provide limited evidence,
never a safety or completeness guarantee. Interface work and release gates
remain unfinished; stage acceptance is recorded in the linked roadmap and issues.
See [capabilities and limits](docs/DEVELOPMENT.md), the [roadmap](ROADMAP.md), and
[published releases](https://github.com/blisspixel/nemalo/releases).

## Install

Linux and macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/blisspixel/nemalo/main/scripts/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/blisspixel/nemalo/main/scripts/install.ps1 | iex
```

The installers verify the release archive against its published checksums.
For version pinning, uninstalling and source builds, see
[installation](docs/INSTALLING.md).

## Quick start

Acquire one public-domain book and read it. This worked example uses *Pride and
Prejudice*; use any title instead. Each command prints the next step: `search`
shows the book's ID and `evaluate` its file names.

```sh
nemalo catalog fetch gutenberg --output ~/pg_catalog.csv
nemalo search "pride and prejudice" --source gutenberg --catalog ~/pg_catalog.csv
nemalo evaluate gutenberg:1342 --files
nemalo acquire gutenberg:1342 --file 1342.epub.noimages --output ~/intake/pride
mkdir ~/books && nemalo library init ~/books
nemalo library import ~/books ~/intake/pride --apply
nemalo read ~/books --root ~/books --asset sha256:HASH
```

`import` prints the read command with the book's real hash. Run `nemalo doctor`
for scanner setup, `nemalo tui` for the terminal interface, and
`nemalo help COMMAND` for options and limits. Local intake stays offline;
explicit provider requests use the network.

![Nemalo reading a bounded source passage](docs/images/tui-reading.png)

These are application-view captures from the current Go TUI. The
[terminal guide](docs/TUI.md#application-view-captures) records their inputs and
capture method, with a discovery view and keyboard controls.

## Documentation

| Guide | Contents |
| --- | --- |
| [Installation](docs/INSTALLING.md) | Native packages, version pinning, uninstalling and source builds |
| [Operation and development](docs/DEVELOPMENT.md) | Implemented commands, configuration, capability boundaries and verification |
| [Terminal guide](docs/TUI.md) | Browsing, reading, shortcuts and application-view captures |
| [Content access](docs/CONTENT-ACCESS.md) | Source-bound text, audio, locators, continuations and gaps |
| [Acquisition](docs/ACQUISITION.md) | Selected downloads, intake receipts and recovery |
| [Agent integration](docs/AGENT-INTEGRATION.md) | MCP permissions and plugin installation |
| [Sources](docs/SOURCES.md) | Providers, rights and integration constraints |
| [Product](docs/PRODUCT.md) and [roadmap](ROADMAP.md) | Product contract, remaining work and release gates |
| [Architecture](docs/ARCHITECTURE.md) | Shared services, data protection and decisions |
| [Validation](docs/VALIDATION.md) and [research](docs/RESEARCH.md) | Measured evidence, limitations and primary references |
| [Releases](docs/RELEASING.md) | Packaging, checksums and native validation requirements |

Contributing: read [AGENTS.md](AGENTS.md), then run `go run ./cmd/verify` and
`go test -race ./...` (race checks require a native C compiler).
Security reports: see the [reporting policy](SECURITY.md).

Licensed under [Apache 2.0](LICENSE). Downloaded content retains its own rights.
