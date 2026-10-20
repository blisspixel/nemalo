# 0047: one explicit arXiv paper version

Status: accepted and implemented, 2026-10-19. The first paper provider of
milestone 7, tracked by [issue 19](https://github.com/blisspixel/nemalo/issues/19),
reusing the selected acquisition of [decision 0007](0007-selected-acquisition.md),
PDF assessment ([decision 0032](0032-pdf-checked-policy.md)), and PDF text
([decision 0021](0021-pdf-pages-and-text.md)).

## Context

A research PDF could be read once supplied, but Nemalo could not resolve or
acquire one. arXiv's [API terms](https://info.arxiv.org/help/api/tou.html),
checked 2026-10-19, allow one connection and at most one request every three
seconds to its APIs, and use of e-prints for personal or research use; serving
e-prints to others needs the copyright holder's permission or the paper's
license. The [query API](https://info.arxiv.org/help/api/user-manual.html) at
`export.arxiv.org` returns a paper's versioned identifier, title, authors,
dates, DOI, and category, but no license, size, or checksum. The license is in
the OAI-PMH `arXiv` record at `oaipmh.arxiv.org`, for the latest version only.

## Decision

- `evaluate arxiv:ID` or `arxiv:IDvN`, in the new scheme (`YYMM.NNNN` from
  April 2007 through 2014, `YYMM.NNNNN` since) or the old one
  (`hep-th/9901001`, August 1991 through March 2007; the subject class form
  `math.GT/0309136` is refused, since arXiv answers with the archive form),
  asks the query API for the latest version and, for an
  older version, for that version too. An unversioned request is resolved to
  the latest, shown as `arxiv:IDvN` with "version N of M". A version beyond
  the latest, an unknown identifier, or an answer naming another paper or
  another version than the one asked for is an error.
- The license from OAI-PMH is recorded only when the selected version is the
  latest and the record names this paper; for an older version the latest
  license is quoted and that version's license stays not established.
- One file is offered, `IDvN.pdf` (slashes of an old identifier become
  underscores), from `https://export.arxiv.org/pdf/IDvN`. Its size is the
  length a HEAD request returns with a PDF content type; without one no
  length is declared and the file cannot be acquired. arXiv's ETag is a
  validator, not a checksum: on 2026-10-19 its `sha256:` value did not equal
  the SHA-256 of the PDF served, and it is not used as one.
- `acquire arxiv:IDvN --file IDvN.pdf` needs an explicit version, so the
  version is seen before anything is fetched. It runs the shared selected
  acquisition with no source checksum: the transfer must be exactly the
  declared length and within the budget, and Nemalo records the SHA-256 it
  measured. The PDF is then assessed, scanned, and imported like any PDF.
- Every arXiv request shares one pace of one request every three seconds; a
  transport failure or a 429 or 5xx answer is tried again at that pace at most
  three times in all. A `Retry-After` delays every later request and never
  shortens the pace; one longer than 30 seconds refuses every arXiv request
  until it passes. Other answers are not retried. One shared client holds at
  most one connection per host. Requests go only to `export.arxiv.org` and
  `oaipmh.arxiv.org`.
- arXiv has no search here and no MCP tool, and `search --source all` does
  not include it.

## Consequences

A paper version can be resolved, acquired, checked, read page by page, and
quoted exactly, and two versions are two assets with their own provenance.
An evaluation makes three or four paced requests, about ten seconds. arXiv
does not support resuming a transfer here, so an interrupted one is retained
and fetched again. DOI resolution, other providers, and citation export
remain later work in milestone 7.

## Validation

Offline tests through a stand-in transport: an unversioned identifier
resolved to version 7 with its license and a PDF length, every request to the
two hosts and paced; version 1 with its own title and no license; version 9
refused as beyond the latest; an unknown identifier and an answer naming
another paper refused; a license record naming another paper not applied; a
PDF without a length not acquirable; a download outside the pinned URL
refused; a 429 tried three times and a 403 once. Identifier tests cover both
schemes, the 2015 change in sequence length, months, and PDF names.

Live on 2026-10-19: `evaluate arxiv:1706.03762` resolved to `v7`, "Attention
Is All You Need", declared 2,215,244 bytes, and quoted the latest license;
`arxiv:1706.03762v1` declared 2,128,686 bytes and left its license not
established. `acquire arxiv:1706.03762v7` transferred exactly 2,215,244 bytes
(SHA-256 `bdfaa68d…df697`), ClamAV reported no detections, and the PDF imported
as checked with source `arxiv:1706.03762v7`; `pdf-text/1` gave its 15 pages and
located the exact title. An unversioned acquisition was refused before any
request.

An independent adversarial review then found, each now fixed with a test: a
short `Retry-After` replaced the next permitted time and broke the pace; a
long one was ignored by later requests; a new client per request opened a
connection each time; a versioned query answered with another version was
accepted with that version's title; the subject class form could never
resolve; and a dropped transfer from a source that cannot resume reported
the resume refusal instead of its own error. In the TUI an unversioned
evaluation acquires the version it resolved, and the review screen describes
arXiv rather than Archive.

On 2026-10-20, hosted Windows CI exposed the wall-clock pacing assertion again.
The test now uses Go's virtual clock to check the full
three-second product interval. Further deterministic regressions showed that a
waiting call ignored an extended Retry-After and that allowed redirects bypassed
the pace. The gate now rechecks clock and quota after every wake, grants slots
when due rather than reserving future ones, and refuses a newly long Retry-After
even for an already waiting call. Cancelled waiters do not consume slots. Followed
redirects retain the checked-host policy and share the same owner's pace, including
on a lazily initialized client. Tests cover concurrent waiters, cancellation,
quota extensions, and redirects without network access or reduced intervals.
Request dispatch is serialized through response headers, and the next interval
starts after those headers arrive, so scheduling or header delays cannot compress
the pace. This is a conservative minimum interval; body reading keeps its own
bounds and the existing per-host connection limit. A delayed-dispatch regression
checks this separately from virtual timer and quota behavior.
