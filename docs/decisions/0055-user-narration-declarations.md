# 0055: explicit user narration declarations over held recordings

Status: accepted and implemented, 2026-10-20. Completes the bounded
non-LibriVox evidence path in [issue 44](https://github.com/blisspixel/nemalo/issues/44),
extending [decision 0044](0044-narration-identity.md) and the exact ordered-asset
identities of [decision 0052](0052-recording-byte-identities.md).

## Decision

A user can explicitly assert that selected, already-held recordings carry one
reading. The declaration does not require original intake packets, modify provider
provenance, infer a reading, or copy audio. Holdings remain the only resolved asset
records. This is a user relationship statement over those records, not another
catalog or a source of acquisition, rights, scan, or publication policy.

The input is a UTF-8 JSON user file with schema `narration-declaration/1`,
`narration_id`, and `recordings`. A selection contains `source_id`, `source_format`,
`kind` (`tracks` or `single_file`), and `byte_identity`: its exact
`recording-sha256:HEX` from `library recordings`. That digest binds every asset
and repeated position in declared order. The file has at most 16 distinct
selections and 64 KiB. Selection order is normalized; unknown fields, duplicate
keys, invalid identifiers, and trailing data are refused. Narration IDs are
`user-narration:NAME`, scoped by the existing library ID. NAME starts with a
lowercase letter or digit and has at most 64 lowercase letters, digits, dots,
underscores, or hyphens. This namespace is distinct from `librivox:PROJECT`.
Evidence is always `user_declaration/1`; a provider receipt cannot create it.
Supported selections are the existing MP3 track-set and packet-origin M4B
single-file recordings. A loose MP3 or arbitrary M4A is not newly assumed to
be a complete audiobook.

`library narration LIBRARY --declaration FILE` previews the statement and its
exact selected assets. `--apply --plan DIGEST` repeats the selection and verifies
stored bytes before writing. Both preview and apply require complete,
unambiguous current recording selections matching every supplied byte identity;
provider narration evidence is not replaced. Verification is offline, bounded to
4 GiB of unique assets and ten minutes, with cancellation. No scanner runs.
A plan binds the library, action, normalized declaration, and relationship journal
state. Refusals withhold replacement tokens. Repeated declarations with a fresh
preview are no-ops. A changed journal requires another preview.
The token compares a reviewed proposal; it does not grant write permission.

`library narration LIBRARY --retract DECLARATION_ID` previews withdrawal of one
user statement; `--apply --plan DIGEST` records it. Historical evidence remains.
Retraction can resolve conflicting assertions without rewriting provenance or
removing assets. Repeating a retraction with a fresh preview is a no-op.

## State and recovery

The optional `.nemalo/narrations.jsonl` contains canonical, sequenced, library-bound
`declared` and `retracted` facts. A declared fact includes the normalized statement
and its content-derived declaration ID. A retraction names an existing declaration.
The ID is `narration-declaration-sha256:` plus lowercase SHA-256 of canonical
struct JSON, without LF, with fields `library_id` then `declaration`. The nested
statement fields follow the input order defined above; selections sort by source,
format, then kind. A fixed construction vector is pinned in the tests. New
identity construction rules require a new schema rather than remapping an old ID.
The journal is bounded to 1 MiB and each line to the existing 64 KiB record limit.
It stores no resolved holding or audio. Older libraries need no rewrite; older
executables refuse this new control file rather than silently ignoring it.

The existing OS writer lock, capability-bound file opens, canonical append, sync,
and torn-tail recovery are reused. Every writer first finishes a prior rescan;
a pending import refuses a new declaration. One complete journal line commits
one relationship fact, as explicit bookmarks do. There is no multi-file content
operation requiring an intent/completed pair: bytes and holdings never change.
An empty or bounded unterminated last line records no fact. Readers preserve it;
the next authorized declaration append removes it before writing. Invalid complete
records, sequencing, IDs, or transitions are retained and refused. A complete
append left by a crash is recognized by content identity and synced on retry. File sync is
required; directory-entry durability across power loss depends on the filesystem.
Initialization remains two records and no dependency is added.

## Retrieval

The shared recordings service exposes user declarations, their evidence origin,
active/retracted state, and per-selection match status separately from provider
`narrations` and `recording-bytes/1` evidence. Only a wholly matching active statement
can contribute a user narration group. Missing selections, incomplete sets,
changed ordered assets, conflicting user IDs for a selection, and later provider
narration evidence are visible and do not silently apply a statement to new bytes.
Provider-only field meanings and schema IDs stay unchanged. Source filtering
projects a globally evaluated result; it cannot hide a conflict. Journal-only
listing is historical evidence, not a fresh integrity check. Source probing still
reports changed or missing stored files separately.
Asset lists are returned in write previews and in the existing recording/byte
manifests; they are not multiplied into every historical declaration status.
Selection evaluation publishes no partial user groups on cancellation.

CLI/JSON, `library related`, TUI rendering of recordings, and read-only MCP retrieval
use the same library/application services. This increment exposes no MCP writer;
existing write permissions and tool scopes do not expand. No title, author,
duration, book identifier, audio matching, or model supplies narration identity.

## Verification

Synthetic fixtures relate a two-track MP3 recording, another encoding split into
three tracks, and one M4B by an explicit user statement. A mirror retains every
source and reuses the same assets and byte identity; another reading with the
same work/title stays separate. Original intake packets are removed before
declaring, so the fixture exercises curation from holdings alone.

Tests cover normalized repeat declarations and withdrawals, reactivation, stale
tokens and changed user files, conflicting user IDs, global conflict evaluation
under a source filter, insufficient selections, changed/missing stored bytes,
and later provider evidence. Withdrawal still works when audio is missing.
User files cannot choose provider evidence origin. Existing provider identities,
holdings, source records, and initialization records remain unchanged.

State tests cover strict JSON and transitions, library scope, a fixed declaration
ID vector, byte/selection/journal limits, OS locks, cancellation, pending imports,
prior rescan recovery and separate stale refusal, and refusal without initializing
an uninitialized root. Full-disk tests leave zero or partial bytes without a
fact; a complete unacknowledged append is recognized on retry. A bounded tail
is recovered even at 64 KiB and when physical file length exceeds the journal cap,
while malformed complete state is retained and refused. Human output tests cover
untrusted terminal controls; CLI/JSON and read-only in-memory MCP use the shared
services and preserve scope and permission boundaries.
An input that fits 64 KiB but expands beyond the canonical record limit is
refused during preview, before a plan is offered.

`go run ./cmd/verify` passes locally, including 85.3% aggregate coverage and
govulncheck with no reported vulnerabilities. `CGO_ENABLED=1 go test -race ./...`
passes with the native C compiler. Local validation used an explicit temporary
directory on the main filesystem after `/tmp` exhausted its inode budget; no
unrelated temporary data was removed. Actual hosted native results are recorded
in the issue closeout after the run.
