# 0106: OpenAlex work search

Status: accepted and implemented, 2026-10-28
([issue 73](https://github.com/blisspixel/nemalo/issues/73)). Milestone 7, after arXiv
([decision 0047](0047-arxiv-selected-versions.md)) and PMC
([0100](0100-pmc-selected-article-versions.md)). Discovery only; not a 1.0 gate.

## Context

Milestone 7 orders OpenAlex discovery before Unpaywall DOI resolution. OpenAlex
indexes scholarly works and their identifiers. Its current documentation, read
on 2026-10-28:

- [Authentication](https://help.openalex.org/api/authentication/): basic
  queries work without a key; a free key gives ten times the keyless budget.
  Keyless callers may send 10 requests a second. A key goes in `api_key` or a
  bearer header.
- [Example costs](https://help.openalex.org/access/example-costs/): without a
  key the budget is $0.10 a day. A keyword search (`/works?search=`) costs $1
  per 1,000 calls, so about 100 keyless searches a day; a list or filter costs
  $0.10 per 1,000 and a single entity is free. A query costing more than the
  remaining balance is refused before it runs, free of charge. The budget
  resets at midnight UTC. A keyless response's `meta.notice` adds that the
  keyless budget is shared by every keyless caller on the same IP address.
- [Paging](https://help.openalex.org/how-to-use-the-api/get-lists-of-entities/paging):
  `per_page` is 1 to 100 (25 by default), and basic paging reaches only the
  first 10,000 results (`page` times `per_page`). Cursor paging needs a key
  after the first page.
- [Work object](https://help.openalex.org/api-entities/works/work-object): `id`
  is `https://openalex.org/W...`; `display_name`, `doi`, `publication_year`,
  `language` (ISO 639-1, detected from metadata), `type`, `authorships`,
  `ids` (`openalex`, `doi`, `mag`, `pmid`, `pmcid`) and `is_retracted` (from
  Retraction Watch). arXiv is not an `ids` key.

## Decision

- `search --source openalex QUERY` searches works in OpenAlex relevance order,
  through the shared metadata client fixed to `api.openalex.org`, with checked
  public destinations, no proxies, bounded JSON and the existing quota handling.
  The TUI Search step and the read-only MCP `search` tool (with
  `--allow-network`) offer the same source.
- No key is sent or stored. Every search is keyless, so it is metered against
  OpenAlex's keyless daily budget; help and the source guide say so. A
  configured key is later work with its own secret-handling decision.
- One request per search. `--limit` is 1 to 50 and `--offset` a multiple of the
  limit, mapped to `page`; `page` times `per_page` may not pass 10,000. Cursor
  paging is not used. `select` asks only for the fields below.
- Each result is `openalex:W` plus digits, with the title, author names in
  listed order, the declared language, and `https://openalex.org/W...` as its
  landing page, access `discovery_only`. A DOI, PMCID, publication year, work
  type and a retraction flag are shown when OpenAlex states them, as stated
  claims. A PMCID is the explicit `pmc:` identifier a user may then evaluate.
  A retraction flag is never dropped.
- `--source all` stays the book catalogs (Open Library, Archive, Gutenberg),
  so it spends no OpenAlex budget.
- Nothing is evaluated, downloaded or merged from OpenAlex. An OpenAlex work
  is not a file selection, a rights statement, or evidence that two records
  are one edition.

## Consequences

`discovery.Result` gains optional `doi`, `pmcid`, `year`, `work_type` and
`retracted` fields, recorded in the field contract. Other sources leave them
empty. A malformed identifier, a missing title, a result count above the
limit, or a page past 10,000 results fails the search instead of returning a
partial page. DOI resolution followed as an OpenAlex single-work lookup
([decision 0107](0107-openalex-doi-resolution.md)), which also found that
OpenAlex writes most PMCIDs without the `PMC` prefix; the parser accepts both.

## Verification

Tests cover the request URL and parameters, the fixed host, paging limits, the
offset rule, field mapping including absent and null values, retraction and
PMCID claims, malformed identifiers and titles, an oversized result list, and
the CLI, MCP and TUI source lists. Tests never reach the network.

On 2026-10-28 two live keyless searches returned works with titles, authors,
languages, DOIs, years and work types mapped as above. The same day
`filter=has_pmcid:true` counted zero works and sampled records carried only
`openalex`, `doi` and `pmid` in `ids`, so the PMCID path is tested offline
against the documented form and could not be observed live.
