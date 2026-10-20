# 0059: bounded PAR2 metadata inventory

Status: accepted and implemented, 2026-10-20. Stage 2 of the
[ordered roadmap](../../ROADMAP.md#order-of-work-to-10), tracked in
[issue 51](https://github.com/blisspixel/nemalo/issues/51). Uses the explicit-root
inventory and filesystem boundaries in decisions
[0004](0004-portable-library-snapshots.md) and
[0008](0008-filesystem-and-display-boundaries.md).

## Decision

`inspect DIRECTORY --par2` requests read-only PAR2 packet metadata. Ordinary
inventory still reads no file bodies by default; `--hashes` remains a separate
choice. No verifier, repairer, parity generator, new module, executable, source
mutation, network operation, or checked-library policy is added.

The [PAR 2.0 specification](https://parchive.sourceforge.net/docs/specifications/parity-volume-spec/article-spec.html)
defines bounded headers with lengths, packet checksums, declared set IDs and
types. Main, file-description and creator packets provide metadata. Recovery
slices contain parity; input-slice checksum packets describe protected data.
These are distinct from evidence that protected files are present or repairable.
The maintained [reference format definitions](https://github.com/Parchive/par2cmdline/blob/master/src/par2fileformat.h)
were also checked. No reference implementation code is incorporated.

- With `--par2`, probe at most the first eight bytes of each inventoried regular
  file. This finds renamed recovery files without treating a suffix as identity.
  A `.par2` name remains a filename candidate when its signature does not match.
  Read packet headers only from offset zero and successive bounded declared
  extents. Do not search arbitrary payload bytes for another marker or resynchronize
  damaged streams; report the unresolved coverage instead.
- Use the same capability-bound regular-file open for metadata and any requested
  SHA-256. Revalidate identity, size and modification time after reading. Changed
  files contribute no hash or set declarations. Packet filenames are untrusted
  text, never paths to open, checksums to pass to Import, or permission to inspect
  a protected file. Preserve opaque filename bytes when UTF-8 cannot represent them.
- All reads, including probes, count against existing per-file and total read
  budgets. Metadata has additional ceilings: 1 MiB per file, 16 MiB per run,
  256 packet attempts per file, 4,096 per run, 128 declared set IDs per run,
  and a two-minute inventory deadline. These counters span files and failed
  attempts. Existing entry/depth limits bound the number of files. Requested
  hashes and metadata share the actual read allowance; neither gets a reset.
- Require each packet extent to contain its header, be aligned as the format
  requires, fit the opened file, and advance without overflow. Allocate no
  buffer from a parity length. Read/check only bounded main, file-description
  and creator bodies: at most 1,024 main file IDs, 1,024 filename bytes plus
  permitted padding, and 1,024 creator bytes. Use the standard library MD5 only
  for the format's packet checksum, never as a security or deduplication identity.
  Recovery, input-slice-checksum and unknown packet bodies remain unread and
  explicitly unchecked. Unknown types are reported without invoking an adapter.
- Group header declarations by declared set ID, not by filename stems or volume
  numbers. Only matching small-packet checksums supply decoded declarations.
  Preserve distinct main and file-description variants and their locations;
  identical repeats are repeated observations, while contradictory mains,
  file IDs or names are visible conflicts. Do not select a winner. Do not enforce
  the current main-body-derived set-ID calculation, which the specification says
  readers must not assume permanently.
- Header traversal coverage, matching/mismatching/unchecked packet checksums,
  declarations and protected-data verification are separate fields. A known
  whole-file hash refusal makes the overall inventory incomplete without
  changing metadata coverage that already finished within its own bounds. A
  checksum mismatch is reported evidence, not permission to repair. Unbounded,
  truncated, malformed or limited header traversal is incomplete. Intentionally
  unread packet bodies remain explicit coverage gaps, never checked data.
  No absent-file or recovery-capacity claim follows from this inventory.

## Interfaces and retention

`inventory.Limits.PAR2` is the application-service option. CLI text and JSON
use the same report. Optional `par2_metadata` fields are omitted unless requested;
the nested report has schema version 1, names its packet-metadata scope, and
always reports protected data as unverified and recoverability as unassessed.
Per-file evidence stays beside its ordinary inventory entry; grouped declarations
include their actual source locations. Existing envelopes and stored records
remain unchanged. No new MCP tool or alternate inventory policy is introduced.

Inventory defaults used by ingest, snapshots and cleanup remain unchanged.
Ingest lists and preserves PAR2 files, including uncertain and unsupported
material. Cleanup keeps loose recovery files and archives with unaccounted
recovery members. Set metadata cannot authorize removal or change holdings,
format assessment, rights evidence, scanner coverage or publication status.

## Validation

Use synthetic Go packet fixtures and injected readers to test independent sets,
renames, repeat/conflicting declarations, opaque/hostile names, unknown and
unchecked bodies, false suffixes, checksum mismatch, truncation, overflow,
budget exhaustion across files, cancellation and changed sources. Verify default
zero body reads, optional hashes, CLI/JSON output, and ingest/cleanup retention.
Tests run no installed recovery tool, scanner or network. A sparse one-terabyte
parity fixture proves that only its headers and later small metadata are read;
per-file and shared metadata limits, packet/set limits, and actual read counts
are tested independently. Tests preserve separate metadata and whole-file hash
coverage and qualify an unmatched offset-zero signature without claiming there
are no later packets. A bounded ten-second Go fuzz run with two workers passed
404,810 executions on 2026-10-20. This is metadata-reader evidence, not PAR2 client
conformance, data verification or repair acceptance. Canonical local and actual
hosted checks are recorded in issue 51 before closure.
