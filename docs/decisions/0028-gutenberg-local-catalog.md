# 0028: Project Gutenberg local catalog

Status: accepted and implemented, 2026-10-12. Search and evaluation for one
P0 source. Selected EPUB intake is decision 0029. This does not change Archive
or Open Library behavior in
[decision 0005](0005-provider-search-and-evaluation.md).

## Decision

Project Gutenberg search and evaluation read a local copy of the official
catalog, `pg_catalog.csv`. They do not request gutenberg.org. The website
search pages are for people. The permitted machine-readable catalog is the
CSV named on the offline-catalogs page.

`catalog fetch gutenberg --output FILE` is the explicit network step. It
downloads one URL,
`https://www.gutenberg.org/cache/epub/feeds/pg_catalog.csv`, to a new
absolute path, with the shared file client limited to `www.gutenberg.org`.
A failed download or a catalog that does not parse is removed. Search never
calls this command.

The catalog path is `gutenberg_catalog` in config, `NEMALO_GUTENBERG_CATALOG`,
or `--catalog` on `search`, `evaluate`, and `tui`. It must be absolute. An
empty path is an error that names the setting and says the website is not
queried.

Identifiers are `gutenberg:N`, where N is a canonical positive decimal text
number of at most 8 digits and no leading zero. Evaluation of a missing row
is an error. A present row is `complete`, with access `local_catalog`, an
empty file list, and no publishers field. The landing page is the human
ebook URL and is not fetched. `metadata_sha256` is the SHA-256 of the
canonical CSV encoding of that row, including the trailing newline
`encoding/csv` writes. `checked_at` is the time of the local read.

Limits recorded on every evaluation: the row came from a local file; it
lists no files, checksums, or download URL, so nothing is offered for
acquisition; it does not state rights, and Project Gutenberg's permissions
are US-based and are not applied to the book by this record; catalog
metadata is not a byte check, a scan, or edition identity.

Local ranking is explicit. Relevance rank 0 is an exact text number
(`1342` or `gutenberg:1342`) or a title substring. Rank 1 is an author
substring. Rank 2 is a subject or bookshelf substring. Equal ranks use the
lower text number. Identifier order is that number. There is no downloads
order. Matching uses Unicode simple lowercasing. A query is not a substring
of the text number, so `1` does not match every number that contains 1.
Title newlines collapse to spaces.

The catalog fails closed. The header must be exactly
`Text#,Type,Issued,Title,Language,Authors,Subjects,LoCC,Bookshelves`.
A repeated text number, a non-canonical number, an empty title, a field
over 16384 bytes, NUL, or invalid UTF-8 fails the file. Issued is empty or
`YYYY-MM-DD` as digits and hyphens, not a calendar check. The file must be
a regular non-symlink of at most 64 MiB. Parsing streams rows and keeps only
the best `offset+limit` matches, at most 10050, plus a total count.

Authors, subjects, Library of Congress classes, and bookshelves split on
semicolons. Languages split on semicolons, or commas when every piece is a
short ASCII token of at most 16 bytes; otherwise the field stays one
language string.

Each result keeps the row's Type and Issued as `media_type` and `issued`,
omitted when empty, and the CLI and TUI show them as a catalog claim, such
as `Sound, issued 2007-02-03`. One title can name a text and its recordings
(issue 24); without these fields they looked identical until evaluated.
Open Library and Archive results leave both fields empty.

The TUI cycles Open Library, Internet Archive, and Gutenberg. Gutenberg
search shows a catalog field and does not say the search is a network
request. That field is not applied to the other providers. `e` evaluates
`gutenberg:` results. An evaluation with no files offers no acquisition.
F3 cycles relevance and identifier only.

MCP is unchanged. There is no Gutenberg tool. `catalog_search` remains
Open Library and Internet Archive, and it still requires `--allow-network`.

## Evidence

Reviewed the Project Gutenberg
[robot access policy](https://www.gutenberg.org/policy/robot_access.html)
and the
[offline catalogs page](https://www.gutenberg.org/ebooks/offline_catalogs.html)
on 2026-10-12. The policy reserves the website for people and points
automation at mirrors, the harvest robot, and offline catalog data. The
catalogs page says to use the catalog files instead of crawling the site,
names the weekly CSV, and says print publication dates are not in the
metadata. The
[permissions page](https://www.gutenberg.org/policy/permission.html) is
US-based and is not copied into a catalog row.

One download of the CSV on 2026-10-12 was 21,253,758 bytes, with no byte
order mark and the header above. It had 79,576 data rows, no repeated text
numbers, no empty titles, and no field over 16384 bytes. Every Issued value
was `YYYY-MM-DD`. Types were Text (78,325), Sound (1,114), Dataset (89),
Image (33), MovingImage (8), Collection (4), and StillImage (3). Authors,
subjects, shelves, and classes use semicolons (23,766 author rows,
47,804 subject rows, 66,020 shelf rows, 3,281 class rows). 219 language
fields use semicolons, including `en; fr`, and each piece was a short ASCII
tag. That file is not in the repository. Tests use a synthetic catalog.

The same day, `catalog fetch gutenberg` used the production file client and
wrote that catalog. The parser accepted it. Search put `gutenberg:1342`
first for Pride and Prejudice, `gutenberg:84` for Frankenstein, `gutenberg:11`
for Alice, and `gutenberg:1` (the Declaration of Independence) first for the
word "the", which matched 42,111 rows in under a second. `gutenberg:1342`
matched that row only. The digits `1342` also matched a subject containing
the year 1342, after the exact text. Evaluation of a Text, a Sound, a
StillImage, and a MovingImage listed no files. Gutenberg 18 listed Hamilton,
Jay, and Madison as three creators. Gutenberg 417 joined a two-line title
into one line and split languages `de` and `en`. A second fetch to the same
path was refused and the bytes stayed put. A truncated copy, a directory,
and a symlink were refused. A path containing spaces worked.

The CSV does not contain download URLs. This catalog record still lists no
files. Selected EPUB intake is
[decision 0029](0029-gutenberg-selected-epub.md). Harvest, OPDS, and website
search stay out of this catalog.

## Consequences

`nemalo search --source gutenberg` and `nemalo evaluate gutenberg:NUMBER`
work offline once a catalog file is configured. `catalog fetch gutenberg`
is the catalog network command, and it writes one checked CSV. Archive
acquisition is unchanged. A Gutenberg hit cannot be acquired from this
catalog record. Selected EPUB intake uses the bibliographic record in
[decision 0029](0029-gutenberg-selected-epub.md). A configuration file that contains `gutenberg_catalog` cannot be
read by 0.1.0-alpha.8, which rejects unknown keys. A file without the key
is unchanged. 0.1.0-alpha.9 is the first release that includes this catalog.
