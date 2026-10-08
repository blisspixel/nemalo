# Real-resource validation collection

The [foundations manifest](../collections/foundations.json) selects 100 resources:
80 EPUB editions, 10 LibriVox recordings, and 10 versioned arXiv papers. It covers
13 languages. The recordings contain 118 exposed tracks, not 10 sample chapters.
Books and their audio versions remain distinct resources, linked by their sources.

The collection supports broad study of literature, perspective, inquiry, ethics,
freedom, society, mathematics, science, and listening. It is an editorial starting
point, not a universal curriculum, a consciousness test, or an endorsement of each
author's claims. English and European traditions remain overrepresented. Historical
prejudice and obsolete science need context; prominence is not evidence of truth.
Each selection records its rationale, language, source identity, and rights caveat.
The [Gutenberg catalog subset](../collections/gutenberg-catalog.csv) records the
selected IDs and catalog types from the official weekly CSV, retrieved 2026-10-08.
Its line endings and dash typography are normalized. An offline test requires all
80 ebook selections to be cataloged `Text`: an audio catalog page can itself have
an EPUB wrapper, so file extension and container validity are insufficient evidence
that the selected resource is a full text edition.

## Acquisition harness

`cmd/collection` is Go validation tooling over `internal/collection`, not the
production acquisition manager. Keep the actual collection outside the Git checkout.
Only the manifest, code, tests, and documentation belong in Git. No public provider
is contacted by unit tests or CI.

For an explicitly chosen external directory, with its report parent already present:

```sh
go run ./cmd/collection --manifest collections/foundations.json \
  --destination /srv/nemalo-validation/intake \
  --report /srv/nemalo-validation/acquisition-001.json \
  --index /srv/nemalo-validation/reading-list-001.md
```

Choose a new report filename for each run. The harness never overwrites reports.
The optional reading list links acquired assets and records selection context;
generating it does not open a reader or promote intake to checked library status.
The run has one transfer at a time, a 5 GiB transfer budget, a 256 MiB per-file cap,
five-minute HTTP deadlines, identified requests, a three-second interval between
asset requests, fixed provider hosts, and checked HTTPS redirects. It uses an official
Gutenberg mirror rather than automating ordinary search pages. Most selected EPUBs
omit optional illustrations to reduce transfer and image processing. Image-dependent
editions need a separate selection. Existing completed assets are rehashed and
reinspected without downloading them again. Changed or unreceipted files are
preserved and rejected, not overwritten.
Archive delivery redirects admit only the observed numbered `ia`/`dn` host patterns
under its US/Canadian domains. Other hosts fail closed pending source verification.

Each asset gets a SHA-256 receipt with the selected and final URLs, measured size,
acquisition time, and separate assessment fields. This hash identifies the acquired
bytes; it is not a provider signature or proof that the source was uncompromised.
The selected source metadata is preserved beside the local collection when running
the real validation exercise. It is not a second production catalog.

## What the checks establish

- EPUB: ZIP signature, bounded member names/count/expanded size, CRC and lengths,
  required mimetype placement, container/package XML, manifest member existence,
  and spine references. Remote manifest resources are flagged and never fetched.
- PDF: header and end marker only. No complete PDF parser or active-content audit.
- MP3: candidate signature only. No complete frame decoding, playback, or audio
  quality review. All tracks exposed by each selected recording record are required.
- All types: measured SHA-256, transfer accounting, and recheck of existing bytes.

EPUB limits are 10,000 members, 32 MiB per expanded member, 128 MiB total expansion,
1 MiB container XML, and 2 MiB package XML. CRC passes do not imply malware safety.
EPUB conformance, content quality, antivirus, paper peer review, and semantic/audio
completeness remain separate evidence. The acquisition report says `not_scanned`.
Any later external antivirus run gets its own dated report; it does not rewrite
historical acquisition receipts or guarantee safety. Scanner privacy follows its
own configuration and must be disclosed separately from Nemalo's network behavior.

One LibriVox record declares 13 sections but exposes 14 tracks. The manifest retains
all 14 and the discrepancy. Papers are repository versions; metadata does not prove
peer review. Eight current OAI records expose license URIs, retained as evidence;
two older records expose no license. A current-record declaration is not proof of
rights for every pinned version. Local reading is not permission to redistribute.
Gutenberg and LibriVox declarations use US copyright boundaries,
not a global public-domain assertion. Embedded notices and attribution remain intact.

## Recovery and production boundary

The harness uses root-scoped files and an exclusive writer lock. Per-file writes
are synced before rename, followed by a synced receipt. A process or power failure
between these steps can leave an unreceipted asset or partial file. A stale lock
requires inspection after confirming the writer is gone. Preserve ambiguous files
for review; do not remove them or manufacture a receipt to make a retry pass.
The immutable report is finalized on normal completion, including failed assets;
a forced termination may leave an empty report. Receipts remain the per-asset facts.

No HTTP range resume, durable production journal, automatic retry, metadata refresh,
archive extraction, checked publication, reader handoff, or source deletion is
implemented here. These remain roadmap work through the shared application core
and CLI/TUI. Synthetic tests exercise failure boundaries; real provider runs prove
only the particular bytes and checks recorded at that time. Public CI success
does not validate live provider availability or certify this collection.

## Recorded exercise: 2026-10-08

The Windows/amd64 run acquired all 100 selected resources: 80 EPUBs, 118 MP3
tracks across 10 recordings, and 10 PDFs. The 208 content assets total
1,046,965,564 bytes. All passed the limited checks above; completed assets were
rehashed on retry. Three Archive HTTP 500 failures succeeded in a subsequent run
without redownloading completed files. No partial files or writer lock remained.

An external Windows Defender custom scan of the intake completed with exit code 0
and output reporting no threats. Remediation was disabled, and existing machine
cloud/sample-consent settings were unchanged. This is scanner evidence for that
local run, not evidence that Nemalo performed that initial scan or a safety certification.
The later shared `check FILE --scan` adapter was separately smoke-tested against
one EPUB with Defender and reported `no_detections_reported`. Historical
acquisition receipts remain unchanged.
EPUB conformance, full PDF validation, MP3 decoding, and editorial review remain
unperformed. No downloaded content, personal paths, or local scanner logs are in Git.

The local reading list, selected source snapshots, asset receipts, failed-attempt
reports, successful acquisition report, and scanner report live with the external
collection. Gutenberg files can be regenerated; a URL-based manifest selects
resources but cannot reproduce historical bytes without preserved assets/receipts.
Manifest SHA-256 for this exercise:
`623c8101bacb698cb6864e336852244a7a2c562ab9b7eddeb649a316e2ea01cc`.

A subsequent CLI file-health pass checked all 208 content assets using the shared
assessment service. Every result was `limited_checks_passed`, with no active-content
indicators reported by these limited checks. The 80 EPUBs contained measured HTML
body text, from 22,162 to 2,657,494 non-whitespace characters per EPUB, totaling
40,824,129. These are measurements, not expected-length or completeness verdicts.
EPUB pages, PDF page trees, media decoding, and semantic completeness remain unknown.
The health report is local to the collection and does not overwrite scan evidence.

The shared library snapshot command subsequently cataloged the external intake:
416 unique byte assets/locations, comprising 208 content files and 208 acquisition
receipts. Optional assessment recorded health for all 208 content assets, including
titles/languages for all 80 EPUBs. Snapshot source reads totaled 2,094,080,564 bytes
across inventory hashing and assessment copies. A fresh explicit-root preservation
audit found all 416 file locations unchanged with no findings. A literal title
query retrieved the Italian edition of Dante's *Divina Commedia*. Catalog/audit
reports remain outside Git. No new antivirus scan, reader handoff, content mutation,
or checked publication was performed by these operations.

## Repeat validation and discovery expansion: 2026-10-08

The original 100-resource selection was rechecked against its receipts with zero
transferred bytes. The previous 416-location catalog audit was complete and found
no changes before expansion. No original asset or receipt was replaced.

The [discovery validation selection](../collections/discovery-validation.json)
then added four distinct resources: *Little Women*, the Danish novel *Tine*,
*The Happy Prince and Other Tales, Version 2*, and the versioned preprint
*Coded MapReduce*. Its eight assets comprise two EPUBs, five MP3 tracks, and one
PDF, totaling 51,625,325 bytes. The external collection now contains 104 resources:
82 EPUBs, 11 recordings with 123 tracks, and 11 papers, across 14 declared languages.
The 216 content assets total 1,098,590,889 bytes. Downloads remain outside Git.

The exercise used the following distinct source boundaries:

| Source | Exercised behavior | Product boundary |
| --- | --- | --- |
| Open Library | Native CLI search for Alcott and book metadata | Discovery only |
| Internet Archive | Native search, item evaluation, distinct-page checks, offered audio files | Acquisition uses the separate Go harness |
| Project Gutenberg | Official offline CSV selection, per-book RDF, mirror EPUB acquisition | Native provider search remains planned |
| arXiv | Official API title search and pinned-version PDF acquisition | Native provider search remains planned |
| LibriVox | Three title-search probes returned not-found errors | Not a successful search integration; new audio selected from Archive's exposed LibriVox item |

Real pagination testing found and fixed an Archive adapter defect: `start` echoed
the requested offset while repeating the first documents. Document-ID comparison
now proves disjoint aligned pages and correct unaligned offsets using `page`/`rows`.
Offline tests cover adjacent-page consistency, duplicate IDs, and changing totals.
Failed probes and the pre-fix response remain local evidence, not successful checks.

Every content asset was checked through the actual CLI with its receipt's expected
bytes and SHA-256. All 216 reports were `limited_checks_passed`, with no findings.
All five new audio files also matched Archive's declared size and SHA-1. These
source checksums are integrity declarations, not cryptographic authentication.
Explicit Nemalo `--scan` checks of one EPUB, one MP3, and one PDF returned
`no_detections_reported`, bound to their private snapshots. A separate fresh Defender
directory scan completed with exit code 0 and no threats reported, with remediation
disabled and host cloud/sample policy unchanged. Receipts were not rewritten.

An already-installed FFmpeg/ffprobe diagnostic independently probed and decoded
all 123 audio tracks to a null output, sequentially with bounded processes. All
decoded without reported errors; probed durations sum to 36.56 hours. This is
additional local validation evidence, not a Nemalo decoder feature, a dependency,
listening review, or proof that every recording is semantically complete. PDF
parsing and EPUB conformance remain unperformed. No content was executed or
handed to a reader/player.

A fresh assessed catalog and audit accounted for 432 unchanged file locations:
216 content files and 216 receipts. The expanded collection exceeds the default
1 GiB inventory budget, so the explicit 5 GiB option was exercised. Local holdings
retrieved *Tine* with language `da`; exact format filtering returned 82 EPUB assets,
and MP3 pagination exposed all 123 tracks, including the final 23 after offset 100.
A receipt containing `.epub` in its name is no longer included by the EPUB format
filter. The filename filter remains separate from health evidence.
Negative CLI exercises rejected a wrong expected hash, insufficient hash budget,
and an attempt to overwrite the saved catalog. Auditing the old baseline after
expansion reported exactly 16 added locations and no changed or missing originals.

The Windows native TUI smoke exercised direct mode shortcuts, real catalog queries,
source evaluation, result scrolling, restored inputs/results, and clean quit.
Responsive light/dark/no-color and pagination behavior have synthetic tests.
The browsing refinement additionally exercised the real 82-EPUB holdings view,
live Archive results, native Windows focus/selection/detail/report scrolling, and
quit. Synthetic tests cover odd/even widths and long Japanese text; wrapped reports
retain their original evidence and allow scrolling to its end. No new assets
were acquired or rescanned for this presentation change.
The README and [terminal guide](TUI.md) images are rendered application-view frames from the real catalog and
live source results, not operating-system screenshots. Only public metadata
and relative asset paths appear. Raw reports, scanner details, copied catalogs,
and content remain outside tracked project state.

## Library control-state validation on 2026-10-08

Native Windows CLI and TUI exercises initialized a separate
`G:\Nemalo Library\Validation\managed-state-smoke` directory, outside the intake
collection and Git. Repeated initialization preserved the library ID and two
journal records. Status, the TUI's exact-root write review, cancellation, and
confirmed initialization were exercised. These checks wrote control metadata
only; they did not import, assess, open, or delete library content.

Synthetic tests cover shared readers, exclusive writers, process termination,
resuming a synced initialization intent, corrupt journals, and unsafe control
entries. This is evidence for initialization recovery only. Content publication,
import recovery, cleanup recovery, and power-loss durability remain unvalidated.

## Source-bound EPUB retrieval on 2026-10-08

Native Windows CLI/JSON listed reading-order units for five existing Gutenberg
EPUBs: *西遊記*, *The Upanishads*, *Alice's Adventures in Wonderland*, *Thus Spake
Zarathustra*, and *Introduction to Mathematical Philosophy*. Each exposed supported
text plus one or two explicitly unsupported cover/layout units. The first supported
unit was selected explicitly. Each book returned a 256-byte-bounded excerpt,
byte-identical retry, and adjacent continuation with the same source-bound identity.
Before/after SHA-256 checks confirmed unchanged originals. Reports and excerpt text
remain outside Git in the validation report directory.

The synthetic suite executes 1,365 actual retrieval calls across three fixture works
with retries, interleaved work selection, serialized/restored references, Unicode,
stanza breaks, and exact source-order assertions. Separate cases cover unsupported
gaps without silent skipping, malformed content, changed/missing sources, stale
references, budgets, and cancellation. A short local extraction fuzz run completed
12,650 inputs without a reported failure. This is bounded evidence, not exhaustive
fuzzing, conformance, safe rendering, or proof that content was read. Paper/audio
retrieval, caller-owned acknowledgement/crash handling, TUI reading, MCP, and the
actual external Kilo tool path remain unimplemented and unvalidated here.

## Source contracts reviewed on 2026-10-08

- Gutenberg [robot policy](https://www.gutenberg.org/policy/robot_access.html),
  [catalogs](https://www.gutenberg.org/ebooks/offline_catalogs.html), and
  [official mirrors](https://www.gutenberg.org/dirs/MIRRORS.ALL).
- LibriVox [API fields and limits](https://librivox.org/api/info) and
  [US public-domain declaration](https://librivox.org/pages/public-domain/).
- arXiv [programmatic retrieval](https://info.arxiv.org/help/bulk_data.html),
  [API terms](https://info.arxiv.org/help/api/tou.html), and
  [licenses](https://info.arxiv.org/help/license/index.html).
