# Development and operation

Nemalo is an early Go CLI/TUI application. Go 1.27.2 is pinned in `go.mod`.
The launcher may download that patched toolchain when the local Go installation is
older; this does not replace the system installation. No Python or Node runtime is
needed for the application, tests, or first-party tooling.

## Current operations

`help`, `version`, `doctor`, `search`, `evaluate`, `acquire`, `inspect`, `check`, `library snapshot`,
`library list`, `library audit`, `library import`, `library ingest`, `library cleanup`, `library restore`,
`library rescan`, `library recordings`,
`library open`, `library export`, `library init`, `library status`, `content units`,
`content read`, `content locate`, `content source`, `content resource`, `content capabilities`,
`mcp serve`, and `tui` are implemented.
Search defaults to Open Library work metadata; `--source archive` selects Internet
Archive text/audio item search. Neither search resolves download permissions.
Results keep the provider's relevance ranking, and the result names its order.
For Archive, `--sort identifier` pages repeatably and `--sort downloads` ranks by
the declared download count, a popularity signal rather than quality; both use
an identifier tie-break. Relevance ties can move between requests, and a repeat
across the two pages of one unaligned request fails rather than skipping items.
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
Explicit selected-file acquisition has its own
[intake and transfer contract](ACQUISITION.md). It does not publish checked holdings.

Inventory requires an explicit directory. It uses `os.Root`, skips links/special
files, and reports filename-based candidates without extracting or validating them.
Default limits: 10,000 entries, depth 32, 256 MiB per hashed file, and 1 GiB total
hashed input. Hashing is opt-in. Default inventory reads directory metadata only.
Limits, errors, and cancellation produce an incomplete result and nonzero exit.
Inventory performs no security scan; tool availability in `doctor` is not scan evidence.
Filesystem operations can still block on a stalled device; this is not a sandbox.

The TUI exposes search, evaluation, inventory, checks, and library services with session-scoped forms/results, selectable
search/holdings, adaptive detail panes, and complete report access. It makes no
startup network request. Source text and paths are escaped; reports are wrapped
at grapheme boundaries without discarding original evidence. Scopes and scanner
cloud policy remain explicit. See the [terminal guide](TUI.md) for keyboard
controls, budgets, paging, appearance, and application-view captures.
Content retrieval uses CLI/JSON and the TUI Read operation over shared services.
The default `epub-text/2` returns readable text with each omitted image, table,
or other element located as a gap. `mpeg-audio/1` indexes MP3 frames and returns
decodable frame ranges with decoder preroll; nothing is decoded. Capability negotiation, structured EPUB parts,
note references, original markup, and bounded local image bytes are available in
CLI/JSON, and through the read-only MCP server.
See [source-bound access](CONTENT-ACCESS.md) and [structured EPUB](STRUCTURED-EPUB.md)
for supported representations, exact contracts, bounds, and remaining issue scope.

JSON mode uses schema version 1 and exit codes 0
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
represent the same tracks. `recordings` groups MP3 files by declared format in
declared track order, with `order_source` and an `acquirable` verdict; see
[decision 0018](decisions/0018-whole-recording-acquisition.md). After ingest,
`library recordings LIBRARY [--source archive:ITEM]` lists held recordings in
declared order with missing positions and, with `--source`, verified and indexed
track timing ([decision 0020](decisions/0020-recording-manifest.md)). Actual
edition identity, narrator identity, rights policy, and checked publication
remain planned.

## File health and scanning

Filesystem capabilities and output encoding follow
[decision 0008](decisions/0008-filesystem-and-display-boundaries.md). Linux requires
`/proc/self/fd`, macOS `/dev/fd`; missing facilities fail explicitly. EPUB compressed
ranges are preflighted; lifetime compressed reads are capped at 1 GiB, expanded
members retain their separate limits, and assessment has a five-minute deadline.
JSON escapes terminal-sensitive Unicode without changing parsed data.

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

`--expected-bytes` and `--expected-sha256` compare known external expectations.
Pass a publisher SHA-256 when one exists. A match shows the bytes are the
published bytes. A locally computed hash alone does not authenticate a source,
and a checksum does not prove the file is free of malware. File size without
edition evidence cannot decide whether a poem, novel, or scan is complete.
`limited_checks_passed` means exactly those checks passed, even when antivirus is
`not_scanned`. Review/invalid/incomplete results exit 1 and never publish content.

`--scan` (or TUI scan mode) is recommended for a file you did not produce. It
invokes installed Defender on Windows, otherwise ClamAV. `doctor` names that
default and, without executing a scanner, reports definition age from the
ClamAV daily database header or Defender's registry values; definitions older
than seven days are `stale`. Without usable scan evidence, the human output of
`check`, `library import`, and `acquire` ends with one `Next:` line: `--scan`
when a scanner is installed, otherwise the platform install command (pacman,
apt, dnf, zypper, apk, Homebrew, or Windows Security). Nothing is installed or
updated. On Linux and macOS, install the
distributor package that provides `clamscan` and update signatures with
`freshclam` first. On Arch, including Omarchy, that package is `clamav`. Calls
use argument arrays and no remediation, with two-minute deadlines and 64 KiB
combined output. ClamAV requests limit and
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
nemalo library list /path/to/catalog.json --format epub --limit 100
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
limit 1-100 and offset 0-100000. TUI holdings show 50 matching assets per page.
`--format all|epub|pdf|mp3` uses exact case-insensitive filename suffixes, excluding
`.epub.receipt.json` from EPUB results. It does not manufacture assessment evidence;
invalid or unassessed files remain representable. CLI defaults to all files; TUI
defaults to EPUB filenames. Byte duplicates retain all locations even when only
one location matches the selected format.

Audits require an explicit root; the informational catalog root never authorizes
filesystem reads. They report changed/missing/added/unverified files, with nonzero
exit on findings or incomplete traversal. No repairs or source mutations occur.
Catalog health is historical and unsigned. File sync plus exclusive publication
prevents partial final JSON but does not guarantee directory-entry persistence
through power loss. See [decision 0004](decisions/0004-portable-library-snapshots.md).

## Library control state

```sh
nemalo library status /path/to/existing/library --json
nemalo library init /path/to/existing/library
```

Status is read-only and does not create control state. Init requires an existing,
explicit directory and creates only `.nemalo/writer.lock` and
`.nemalo/journal.jsonl`. Content is preserved and not inspected. Repeating init is
idempotent; valid interrupted initialization resumes with the same library ID.
Busy, corrupt, linked, oversized, or unexpected control state fails explicitly.
Do not remove a lock file to bypass another process. Preserve malformed state for
review; automatic repair/truncation is unavailable. The initialization journal
does not provide content import, publication, or cleanup recovery.

Writes sync journal files. Directory-entry power-loss persistence and network
filesystem coordination are not guaranteed. See the
[state decision](decisions/0006-library-control-state.md) for the full boundaries.
The TUI's State operation reads on Enter; F7 reviews initialization before a
second Enter writes. Ctrl+N sets one source and F8 reviews import. That review
does not scan. Ctrl+R reviews a rescan of the library; a second Enter scans and
records the results.

## Managed import

```sh
nemalo library import /path/to/library /path/to/book.epub
nemalo library import /path/to/library /path/to/book.epub --apply
nemalo library import /path/to/library /path/to/intake --apply --scan
nemalo library ingest /path/to/library /path/to/downloads --apply --scan
nemalo library audit /path/to/library
nemalo library list /path/to/library --query "verne" --status checked
nemalo library open /path/to/library --asset sha256:HASH
nemalo library export /path/to/library --asset sha256:HASH --output /path/to/folder
nemalo library cleanup /path/to/library /path/to/downloads
nemalo library cleanup /path/to/library /path/to/downloads --apply --plan DIGEST
nemalo library restore /path/to/library --cleanup cleanup:ID --apply
nemalo content read /path/to/library --root /path/to/library --asset sha256:HASH
nemalo content read /path/to/library --root /path/to/library --asset sha256:HASH \
  --representation pdf-text/1 --unit 3
nemalo content locate /path/to/library --root /path/to/library --asset sha256:HASH \
  --representation pdf-text/1 --text "exact quotation"
```

A PDF reads as `pdf-text/1`: one unit per physical page, with the printed page
label separately, text in content-stream order, and located gaps; `check`
records its page count and labels and keeps an unreadable page tree in review
([decision 0021](decisions/0021-pdf-pages-and-text.md)).

Import requires an initialized library. The source is an EPUB, PDF, or MP3 file
outside that library, or a completed intake directory with exactly one
`content.epub`, `content.pdf`, or `content.mp3` and a matching `receipt.json`.
Without `--apply`, nothing is written. `--apply` copies the assessed bytes and
leaves the source in place. `--scan` can invoke installed antivirus. `checked`
requires an EPUB whose limited checks passed and whose scan reported no
detections. Other results stay in review. For an intake packet, a clean scan
recorded at acquisition and bound to the same SHA-256 counts without scanning
again, but only where the holding has no usable scan evidence; it never replaces
a recorded detection. A packet whose receipt records a detection is refused. A later import without `--scan` does
not downgrade `checked`. A scan gap with no other finding is recorded as
`scan_` plus the scanner status. A conflicting stored file, an unexpected extra
hard link, or a store path whose parent is not a real directory fails before a
new intent is written. The existing bytes stay in place.

`library ingest` imports a whole folder: every EPUB, PDF, MP3, intake packet,
and supported ZIP member, through the same import and journal, and lists every
other entry with a reason. ZIP members are extracted into private temporary
staging with path, size, ratio, and CRC checks and recorded as
`ARCHIVE!/MEMBER`. The folder and its archives are never changed. See
[decision 0017](decisions/0017-folder-and-zip-ingest.md).

Stored files live under `assets/sha256/` beside `.nemalo`. Operations and
holdings journals are separate from the initialization journal. A trailing
import intent resumes only for the same source. Corrupt journals are retained.
`library list` on the library directory lists current holdings in import order,
with a literal `--query` over titles, languages, identifiers, paths, and source
IDs, and `--format` and `--status checked|review` filters. It reads journals only.
`library open` hands the default application, or `--app`, a verified read-only
copy in `<state>/handoff`, never the stored file, because some readers write
annotations into the files they open. `library export` makes the same verified
copy in an explicit directory and never overwrites; see
[decision 0016](decisions/0016-reader-handoff-copies.md).
`library audit` on the library directory reports missing, changed, and review
holdings and does not repair them. Snapshot audit still requires `--root`.
An EPUB package identifier is recorded when it is an explicit ISBN, DOI, or
Open Library work or edition key. Other package identifiers stay unassigned.
The same identifier does not merge two files. PDF and MP3 holdings do not claim
that their bibliographic identifiers were read. See
[decision 0009](decisions/0009-managed-epub-import.md) and
[decision 0010](decisions/0010-bibliographic-identity.md).

### MCP server

```sh
nemalo mcp serve --scope /path/to/library
```

`mcp serve` runs read-only MCP tools on stdio for an agent host: library status,
holdings, audit, file checks without antivirus, and EPUB content retrieval, each
over the same services and JSON data as the CLI. `--allow-network` adds catalog
search and Archive evaluation. Tool paths must resolve inside a `--scope`
directory or the configured library and review folders. Stdout carries protocol
messages only. See [agent integration](AGENT-INTEGRATION.md) and
[decision 0014](decisions/0014-read-only-mcp-server.md).

### Rescan review holdings

```sh
nemalo library rescan /path/to/library
nemalo library rescan /path/to/library --apply
```

Rescan reruns the limited checks and scans each EPUB holding in review only
because scan evidence is missing or unusable. It writes nothing without
`--apply`. ClamAV receives up to 32 files per invocation, so signatures load once
per batch rather than once per file. PDF and MP3 holdings, earlier detections,
and other findings are listed with a reason and not scanned. Stored bytes are
never changed; bytes that no longer match their holding receive no status.
Applied changes are journaled as `rescan_intent`, holding line, and
`rescan_completed`; the next writer completes an interrupted rescan first. See
[decision 0012](decisions/0012-scan-on-acquire-quarantine-and-rescan.md).

## Configuration

Configuration is an optional strict JSON object with `library` and `review` paths
and `on_detection` (`quarantine`, the default, or `delete`) for downloads the
scanner flags. Both paths must be absolute and must not overlap. Unknown fields, multiple JSON
values, nonregular files, and files over 64 KiB are rejected. These are reserved
settings for upcoming content operations; current init/status require their own
explicit directory and do not adopt these settings implicitly.

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
and running `doctor` do not create these directories. `doctor` also reports whether
the directory descriptor bridge is present. That is `/proc/self/fd` on Linux and
`/dev/fd` on macOS. It does not execute a scanner or create a hard link.

## Omarchy

Reviewed 2026-10-09 against Omarchy 4.0.4, an Arch Linux x86_64 desktop. The
installer uses Arch packages, systemd, and btrfs or ext4. Hyprland is the
compositor. Omarchy 4 defaults to the `foot` terminal; Ghostty, Alacritty, and
Kitty remain selectable. Omarchy 3 defaulted to Alacritty.

Run the published `linux/amd64` archive. That build has cgo disabled, so it does
not need Ubuntu's C library. Hosted CI runs it on Ubuntu, not on Omarchy or Arch.
Building this tree from source requires Go 1.27.2.

Library directories use `/proc/self/fd`, which Arch provides. Import hard-links
the stored file inside the library directory. ext4 and btrfs both support that
link when the library stays on one filesystem. A source on another filesystem is
copied, then linked only inside the library. `nemalo doctor` reports
`descriptor_bridge` for the `/proc/self/fd` check.

`foot`, Ghostty, Alacritty, and Kitty are recognized as truecolor. `NO_COLOR`
and `TERM=dumb` still disable color. A tmux session stays 256-color. Omarchy
4.0.4 binds Super shortcuts and uses F9 for dictation. It does not bind
Alt+1 through Alt+0, F2 through F8, or Ctrl+C. Ctrl+C still quits Nemalo.
Super+C is the desktop copy key. Nemalo does not read the Wayland clipboard.
ClamAV is the recommended scanner and is not installed by default. `doctor`
reports whether `clamscan` is available and how old its signatures are, and the
first unscanned result prints `sudo pacman -S clamav && sudo freshclam`. Enable
`clamav-freshclam.service` to keep signatures current. A missing scanner leaves
the file unscanned. After installing ClamAV, `library rescan LIBRARY --apply`
scans the review holdings in one command.

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

Single-asset import is in place, and EPUB package identifiers are recorded as
evidence without merging files. Downloads are scanned on acquisition, detections
are quarantined, and review holdings can be rescanned in one command. Folders
and ZIP archives ingest through the single-file import, and whole recordings are
acquired in declared order with track-level resume and listed as ordered
manifests. `library cleanup` moves held sources to a recoverable holding area
and `library restore` returns them (decision 0022); interrupted downloads resume
against a strong validator (decision 0023). Reuse shared file checks and scanners. Keep real
acquisition in the first complete lifecycle. Add those operations to CLI and TUI
together. MCP remains a scoped adapter over the same services. Generated
analysis and model/harness integrations remain post-1.0. Issues 1 and 2 are
closed; their remaining OCR, column, narrator, and M4B work is milestone 7 and 8.
