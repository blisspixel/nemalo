# 0037: base-language filter over declared tags

Status: accepted and implemented, 2026-10-19. Milestone 3's language
acceptance item: normalize language tags without inventing missing metadata,
and support base-language filtering.

## Decision

`library list LIBRARY --language TAG` and the MCP `library_list` `language`
input keep holdings whose declared languages include one with the same base
language as `TAG`. Declared values are read as BCP 47 tags, and ISO 639-2 and
639-3 codes are read through their BCP 47 equivalents, so `en`, `en-US`, and
`eng` share the base `en`, `fre` and `fra` are `fr`, and the deprecated `iw` is
`he`. A value that does not parse exactly, such as the free text `English`,
`und`, or a private-use tag, has no base and never matches; it is not guessed
from text, titles, or file names. A filter that is not a readable tag is
refused.

Holdings that met the other filters but declare no readable language are
counted in `language_unknown` and named in the text output, rather than
silently dropped. Recorded values are unchanged; only the filter compares
bases. Snapshot catalogs do not take the filter.

## Dependency

Tag parsing uses `golang.org/x/text/language` at v0.42.0, maintained by the Go
project and BSD-licensed. BCP 47 with its ISO 639 equivalences and deprecated
codes is a standards-heavy registry better taken from a maintained
implementation than a hand-kept table. The module was already in the build
graph as a requirement of `golang.org/x/net` at this exact version, so the
pinned module set gains no new source or version; `go.mod` now names it
directly. It adds no executable, network access, or platform requirement.

## Validation

`internal/langtag` tests read 15 forms to their base, including ISO 639-2
bibliographic and terminology codes and script and region subtags, and refuse
empty, free text, `und`, private-use, malformed, and over-long values. A
library test lists a Japanese EPUB, a Japanese and `en-US` EPUB, and an MP3
with no language: `eng`, `en`, and `EN-gb` find one, `ja` and `jpn` find two,
`fr` finds none, each with one unknown, and `English` is refused.
