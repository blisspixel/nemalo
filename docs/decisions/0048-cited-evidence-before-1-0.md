# 0048: cited evidence and agent reading before 1.0

Status: accepted, 2026-10-19. Changes the 1.0 boundary set in the
[roadmap](../../ROADMAP.md#version-10-library-and-discovery) and
[product contract](../PRODUCT.md). Splits
[issue 37](https://github.com/blisspixel/nemalo/issues/37) and
[issue 38](https://github.com/blisspixel/nemalo/issues/38).

## Context

All analysis work was post-1.0. Two parts of it differ from the rest:

- Explaining a work, as issue 37 describes, can be done by a person's own agent
  over Nemalo's existing read-only MCP content tools, which already return exact
  text with locators and declared gaps. Nemalo needs no model, provider, key, or
  spending for this; it needs a result that states what was read and what was not.
- The evidence result issue 38 asks for (exact asset and version, extractor,
  anchors, quoted versus interpreted content, coverage) is a contract. 1.0 freezes
  the reference formats a later analysis phase would depend on. Designing that
  contract after 1.0 would force a breaking change or a second format.

The rest, model processing inside Nemalo, a generated Markdown workspace, and
capture of video, podcasts, feeds, and the web with watches and refresh, carries
dependency, cost, privacy, and provider-terms work that would delay the library.

## Decision

- Milestone 9, before 1.0: a versioned evidence result for library holdings
  over the content services, documented citation export, and agent guidance for
  reading a work from its own structure, with acceptance reviewed on actual
  agent outputs.
- Nemalo runs no model and stores no generated interpretation in milestone 9.
  Retrieval stays offline, read-only, and unrecorded.
- Post-1.0, unchanged in substance: processing inside Nemalo, persistence of
  generated work, source capture beyond the library, and watches.
- The roadmap states the remaining 1.0 work as ordered stages by dependency, with
  no time estimates.

## Consequences

1.0 gains one milestone that shapes its contracts rather than adding
infrastructure. Identities from milestone 1 and audio ranges from milestone 8
come first, since evidence names them. Issues 37 and 38 keep their post-1.0
scope; their pre-1.0 parts are tracked in milestone 9.
