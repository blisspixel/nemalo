# Changelog

## Unreleased

- Prepare internal exact MP4 packet selections through the existing parser,
  preserving source byte positions and media-table timing with bounded reads.
  This is groundwork for audio ranges; public `mp4-audio/1` remains units-only
  and no decoder is added to content access.

- Read complete supported nested volume sets through shared intake, preserving
  original source chains and every continuation. Completely covered trees with
  verified held leaves use distinct `archive-tree/3`; fresh purge repeats the
  whole proof and partial recovery binds the original evidence without
  reconstruction. Earlier proof scopes stay unchanged, raw restore needs no
  reader, and unsupported or uncertain sources stay preserved.

- Add internal opt-in nested-volume intake and read-only coverage through the
  existing checked set adapters. Preserve every original part and source chain,
  share work limits across groups, and refuse ambiguous or failed parts. All
  application callers stayed disabled in that read-only increment; existing
  proof IDs gained no authority from its separate child-set evidence.

- Clean completely covered old-name root RAR4/RAR5 sets under distinct versioned
  proofs after exact held-leaf and source checks. Fresh purge repeats the whole
  proof; partial recovery uses immutable original evidence and verified remaining
  bytes. Earlier proofs retain their scopes, raw restore replaces nothing, and
  unsupported material stays preserved. Nested sets need the separate tree
  authority above.

- Add opt-in read-only old-name RAR4/RAR5 set coverage with distinct internal
  evidence, strict physical and reader-table binding, and shared work limits.
  This read-only increment kept application cleanup disabled until its separate
  verified writer contract; that root-only evidence did not cover nested sets.

- Keep EPUBs with active URI indicators in review even when antivirus reports no
  detections. Recognize encoded and whitespace-separated script schemes, data
  navigation, and XHTML XLink aliases without treating metadata attributes as
  navigation. Bound XML inspection and preserve static images, ordinary links,
  extractor identities and eligible source continuations.

- Read old-name RAR sets across supported later starting positions and letter
  rollovers by recognizing exact isolated-reader fallback names. Keep checked
  volume identity, split continuity and a clean assembled table; these sources
  still cannot gain cleanup authority from intake diagnostics alone.

- Change the tagline to "Knowledge is power." in help, the terminal interface and
  packaged guides. Shorten the README and refresh its application-view captures;
  detailed capabilities and limitations remain in the linked operation guides.

## 0.1.0-alpha.17

Seventeenth native prerelease. This remains an early library, not 1.0. The roadmap now orders the remaining 1.0 work by dependency and brings cited evidence for agent-led reading before 1.0 ([decision 0048](docs/decisions/0048-cited-evidence-before-1-0.md)). Holdings relate by the identifiers their sources declare, collections of exact selections are resolved against the library and acquired idempotently, and 7z archives and split sets can be cleaned up after import.

- Collections, related holdings, and 7z cleanup were reviewed adversarially before release; the fixes are listed below.
- `library cleanup` moves 7z archives, and split 7z sets as one entry, when every member is held and 7-Zip lists the archive with no comment, offset, or tail and with its header and packed sizes filling the file; restore and purge handle a set volume by volume. A RAR stays, since it can carry blocks no listing accounts for. A ZIP now moves only when its entries and directory cover every byte, and a ZIP or 7z member marked as a directory that holds data is reported and keeps its archive ([decision 0051](docs/decisions/0051-cleanup-of-7z-and-split-sets.md)).
- Review fixes for collections: a receipted recording, or a packet whose receipt records a detection, is no longer resumed or offered for ingest; identical tracks count by position; a packet that cannot resume is reported as retained; a resume starts only within the remaining `--max-total-bytes`, and `--max-recording-bytes` applies; collection files with trailing data or repeated keys are refused.
- Collections: a JSON file of exact selections (Archive files and recordings, Gutenberg EPUBs, arXiv versions). `collection status COLLECTION LIBRARY` and the MCP tool `collection_status` resolve each item against holdings offline; `collection acquire COLLECTION LIBRARY --output INTAKE` previews and, with `--apply`, acquires only what the library does not hold, one item at a time through the acquire service, resuming interrupted packets, leaving other existing packets untouched, and honoring `--max-total-bytes`. Running it again fetches only what is still missing ([decision 0050](docs/decisions/0050-collections.md)).
- An Archive evaluation lists the ISBN and Open Library edition and work keys the item declares, and import records valid ones on the source. `library related LIBRARY [--asset ID]` and the MCP tool `library_related` group holdings that share a stated work, edition, or version, versions of one arXiv paper, a narration, or files of one source item, each with its evidence; nothing is merged and titles are never compared. An ISBN-10 and its ISBN-13 relate as one edition, groups are paged, and a library holding a source identity needs this version or newer ([decision 0049](docs/decisions/0049-declared-identifiers-and-related-holdings.md)).

## 0.1.0-alpha.16

Sixteenth native prerelease. This remains an early library, not 1.0. One explicit arXiv paper version can be evaluated and acquired, MCP servers run long work as durable jobs that a terminal can read and cancel, split 7z and RAR volume sets are read whole, and recordings of one reading are related by narration.

- The volume sets, jobs, and arXiv provider were reviewed adversarially before release. A RAR comment can no longer stand in for 7-Zip's volume count, a split 7z must end exactly where its volumes end, and a set in a folder holding `!/` is reported as failed with every volume. A job cancelled from another process while queued does not start. A `Retry-After` from arXiv only delays later requests, one shared client holds one connection per host, and a versioned arXiv answer naming another version is refused.
- `evaluate arxiv:ID[vN]` resolves one arXiv paper version (an unversioned identifier to the latest), with its title, authors, dates, DOI, category, and a PDF length the host declares; the license arXiv records for the latest version is applied only to that version. `acquire arxiv:IDvN --file IDvN.pdf` downloads that PDF from export.arxiv.org, checked against the declared length, then assessed, scanned, and imported like any PDF. Every arXiv request keeps arXiv's pace of one every three seconds ([decision 0047](docs/decisions/0047-arxiv-selected-versions.md); issue 19).
- `nemalo jobs list`, `jobs get ID`, and `jobs cancel ID` read and cancel the durable jobs MCP servers started, from the same journals.
- `library ingest` reads split 7z sets (`NAME.7z.001`, ...) and RAR volume sets (`NAME.part1.rar`, ...) whole through their first volume. A split 7z is joined in order; a RAR set is read only when 7-Zip names it multi-volume with exactly the volumes found. The other volumes are reported as parts of the set, and a set with a missing volume is preserved ([decision 0046](docs/decisions/0046-archive-volume-sets.md)).
- `nemalo mcp serve --allow-write` runs long work as durable jobs: `library_rescan_start`, and with `--allow-network` `acquisition_start` for a selected Archive file or a whole recording, return a job ID at once, and `job_get`, `job_list`, and `job_cancel` read and stop them. Each job is journaled in the state folder and owned through an operating system lock, so its status, progress, and result outlive the call and the connection; the server finishes running jobs before it exits, and a job whose process ended is recorded as interrupted and can be resumed. Only Archive sources are accepted, a server shows only jobs inside its scopes, another process can cancel a job, jobs queued at disconnect do not start, and servers sharing a state folder share one bound ([decision 0045](docs/decisions/0045-durable-mcp-jobs.md)).
- An acquisition holds a lock named by its output folder, so two acquisitions, from jobs or terminals, never write one packet at once.
- `library recordings` relates recordings of the same reading. An audio source records a narration (`librivox:PROJECT`) only when its LibriVox project names the same Archive item back, and recordings in different formats of that reading are grouped under it; different readings of one text stay apart, and nothing is grouped by title. A held M4B is now a single-file recording, probed from its sample tables. A library holding a narration record needs this version or newer ([decision 0044](docs/decisions/0044-narration-identity.md)).
- The MP4 reader is `nemalo.mp4.samples/2`: only audio samples must lie inside media data. Cover art and chapter titles some writers place after the media data are noted, not findings, so such LibriVox M4Bs from Archive are no longer held in review.
- The installers ask for the release list again, up to three times, when it names no release, since GitHub's list has briefly answered with none while releases existed.

## 0.1.0-alpha.15

Fifteenth native prerelease. This remains an early library, not 1.0. Held cleanup copies can be purged once the library holds the same verified bytes, audio can be decoded in full by an installed FFmpeg, RAR and 7z archives are read through an installed 7-Zip, and every source can be searched at once.

- The RAR and 7z reader was reviewed adversarially before release. 7-Zip is now held to the handler the file's signature names, so a tar or cpio renamed `.rar` is refused; links stated by a `Mode` or link key, members without a CRC, and names that are also folders are not extracted; a hex attribute word no longer makes a book look like a folder; and one archive has a single extraction deadline and at most 1,000 runs. A `--decode` or `--epubcheck` that does not apply is named in the report.
- `search QUERY --source all` searches Open Library and Internet Archive at once, and the local Gutenberg catalog when one is configured, each by relevance in its own section with its own status. A source that fails or is not configured says why without hiding the others, results are not ranked against each other, and the search fails only when no source completed.
- `library ingest` imports the supported members of RAR and 7z archives when 7-Zip (`7zz`, `7z`, or `7za`) is installed. The archive is copied to a private snapshot and listed; every member is accounted for as in a ZIP, and each eligible member is extracted alone, by exact name with wildcards off, to standard output into a flat staging name, and must match its listed size and CRC. No member name is ever a path on disk, and 7-Zip never waits for a password. Without 7-Zip the archive is preserved as before ([decision 0043](docs/decisions/0043-rar-and-7z-through-7-zip.md)).
- `check FILE --decode` decodes an MP3, M4B, or M4A in full with the installed FFmpeg and FFprobe and reports the decoded duration and decoder errors beside the probed timing. Decoder errors, a requested decode that did not pass, or a decoded duration more than a second from the encoded one are review findings. FFmpeg reads only the snapshot through the file protocol and a fixed demuxer, so no network request or playlist is followed. `doctor` lists both tools ([decision 0042](docs/decisions/0042-optional-ffmpeg-decode.md)).
- A check's five-minute bound now covers only its snapshot and inspection; a scan, EPUBCheck, or decode keeps its own deadline by size, so a large file is not cut short.
- `library purge LIBRARY --cleanup ID` previews, and with `--apply --plan DIGEST` removes, a completed cleanup's held copies whose bytes are a current holding with verified stored bytes, and held ZIPs whose every member is. A packet's receipt and intent, copies that changed, and copies the library cannot vouch for stay. The purge is journaled with a manifest and resumes after an interruption; restore reports a purged file as purged. `--stop` closes a purge that cannot finish, and the TUI previews one with Ctrl+P in State. A library with a purge record needs this version or newer ([decision 0041](docs/decisions/0041-purge-held-sources.md)).
- `check --epubcheck` was validated against the official EPUBCheck 5.4.0: valid Gutenberg EPUBs pass, and a document with an unknown element reports `RSC-005` errors with their location and stays in review ([decision 0038](docs/decisions/0038-optional-epubcheck.md)).
- Restore, cleanup, and check open every folder of a held copy without following links. Before, a link planted in a cleanup's holding area that led into `assets/` let restore remove a stored asset after returning its bytes.

## 0.1.0-alpha.14

Fourteenth native prerelease. This remains an early library, not 1.0. M4B audiobooks and M4A tracks join the checked formats, single-file audiobooks up to 2 GiB can be acquired and held, a ClamAV limit is no longer mistaken for a detection, and EPUBs with only font obfuscation are readable.

- The MP4 sample table reader was reviewed adversarially before release. A second sample size table that crashed the probe, protection hidden by box order or a second sample description, empty samples that skipped placement checks, a chapter track that could allocate over 100 MiB from a tiny file, and a 64-bit size overflow are fixed, each with a regression test.
- M4B audiobooks and M4A tracks can be checked, imported, ingested, acquired from Archive with `acquire --file`, and listed with `library list --format m4b`. Their sample tables are read without loading or decoding audio, and every sample must lie inside the file: a cut-short download, misplaced or overlapping samples, a fragmented file, or protected audio keeps it in review, and protection is an access restriction. With a clean scan such a file is `checked`. `content units --representation mp4-audio/1` and the TUI list the timing and the QuickTime or Nero chapters; a player gets the file through `library open`. A library that holds an M4B or M4A needs this version or newer ([decision 0039](docs/decisions/0039-mp4-audio-samples.md)).
- An M4B or M4A may be up to 2 GiB; EPUB, PDF, and MP3 stay at 256 MiB because they are parsed in memory. `acquire --file` defaults its budget to the format's limit. A check refuses a snapshot that would not fit in the temporary folder. An acquisition allows five minutes plus a second per 512 KiB, and a download that delivers nothing for 60 seconds is continued from its bytes like a dropped connection. `mp4-audio/1` reads large holdings at offsets and then hashes the whole file. A library that holds a file over 256 MiB needs this version or newer ([decision 0040](docs/decisions/0040-large-mp4-audio-and-scan-limits.md)).
- ClamAV now scans files up to 2 GiB, with a deadline that grows with file size. A ClamAV limit alert such as `Heuristics.Limits.Exceeded.MaxScanSize` names bytes it did not scan: it is now an incomplete scan, never a detection, so a large file is no longer quarantined as malware for exceeding a limit. A real detection beside it is still a detection.
- `check` shows MP3 frame and MP4 sample facts in its text output.
- The `library import` help and rescan limitations no longer say that only EPUBs can be checked or rescanned.
- `check --epubcheck` runs an installed EPUBCheck on an EPUB and lists its errors and warnings. Reported errors, or a requested check that could not run, are review findings. It is optional and not part of the checked policy ([decision 0038](docs/decisions/0038-optional-epubcheck.md)).
- `library list LIBRARY --language TAG` and MCP `library_list` `language` keep holdings that declare that base language, so `en`, `en-US`, and `eng` are one. Free text such as `English` is never read as a language, and holdings without a readable tag are counted ([decision 0037](docs/decisions/0037-base-language-filter.md)).
- An EPUB whose only encryption is font obfuscation is no longer held for review or refused by content access; the obfuscated fonts are recorded. Other encryption, a wrapped key, an unreadable declaration, or an Adobe or LCP DRM license still keeps it in review, and an obfuscated font is never served as a resource ([decision 0036](docs/decisions/0036-epub-font-obfuscation.md)).

## 0.1.0-alpha.13

Thirteenth native prerelease. This remains an early library, not 1.0. PDFs and MP3s can now be checked, agents can import through a previewed plan, LibriVox readers are credited per track, and downloads, catalogs, and the journal recover from interrupted transfers, full disks, and torn writes.

- Evaluating a LibriVox item on Archive reads the LibriVox project its call number names. When the project names the item back and its sections match the tracks by count and declared length, each track is credited with its readers; acquisition and ingest keep them, and `library recordings` lists them as narrators. Otherwise no reader is credited ([decision 0035](docs/decisions/0035-librivox-readers.md)).
- A file request that fails before any body, with a transport error or a 429 or 5xx answer, is sent again up to three times in all, honoring a short `Retry-After`.
- Tests cover every writer refused as busy while another holds the library, racing imports, and an unwritable store or control folder that recovers once writable.
- `nemalo mcp serve --allow-write` adds `library_import_preview` and `library_import_apply`. An apply stores only the plan digest its preview returned and writes nothing when the source, its assessment, the scan choice, or the library changed. Sources are never changed, and the plugins stay read-only ([decision 0034](docs/decisions/0034-mcp-import-tools.md)).
- `library import` previews show a plan digest, and `--apply --plan DIGEST` applies only that plan.
- `check` and import index an MP3's frames. A declared frame count the file does not hold (a cut-short download) or bytes between frames (damage) keep it in review; a trailing nonstandard tag or a differing ID3 length is noted. A cleanly indexed MP3 with a clean scan is now `checked`, and `library rescan` promotes MP3s already held. A library that holds a checked MP3 needs this version or newer ([decision 0033](docs/decisions/0033-mp3-checked-policy.md)).
- `check` and import examine a PDF's active content: JavaScript, launch and automatic actions, embedded files, form submission, remote references, rich media, and XFA forms are findings that keep it in review, and web links are counted. A PDF whose page tree and active content were fully examined and clean, with a clean scan, is now `checked`, like an EPUB. `library rescan` promotes such PDFs already held. A library that holds a checked PDF needs this version or newer ([decision 0032](docs/decisions/0032-pdf-checked-policy.md)).
- A library journal line cut short by a crash or a full disk no longer blocks the library. Its complete lines are read, `library check` reports the interrupted write, and the next library write removes the unterminated bytes. A longer unterminated tail is kept for review. Tests fill the disk during import copies, journal lines, cleanup copies, and exports, and fail the scanner, and each leaves the sources intact and the library recoverable.
- Downloads, imports, ZIP extraction, cleanup copies, and exports check the free space of their folder first and refuse a write that would leave less than 64 MiB free, naming the needed and free sizes. An import is refused before any journal entry. Free space that cannot be read does not refuse a write.
- A transport failure is judged by its cause: a reset, refused, or unreachable socket, a temporary DNS failure, a timeout, or a body that ended early. A refused destination or other request error is not retried.
- Search, evaluation, and the Gutenberg bibliographic record retry a dropped connection, a timeout, or HTTP 429, 500, 502, 503, or 504 at most twice, inside the same 30-second deadline and one-request-per-second pacing, after any `Retry-After`. A wait past the deadline ends the request and says when to try again. A 404 or other error is not retried, and the record is never fetched from another host.
- `catalog fetch gutenberg` continues a download that gutenberg.org drops partway, up to three times, with a `Range` request bound to the first response's strong validator. It accepts only exactly the missing bytes of the same length, starts again when the catalog changed, and still checks the declared length and every row.
- `acquire` continues its own transfer after a dropped connection, up to three times, when the source sent a strong validator. Each continuation is a checked resume of only the missing bytes, and the whole file is verified at the end. A refused resume, a mismatch, the budget, or cancellation stops at once and keeps the packet. Recording tracks continue in place the same way.
- The installers retry a dropped connection, a timeout, or a server error up to three times, starting each try from an empty file. A missing release says so at once, and a failure to write the download says the temporary folder may be full instead of reporting a download failure. A temporary folder that cannot be created falls back to `~/.cache` or `%LOCALAPPDATA%`.

## 0.1.0-alpha.12

Twelfth native prerelease. This remains an early library, not 1.0. Closes issues 20 through 35.

- The reader flows a hard-wrapped EPUB paragraph, such as every Project Gutenberg paragraph, as one paragraph. Only a block end gets a blank row and the paragraph hold in word mode. An explicit line break, as in verse, keeps its line. `epub-text/2` and `epub-nav/1` ranges now list these structural line feeds in `breaks`; the text, its offsets, and representation identities are unchanged. Closes issue 23.
- Gutenberg search results show the catalog's Type and Issued date, such as `Text, issued 1998-06-01` or `Sound, issued 2007-02-03`, in the CLI and TUI, and as `media_type` and `issued` in JSON. A text and its recordings no longer look identical. Closes issue 24.
- Each step of the first-book path names the next command: a local Gutenberg evaluation names `evaluate --files`, an evaluation names `acquire` for the first eligible EPUB or PDF, an acquisition names `library import`, a preview names `--apply`, and an import names `read`. Scan guidance follows these lines instead of replacing them. The README quick start goes from the catalog to the reader. Closes issue 25.
- Suggested commands quote source file names and paths for the shell. They were Go-quoted before, and a shell expands `$(...)` inside double quotes. A name with a control or format character is not offered.
- Errors print as one plain sentence. A refused request names the host and the reason instead of the encoded URL, for example `nemalo: request to archive.org failed: archive.org resolved to a non-public address`, and a missing library folder says so. `--json` keeps the full message. Closes issue 27.
- `library init`, `acquire`, `library import`, and `catalog fetch` print only the error when they fail before doing anything, instead of a block of empty values. An interrupted catalog download says how much arrived and that nothing was kept. Closes issue 28.
- Human output reads as labelled values and sentences instead of Go values. Empty values and lists are left out, status tokens read as words (`Status: review (unscanned)`, `File rights: not resolved at file level`), and source text is shown without Go quotes on one row with control characters escaped. This covers search, evaluate, acquire, check, content, bookmarks, and every library command. `--json` keeps every field and exact token. Closes issue 29.
- The terminal interface follows the same rules: browser rows, details panes, review screens, and status messages show file names and paths without Go quotes, status values as words, and no empty lists. A path is quoted only when spaces or control characters would otherwise hide its exact value. Closes issue 30.
- `nemalo doctor` prints a short text summary by default: paths, configuration, the recommended scanner and its signature age, the descriptor bridge, optional tools, and a `Next:` line for each thing that needs action. `doctor --json` is the full report, which was the default before. Closes issue 31.
- `p` and `s` in TUI Read open the reader exactly where the last successful excerpt began, including after a continuation, instead of at the start of its unit. An excerpt of a declared entry reopens that entry there, so pages stop at its declared end even after it crossed into another spine file. A failed read does not move the place, and another asset or root never reuses it. At the end of a declared entry, a page key says so instead of waiting for another range. Closes issue 20.
- `f` in the reader lists the page's EPUB links with their source labels and status. A source-declared note reference is marked as one; other links are not presumed to be notes. Enter reads one note's bounded part, or the paragraph a `[1]` marker starts, and Escape returns to the exact passage, paused in word mode. Long notes continue within their part. Notes nest four deep. External, missing, and off-spine targets are never followed, and bookmarks stay with the passage. `epub-structure/1` ranges now carry the same `breaks` as `epub-text/2`. Closes issue 21.
- List an EPUB's declared page labels, such as `iv` or `42`, with `content pages`, MCP `content_pages`, or `g` in TUI Read, and read from one with `epub-page-list/1` by reference or exact label. A duplicate label lists its candidates. The package's stated pagination source is shown, or `unknown`. A page end is given only when the next marker follows. Page labels are the book's own markers, never generated, and the table of contents and its ids are unchanged ([decision 0031](docs/decisions/0031-epub-page-list.md)). Closes issue 22.
- A failed `content` request prints only the error.
- `nemalo help` is a one-screen overview grouped by find, acquire, check, keep, and read. `nemalo help COMMAND` and `COMMAND --help` print that command's options and limits, with today's text. Help wraps to a terminal narrower than 80 columns. `nemalo --version` and `nemalo version` print the version and, when built from a source checkout, the commit. Closes issue 34.
- The TUI uses the terminal's own ANSI colours: blue accent, green for done, yellow for work and attention, red for failures and detections, and dim context. It no longer emits truecolor values that assumed a dark background, which left about 2:1 contrast on light themes. The status line's colour comes from the operation's result, not from words in the status. Closes issue 32.
- TUI mode labels never split from their number: when every name does not fit, the other modes show their numbers and the selected mode keeps its name. Field hints and context lines end at a whole word, the footer is one line when it fits, and the reader no longer repeats the unit label above its own heading. Closes issue 33.
- Closing the reader prints one line, such as `Closed at Chapter One, 12% through. Nothing about this reading was recorded.`, naming the book's declared contents entry, or the unit, and how far through it is. The exact unit and byte stay in the full report and in `nemalo read`'s return command. A saved bookmark is mentioned only when it is at another place. Closes issue 35.

## 0.1.0-alpha.11

Eleventh native prerelease. This remains an early library, not 1.0.

- The installers choose the highest semantic version among published releases. The GitHub releases list does not put that version first. After 0.1.0-alpha.10 was published, the first list item was still v0.1.0-alpha.9
  ([decision 0013](docs/decisions/0013-release-installer-scripts.md)).
- List editions of one Open Library work, then read one edition. A page whose entry count differs from the requested window is refused. When that edition's `ocaid` names one Archive item, evaluation continues with that item's existing metadata. Search does not read work-level Archive identifiers. Acquire remains `archive:ITEM`. There is no Open Library edition MCP tool
  ([decision 0030](docs/decisions/0030-openlibrary-selected-edition.md)).

## 0.1.0-alpha.10

Tenth native prerelease. This remains an early library, not 1.0.

- Acquire one selected Project Gutenberg EPUB. `evaluate gutenberg:NUMBER --files` fetches one bibliographic record from gutenberg.pglaf.org and lists the files that record names. `acquire gutenberg:NUMBER --file NAME --output NEW_DIRECTORY` downloads one of `N.epub.noimages`, `N.epub.images`, or `N.epub3.images` into untrusted intake. The record has no checksum. A declared extent that differs from the response length is recorded, and the transfer checks the response length. Other formats are listed and not downloaded. Search still reads only the local catalog. There is no Gutenberg MCP tool
  ([decision 0029](docs/decisions/0029-gutenberg-selected-epub.md)).
- An interrupted download is offered for `--resume` only when that response had a strong validator. A packet that has partial bytes and no strong validator says to start a new output.

## 0.1.0-alpha.9

Ninth native prerelease. This remains an early library, not 1.0. Closes issues 11, 12, 13, 14, 15, 16, 17, and 18.

- Search and evaluate a local Project Gutenberg catalog. `catalog fetch gutenberg --output FILE` downloads the official CSV. Search does not contact gutenberg.org, ranks results on this machine, and lists no ebook files
  ([decision 0028](docs/decisions/0028-gutenberg-local-catalog.md)).
- A failed search prints only the error. It does not print a zero-match page.
- `library cleanup` on a missing library prints only `library not found`. It does not print a preview.
- `nemalo read` names a missing `--root` or `--asset`. For a managed library, `--root` is the catalog folder. `read` and `tui` require an interactive terminal and say so, with exit 2, before starting the interface.
- The reader footer keeps `q close` visible at 80 columns. `?` lists the keys. A resize, or a switch between pages and one word at a time, repaints the whole frame.
- In Evaluate, `a` or `d` with an empty Intake field asks for a folder (Ctrl+N) before review.
- An acquisition that fails before any bytes says to start a new output. `--resume` is offered when the packet has a transfer record and partial bytes.

## 0.1.0-alpha.8

Eighth native prerelease. This remains an early library, not 1.0. Closes issues 9 and 10.

- `nemalo read` and `p` or `s` in TUI Read open a full-screen reader for EPUB
  and PDF text: pages sized to the terminal, or one word at a time with
  sentence pauses, starting paused at 250 words per minute. Omitted images and
  tables are shown where they were omitted, PDF pages keep their lines, and an
  unreadable unit stops reading until you choose the next one. Closing prints
  the position to return to and does not save a bookmark
  ([decision 0025](docs/decisions/0025-terminal-reading.md)).
- `content navigation` and `--representation epub-nav/1` (MCP
  `content_navigation`; TUI Read: `c`) list source-declared EPUB 3 nav and
  EPUB 2 NCX entries. A read follows one entry until the next entry that is
  not nested inside it. A short result and the end of a spine file are
  reported separately from that end. When both sources exist, a read names
  `--navigation nav-doc` or `--navigation ncx`. `epub-structure/1` heading
  parts are unchanged
  ([decision 0026](docs/decisions/0026-epub-navigation-and-chapter-selection.md)).
- Explicit bookmarks: `bookmark enable`, `disable`, `list`, `save`, `note`,
  `delete`, `export`, `import`, and `resume`. Enable creates
  `.nemalo/bookmarks.jsonl`. Reading, repeating, continuing, and closing write
  nothing. In the TUI, `m` saves the place on screen and `'` restores it. A
  changed or missing file is reported and is not remapped. A note stays
  separate from the book. There is no reading acknowledgement. MCP does not
  read or write bookmarks. A library with the bookmark file needs this version
  or newer to open its control directory
  ([decision 0027](docs/decisions/0027-explicit-local-bookmarks.md)).

## 0.1.0-alpha.7

Seventh native prerelease. This remains an early library, not 1.0.

- `library check LIBRARY` (MCP `library_check`; TUI Audit with a library folder)
  verifies the whole library read-only: stored and held bytes, untracked and
  partial store files, unfinished operations, and review counts with next steps.
- TUI: `d` on an interrupted packet reviews resuming it, and after a cleanup F9
  previews restoring it.
- Resume an interrupted download: `acquire ... --file NAME --output PACKET
  --resume` requests only the missing bytes with `If-Range` on the strong ETag
  recorded before the body, accepts only exactly that range, and verifies the
  whole file as one transfer. Interrupted recording tracks resume in place.
- Recoverable source cleanup: `library cleanup LIBRARY FOLDER` previews every
  entry with a disposition, reason, and plan digest; `--apply --plan DIGEST`
  moves into `<library>/cleanup/<id>/` loose files whose exact bytes the library
  holds (stored bytes verified), intake packets whose receipt binds held content,
  and ZIPs whose every member is held, through verified copies and a journal that
  resumes after an interruption. `--stop` closes a cleanup that cannot finish.
  `library restore` returns every held file without replacing anything, and
  `library restore --stop` closes a restore that cannot finish. TUI: F9
  in State. Nothing is purged. Libraries with a cleanup journal cannot be opened
  by earlier prereleases.

## 0.1.0-alpha.6

Sixth native prerelease. This remains an early library, not 1.0. Closes issues 1 and 2.

- PDF pages and text: `--representation pdf-text/1` reads one unit per physical
  page with its printed page label, text in content-stream order, and located
  gaps for images, font codes without a Unicode mapping, and pages without a text
  layer, in CLI, JSON, TUI Read, and MCP. `check` walks the page tree and records
  page count and labels; a file with only PDF header and end markers now needs
  review. Uses `github.com/digitorus/pdf` for object parsing, behind budgets.
- `content locate --text` (MCP `content_locate`) finds exact quotations in EPUB
  or PDF text and returns page or unit, offsets, and a cursor that reads each back.
- Recording manifests: `library recordings LIBRARY [--source archive:ITEM]`,
  the MCP `library_recordings` tool, and `t` in TUI Holdings list each held
  recording's tracks in declared order with exact assets, missing positions, and
  conflicts. With a source, each track's stored bytes are verified and indexed
  for probed duration, chapters, and offset in the recording. Track titles and
  declared durations are now recorded at acquisition.

## 0.1.0-alpha.5

Fifth native prerelease. This remains an early library, not 1.0.

- List and search a managed library: `library list LIBRARY` with `--query`,
  `--format`, and `--status checked|review`, in the TUI Holdings operation, and
  through the MCP `library_list` tool.
- Open a holding in the default application or `--app` through a verified
  read-only copy in the Nemalo handoff folder (TUI: `o`), and export a verified
  copy with `library export`. Stored files are never handed to readers, because
  some readers write annotations into the files they open.
- Ingest a whole folder with `library ingest LIBRARY FOLDER [--apply] [--scan]`
  (TUI: F8 with a folder): loose EPUB, PDF, and MP3 files, intake packets, and
  ZIP members extracted into private staging with path, size, ratio, and CRC
  checks, through the same import and journal. Every entry is accounted for and
  the folder is never changed. Archive members are recorded as
  `ARCHIVE!/MEMBER`, which earlier prereleases cannot read. An intake packet
  without a receipt is listed, never imported as a loose file.
- Acquire a whole LibriVox recording with `acquire archive:ITEM --recording
  FORMAT --output DIRECTORY` (TUI: `a` in Evaluate). Evaluation lists each
  item's recordings in declared track order. Tracks become ordinary intake
  packets with shared checks, scans, and quarantine; ClamAV scans them in
  batches; `recording.json` is written last. `--resume` keeps completed tracks
  and refetches the rest under new names. Ingest records each track's place,
  shown as "Track N of M", which earlier prereleases cannot read.
- Install the read-only MCP tools as a Claude Code plugin
  (`/plugin marketplace add blisspixel/nemalo`, then
  `/plugin install nemalo@nemalo`), which asks for the library folder, or as an
  Agent Plugins 1.0 package. Both include a skill that explains the tools and
  how to read their evidence. The `nemalo` executable must be on PATH.

## 0.1.0-alpha.4

Fourth native prerelease. This remains an early library, not 1.0.

- Index MP3 frames with `--representation mpeg-audio/1`: probed sample rate,
  channels, frames, and encoded duration, kept separate from declared info-frame
  counts, encoder delay and padding, and ID3 text, length, and chapters. Reads
  return whole-frame byte ranges with decoder preroll and exact sample bounds,
  over CLI, JSON, MCP, and TUI Read. Nothing is decoded. Ranges decoded bit-exactly
  against a whole-file FFmpeg decode on synthesized and LibriVox files.
- Validated the MCP server with a real host: Claude Code on Windows read one
  book, switched to another, and resumed the first from its continuation at
  the exact byte where it stopped.
- Check MCP tool paths lexically against the scopes before any filesystem
  access, so a Windows UNC path cannot trigger a network connection, and
  return one error for missing and outside paths. Skip a configured folder
  that does not exist yet instead of refusing to start.
- Bound audio indexing memory: compact per-frame records and ID3 text capped
  before decoding.

## 0.1.0-alpha.3

Third native prerelease. This remains an early library, not 1.0. It resolves
issues 3 through 8.

- Accept Internet Archive downloads redirected to `eu` data nodes, beside `us`
  and `ca`. Arbitrary archive.org subdomains stay refused.
- Keep Archive search in Archive's relevance order instead of identifier order.
  `--sort identifier` pages repeatably and `--sort downloads` ranks by declared
  download count, a popularity signal. Each result names its order. TUI F3
  cycles the Archive order.
- Make `epub-text/2` the default text representation. Illustrated documents stay
  readable; each omitted image, table, MathML, SVG, or media element is located
  as a gap with its source alt text, and human output marks it in place.
  `epub-text/1` and its continuations are unchanged.
- Scan each download once during `acquire` with the installed scanner. A
  detection is quarantined under the Nemalo state directory (or deleted with
  `on_detection`), never imported, and the item's other files and a search are
  offered. `--no-scan` opts out. A clean acquisition scan bound to the same
  SHA-256 lets the packet import as checked without scanning again.
- Add `library rescan LIBRARY [--apply]` and TUI Ctrl+R in State. It scans review
  EPUBs that lack scan evidence, with ClamAV batches that load signatures once.
- Report signature age and the platform install command from `doctor`, and end
  unscanned `check`, `import`, and `acquire` output with one next step.
- Create missing parent folders for `acquire --output`, only after evaluation
  succeeds.
- Add `scripts/install.sh` and `scripts/install.ps1`, which verify release
  archives against `SHA256SUMS`.
- Add `nemalo mcp serve`, a read-only MCP server on stdio built on the official
  Go SDK v1.8.0. Its tools call the same services as the CLI and return the
  same data; catalog tools need `--allow-network`, and every path must resolve
  inside a configured scope.
- Fix the journal check that reported an interrupted scan-upgrade import of an
  already held EPUB as corrupt state.

Validation for this release: on the six Project Gutenberg EPUBs from issue 5,
`epub-text/1` read 28 of 54 units and `epub-text/2` read all 54, locating 84
omissions. Live acquisitions were scanned by Defender on Windows and by ClamAV
1.5.4 on Ubuntu; Treasure Island was delivered from an `eu` node. On Ubuntu,
six sequential `check --scan` runs took 59.5 seconds and one batched rescan of
seven files took 17.5 seconds, detecting an EICAR test member. Libraries with
rescan journal events cannot be opened by earlier prereleases. The Defender
detection pattern is covered by synthetic tests, not a live detection.

## 0.1.0-alpha.2

Second native prerelease. This remains an early library, not 1.0.

- Import one assessed EPUB, PDF, MP3, or completed intake packet into an
  initialized library. Preview writes nothing. `checked` requires a passed EPUB
  check and a recorded no-detection scan. Other assessed files stay in review.
- Record an explicit ISBN, DOI, or Open Library work or edition key from an
  EPUB package as evidence. Each file stays under its SHA-256.
- Report the directory descriptor bridge and the recommended scanner from
  `doctor`. Windows uses Microsoft Defender. Linux and macOS use ClamAV.
- Compare a publisher SHA-256 with `--expected-sha256` when one is supplied.
  Nemalo also records the SHA-256 it measured.

A scan is recommended for a file you did not produce. Nothing is installed
automatically. A matching checksum shows the bytes are the published bytes.
A no-detection scan and a matching checksum do not prove the file is free of
malware. Cleanup, archive extraction, download resume, recording and track
identities, PDF page trees, decoded audio, and MCP remain planned. Hosted CI
runs on Ubuntu, macOS, and Windows. It does not boot Omarchy.

## 0.1.0-alpha.1

First native prerelease. This is an early library/discovery foundation, not 1.0.

- Search Open Library and Archive, evaluate source assets, and acquire selected
  Archive EPUB/PDF/MP3 files into provenance-bearing untrusted intake packets.
- Inventory, assess with optional antivirus, snapshot, browse, audit, and initialize
  library control state through shared Go CLI/TUI services.
- Retrieve source-bound EPUB text, structured parts, notes, source XML, and local
  resource bytes with exact continuation and explicit extraction gaps.
- Bind filesystem validation to held capabilities, bound compressed parser work,
  and preserve safe, parse-equivalent multilingual terminal output.
- Package cgo-free Linux, macOS, and Windows archives with SHA-256 checksums.

Resumable downloads, archive extraction, checked publication, mutable managed
catalogs, complete audiobook management, and MCP remain planned. See the
[roadmap](ROADMAP.md) and [capability limits](docs/DEVELOPMENT.md).

## Development history

### Since 0.1.0-alpha.1

### Development: scanner and checksum recommendations

- Recommend Microsoft Defender on Windows and ClamAV `clamscan` on Linux and
  macOS for a file the user did not produce. `doctor` names that default.
- A publisher SHA-256 confirms the published bytes. Nemalo records its own
  SHA-256 either way. Neither a checksum nor a no-detection scan proves the
  file is safe. Nothing is installed automatically.

### Development: Omarchy desktop compatibility

- Report the Linux `/proc/self/fd` and macOS `/dev/fd` directory bridge from
  `doctor`. Import still hard-links only inside the library directory.
- Treat `foot`, Ghostty, Alacritty, and Kitty as truecolor. Leave tmux at
  256-color. Omarchy 4.0.4 does not bind Nemalo's Alt, F2 through F8, or Ctrl+C keys.
- The checked Linux build remains the cgo-free `linux/amd64` archive. Hosted CI
  runs that archive on Ubuntu. See [development notes](docs/DEVELOPMENT.md).

### Development: EPUB package identifier evidence

- Record an explicit ISBN, DOI, or Open Library work/edition key from an EPUB
  package as identity evidence. Preserve other package identifiers as unassigned.
- Keep each file under its SHA-256. The same identifier on two files does not
  merge them, and a title match assigns no work, recording, or track.
- Leave PDF and MP3 bibliographic identifiers unread. See
  [decision 0010](docs/decisions/0010-bibliographic-identity.md).

### Development: managed single-file import

- Copy one assessed EPUB, PDF, MP3, or completed intake packet into an initialized
  library through shared services, CLI, and a terminal review that does not scan.
- Sync import intent before the copy. Resume only one trailing intent for the same
  source. Retain corrupt journals. A later import without a scan does not downgrade
  a checked EPUB.
- Store the bytes beside `.nemalo` by SHA-256. Leave a conflicting file in place,
  reject an unexpected extra hard link, and reject a store parent that is not a
  real directory before writing a new intent.
- Record `checked` only when limited EPUB checks passed and the scan reported no
  detections. Name a scan-only gap with the scanner status. Managed reads use the
  256 MiB import cap; snapshot reads stay 32 MiB.
- Work and edition identity, archive extraction, download resume, folder cleanup,
  and a full checked-library policy remain outstanding. See
  [decision 0009](docs/decisions/0009-managed-epub-import.md).

### Before 0.1.0-alpha.1

### Development: structured EPUB access

- Address the EPUB scope of issues 1 and 2 with versioned capability negotiation,
  paragraphs/headings/stanzas, inherited language/direction, and source-bound parts.
- Resolve local note/backlink references and disclose cycles without auto-following.
  Expose source alt text, captions, and bounded image-member bytes with parent/member
  hashes. Remote resources remain unfetched; missing alt text stays explicit.
- Locate unsupported tables/math/hidden content as gaps instead of flattening them.
  Add bounded original XHTML/XML retrieval, preserving table spans and source math.
- Add TUI unit selection, exact repeat/continuation, explicit-root staging from
  Holdings, escaped source lines, and session navigation without consumption records.
- Keep the existing plain-text representation available. Add hostile-boundary,
  range-coverage, note/resource, CLI/TUI recovery, and repeated-retrieval tests.
  PDF/OCR, decoded audio ranges, MCP, and external caller validation remain open.

### Development: source-bound EPUB access

- Address the first Nemalo-owned slice of issue 1 with offline content units/read
  CLI/JSON over shared services and the existing bounded EPUB container parser.
- Revalidate exact source bytes; add versioned locators, deterministic continuation,
  explicit unsupported document gaps, bounded errors, and no reading-history writes.
- Test more than 300 actual retrieval cycles, retries, interleaved works, Unicode,
  stanza breaks, stale references, changed/missing content, limits, and cancellation.
  MCP, paper/audio ranges, and external caller integration remain open.

### Development: managed library initialization

- Add explicit library init/status in CLI/JSON/TUI, stable library identity,
  native reader/writer locks, synced initialization intent/completion, and
  idempotent completion/resume without modifying existing content.
- Retain/reject corrupt, oversized, linked, replaced, or unexpected control state;
  add cross-process crash tests and TUI write-scope review.
- Promote the existing x/sys module to direct use without new modules or versions.
  Content publication, mutable catalogs, and import/cleanup recovery remain planned.

### Development: terminal browsing and documentation

- Replace teal with an adaptive indigo/neutral palette; retain no-color focus markers.
- Add selectable search/holdings, wide-terminal evidence panes, focused details,
  complete reports, and staged Archive evaluation without automatic requests.
- Prevent wide-character report overflow with grapheme-aware wrapping and test
  odd/even terminal widths, focus isolation, selection, and evidence preservation.
- Shorten the README; move the full product contract and terminal guide into linked docs.

### Development: validation and terminal usability

- Revalidated the external collection and added four resources using Gutenberg,
  Archive, and arXiv; retained all source files and per-asset evidence outside Git.
- Fixed Archive pagination using documented page indexes, with actual document-ID
  tests and bounded adjacent-page handling for unaligned offsets.
- Added direct/reverse TUI navigation, per-operation drafts/results, search/holdings
  pagination, labeled forms, explicit read-budget selection, and adaptive color.
- Added CLI/TUI exact filename-format filters and title-first holdings; EPUB-only
  views exclude receipts without claiming filename validity.
- Added real-data rendered TUI images and updated validation limits/evidence.

### Development:

- Establish Nemalo as an Apache-2.0 Go CLI/TUI application, with no Python prototype
  in the repository. Use `nemalo` as the command/integration identifier.
- Add shared read-only inventory with explicit roots, link skipping, bounds,
  optional SHA-256 hashes, and incomplete-result reporting.
- Add bounded Open Library discovery with stable IDs, paging, cancellation,
  rate control, checked redirects, and clear discovery-only availability.
- Add strict configuration/path precedence, capability reporting, and schema-versioned
  JSON results. CLI and TUI use the same application services and safe formatting.
- Pin Go and verification tools; add offline tests and a native three-OS CI workflow
  enforcing formatting, compilation, vet, Staticcheck, 80% coverage, race checks,
  vulnerability checks, and cgo-free executable smokes.

- Add a Go-only real-resource collection harness with a 100-resource manifest,
  13 languages, complete exposed audiobook track sets, bounded container checks,
  per-asset receipts, and rehashing without overwriting changed files.
- Exercise all 100 selected resources outside the checkout, including 118 audiobook
  tracks. Record container/hash evidence and an external Windows Defender scan;
  retain incomplete-attempt evidence and distinguish unperformed checks.
- Add shared file health checks in CLI/TUI: actual signatures, EPUB structure,
  measured text/documents, expected size/hash comparisons, and review findings.
- Add explicit installed antivirus adapters with bounded private snapshots,
  no remediation, hash revalidation, and unsuccessful unknown/missing scan states.
- Add portable immutable byte-identity snapshots, exact duplicate locations,
  optional historical EPUB/file-health metadata, literal holdings search, and
  explicit-root preservation audits in CLI/TUI. Save complete catalogs exclusively
  outside source roots without replacing existing outputs or mutating content.
- Add Internet Archive discovery and item/file evaluation alongside Open Library,
  with source-declared rights, access restrictions, candidate assets, and explicit
  unverified DRM/file-rights states through CLI/TUI and structured output.
- Consolidate provider metadata HTTP handling with bounded JSON, checked direct
  destinations/redirects, cancellable pacing, and Retry-After handling.
- Update the tagline to "Find knowledge. Care for it. Realize its potential."

These entries record development history. The version sections above are the release notes. Current capability limits are documented in docs/DEVELOPMENT.md.
