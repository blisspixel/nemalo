# Selected source acquisition

Implemented in CLI, JSON, and TUI through `internal/app` and
`internal/acquisition`. It acquires one selected file, or every track of one
declared recording, into untrusted intake, and resumes an interrupted transfer
against the same bytes. It is not checked library publication or a queue.
Scanning, quarantine, and alternatives follow
[decision 0012](decisions/0012-scan-on-acquire-quarantine-and-rescan.md).

## CLI workflow

```sh
nemalo search 'collection:librivoxaudio AND title:"Art of War"' --source archive --limit 3
nemalo evaluate archive:art_of_war_chinese_1506_librivox --json
nemalo acquire archive:art_of_war_chinese_1506_librivox \
  --file artofwar_01_sun.mp3 --output /path/to/new-intake-packet \
  --max-file-bytes 16777216 --json
nemalo library import /path/to/library /path/to/new-intake-packet --apply

nemalo evaluate gutenberg:1342 --files
nemalo acquire gutenberg:1342 --file 1342.epub.noimages \
  --output /path/to/new-gutenberg-packet

nemalo evaluate openlibrary:OL45804W --editions --limit 10
nemalo evaluate openlibrary:OL7353617M
nemalo acquire archive:fantasticmrfoxpu00roal --file NAME \
  --output /path/to/new-intake-packet
```

The Open Library commands choose an edition. `fantasticmrfoxpu00roal` is the
`ocaid` on edition OL7353617M at the time of the decision, not a filename.
`NAME` is the exact Archive filename from that evaluation. A work id is not
an acquire id.

Choose the exact source filename after reviewing availability, language, format,
and rights evidence. The example is one track, not a complete audiobook. Downloads
do not bypass borrowing restrictions, authentication, or DRM. Public availability
does not establish file-level rights or permission to redistribute.

Archive metadata is resolved again before acquisition. A saved evaluation cannot
authorize a URL or filesystem write. Only an unrestricted public file candidate
with a positive declared size and at least one valid source MD5/SHA-1 checksum is
eligible. Supported suffixes are EPUB, PDF, and MP3. The provider constructs the
canonical URL from validated item/file identities. See the official
[metadata interface](https://archive.org/developers/md-read.html) and
[source evaluation contract](decisions/0005-provider-search-and-evaluation.md).

## Gutenberg EPUB

`evaluate gutenberg:NUMBER` without `--files` reads the local catalog and lists
no files. `--files` fetches one bibliographic record,
`https://gutenberg.pglaf.org/cache/epub/NUMBER/pgNUMBER.rdf`, and lists the
files it names. Search does not fetch that record.

`acquire gutenberg:NUMBER --file NAME` downloads one of `N.epub.noimages`,
`N.epub.images`, or `N.epub3.images`. The packet stores the bytes as
`content.epub`. The record has no checksum, so Archive's checksum rule is
unchanged. The declared extent is stored and is not required to equal the
response length. The transfer requires a `Content-Length` inside the byte
budget and checks the body against that length. A missing or over-budget
length fails before `transfer.json`. When the sizes differ, the receipt carries
a size note. Other formats are listed and refused. The client allows only
`gutenberg.pglaf.org` and does not fall back to www.gutenberg.org. See
[decision 0029](decisions/0029-gutenberg-selected-epub.md).

## Intake packet and evidence

The explicitly selected output must be a new directory. Missing parent folders
are created, but only after the fresh evaluation and file selection succeed, so a
refused request leaves no new folders. Directory reservation and all file
creation are exclusive. A completed packet has:

| File | Purpose |
| --- | --- |
| `intent.json` | Selected asset, fresh declaration evidence, metadata digest, request bounds, time |
| `transfer.json` | Response status, `ETag` and whether it is strong, `Last-Modified`, length, final URL; synced before the body |
| `content.epub`, `content.pdf`, or `content.mp3` | Actual bytes, with a controlled local name |
| `receipt.json` | Measured bytes/SHA-256, source checks, final delivery URL, limited assessment, scan result |

Intent is synced before requesting content. Records are synced pending files
published by exclusive hard link; hard-link support is required. A receipt is
published only after transfer validation, local inspection, and a read-back hash.
Directory-entry persistence across power loss remains filesystem-dependent.

`complete` means this bounded intake operation succeeded. `transfer_complete`
separates successful byte verification from later inspection or receipt failures.
Successful status is `untrusted_intake`. `antivirus` and `scan` record the
post-download scan (below), or `not_scanned` with `--no-scan`.
Source MD5/SHA-1 checks establish consistency with metadata, not authenticity.
The measured SHA-256 identifies local bytes, not a trustworthy publisher.

EPUB checks use the shared bounded parser; PDF and MP3 checks establish candidate
signatures only. Format validity, warnings, antivirus coverage, DRM, licensing,
and semantic completeness remain separate.

## Scan, quarantine, and another source

After the transfer checks, local inspection, and a read-back hash, `acquire`
scans the content once with the installed scanner: Microsoft Defender on
Windows (remediation disabled, so Nemalo decides what happens to the file) and
ClamAV `clamscan` on Linux and macOS. `--no-scan` skips it. A second read-back
hash binds the scan to the receipt bytes. If host antivirus removes or changes
the file on its own during the scan, the packet is retained incomplete.

| Scan result | Outcome |
| --- | --- |
| `no_detections_reported` | `untrusted_intake`; `library import` of the packet records an EPUB as `checked` without scanning again |
| `unavailable`, `incomplete` | `untrusted_intake`, treated as unscanned; one line names the install command or a retry |
| `findings` | The content moves to `<state>/quarantine/<time>-<hash>/content.<ext>.quarantined`, or is deleted with `--on-detection delete` or configuration `"on_detection": "delete"`; exit 1 |

A detection writes a receipt with status `detected_quarantined`,
`detected_deleted`, `detected_retained` (if removal failed), or
`detected_changed_during_scan` (if the file changed or vanished during the scan,
for example through host antivirus), and import refuses such a packet. The result lists other eligible files from the same
fresh evaluation as ready-to-run commands, and a search query built from the
declared title and creator. Nothing else is requested automatically. A
detection can be a false positive, and a no-detection result is not proof the
file is safe. Scanner cloud and sample-submission settings belong to the host.

## Whole recordings

```sh
nemalo evaluate archive:art_of_war_librivox
nemalo acquire archive:art_of_war_librivox --recording "64Kbps MP3" \
  --output /path/to/new-recording-directory
nemalo library ingest /path/to/library /path/to/new-recording-directory --apply
```

Evaluation lists `recordings`: the item's MP3 files grouped by declared Archive
format, in declared track order. Derivative files take their order from the
original they name. `order_source` is `file_name` when the source declares no
distinct track numbers. Only an `acquirable` recording, where every track has
public access, a usable size, and a checksum, can be acquired as a whole.
`--max-recording-bytes` (default 4294967296, at most 16 GiB) bounds the declared
total; each track keeps the 256 MiB file limit.

| File | Purpose |
| --- | --- |
| `recording-intent.json` | Request, fresh item evidence, the selected recording, and its track files; synced before any request |
| `0001/`, `0002/`, ... | One ordinary intake packet per track, by position; each intent and receipt records `recording: {source_format, position, tracks}` |
| `recording.json` | Every track's packet, bytes, SHA-256, and scan; written last, only when every track has a receipt |

ClamAV scans groups of up to 32 tracks or 256 MiB at once, and their receipts
wait for that scan. With Defender, which scans one file at a time, or with
`--no-scan`, each track's receipt is written before the next track is
requested. A detection stops the recording.

`--resume` with the same item, format, and output continues an interrupted
recording. Fresh declarations must still list the same files, sizes, and
checksums in order, or nothing is requested. A track packet is kept only when its
receipt completes that file at that position and its content still matches the
declared checksum and the receipt hash. A kept track keeps its original scan
result. Any other attempt is retained untouched, and the track is fetched again
as `0003-2` (up to nine attempts). A leftover `.pending` record from an
interrupted write stops the resume for review. Track order, titles, and durations are declarations, not
checks of the audio. See
[decision 0018](decisions/0018-whole-recording-acquisition.md).

## Network and failure bounds

- At most 256 MiB per selected file, configurable lower with `--max-file-bytes`.
  One extra byte detects an over-budget response. No automatic retry or bulk fetch.
- Five-minute acquisition deadline, 30-second response-header timeout, and shared
  provider pacing/`Retry-After`. Separate processes do not coordinate quotas.
- HTTPS only, normal TLS validation, public-address DNS checks and pinned-IP direct
  dialing. Initial requests and up to four redirects have approved delivery hosts:
  `archive.org`, `www.archive.org`, and numbered `ia`/`dn` data nodes in the
  `us`, `ca`, and `eu` regions, which Archive assigns per item.
  A Gutenberg record or EPUB uses the same file client limited to
  `gutenberg.pglaf.org`. A redirect to any other host is refused.
  Environment proxies, cookie storage, and automatic decompression are disabled.
- Existing output is refused before provider access. Failures/cancellation retain
  intent, partial bytes, and any pending record. They never silently resume, restart,
  overwrite, clean up, or publish those bytes into a library.

Keep a failed packet for review. `acquire ... --file NAME --output PACKET
--resume` continues an interrupted transfer in that packet when `transfer.json`
was synced before the body and partial bytes remain. A refusal before any
bytes, such as an HTTP 500 with only `intent.json` in the folder, has no
transfer record: the summary says to start a new output, and `--resume` is not
offered. A transfer that started without a strong validator is also not offered
for `--resume`; the summary says to start a new output. Gutenberg resume uses
the recorded response length when it differs from the declared extent. Every transfer that does start syncs
`transfer.json` with the response's validator before its body, and a resume
requests only the missing bytes with `If-Range` on that strong `ETag`. A reply
other than exactly the missing range of the same file, a weak or missing
validator, or changed declarations leave the packet untouched. The whole file is
then verified, inspected, scanned, and receipted as one transfer, and the
receipt records `resumed_from`. A recording resumes an interrupted track in
place the same way, or fetches it again under a new name when the source refuses
the range. See [decision 0023](decisions/0023-validator-bound-download-resume.md). A receipt
is historical evidence; recheck current bytes before relying on it.

The validation harness shares the transfer primitive and checked download client.
Its curated multi-resource orchestration and existing-file receipts remain a
separate validation interface, not a second production acquisition policy.

## TUI workflow

Select an Archive or Gutenberg search result and press `e`, then Enter to
evaluate. A Gutenberg catalog row lists no files. With an intake folder, `d`
on that row requests one bibliographic record and then lists EPUB offers.
That record fetch is only for a Gutenberg catalog row. In Evaluate,
Ctrl+N edits the new Intake directory. F6 focuses the source-file list; `d` reviews
the exact file, output, transfer ceiling, scan, and limitations. Enter confirms the
frozen request; Escape returns without downloading. Successful intake stages the
content path in Check and Scan without starting either operation, and the status
line states the scan result. After a detection the list shows the item's other
files, so F6 and `d` review another one; editing the Item evaluates another
source. Failed intake retains its report and output; cancellation does not
remove partial files. When the selected file belongs to an acquirable recording,
`a` reviews acquiring every track of that recording into the Intake directory;
resume a partial recording with the CLI `--resume`.
