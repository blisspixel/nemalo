# 0032: checked PDFs and PDF active content

Status: accepted and implemented, 2026-10-19. Extends the checked policy of
[decision 0009](0009-managed-epub-import.md) to PDF, for milestones 3 and 7,
over the page tree walk of [decision 0021](0021-pdf-pages-and-text.md).

## Decision

`check`, import, ingest, and rescan examine a PDF's active content: anything a
reader may run, open, fetch, or extract without the person choosing to. A
bounded walk visits every object reachable from the trailer once, reading
dictionary keys and array items and never decoding stream data. It reports:

- `pdf_javascript`: a `JS` or `JavaScript` entry, or a JavaScript action.
- `pdf_launch_action`: a Launch action.
- `pdf_automatic_action`: an `AA` entry, or an `OpenAction` other than opening
  at a page (a destination array or a GoTo action is ordinary).
- `pdf_embedded_file`: `EmbeddedFile`, `EmbeddedFiles`, an `EF` file
  specification, or a FileAttachment annotation.
- `pdf_form_submission`: SubmitForm or ImportData.
- `pdf_remote_reference`: GoToR or GoToE.
- `pdf_rich_media`: RichMedia, Movie, Sound, Screen, 3D, or a rendition.
- `pdf_xfa_form`: an XFA form.
- `pdf_active_content_not_fully_examined`: the walk reached 500,000 values or
  64 levels of nesting, or was cancelled, so the rest is unknown.

Web links (URI actions) open only when a person follows them, so they are
counted as `web_links` and are not findings; papers cite DOIs as links. Each
finding is an assessment warning, so the file stays in review. Nothing is
removed, rewritten, or rendered.

A PDF is `checked`, with reason `checks_and_scan_recorded`, exactly as an EPUB:
limited checks passed with no finding, the page tree was walked completely, the
active-content walk finished with nothing found, and the installed scanner
reported no detections. Otherwise it stays in review with the reason, or
`pdf_not_fully_examined` when the walk did not finish. An encrypted PDF cannot
be walked and stays in review. MP3 follows in
[decision 0033](0033-mp3-checked-policy.md).

`library rescan` now assesses PDF holdings in review for lack of a scan,
including those an earlier version held as
`format_not_checked_for_publication`, and records `checked` when the policy is
met. A library that holds a checked PDF needs this version or newer: an earlier
version reads that journal line as invalid and keeps the library for review.

## Evidence

[ISO 32000-2](https://www.pdfa.org/resource/iso-32000-2/) defines the action
types (JavaScript, Launch, SubmitForm, ImportData, GoToR, GoToE, Rendition,
RichMediaExecute), additional-action (`AA`) dictionaries, and embedded file
streams that this walk names. Automatic actions and scripts are the parts a
reader may run on its own; ordinary navigation is not.

## Validation

Synthetic PDFs cover a plain page, opening at a page and a GoTo on open
(neither is a finding), a script on open, named scripts, a launch link, an
attached file and a file attachment annotation, form submission, a remote
document, an XFA form, two web links (counted, not findings), a widget with an
automatic action, a reference cycle examined once, and a chain deeper than the
nesting bound (reported as not fully examined). Library tests import a clean
PDF as checked, keep one with a script in review, and promote an unscanned PDF
and one held under the earlier reason by rescan. Live: the arXiv PDF of
1706.03762v7 (15 pages, 18 web links) and NIST CSWP 29 (32 pages, 56 links)
reported no active content, the second in 0.18 seconds.
