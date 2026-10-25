# 0100: one explicit PubMed Central article version

Status: implemented, 2026-10-25; acceptance tracked in
[issue 64](https://github.com/blisspixel/nemalo/issues/64). Milestone 7, after the arXiv
provider ([decision 0047](0047-arxiv-selected-versions.md)), reusing selected
acquisition ([0007](0007-selected-acquisition.md)), PDF assessment
([0032](0032-pdf-checked-policy.md)) and PDF text ([0021](0021-pdf-pages-and-text.md)).
Not a 1.0 gate.

## Context

PMC changed its automated distribution in August 2026
([NCBI announcement](https://ncbiinsights.ncbi.nlm.nih.gov/2026/02/12/pmc-article-dataset-distribution-services/)).
The OA Web Service is no longer available and the legacy FTP and cloud article
packages were removed. Only the PMC Cloud Service, OAI-PMH, E-utilities and the
BioC API may be used for automated retrieval
([developers](https://pmc.ncbi.nlm.nih.gov/tools/developers/),
[OA subset](https://pmc.ncbi.nlm.nih.gov/tools/openftlist/)); "systematic
retrieval (or bulk retrieval) of articles through any other automated process
is prohibited".

The Cloud Service is the world-readable S3 bucket `pmc-oa-opendata`, readable
over HTTPS at `pmc-oa-opendata.s3.amazonaws.com` without an account
([PMC on AWS](https://pmc.ncbi.nlm.nih.gov/tools/pmcaws/)). It is organized by
article version, `PMCnnnnnnn.N/`, and "version 1" may not exist. Each version
has `metadata/PMCnnnnnnn.N.json` with `pmcid`, `version`, `pmid`, `doi`,
`title`, `citation`, `is_pmc_openaccess`, `is_manuscript`,
`is_historical_ocr`, `is_retracted`, `license_code`, and `s3://` object URLs
such as `pdf_url` carrying an `md5`. A PDF is present only when the publisher's
license permits. The [dataset README](https://pmc-oa-opendata.s3.amazonaws.com/README.txt)
asks users to acknowledge NLM as the source
and to redistribute only data licensed for redistribution. PMC states no rate
limit for the bucket.

Europe PMC remains a separate future metadata/search candidate. This increment
uses the documented PMC Cloud Service only and has no alternate-host fallback.

## Decision

- `evaluate pmc:PMCnnnnnnn` or `pmc:PMCnnnnnnn.N` lists the article's version
  folders, reads the selected version's metadata JSON and lists that version's
  exact PDF key. An unversioned identifier resolves to the highest version present,
  shown as "version N; versions present: ...", which is not a claim that PMC
  renders that version by default. A missing version, an unknown article, or
  metadata naming another article or version is an error.
- Rights come from that exact version's `license_code`, recorded as PMC's
  declaration. `is_retracted` is shown as a restriction-level finding without
  blocking access, since retraction is evidence to display, not a permission.
  Author-manuscript and historical-OCR flags are recorded.
- One file is offered, `PMCnnnnnnn.N.pdf`, only when `pdf_url` names exactly
  that object in this bucket. Its size is the listed object size and its
  checksum the metadata MD5, which the transfer verifies. Acquisition is
  offered only when the version is in the open-access subset and its license
  code is a Creative Commons license; `TDM` and anything else stay evaluated
  but not acquirable.
- `acquire pmc:PMCnnnnnnn.N --file PMCnnnnnnn.N.pdf` needs the explicit version.
  It runs the shared selected acquisition with that size and MD5, then the PDF
  is assessed, scanned and imported like any PDF.
- Each listing/metadata response is bounded to 1 MiB, each listing to 1,000
  entries, and evaluation to 30 seconds. Truncated listings fail rather than
  silently selecting from partial results. The PDF listing uses its exact key
  prefix and requires exactly that one object. Missing flags remain unknown.
- Requests go only to `pmc-oa-opendata.s3.amazonaws.com`, serialized through
  response headers and at most one per second, with the arXiv retry and
  `Retry-After` rules. Redirects are refused, including same-host key changes.
  No search, no MCP tool, no collection selections and no byte resume in this
  increment; an interrupted transfer is retained and fetched again into new output.
- Evaluation and imported rights statements acknowledge NLM and disclaim
  endorsement and current-data guarantees. The dataset permits updates within
  a version, so size/MD5 are fresh declarations and SHA-256 identifies received
  bytes. DOI/PMID, citation and flags remain in the receipt, not normalized held
  bibliographic identities. No persisted/result field is added or retyped.

## Validation required

Offline tests through a stand-in transport for listing and metadata parsing,
version resolution, mismatched metadata, retraction, non-CC and `TDM` licenses,
a missing or foreign `pdf_url`, the pinned download URL, MD5 verification and
pacing. A live evaluation and acquisition of one CC-licensed article, and a
dedicated adversarial review, precede acceptance.

## Observed validation

On 2026-10-25, `PMC10009416.1` declared CC BY and a 471,593-byte PDF with MD5
`648db0ce6ce56688995af0450090db28`. Acquisition verified both and recorded SHA-256
`95646dd6fefc136cb522bdcc6caaf504da0ccb710a3df623a298b9dc83c1b1c4`.
ClamAV reported no detections; PDF assessment walked ten pages labeled 85-94
with no active content findings. Import recorded a checked holding with the
versioned source and NLM attribution. `pdf-text/1` listed ten units and returned
a 2,416-character first-page range. This does not establish semantic completeness,
scientific validity or universal PDF support. Content and the validation library
remain outside the checkout.

Live metadata uses `binary/octet-stream`; that type and `application/json` are
accepted only for pinned JSON objects, with strict parsing. XML listings require
`application/xml`. Review added duplicate/case-variant JSON field refusals and
fixtures, and fixed the TUI resume review for providers without range support.
The shared pacer retains the existing arXiv quota, cancellation, redirect and
delayed-dispatch regression cases. Local and exact hosted gate results and
release publication are recorded separately in issue 64.
