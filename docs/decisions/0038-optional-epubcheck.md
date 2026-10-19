# 0038: optional EPUBCheck adapter

Status: accepted and implemented, 2026-10-19. Milestone 3's optional
EPUBCheck adapter, beside the scanner adapters of
[decision 0012](0012-scan-on-acquire-quarantine-and-rescan.md).

## Decision

`nemalo check FILE --epubcheck` runs an installed `epubcheck` found on `PATH`
on the private assessment snapshot of an EPUB:
`epubcheck SNAPSHOT --json PRIVATE_DIR/report.json`, with an argument array,
no shell, output discarded, a five-minute deadline, and the report read from a
private temporary directory with a 16 MiB bound. Nemalo never downloads,
installs, or updates EPUBCheck, and `doctor` reports whether it is available.

The result is read from the JSON report, not the exit code. The report must
name a `checkerVersion` and give non-negative `nFatal`, `nError`, `nWarning`,
and `nUsage` counts and a message list whose severities are known; a report
that counts errors it does not list, or lists errors it does not count, is not
taken at its word. Statuses:

- `passed`: no fatal errors or errors; warnings may be listed.
- `errors_reported`: a review finding, "EPUBCheck reported errors".
- `unavailable` or `incomplete` (no report, an unreadable or inconsistent
  report, or the deadline): a review finding, because a requested check that
  did not complete is not a pass.

Up to 200 messages are kept with their ID, severity, text, and up to five
locations inside the EPUB. After the run the snapshot is revalidated by
identity, size, and SHA-256, as after a scan. Other formats ignore the option.

EPUBCheck is optional. It is not part of the checked policy, so import,
ingest, and rescan do not run it and a library never depends on it. It is a
conformance result, separate from limited checks, antivirus coverage, and
completeness. MCP `file_check` does not run it, as it runs no scanner.

## Interface evidence

EPUBCheck 5.3.0 source (`EpubChecker.java`, `CheckingReport.java`,
`CheckMessage.java`, `CheckerMetadata.java`, `Severity.java`) defines
`--json FILE`, the `checker` block with `checkerVersion` and the four counts,
messages with `ID`, `severity`, `message`, and `locations`, the severities
FATAL, ERROR, WARNING, USAGE, INFO, and SUPPRESSED (not written), and exit
code 1 for errors and for invalid arguments. Arch Linux `extra` packages
EPUBCheck 5.4.0.

## Validation

Tests run the test binary as a stand-in `epubcheck`: a report with a warning
passes, one with an error is `errors_reported` with its location, and a
missing report, non-JSON, a missing checker block, version, or message list,
an error counted but not listed, errors listed but not counted, a negative
count, and an unknown severity are each `incomplete`. A checker that never
exits stops at the deadline, 205 messages keep 200, and a missing checker is
`unavailable`. Assessment turns a clean EPUB with an unavailable EPUBCheck into
`needs_review` and leaves an MP3 unaffected.

Live, on Linux amd64, against the official EPUBCheck 5.4.0 release run by an
Eclipse Temurin 21 JRE through an `epubcheck` wrapper on `PATH`: three Project
Gutenberg EPUBs held in an earlier validation passed with 0 fatal, 0 errors,
and 0 warnings in about six seconds each, and their status stayed `limited
checks passed`. A copy of one with an unknown element and an undeclared
attribute added to one document reported 2 errors, both `RSC-005` with the
member path and line, and `check` held it in review and exited 1.
