# 0040: large M4B audio and scanner limits

Status: accepted and implemented, 2026-10-19. Raises the M4B and M4A file
limit of [decision 0039](0039-mp4-audio-samples.md) and refines how
[decision 0003](0003-file-health-and-scanners.md) and
[decision 0012](0012-scan-on-acquire-quarantine-and-rescan.md) read ClamAV
limit alerts.

## Context

A long audiobook is often one M4B larger than the 256 MiB file limit that
check, import, and acquisition share: a thirteen-hour LibriVox M4B is 380 MB.
That limit exists because EPUB, PDF, and MP3 are parsed in memory. The MP4
reader of decision 0039 reads boxes at offsets and never loads media data, so
the limit does not protect it.

Raising the limit exposed two scanner problems, both measured with ClamAV
1.5.4 on a synthetic 936 MiB M4B. First, `--max-scansize` counts the file's
own bytes: at 512M the file was reported as
`Heuristics.Limits.Exceeded.MaxScanSize FOUND` with exit code 1 and one
infected file, so Nemalo recorded a detection and acquisition would have
quarantined the audiobook as malware. A file over `--max-filesize` was
reported the same way with exit code 2. A limit alert names bytes that were
not scanned; it is a coverage gap, not a detection. Second, the scan took 126
seconds, past the fixed two-minute deadline, which would have made every such
scan incomplete. Downloads had the same shape of problem: a five-minute
acquisition deadline and a five-minute total request timeout. A 380 MB file
from Archive took about four minutes, so a larger one could never finish.

## Decision

- EPUB, PDF, and MP3 stay at 256 MiB (`assessment.MaxParsedBytes`). M4B and
  M4A may be 2 GiB (`assessment.MaxMP4Bytes`). `assessment.MaxFileBytes`
  names the limit by extension, and check, import, ingest, rescan, holdings
  validation, cleanup, acquisition, the transfer budget, and `mp4-audio/1`
  use it. A file named `.m4b` that is not MP4 is still held to its detected
  format's limit by inspection.
- A check needs free temporary space for its private snapshot; a snapshot
  that would not fit is refused before copying, through the existing
  free-space check.
- `acquire --file` defaults its budget to the selected format's limit;
  `--max-file-bytes` can lower it. The TUI uses the same default.
- An acquisition's deadline is five minutes plus a second per 512 KiB of its
  byte budget, about 73 minutes for 2 GiB; each recording track gets the same
  allowance for its declared size. The file client's total request bound is
  two hours, and a response body that delivers no bytes for 60 seconds ends as
  a transient timeout, so an acquisition continues it from the bytes it has,
  as for a dropped connection (decision 0023), instead of waiting out the
  deadline.
- `mp4-audio/1` reads a holding at offsets instead of into memory. After the
  read the whole file is hashed and its identity, size, and time rechecked, so
  a file changed while it was read is refused as `changed_source`.
- ClamAV runs with `--max-filesize=2048M --max-scansize=2048M`, so its limits
  cover every file Nemalo holds. A scan's deadline is two minutes, thirty
  seconds per further file in a batch, and a minute per 256 MiB, at most thirty
  minutes.
- A `FOUND` line whose signature begins with `Heuristics.Limits.Exceeded` is
  an incomplete scan, never a detection and never clean, both for one file and
  in a batch. A real detection beside it is still a detection. An incomplete
  acquisition scan keeps the file in intake as untrusted, and it cannot be
  checked until a later scan completes.

## Consequences

Long single-file audiobooks can be acquired, checked, held, listed, and opened.
A library that holds a file over 256 MiB needs this version or newer. A check
of a large file copies it to a private snapshot first, so it needs that much
temporary space and time. MP3 tracks, ZIP members, recording tracks, catalog
snapshots, and inventory hashing keep their 256 MiB limits. A file over 2 GiB,
or a scan slower than its deadline, stays outside checked status.

## Validation

Scanner tests cover a limit alert alone (exit 1 and exit 2), beside a
detection, and in a batch, and the deadline formula. A discovery test feeds a
body that sends steadily for longer than the stall timeout and then stops: it
ends with a transient timeout after the bytes it sent, and a whole body is
never cut. An acquisition test checks the transfer budget. Assessment tests inspect
a 300 MiB M4B through a sparse reader without its media data, refuse an EPUB
of that size, and refuse an M4B over 2 GiB. A content test rewrites a file
while it is read, restoring its size and time, and the following hash refuses
it.

Live, on this machine: the synthetic 936 MiB ALAC M4B was checked with a
ClamAV scan in 126 seconds that reported no detections, imported as checked,
listed in under a second, verified by `library check`, and exported. From
Archive, `cambridgemodernhistory5_2307_librivox/CambridgeModernHistory0501-24_librivox.m4b`
(379,543,539 bytes, MD5 equal to Archive's published value) was acquired with
`acquire --file` and the default budget, scanned with no detections, and
located 2,032,823 AAC samples for 47,202,057 ms and 24 chapters, the sample
count and duration FFprobe reports. It imported as checked on the
acquisition scan bound to the same bytes.
