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
peer review. Their item licenses are unresolved, so local reading is not permission
to redistribute. Gutenberg and LibriVox declarations use US copyright boundaries,
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

## Source contracts reviewed on 2026-10-08

- Gutenberg [robot policy](https://www.gutenberg.org/policy/robot_access.html),
  [catalogs](https://www.gutenberg.org/ebooks/offline_catalogs.html), and
  [official mirrors](https://www.gutenberg.org/dirs/MIRRORS.ALL).
- LibriVox [API fields and limits](https://librivox.org/api/info) and
  [US public-domain declaration](https://librivox.org/pages/public-domain/).
- arXiv [programmatic retrieval](https://info.arxiv.org/help/bulk_data.html),
  [API terms](https://info.arxiv.org/help/api/tou.html), and
  [licenses](https://info.arxiv.org/help/license/index.html).
