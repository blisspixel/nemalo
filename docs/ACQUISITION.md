# Selected source acquisition

Implemented in CLI, JSON, and TUI through `internal/app` and
`internal/acquisition`. This is an early single-file intake workflow, not checked
library publication, complete audiobook acquisition, or a resumable transfer queue.
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
```

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

## Intake packet and evidence

The explicitly selected output must be a new directory. Missing parent folders
are created, but only after the fresh evaluation and file selection succeed, so a
refused request leaves no new folders. Directory reservation and all file
creation are exclusive. A completed packet has:

| File | Purpose |
| --- | --- |
| `intent.json` | Selected asset, fresh declaration evidence, metadata digest, request bounds, time |
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

## Network and failure bounds

- At most 256 MiB per selected file, configurable lower with `--max-file-bytes`.
  One extra byte detects an over-budget response. No automatic retry or bulk fetch.
- Five-minute acquisition deadline, 30-second response-header timeout, and shared
  provider pacing/`Retry-After`. Separate processes do not coordinate quotas.
- HTTPS only, normal TLS validation, public-address DNS checks and pinned-IP direct
  dialing. Initial requests and up to four redirects have approved delivery hosts:
  `archive.org`, `www.archive.org`, and numbered `ia`/`dn` data nodes in the
  `us`, `ca`, and `eu` regions, which Archive assigns per item.
  Environment proxies, cookie storage, and automatic decompression are disabled.
- Existing output is refused before provider access. Failures/cancellation retain
  intent, partial bytes, and any pending record. They never silently resume, restart,
  overwrite, clean up, or publish those bytes into a library.

Keep a failed packet for review and choose a new output for a deliberate retry.
Automatic validator-bound resume and packet recovery remain planned. A receipt
is historical evidence; recheck current bytes before relying on it.

The validation harness shares the transfer primitive and checked download client.
Its curated multi-resource orchestration and existing-file receipts remain a
separate validation interface, not a second production acquisition policy.

## TUI workflow

Select an Archive search result and press `e`, then Enter to evaluate. In Evaluate,
Ctrl+N edits the new Intake directory. F6 focuses the source-file list; `d` reviews
the exact file, output, transfer ceiling, scan, and limitations. Enter confirms the
frozen request; Escape returns without downloading. Successful intake stages the
content path in Check and Scan without starting either operation, and the status
line states the scan result. After a detection the list shows the item's other
files, so F6 and `d` review another one; editing the Item evaluates another
source. Failed intake retains its report and output; cancellation does not
remove partial files.
