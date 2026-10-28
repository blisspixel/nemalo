# 0107: DOI resolution through OpenAlex work lookup

Status: accepted and implemented, 2026-10-28
([issue 74](https://github.com/blisspixel/nemalo/issues/74)). Milestone 7, after
OpenAlex work search ([decision 0106](0106-openalex-work-search.md)). Discovery
only; not a 1.0 gate.

## Context

Milestone 7 planned Unpaywall for DOI-to-open-copy resolution. Unpaywall's
`/v2/:doi` requires a contact email, which Nemalo would have to collect and
send with every request. Current OpenAlex documentation, read on 2026-10-28:

- [Unpaywall](https://help.openalex.org/access/unpaywall/): Unpaywall and
  OpenAlex "now run on the same database"; Unpaywall is "a legacy-compatible
  format over it". Its `is_oa`, `oa_status` and PDF link "are the same values
  you'll find in the OpenAlex work's" `open_access` and `best_oa_location`.
  Unpaywall is not being retired.
- [Single entities](https://help.openalex.org/how-to-use-the-api/get-single-entities):
  a work is read as `/works/W...` or `/works/doi:10...`. Single-entity lookups
  are free for every entity type. A merged ID redirects to the surviving record
  with a normal 200, detected by comparing the returned `id`; a deleted ID is
  404.
- [Work object](https://help.openalex.org/api-entities/works/work-object):
  `open_access` has `is_oa` and `oa_status` (`diamond`, `gold`, `green`,
  `hybrid`, `bronze` or `closed`). `locations` holds every unique place the
  work lives, up to the first 100; `locations_count` counts all of them.
- [Locations](https://help.openalex.org/data/locations/): each has `is_oa`,
  `landing_page_url`, `pdf_url` (or null), `source` (or null), `license` (a
  short code such as `cc-by`, or null) and `version` (`publishedVersion`,
  `acceptedVersion`, `submittedVersion` or null). Locations of one work can
  carry different licenses.

Observed live the same day: a work's `ids.pmcid` and PMC location URLs are
written as `https://www.ncbi.nlm.nih.gov/pmc/articles/5815332`, without the
`PMC` prefix, which the 0106 search parser did not accept. arXiv locations
appear as `http://arxiv.org/abs/ID`, `https://arxiv.org/pdf/IDvN` and
`https://doi.org/10.48550/arxiv.ID`. The arXiv DOI `10.48550/arXiv.1706.03762`
resolved to a work stating a different DOI.

## Decision

- `evaluate openalex:WORK` and `evaluate doi:DOI` read one work from
  `api.openalex.org` through the search adapter's metadata client: fixed host,
  checked public destinations, no proxies, bounded JSON, the existing quota
  handling, no key and no email. The TUI Evaluate step (`e` on an OpenAlex
  result) and the MCP `catalog_evaluate` tool (with `--allow-network`) offer
  the same evaluation.
- A DOI is accepted only within the character set Crossref recommends matching
  (`10.` four to nine digits, `/`, then letters, digits and `-._;()/:`), with
  no `.` or `..` path segment. Rarer DOIs are refused, not escaped.
- The evaluation is `discovery_only` and lists no files. It records the title,
  authors, language, work type, year, the OpenAlex ID, the stated DOI and
  PMCID, the work's open access status, and each stated location's openness,
  source name and type, landing page, PDF URL, license and version, as
  OpenAlex's claims.
- Location URLs are untrusted and never opened. Only absolute `http` or
  `https` URLs of at most 2048 bytes without credentials or controls are kept;
  a dropped URL is counted in a limitation.
- A location whose URL names a PMC article or an arXiv paper (including the
  arXiv DOI form) states that explicit `pmc:` or `arxiv:` identifier. That is
  the only path from an OpenAlex work to a file: the person evaluates that
  identifier, under its own decision ([0047](0047-arxiv-selected-versions.md),
  [0100](0100-pmc-selected-article-versions.md)), and acquisition follows
  from that evaluation. Any other location stays discovery only.
- The record is reported incomplete when OpenAlex declares more locations than
  it lists. A returned work other than the requested `openalex:` ID (a merge)
  and a resolved work whose stated DOI differs from the requested DOI are each
  reported, never silently accepted. A retraction is stated first.
- Nemalo adds no Unpaywall adapter and collects no contact email. Unpaywall
  would serve the same values under the same team's database, at the cost of
  sending the person's address with each lookup.

## Consequences

`discovery.Evaluation` gains an optional `open_access` object, recorded in the
field contract. Acquisition intents and results embed that type but never set
the field, since no OpenAlex evaluation is acquired. The 0106 search parser now
accepts PMCIDs with and without the `PMC` prefix. The MCP evaluation tool now
returns a refusal as an error envelope that matches its output schema, instead
of failing validation.

## Verification

Offline tests cover the request path and parameters, field mapping, PMC and
arXiv identifiers in each observed form, unsafe and credentialed URLs, merge
and DOI-mismatch reports, truncated location lists, malformed records,
refused identifiers before any request, CLI usage, MCP acceptance and
refusals, the human report and its next command, and the TUI flow from a search
result to a location's `pmc:` identifier, with `d` refused. Tests never reach
the network.

On 2026-10-28 a development build evaluated `doi:10.7717/peerj.4375` live: 12
locations, two naming `pmc:PMC5815332`; and `doi:10.48550/arXiv.1706.03762`: 11
locations naming `arxiv:1706.03762` and `arxiv:1706.03762v5`, with the DOI
mismatch reported.
