# 0087: source-bound evidence and declared citation export

Status: decision recorded before Go implementation, 2026-10-23. Not implemented
or released. Stage 3 passed all six hosted jobs and Go/Actions CodeQL at
`5091ce7`; its [receipt](https://github.com/blisspixel/nemalo/issues/60#issuecomment-6374231710)
was read back while issue 60 was open, before closure. Stage 1 is also accepted.
This implements the pre-1.0 boundary in [decision 0048](0048-cited-evidence-before-1-0.md)
and [issue 39](https://github.com/blisspixel/nemalo/issues/39); generated analysis,
model execution, workspaces and source capture remain post-1.0.

Implement selected declared metadata and citation export first, then opt-in
evidence/replay using that same view, then agent guidance and actual semantic
acceptance. Partial implementation does not close issue 39. Every published wave
requires its own actual verification; the final stage receipt also needs the
roadmap's agent-output review.

## One existing retrieval route

Add explicit `--evidence` and `--evidence-reference TOKEN` to `content read`, with
corresponding optional `evidence` and `evidence_reference` inputs on existing
`content_read` and `content_decode_read`. Both call the existing `Service.Content`.
No new decoder route or tool is registered: ordinary content tools remain
decoder-free, and decoded evidence still requires the existing per-call CLI
permission or separately enabled named MCP decoder tool. Units, locate, source,
resource and TUI/default operations keep their current behavior.

Return optional `evidence` in the existing result, omitted for ordinary reads.
Its schema is `nemalo-evidence/1`. It records the exact asset/library, declared
work/edition/version/source and recording relationships, extractor and
representation, exact source quotation or audio payload hash and typed locator,
and single-request coverage. Preserve each existing locator's offset units;
decoder samples never become MP4 packet/presentation times. Audio is not a
transcript. Label direct quotation/retrieval separately from any caller-generated
interpretation, which Nemalo neither accepts nor stores here.

The evidence reports units selected, unsupported units, located gaps, continuation,
stop/end facts and material unknowns. An end marker is not proof that other units
were read. A bounded response never claims whole-work understanding. Unsupported
reads may report coverage without inventing a quotation or a replay reference.

## Selected metadata and concurrency

Reuse the managed holding lookup, canonical journals and non-mutating shared
reader lock in `internal/library`. Select only by exact asset ID. A reader-lock
callback can keep the selected metadata stable while the existing content service
retrieves and revalidates bytes. Test nested shared reads and writer exclusion on
all native platforms. Explicit full decoded evidence can retain the reader lock
for the existing five-minute ceiling; never upgrade it to write authority.
The outer evidence operation, including metadata and lock acquisition, also has
a five-minute ceiling. Existing inner retrieval deadlines and a shorter caller
deadline still apply; metadata work cannot restart or extend that allowance.
Metadata refuses a torn control-journal tail without repairing it. Ordinary
content lookup retains its existing complete-prefix semantics; a recoverable
partial append must not appear as complete evidence metadata.

Reuse canonical recording derivation with no source probing, keeping declared
source/format/track selection, complete ordered-byte identity, provider narration
and matching user assertions separate. Missing/conflicting/incomplete relations
stay visible; do not derive recording identity from one track or merge editions.
Preserve identifier origins, confidence and omission counts.

The selected metadata projection is at most 256 KiB, including all exported
identity/provenance declarations. Refuse oversized metadata explicitly instead
of silently dropping identities. Existing journal file/work bounds still apply.
Omit private intake paths from portable citation output. Recorded metadata is
declaration evidence, distinct from verified retrieved bytes and rights/safety.

## Replay without authority

Use a separate canonical `nemaloevidence1:` base64url JSON token, at most 2,048
bytes. It binds schema 1, exact asset/library and selected metadata digest,
extractor, representation ID and a versioned digest of the exact retrieved
payload and typed range. Unknown/duplicate/extra fields, noncanonical encodings
and malformed hashes fail. This is an integrity comparison, not a signature or
an authorization credential.

Required token fields, in canonical order, are `schema_version`, `asset_id`,
`library_id`, `holding_sha256`, `metadata_sha256`, `extractor`, `representation_id` and
`retrieval_sha256`. The holding digest hashes the canonical selected journal
record, including declarations, before its bounded portable projection. The
metadata digest additionally binds the complete portable view, including current
recording relationships and user statements; a different track's change cannot
silently attach a new recording identity to a replayed passage.
For retrieval digest version 1, copy the successful `content.Result`, clear
`Evidence`, `Limitations`, `SourcePath` and `CatalogID`. Put that copy in `result`
and the caller's original bounded read parameters in `selection`, then serialize
this two-field object with Go's
`encoding/json`, decode with `UseNumber`, then reserialize the resulting objects
with sorted map keys and hash those bytes. Preserve number literals without a
float conversion; this makes digest version 1 independent of struct field order.
This is Nemalo's versioned representation, not a general canonical-JSON standard.
It binds actual text/resource bytes,
typed ranges, coverage, stop facts, continuation and the requested bounds,
independently of presentation
prose and source path display. New evidence rules require a new schema/token
version; earlier references remain explicitly selectable or visibly unsupported.

The token contains no paths, selectors, decoder flag or serialized command.
Return the original bounded read parameters separately. The caller explicitly
repeats those parameters with the token; a different selection fails rather than
silently changing the request. Roots/scopes and all existing flags are checked as
before. Do not reinterpret continuations or widen legacy cursor limits. A
continuation names the next range, not the passage just quoted.

The application verifies the expected metadata before expensive retrieval, then
compares the complete retrieved payload/range identity before publishing evidence.
Changed bytes, removed assets, metadata changes, unknown extractor rules or
different decoder output fail visibly. On any retrieval, metadata, reference,
serialization or close error, discard all quotation, audio/resource bytes,
evidence and continuation. No usable partial evidence escapes.

## Citation export

Add read-only `library citation LIBRARY --asset ID --json` and scoped MCP
`library_citation`, backed by one application service and the same selected
metadata. Return a standard CSL-JSON item array separately from Nemalo's versioned
sidecar of declarations, provenance, missing/ambiguous fields and unexported
identifiers. Consumers can pass only the array to a CSL processor. No renderer,
style download, new module, provider lookup or source-body access is needed.

Use the official CSL input schema at
[40b3ce0bbbda517ad14b98c93a2c65ab4cde56a9](https://github.com/citation-style-language/schema/blob/40b3ce0bbbda517ad14b98c93a2c65ab4cde56a9/schemas/input/csl-data.json),
checked 2026-10-23 alongside the [CSL 1.0.2 specification](https://docs.citationstyles.org/en/stable/specification.html).
Its items require
`id` and `type`, forbid arbitrary extra properties and permit `document`. Use
the exact asset as `id`; use the generic `document` type as a clearly labeled
export fallback when bibliographic type is unknown. Project unambiguous recorded
titles/languages and explicitly classified ISBN/DOI declarations only. Preserve
conflicts and unknown author/date/publisher/edition/type fields in the sidecar.
Never infer them from a filename, a similarly titled work, or another version's
receipt. Unknown metadata remains useful evidence of what is not established.

## Verification and stage acceptance

Test CLI/MCP/service parity, default compatibility, exact Unicode quotation and
all existing text/audio locator units, canonical references and exact replay,
stale/forged/mismatched references, permission separation, unsupported units/gaps,
metadata conflicts/limits/corruption, cancellation and full failure discard.
Assert no provider, scanner, decoder without permission, journal/control mutation,
initialization, repair, bookmark or consumption state. Use injected/synthetic
fixtures in normal tests.

Update the one portable skill for question-led structural investigation, exact
citations, qualifications, competing readings and coverage. Separately review
actual agent outputs on permitted public-domain works and an arXiv paper against
all milestone 9 acceptance cases. Mechanical provenance checks do not establish
faithfulness or understanding. Keep issue 39 open until that semantic review,
full canonical/native-race/exact-hosted gates and its receipt are complete.

The existing [reader/writer lock tests](../../internal/library/state_test.go)
exercise simultaneous shared readers and writer exclusion. The
[Windows LockFileEx contract](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-lockfileex)
documents the native shared byte-range lock used by `safeio`; reuse it rather
than adding a lock implementation. This is a concurrency boundary, not protection
against unrelated software directly modifying a library outside Nemalo's protocol.

The planned CLI shapes are `library citation LIBRARY --asset sha256:HASH --json`
and `content read LIBRARY --root LIBRARY --asset sha256:HASH --evidence --json`.
A replay repeats the same selection/max-bytes with `--evidence-reference TOKEN`;
an audio replay still supplies the existing explicit decode permission. These are
planned interfaces until their implementation and actual acceptance are recorded.
