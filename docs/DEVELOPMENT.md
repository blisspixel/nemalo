# Development and operation

Nemalo is an early Go CLI/TUI application. Go 1.27.2 is pinned in `go.mod`.
The launcher may download that patched toolchain when the local Go installation is
older; this does not replace the system installation. No Python or Node runtime is
needed for the application, tests, or first-party tooling.

## Current operations

`help`, `version`, `doctor`, `search`, `evaluate`, `catalog fetch`, `acquire`, `inspect`, `check`, `library snapshot`,
`library list`, `library audit`, `library check`, `library import`, `library ingest`, `library cleanup`, `library restore`, `library purge`,
`library rescan`, `library recordings`,
`library open`, `library export`, `library init`, `library status`, `bookmark`,
`content units`, `content navigation`,
`content read`, `content locate`, `content source`, `content resource`, `content capabilities`,
`read`, `mcp serve`, `jobs`, and `tui` are implemented.
Search defaults to Open Library work metadata; `--source archive` selects Internet
Archive text/audio item search. `--source gutenberg` reads a local
`pg_catalog.csv` (`--catalog`, `gutenberg_catalog`, or `NEMALO_GUTENBERG_CATALOG`)
and does not contact gutenberg.org. `catalog fetch gutenberg --output FILE`
downloads that one official CSV. `evaluate gutenberg:NUMBER` without `--files`
reads that row and lists no files. `evaluate gutenberg:NUMBER --files` fetches
one bibliographic record from gutenberg.pglaf.org and lists its files. It does
not download an ebook. `acquire gutenberg:NUMBER --file NAME --output DIRECTORY`
downloads one EPUB that record names (`N.epub.noimages`, `N.epub.images`, or
`N.epub3.images`) from the same host. The record has no checksum. The transfer
checks the response length. Other formats are listed and refused. Neither
search nor a catalog row resolves a download. `evaluate arxiv:ID[vN]` resolves one
paper version through arXiv's query API and its license through OAI-PMH, and
`acquire arxiv:IDvN --file IDvN.pdf` downloads that PDF from export.arxiv.org,
one request every three seconds
([decision 0047](decisions/0047-arxiv-selected-versions.md)). `--source all` searches Open
Library and Archive at once, and Gutenberg's local catalog when one is
configured, each by relevance in its own section with its own status; one
source's failure does not hide another's results, results are not ranked
against each other, and the command fails only when no source completed.

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
coordinate quotas. A dropped connection, a timeout, or HTTP 429, 500, 502,
503, or 504 is tried again at most twice within that deadline and pacing; no
other failure is retried, and no bulk harvesting occurs. Downloads are
continued, not restarted, after a dropped connection: `acquire` resumes its
own packet against the same strong validator, and `catalog fetch` requests
exactly the missing range.

Provider metadata requests use checked direct connections and fixed-host HTTPS
redirects. Environment proxies are disabled for this boundary; proxy-only networks
are currently unsupported. Private/reserved/mixed DNS destinations are rejected,
and connections use the validated numeric address with normal TLS validation.
Metadata/rights URLs returned by a provider are not fetched. See
[decision 0005](decisions/0005-provider-search-and-evaluation.md).
Gutenberg ranking and the catalog failure rules are in
[decision 0028](decisions/0028-gutenberg-local-catalog.md). Selected EPUB
intake is in [decision 0029](decisions/0029-gutenberg-selected-epub.md).
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
decodable frame ranges with decoder preroll; `mp4-audio/1` lists an M4B's or
M4A's sample tables and chapters, units only; nothing is decoded. Capability negotiation, structured EPUB parts,
note references, original markup, and bounded local image bytes are available in
CLI/JSON, and through the read-only MCP server.
See [source-bound access](CONTENT-ACCESS.md) and [structured EPUB](STRUCTURED-EPUB.md)
for supported representations, exact contracts, bounds, and remaining issue scope.
`content navigation` lists source-declared EPUB contents, and
`content read --representation epub-nav/1` reads one entry until its declared
end ([decision 0026](decisions/0026-epub-navigation-and-chapter-selection.md)).
`bookmark enable LIBRARY` creates `.nemalo/bookmarks.jsonl`. Save, note, delete,
export, import, and resume are explicit. A library with that file cannot be
opened by an earlier prerelease
([decision 0027](decisions/0027-explicit-local-bookmarks.md)).

JSON mode uses schema version 1 and exit codes 0
(success), 1 (failed/incomplete operation), and 2 (usage/configuration error).
Without `--json`, an error is one line on stderr, `nemalo: ` and a plain
sentence: a request error names the host and the cause instead of the URL, a
file error says what is wrong with the path, and control characters and line
breaks from a source cannot start another line. A command that failed before
doing anything prints only that line; one that kept bytes, a journal, or a
detection prints its result first. `--json` keeps the full error and result.

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
track timing ([decision 0020](decisions/0020-recording-manifest.md)). A held
M4B is a single-file recording. Recordings of one LibriVox reading share a
narration, named only when the LibriVox project names the Archive item back,
and different readings stay apart ([decision 0044](decisions/0044-narration-identity.md)).
The result's `byte_identities` relates complete declared recordings with the
same ordered asset hashes across source items, even without LibriVox metadata;
repeated positions are retained. It uses journal evidence, not a fresh integrity
check, and preserves every narration declaration. Different encodings and track
divisions stay distinct ([decision 0052](decisions/0052-recording-byte-identities.md)).
An Archive evaluation copies the ISBN and Open Library edition and work keys the
item declares, import records them on the source, and `library related LIBRARY
[--asset ID]` groups holdings that share a stated work, edition, or version, an
arXiv paper, a narration, or a source item, without merging anything
([decision 0049](decisions/0049-declared-identifiers-and-related-holdings.md)).
A collection file names exact selections; `collection status` resolves them
against holdings offline, and `collection acquire` previews and, with `--apply`,
acquires only the missing ones into an intake directory for `library ingest`
([decision 0050](decisions/0050-collections.md)).
Conflicting track positions or totals keep a collection's recording incomplete,
with every conflicting asset listed for review.
Registry confirmation of identifiers and rights policy remain planned.

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
nemalo check /path/to/book.epub --epubcheck
```

Supply real known size/hash values in the second example. `check` requires one
nonempty regular file, rejects final-component symlinks, and uses a private
temporary snapshot capped at 256 MiB, or 2 GiB for an M4B or M4A, and refuses
a snapshot that would not fit in the temporary folder
([decision 0040](decisions/0040-large-mp4-audio-and-scan-limits.md)). Source identity/size/mtime are checked across
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
Font obfuscation alone is recorded, not a finding
([decision 0036](decisions/0036-epub-font-obfuscation.md)). PDF checks walk the page
tree and active content without reading page content
([decision 0032](decisions/0032-pdf-checked-policy.md)); MP3 checks index frames
without decoding ([decision 0033](decisions/0033-mp3-checked-policy.md)); M4B
and M4A checks place every sample from the sample tables, reading no media
data, and find truncation, misplaced samples, fragments, and protected audio
([decision 0039](decisions/0039-mp4-audio-samples.md)). `--decode` decodes an
MP3, M4B, or M4A in full with an installed FFmpeg, through the file protocol
and a fixed demuxer only, and reports decoded duration and decoder errors as
evidence ([decision 0042](decisions/0042-optional-ffmpeg-decode.md)).

`--expected-bytes` and `--expected-sha256` compare known external expectations.
`--epubcheck` runs an installed EPUBCheck on an EPUB snapshot; reported errors,
or a check that could not run, are review findings, and it is not part of the
checked policy ([decision 0038](decisions/0038-optional-epubcheck.md)).
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
`check`, `library import`, and `acquire` ends with a `Next:` line: `--scan`
when a scanner is installed, otherwise the platform install command (pacman,
apt, dnf, zypper, apk, Homebrew, or Windows Security). Nothing is installed or
updated. On Linux and macOS, install the
distributor package that provides `clamscan` and update signatures with
`freshclam` first. On Arch, including Omarchy, that package is `clamav`. Calls
use argument arrays and no remediation, with deadlines of two minutes plus a
minute per 256 MiB and 64 KiB combined output. ClamAV requests limit and
encryption alerts; a limit alert is an incomplete scan, not a detection
([decision 0040](decisions/0040-large-mp4-audio-and-scan-limits.md)). A post-scan hash/identity check binds evidence to the snapshot.
Unknown output, failure, timeout, truncation, and missing tools remain unsuccessful;
Defender output in unsupported languages may be incomplete. No-detection output
does not independently establish signatures are current or every internal member
was inspected. Scanner cloud/sample-submission settings apply, without Nemalo
changing host preferences. Pure local checks make no network request.

Human output also names the command that continues the first-book path, before
any scan line. A local Gutenberg evaluation names `evaluate gutenberg:N --files`;
an evaluation with an eligible file names `acquire` for the first EPUB, else the
first PDF, by the same check acquire applies; a complete, undetected acquisition
names `library import` (the configured library, or `LIBRARY`); an import preview
names `--apply`; and a held EPUB or PDF names `read`, an audio file `library open`.
Arguments are quoted for a POSIX shell, or PowerShell on Windows, so a pasted
command interprets nothing from a source name, and a name holding a control or
format character is left out. `--json` output carries no `Next:` lines.

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
nemalo library check /path/to/library
nemalo library list /path/to/library --query "verne" --status checked
nemalo library open /path/to/library --asset sha256:HASH
nemalo library export /path/to/library --asset sha256:HASH --output /path/to/folder
nemalo library cleanup /path/to/library /path/to/downloads
nemalo library cleanup /path/to/library /path/to/downloads --apply --plan DIGEST
nemalo library restore /path/to/library --cleanup cleanup:ID --apply
nemalo library purge /path/to/library --cleanup cleanup:ID
nemalo library purge /path/to/library --cleanup cleanup:ID --apply --plan DIGEST
nemalo collection status /path/to/books.json /path/to/library
nemalo collection acquire /path/to/books.json /path/to/library --output /path/to/intake --apply
nemalo content read /path/to/library --root /path/to/library --asset sha256:HASH
nemalo content read /path/to/library --root /path/to/library --asset sha256:HASH \
  --representation pdf-text/1 --unit 3
nemalo content locate /path/to/library --root /path/to/library --asset sha256:HASH \
  --representation pdf-text/1 --text "exact quotation"
nemalo read /path/to/library --root /path/to/library --asset sha256:HASH
nemalo read /path/to/library --root /path/to/library --asset sha256:HASH --speed --rate 300
```

`read` opens the terminal reader (decision 0025) on EPUB or PDF text: pages
sized to the terminal, or one word at a time with `--speed`. `--unit` and
`--text-offset` choose the start; closing prints the values that return to the
same place. It requires an interactive terminal and has no `--json`. A missing
`--root` or `--asset` names that flag. For a managed library, `--root` is the
same folder as the catalog. `nemalo tui` has the same terminal requirement.
Without a terminal, both commands say so and exit 2.

A PDF reads as `pdf-text/1`: one unit per physical page, with the printed page
label separately, text in content-stream order, and located gaps; `check`
records its page count and labels and keeps an unreadable page tree in review
([decision 0021](decisions/0021-pdf-pages-and-text.md)).

Import requires an initialized library. The source is an EPUB, PDF, MP3, M4B,
or M4A file outside that library, or a completed intake directory with exactly
one `content.epub`, `content.pdf`, `content.mp3`, `content.m4b`, or
`content.m4a` and a matching `receipt.json`. Without `--apply`, nothing is
written. `--apply` copies the assessed bytes and leaves the source in place.
`--scan` can invoke installed antivirus. `checked` requires limited checks
that passed with no finding and a scan that reported no detections; a PDF,
MP3, M4B, or M4A also needs its format's examination (decisions 0032, 0033,
and 0039). Other results stay in review. For an intake packet, a clean scan
recorded at acquisition and bound to the same SHA-256 counts without scanning
again, but only where the holding has no usable scan evidence; it never replaces
a recorded detection. A packet whose receipt records a detection is refused. A later import without `--scan` does
not downgrade `checked`. A scan gap with no other finding is recorded as
`scan_` plus the scanner status. A conflicting stored file, an unexpected extra
hard link, or a store path whose parent is not a real directory fails before a
new intent is written. The existing bytes stay in place.

`library ingest` imports a whole folder: every EPUB, PDF, MP3, M4B, M4A, intake packet,
and supported ZIP member, through the same import and journal, and lists every
other entry with a reason. ZIP members are extracted into private temporary
staging with path, size, ratio, and CRC checks and recorded as
`ARCHIVE!/MEMBER`. With 7-Zip installed, RAR and 7z members are extracted the
same way, one at a time by exact name to standard output, from a private
snapshot of the archive ([decision 0043](decisions/0043-rar-and-7z-through-7-zip.md)).
A split 7z (`NAME.7z.001`, ...) or a RAR volume set (`NAME.part1.rar`, ...) is
read whole through its first volume, and a set with a missing volume is
preserved ([decision 0046](decisions/0046-archive-volume-sets.md)).
The folder and its archives are never changed. See
[decision 0017](decisions/0017-folder-and-zip-ingest.md).

Stored files live under `assets/sha256/` beside `.nemalo`. Operations and
holdings journals are separate from the initialization journal. A trailing
import intent resumes only for the same source. Corrupt journals are retained.
`library list` on the library directory lists current holdings in import order,
with a literal `--query` over titles, languages, identifiers, paths, and source
IDs, and `--format`, `--status checked|review`, and `--language TAG` filters;
the language filter matches the declared base language, so `en`, `en-US`, and
`eng` are one, and counts holdings with no readable tag
([decision 0037](decisions/0037-base-language-filter.md)). It reads journals only.
`library open` hands the default application, or `--app`, a verified read-only
copy in `<state>/handoff`, never the stored file, because some readers write
annotations into the files they open. `library export` makes the same verified
copy in an explicit directory and never overwrites; see
[decision 0016](decisions/0016-reader-handoff-copies.md).
`library cleanup` moves a folder's sources whose bytes the library holds into
the library's holding area, and `library restore` returns them
([decision 0022](decisions/0022-recoverable-source-cleanup.md)). `library purge`
then removes held copies of a completed cleanup whose bytes are a current
holding with verified stored bytes, after its own preview and plan digest;
receipts, intents, and anything else stay, and restore reports a purged file
as purged ([decision 0041](decisions/0041-purge-held-sources.md)).
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
search and Archive evaluation. `--allow-write` adds `library_import_preview`
and `library_import_apply`; an apply stores only the plan digest its preview
returned and refuses a changed source, assessment, or scan choice
([decision 0034](decisions/0034-mcp-import-tools.md)). `library import
--apply --plan DIGEST` gives the CLI the same check. `--allow-write` also adds
`library_rescan_start`, `job_get`, `job_list`, and `job_cancel`, and with
`--allow-network` `acquisition_start`: long work runs as a durable job journaled
in the state folder, which outlives the call and the connection; the server
finishes running jobs before it exits, and a job whose process ended is
recorded as interrupted ([decision 0045](decisions/0045-durable-mcp-jobs.md)).
`nemalo jobs list`, `jobs get ID`, and `jobs cancel ID` read and cancel the
same jobs from a terminal.
Tool paths must resolve inside a `--scope`
directory or the configured library and review folders. Stdout carries protocol
messages only. See [agent integration](AGENT-INTEGRATION.md) and
[decision 0014](decisions/0014-read-only-mcp-server.md).

### Rescan review holdings

```sh
nemalo library rescan /path/to/library
nemalo library rescan /path/to/library --apply
```

Rescan reruns the limited checks and scans each holding in review only
because scan evidence is missing or unusable. It writes nothing without
`--apply`. ClamAV receives up to 32 files per invocation, so signatures load once
per batch rather than once per file. Holdings of every held format are rescanned;
earlier detections and other findings are listed with a reason and not scanned. Stored bytes are
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
Like every other command, `doctor` prints a short text summary: paths, any
configured library, review folder, and catalog, the recommended scanner and its
signature age, the descriptor bridge, optional tools, and a `Next:` line for
each thing that needs action. `doctor --json` is the full report. Before
0.1.0-alpha.12, `doctor` printed that JSON object by default.

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

Import, ingest, scanned acquisition, recording manifests, cleanup, restore,
and purge are in place. Interrupted downloads resume against a strong validator
(decision 0023). Gutenberg catalog search stays local (decision 0028). One
selected Gutenberg EPUB can be acquired from gutenberg.pglaf.org
(decision 0029). An Open Library work lists editions, and one chosen edition
can name an Archive item through its ocaid before the existing Archive
acquire runs (decision 0030). EPUB, PDF, MP3, M4B, and M4A holdings can be
checked (decisions 0009, 0032, 0033, and 0039). LibriVox readers are credited
per track when the project links back
([decision 0035](decisions/0035-librivox-readers.md)). MCP is a scoped
adapter over the same services; `--allow-write` adds previewed imports
([decision 0034](decisions/0034-mcp-import-tools.md)) and durable acquisition
and rescan jobs ([decision 0045](decisions/0045-durable-mcp-jobs.md)). Website search, other
Gutenberg formats, recording relationships across different encodings beyond
LibriVox, decoded timing in content
access, files above 2 GiB, OCR, and column reconstruction stay later. The remaining 1.0 work is
ordered by dependency in the [roadmap](../ROADMAP.md#order-of-work-to-10), starting
with identity and collections. Cited evidence for agent-led reading is milestone 9
([decision 0048](decisions/0048-cited-evidence-before-1-0.md)); generated analysis
inside Nemalo and model/harness integrations remain post-1.0. Issues 1 and 2 are closed;
their remaining OCR, column, and decoded audio work is milestone 7 and 8.
