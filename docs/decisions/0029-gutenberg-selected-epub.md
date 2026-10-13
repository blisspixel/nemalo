# 0029: one selected Gutenberg EPUB

Status: accepted and implemented, 2026-10-13. Selected EPUB intake from one
pinned mirror. This does not change the offline catalog in
[decision 0028](0028-gutenberg-local-catalog.md), Archive checksum rules in
[decision 0007](0007-selected-acquisition.md), or strong-validator resume in
[decision 0023](0023-validator-bound-download-resume.md).

## Decision

`evaluate gutenberg:N` stays a local catalog row and lists no files. Search
does not fetch a bibliographic record. `evaluate gutenberg:N --files` and
`acquire gutenberg:N --file NAME` are the explicit network steps. Each fetches
one record, `https://gutenberg.pglaf.org/cache/epub/N/pgN.rdf`, with the shared
file client limited to `gutenberg.pglaf.org`. There is no second host and no
fallback to `www.gutenberg.org`.

The client is the checked direct file client: one host, public addresses,
identity encoding, and cancellation. The record is at most 1 MiB, UTF-8, and
rejected if it declares a document type. Parsing uses `encoding/xml` with a
token budget and fails closed. `pgterms:marc520` and the other generated
summary fields are not stored.

A file is an offer only when the record names it
`https://www.gutenberg.org/ebooks/N.KIND` for this same N, the media type is
`application/epub+zip`, the extent is a positive integer, and KIND is one of
three cache objects:

| Record name | Cache object |
| --- | --- |
| `N.epub.noimages` | `/cache/epub/N/pgN.epub` |
| `N.epub.images` | `/cache/epub/N/pgN-images.epub` |
| `N.epub3.images` | `/cache/epub/N/pgN-images-3.epub` |

The download URL is that object on `gutenberg.pglaf.org`. It is constructed
here. The website URL in the record is not requested. Any other format, an
EPUB whose KIND is not in the table, a different text number, or a media type
other than `application/epub+zip` is listed and is not acquired. Plain text,
HTML, Kindle, ZIP, covers, and the record itself stay in that list.

`--file` must be the record name, such as `1342.epub.noimages`. The packet
stores the bytes as `content.epub`. The record name stays in the intent.

The record has no checksum. Eligibility does not invent one, and Archive files
still need a source MD5 or SHA-1. The declared extent is stored and is not
required to equal the response length. The transfer requires a `Content-Length`
inside `--max-file-bytes` and checks the body against that length. A missing
length fails before the body is kept. When the two sizes differ, the receipt
says so.

A quoted ETag without the weak prefix is the same strong validator decision
0023 already accepts. Resume uses that validator and the recorded response
length. No strong validator means resume is not offered, and a later resume
refuses without changing the packet. A failure before any body still says to
start a new output.

The rights line is the record's `dcterms:rights` sentence. It is not a
file-level rights decision. The file remains untrusted intake. This slice adds
no publication state, no MCP tool, and no website search.

In Evaluate, `d` on a Gutenberg row that lists no files asks for the intake
folder first. With a folder, it fetches that one record and then lists the
EPUB offers. Archive and Open Library rows do not gain this fetch.

## Evidence

Project Gutenberg's [mirroring how-to](https://www.gutenberg.org/help/mirroring.html)
describes the generated collection under `cache/epub/N/`. The filename table is
the `FILENAMES` map in Project Gutenberg's
[pg_archive_urls.py](https://github.com/gutenbergtools/libgutenberg/blob/master/pg_archive_urls.py),
and the generator log names the same objects (`pgN.epub`, `pgN-images.epub`,
`pgN-images-3.epub`). On 2026-10-13, HEAD requests to `gutenberg.pglaf.org`
returned 200 for all three text-1342 objects and for `pg1342.rdf`. Each EPUB
ETag was quoted and not weak (`"6abe5e40-88fce"`, `"6abe5e42-17b298f"`,
`"6abe5e4c-17af9c4"`). `Content-Type` was `application/octet-stream`. Lengths
were 561102, 24848783, and 24836548. The record fetched the same day, 18220
bytes, declared extents 561111, 24848791, and 24836543 for those three names,
with media type `application/epub+zip` and rights "Public domain in the USA."
The no-images object is the one the website redirects to for
`1342.epub.noimages`. This client does not follow that redirect.

`www.gutenberg.org` is not a download host for this slice. Other mirrors that
answered a cache path, or that 404, are not fallbacks.

## Consequences

`nemalo acquire gutenberg:NUMBER --file NAME --output NEW_DIRECTORY` downloads
one selected EPUB into the existing intake packet, including the scan and
quarantine already used for Archive. `evaluate --files` prints the offers and
does not download one. A catalog row still cannot be acquired until that record
is fetched. Packets whose response length differs from the declared extent
record the difference and still verify the bytes they received. A configuration
file is unchanged. 0.1.0-alpha.9 does not include this command.
0.1.0-alpha.10 is the first release that includes it.
