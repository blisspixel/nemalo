# Development and operation

Nemalo is an early Go CLI/TUI application. Go 1.27.2 is pinned in `go.mod`.
The launcher may download that patched toolchain when the local Go installation is
older; this does not replace the system installation. No Python or Node runtime is
needed for the application, tests, or first-party tooling.

## Current operations

`help`, `version`, `doctor`, `search`, `inspect`, and `tui` are implemented.
Open Library search returns works and original language metadata, not resolved
download permissions or file availability. Search sends only explicit query terms,
paging/field parameters, and client identification to the fixed HTTPS source.
Responses are bounded to 2 MiB, page sizes to 50, and requests to one per second
per client instance. Separate CLI processes do not yet coordinate provider quotas.
No automatic HTTP retry, arbitrary URL fetching, or bulk catalog harvesting occurs.

Inventory requires an explicit directory. It uses `os.Root`, skips links/special
files, and reports filename-based candidates without extracting or validating them.
Default limits: 10,000 entries, depth 32, 256 MiB per hashed file, and 1 GiB total
hashed input. Hashing is opt-in. Default inventory reads directory metadata only.
Limits, errors, and cancellation produce an incomplete result and nonzero exit.
No security scan has occurred; tool availability in `doctor` is not scan evidence.
Filesystem operations can still block on a stalled device; this is not a sandbox.

The TUI offers search and explicit-folder inventory using the same application
services. Tab changes mode, Enter submits, Escape cancels, PageUp/PageDown scroll,
and Ctrl+C quits. It makes no startup network request. Untrusted results and paths
are escaped before display. JSON mode uses schema version 1 and exit codes 0
(success), 1 (failed/incomplete operation), and 2 (usage/configuration error).

## Configuration

Configuration is an optional strict JSON object with `library` and `review` paths.
Both paths must be absolute and must not overlap. Unknown fields, multiple JSON
values, nonregular files, and files over 64 KiB are rejected. These are reserved
settings for upcoming library operations; current commands do not create a library.

Precedence: file, then `NEMALO_LIBRARY`/`NEMALO_REVIEW`, then command flags.
`--config FILE` overrides `NEMALO_CONFIG`, which overrides the default config path.
An explicitly selected missing file is an error. Flags can precede/follow positional
arguments; `--` ends option parsing. Flags for another command are rejected.

| Platform | Configuration | State | Cache |
| --- | --- | --- | --- |
| Linux | `$XDG_CONFIG_HOME/nemalo/config.json` | `$XDG_STATE_HOME/nemalo/state` | `$XDG_CACHE_HOME/nemalo` |
| macOS | `~/Library/Application Support/nemalo/config.json` | `~/Library/Application Support/nemalo/state` | `~/Library/Caches/nemalo` |
| Windows | `%APPDATA%/nemalo/config.json` | `%LOCALAPPDATA%/nemalo/state` | `%LOCALAPPDATA%/nemalo` |

Linux defaults are `~/.config`, `~/.local/state`, and `~/.cache` when the respective
XDG variables are absent. Relative environment paths are rejected. Reading config
and running `doctor` do not create these directories.

## Verification

```sh
go run ./cmd/verify
go test -race ./...
```

`verify` runs formatting, build, vet, `go tool staticcheck`, offline tests with all
first-party packages instrumented, an enforced 80% aggregate statement threshold,
and `go tool govulncheck`. Exact tool modules/checksums are in `go.mod`/`go.sum`.
Vulnerability checking accesses the Go vulnerability database. Normal unit tests
use local synthetic fixtures/HTTP servers and do not access public providers.

Race instrumentation needs cgo and a C compiler. Cgo-free release builds do not.
On Windows with MSYS2 installed, put its compiler directory on PATH for race tests
and set `CGO_ENABLED=1`. CI runs native Linux, macOS, and Windows checks, followed
by a `CGO_ENABLED=0` binary smoke. Review actual runs before claiming platform support.

## Next bounded work

Finish milestone 1 catalog schema, locking, and durable operation journal before
mutating user libraries. Follow with signatures/bounded archive staging and EPUB
assessment/scanning. Keep real acquisition in the first complete lifecycle; do not
turn this discovery foundation into a download-only or inspection-only product.
Add those operations to CLI and TUI together. MCP remains a scoped adapter over
the same services. Generated analysis and model/harness integrations remain post-1.0.
