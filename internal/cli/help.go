package cli

import (
	"io"
	"os"
	"runtime/debug"
	"sort"
	"strings"

	"github.com/charmbracelet/x/term"
)

// overview is the one-screen help, grouped by the path a book takes.
const overview = `Nemalo VERSION
Knowledge is power.

Usage: nemalo <command> [options]

Find      search QUERY, evaluate ID, catalog fetch gutenberg
Acquire   acquire ID --file NAME --output NEW_DIRECTORY,
          collection diff, collection status, collection acquire
Check     inspect DIRECTORY, check FILE [--scan]
Keep      library init, import, ingest, list, audit, check, cleanup,
          restore, rescan, open, export, recordings, related, snapshot,
          status
Read      read LIBRARY --root LIBRARY --asset sha256:HASH,
          content units, navigation, pages, read, locate, bookmark
Tools     tui, doctor, mcp serve, jobs, version

First book: catalog fetch gutenberg, search, evaluate --files, acquire,
library init, library import --apply, read. Each step prints the next.

nemalo help COMMAND shows a command's options and limits.
Shared options: --json, --config FILE, --library DIRECTORY, --review DIRECTORY
Exit codes: 0 success, 1 operation failed or incomplete, 2 invalid usage.
`

// commandHelp is each command's usage, options, and limits.
var commandHelp = map[string]string{
	"tui": `nemalo tui
  Open the interactive terminal interface. It needs a terminal and has no
  --json. Keys are listed on screen and in docs/TUI.md.
`,
	"doctor": `nemalo doctor [--json]
  Report paths, configuration, the recommended scanner and its signature age,
  the directory descriptor bridge, and optional tools, with a Next line for
  each thing that needs action. --json is the full report. Nothing is
  executed or installed, and tool availability is not scan evidence.
`,
	"search": `nemalo search QUERY [options]
  Search a selected catalog. Open Library and Internet Archive use the
  network. Gutenberg reads a local catalog file and does not query the
  website.

Options:
  --source openlibrary|archive|gutenberg|all (default openlibrary)
  --limit 10, --offset 0

--source all searches every source at once by relevance, each in its own
section; one source's failure does not hide the others', and Gutenberg is
searched only when a local catalog is configured. Results are not ranked
against each other. Sort and page one source at a time.
  --sort relevance|identifier|downloads (identifier and downloads are Archive
         only; Gutenberg also accepts identifier)
  --catalog FILE   absolute pg_catalog.csv for Gutenberg search, evaluate,
                   and tui; also gutenberg_catalog in config, or
                   NEMALO_GUTENBERG_CATALOG
`,
	"evaluate": `nemalo evaluate archive:ITEM|gutenberg:NUMBER|arxiv:ID|openlibrary:ID [--files|--editions]
  Inspect source-declared files, rights, and access restrictions.
  gutenberg:NUMBER without --files is one local catalog row and lists no
  files. --files fetches one Gutenberg bibliographic record and lists EPUB
  offers; it does not download an ebook.
  openlibrary:WORK --editions lists one page of editions (--limit, --offset).
  openlibrary:EDITION reads that edition. When its ocaid names one Archive
  item, the result is that item's metadata; acquire the printed archive:ITEM.
  A work-level Archive identifier is not selected.
  arxiv:ID or arxiv:IDvN reads one paper version from arXiv: an unversioned
  identifier resolves to the latest. The license arXiv records for the latest
  version is applied only to that version. Requests keep arXiv's pace of one
  every three seconds.
`,
	"catalog": `nemalo catalog fetch gutenberg --output FILE
  Download the official Gutenberg catalog to a new file. Search does not do
  this, and it does not download ebooks. An interrupted download keeps
  nothing.
`,
	"acquire": `nemalo acquire archive:ITEM --file NAME --output NEW_DIRECTORY
  Download a selected EPUB, PDF, MP3, M4B, or M4A into untrusted intake.
nemalo acquire gutenberg:NUMBER --file NAME --output NEW_DIRECTORY
  Download one EPUB named by the bibliographic record, such as
  1342.epub.noimages, into untrusted intake.
nemalo acquire arxiv:IDvN --file IDvN.pdf --output NEW_DIRECTORY
  Download one arXiv paper version's PDF; the version must be explicit.
nemalo acquire archive:ITEM --recording FORMAT --output NEW_DIRECTORY [--resume]
  Download every track of a declared recording, in order.

Options:
  --file NAME             exact source file name
  --output NEW_DIRECTORY  a new folder; missing parents are created
  --max-file-bytes N      default and maximum 256 MiB, or 2 GiB for M4B/M4A
  --no-scan, --on-detection quarantine|delete
  --recording "64Kbps MP3"  a format listed under recordings by evaluate
  --max-recording-bytes 4294967296 (maximum 16 GiB)
  --resume  continue an interrupted file or recording

Acquisition retains incomplete output and never overwrites it. --resume
continues an interrupted file in its packet when the source still has the
same bytes and the packet already has a transfer record and partial bytes.
A failure before any bytes has no transfer record; start a new output.
--resume also continues a recording, keeping completed tracks. When a
scanner is installed it scans the downloaded bytes once. A detection moves
them to the Nemalo quarantine (or deletes them) and lists other files from
the same item. Rights remain unresolved.
`,
	"inspect": `nemalo inspect DIRECTORY [options]
  Read-only inventory of an explicitly selected folder.

Options:
  --hashes, --max-entries 10000, --max-depth 32,
  --max-file-bytes 268435456, --max-total-bytes 1073741824
  --par2  probe regular files and read bounded PAR2 packet metadata

Inventory does not extract archives, validate books, or scan for malware.
Default inventory reads no file bodies. --par2 and --hashes share actual
read budgets; unchecked parity data stays explicit. Declared filenames
never cause another file to be opened. Protected-file verification and
repair do not run.
`,
	"check": `nemalo check FILE [options]
  Bounded file health assessment, without rendering content.

Options:
  --scan, --epubcheck, --decode, --expected-bytes N, --expected-sha256 HASH

--epubcheck runs the installed EPUBCheck on an EPUB snapshot. Errors it
reports, or a check that could not run, are review findings; warnings are
listed. Nemalo never installs or downloads EPUBCheck.

--decode decodes an MP3, M4B, or M4A snapshot in full with the installed
FFmpeg and FFprobe, reading only that file, and reports the decoded duration
beside the encoded one. Decoder errors, a decoded duration more than a second
from the encoded one, or a decode that could not run are review findings.
FFmpeg parses the untrusted file; use it on files you mean to keep.

Check uses a private temporary snapshot (up to 256 MiB, or 2 GiB for an M4B
or M4A) and needs that much free temporary space. EPUB text and document
counts, PDF page trees, MP3 frames, and MP4 samples are measured; fixed pages
are not inferred and audio is not decoded. --scan invokes installed antivirus without
remediation. It is recommended for a file you did not produce. Windows uses
Microsoft Defender. Linux and macOS use ClamAV (clamscan) after you install
it and update signatures with freshclam. External scanner cloud and
sample-submission settings apply. Missing or failed scans cannot mean clean.
A no-detection result is not proof the file is safe. When a publisher
supplies a SHA-256, pass --expected-sha256. A match shows the bytes are the
published bytes. It does not prove those bytes are safe.
`,
	"library": `nemalo library init DIRECTORY
  Initialize local control state in an existing explicit folder.
nemalo library status DIRECTORY
  Read control identity and journal state without creating files.
nemalo library import LIBRARY SOURCE [--apply] [--scan]
  Assess one EPUB, PDF, MP3, M4B, M4A, or intake packet; --apply stores it.
nemalo library ingest LIBRARY FOLDER [--apply] [--scan]
  Import every EPUB, PDF, MP3, M4B, M4A, intake packet, and ZIP member in a folder;
  the folder and its archives are preserved.
nemalo library list CATALOG|LIBRARY [--query TEXT]
  Browse a snapshot's files, or a managed library's holdings.
  Options: --format all|epub|pdf|mp3|m4b|m4a, --limit 10, --offset 0,
           --status all|checked|review, --language en (managed library
           only; matches the declared base language, so en, en-US, and
           eng are one; holdings without a readable tag are counted)
nemalo library recordings LIBRARY [--source archive:ITEM]
  List held recordings in declared track order; with --source, verify and
  index each track's timing.
nemalo library narration LIBRARY --declaration FILE [--apply --plan DIGEST]
  Preview an explicit user narration over exact held recording selections;
  apply records it only with that preview's plan. No audio or holding changes.
nemalo library narration LIBRARY --retract ID [--apply --plan DIGEST]
  Preview or record withdrawal of one user statement, retaining its history.
nemalo library related LIBRARY [--asset sha256:HASH] [--limit 10] [--offset 0]
  Group holdings that share an identifier an EPUB package or a source's
  metadata states, versions of one arXiv paper, files of one source item,
  or a narration. Nothing is merged, and titles are never compared.
nemalo library citation LIBRARY --asset sha256:HASH [--json]
  Export recorded citation declarations as CSL-JSON with a provenance and
  unknown-field sidecar. No stored content, receipts or provider is opened.
  Byte status is not revalidated; this is not a fresh integrity or rights check.
nemalo library cleanup LIBRARY FOLDER [--apply --plan DIGEST]
  Move sources the library holds into a recoverable holding area; the
  preview lists every entry and a plan digest.
nemalo library cleanup LIBRARY --stop
  Close a pending cleanup that cannot finish, so it can be restored.
nemalo library restore LIBRARY --cleanup ID [--apply | --stop]
  Return a cleanup's files to their folder, never replacing.
nemalo library purge LIBRARY --cleanup ID [--apply --plan DIGEST | --stop]
  Remove a completed cleanup's held copies whose bytes the library holds and
  verifies; receipts, intents, and anything else stay. A purged copy cannot
  be restored. --stop closes a purge that cannot finish.
nemalo library rescan LIBRARY [--apply]
  Scan review holdings that lack scan evidence; --apply records results.
nemalo library open LIBRARY --asset sha256:HASH [--app APPLICATION]
  Open a verified read-only copy in a reader or player.
nemalo library export LIBRARY --asset sha256:HASH --output DIRECTORY
  Copy one holding out, verified, never overwriting.
nemalo library audit LIBRARY
  Compare managed holdings with their stored bytes.
nemalo library audit CATALOG --root DIRECTORY
  Compare a snapshot catalog with files under an explicit root.
nemalo library check LIBRARY
  Verify the whole library read-only: stored and held bytes, untracked
  files, unfinished operations, review counts.
nemalo library snapshot DIRECTORY --output FILE [--assess]
  Save a new portable byte-identity catalog outside the root. Also accepts
  the inspect max-* limits.

library import copies one assessed file into an initialized library and
preserves the source. Without --apply it writes nothing. checked requires
limited checks that passed with no finding and a scan that reported no
detections: a PDF's page tree and active content fully examined, an MP3's
frames indexed, an M4B's or M4A's samples all located. Unscanned and other
results stay in review. Snapshots hash all regular
files, account for excluded links, refuse incomplete inventories, and never
overwrite. A snapshot audit requires --root; the catalog cannot authorize a
scan. A library audit compares managed holdings only.
`,
	"collection": `nemalo collection diff BEFORE AFTER
  Compare two collection files offline: added/removed exact selections,
  name/note changes, and changes in retained selection order. Reports both
  file hashes. Needs no library and writes nothing. Review this diff and
  the acquisition preview before applying an updated collection.
nemalo collection status COLLECTION LIBRARY
  Resolve each item of a collection file against the library, offline:
  held, held in review, recording incomplete, or not held.
nemalo collection acquire COLLECTION LIBRARY --output INTAKE [--apply]
  Preview, then with --apply acquire each item the library does not hold
  into INTAKE, one at a time, as acquire does. Run library ingest on INTAKE
  afterwards. A packet with a receipt waits for ingest, an interrupted one
  is resumed, and any other existing packet is left as it is.

Options:
  --max-total-bytes N      bytes the run may transfer in all; each new item's
                           budget is cut to what remains, so a larger item
                           fails, and a resume starts only if it fits
  --max-recording-bytes N  a recording's declared size budget (4 GiB)
  --no-scan, --on-detection quarantine|delete  as for acquire

A collection file is JSON: {"schema_version":1,"name":"...","items":[
  {"source":"archive:ITEM","file":"NAME"},
  {"source":"archive:ITEM","recording":"64Kbps MP3"},
  {"source":"gutenberg:NUMBER","file":"NUMBER.epub.noimages"},
  {"source":"arxiv:IDvN","file":"IDvN.pdf","note":"optional"}]}
Items are exact selections; titles and identifiers are not matched.
`,
	"jobs": `nemalo jobs list
  List durable jobs that MCP servers started, newest first.
nemalo jobs get job:ID
  Show one job: status, last progress, and its result or error.
nemalo jobs cancel job:ID
  Ask the process running the job to stop it.

Jobs run only inside nemalo mcp serve --allow-write. A job whose process
ended before it finished is recorded as interrupted; start it again, with
resume for an acquisition.
`,
	"bookmark": `nemalo bookmark enable LIBRARY
  Allow explicit local bookmarks in this library.
nemalo bookmark disable LIBRARY
  Keep saved places and refuse new saves and resume.
nemalo bookmark list LIBRARY
  List explicit bookmarks. Notes are not book text.
nemalo bookmark save LIBRARY --asset sha256:HASH --representation ID
       --representation-id sha256:HASH --unit N --text-offset N [--cursor TOKEN]
  Save one source location. Repeating the same place writes nothing.
nemalo bookmark note LIBRARY --asset sha256:HASH [--text NOTE]
  Set the reader's note, or clear it when the text is empty.
nemalo bookmark delete LIBRARY --asset sha256:HASH
  Drop one asset's saved location. Earlier journal lines stay.
nemalo bookmark export LIBRARY --output FILE
  Write current bookmarks to a new file outside .nemalo.
nemalo bookmark import LIBRARY --input FILE [--replace]
  Add bookmarks from an export. A difference writes nothing unless
  --replace is set.
nemalo bookmark resume LIBRARY --asset sha256:HASH
  Open the saved location when those bytes still match.

Bookmarks are a separate explicit record. Reading, repeating, continuing, and
closing do not write one. bookmark save checks the representation id you pass
and refuses the save when the bytes no longer match. bookmark resume returns
no text when the file or the representation changed. A note is your text,
kept with the bookmark and never written into the book. MCP does not read or
write bookmarks. Enabling bookmarks adds .nemalo/bookmarks.jsonl; a library
that has that file needs this version or newer to open its control directory.
`,
	"content": `nemalo content units CATALOG --root DIRECTORY --asset sha256:HASH
  List supported reading-order documents or PDF pages, offline.
nemalo content navigation CATALOG --root DIRECTORY --asset sha256:HASH
  List source-declared EPUB contents (nav and NCX), offline.
nemalo content pages CATALOG --root DIRECTORY --asset sha256:HASH
  List an EPUB's declared page labels (page-list), offline.
nemalo content read CATALOG --root DIRECTORY --asset sha256:HASH
  Retrieve bounded source text or selected audio bytes, offline.
nemalo content locate CATALOG --root DIRECTORY --asset sha256:HASH --text TEXT
  Find exact quotations by unit or PDF page, with offsets.
nemalo content source CATALOG --root DIRECTORY --asset sha256:HASH
  Read bounded original EPUB XML, optionally --part ID.
nemalo content resource CATALOG --root DIRECTORY --asset sha256:HASH --cursor TOKEN
  Read referenced local image bytes as base64; never render.
nemalo content capabilities
  Report supported representation versions and limits.

Read options:
  --unit 0, --text-offset 0, --max-bytes 4096, --cursor TOKEN
  --evidence returns exact source evidence for a managed content read only.
  --evidence-reference TOKEN compares an explicitly repeated bounded read.
  Neither option grants decoder permission or records reading state.
  --representation epub-text/2|epub-text/1|epub-structure/1|epub-nav/1|
                   epub-page-list/1|pdf-text/1|mpeg-audio/1|
                   mp4-audio/1|mp4-audio/2|mp4-pcm/1
  --navigation nav-doc|ncx, --label LABEL (epub-page-list/1),
  --part ID, --sample N (mpeg-audio/1), --packet N (mp4-audio/2)
  --decode, --pcm-sample N (mp4-pcm/1 only)

The default epub-text/2 omits images, tables, mathematics, and media from the
text and locates each omission as a gap. A continuation keeps the
representation it was issued for. epub-nav/1 reads one source-declared
contents entry: --unit is that entry's order, and --text-offset is relative
to the entry. When a book has both a navigation document and an NCX, a read
names which one with --navigation. epub-page-list/1 reads from one declared
page label: --label LABEL by its exact text, --unit by its order, or --cursor
with its reference; a label that several markers carry lists them instead. A
page label is the EPUB's own marker, not a screen page or a print-edition
match. mp4-audio/1 lists an M4B's or M4A's located samples, timing, and
declared chapters, units only; play the file with library open or export.
Explicit mp4-audio/2 returns whole original packets and their bounded sample
entry as base64 in --json. --packet is a zero-based ordinal; continuations do
not skip packets. This is not a playable M4B header or decoded timing. Each
call prepares a verified private copy, up to 2 GiB temporary disk and 4 GiB
source reads, with a five-minute bound and caller cancellation. Earlier
representations keep their existing limits.
Explicit mp4-pcm/1 units/read also require --decode on every call, including
continuation. --pcm-sample is a decoded output sample, not an encoded sample
or packet. A read returns a bounded mono PCM WAV in --json, identified by the
complete sequential output hash. Each call may stream 8 GiB of PCM, prepare
2 GiB private disk, and use five minutes; earlier content paths never decode.
Extraction is not rendered layout or acknowledgement of reading.
`,
	"read": `nemalo read CATALOG|LIBRARY --root DIRECTORY --asset sha256:HASH [options]
  Read EPUB or PDF text as terminal pages, or one word at a time. For a
  managed library, --root is the library folder. It needs a terminal.

Options:
  --speed           one word at a time, starting paused
  --rate 250        words per minute, 100 to 1000
  --unit N, --text-offset N   where to start

Closing prints the unit and byte to return to. Nothing is recorded, and
closing does not save a bookmark.
`,
	"mcp": `nemalo mcp serve --scope DIRECTORY [options]
  Serve MCP tools over stdio for an agent host; read-only by default.

Options:
  --scope DIRECTORY  repeatable; the configured library and review folders
                     are added
  --allow-network    catalog search and evaluation
  --allow-decode     separate content_decode_units/read tools; installed
                     FFmpeg, full sequential work, bounded derived WAV output
  --allow-write      library import preview and apply; apply stores only
                     the plan a preview returned and never changes a source
  --transport stdio

Without --allow-write, tools are read-only; stdout carries protocol only.
`,
	"version": `nemalo version, nemalo --version
  Show the version, and the commit when built from a source checkout.
`,
	"help": `nemalo help [COMMAND]
  Show the overview, or one command's options and limits.
`,
}

// topics lists the commands that have help, for an unknown topic.
func topics() string {
	names := make([]string, 0, len(commandHelp))
	for name := range commandHelp {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// versionLine is the version, with the source commit when the build
// recorded one.
func versionLine() string {
	line := "nemalo " + Version
	if info, ok := debug.ReadBuildInfo(); ok {
		revision, modified := "", false
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				revision = s.Value
			case "vcs.modified":
				modified = s.Value == "true"
			}
		}
		if len(revision) >= 12 {
			line += " (" + revision[:12]
			if modified {
				line += ", modified"
			}
			line += ")"
		}
	}
	return line
}

// writeHelp writes help text wrapped to the terminal width, or 80 columns
// when output is not a terminal.
func writeHelp(out io.Writer, text string) error {
	width := 80
	if f, ok := out.(*os.File); ok && term.IsTerminal(f.Fd()) {
		if w, _, err := term.GetSize(f.Fd()); err == nil && w >= 20 && w < width {
			width = w
		}
	}
	_, err := io.WriteString(out, wrapHelp(strings.ReplaceAll(text, "VERSION", Version), width))
	return err
}

// wrapHelp breaks a line wider than width at spaces, continuing it under its
// own indent plus two.
func wrapHelp(text string, width int) string {
	var b strings.Builder
	for _, line := range strings.SplitAfter(text, "\n") {
		body := strings.TrimSuffix(line, "\n")
		indent := body[:len(body)-len(strings.TrimLeft(body, " "))]
		for len(body) > width {
			cut := strings.LastIndex(body[:width+1], " ")
			if cut <= len(indent) {
				break
			}
			b.WriteString(strings.TrimRight(body[:cut], " ") + "\n")
			body = indent + "  " + strings.TrimLeft(body[cut:], " ")
		}
		b.WriteString(body)
		if strings.HasSuffix(line, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
