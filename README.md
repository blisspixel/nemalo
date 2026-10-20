# Nemalo

**Find knowledge. Care for it. Realize its potential.**

Nemalo is an open-source, local-first library and discovery system for books,
ebooks, and audiobooks, with research papers and textbooks extending the same
collection. Find resources, evaluate sources, and care for a portable library
under your control. Pronounced **neh-MAH-lo**: acquire and nurture knowledge.

Built in Go. CLI and TUI first. Linux, macOS, and Windows. Desktop and terminal
compatibility were reviewed on Omarchy 4 (Arch Linux x86_64). No telemetry or required
AI service. The agent interface (MCP) uses the same application services.

## Status

**Early development, not 1.0.** Implemented in the current source tree;
published prereleases may lag:

- Search Open Library and Internet Archive by provider relevance, or Archive by
  download count or identifier, or every source at once with `--source all`,
  each in its own section; evaluate Archive item/file metadata.
  An Open Library work lists editions. One chosen edition can name an Archive
  item through its `ocaid`. The first Archive identifier on the work is not used.
- Search and evaluate a local Project Gutenberg catalog (`pg_catalog.csv`).
  Ranking is local. `catalog fetch gutenberg` downloads that one file when
  you ask. Search does not contact gutenberg.org.
- Evaluate and acquire one explicit arXiv paper version (`arxiv:1706.03762v7`)
  from export.arxiv.org at arXiv's request pace, with its latest-version
  license applied only to that version.
- Acquire one Gutenberg EPUB named by its bibliographic record, such as
  `1342.epub.noimages`, from gutenberg.pglaf.org. `evaluate --files` lists
  the offers and does not download one. Other formats stay listed.
- Acquire a selected Archive EPUB, PDF, MP3, M4B, or M4A into exclusive untrusted intake,
  with source checksums, local checks, a scan by the installed antivirus, and a
  provenance receipt. A detection is quarantined and other files are offered.
  Acquire a whole LibriVox recording in declared track order, with each
  track's readers when its LibriVox project links back to the item, and resume
  an interrupted download or recording without fetching completed bytes again.
- Inventory explicit folders; check file structure, size, hashes, and EPUB text
  measurements. `inspect --par2` inventories bounded recovery-packet metadata
  and declared sets while retaining recovery files. A scan is recommended for a
  file you did not produce and stays
  optional: Defender on Windows, ClamAV on Linux and macOS. Pass a publisher
  SHA-256 when one exists. An installed EPUBCheck can check EPUB conformance,
  and an installed FFmpeg can decode audio in full, both only when asked.
- Save portable catalog snapshots, browse holdings, and audit changed/missing files.
- List and search a managed library, and open a holding in a reader or player
  through a verified read-only copy, so readers never modify stored files.
- Keep collections of exact selections, compare two manifest revisions offline,
  resolve them against holdings, and acquire only missing selections. Removed
  selections leave earlier holdings and annotations intact.
- Initialize library control state with locking and resumable initialization.
- Import one EPUB, PDF, MP3, M4B, or M4A, or a completed intake packet, into an
  initialized library. Preview writes nothing and shows the source evidence
  and proposed holding; a plan token binds both. Checked status requires a passed
  EPUB check, a PDF whose page tree and active content (scripts, launch and
  automatic actions, attachments) were examined and clean, an MP3 whose frames
  indexed without damage, or an M4B or M4A whose every sample lies inside the
  file, and a recorded no-detection scan; other assessed files stay in review.
  `library ingest` imports a whole folder, including intake packets and the
  members of ZIP, RAR, and 7z archives and their volume sets, including bounded
  standalone nested archives. RAR, 7z, split ZIP, and nested ZIP intake use an
  installed 7-Zip. The folder stays intact.
  `library cleanup` then moves sources whose
  books the library holds into a recoverable holding area, and `library restore`
  undoes it. `library purge` frees that space once the library holds the
  same verified bytes, after its own preview. `library rescan` scans review holdings in
  one batched command.
- Retrieve bounded EPUB text in CLI/JSON/TUI, with exact continuation, source parts,
  note references, local image bytes, and explicit extraction gaps. Illustrated
  chapters stay readable; each omitted image or table is located. List the
  book's declared contents (EPUB 3 nav and EPUB 2 NCX) and read one entry until
  its declared end. List its declared page labels, such as `iv` or `42`, and
  read from one. Several entries can share one spine file, and one entry can
  span files. PDFs read by physical page and printed label, with images,
  unmapped text, and scanned pages located as gaps. Exact quotations can be
  located by page and offset. MP3 tracks report probed and declared timing and
  chapters, and return decodable ranges; held recordings list their tracks in
  order with missing positions. Recordings from different source items relate
  by exact asset hashes in declared track order, independently of LibriVox.
  Explicit user declarations can relate different encodings or track divisions
  of one reading, with exact selections, visible conflicts, and withdrawal.
  M4B and M4A audiobooks list their timing and
  chapters and open in a player.
- Read a held EPUB or PDF in the terminal with `nemalo read`: pages sized to
  the screen, or one word at a time at a rate you control. Hard-wrapped
  paragraphs flow as paragraphs. `f` opens a source note or link and Escape
  returns to the same place. Closing names where you stopped and how far
  through you are, and records nothing.
- Save an explicit bookmark in an initialized library with `bookmark enable`,
  then `bookmark save` or `m` in the terminal. `'` restores that place. A
  changed or missing file is reported and is not remapped. A note stays
  separate from the book. Ordinary reads, repeats, and closing write nothing.
  MCP does not read or write bookmarks. A library with
  `.nemalo/bookmarks.jsonl` needs this version or newer to open its control
  directory.
- Report environment paths, tool availability, scanner setup and signature age,
  and the directory descriptor bridge (/proc/self/fd on Linux, /dev/fd on macOS)
  with doctor.
- Use CLI, versioned JSON, an interactive terminal interface, or read-only MCP
  tools for an agent host (`nemalo mcp serve --scope DIRECTORY`), also packaged
  as a Claude Code plugin and an Agent Plugins package. `--allow-write` adds
  import tools that store only the plan a preview returned, and acquisition
  and rescan as durable jobs that outlive the connection.

Cleanup of RAR, split ZIP, and nested archive sources,
and a TUI view of jobs are planned. Single-file import is not
a full checked-library policy.
File checks report limited evidence, never a safety or completeness guarantee.
See [current capabilities and limits](docs/DEVELOPMENT.md) and the [roadmap](ROADMAP.md).
Native prerelease archives and checksums are on [GitHub Releases](https://github.com/blisspixel/nemalo/releases).

## Install

Linux and macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/blisspixel/nemalo/main/scripts/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/blisspixel/nemalo/main/scripts/install.ps1 | iex
```

The installers fetch the newest published release archive, verify it against the
release `SHA256SUMS`, and install one executable to `~/.local/bin` or
`%LOCALAPPDATA%\Programs\nemalo`. Pass `--version`/`-Version` to pin a release and
`--uninstall`/`-Uninstall` to remove it. See
[decision 0013](docs/decisions/0013-release-installer-scripts.md).

With `nemalo` on PATH, add the read-only tools to Claude Code:

```text
/plugin marketplace add blisspixel/nemalo
/plugin install nemalo@nemalo
```

See [agent integration](docs/AGENT-INTEGRATION.md#plugin-packages).

## Quick start

Get one public-domain book and read it. Each command prints the next one.

```sh
nemalo catalog fetch gutenberg --output ~/pg_catalog.csv
nemalo search pride and prejudice --source gutenberg --catalog ~/pg_catalog.csv
nemalo evaluate gutenberg:1342 --files
nemalo acquire gutenberg:1342 --file 1342.epub.noimages --output ~/intake/pride
mkdir ~/books && nemalo library init ~/books
nemalo library import ~/books ~/intake/pride --apply
nemalo read ~/books --root ~/books --asset sha256:HASH
```

`import` prints the `read` command with the book's real hash. `nemalo doctor`
reports scanner setup, `nemalo tui` opens the terminal interface, and
`nemalo check FILE --scan` checks a file you already have.

To build from source instead, with Go 1.27.2 or a compatible supported toolchain,
run `go build -trimpath -o bin/ ./cmd/nemalo`; the binary is `bin/nemalo`
(`bin/nemalo.exe` on Windows). Run `nemalo help` for an overview and `nemalo help COMMAND` for a command's options and limits. Local inventory and catalog operations are offline; explicit searches
send queries to the selected provider. Sources are preserved.

![Nemalo browsing real multilingual library holdings](docs/images/tui-holdings.png)

Application-view render using the external validation catalog, not a mockup or an
OS window capture. See [terminal guide and discovery view](docs/TUI.md).

## Documentation

| Guide | Contents |
| --- | --- |
| [Product contract](docs/PRODUCT.md) | Vision, commitments, intended workflows, and format policies |
| [Roadmap](ROADMAP.md) | Milestones, release gates, and implemented versus planned work |
| [Operation and development](docs/DEVELOPMENT.md) | Commands, configuration, bounds, safety limits, and verification |
| [Terminal guide](docs/TUI.md) | Navigation, browsing, keyboard shortcuts, and screenshots |
| [Content access](docs/CONTENT-ACCESS.md) | EPUB excerpts, exact continuation, unsupported gaps, and remaining access work |
| [Acquisition](docs/ACQUISITION.md) | Selected downloads, intake receipts, and failure handling |
| [Architecture](docs/ARCHITECTURE.md) | Shared services, data protection, recovery, and decisions |
| [Sources](docs/SOURCES.md) | Provider directory, rights, and integration constraints |
| [Agent integration](docs/AGENT-INTEGRATION.md) | Read-only MCP server, plugin packages, and planned write tools |
| [Validation](docs/VALIDATION.md) | External 104-resource collection, evidence, and remaining gaps |
| [Research](docs/RESEARCH.md) | Evaluated patterns and primary references |
| [Releases](docs/RELEASING.md) | Packaging, checksums, and native validation requirements |

Contributing: read [AGENTS.md](AGENTS.md), then run `go run ./cmd/verify` and
`go test -race ./...` (race checks require a native C compiler).

Licensed under [Apache 2.0](LICENSE). Downloaded content retains its own rights.
