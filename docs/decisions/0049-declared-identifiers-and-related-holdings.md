# 0049: source-declared identifiers and related holdings

Status: accepted and implemented, 2026-10-19. Stage 1 of the
[order of work to 1.0](../../ROADMAP.md#order-of-work-to-10). Extends
[decision 0010](0010-bibliographic-identity.md), which records identifiers an
EPUB package states, and [decision 0044](0044-narration-identity.md), which
relates recordings of one reading.

## Context

A holding's identity is its SHA-256. Package identifiers (decision 0010) were
the only edition and work evidence, so two scans of one work from Archive, or two
versions of one arXiv paper, had nothing linking them. Archive item metadata
often declares `isbn`, `openlibrary_edition`, and `openlibrary_work`; checked
live on 2026-10-19, `prideprejudicebe0000jane` declares ISBN `0330106899`,
edition `OL49359464M`, and work `OL66554W`.

## Decision

- An Archive evaluation copies, as declared, the values of `isbn`,
  `openlibrary_edition`, `openlibrary_work`, and the `urn:isbn:` values of
  `external-identifier`, at most 32, each at most 300 bytes. A field of another
  shape is skipped without failing the evaluation. The evaluation lists them as
  `declared_identifiers`.
- Import records them on the packet's source as identities with origin
  `source_metadata` and confidence `source_declared`: an ISBN with a valid check
  digit as an edition, an Open Library key as an edition or work. Any other value
  is not recorded. At most 16 are kept, in identity order. A stored identity that
  does not reclassify to itself fails closed, as package identities do. A later
  import of the same packet adds identities to a source recorded before them.
- `library related LIBRARY [--asset ID]` and the read-only MCP tool
  `library_related` group holdings that share one key: a stated work, edition,
  or publication version, from a package or a source; versions of one arXiv
  paper (`arxiv:ID`, each member with its version); a narration; or files of one
  Archive item or Gutenberg book. Each member lists the statements that place it.
  Only keys held by two or more holdings form a group; a holding can be in
  several groups.
- Nothing is merged, no group implies the same text, edition, or bytes, and
  titles, authors, and text are never compared. Identifiers are not confirmed
  against a registry.
- An arXiv DOI names the published article, a different work of record from the
  preprint, so it is not recorded as the preprint's identity.

## Consequences

Holdings acquired from Archive relate by the work and edition the item
declares, arXiv versions relate by paper, and evidence results in milestone 9
can name the work behind an asset. A holding file with source identities needs
this version or newer, since older versions refuse unknown fields. Relating an
Open Library edition chosen at evaluation when the Archive item does not
declare it, and registry confirmation, remain later work.

## Validation

Offline tests: two Archive scans declaring one work grouped, with an invalid
ISBN and an unknown field not recorded; two files of one item grouped; two
versions of one arXiv paper grouped in version order and a third paper alone; a
filter to one holding; forged, duplicated, and unknown-field identities refused
on read; Archive fields of the wrong shape skipped and the count bounded.
Live: the Archive item above lists its three declared identifiers in evaluate.
