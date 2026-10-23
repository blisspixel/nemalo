# 0083: source-bound MP4 packet evidence

Status: accepted design, 2026-10-23. Implementation and verification are tracked
in [issue 60](https://github.com/blisspixel/nemalo/issues/60), roadmap stage 3.
This decision does not finish that stage or change public content capabilities.

## Context

Stage 2 passed its complete hosted gate and exact open-issue receipt readback
at `1a9e67b`, before issue 57 closed. Audio access follows next. The existing
MP4 parser locates chunks and validates every track, but discards the selected
audio track's packet tables before returning its units-only summary. Source
ranges need those same checked tables, not another parser or duration estimates.

[AAC research](../RESEARCH.md#aac-range-boundaries-and-decoder-state) demonstrates
that packet selection, decoder history and presented PCM cannot be equated.
The existing `check --decode` opt-in remains unchanged. No decoder belongs in
default content retrieval, assessment, inventory or import.

## Decision

Add internal read-only packet evidence in `internal/audio` through the existing
MP4 parser. Keep `ProbeMP4`, `nemalo.mp4.samples/2` and public `mp4-audio/1`
behavior unchanged. Give the distinct packet rules their own extractor identity.
The new index retains the selected track's bounded, already validated tables
and the same open offset reader. It owns no file and never reopens a pathname.
The caller must keep that handle alive and bind results to its checked source
identity; application integration will reuse content's existing opened-handle
and final whole-source hash checks.

Select only one clear audio track with no assessment finding. Preserve explicit
unsupported results for fragments, protection, inconsistent tables, mixed sample
descriptions, misplaced/overlapping samples and unknown codecs. Existing notes
remain separate from findings. No new parser subset or checked policy is added.

A request selects a zero-based packet ordinal and a byte allowance of at most
64 KiB. Return whole original packets in track order, at most 128 per result,
with each exact source byte interval and half-open decode-time interval in the
track's declared media timescale. Never imply that a packet is one PCM sample,
that timescale equals sample rate, or that decode ticks are edited presentation
time. Preserve noncontiguous source spans. Never silently skip an empty packet;
identify it as an unsupported selected boundary. A byte cap that cannot hold the
first packet reports the required size without reading its body. Continuation
is the next packet ordinal only after delivered packets, with end and bound
states explicit.

Reuse existing box, metadata, track, sample/chunk and source-size limits. Walk
compact chunk/time runs rather than allocate another per-sample index. Bound
selected reads and result count independently; check cancellation during metadata
walks and before/after body reads. Failed reads return no deliverable payload.
An index does not grant a pathname, cleanup authority, format conformance,
decoder initialization/preroll, PCM equivalence or playback acknowledgement.

## Consequences and remaining stage work

This is exact source-packet evidence, not a playable container or a new public
representation. In particular, AAC decoder configuration and edit/roll metadata
must not be invented from the summary. The application contract, initialization,
presentation coverage, decoded timing through the existing explicit boundary,
verified-copy/player demonstration and public continuations remain issue 60
work. Record that design before public integration. No M4B generation, mandatory
transcoding, new decoder, executable, module, runtime or network request is added.

## Verification

Use synthetic Go fixtures for constant/variable packet sizes and timing runs,
noncontiguous chunks, 32/64-bit offsets, packed size tables, every refusal above,
empty and oversized packets, exact byte/packet bounds, short/failing readers and
active cancellation. Verify no audio body reads during indexing, exact delivered
source slices, no unbounded per-sample allocation and unchanged old probe output.
Normal tests invoke no installed tool or network. Separately compare generated
native packet tables and bytes against a maintained independent demuxer outside
the checkout. Keep packet agreement separate from the documented decoder-state
counterexample and actual playback. Require self-review, canonical verification,
full native-cgo race, documentation checks and actual hosted evidence before
claiming the increment verified. Issue 60 remains open for its unmet acceptance.
