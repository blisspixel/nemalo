# 0012: scan on acquire, quarantine detections, and batch rescans

Status: accepted and implemented, 2026-10-11. Resolves
[issue 6](https://github.com/blisspixel/nemalo/issues/6) and
[issue 8](https://github.com/blisspixel/nemalo/issues/8). Extends
[decision 0003](0003-file-health-and-scanners.md),
[decision 0007](0007-selected-acquisition.md), and
[decision 0009](0009-managed-epub-import.md).

## Scan the bytes Nemalo just fetched

`acquire` (CLI and TUI) scans the downloaded content once, after the transfer
checksums, local structure checks, and a read-back hash, and before the receipt.
It uses the platform default scanner from decision 0003: Microsoft Defender
(`MpCmdRun.exe -Scan -ScanType 3 -File FILE -DisableRemediation`) on Windows and
ClamAV `clamscan` on Linux and macOS. `--no-scan` opts out. There is no daemon,
background job, or whole-disk scan. A second read-back hash after the scan binds
the result to the receipt bytes; if host antivirus removed or changed the file on
its own, the packet is retained incomplete, never reported clean. If the scan
had reported a detection, the receipt still records it as
`detected_changed_during_scan`. Transfer phases run under the five-minute
acquisition deadline; the scan has its own scanner deadline, and only the
caller's cancellation, not the expired transfer deadline, can stop the receipt
after a clean scan.

The receipt records `antivirus` and the full `scan` result. A missing scanner
(`unavailable`) or an `incomplete` scan keeps the packet as ordinary
`untrusted_intake`, and the command prints one platform install or retry step.

## Detections never become intake

On `findings` the content is removed from the intake packet. By default it is
copied, synced, and hash-verified into a new private directory under
`<state>/quarantine/` (for example `~/.local/state/nemalo/state/quarantine/`),
named `content.<ext>.quarantined` so no reader or thumbnailer treats it as a
book, and only then removed from the packet. Both directories are used through
their opened roots, so no validated pathname is reopened (decision 0008). The
`on_detection` setting or `--on-detection delete` deletes the bytes instead.
If removal fails, the bytes stay in the packet with status `detected_retained`.

Every outcome writes a receipt with status `detected_quarantined`,
`detected_deleted`, `detected_retained`, or `detected_changed_during_scan`, and
the command exits 1. The CLI and TUI both apply the configured `on_detection`. Import
refuses any packet whose receipt is not `untrusted_intake` or whose antivirus
status is `findings`. The result lists up to ten other eligible files from the
same fresh evaluation, with ready-to-run commands, and a search query built from
the declared title and creator. The TUI restores the item's other files for a
deliberate new review. Nothing is requested automatically.

## Import accepts the bound intake scan

A packet import without `--scan` uses the receipt scan when the receipt and its
scan both report `no_detections_reported` and the receipt SHA-256 is the
assessed SHA-256. A clean EPUB acquired with a scanner installed can therefore
be imported as `checked` without scanning again. The holding reason stays
`checks_and_scan_recorded`, and the result notes that signatures may have changed
since acquisition. A live `--scan` always takes precedence. Because a receipt scan
is older evidence, it only fills a holding of the same bytes that has no usable
scan evidence; it never replaces a recorded detection or a later scan outcome.

## Rescan review holdings in one command

`library rescan LIBRARY` (TUI: Ctrl+R in State) reruns the limited checks and
scans each EPUB holding that is in review only because scan evidence is missing
or unusable (`unscanned`, `scan_unavailable`, `scan_incomplete`,
`scan_not_scanned`, `scan_unrecognized`). It is a dry run unless `--apply`.
PDF and MP3 holdings, earlier detections, and other findings are listed with a
reason and not scanned; a previous detection needs a deliberate review rather
than an automatic promotion. Stored bytes are never changed. Bytes that no
longer match the holding receive no status and point to `library audit`.

Applying records each change as a journaled `rescan_intent`, a new holding line,
and `rescan_completed`, under the writer lock held for the whole scan. The intent
carries the complete new status, so a crash is completed deterministically by the
next writer (rescan or import) before it starts its own work. A pending intent
must match its holding's format, bytes, and path, or the state is retained for
review and nothing is written. A trailing import
intent blocks rescan until that import is finished. Libraries with rescan events
are not readable by earlier prereleases, which treat unknown events as corrupt.

The journal consistency check now accepts a holding line that already matches a
pending intent even when an earlier completed fact disagrees. Before this, an
interrupted scan-upgrade import of an already held EPUB was misreported as
corrupt state.

## Batch ClamAV scans; do not prefer clamdscan

Most of a small file's `clamscan` time is loading signatures, which happens once
per process. Rescans pass up to 32 private snapshots (and at most 1 GiB) to one
`clamscan` invocation. Each file is clean only when its single line is
`path: OK`, the summary counts every file, no unattributed warning or error
exists, and the exit code agrees; a missing or repeated line marks the whole
batch incomplete. A `FOUND` line is a detection for that file even if the batch
otherwise failed. Defender accepts one `-File` per invocation and loads no
database per process, so it scans files in turn.

`clamdscan` is not used even when `clamd` is running. Its size and scan limits
come from the daemon's `clamd.conf`, where `AlertExceedsMax` is off by default,
so a file larger than the daemon limit can be partially scanned and still report
`OK`. That would let a limit look clean. Batching `clamscan` keeps Nemalo's
`--alert-exceeds-max=yes` and `--alert-encrypted=yes` and removes most of the
cost the issue measured.

## Defender detection recognition

Defender exits 2 both for an unremediated detection and for scan errors. A
detection is recognized only when the output has a `Scanning ... found N
threats.` summary with N at least 1, a `Threat :` entry, and no `[Failed]`
marker. Anything else is `incomplete`. Microsoft documents the exit codes but not
the detection text; the pattern is covered by synthetic tests and must be
rechecked against a real detection before relying on it in new contexts.

## Doctor and guidance

`doctor` reads, without executing a scanner, the ClamAV daily database header
(`daily.cld` or `daily.cvd`, located through `freshclam.conf DatabaseDirectory`
or distribution defaults) or Defender's registry definition version and apply
time, and reports the age. Definitions older than seven days are `stale`, with
advice to run `freshclam` or enable `clamav-freshclam.service`. The Defender
registry values are not a documented interface and are reported as such. When
the default scanner is missing, `doctor` and the human output of `check`,
`library import`, and `acquire` name the platform install command derived from
`/etc/os-release` (pacman, apt, dnf, zypper, apk), Homebrew on macOS, or
Windows Security.

## Acquire output parents

`acquire --output a/b` creates missing parent directories, but only after the
fresh source evaluation and file selection succeed, so a refused request leaves
no new directories. The packet directory itself is still created exclusively.
