# Language and stack decision

Date: 2026-10-08

Status: accepted. Go confirmed by the user on 2026-10-08.

Decision: **100% Go for first-party application code, tests, and executable tooling**,
Linux-first CLI and TUI, with native macOS and Windows targets. A non-Go exception
needs a concrete unmet requirement and a recorded decision before adoption.
The new repository contains only the Go implementation, tests, and tooling;
the earlier Python experiment is excluded.

## Workload and constraints

Nemalo unifies discovery, evaluation, acquisition, verification, ingest, organization,
preservation, content access, and automation for books, papers, and audio. It reads
untrusted archives, coordinates scanners/validators, transfers large files,
maintains provenance and resource identities, and exposes CLI/JSON/MCP services.
Correctness depends on preserving data through partial failures and recording
which checks actually completed. Discovery/acquisition are core from the outset.

There is no measured requirement for low-latency processing, zero allocation,
embedded deployment, or a custom compression/antivirus engine. We expect I/O and
external tools to account for much of a typical run, but that is a hypothesis to
measure, not a prototype performance result.

Requirements are simple Linux deployment, a maintainable CLI, explicit resource
limits, portable filesystem behavior, safe subprocess management, useful JSON,
and recoverable operations. macOS and Windows need native tests and adapters
whichever language is chosen.

## Go compared with Rust

| Consideration | Go | Rust | Implication for Nemalo |
| --- | --- | --- | --- |
| Distribution | Native executable with an included runtime; avoid cgo in release builds | Native executable; linkage depends on crates and target | Both are viable; dependency choices matter |
| Memory model | Garbage collection and runtime-managed memory | Ownership and borrowing, without GC | Rust offers tighter lifetime/allocation control; both still need resource limits |
| Core operations | Standard libraries cover HTTP, JSON/XML, hashing, processes, ZIP/TAR, common compression | Standard library plus crates cover the workload | Both can implement the pipeline |
| Concurrency | Goroutines and contexts suit bounded workers and cancellation | Threads or an async runtime suit the same design | Neither requires unbounded concurrency or a daemon |
| Untrusted input | Memory-safe Go plus assessed parser dependencies | Safe Rust plus assessed parser dependencies | Neither protects against unsafe tools, malicious content, or flawed cleanup logic |
| Filesystem protection | Root-scoped filesystem APIs are useful building blocks | Capability-style libraries and platform APIs are useful building blocks | Both need platform tests and race analysis |
| Development tradeoff | Preferred here for a relatively small orchestration CLI | Preferred when precise memory control or native processing is central | Go fits this scope; this is not a universal superiority claim |

Go includes garbage collection and concurrency machinery in its runtime; linking
C libraries changes the risk and build profile. See the
[Go FAQ](https://go.dev/doc/faq). Rust uses ownership to manage resources without a
garbage collector; see the
[Rust ownership chapter](https://doc.rust-lang.org/book/ch04-01-what-is-ownership.html).
Go's [root-scoped file APIs](https://go.dev/blog/osroot) help constrain access, but
do not by themselves establish the whole intake or cleanup security policy.

These sources describe language mechanisms. The decision is an engineering
judgment about Nemalo's workload and maintenance needs, not a published benchmark
or a claim that Rust cannot meet the requirements.

## Selected stack direction

| Layer | Initial choice | Reason and boundary |
| --- | --- | --- |
| Toolchain | Supported stable Go; pin when implementation begins | No preview-toolchain requirement |
| CLI | Standard-library flags and explicit subcommands initially | Small dependency surface; reconsider a CLI library if help/completion complexity warrants it |
| HTTP and metadata | Standard-library HTTP, URL, JSON, XML, hashing | Bounded requests, checked redirects, cancellation, no XML network resolution |
| Archives | Standard-library ZIP/TAR/compression where supported; 7-Zip adapter otherwise | No custom RAR engine or native archive linkage initially |
| Antivirus | ClamAV command adapter on Linux/macOS; Defender on Windows | Scanner capability, signatures, coverage, and failure separate from validity |
| EPUB checks | Static Go checks; optional external EPUBCheck | No Java prerequisite for basic intake |
| PDF/audio assessment | Select maintained Go parsing or an explicit external validator per policy | No hand-written full PDF engine or mandatory transcoding framework |
| Catalog | Versioned JSON and provenance records | Portable, inspectable, rebuildable; no database server |
| Operation state | Durable run journal and one-writer library lock | Recovery is required; a rebuildable catalog does not replace it |
| Config | Small versioned JSON config and explicit CLI overrides | Avoid another configuration dependency initially |
| UI | First-class CLI/TUI and stable JSON | Bubble Tea/Bubbles v2 over shared services; no web frontend or daemon now |
| Agent tools | Official Go MCP SDK over shared application services | A justified protocol dependency; no embedded LLM or agent runtime |
| Agent packaging | Agent Plugins manifests and skill text | No Node/Python wrapper, executable downloader, or general plugin loader |
| Tests/release | Go tooling, adversarial fixtures, native OS CI, checksummed binaries | At least 80% coverage plus meaningful failure/recovery tests |

Target cgo-free release builds; do not describe the whole toolchain as dependency-free.
Archive tools and antivirus databases remain separately installed capabilities.
Race testing in CI can have extra toolchain requirements. Pin third-party libraries
only after an actual need, license review, and maintenance check.

Start with explicit `clamscan` integration. `clamdscan` is a later optional adapter
for installations already running the daemon; it has different permission,
transport, and configuration behavior. See the
[ClamAV scanning documentation](https://docs.clamav.net/manual/Usage/Scanning.html).
Large files can be skipped by scanner limits; a successful exit is not enough to
mark every input scanned. Avoid destructive remediation flags and retain evidence.

Use maintained [7-Zip console builds](https://www.7-zip.org/download.html) when
additional formats are needed. A subprocess is an integration boundary, not a
sandbox; root restrictions, quotas, cancellation, and platform containment require
their own implementation and tests.

## Platform design

Linux config/state/cache follow the
[XDG Base Directory Specification](https://specifications.freedesktop.org/basedir/latest/).
Library and review folders are explicit choices, separate from application config.
macOS and Windows use native application-directory conventions.

Handle Unicode filenames, case sensitivity, reserved names, junctions/symlinks,
permissions, atomic replacement limits, cross-filesystem moves, and interruption
as portability concerns. Do not infer Linux behavior from Windows or runtime
support from a successful cross-build.

## Why not keep Python as the final stack?

The prototype and its tests capture real edge cases. Python is a reasonable
language for the workload, but the user wants a native CLI without a Python
environment. Carry forward requirements and synthetic fixtures rather than an
embedded Python runtime or a literal line-by-line port.

## When to revisit Rust

Revisit only if predictable memory use without GC becomes a primary requirement,
substantial in-process binary/document processing becomes central, or maintenance
ownership changes materially. These requirements have not been established for
the first release.

Do not introduce a mixed Go/Rust stack speculatively. If a measured bottleneck
later needs a native component, evaluate it independently and preserve CLI/data
contracts.

## Next step and change boundary

The Go foundation implements CLI/TUI, configuration, verification, read-only
inventory, and Open Library discovery. Complete durable catalog/journal state
alongside discovery/evaluation/acquisition contracts. Build an end-to-end provider
slice through the same local intake and access services, rather than a cleanup-only
scaffold or a full rewrite in one pass. The [roadmap](../../ROADMAP.md) defines gates.

Existing user library files remain untouched. The earlier experiment is not the
Go verification stack. This decision fixes the language and interfaces; individual
milestone status records which production capabilities have actually been implemented.
