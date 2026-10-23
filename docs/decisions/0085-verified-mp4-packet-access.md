# 0085: verified MP4 packet access

Status: accepted design, 2026-10-23. Implementation and verification are tracked
in [issue 60](https://github.com/blisspixel/nemalo/issues/60), roadmap stage 3.
This decision adds source access, not decoding or completion of that stage.

## Prerequisite and scope

The internal packet index and test-only scheduling correction passed all six
hosted jobs and Go/Actions CodeQL at `c95696e`; the
[acceptance receipt](https://github.com/blisspixel/nemalo/issues/60#issuecomment-6370722056)
was read back exactly while issue 60 remained open. Reuse that index and the
existing content, application, CLI/JSON and scoped read-only MCP boundaries.
Keep public `mp4-audio/1`, its extractor and units-only behavior unchanged.

Add explicit `mp4-audio/2`, backed by `nemalo.mp4.packets/1`, for units and bounded
original packet access. A read selects a zero-based `packet`, distinct from MP3
encoded samples and text byte offsets. An explicitly supplied zero is still a
selector and must be rejected on other representations, units listings or cursor
requests. No TUI default changes, new module, decoder, process or network call.
MCP content output explicitly maps Go byte slices to the JSON encoder's base64
strings, retaining output validation. This uses the already pinned schema library
as a direct import; no dependency or version is added or upgraded.

## Source binding and budgets

Resolve the selected asset through existing content services and capability-bound
opens. Copy from that same open handle into an exclusive controlled file in an
owned private temporary directory. Check space before copying and hash the actual
copied bytes against the requested asset identity before parsing. Parse metadata
and read packet payload only from that verified copy, using its retained handle.
Do not reopen the validated source path. Keep the existing final original-source
identity/hash revalidation; a later hash alone cannot bind earlier mutable reads.

The operation changes no selected source, library, bookmark or job state. Remove
owned working files on every return; cleanup errors cannot become success. This
is ephemeral read preparation, not durable publication or a second import policy.
Use the existing copy/hash, space-check and context-reader mechanisms.

Keep the existing 2 GiB MP4 source cap and parser limits. Original-source copy
and final hashing read at most twice that size, plus bounded EOF probes. Charge
actual failed reads as well. A single private-reader allowance of 80 MiB includes
metadata, chapter reads and delivered packets. Initialization is at most 16 KiB;
packet data at most 64 KiB and 128 whole packets; the encoded result retains the
existing 1 MiB ceiling. Temporary disk may approach 2 GiB. State those preparation
costs in capabilities and documentation; a small response is not cheap preparation.
Only this explicitly selected representation gets a five-minute operation bound;
earlier representations retain their existing fifteen-second bound. A caller's
shorter cancellation/deadline always wins.

## Returned evidence

Require the index's one clear audio track, one description and absence of format
findings. Return the exact original audio sample-entry bytes and their `stsd`
version as separately hashed, untrusted initialization evidence. The
[sample-description contract](https://developer.apple.com/documentation/quicktime-file-format/sound_sample_descriptions)
describes how readers interpret that entry; it is not a complete playable MP4
header or decoder verification. Refuse oversized initialization rather than
truncating it or silently replacing it with inferred configuration.

Return concatenated original packet bytes with each individual source byte span.
Hash this delivered payload separately from the whole asset and initialization.
Packet locators are zero-based, half-open ordinals within the one audio unit.
Media decode tick bounds use canonical decimal strings in JSON so large declared
values cannot silently lose precision in consumers. Keep media timescale,
container declarations, PCM samples and edited player presentation distinct.
Decoder preroll, PCM equivalence and presentation mapping remain unestablished;
neither configuration bytes nor a packet table resolves the measured AAC state
and player-alignment limitations recorded in the research.

A representation identity binds the exact asset, extractor, representation and
initialization. Continuations bind that identity and the next packet ordinal.
Never accept another representation's fields, skip an empty/unsupported boundary,
advance on failed reads or expose a usable partial result on error/cancellation.
Report byte/packet stops and known unsupported next boundaries separately from
end of track. A client may explicitly select a later packet; that cannot establish
coverage of unselected packets. Retrieval records no playback or consumption.

## Verification and remaining stage work

Use synthetic Go tests for immutable binding against changed/reverted sources,
failed/canceled copies and reads, disk refusal, scratch lifetime, initialization
bounds, noncontiguous packets, exact byte/packet limits, large tick strings,
stale/forged continuations and unchanged earlier representations. Test CLI and
MCP through the shared service, including denied path scopes and an explicit
zero selector. Normal tests invoke no installed scanner, decoder or network.
Separately compare native generated source selections to an independent demuxer
and demonstrate an explicit client consuming initialized packet output or a
canonical verified-copy reference in a real player. Do not equate playability,
requested window counts and whole-file PCM equality.

Require consequential self-review, canonical verifier, full native-cgo race,
documentation checks and all exact hosted jobs. Keep issue 60 open until its
remaining decoded timing and full player/integration acceptance are established.
The standing `check --decode` boundary is unchanged; this decision does not
authorize another decoder entry point or inferred timing from a supplied report.
