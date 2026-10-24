# Development and operation

Nemalo is an early Go CLI/TUI application. Go 1.27.3 is pinned in `go.mod`.
The launcher may download that patched toolchain when the local Go installation is
older; this does not replace the system installation. No Python or Node runtime is
needed for the application, tests, or first-party tooling.

`go run ./cmd/nativecheck --binary FILE --commit FULL_SHA` exercises a clean,
cgo-free executable for the current native platform over 39 CLI cases. It creates
a new private workspace outside the checkout, retains its receipt and bounded
diagnostics, and never adopts existing data. Optional `--output` names a new
absolute outside-checkout directory. External tool discovery is disabled and
unscanned holdings remain in review. This is acceptance tooling, separate from
installed scanners, real agent hosts and interactive terminal validation; see
[decision 0091](decisions/0091-native-executable-lifecycle-acceptance.md).

## Current operations

`help`, `version`, `doctor`, `search`, `evaluate`, `catalog fetch`, `acquire`, `inspect`, `check`, `library snapshot`,
`library list`, `library audit`, `library check`, `library import`, `library ingest`, `library cleanup`, `library restore`, `library purge`,
`library rescan`, `library recordings`, `library citation`,
`library open`, `library export`, `library init`, `library status`, `bookmark`,
`content units`, `content navigation`,
`content read`, `content locate`, `content source`, `content resource`, `content capabilities`,
`read`, `mcp serve`, `jobs`, and `tui` are implemented.
Managed `content read` can add `--evidence`, or compare the same explicit
selection with `--evidence-reference TOKEN`. `library citation LIBRARY --asset ID`
exports recorded metadata as CSL-JSON plus an uncertainty/provenance sidecar.
These are read-only and offline; citation export opens no stored bodies, and
evidence keeps each representation's existing retrieval and decoder boundaries.
TUI Read exposes citation metadata with F2 and evidence for its last successful
bounded excerpt with F4. A separate report provides explicit F3 replay and Escape
return without replacing ordinary navigation or bookmarks
([decision 0090](decisions/0090-terminal-cited-evidence.md)).
See [evidence and citation export](CONTENT-ACCESS.md#evidence-and-citation-export).
The [five selected semantic cases](CITED-EVIDENCE-REVIEW.md) are reviewed
separately from the exact hosted acceptance ledger in
[issue 39](https://github.com/blisspixel/nemalo/issues/39). Implemented interfaces
alone do not complete the stage or establish a release.
`collection diff`, `collection status`, and `collection acquire` compare exact
declarations, resolve holdings, and acquire missing selections.
Search defaults to Open Library work metadata; `--source archive` selects Internet
Archive text/audio item search. `--source gutenberg` reads a local
`pg_catalog.csv` (`--catalog`, `gutenberg_catalog`, or `NEMALO_GUTENBERG_CATALOG`)
and does not contact gutenberg.org. `catalog fetch gutenberg --output FILE`
downloads that one official CSV. `evaluate gutenberg:NUMBER` without `--files`
reads that row and lists no files. `evaluate gutenberg:NUMBER --files` fetches
one bibliographic record from gutenberg.pglaf.org and lists its files. It does
not download an ebook. `acquire gutenberg:NUMBER --file NAME --output DIRECTORY`
downloads one EPUB that record names (`N.epub.noimages`, `N.epub.images`, or
`N.epub3.images`) from the same host. The record has no checksum. The transfer
checks the response length. Other formats are listed and refused. Neither
search nor a catalog row resolves a download. `evaluate arxiv:ID[vN]` resolves one
paper version through arXiv's query API and its license through OAI-PMH, and
`acquire arxiv:IDvN --file IDvN.pdf` downloads that PDF from export.arxiv.org,
one request every three seconds
([decision 0047](decisions/0047-arxiv-selected-versions.md)). `--source all` searches Open
Library and Archive at once, and Gutenberg's local catalog when one is
configured, each by relevance in its own section with its own status; one
source's failure does not hide another's results, results are not ranked
against each other, and the command fails only when no source completed.

Results keep the provider's relevance ranking, and the result names its order.
For Archive, `--sort identifier` pages repeatably and `--sort downloads` ranks by
the declared download count, a popularity signal rather than quality; both use
an identifier tie-break. Relevance ties can move between requests, and a repeat
across the two pages of one unaligned request fails rather than skipping items.
Only explicit query terms, paging/field parameters, and client identification go to
the selected fixed HTTPS source. Shared metadata responses are bounded to 2 MiB
of decompressed JSON, page sizes to 50, and requests to one per second per provider
instance, with a 30-second deadline including waits. Valid `Retry-After` on 429/503
postpones subsequent requests in that instance. Separate CLI processes do not
coordinate quotas. A dropped connection, a timeout, or HTTP 429, 500, 502,
503, or 504 is tried again at most twice within that deadline and pacing; no
other failure is retried, and no bulk harvesting occurs. Downloads are
continued, not restarted, after a dropped connection: `acquire` resumes its
own packet against the same strong validator, and `catalog fetch` requests
exactly the missing range.

Provider metadata requests use checked direct connections and fixed-host HTTPS
redirects. Environment proxies are disabled for this boundary; proxy-only networks
are currently unsupported. Private/reserved/mixed DNS destinations are rejected,
and connections use the validated numeric address with normal TLS validation.
Metadata/rights URLs returned by a provider are not fetched. See
[decision 0005](decisions/0005-provider-search-and-evaluation.md).
Gutenberg ranking and the catalog failure rules are in
[decision 0028](decisions/0028-gutenberg-local-catalog.md). Selected EPUB
intake is in [decision 0029](decisions/0029-gutenberg-selected-epub.md).
Explicit selected-file acquisition has its own
[intake and transfer contract](ACQUISITION.md). It does not publish checked holdings.

Inventory requires an explicit directory. It uses `os.Root`, skips links/special
files, and reports filename-based candidates without extracting or validating them.
Default limits: 10,000 entries, depth 32, 256 MiB of reads per file, and 1 GiB
total input read. Hashing is opt-in. Default inventory reads directory metadata only.
Limits, errors, and cancellation produce an incomplete result and nonzero exit.
Inventory performs no security scan; tool availability in `doctor` is not scan evidence.
Filesystem operations can still block on a stalled device; this is not a sandbox.

`nemalo inspect DIRECTORY --par2 [--hashes] [--json]` additionally probes the first
eight bytes of inventoried regular files, including renamed recovery files. A
`.par2` suffix remains a filename candidate when the signature does not match.
This is an offset-zero probe, with no search or resynchronization inside payloads.
Recognized streams report bounded packet headers, observed recovery/input-checksum/
unknown header counts, and matching, mismatched and unchecked packet checksums.
Only small main, file-description and creator bodies are checksummed and decoded.
Parity, input-slice-checksum and unknown bodies remain unread and unchecked.

Declarations group by declared set ID, with exact source locations, repeated
observations and conflicting variants retained. Declared filenames are reporting
text, never paths to open or permission to verify another file. Invalid UTF-8
metadata stays as opaque hexadecimal bytes. Packet MD5 checksums are format
evidence, never security identities; no protected file or recovery capacity is
verified, and no repair runs. Sources and holdings stay unchanged. Ingest and
cleanup retain recovery files, including archives with unaccounted recovery members.

Metadata shares the existing actual read budgets with optional SHA-256 hashing.
It also has 1 MiB per-file and 16 MiB per-run read ceilings, 256 packet attempts
per file and 4,096 per run, 128 declared set IDs, a two-minute run deadline, and
bounded declaration sizes (1,024 main IDs, filename bytes or creator bytes).
Counters span files and failed attempts. A large parity extent is skipped within
the opened file without loading it. Identity, size and modification time are
rechecked on the same opened file; changed sources supply no hash or declarations.

Optional `par2_metadata` JSON sections are absent without `--par2`; the grouped
report has schema version 1 and scope `packet_metadata_only`. Signature-probe
coverage, header traversal, bounded metadata coverage, packet checksums, requested
whole-file hashes and unverified protected data remain separate. A known checksum
mismatch is a finding; malformed/truncated/limited traversal is incomplete.
A hash refusal makes the overall inventory incomplete without changing metadata
coverage that finished. Header counts cannot establish missing files or recovery
completeness. See [decision 0059](decisions/0059-par2-metadata-inventory.md).

The TUI exposes search, evaluation, inventory, checks, and library services with session-scoped forms/results, selectable
search/holdings, adaptive detail panes, and complete report access. It makes no
startup network request. Source text and paths are escaped; reports are wrapped
at grapheme boundaries without discarding original evidence. Scopes and scanner
cloud policy remain explicit. See the [terminal guide](TUI.md) for keyboard
controls, budgets, paging, appearance, and application-view captures.
Content retrieval uses CLI/JSON and the TUI Read operation over shared services.
The default `epub-text/2` returns readable text with each omitted image, table,
or other element located as a gap. `mpeg-audio/1` indexes MP3 frames and returns
decodable frame ranges with decoder preroll; `mp4-audio/1` lists an M4B's or
M4A's sample tables and chapters, units only. Explicit `mp4-audio/2` returns
bounded original packets and their original sample entry, with exact source
spans and media tick strings. Each call prepares a verified private copy, with
up to 2 GiB temporary disk and 4 GiB source reads. It is not a playable range
container, decoded timing or verified decoder initialization. These representations
never decode. Explicit `mp4-pcm/1` units/read require `--decode` on every call;
they measure a full sequential installed FFmpeg output and return a small derived
mono PCM WAV with exact output-sample references. MCP requires independently
enabled `content_decode_units`/`content_decode_read` tools. Earlier content tools
and the TUI do not decode. Full stage acceptance remains separate from source
implementation ([decision 0086](decisions/0086-explicit-decoded-mp4-access.md)).
Capability negotiation, structured EPUB parts,
note references, original markup, and bounded local image bytes are available in
CLI/JSON, and through the read-only MCP server.
See [source-bound access](CONTENT-ACCESS.md) and [structured EPUB](STRUCTURED-EPUB.md)
for supported representations, exact contracts, bounds, and remaining issue scope.
`content navigation` lists source-declared EPUB contents, and
`content read --representation epub-nav/1` reads one entry until its declared
end ([decision 0026](decisions/0026-epub-navigation-and-chapter-selection.md)).
`bookmark enable LIBRARY` creates `.nemalo/bookmarks.jsonl`. Save, note, delete,
export, import, and resume are explicit. A library with that file cannot be
opened by an earlier prerelease
([decision 0027](decisions/0027-explicit-local-bookmarks.md)).

JSON mode uses schema version 1 and exit codes 0
(success), 1 (failed/incomplete operation), and 2 (usage/configuration error).
Without `--json`, an error is one line on stderr, `nemalo: ` and a plain
sentence: a request error names the host and the cause instead of the URL, a
file error says what is wrong with the path, and control characters and line
breaks from a source cannot start another line. A command that failed before
doing anything prints only that line; one that kept bytes, a journal, or a
detection prints its result first. `--json` keeps the full error and result.

## Source item evaluation

```sh
nemalo search 'collection:librivoxaudio AND title:"Art of War"' --source archive --limit 3
nemalo evaluate archive:art_of_war_chinese_1506_librivox --json
```

Archive evaluation requires a valid namespaced item ID and interprets at most
5000 files within the shared response budget. It retains source titles, languages,
creators, dates, publishers, rights, and license strings; these are declarations.
Source file sizes/checksums, filename candidates, restriction evidence, unresolved
file rights, and unassessed DRM are separate fields. Restricted or uncertain
items/files receive no download URL. ACSM/LCPL delivery documents are not content
downloads. Other URLs are escaped candidates constructed on the fixed source host.

`complete` means bounded metadata interpretation succeeded, including a restricted
item. No candidate URLs are probed or acquired; no files are scanned or opened.
An Archive audio item is not necessarily an audiobook, and multiple formats may
represent the same tracks. `recordings` groups MP3 files by declared format in
declared track order, with `order_source` and an `acquirable` verdict; see
[decision 0018](decisions/0018-whole-recording-acquisition.md). After ingest,
`library recordings LIBRARY [--source archive:ITEM]` lists held recordings in
declared order with missing positions and, with `--source`, verified and indexed
track timing ([decision 0020](decisions/0020-recording-manifest.md)). A held
M4B is a single-file recording. Recordings of one LibriVox reading share a
narration, named only when the LibriVox project names the Archive item back,
and different readings stay apart ([decision 0044](decisions/0044-narration-identity.md)).
The result's `byte_identities` relates complete declared recordings with the
same ordered asset hashes across source items, even without LibriVox metadata;
repeated positions are retained. It uses journal evidence, not a fresh integrity
check, and preserves every narration declaration. Different encodings and track
divisions stay distinct under byte identity
([decision 0052](decisions/0052-recording-byte-identities.md)). Explicit user
narration declarations can relate them separately, as described below.
An Archive evaluation copies the ISBN and Open Library edition and work keys the
item declares, import records them on the source, and `library related LIBRARY
[--asset ID]` groups holdings that share a stated work, edition, or version, an
arXiv paper, a narration, or a source item, without merging anything
([decision 0049](decisions/0049-declared-identifiers-and-related-holdings.md)).
A collection file names exact selections; `collection status` resolves them
against holdings offline, and `collection acquire` previews and, with `--apply`,
acquires only the missing ones into an intake directory for `library ingest`
([decision 0050](decisions/0050-collections.md)).
Conflicting track positions or totals keep a collection's recording incomplete,
with every conflicting asset listed for review.
An existing collection intake packet is resumed or offered for ingest only when
it records the item's exact source and file or recording format; a copied or
mismatched packet stays retained.
Registry confirmation of identifiers and rights policy remain planned.

## Explicit user narration declarations

`library narration` records a user's statement that selected, already-held
recordings carry one reading. It needs neither the original intake packets nor
another acquisition. First use `library recordings LIBRARY --json` to obtain
each recording's exact source, format, kind, and `recording-sha256:` identity.
Create a user file like this, replacing the illustrative source, format, and
all-zero digest with the exact values returned for your selection:

```json
{
  "schema": "narration-declaration/1",
  "narration_id": "user-narration:reading-one",
  "recordings": [
    {
      "source_id": "archive:my-recording",
      "source_format": "MP3",
      "kind": "tracks",
      "byte_identity": "recording-sha256:0000000000000000000000000000000000000000000000000000000000000000"
    }
  ]
}
```

Add the other exact selections carrying that same reading to `recordings`.
The name after `user-narration:` is scoped by the library ID, starts with a
lowercase letter or digit, and contains at most 64 lowercase letters, digits,
dots, underscores, or hyphens. Another reading gets another name even if its
title and work are identical. Files are at most 64 KiB with 1 to 16 distinct
selections; unknown or duplicate fields are refused. Input order is normalized.

```sh
nemalo library narration /path/to/library --declaration reading.json
nemalo library narration /path/to/library --declaration reading.json --apply --plan DIGEST
nemalo library recordings /path/to/library
nemalo library narration /path/to/library --retract DECLARATION_ID
nemalo library narration /path/to/library --retract DECLARATION_ID --apply --plan DIGEST
```

Review the statement and exact asset IDs, then replace `DIGEST` with its preview
token. Both declaration preview and apply verify the complete, unambiguous held
selections and their stored bytes, with a 4 GiB unique-asset budget and a
ten-minute deadline. Missing, incomplete, changed, or provider-named selections
are refused. No scanner or network operation runs, no audio is copied, and no
holding, source provenance, scan result, or access classification is rewritten.
Pending imports must finish first; an apply completes a prior pending rescan
and reports that recovery separately.

`user_declarations` retains each statement and its active/retracted state and
per-selection status. `user_narrations` groups only wholly matching active
statements. Conflicting user IDs for one selection are visible and suppress the
ambiguous group; withdrawal of one statement can resolve them. Later provider
evidence or changed selections leave a statement unresolved. Source filtering
does not hide a conflict in another selection of that statement. The origin is
`user_declaration/1`, separate from provider `narrations` and `byte_identities`.
`library related` cites the same user evidence. Ordinary listing matches journal
identities without rereading audio; `--source` still verifies current bytes and
reports gaps separately. Assertions never establish the same reading as a fact,
rights, or checked status.

The optional `.nemalo/narrations.jsonl` is a bounded relationship journal, not a
second catalog. A single synced declaration or withdrawal line is one fact;
holdings remain the only resolved records. Repeating a declaration or withdrawal
with a fresh preview is a no-op. Incomplete final lines are retained on reads and
removed only by an authorized subsequent append after the complete prefix is
validated. An invalid complete record is retained and refused. Retrying a complete
but unacknowledged append recognizes and syncs the recorded fact. Older libraries
need no rewrite; builds without decision 0055 support refuse this new control file.
MCP exposes these relationships read-only through `library_recordings` and
`library_related`; it has no narration writer. See
[decision 0055](decisions/0055-user-narration-declarations.md).

## Collection revision previews

`collection diff BEFORE AFTER [--json]` compares two explicit collection files
offline, without a library or writes. It reports both file hashes and names,
added and removed exact selections, changed notes, and changes in the relative
order of retained selections. Inserting or removing an item alone does not mean
the retained items were reordered; formatting alone can change the hash without
changing a declaration. Positions are one-based. A changed version or encoding
is a removal and addition, not an inferred edition or narration update.

```sh
nemalo collection diff collection-before.json collection-after.json
nemalo collection status collection-after.json ~/books
nemalo collection acquire collection-after.json ~/books --output ~/intake
```

Review the diff and acquisition preview before adding `--apply` to acquire.
Earlier holdings and annotations remain intact when selections are removed.
Comparison uses regular files, at most 1 MiB and 1,000 items each, through the
same strict parser as status and acquisition; invalid or cancelled input returns
an error and no partial diff. Parent directory aliases are supported. The service
refuses final file links; MCP may resolve an explicitly selected file link only
inside scope. No upstream rights, changed bytes, or transfer estimate is inferred.
See [decision 0053](decisions/0053-collection-revision-previews.md).

Acquisition previews also return `plan_digest`. Optional
`collection acquire ... --apply --plan DIGEST` requires the same local preview
facts before creating intake or requesting a provider. It binds collection bytes,
library identity, exact missing selections, packet actions and transfer/scan choices.
A changed preview refuses the apply and requires a new review. Existing explicit
CLI apply without a plan remains available. The digest is not a durable snapshot
or authorization: normal fresh source and packet checks still apply. The TUI's
Collections review always uses this guard, with a deliberate 1 GiB or 5 GiB total
transfer cap. See [decision 0089](decisions/0089-collection-acquisition-review.md).

## File health and scanning

Filesystem capabilities and output encoding follow
[decision 0008](decisions/0008-filesystem-and-display-boundaries.md). Linux requires
`/proc/self/fd`, macOS `/dev/fd`; missing facilities fail explicitly. EPUB compressed
ranges are preflighted; lifetime compressed reads are capped at 1 GiB, expanded
members retain their separate limits, and assessment has a five-minute deadline.
JSON escapes terminal-sensitive Unicode without changing parsed data.

```sh
nemalo check /path/to/book.epub --json
nemalo check /path/to/paper.pdf --expected-bytes 123456 --expected-sha256 HASH
nemalo check /path/to/book.epub --scan
nemalo check /path/to/book.epub --epubcheck
```

Supply real known size/hash values in the second example. `check` requires one
nonempty regular file, rejects final-component symlinks, and uses a private
temporary snapshot capped at 256 MiB, or 2 GiB for an M4B or M4A, and refuses
a snapshot that would not fit in the temporary folder
([decision 0040](decisions/0040-large-mp4-audio-and-scan-limits.md)). Source identity/size/mtime are checked across
copying. Temporary files are removed after assessment; cleanup errors fail the
operation. No content is rendered, executed, or fetched from embedded links.

Results include actual signature, measured bytes/hash, limited container evidence,
review findings, limitations, and separate antivirus status. EPUB checks bound
members and package entries to 10,000, ZIP metadata reads to 2 MiB, expanded bytes
to 128 MiB total and 32 MiB each, and HTML/XML tokens to 1 MiB. XHTML URI
inspection also stops at 256 nested elements. They check ZIP
lengths/CRC, mimetype, container/manifest/spine references,
then report titles/languages, HTML reading-order documents, images, and
non-whitespace body-text characters. EPUB is internally ZIP; a plain ZIP or RAR
does not pass its package checks. Page count stays unknown. Text metrics are not
DOM validation or proof of semantic completeness. Positive script, event,
embedded-resource, and encryption indicators require review; absence proves nothing.
URI inspection recognizes entity-decoded and ASCII-tab/newline-separated
`javascript:` and `vbscript:` values, plus `data:` navigation links. XHTML uses
the standard XML decoder to recognize XLink namespace aliases and links after
self-closing elements; unrelated namespaced metadata is not navigation.
Malformed or over-budget XHTML cannot establish a passed inspection. Static
data images remain unfetched resource evidence, and ordinary local, HTTP(S) and
mail links retain their existing behavior. Active indicators require review even
when an antivirus reports no detections. Source-bound reads refuse these access
restrictions without returning partial text. This does not establish safe rendering
or comprehensive active-content detection; extractor rules and IDs are unchanged.
Existing holdings retain their historical disposition. An explicit import of the
same source with `--scan --apply` records a fresh disposition; `library rescan`
only selects eligible review holdings, not earlier checked holdings.
Font obfuscation alone is recorded, not a finding
([decision 0036](decisions/0036-epub-font-obfuscation.md)). PDF checks walk the page
tree and active content without reading page content
([decision 0032](decisions/0032-pdf-checked-policy.md)); MP3 checks index frames
without decoding ([decision 0033](decisions/0033-mp3-checked-policy.md)); M4B
and M4A checks place every sample from the sample tables, reading no media
data, and find truncation, misplaced samples, fragments, and protected audio
([decision 0039](decisions/0039-mp4-audio-samples.md)). `--decode` decodes an
MP3, M4B, or M4A in full with an installed FFmpeg, through the file protocol
and a fixed demuxer only, and reports decoded duration and decoder errors as
evidence ([decision 0042](decisions/0042-optional-ffmpeg-decode.md)).

`--expected-bytes` and `--expected-sha256` compare known external expectations.
`--epubcheck` runs an installed EPUBCheck on an EPUB snapshot; reported errors,
or a check that could not run, are review findings, and it is not part of the
checked policy ([decision 0038](decisions/0038-optional-epubcheck.md)).
Pass a publisher SHA-256 when one exists. A match shows the bytes are the
published bytes. A locally computed hash alone does not authenticate a source,
and a checksum does not prove the file is free of malware. File size without
edition evidence cannot decide whether a poem, novel, or scan is complete.
`limited_checks_passed` means exactly those checks passed, even when antivirus is
`not_scanned`. Review/invalid/incomplete results exit 1 and never publish content.

`--scan` (or TUI scan mode) is recommended for a file you did not produce. It
invokes installed Defender on Windows, otherwise ClamAV. `doctor` names that
default and, without executing a scanner, reports definition age from the
ClamAV daily database header or Defender's registry values; definitions older
than seven days are `stale`. Without usable scan evidence, the human output of
`check`, `library import`, and `acquire` ends with a `Next:` line: `--scan`
when a scanner is installed, otherwise the platform install command (pacman,
apt, dnf, zypper, apk, Homebrew, or Windows Security). Nothing is installed or
updated. On Linux and macOS, install the
distributor package that provides `clamscan` and update signatures with
`freshclam` first. On Arch, including Omarchy, that package is `clamav`. Calls
use argument arrays and no remediation, with deadlines of two minutes plus a
minute per 256 MiB and 64 KiB combined output. ClamAV requests limit and
encryption alerts; a limit alert is an incomplete scan, not a detection
([decision 0040](decisions/0040-large-mp4-audio-and-scan-limits.md)). A post-scan hash/identity check binds evidence to the snapshot.
Unknown output, failure, timeout, truncation, and missing tools remain unsuccessful;
Defender output in unsupported languages may be incomplete. No-detection output
does not independently establish signatures are current or every internal member
was inspected. Scanner cloud/sample-submission settings apply, without Nemalo
changing host preferences. Pure local checks make no network request.

Human output also names the command that continues the first-book path, before
any scan line. A local Gutenberg evaluation names `evaluate gutenberg:N --files`;
an evaluation with an eligible file names `acquire` for the first EPUB, else the
first PDF, by the same check acquire applies; a complete, undetected acquisition
names `library import` (the configured library, or `LIBRARY`); an import preview
names `--apply`; and a held EPUB or PDF names `read`, an audio file `library open`.
Arguments are quoted for a POSIX shell, or PowerShell on Windows, so a pasted
command interprets nothing from a source name, and a name holding a control or
format character is left out. `--json` output carries no `Next:` lines.

## Catalog snapshots and preservation

```sh
nemalo library snapshot /path/to/books --output /path/to/catalog.json --assess
nemalo library list /path/to/catalog.json --query "fr" --limit 20 --offset 0
nemalo library list /path/to/catalog.json --format epub --limit 100
nemalo library audit /path/to/catalog.json --root /path/to/books --json
```

Use an existing output directory outside the source root. Outputs never overwrite;
the destination filesystem must support hard links. Snapshots hash all regular
files, group exact duplicates without losing locations, and account for skipped
links/special files. Incomplete inventory prevents saving. Optional `--assess`
records shared EPUB/PDF/MP3 health evidence and EPUB titles/languages, without
antivirus. Invalid content can be cataloged with findings; this is a preservation
baseline, not publication eligibility.

Snapshot/audit use inventory defaults and accept `--max-*` limits, capped at
256 MiB/file and 5 GiB/inventory pass, with cooperative five-minute deadlines.
Assessment makes an additional source pass over supported unique assets. Catalog
JSON is bounded to 16 MiB. `list` searches literal metadata/path/hash text, with
limit 1-100 and offset 0-100000. TUI holdings show 50 matching assets per page.
`--format all|epub|pdf|mp3` uses exact case-insensitive filename suffixes, excluding
`.epub.receipt.json` from EPUB results. It does not manufacture assessment evidence;
invalid or unassessed files remain representable. CLI defaults to all files; TUI
defaults to EPUB filenames. Byte duplicates retain all locations even when only
one location matches the selected format.

Audits require an explicit root; the informational catalog root never authorizes
filesystem reads. They report changed/missing/added/unverified files, with nonzero
exit on findings or incomplete traversal. No repairs or source mutations occur.
Catalog health is historical and unsigned. File sync plus exclusive publication
prevents partial final JSON but does not guarantee directory-entry persistence
through power loss. See [decision 0004](decisions/0004-portable-library-snapshots.md).

## Library control state

```sh
nemalo library status /path/to/existing/library --json
nemalo library init /path/to/existing/library
```

Status is read-only and does not create control state. Init requires an existing,
explicit directory and creates only `.nemalo/writer.lock` and
`.nemalo/journal.jsonl`. Content is preserved and not inspected. Repeating init is
idempotent; valid interrupted initialization resumes with the same library ID.
Busy, corrupt, linked, oversized, or unexpected control state fails explicitly.
Do not remove a lock file to bypass another process. Preserve malformed state for
review; automatic repair/truncation is unavailable. The initialization journal
does not provide content import, publication, or cleanup recovery.

Writes sync journal files. Directory-entry power-loss persistence and network
filesystem coordination are not guaranteed. See the
[state decision](decisions/0006-library-control-state.md) for the full boundaries.
The TUI's State operation reads on Enter; F7 reviews initialization before a
second Enter writes. Ctrl+N sets one source and F8 reviews import. That review
does not scan. Ctrl+R reviews a rescan of the library; a second Enter scans and
records the results.

## Managed import

```sh
nemalo library import /path/to/library /path/to/book.epub
nemalo library import /path/to/library /path/to/book.epub --apply
nemalo library import /path/to/library /path/to/intake --apply --scan
nemalo library ingest /path/to/library /path/to/downloads --apply --scan
nemalo library audit /path/to/library
nemalo library check /path/to/library
nemalo library list /path/to/library --query "verne" --status checked
nemalo library open /path/to/library --asset sha256:HASH
nemalo library export /path/to/library --asset sha256:HASH --output /path/to/folder
nemalo library cleanup /path/to/library /path/to/downloads
nemalo library cleanup /path/to/library /path/to/downloads --apply --plan DIGEST
nemalo library restore /path/to/library --cleanup cleanup:ID --apply
nemalo library purge /path/to/library --cleanup cleanup:ID
nemalo library purge /path/to/library --cleanup cleanup:ID --apply --plan DIGEST
nemalo collection status /path/to/books.json /path/to/library
nemalo collection acquire /path/to/books.json /path/to/library --output /path/to/intake --apply
nemalo content read /path/to/library --root /path/to/library --asset sha256:HASH
nemalo content read /path/to/library --root /path/to/library --asset sha256:HASH \
  --representation pdf-text/1 --unit 3
nemalo content locate /path/to/library --root /path/to/library --asset sha256:HASH \
  --representation pdf-text/1 --text "exact quotation"
nemalo read /path/to/library --root /path/to/library --asset sha256:HASH
nemalo read /path/to/library --root /path/to/library --asset sha256:HASH --speed --rate 300
```

`read` opens the terminal reader (decision 0025) on EPUB or PDF text: pages
sized to the terminal, or one word at a time with `--speed`. `--unit` and
`--text-offset` choose the start; closing prints the values that return to the
same place. It requires an interactive terminal and has no `--json`. A missing
`--root` or `--asset` names that flag. For a managed library, `--root` is the
same folder as the catalog. `nemalo tui` has the same terminal requirement.
Without a terminal, both commands say so and exit 2.

A PDF reads as `pdf-text/1`: one unit per physical page, with the printed page
label separately, text in content-stream order, and located gaps; `check`
records its page count and labels and keeps an unreadable page tree in review
([decision 0021](decisions/0021-pdf-pages-and-text.md)).

Import requires an initialized library. The source is an EPUB, PDF, MP3, M4B,
or M4A file outside that library, or a completed intake directory with exactly
one `content.epub`, `content.pdf`, `content.mp3`, `content.m4b`, or
`content.m4a` and a matching `receipt.json`. Without `--apply`, nothing is
written. `--apply` copies the assessed bytes and leaves the source in place.
`--scan` can invoke installed antivirus. `checked` requires limited checks
that passed with no finding and a scan that reported no detections; a PDF,
MP3, M4B, or M4A also needs its format's examination (decisions 0032, 0033,
and 0039). Other results stay in review. For an intake packet, a clean scan
recorded at acquisition and bound to the same SHA-256 counts without scanning
again, but only where the holding has no usable scan evidence; it never replaces
a recorded detection. A packet whose receipt records a detection is refused. A later import without `--scan` does
not downgrade `checked`. A scan gap with no other finding is recorded as
`scan_` plus the scanner status. A conflicting stored file, an unexpected extra
hard link, or a store path whose parent is not a real directory fails before a
new intent is written. The existing bytes stay in place.

Preview exposes normalized incoming `source_evidence` and the full proposed
`holding`, including any retained source declarations and disposition. `--plan`
binds both records, byte identity, library identity, and the scan choice. Changing
stored provenance refuses an old plan before a new import write. A writer first
completes any previously pending rescan and reports `recovered_rescan` separately.
New import intents also bind the source and holding for recovery. Completed
legacy imports remain readable; an unfinished legacy import is retained and
must finish with its earlier executable and original source before upgrading,
or remain for review. See [decision 0054](decisions/0054-provenance-bound-import-plans.md).

`library ingest` imports a whole folder: every EPUB, PDF, MP3, M4B, M4A, intake packet,
and supported ZIP member, through the same import and journal, and lists every
other entry with a reason. ZIP members are extracted into private temporary
staging with path, size, ratio, and CRC checks and recorded as
`ARCHIVE!/MEMBER`. With 7-Zip installed, RAR and 7z members are extracted the
same way, one at a time by exact name to standard output, from a private
snapshot of the archive ([decision 0043](decisions/0043-rar-and-7z-through-7-zip.md)).
A split 7z (`NAME.7z.001`, ...) or a RAR volume set (`NAME.part1.rar`, ...;
or `NAME.rar`, `NAME.r00`, `NAME.r01`, ... through `NAME.z99`) is read through
its actual first volume. RAR volumes must declare compatible format, role,
ordinal, size, and adjacent split members; the assembled listing must account
for the exact count and total size without errors or unresolved splits. Missing
volumes, aliases, overlapping naming families, and inconclusive boundaries stay
preserved. This includes valid sets with no member split across a boundary,
since ordinals alone do not establish that those volumes belong together.
The set is at most 4 GiB and 1,000 volumes (901 in the old letter naming range).
Old-name identity probes accept only exact private reader fallback names at the
supported later RAR4 positions and RAR5 letter rollovers. They still require
matching split continuity and a clean assembled listing before extraction;
they grant no physical coverage or cleanup authority
([decision 0077](decisions/0077-old-rar-isolated-probe-names.md)).
Unsupported RAR sets remain preserved by cleanup; the modern RAR5 subset below
requires a separate complete physical proof. See
[decision 0046](decisions/0046-archive-volume-sets.md) and
[decision 0056](decisions/0056-rar-volume-identity.md).

An internal read-only opt-in now measures physical framing of modern root RAR5
sets, then binds each isolated volume table and the complete assembled table
before that same table drives extraction. Direct calls share source-copy, proof,
reader, process, diagnostic and output budgets. A split part's packed CRC is not
the final whole-file CRC, and framing alone does not validate extracted bytes.
The separately measured set version is omitted from JSON. This adds no public
option or application cleanup permission by itself. RAR4,
old-named and nested sets are outside this new coverage contract. Acceptance is
tracked in issue 57; see [decision 0073](decisions/0073-read-only-rar5-set-coverage.md).

Application cleanup now requests that capability for completely covered modern
root RAR5 sets under `rar5-set/1`. All volumes move as one recoverable entry only
after every leaf is held with verified bytes and final original hashes match.
Covered standalone children reuse existing tree checks; a child cannot lend its
proof to the outer set. Neither `archive-tree/1` nor `archive-tree/2` accepts RAR outer sets.
Fresh purge rechecks the complete held set and exact leaves; durable partial
recovery binds the original proof, file indices and leaf digest without joining
missing parts or adopting replacements. Raw restore needs no reader and replaces
nothing. That proof excludes old-name and nested-set layouts; their separate
authorities are described below. Unsupported material stays preserved.
See [decision 0074](decisions/0074-modern-rar5-set-cleanup.md). Source implementation
is separate from full acceptance, tracked in issue 57, and from a release.

An additional internal read-only `RAR4SetCoverage` requires both `Coverage` and
`RAR4Coverage` for modern root RAR4 sets. Physical framing binds exact end
ordinals/roles, known metadata, split records and the same isolated/assembled
tables used for extraction. Optional reserved end bytes must be exactly seven
zeros. That original scope excluded old naming and nested sets; volume-wide CRC
declarations remain unsupported.
Both versions share a format-neutral continuation/table matcher with separate
declaration fingerprints and explicit capabilities. Standalone facts and earlier
proof IDs keep their meanings. Source-copy and all work limits span every part
and failed attempt; direct covered calls still do not enable traversal. The
JSON-omitted root-set version is separate from cleanup permission. That read-only
increment kept the application flag false and required version 5 for its RAR5 writer.
See [decision 0075](decisions/0075-read-only-rar4-set-coverage.md), with acceptance
tracked in issue 57. This does not complete stage 2 or authorize a release.

Application cleanup now also requests RAR4 set coverage for the same bounded
modern root layouts and selects distinct `rar4-set/1` from measured version 4.
Version 5 retains `rar5-set/1`; no filename or child selects outer authority.
Both require complete physical/reader coverage, every verified held leaf and
unchanged original hashes. Persisted set shapes share validation, while fresh
purge compares the measured format and exact original proof. Immutable partial
recovery binds original evidence and current remaining bytes/holdings without
reconstructing a partly purged set. Unsupported metadata, old names and nested
volume sets stay preserved. See [decision 0076](decisions/0076-modern-rar4-set-cleanup.md).
Full local/native/hosted acceptance and actual preceding-executable retention and
raw restore are tracked separately in issue 57; no release is included.

Read-only root old-name RAR4/RAR5 coverage is implemented behind a separate
internal `OldRARSetCoverage` capability, requiring `Coverage` and the selected
format's set flags. The same physical framer and table matcher account for each
part of the closed 2..901-name family. Public modern RAR4 framing remains strict;
That original increment refused nested volume sets; unsupported records still
stay refused. Old facts use separate,
JSON-omitted `OldRARSetVersion`; modern and standalone facts remain zero. Shared
source, proof, process, diagnostic and output budgets include failed work.
The initial read-only increment kept every application flag false: even a fully
covered old set with every leaf held stayed preserved by preview and apply. Generated
native stored sets through 901 parts and compressed references at 2, 3 and 7
parts returned exact independently expected bytes; hidden records omitted by the
reader were refused before extraction. These measurements do not validate every
RAR variant. This read-only prerequisite passed all six hosted jobs at `1e927a2`;
its acceptance receipt was posted and read back in issue 57 before the separate
old-name writer decision. See
[decision 0078](decisions/0078-read-only-old-name-rar-set-coverage.md).

After that prerequisite's accepted hosted run, application cleanup now requests
the old capability for a separately validated root layout and selects distinct
`rar4-old-set/1` or `rar5-old-set/1` from measured old-set facts. Modern and
standalone proofs retain their scopes. Every exact leaf must be held with verified
bytes, snapshots must match planned hashes, and original sources must rehash
unchanged. Fresh purge repeats the complete proof and exact leaf check. Partial
recovery binds the immutable original proof, indices and leaf digest, with fresh
remaining-byte and holding checks; it never reconstructs a missing set or adopts
a replacement. Raw restore needs no archive reader and replaces nothing.
The accepted preceding executable retained unfamiliar plans and pending groups;
after canonical stop it restored remaining originals without a reader and reported
absent parts honestly. Separate current-version snapshots resumed whole and
partial purges. These native pending states were constructed durable snapshots,
not observed crashes. Unsupported records, ambiguous families, unheld material
and nested volume sets were outside those root-only proofs. See
[decision 0079](decisions/0079-old-name-rar-set-cleanup.md); full writer acceptance
remains tracked in issue 57, and this is not a release.

An internal `NestedVolumeSets` opt-in now permits tree intake and requested
read-only coverage of the existing modern/old RAR4/RAR5, split 7z, ZIP chunk and
ZIP segment layouts. Every part must independently pass outer extraction before
the same checked set adapter reads fixed private names. Grouping uses the shared
canonical volume-family rules; leaf provenance retains the original first member
name, even when it appears last in the outer table. Failed or uncertain parts
prevent the whole group, and all children share depth, output, proof, read,
process and diagnostic limits. JSON-omitted `CoveredVolumeSets` measures covered
child sets separately from root and standalone facts; it grants no cleanup right.
That read-only increment kept application callers false. Native generated combinations
returned exact independently expected bytes, while application cleanup retained
even fully covered sets with held leaves. This prerequisite and the test-only
counter-housekeeping correction passed all six hosted jobs at `1f63374`, with
exact acceptance readback before the separate writer decision. See
[decision 0080](decisions/0080-opt-in-nested-volume-set-coverage.md).

Application intake now explicitly enables the checked child groups through the
same tree and Import services. Preview, apply, repeat and lost-staging recovery
retain exact original first-part labels; uncertain parts never become book
imports. Incomplete trees report failure while independent verified leaves can
still import. Sources remain intact on intake.

Cleanup now requests full child coverage and selects distinct `archive-tree/3`
only with measured covered child sets, complete output, every verified held leaf
and bound unchanged original bytes. Root RAR format/layout facts stay independent.
Only /3 accepts safe exact first-part chains for chunks and disk segments. A child
set with no file leaves beside a direct held book also needs /3. Old-name RAR
chains can share historical standalone syntax; fresh purge must reproduce actual
authority rather than guess from names. The existing durable group binds the
proof, original indices and canonical leaf digest; partial recovery verifies
remaining bytes and holdings without reconstructing a missing tree. Earlier IDs
and receipts keep their meanings, and raw restore needs no reader. Unsupported,
incomplete and uncertain sources stay preserved. Writer acceptance is recorded
in issue 57 before closure; no release is included. See
[decision 0082](decisions/0082-nested-volume-tree-cleanup.md).

Split ZIP byte chunks (`NAME.zip.001`, `.002`, ...; three or four numeric digits)
are joined privately, checked as exactly one ordinary ZIP through installed
7-Zip, and extracted by the existing Go ZIP reader. True ZIP segments
(`NAME.z01`, `.z02`, ..., `NAME.zip`; two to four numeric digits) are copied
under fixed private names. The complete set must list successfully through every
part with matching count/total size/member tables and the opened part's exact
size/ordinal. Member disk/offset/packed extents must fit the snapshots. The first
segment must carry the standard split marker; archive comments, stubs beyond
that marker, tails, warnings, errors, or contradictory evidence preserve the set.
Old RAR rollover also uses `.zNN`: conflicting or absent RAR/ZIP anchors preserve
the ambiguous paths. A set has 2 to 1,000 volumes and at most 4 GiB. Preparation
shares two minutes and 16 MiB of reader output. Both ZIP layouts require installed
7-Zip for container evidence; missing tools preserve every part. Complete byte
chunks and true disk segments can move as one recoverable set after independent
byte coverage and held-member checks; all parts must share one exact parent.
Segments additionally require raw disk/count/offset and header-boundary checks.
A read-only view translates only validated references and excludes only the first
four-byte marker, preserving all other bytes for the shared ZIP framing proof.
Directories can span segments, but individual local/central headers cannot.
Raw and view proof reads share 16 MiB; one private file handle is open at a time.
The exact central names, sizes, CRCs, types and disk offsets must match the installed
reader's table before that table drives extraction. Unknown or unaccounted layouts
stay. Source rehashes share the cleanup's 64 GiB hash-work limit. See decisions
[0057](decisions/0057-split-zip-intake.md) and
[0061](decisions/0061-split-zip-stream-cleanup.md), extended by
[0062](decisions/0062-segmented-zip-cleanup.md).

Go ZIP directory parsing has a 16 MiB read budget before member processing,
with ZIP64 counts bounded before decoder preallocation, a persistent 8 GiB
compressed-read budget, and the shared archive deadline. EPUB assessment retains
its separate 2 MiB directory/1 GiB compressed-read budgets. These limits are not
antivirus or semantic-completeness evidence. A volume-evidence refusal happens
before extraction. Later member extraction failures make ingest incomplete;
independently verified extracted members can still be imported. This is the
existing per-member recovery model, not archive-wide publication atomicity.
An interrupted member previously read from a standalone terminal `.zip` resumes
first under that original source label when its segments are now grouped.
Nested standalone ZIP, RAR, and 7z children now traverse through the same
extractors and Import service, with four archive levels including the outer
container. Every tree shares 10,000 members, a 1 GiB copy allowance charged as
declared bytes plus the detection byte for every attempt, 8 GiB of Go ZIP read
work, 16 MiB of listing stdout and subprocess stderr, 1,000 subprocess calls,
and the outer archive deadline (at most an hour). Failed attempts do not refund
their allowance. Existing per-member, ratio, source-copy, and free-space checks
still apply. Reader snapshots can add up to 4 GiB beside the tree's bounded
member staging, so an outer snapshot can need 5 GiB of temporary space in total.
Traversal and extraction are sequential; a subprocess is not a memory sandbox.

Nested ZIPs require installed 7-Zip to establish one ordinary container before
the Go extractor reads the same private bytes. Standalone nested RARs must explicitly
declare one non-volume container; complete recognized nested sets use the checked
group adapter. Uncertain groups, unsupported archive types and missing reader
capabilities stay preserved with reasons. Tree intake
refuses every occurrence of a duplicate member name; it never cites an ambiguous
first occurrence. Depth/work limits or corrupt children make ingest incomplete,
while separately verified leaves can still import. Cleanup can move a complete
nested tree only when every container's original bytes are accounted for
and every non-directory leaf is held with currently verified stored bytes.
/1 remains limited to ZIP/7z nodes; standalone RARs require /2 and covered nested
volume sets require /3. Comments, unknown metadata, unheld notes and incomplete
trees preserve the whole outer source/set.

Cleanup requests coverage through the same tree. ZIP proof work shares 16 MiB
across every node, also consuming the existing 8 GiB reader allowance; 7z coverage
probes use the same process and diagnostic budgets. Failed work stays charged.
Default intake keeps its existing behavior. Refusals identify the exact first
unproven container or leaf chain. The CLI/TUI preview and JSON `archive_leaves`
show each held selection, byte count, SHA-256 and holding ID, bound into the plan
and canonical cleanup manifest with `archive-tree/1`. All outer parts must share
one exact parent. See [decision 0063](decisions/0063-complete-archive-tree-cleanup.md).
All cleanup root snapshots must match the plan's original sizes and SHA-256 values
while copying. Covered ordinary ZIPs are also snapshotted, keeping their outer
check Go-only. Initial hashes, copy-hash reads and final rehashes share the 64 GiB
source-hash allowance, including failed reads. Extraction never uses different
bytes that merely changed back before the final rehash. Default intake does not
add these snapshots or hash work.

Direct labels remain `OUTER!/MEMBER`, even when a literal member name contains
`!/`. A nested selection is `OUTER!//["inner.zip","book.epub"]`, using a canonical
JSON array of exact member names. The leading slash distinguishes it from any
previously eligible direct member. The existing 4,096-byte record limit applies
before publication. Recovery re-extracts that exact selection, resumes it first,
and stops on unavailable or changed bytes; historical labels stay unchanged.
Cancellation and early errors report incomplete results with counted outcomes.
See [decision 0058](decisions/0058-bounded-nested-intake.md).
The folder and its archives are never changed. See
[decision 0017](decisions/0017-folder-and-zip-ingest.md).

Stored files live under `assets/sha256/` beside `.nemalo`. Operations and
holdings journals are separate from the initialization journal. A trailing
import intent resumes only for the same source. Corrupt journals are retained.
`library list` on the library directory lists current holdings in import order,
with a literal `--query` over titles, languages, identifiers, paths, and source
IDs, and `--format`, `--status checked|review`, and `--language TAG` filters;
the language filter matches the declared base language, so `en`, `en-US`, and
`eng` are one, and counts holdings with no readable tag
([decision 0037](decisions/0037-base-language-filter.md)). It reads journals only.
`library open` hands the default application, or `--app`, a verified read-only
copy in `<state>/handoff`, never the stored file, because some readers write
annotations into the files they open. `library export` makes the same verified
copy in an explicit directory and never overwrites; see
[decision 0016](decisions/0016-reader-handoff-copies.md).
`library cleanup` moves a folder's sources whose bytes the library holds into
the library's holding area, and `library restore` returns them
([decision 0022](decisions/0022-recoverable-source-cleanup.md)). `library purge`
then removes held copies of a completed cleanup whose bytes are a current
holding with verified stored bytes, after its own preview and plan digest;
receipts, intents, and anything else stay, and restore reports a purged file
as purged ([decision 0041](decisions/0041-purge-held-sources.md)).
An ordinary ZIP must account for every local and central record and descriptor,
with matching names, flags, method, CRC and sizes. Proof reads share 16 MiB,
including the standard reader's end search and offset queries. Comments, unknown
or malformed extra metadata, ZIP64 and unsupported layouts stay for review.
Known fixed timestamp/identity fields remain supported. This stricter subset
does not change intake. Purge rechecks ZIPs, including historical cleanup copies
and pending purge manifests, and reports `archive_not_accounted` when the proof
fails. Those bytes remain available to restore. See
[decision 0060](decisions/0060-complete-zip-cleanup-accounting.md).
For split ZIPs, a fresh purge verifies the entire held set again. Its JSON
preview includes `archive_groups`, with the supported proof and ordered file
indices bound to the plan digest. A schema-2 purge manifest records that proof
before removal: `zip-stream/1` for chunks and `zip-segments/1` for disk segments.
The terminal `.zip` is included in its segment group, never independently cleared
by an ordinary-ZIP claim. Resume rehashes remaining parts and checks current holdings;
changed parts stay and recorded removals never adopt replacements. Unknown proof
versions retain remaining bytes, while malformed bindings stop for review.
An unreferenced manifest cannot authorize a retry. Older executables cannot read
these cleanup entries or purge manifests; use this source version or newer to
recover them. Ordinary-file and ordinary-ZIP legacy manifests retain their existing
contract.
Historical RAR-named archive entries and old continuation names without a supported
proof stay for restore. Cleanup resume stops for review; fresh and pending purge
plans report `archive_not_accounted` and retain every file of the entry. This does
not authorize those unversioned entries. The read-only standalone RAR5 coverage path now
binds physical framing to the installed reader's exact table; child coverage
requires the separate nested opt-in below
([decision 0064](decisions/0064-bounded-rar-framing-accounting.md)).
For nested cleanup, fresh purge rechecks every held container and leaf through the
same tree. Its schema-2 group also includes `leaf_digest`, binding the canonical
leaf evidence to that reviewed purge and durable intent. Single-file trees and
supported outer sets use the same recovery protocol. Resume checks the supported
original-tree proof, each remaining source part and current holdings; it never
reconstructs a partial tree. Unknown evidence retains bytes and malformed bindings
stop before removal. Earlier executables refuse the new canonical leaf fields;
use this source version or newer to restore or finish recovery.
Every new 7z cleanup uses this same `archive-tree/1` contract, including a flat
standalone archive or flat split set with only direct book leaves. Fresh purge
repeats complete accounting and binds the exact file group and leaf digest.
Historical 7z-named archive entries or chunks without a supported proof stop cleanup
resume for review; fresh and pending purge retain all parts as `archive_not_accounted`.
Their original bytes remain restorable without a reader. Stop an interrupted
operation, restore what remains and preview a new cleanup to establish current
coverage. No old manifest is rewritten. See
[decision 0065](decisions/0065-versioned-7z-coverage.md).
Standalone root RAR5 cleanup now supports only held book leaves and empty
directories within the bounded framing subset. It records `rar5-frames/1`, one
original source identity and exact one-component leaves. Every physical file
record must match the maintained reader's same UTF-8 table; output size/CRC,
current held hashes and final source hashes are checked separately. Service,
comment and recovery records, unknown metadata, encryption, directory data,
aliases, gaps and tails stay preserved. Volume sets, child RARs and a RAR
root containing nested archives also stay. Fresh purge repeats the full check and
binds the single-file group and leaf digest in its schema-2 intent; partial recovery
uses that supported immutable evidence and never adopts replacements. Earlier
executables retain or refuse the unfamiliar proof. See
[decision 0066](decisions/0066-standalone-rar5-cleanup.md); this is implemented source
behavior, with acceptance evidence tracked in
[issue 57](https://github.com/blisspixel/nemalo/issues/57).
`staging.RAR4Accounted` separately measures a strict standalone subset read-only,
without payload reads or integrity claims. It shares the 16 MiB lifetime proof
reader and refuses subblocks, comments, encryption, volumes, ambiguous names and
unsupported metadata. Its facts alone grant no coverage or cleanup permission;
acceptance evidence is tracked in issue 57. See
[decision 0067](decisions/0067-read-only-rar4-framing.md).
An internal caller can explicitly request standalone RAR4 reader binding with
both `Coverage` and `RAR4Coverage`. The same private snapshot and complete UTF-8
table drive checked exact-name extraction through the common RAR matcher. This is
not a CLI/JSON/MCP option. This root-only capability added no child or set coverage.
The capability requires separate verification and alone grants no application proof;
see [decision 0068](decisions/0068-opt-in-rar4-reader-binding.md).
RAR4 framing now measures known zero high packed/unpacked-size words and exact
extended modification, creation and access time metadata. It accepts only the
documented subsecond subset and refuses missing fields, a fourth time field,
trailing metadata and nonzero high words. This does not add larger-file support,
Unicode names or application cleanup. See
[decision 0069](decisions/0069-bounded-rar4-time-and-size-metadata.md).
Standalone root RAR4 cleanup now uses its distinct `rar4-frames/1` within the
verified ASCII-name, known-attribute/method, zero-high-size and exact time subset.
Standalone RAR4 or RAR5 authority is selected from the checked private outer signature. A child, filename,
unknown signature or successful extraction alone cannot supply that permission.
Every direct leaf must be held with verified current bytes. Fresh purge repeats
the complete check and binds the exact index and leaf digest; recovery validates
supported immutable evidence and current remaining identities/holdings. Known
RAR4/RAR5/tree proof mismatches stop before removal, and unknown proofs retain
bytes. Restore needs no reader and never replaces a target. Unsupported RAR volumes,
Unicode and unsupported metadata stay preserved.
See [decision 0070](decisions/0070-standalone-rar4-cleanup.md); acceptance is tracked
in issue 57 and this does not complete stage 2 or authorize a release.
An explicit internal `NestedRARCoverage` plus `Coverage` now permits the same
strict standalone framing/table evidence on checked private RAR children. RAR4
also requires `RAR4Coverage`. No metadata or volume subset is broadened. Every
child still needs the reader's standalone-container preflight and shares the
whole tree's proof/read/member/output/diagnostic/process/deadline bounds. Failed
work stays charged. Framed notes remain visible unsupported material; failed
output integrity remains separate from physical coverage. The initial read-only
increment left this nested flag false in application cleanup. See
[decision 0071](decisions/0071-opt-in-nested-rar-coverage.md); acceptance remains
tracked in issue 57.
Covered nested trees containing standalone RAR4/RAR5 nodes now use new
`archive-tree/2`, with exact chains, current held bytes and unchanged originals.
Selection uses measured covered RAR nodes, including an empty child beside a
direct held book, rather than leaf names. Direct-only RARs keep their own proof;
ZIP/7z-only trees keep /1, whose RAR exclusions are unchanged. Every known proof's
shape and binding is checked before any purge group removes bytes. Unknown proof
versions retain copies for restore. Fresh purge repeats the full tree check and
binds the exact original indices and canonical leaf digest; partial recovery never
reconstructs missing parts or adopts replacements. Earlier executables refuse or
retain unfamiliar authority; raw restore is tested separately. Unsupported RAR
volume variants, metadata and unheld material remain preserved. Covered nested
sets use the separate /3 contract above.
See [decision 0072](decisions/0072-nested-rar-cleanup.md), tracked in issue 57;
RAR volume cleanup still leaves roadmap stage 2 unfinished.
`library audit` on the library directory reports missing, changed, and review
holdings and does not repair them. Snapshot audit still requires `--root`.
An EPUB package identifier is recorded when it is an explicit ISBN, DOI, or
Open Library work or edition key. Other package identifiers stay unassigned.
The same identifier does not merge two files. PDF and MP3 holdings do not claim
that their bibliographic identifiers were read. See
[decision 0009](decisions/0009-managed-epub-import.md) and
[decision 0010](decisions/0010-bibliographic-identity.md).

### MCP server

```sh
nemalo mcp serve --scope /path/to/library
```

`mcp serve` runs read-only MCP tools on stdio for an agent host: library status,
holdings, audit, file checks without antivirus, and EPUB content retrieval, each
over the same services and JSON data as the CLI. `--allow-network` adds catalog
search and Archive evaluation. `--allow-write` adds `library_import_preview`
and `library_import_apply`; an apply stores only the plan digest its preview
returned and refuses a changed source, assessment, normalized provenance,
proposed holding, or scan choice
([decision 0034](decisions/0034-mcp-import-tools.md)). `library import
--apply --plan DIGEST` gives the CLI the same check. `--allow-write` also adds
`library_rescan_start`, `job_get`, `job_list`, and `job_cancel`, and with
`--allow-network` `acquisition_start`: long work runs as a durable job journaled
in the state folder, which outlives the call and the connection; the server
finishes running jobs before it exits, and a job whose process ended is
recorded as interrupted ([decision 0045](decisions/0045-durable-mcp-jobs.md)).
`nemalo jobs list`, `jobs get ID`, and `jobs cancel ID` read and cancel the
same jobs from a terminal. Opening their store can reconcile abandoned owners
and prune old history. They are not passive polling APIs.

`nemalo jobs snapshot LIBRARY [--limit N] [--json]` instead uses the shared
non-mutating application service. It observes an explicitly selected existing
library and existing configured state/jobs directory without creating either,
acquiring job locks, reconciling owners, pruning, repairing tails or opening
stored content and historical receipt/source paths. The result is
`nemalo-job-snapshot/1`, with observation times, recorded status/progress,
relationship evidence, work counters, coarse problems and explicit coverage.
Raw requests/results/errors, private paths and unrelated job details are omitted.
Unfinished ownership is `not_observed`, including abandoned recorded running jobs.
An empty observation does not prove that no unbound legacy work exists.

New rescan jobs record an immutable `nemalo-job-library/1` declaration.
`acquisition_start` optionally accepts an initialized scoped `library` to record
the intended context; it grants no import or library write authority. Omission
leaves intake behavior unchanged. Legacy completed rescan results can bind an
exact library ID; legacy acquisitions need exact current recorded packet
provenance, source selection and bytes/hash. Recording positions and formats
remain distinct, and held tracks do not establish a complete held recording.
An unknown context never falls back to a legacy rule. A path/title alone cannot
bind a job. During an active library writer only the immutable initialization
identity is observed; mutable holdings and their acquisition relationships are
unavailable until a shared read lock is available.

The passive operation examines at most 4,096 directory entries, 6 MiB per job
journal, 32 MiB of actual journal reads including failed work, and 10,004 records
per journal. Return limit is 20 by default and 1-100 explicitly; ordering is
newest recorded creation time with an ID tie-break within observed records.
At exactly the directory ceiling, completeness remains unestablished without
an extra uncounted EOF probe, so that observation is conservatively incomplete.
Safely encoded data is at most 1 MiB. A 15-second service deadline includes
library validation; shorter caller deadlines win. Filesystem calls may still
stall. Incomplete observations return their bounded valid observations and exit
nonzero; cancellation, library identity changes and capability/close failures
invalidate scoped output. Complete lines are validated strictly; a torn trailing
line remains untouched and supplies no committed fact. This prerequisite does
not implement a panel or complete issue 59; see
[decision 0088](decisions/0088-non-mutating-library-job-snapshot.md).
The TUI's explicit F10 action in State uses this same service and configured
jobs directory, with selectable recorded summaries and coverage details.
It refreshes only on F10 and preserves the existing cancellation/generation
checks. No TUI job writer, polling timer or cancellation command is added.

Tool paths must resolve inside a `--scope`
directory or the configured library and review folders. Stdout carries protocol
messages only. See [agent integration](AGENT-INTEGRATION.md) and
[decision 0014](decisions/0014-read-only-mcp-server.md).

### Rescan review holdings

```sh
nemalo library rescan /path/to/library
nemalo library rescan /path/to/library --apply
```

Rescan reruns the limited checks and scans each holding in review only
because scan evidence is missing or unusable. It writes nothing without
`--apply`. ClamAV receives up to 32 files per invocation, so signatures load once
per batch rather than once per file. Holdings of every held format are rescanned;
earlier detections and other findings are listed with a reason and not scanned. Stored bytes are
never changed; bytes that no longer match their holding receive no status.
Applied changes are journaled as `rescan_intent`, holding line, and
`rescan_completed`; the next writer completes an interrupted rescan first. See
[decision 0012](decisions/0012-scan-on-acquire-quarantine-and-rescan.md).

## Configuration

Configuration is an optional strict JSON object with `library` and `review` paths
and `on_detection` (`quarantine`, the default, or `delete`) for downloads the
scanner flags. Both paths must be absolute and must not overlap. Unknown fields, multiple JSON
values, nonregular files, and files over 64 KiB are rejected. These are reserved
settings for upcoming content operations; current init/status require their own
explicit directory and do not adopt these settings implicitly.

Precedence: file, then `NEMALO_LIBRARY`/`NEMALO_REVIEW`, then command flags.
`--config FILE` overrides `NEMALO_CONFIG`, which overrides the default config path.
An explicitly selected missing file is an error. Flags can precede/follow positional
arguments; `--` ends option parsing. Flags for another command are rejected.

| Platform | Configuration | State | Cache |
| --- | --- | --- | --- |
| Linux | `$XDG_CONFIG_HOME/nemalo/config.json` | `$XDG_STATE_HOME/nemalo/state` | `$XDG_CACHE_HOME/nemalo` |
| macOS | `~/Library/Application Support/nemalo/config.json` | `~/Library/Application Support/nemalo/state` | `~/Library/Caches/nemalo` |
| Windows | `%APPDATA%/nemalo/config.json` | `%LOCALAPPDATA%/nemalo/state` | `%LOCALAPPDATA%/nemalo` |

Linux defaults are `~/.config`, `~/.local/state`, and `~/.cache` when the respective
XDG variables are absent. Relative environment paths are rejected. Reading config
and running `doctor` do not create these directories. `doctor` also reports whether
the directory descriptor bridge is present. That is `/proc/self/fd` on Linux and
`/dev/fd` on macOS. It does not execute a scanner or create a hard link.
Like every other command, `doctor` prints a short text summary: paths, any
configured library, review folder, and catalog, the recommended scanner and its
signature age, the descriptor bridge, optional tools, and a `Next:` line for
each thing that needs action. `doctor --json` is the full report. Before
0.1.0-alpha.12, `doctor` printed that JSON object by default.

## Omarchy

Reviewed 2026-10-09 against Omarchy 4.0.4, an Arch Linux x86_64 desktop. The
installer uses Arch packages, systemd, and btrfs or ext4. Hyprland is the
compositor. Omarchy 4 defaults to the `foot` terminal; Ghostty, Alacritty, and
Kitty remain selectable. Omarchy 3 defaulted to Alacritty.

Run the published `linux/amd64` archive. That build has cgo disabled, so it does
not need Ubuntu's C library. Hosted CI runs it on Ubuntu, not on Omarchy or Arch.
Building this tree from source requires Go 1.27.3.

Library directories use `/proc/self/fd`, which Arch provides. Import hard-links
the stored file inside the library directory. ext4 and btrfs both support that
link when the library stays on one filesystem. A source on another filesystem is
copied, then linked only inside the library. `nemalo doctor` reports
`descriptor_bridge` for the `/proc/self/fd` check.

`foot`, Ghostty, Alacritty, and Kitty are recognized as truecolor. `NO_COLOR`
and `TERM=dumb` still disable color. A tmux session stays 256-color. Omarchy
4.0.4 binds Super shortcuts and uses F9 for dictation. It does not bind
Alt+1 through Alt+0, F2 through F8, or Ctrl+C. Ctrl+C still quits Nemalo.
Super+C is the desktop copy key. Nemalo does not read the Wayland clipboard.
ClamAV is the recommended scanner and is not installed by default. `doctor`
reports whether `clamscan` is available and how old its signatures are, and the
first unscanned result prints `sudo pacman -S clamav && sudo freshclam`. Enable
`clamav-freshclam.service` to keep signatures current. A missing scanner leaves
the file unscanned. After installing ClamAV, `library rescan LIBRARY --apply`
scans the review holdings in one command.

## Verification

```sh
go run ./cmd/verify
go test -race ./...
```

`verify` runs formatting, build, vet, `go tool staticcheck`, offline tests with all
first-party packages instrumented, an enforced 80% aggregate statement threshold,
and `go tool govulncheck`. Exact tool modules/checksums are in `go.mod`/`go.sum`.
Vulnerability checking accesses the Go vulnerability database. Normal unit tests
use local synthetic fixtures/HTTP servers and do not access public providers.
Synchronous synthetic archiver children set only `atexit_sleep_ms=0` in their
inherited `GORACE` options, avoiding a one-second sleep per invocation. They remain
race-instrumented with failure reporting; the running parent keeps its startup
settings. See [Go's race options](https://go.dev/doc/articles/race_detector#Options).

Library recovery tests build the shared synthetic reader as a small Go executable
once in the parent suite. Isolated recovery children reuse that parent-owned
file after checking its hash, handler identity, toolchain and instrumentation.
The lazy offline build matches the parent's race and coverage instrumentation;
a test verifies the executable's build information and rejects a different hash.
This avoids repeatedly starting the full library test executable as the reader
and rebuilding the small reader in every isolated group.
Real child processes, production durability operations, every recovery case and
the default test timeout remain in place. Other packages may continue to use
their test executable for the same handler.

After required counters have been merged and published, caller-owned coverage
scratch is walked without following links, its files removed with `os.Remove`,
then its directories removed in reverse order. Cleanup errors fail verification.
This test-only housekeeping avoids recursive Windows handle churn without
changing application cleanup, fixture bodies, instrumentation or durability calls.
See [decision 0081](decisions/0081-coverage-counter-scratch-cleanup.md).

Full library suites run independent RAR tree and RAR4/RAR5 set recovery groups,
archive-tree and split-ZIP cleanup matrices, and RAR4/RAR5 part-count matrices in
at most three isolated copies of that same compiled test executable. The fixture
groups retain their original bodies and test names. Registered groups must match
the original selections exactly; unknown, missing and repeated results fail.
Default single-run suites start the isolated work before ordinary
fixtures, with a frozen environment, working directory, scratch root, tool path
and parent-owned reader. Two groups can overlap serial fixtures, reserving the
third lane for those fixtures and their control helpers. When Go releases the
parallel recovery roots after its serial phase, all three lanes become available.
The roots collect each exact result once; missing, unknown or repeated selections
fail. Shutdown cancels and joins workers before removing their private data.
The overall package deadline starts before scheduled work, with the same timeout
as Go's package alarm. Each child retains its four-minute timeout and five-minute
execution context. Filtered, repeated, CPU-matrix, listed, benchmark and fuzz
invocations, short runs and parallel limits below three retain ordinary test
selection instead of reusing scheduled results.
Fixture globals stay inside their isolated children. The shared three-lane bound
overlaps filesystem waits without changing any case or durability operation.
See [Go's rules for parallel tests](https://go.dev/blog/subtests).
Each covered group writes its test and reader counters to separate private
directories, avoiding concurrent metadata replacement on Windows. After the
group succeeds, the running toolchain's offline `go tool covdata merge` combines
each executable's counters in that group's private outputs. Only after serial
fixtures finish do the roots publish those counters into the parent's directories.
Serialized exclusive publication retains every
merged counter; identical metadata is reused without replacement, and conflicts
or missing counters fail the test. Stock merges use PID zero and wall-clock
timestamps, which can give separate groups the same filename on Windows. Before
publication, the parent assigns increasing unique timestamps to those filenames,
retaining their metadata hashes, PID and exact counter bytes. A constant-clock
regression preserves every concurrent group's distinct payload; a separate check
uses actual covered reader invocations and the stock Go report to prove both
invocations contribute. Existing overwrite/conflict checks remain in place.
Go test selects its own executable's metadata
when reporting, so the parent's TestMain also uses stock `covdata textfmt` to
append the reader's records to Go's completed text profile. Original records,
including uncovered code, remain intact. Raw counters from both executables
remain available for full-suite coverage comparisons. See
[Go's integration coverage tools](https://go.dev/doc/build-cover).
Race instrumentation, every case/assertion and real
durability operations stay enabled. Children have a four-minute timeout inside
the parent's unchanged ten-minute package timeout. An unknown/unexecuted group
or a failed child fails the parent. Explicit `-run` filters keep their original
serial subtest selection.

Race instrumentation needs cgo and a C compiler. Cgo-free release builds do not.
On Windows with MSYS2 installed, put its compiler directory on PATH for race tests
and set `CGO_ENABLED=1`. CI runs native Linux, macOS, and Windows checks, followed
by a `CGO_ENABLED=0` binary smoke. Review actual runs before claiming platform support.

Windows CI also runs `TestWindowsReparse` verbosely in `internal/safeio`,
`internal/app`, and `internal/library`. These synthetic fixtures require real
directory junctions and file/directory symbolic links, verify their native tags,
and fail if creation is unavailable. They test root and opened-identity boundaries,
inventory with hashes/PAR2 metadata, and source, store and recovery paths through
ingest/cleanup/restore/purge. They invoke no shell, scanner or network. A cross-build
does not satisfy this gate; see [decision 0008](decisions/0008-filesystem-and-display-boundaries.md).

## Next bounded work

Import, ingest, scanned acquisition, recording manifests, cleanup, restore,
and purge are in place. Interrupted downloads resume against a strong validator
(decision 0023). Gutenberg catalog search stays local (decision 0028). One
selected Gutenberg EPUB can be acquired from gutenberg.pglaf.org
(decision 0029). An Open Library work lists editions, and one chosen edition
can name an Archive item through its ocaid before the existing Archive
acquire runs (decision 0030). EPUB, PDF, MP3, M4B, and M4A holdings can be
checked (decisions 0009, 0032, 0033, and 0039). LibriVox readers are credited
per track when the project links back
([decision 0035](decisions/0035-librivox-readers.md)). MCP is a scoped
adapter over the same services; `--allow-write` adds previewed imports
([decision 0034](decisions/0034-mcp-import-tools.md)) and durable acquisition
and rescan jobs ([decision 0045](decisions/0045-durable-mcp-jobs.md)). Website search, other
Gutenberg formats, files above 2 GiB, OCR and column reconstruction stay later.
Explicit narration declarations beyond LibriVox are implemented in stage 1.
Explicit decoded MP4 access passed stage 3 acceptance at `5091ce7`, with its
[receipt](https://github.com/blisspixel/nemalo/issues/60#issuecomment-6374231710)
read back before issue 60 closed. The remaining 1.0 work is
ordered by dependency in the [roadmap](../ROADMAP.md#order-of-work-to-10), starting
with cited evidence before interfaces and release gates.
Cited evidence for agent-led reading is milestone 9
([decision 0048](decisions/0048-cited-evidence-before-1-0.md)); generated analysis
inside Nemalo and model/harness integrations remain post-1.0. Issues 1 and 2 are
closed; remaining OCR and column reconstruction are milestone 7 work, while the
separate stage 3 receipt governs acceptance of explicit decoded audio.
