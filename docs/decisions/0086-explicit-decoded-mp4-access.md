# 0086: explicit decoded MP4 content access

Status: accepted source implementation, 2026-10-23, at `5091ce7`. All six hosted
jobs and Go/Actions CodeQL passed; native CLI/reference/player checks and the
[stage 3 receipt](https://github.com/blisspixel/nemalo/issues/60#issuecomment-6374231710)
were verified before issue 60 closed. Native installed-tool/player evidence is
Linux-specific. Not released.
The user explicitly approved the bounded decoded-access proposal and revision
of the earlier check-only constraint. The preceding packet increment and CI
repair passed all six hosted jobs and Go/Actions CodeQL at `9e248a8`; its
[acceptance receipt](https://github.com/blisspixel/nemalo/issues/60#issuecomment-6372689487)
was read back exactly while issue 60 remained open. This accepted prerequisite
permits the separate Go implementation; it does not establish the new
increment's verification.

## Boundary and prerequisites

Reuse [decision 0085](0085-verified-mp4-packet-access.md)'s verified private
snapshot, MP4 index with one clear audio track and one description, and shared
content service. Keep `mp4-audio/1`, `mp4-audio/2`, `mpeg-audio/1`, their extractors,
selectors and continuations unchanged. The existing optional
[`check --decode`](0042-optional-ffmpeg-decode.md) remains available.

Add explicit `mp4-pcm/1`, extractor `nemalo.ffmpeg.pcm/1`, for M4B/M4A only.
CLI `content units` and `content read` require `--decode` on every invocation,
including continuation. A read accepts `--pcm-sample N`, a zero-based decoded
output sample, distinct from MP3 encoded samples and MP4 packets. An explicitly
supplied zero is still a selector and fails on other representations, units
listings or cursor requests. A cursor cannot authorize a decoder invocation.
No import, acquisition, TUI, provider or default content operation decodes.

Register separate scoped read-only MCP `content_decode_units` and
`content_decode_read` tools only with operator `--allow-decode`. Existing
content tools never decode, including when the operator enables those new tools.
Write or network permission grants no decoder permission. CLI and MCP call the
same application/content services and return the same data in their envelopes.
Do not install, download or bundle FFmpeg/FFprobe, or make them mandatory.

## Measurement and source identity

Parse the existing checked tables from the same verified private copy used by
the decoder. Give only its owned controlled private filename to `internal/decode`,
as assessment does, never the mutable original or managed stored path. Keep the
retained Go handle; revalidate private bytes after the child and original
identity/hash before success. Remove owned scratch on every return. No persistent
assessment cache, new holding, generated interpretation or consumption state.

Reuse or refactor `internal/decode`, preserving the existing check contract.
Sequentially decode the whole verified input from its beginning to mono signed
16-bit little-endian PCM at the selected probed stream output rate. Do not seek
or substitute short preroll. Count and hash the entire output while retaining
only the requested bounded sample interval; finish the full decode before
publishing any selected bytes. A late error, failed exit, partial sample,
timeout, cancellation, byte cap or changed identity discards the entire proposed
payload and continuation. Missing tools produce an explicit unavailable result.

Describe the output transform, downmix, quantization, selected rate and decoder
trimming explicitly. This sample timeline is actual output of that decoder
invocation, not an inferred packet/chapter/edit-list/presentation mapping. Keep
raw declarations and encoded table evidence separate from actual output. Use
exact tick/count strings where available; do not silently promote rounded or
saturated legacy millisecond summaries into exact decoded evidence. Unknown
mapping and coverage remain explicit.

`mp4_pcm` carries output format/rate/channels, full sample count and PCM SHA-256,
tool version, separate source declarations, selected sample span and range PCM
hash. Sample positions/counts are canonical decimal strings. A separately typed
nested locator binds the original asset, extractor, representation and half-open
decoder-output sample span; do not reinterpret legacy locator fields.

The representation identity binds source asset, extractor, transform, tool
version, rate, total samples and the complete observed PCM hash. Use a dedicated
`mp4pcm1:` prefix with strict canonical versioned base64url JSON for its cursor,
binding that identity and the next decoded sample. Leave generic cursors and
their validation unchanged. A new invocation with a different full-output hash
or incompatible version refuses a stale continuation, even for identical input.
A caller-supplied report neither proves measurement nor authorizes execution.

A successful read returns a complete small mono PCM WAV in `resource`, separately
hashed, with the selected PCM hash reported independently. Write its fixed
44-byte RIFF/PCM header in Go from checked small lengths. A client may explicitly
save and play this derived response outside the library; Nemalo does not launch
a player or publish a derived holding. Return no partial sample or misleading
empty playable file when the requested position is outside actual output.
The [RIFF layout](https://learn.microsoft.com/en-us/windows/win32/xaudio2/resource-interchange-file-format--riff-)
and [PCM format fields](https://learn.microsoft.com/en-us/windows/win32/api/mmreg/ns-mmreg-waveformatex)
provide the bounded container contract; whole-sample block alignment is required.

## Budgets and external process limits

- Source at most 2 GiB; initial copied-stream hash and final original revalidation
  read up to 4 GiB plus bounded EOF probes. Private parser work retains the shared
  80 MiB allowance. Private-file revalidation adds at most 2 GiB of Go reads.
  Charge actual failed reads as well.
- One private source copy on disk. No full-waveform file/allocation; retain only
  the selected response, at most 64 KiB including its 44-byte WAV header and
  within the existing 1 MiB JSON cap. A read needs room for a whole 16-bit sample.
- At most 8 GiB observed streamed PCM output. Count actual observed bytes,
  including the failing chunk, and cancel at the limit. Bound Go chunks and
  disclose pipe/in-flight overshoot; it is not a hard process sandbox.
- At most three children: version, bounded FFprobe metadata, one FFmpeg decode.
  Use argument arrays, fixed mov demuxer, file-only private input, explicitly
  disabled external data references/absolute paths, one decoder/filter thread,
  bounded diagnostics and the existing two-second pipe wait limit.
- Five-minute service deadline for this opt-in representation, with shorter
  caller cancellation winning. A small response can require full-file work.
  The available decoder input is bounded by the private snapshot; cumulative
  internal child reads and aggregate memory are not measured directly by Go.
  Time/output/thread/input bounds are not an OS memory or I/O sandbox.

No new module, cgo linkage, installed binary, toolchain upgrade or redistribution.
Reuse the deliberately installed optional tools and retain operation without them.
Normal tests use injected synthetic subprocesses and no installed decoder,
scanner, player or network.

## Evidence and acceptance

[Full sequential decode research](../RESEARCH.md#full-sequential-decoded-range-feasibility)
matched 48 selections against a separate full-output reference across eight
synthetic profiles, including AAC noise substitution. Forty nonempty bounded WAV
client outputs played back through mpv byte-for-byte. This is native Linux
feasibility, not application or cross-platform acceptance.

Test arbitrary split/odd stdout chunks, exact sample/byte limits, failed-chunk
accounting, full-output digest/cursor binding, end-crossing and past-end requests,
version/rate/output changes, late errors, partial samples, missing/error/hanging
children, cancellation and full payload discard. Assert exact safe flags, child
counts, snapshot identity, scope and no selected-source/library mutation. Test
explicit zero and permission boundaries through CLI and MCP. Retain old check
and representation fixtures, worker limits and production durability.

Repeat actual public client/full-reference/player tests, including PNS, on the
final implementation. Require consequential self-review, canonical verifier,
full native-cgo race, links/examples, all six exact hosted jobs and Go/Actions
CodeQL. Keep [issue 60](https://github.com/blisspixel/nemalo/issues/60) open until
the full stage acceptance receipt is posted and read back while open. Cited
evidence, interface/platform and release gates follow the roadmap's order.

### Native verification correction

The first application head, `d0ae20c`, passed local verification, native
CLI/reference/player checks, five hosted jobs and Go/Actions CodeQL. Its
[Windows canonical gate](https://github.com/blisspixel/nemalo/actions/runs/40015192622)
failed in the new replacement fixture: Windows refused renaming the retained
open snapshot with `ERROR_SHARING_VIOLATION`. The library suite passed; Windows
race, reparse and binary smokes were skipped. This is failed acceptance evidence.

Test the actual native denial explicitly, including unchanged private bytes and
no delivered payload, and retain actual replacement refusal where the OS permits
it. Add a portable same-bytes/different-file fixture for the retained-handle versus
named-identity check, without requiring an otherwise forbidden rename. Skip no
case, accept no other rename error and retain all existing assertions, limits and
durability. The final named lookup uses the existing `safeio.Lstat`, which captures
Windows file identity when observed rather than deferring it until `SameFile`.
A separate verbose Windows CI step repeats both private-snapshot roots so its
native denial and every failure case can be audited without inferring them from
a package summary. Repeat full local/native/exact-hosted acceptance on the
corrected head.
