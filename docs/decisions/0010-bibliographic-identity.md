# 0010: bibliographic identity is package evidence

Status: accepted and implemented, 2026-10-09.

## Decision

A managed holding keeps the asset SHA-256 as the only identity used to store,
deduplicate, and retrieve bytes. Work, edition, and publication-version
identifiers are recorded only when an EPUB package states one of the forms
below. The same identifier on two files does not merge those files. A matching
title, author, filename, archive item, or UUID does not create an identity.

This increment does not assign recording or track identities, does not read PDF
or audio metadata for bibliographic identifiers, and does not build a separate
work catalog. Snapshots remain byte inventories.

## What is recorded

Assessment copies package identifier strings. It does not assign a bibliographic
role. Import then keeps at most 32 identities on an EPUB holding:

| Stated form | Role | Recorded ID |
| --- | --- | --- |
| Scheme `ISBN`, `ISBN-10`, or `ISBN-13`, or a `urn:isbn:` value, with a valid check digit | `edition` | `isbn:` and the digits |
| Scheme `DOI`, or a `doi:` / `https://doi.org/` / `http://dx.doi.org/` value | `publication_version` | `doi:` and the DOI body |
| `https://openlibrary.org/works/OL…W` | `work` | `openlibrary_work:` and the key |
| `https://openlibrary.org/books/OL…M` | `edition` | `openlibrary_edition:` and the key |

Any other package identifier, including a UUID, an invalid check digit, or an
Archive item, is kept as role `unassigned`. Its ID is `stated:` plus the
SHA-256 of the scheme and the raw value. Recognized roles use confidence
`scheme_stated`. Preserved strings use confidence `preserved`. Neither
confidence is registry confirmation.

`identities_recorded` means the EPUB package was read. An empty result omits
the identity list. PDF and MP3 holdings omit the flag, because their
bibliographic identifiers were not read. Identifiers that exceed the size,
count, or character limits are counted in `identifiers_omitted` and are not
truncated into an identity. The package `unique-identifier` flag is stored with
the matching string and does not change its role.

A later import of the same source does not write again when the asset facts
and these identities are unchanged. A crafted identity that does not match its
raw value, or that uses a role this record does not define, fails closed.
The holding file is not repaired.

## Still outstanding

Recording and track identities wait for a manifest that states them. PDF
page-level publication identity waits for the PDF assessment policy. Links
from a work to its editions, and from an edition to a recording, are not
created here. Issues 1 and 2 stay open until their PDF, audio, MCP, and
external-caller acceptance is implemented.
