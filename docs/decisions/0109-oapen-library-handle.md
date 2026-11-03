# 0109: OAPEN Library explicit handle

Status: accepted as a contract record, 2026-11-03
([issue 77](https://github.com/blisspixel/nemalo/issues/77)). Milestone 7.
No adapter is implemented by this decision. Not a 1.0 gate.

## Context

Milestone 7 names DOAB, OAPEN, OpenStax and LibreTexts as one textbook cohort,
after each contract and the real asset format are verified. OAPEN was
rechecked on 2026-11-03.

The [metadata page](https://oapen.org/article/metadata) is current. It offers
daily MARCXML, KBART, ONIX, JSON, CSV and RIS feeds, separates books, chapters
and records without a download link, and places OAI-PMH at
`https://library.oapen.org/oai/request`. Those feeds are CC0. The article that
used to document REST search,
`https://oapen.org/article/8185269-search-using-a-rest-api`, returns 404.

Observed the same day, one request each:

- `GET https://library.oapen.org/rest` returns the DSpace REST index. It
  documents `GET /bitstreams/{id}/retrieve`. It does not list `/search` or
  `/handle`. The proxy backend is `oapen-prod_dspace-rest_http`.
- `GET /oai/request?verb=Identify` names the repository OAPEN Library,
  protocol 2.0, `deletedRecord` transient, and the sample identifier
  `oai:library.oapen.org:20.500.12657/1234`.
- `GET /rest/handle/20.500.12657/25287?expand=metadata,bitstreams` returns
  *The deliverance of open access books*. `dc.type` is `book`. `archived` and
  `withdrawn` are the JSON strings `true` and `false`. Item metadata has no
  `dc.rights`. The DOI is `oapen.identifier.doi` `10.26530/OAPEN_1004809`.
  The `ORIGINAL` bitstream is `9789085551201_OA_version.pdf`,
  `application/pdf`, 9593943 bytes, MD5 `fad59e1f7accb638a009eb242938b54e`,
  `code` `CC-BY-NC`, and `dc.rights.uri`
  `http://creativecommons.org/licenses/by-nc/3.0/`. The same item also has
  `TEXT`, `THUMBNAIL` and `EXPORT` bitstreams, with `code` `Other open license`.
- `GET /rest/bitstreams/4ca2f021-851f-483a-af4e-efd1b86654ba/retrieve` returns
  HTTP 200 and `application/pdf`. The response begins as a PDF whose linearized
  length is 9593943. The body was not kept.
- `oai_dc` for that handle lists `dc:rights` as
  `info:eu-repo/semantics/openAccess`, `dc:format` `application/pdf`, and the
  handle URL. It has no file URL and no Creative Commons license. `dc:language`
  is `eng`; the REST item says `English`.
- `GET /rest/search?query=handle:"20.500.12657/25287"&expand=metadata&limit=1&offset=0`
  returns one record and the headers `x-total-count: 1`, `x-limit: 1`,
  `x-offset: 0`. Search is not on the REST index.
- `GET /rest/search?query=dc.type:chapter&limit=1&offset=0&expand=metadata,bitstreams`
  returns HTTP 503. No chapter record was observed.
- The same book on DOAB is handle `20.500.12854/26303`.
  `GET https://directory.doabooks.org/rest/search?query=handle:"20.500.12854/26303"&expand=metadata,bitstreams&limit=1`
  returns 200. Its bitstreams are `THUMBNAIL` and `EXPORT`. Two thumbnails
  carry `dc.rights.uri` values for CC BY-NC 4.0 and CC BY-NC 3.0, and both
  name `https://library.oapen.org/bitstream/20.500.12657/25287/1/9789085551201_OA_version.pdf`.
  Item `dc.rights` is `open access`. Earlier research recorded HTTP 403 from
  some networks.

DOAB's [metadata page](https://doabooks.org/en/librarians/doab-metadata)
documents CC0 feeds, OAI-PMH at `https://directory.doabooks.org/oai/request`,
and the PRISM peer-review API. Its
[REST guide](https://s3.cern.ch/doab-cms-media-prod/DOAB_REST_API_advanced_guide_710fe9a1da.pdf)
(version 1.0, August 2026) documents
`https://directory.doabooks.org/rest/search`, `expand=metadata`,
`expand=bitstreams`, and `limit`. An empty bitstream list there means the
request succeeded and no file object is attached.

No rate-limit header was present on these responses. No numeric quota is
adopted from an unread page.

## Decision

- A later adapter may evaluate `oapen:20.500.12657/N`, where `N` is 1 to 10
  digits, by reading
  `https://library.oapen.org/rest/handle/20.500.12657/N?expand=metadata,bitstreams`
  through the shared metadata client: fixed host, checked public destinations,
  bounded JSON, the existing pacing and quota handling. The suffix is the only
  caller-supplied path segment. Any other prefix, including DOAB
  `20.500.12854`, is refused.
- The evaluation records title, creators, the language string as returned,
  `dc.type`, issued year, ISBN, DOI and publisher as declarations. It does not
  normalize `English` to `eng`. It does not open a publisher site, a DOAB URL,
  or a path assembled from a file name.
- `withdrawn` must be the string `false` before any file URL is shown. Any
  other value, including absence or a JSON boolean, suppresses file URLs.
  `archived` `true` is the observed state of a book whose PDF was retrieved,
  so that value is reported and does not itself suppress a URL. Any other
  `archived` value suppresses file URLs.
- The only acquirable files are bitstreams whose bundle name is `ORIGINAL`.
  Each keeps its UUID, declared name, MIME type, size and checksum. `TEXT`,
  `THUMBNAIL`, `EXPORT` and any other bundle are omitted. A successful item
  with no `ORIGINAL` bitstream is a complete evaluation that lists no file.
  Bitstream `code` and `dc.rights.uri` remain declarations, distinct from
  item-level rights and from an `openAccess` statement.
- Acquisition, when that adapter is built, downloads one selected bitstream
  from `https://library.oapen.org/rest/bitstreams/<uuid>/retrieve` on the same
  host. The UUID must be an `ORIGINAL` bitstream of that evaluation. Size and
  MD5 stay source claims until the transfer measures the bytes. The existing
  acquisition checks apply.
- Keyword search is not part of this contract. `/rest/search` answered one
  handle query, and the old search article is gone. A search adapter needs its
  own decision. DOAB is a directory: its handle is a different identifier, and
  the observed record did not host the PDF. No DOAB file is acquired. OpenStax
  and LibreTexts are unchanged.
- This decision adds no command, module or network call.

## Consequences

[SOURCES.md](../SOURCES.md), the [roadmap](../../ROADMAP.md) and
[DEVELOPMENT.md](../DEVELOPMENT.md) record the boundary.
[Issue 77](https://github.com/blisspixel/nemalo/issues/77) tracks the adapter.
Metadata CC0 does not license a book. A chapter export exists on the metadata
page; chapter bitstream shape was not observed, so the adapter keeps `dc.type`
and still selects files by the `ORIGINAL` bundle alone.
