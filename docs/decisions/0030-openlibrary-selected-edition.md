# 0030: one selected Open Library edition

Status: accepted and implemented, 2026-10-13. An Open Library work lists
editions, and one chosen edition may name an Archive item. This does not
change Archive file selection in
[decision 0007](0007-selected-acquisition.md) or the search contract in
[decision 0005](0005-provider-search-and-evaluation.md).

## Decision

Search stays a work query. The request fields remain
`key,title,author_name,language`. The result id is `openlibrary:OL<digits>W`
and its access stays `discovery_only`. Search does not request `ia`,
`availability`, or `editions`.

`evaluate openlibrary:WORK --editions` fetches one page,
`https://openlibrary.org/works/OL<digits>W/editions.json`, with `limit` 1-50
and `offset` 0-10000. It does not follow the `next` link in the response.
The entry count must equal the smaller of `limit` and the declared editions
remaining after `offset`. A shorter or longer page is refused, so the next
page cannot skip entries. The page is bibliographic. It does not contact
Internet Archive.

`evaluate openlibrary:EDITION` fetches
`https://openlibrary.org/books/OL<digits>M.json`. The Archive item is the
edition field `ocaid` when that value is one identifier accepted by
`archive:ITEM`. The existing Archive evaluation then runs for that item, and
the result records the edition id. Acquire stays `acquire archive:ITEM`.
The edition id is not itself an acquire id.

These are not selections:

- The `ia` list on a work. Search documentation says `availability` is the
  first value in that list. That value is not a chosen edition.
- The `editions` object inside a search hit. The search documentation says
  that object currently returns one edition, and requesting it can change
  which works match.
- An `ia:` value in `source_records` when it differs from `ocaid`. It is
  listed on the edition evaluation and is not requested.
- An `ocaid` that is not one string identifier. The edition evaluation fails
  closed. A list page omits that entry and says so.
- `/isbn/...json` and `/api/volumes/brief`. The books documentation treats
  the ISBN URL as a redirect, and the API index calls the volumes route a
  legacy partner API.

An edition with no `ocaid` is a complete bibliographic record with no files.
Restricted, private, and lending-only Archive items stay on the Archive
evaluation that already refuses them. No second acquire path is added.

The metadata client still waits one second between requests to the same
provider. The documented anonymous limit is one request per second. The
User-Agent identifies Nemalo and does not include an email address, so the
higher identified limit is not assumed. Listing editions does not fan out
into one Archive request per row.

## Evidence

Reviewed on 2026-10-13:

- [APIs](https://openlibrary.org/developers/api), last edited 2026-10-09.
  Search and the work and edition record URLs are current. The legacy partner
  API is the former Read API. The anonymous rate is one request per second,
  and three per second when the User-Agent includes a contact email.
- [Search API](https://openlibrary.org/dev/docs/api/search), last edited
  2025-05-07. Works are the default. `availability` reads the first `ia`
  value. The editions sub-object returns one matching edition.
- [Books API](https://openlibrary.org/dev/docs/api/books), last edited
  2025-05-06. A work's editions are `/works/OLID/editions.json`. One edition
  is `/books/OLID.json`.
- [FastAPI migration](https://docs.openlibrary.org/projects/fastapi-migration.html),
  status August 2026. This is an internal move. The public record URLs above
  remain the documented contract. The old internal endpoints page was retired
  on 2026-03-26; its replacement does not name a different public path for
  these reads.
- [Guide to identifiers](https://docs.openlibrary.org/librarians/guide-to-identifiers.html).
  `ocaid` is the Archive item identifier on an edition.

A direct read on 2026-10-13 of
`/works/OL45804W/editions.json?limit=2&offset=0` returned `size` 161, a
relative `next` link, and edition records. The same day's read of
`/books/OL7353617M.json` had `ocaid` `fantasticmrfoxpu00roal` and a source
record `ia:fantasticmrfox00dahl_834`. Those are different Archive items.
Using the source record, or the first work-level `ia` value, would select a
different item from the edition's `ocaid`.

## Consequences

CLI and TUI can list a page of editions and then evaluate one edition.
0.1.0-alpha.11 is the first release that includes it. MCP
`catalog_search` remains work search. `catalog_evaluate` remains
`archive:ITEM`. There is no Open Library edition tool. Checked publication
is unchanged.
