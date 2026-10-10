# Decision 0004: portable library snapshots

Status: accepted and implemented, 2026-10-08.

## Scope and identity

`internal/library` provides immutable catalog snapshots, literal holdings queries,
and preservation audits through `internal/app`. CLI and TUI use the same services
and presentation. This adds useful local library state without prematurely
implementing a mutable publication catalog or pretending a JSON file is a journal.
No dependency was added.

A schema-version-1 catalog records a random snapshot ID, creation time, informational
root hint, exact SHA-256 byte assets, every relative file location, and excluded
links/special files. All regular files are accounted for, including receipts,
archives, unknown types, and empty files. Equal hashes share one asset while
retaining locations. Different editions, translations, and narrations are not
merged by filename or approximate similarity. Work, edition, recording, track,
rights, and collection identities remain future domain work for snapshots.
Managed holdings record EPUB package identifier evidence separately; see
[decision 0010](0010-bibliographic-identity.md).

`--assess` records historical shared file-health evidence for recognized EPUB,
PDF, and MP3 filenames. One supported location per byte asset is checked against
the inventoried size/hash. EPUB titles/languages become searchable. Malformed or
suspicious files remain representable with their explicit assessment findings;
this baseline is not a checked collection. Other formats are still hashed. Empty
supported files cannot be assessed and prevent an assessed snapshot from saving.
No antivirus scan is requested. Use `check FILE --scan` explicitly for that service.

## Filesystem and resource boundaries

Source inventory is root-scoped, read-only, and does not follow links or open
special files. Assessment reuses `CheckInRoot` and its private bounded snapshot.
A complete inventory is required before saving. Default inventory limits remain
10,000 entries, depth 32, 256 MiB per file, and 1 GiB of hashed source bytes.
Snapshot/audit options cannot exceed 256 MiB per file or 5 GiB per inventory pass.
Both operations have a cooperative five-minute context deadline; a stalled OS
filesystem call can still block.

Assessment makes a second source pass over each supported unique asset, at most
another inventory byte budget. `bytes_read` counts inventory hashing and those
snapshot copies, excluding parser reads from temporary snapshots. Existing parser
expansion/token/member limits apply independently to each assessment. Files are
processed sequentially. This is not a transactional point-in-time filesystem
snapshot: changes after a file's checks require a fresh audit.

The catalog destination must have an existing parent outside the inventoried root.
Physical paths are checked to reject source-directory aliases. Preflight rejects
existing outputs before hashing; publication repeats the check. A private exclusive
pending file is written, synced, and closed, then published using an exclusive
[`os.Root.Link`](https://pkg.go.dev/os#Root.Link). Existing files are never replaced.
Hard-link support is required, with no weaker fallback. This protects against
partial JSON publication and competing Nemalo writers; it is not a sandbox against
a privileged local actor moving directories or altering content concurrently.

Normal completion removes the owned pending name. A process crash may leave a
`.nemalo-snapshot-*.pending` file for manual inspection; unknown pending files are
never automatically deleted. [`File.Sync`](https://pkg.go.dev/os#File.Sync) does
not establish durable directory entries on every filesystem. The result reports
this power-loss limitation rather than claiming durable journal recovery.

## Loading, browsing, and auditing

Catalog loading rejects links/nonregular files, files over 16 MiB, unknown JSON
fields, trailing values, invalid schema/identities, incomplete snapshots, invalid
relative paths, duplicate identities/locations, and mismatched health hashes/sizes.
Up to 100,000 total asset/excluded locations are allowed. Relative paths use `/`
and reject traversal, backslashes, colons, and NUL. This validation does not establish
that every filename is creatable on every OS. Browsing never opens content.

`library list` filters literal case-insensitive title, language, path, candidate
kind, or hash text. It supports limit 1-100 and offset 0-100000. The TUI shows the
50 matching assets per page; CLI and TUI pagination expose subsequent results.
Optional `all|epub|pdf|mp3` format filters use exact case-insensitive filename
suffixes. Receipts are excluded from EPUB-only results, while matching duplicate
assets retain every location. A filter is not a content validation verdict. No semantic
quality or relevance score is invented.

`library audit` requires an explicit root even when the catalog includes a root
hint. Relocated libraries can therefore use the same catalog across platforms.
It reports changed, missing, added, unverified, and changed excluded locations.
An incomplete traversal cannot establish missing locations. A complete comparison
with findings still exits unsuccessfully; `complete` describes traversal coverage,
not agreement. It never repairs, opens a reader, scans for malware, or deletes.

A hash establishes byte equality with this user-selected baseline, not authenticity,
rights, semantic completeness, or safety. Catalog JSON is editable and unsigned;
historical health evidence is not independently authenticated. Readers must not
use a loaded catalog as publication authorization.

## Validation and next boundaries

Synthetic tests cover duplicate locations, multilingual EPUB metadata, unhealthy
assets, same-size replacements, missing/added files, incomplete budgets,
cancellation, strict loading, path containment, source preservation, and concurrent
exclusive saves. Native CI exercises the same code on Linux, macOS, and Windows.

Mutable catalogs, writer locks, durable operation journals, provenance/rights
models, checked publication, recovery, and source cleanup remain explicit roadmap
gates. Reuse this identity and assessment seam rather than creating another
inventory/hash implementation when those operations arrive.
