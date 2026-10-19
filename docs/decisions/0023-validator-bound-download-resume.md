# 0023: validator-bound download resume

Status: accepted and implemented, 2026-10-11. Completes the resume part of
milestone 5 over selected acquisition ([decision 0007](0007-selected-acquisition.md))
and whole recordings ([decision 0018](0018-whole-recording-acquisition.md)).

## Context

Decision 0007 retained interrupted packets but never resumed them, so a broken
connection near the end of a large audiobook track cost the whole transfer.
Checked against Archive's data nodes on 2026-10-11: a file answers with a quoted
strong `ETag` (for example `"4549eb81-5ecde2"`), `Accept-Ranges: bytes`, and
`Content-Range`; a range request with a matching `If-Range` returns 206 with
exactly the requested bytes, and a mismatched `If-Range` returns 200 with the
whole file (RFC 9110, 13.1.5 and 14.4). Some nodes intermittently answer 500.

## Decision

- Every transfer syncs `transfer.json` in its packet before reading the body:
  status, `ETag`, whether it is strong, `Last-Modified`, declared length, final
  URL, and file name.
- `acquire archive:ITEM --file NAME --output PACKET --resume` continues that
  packet's content. The packet must have its intent and transfer records,
  partial content shorter than the declared size, and no receipt. A fresh
  evaluation must declare the same file, size, and checksums. The request
  carries `Range: bytes=N-` and `If-Range` with the recorded strong `ETag`.
  Only a 206 whose `Content-Range` is exactly `bytes N-(size-1)/size`, whose
  length matches, and whose `ETag`, when sent, is the same is accepted.
  A 200 (the file changed or the server cannot resume), a weak or missing
  validator, changed declarations, or a different range leave the packet
  untouched and say so.
- The retained bytes are read and hashed, the new bytes appended and hashed,
  and the whole file is checked against the declared size, MD5, and SHA-1, then
  inspected, read back, scanned, and receipted exactly as a transfer done at
  once. The receipt records `resumed_from`.
- `acquire --recording --resume` continues an interrupted track's own packet in
  place when that packet can be resumed for the same track and position; when
  the source refuses the range, the attempt stays untouched and the track is
  fetched whole under the next attempt name, as before.

## Consequences

An interrupted large file costs only the missing bytes. Integrity rests on the
same checks as a whole transfer, not on the validator alone; the validator only
ensures the server serves the same representation. Packets from earlier
prereleases have no transfer record and cannot be resumed. The TUI keeps
retained packets for review and does not yet offer resume.

## Automatic continuation

Added 2026-10-19. `acquire` continues its own dropped transfer up to three
times with growing pauses, through the same resume path: fresh metadata, the
recorded strong `ETag` in `If-Range`, exactly the missing range, and the
whole file verified. Only transport failures continue (an unexpected end of
body, a connection reset, a network error, or the transfer deadline while
the caller still waits). A refused resume, a mismatch, the budget, another
error, or cancellation does not. `continuations` in the result counts them.
Recording tracks continue in place the same way. Tests drop every response
after 150 bytes and complete after two continuations; a whole-file reply,
a weak validator, a non-transport error, a source that keeps dropping (three
continuations, then the packet is kept), and cancellation during the pause
each stop with the partial bytes kept and no receipt.

A request that fails before any body (a transient transport error, or a
429, 500, 502, 503, or 504 answer) is sent again at most three times in all,
after the longer of 2 or 4 seconds and the server's `Retry-After`, for Archive
and Gutenberg files alike. Nothing was written, so no packet state changes. A
`Retry-After` over 30 seconds, any other answer, or a policy failure is
returned at once. Tests cover a 503 then a reset then success, a 502 that
stays (three requests), a 404, a one-hour `Retry-After`, a refused
certificate, and cancellation during the pause.

## Validation

Synthetic tests cover a transfer cut after 40 bytes and resumed with a scan and
receipt; a server that answers with the whole file, a changed validator, a
different range, changed size, and changed checksum, each leaving the partial
content and writing no receipt; a weak validator; a missing packet; a resumed
recording track continued in place from byte 7; and the transfer primitive
rejecting a changed or short prefix and an overlong remainder. Live: a 14 MB
LibriVox track (`art_of_war_09-10_sun_tzu.mp3`) was interrupted at byte
2,596,864; the first resume met one of the node's intermittent 500 replies and
left the packet untouched, and the next completed, with MD5 and SHA-1 equal to
Archive's published values.
