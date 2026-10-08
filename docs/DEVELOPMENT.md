# Development and operation

Nemalo is an early Go CLI/TUI application. Go 1.27.2 is pinned in `go.mod`.
The launcher may download that patched toolchain when the local Go installation is
older; this does not replace the system installation. No Python or Node runtime is
needed for the application, tests, or first-party tooling.

## Current operations

`help`, `version`, `doctor`, `search`, `evaluate`, `inspect`, `check`, `library snapshot`,
`library list`, `library audit`, and `tui` are implemented.
Search defaults to Open Library work metadata; `--source archive` selects Internet
Archive text/audio item search. Neither search resolves download permissions.
Only explicit query terms, paging/field parameters, and client identification go to
the selected fixed HTTPS source. Shared metadata responses are bounded to 2 MiB
of decompressed JSON, page sizes to 50, and requests to one per second per provider
instance, with a 30-second deadline including waits. Valid `Retry-After` on 429/503
postpones subsequent requests in that instance. Separate CLI processes do not
coordinate quotas. No automatic retry or bulk harvesting occurs.

Provider metadata requests use checked direct connections and fixed-host HTTPS
redirects. Environment proxies are disabled for this boundary; proxy-only networks
are currently unsupported. Private/reserved/mixed DNS destinations are rejected,
and connections use the validated numeric address with normal TLS validation.
Metadata/rights URLs returned by a provider are not fetched. See
[decision 0005](decisions/0005-provider-search-and-evaluation.md).

Inventory requires an explicit directory. It uses `os.Root`, skips links/special
files, and reports filename-based candidates without extracting or validating them.
Default limits: 10,000 entries, depth 32, 256 MiB per hashed file, and 1 GiB total
hashed input. Hashing is opt-in. Default inventory reads directory metadata only.
Limits, errors, and cancellation produce an incomplete result and nonzero exit.
Inventory performs no security scan; tool availability in `doctor` is not scan evidence.
Filesystem operations can still block on a stalled device; this is not a sandbox.

The TUI offers search, explicit-folder inventory, file health checks, explicit
antivirus scans, snapshots, holdings, audits, and Archive evaluation using shared
application services. Tab changes mode, F2 changes the search provider, Ctrl+N
switches library form fields, and Ctrl+A toggles local health metadata in snapshot
mode. Enter submits, Escape cancels, PageUp/PageDown scroll,
and Ctrl+C quits. It makes no startup network request. Untrusted results and paths
are escaped before display. Results wrap within the terminal viewport, including
long hashes and findings. JSON mode uses schema version 1 and exit codes 0
(success), 1 (failed/incomplete operation), and 2 (usage/configuration error).

## Source item evaluation

```sh
nemalo search 'collection:librivoxaudio AND title:"Art of War"' --source archive --limit 3
nemalo evaluate archive:art_of_war_chinese_1506_librivox --json
```

Archive evaluation requires a valid namespaced item ID and interprets at most
5000 files within the shared response budget. It retains source titles, languages,
creators, dates, publishers, rights, and license strings; these are declarations.
Source file sizes/checksums, filename candidates, restriction evidence, unresolved
file rights, and unassessed DRM are separate fields. Restricted or uncertain
items/files receive no download URL. ACSM/LCPL delivery documents are not content
downloads. Other URLs are escaped candidates constructed on the fixed source host.

`complete` means bounded metadata interpretation succeeded, including a restricted
item. No candidate URLs are probed or acquired; no files are scanned or opened.
An Archive audio item is not necessarily an audiobook, and multiple formats may
represent the same tracks. Actual edition/recording identity, complete track sets,
rights policy, production acquisition, and checked publication remain planned.

## File health and scanning

```sh
nemalo check /path/to/book.epub --json
nemalo check /path/to/paper.pdf --expected-bytes 123456 --expected-sha256 HASH
nemalo check /path/to/book.epub --scan
```

Supply real known size/hash values in the second example. `check` requires one
nonempty regular file, rejects final-component symlinks, and uses a private
temporary snapshot capped at 256 MiB. Source identity/size/mtime are checked across
copying. Temporary files are removed after assessment; cleanup errors fail the
operation. No content is rendered, executed, or fetched from embedded links.

Results include actual signature, measured bytes/hash, limited container evidence,
review findings, limitations, and separate antivirus status. EPUB checks bound
members and package entries to 10,000, ZIP metadata reads to 2 MiB, expanded bytes
to 128 MiB total and 32 MiB each, and HTML tokens to 1 MiB. They check ZIP
lengths/CRC, mimetype, container/manifest/spine references,
then report titles/languages, HTML reading-order documents, images, and
non-whitespace body-text characters. EPUB is internally ZIP; a plain ZIP or RAR
does not pass its package checks. Page count stays unknown. Text metrics are not
DOM validation or proof of semantic completeness. Positive script, event,
embedded-resource, and encryption indicators require review; absence proves nothing.
PDF checks only recognize header/end markers; MP3 checks only recognize candidate
signatures. Page trees, PDF active content, and audio decoding remain unsupported.

`--expected-bytes` and `--expected-sha256` compare known external expectations;
a locally computed hash alone does not authenticate a source. File size without
edition evidence cannot decide whether a poem, novel, or scan is complete.
`limited_checks_passed` means exactly those checks passed, even when antivirus is
`not_scanned`. Review/invalid/incomplete results exit 1 and never publish content.

`--scan` (or TUI scan mode) invokes installed Defender on Windows, otherwise ClamAV.
Nothing is installed or updated. Calls use argument arrays and no remediation,
with two-minute deadlines and 64 KiB combined output. ClamAV requests limit and
encryption alerts. A post-scan hash/identity check binds evidence to the snapshot.
Unknown output, failure, timeout, truncation, and missing tools remain unsuccessful;
Defender output in unsupported languages may be incomplete. No-detection output
does not independently establish signatures are current or every internal member
was inspected. Scanner cloud/sample-submission settings apply, without Nemalo
changing host preferences. Pure local checks make no network request.

## Catalog snapshots and preservation

```sh
nemalo library snapshot /path/to/books --output /path/to/catalog.json --assess
nemalo library list /path/to/catalog.json --query "fr" --limit 20 --offset 0
nemalo library audit /path/to/catalog.json --root /path/to/books --json
```

Use an existing output directory outside the source root. Outputs never overwrite;
the destination filesystem must support hard links. Snapshots hash all regular
files, group exact duplicates without losing locations, and account for skipped
links/special files. Incomplete inventory prevents saving. Optional `--assess`
records shared EPUB/PDF/MP3 health evidence and EPUB titles/languages, without
antivirus. Invalid content can be cataloged with findings; this is a preservation
baseline, not publication eligibility.

Snapshot/audit use inventory defaults and accept `--max-*` limits, capped at
256 MiB/file and 5 GiB/inventory pass, with cooperative five-minute deadlines.
Assessment makes an additional source pass over supported unique assets. Catalog
JSON is bounded to 16 MiB. `list` searches literal metadata/path/hash text, with
limit 1-100 and offset 0-100000. TUI holdings show the first 50 matching assets.

Audits require an explicit root; the informational catalog root never authorizes
filesystem reads. They report changed/missing/added/unverified files, with nonzero
exit on findings or incomplete traversal. No repairs or source mutations occur.
Catalog health is historical and unsigned. File sync plus exclusive publication
prevents partial final JSON but does not guarantee directory-entry persistence
through power loss. See [decision 0004](decisions/0004-portable-library-snapshots.md).

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

Finish milestone 1 domain identities, mutable catalog locking, and durable operation journal before
mutating user libraries. Follow with bounded archive staging and production
assessment/publication policy. Reuse shared file checks/scanners. Keep real
acquisition in the first complete lifecycle; do not turn this discovery foundation
into a download-only or inspection-only product.
Add those operations to CLI and TUI together. MCP remains a scoped adapter over
the same services. Generated analysis and model/harness integrations remain post-1.0.
