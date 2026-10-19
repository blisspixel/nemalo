// Package cli implements the scriptable terminal interface over application services.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/blisspixel/nemalo/internal/acquisition"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/config"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/mcpserver"
	"github.com/blisspixel/nemalo/internal/present"
	"github.com/blisspixel/nemalo/internal/reader"
	"github.com/blisspixel/nemalo/internal/textsafe"
	"github.com/blisspixel/nemalo/internal/tui"
	"github.com/charmbracelet/x/term"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const Version = "0.1.0-alpha.11"

const help = `Nemalo
Find knowledge. Care for it. Realize its potential.

Usage: nemalo <command> [options]

  tui                 Open the interactive terminal interface
  doctor              Report configuration and optional tool availability
  search QUERY        Search a selected catalog. Open Library and Internet Archive
                      use the network. Gutenberg reads a local catalog file.
  evaluate archive:ITEM|gutenberg:NUMBER|openlibrary:ID [--files|--editions]
                      Inspect source-declared files, rights, and access restrictions.
                      gutenberg:NUMBER without --files is one local catalog row and lists no files.
                      --files fetches one Gutenberg bibliographic record and lists EPUB offers.
                      It does not download an ebook.
                      openlibrary:WORK --editions lists one page of editions (--limit, --offset).
                      openlibrary:EDITION reads that edition. When its ocaid names one Archive
                      item, the result is that item's metadata. Acquire the printed archive:ITEM.
                      A work-level Archive identifier is not selected.
  catalog fetch gutenberg --output FILE
                      Download the official Gutenberg catalog to a new file.
                      Search does not do this, and it does not download ebooks.
  acquire archive:ITEM --file NAME --output NEW_DIRECTORY
                      Download a selected EPUB/PDF/MP3 into untrusted intake
  acquire gutenberg:NUMBER --file NAME --output NEW_DIRECTORY
                      Download one EPUB named by the bibliographic record, such as
                      1342.epub.noimages, into untrusted intake
  acquire archive:ITEM --recording FORMAT --output NEW_DIRECTORY [--resume]
                      Download every track of a declared recording, in order
  inspect DIRECTORY   Read-only inventory of an explicitly selected folder
  check FILE          Bounded file health assessment, without rendering content
  library snapshot DIRECTORY --output FILE
                      Save a new portable byte-identity catalog outside the root
  library list CATALOG|LIBRARY [--query TEXT]
                      Browse a snapshot's files, or a managed library's holdings
  library import LIBRARY SOURCE [--apply] [--scan]
                      Assess one EPUB, PDF, MP3, or intake packet; --apply stores it
  library ingest LIBRARY FOLDER [--apply] [--scan]
                      Import every EPUB, PDF, MP3, intake packet, and ZIP member
                      in a folder; the folder and its archives are preserved
  library recordings LIBRARY [--source archive:ITEM]
                      List held audiobook recordings in declared track order;
                      with --source, verify and index each track's timing
  library cleanup LIBRARY FOLDER [--apply --plan DIGEST]
                      Move sources the library holds into a recoverable holding
                      area; the preview lists every entry and a plan digest
  library cleanup LIBRARY --stop
                      Close a pending cleanup that cannot finish, so it can be restored
  library restore LIBRARY --cleanup ID [--apply | --stop]
                      Return a cleanup's files to their folder, never replacing;
                      --stop closes a restore that cannot finish
  library rescan LIBRARY [--apply]
                      Scan review EPUBs that lack scan evidence; --apply records results
  library open LIBRARY --asset sha256:HASH [--app APPLICATION]
                      Open a verified read-only copy in a reader or player
  library export LIBRARY --asset sha256:HASH --output DIRECTORY
                      Copy one holding out, verified, never overwriting
  library audit CATALOG --root DIRECTORY
                      Compare a snapshot catalog with files under an explicit root
  library audit LIBRARY
                      Compare managed holdings with their stored bytes
  library check LIBRARY
                      Verify the whole library read-only: stored and held bytes,
                      untracked files, unfinished operations, review counts
  library init DIRECTORY
                      Initialize local control state in an existing explicit folder
  library status DIRECTORY
                      Read control identity and journal state without creating files
  bookmark enable LIBRARY
                      Allow explicit local bookmarks in this library
  bookmark disable LIBRARY
                      Keep saved places and refuse new saves and resume
  bookmark list LIBRARY
                      List explicit bookmarks. Notes are not book text
  bookmark save LIBRARY --asset sha256:HASH --representation ID
                      --representation-id sha256:HASH --unit N --text-offset N
                      [--cursor TOKEN]
                      Save one source location. Repeating the same place writes nothing
  bookmark note LIBRARY --asset sha256:HASH [--text NOTE]
                      Set the reader's note, or clear it when text is empty
  bookmark delete LIBRARY --asset sha256:HASH
                      Drop one asset's saved location. Earlier journal lines stay
  bookmark export LIBRARY --output FILE
                      Write current bookmarks to a new file outside .nemalo
  bookmark import LIBRARY --input FILE [--replace]
                      Add bookmarks from an export. A difference writes nothing
                      unless --replace is set
  bookmark resume LIBRARY --asset sha256:HASH
                      Open the saved location when those bytes still match
  content units CATALOG --root DIRECTORY --asset sha256:HASH
                      List supported EPUB reading-order documents, offline
  content navigation CATALOG --root DIRECTORY --asset sha256:HASH
                      List source-declared EPUB contents (nav and NCX), offline
  content read CATALOG --root DIRECTORY --asset sha256:HASH
                      Retrieve bounded source text with exact continuation, offline
  content locate CATALOG --root DIRECTORY --asset sha256:HASH --text TEXT
                      Find exact quotations by unit or PDF page, with offsets
  content source CATALOG --root DIRECTORY --asset sha256:HASH
                      Read bounded original EPUB XML, optionally --part ID
  content resource CATALOG --root DIRECTORY --asset sha256:HASH --cursor TOKEN
                      Read referenced local image bytes as base64; never render
  content capabilities
                      Report supported representation versions and limits
  read CATALOG|LIBRARY --root DIRECTORY --asset sha256:HASH [--speed]
                      Read EPUB or PDF text as terminal pages, or one word at a
                      time with --speed (--rate WPM, default 250). --unit and
                      --text-offset choose the start. Nothing is recorded.
  mcp serve --scope DIRECTORY
                      Serve read-only MCP tools over stdio for an agent host
  version             Show the development version
  help                Show this help

Shared options: --json, --config FILE, --library DIRECTORY, --review DIRECTORY
Search options: --source openlibrary|archive|gutenberg (default openlibrary),
                --limit 10, --offset 0,
                --sort relevance|identifier|downloads (identifier and downloads are
                Archive only; Gutenberg also accepts identifier),
                --catalog FILE (absolute pg_catalog.csv for Gutenberg search,
                evaluate, and tui; also gutenberg_catalog in config, or
                NEMALO_GUTENBERG_CATALOG). Gutenberg does not query the website.
Acquire options: --file exact-source-filename, --output new-directory,
                 --max-file-bytes 268435456 (maximum 256 MiB),
                 --no-scan, --on-detection quarantine|delete,
                 --recording "64Kbps MP3" (a format listed under recordings by
                 evaluate), --max-recording-bytes 4294967296 (maximum 16 GiB),
                 --resume (continue a recording directory; completed tracks stay)
Acquisition creates missing parent folders, retains incomplete output, and never
overwrites it. --resume continues an interrupted file in its packet when the
source still has the same bytes and the packet already has a transfer record
and partial bytes. A failure before any bytes has no transfer record; start a
new output. --resume also continues a recording, keeping completed tracks.
When a scanner is installed it scans the downloaded bytes once. A detection
moves them to the Nemalo quarantine (or deletes them) and lists other files
from the same item. Rights remain unresolved.
Inspect options: --hashes, --max-entries 10000, --max-depth 32,
                 --max-file-bytes 268435456, --max-total-bytes 1073741824
Check options: --scan, --expected-bytes N, --expected-sha256 HASH
Library snapshot options: --output FILE, --assess (local health/EPUB metadata)
Library list options: --query TEXT, --format all|epub|pdf|mp3, --limit 10, --offset 0,
                      --status all|checked|review (managed library only)
Library import options: --apply, --scan
Library rescan options: --apply (without it, scans and reports but writes nothing)
Library audit options: --root DIRECTORY for a snapshot catalog
Content read options: --unit 0, --text-offset 0, --max-bytes 4096, --cursor TOKEN
                     --representation epub-text/2|epub-text/1|epub-structure/1|epub-nav/1|pdf-text/1|mpeg-audio/1,
                     --navigation nav-doc|ncx, --part ID, --sample N (mpeg-audio/1)
The default epub-text/2 omits images, tables, mathematics, and media from the
text and locates each omission as a gap. A continuation keeps the representation
it was issued for. epub-nav/1 reads one source-declared contents entry: --unit
is that entry's order, and --text-offset is relative to the entry. When a book
has both a navigation document and an NCX, a read names which one with
--navigation. Extraction is not rendered layout or acknowledgement of reading.
Bookmarks are a separate explicit record. Reading, repeating, continuing, and
closing do not write one. bookmark save checks the representation id you pass
and refuses the save when the bytes no longer match. bookmark resume returns
no text when the file or the representation changed. A note is your text, kept
with the bookmark and never written into the book. MCP does not read or write
bookmarks. Enabling bookmarks adds .nemalo/bookmarks.jsonl; a library that has
that file needs this version or newer to open its control directory.
MCP options: --scope DIRECTORY (repeatable; the configured library and review
             folders are added), --allow-network (catalog search and evaluation),
             --transport stdio. Tools are read-only; stdout carries protocol only.
Snapshot and snapshot audit also accept inventory max-* limits. Snapshots hash
all regular files, account for excluded links, refuse incomplete inventories,
and never overwrite. Snapshot audit requires --root; the catalog cannot
authorize a scan. A library-directory audit compares managed holdings only.

Check uses a private temporary snapshot (up to 256 MiB). EPUB text/document counts
are measured; fixed pages, PDF page counts, and audio duration are not inferred.
--scan invokes installed antivirus without remediation. It is recommended for
a file you did not produce. Windows uses Microsoft Defender. Linux and macOS
use ClamAV (clamscan) after you install it and update signatures with freshclam.
External scanner cloud and sample-submission settings apply. Missing/failed
scans cannot mean clean. A no-detection result is not proof the file is safe.
When a publisher supplies a SHA-256, pass --expected-sha256. A match shows the
bytes are the published bytes. It does not prove those bytes are safe.

Inventory does not extract archives, validate books, or scan for malware.
library import copies one assessed file into an initialized library and preserves
the source. Without --apply it writes nothing. checked requires an EPUB whose
limited checks passed and whose scan reported no detections. PDF, MP3, unscanned,
and other results stay in review. This is not folder cleanup, archive extraction,
or download resume. PDF/MP3 checks currently establish candidate signatures only.
Exit codes: 0 success, 1 operation failed/incomplete, 2 invalid usage/configuration.
`

type Envelope struct {
	SchemaVersion int    `json:"schema_version"`
	Command       string `json:"command"`
	Data          any    `json:"data,omitempty"`
	Error         string `json:"error,omitempty"`
}

func Run(ctx context.Context, args []string, out, errOut io.Writer) int {
	return Execute(ctx, args, out, errOut, app.New(), os.Getenv, config.DefaultPaths, func(ctx context.Context, s app.Service) error { return tui.Run(ctx, s, os.Stdin, out) })
}

// Execute exposes external boundaries for offline contract and cancellation tests.
func Execute(ctx context.Context, args []string, out, errOut io.Writer, service app.Service, env func(string) string, paths func() (config.Paths, error), terminal func(context.Context, app.Service) error) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, err := io.WriteString(out, help)
		if err != nil {
			return 1
		}
		return 0
	}
	command := args[0]
	action := ""
	arguments := args[1:]
	if (command == "library" || command == "content" || command == "mcp" || command == "bookmark" || command == "catalog") && len(arguments) > 0 {
		action = arguments[0]
		arguments = arguments[1:]
		if action == "--help" || action == "-h" {
			arguments = []string{"--help"}
		}
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	jsonMode := flags.Bool("json", false, "structured output")
	configFile := flags.String("config", env("NEMALO_CONFIG"), "configuration file")
	libraryDir := flags.String("library", "", "explicit library directory")
	review := flags.String("review", "", "explicit review directory")
	limit, offset := flags.Int("limit", 10, "search page size"), flags.Int("offset", 0, "search offset")
	source := flags.String("source", "openlibrary", "search provider")
	sortOrder := flags.String("sort", discovery.OrderRelevance, "search result order")
	catalogPath := flags.String("catalog", "", "absolute path of a local Project Gutenberg catalog")
	listFiles := flags.Bool("files", false, "fetch one Gutenberg bibliographic record and list its files")
	listEditions := flags.Bool("editions", false, "list one page of editions for an Open Library work")
	limits := inventory.Defaults()
	checkOptions := assessment.Options{}
	output := flags.String("output", "", "new snapshot output path")
	sourceFile := flags.String("file", "", "exact selected source filename")
	query := flags.String("query", "", "literal holdings metadata filter")
	format := flags.String("format", "all", "exact holdings filename suffix filter")
	holdingStatus := flags.String("status", "all", "managed holdings status filter")
	auditRoot := flags.String("root", "", "explicit audit root")
	assess := flags.Bool("assess", false, "include local health facts without antivirus")
	apply := flags.Bool("apply", false, "store the assessed import")
	noScan := flags.Bool("no-scan", false, "skip the post-download scan")
	var scopes multiFlag
	flags.Var(&scopes, "scope", "directory MCP tools may read (repeatable)")
	allowNetwork := flags.Bool("allow-network", false, "register MCP catalog tools that contact providers")
	transport := flags.String("transport", "stdio", "MCP transport")
	application := flags.String("app", "", "application to open a library copy with")
	onDetection := flags.String("on-detection", "quarantine", "quarantine or delete a detected download")
	recordingFormat := flags.String("recording", "", "declared recording format to acquire as a whole")
	findText := flags.String("text", "", "exact text to locate")
	planDigest := flags.String("plan", "", "cleanup plan digest from the preview")
	cleanupID := flags.String("cleanup", "", "cleanup ID to restore")
	stop := flags.Bool("stop", false, "close a pending cleanup that cannot finish")
	resume := flags.Bool("resume", false, "continue an existing recording directory")
	maxRecording := flags.Int64("max-recording-bytes", 4<<30, "declared total byte budget for a recording")
	speed := flags.Bool("speed", false, "read one word at a time")
	rate := flags.Int("rate", reader.DefaultRate, "speed reading rate in words per minute")
	contentRequest := content.Request{SchemaVersion: 1}
	flags.StringVar(&contentRequest.AssetID, "asset", "", "exact catalog asset ID")
	flags.IntVar(&contentRequest.Unit, "unit", 0, "zero-based reading-order document")
	flags.IntVar(&contentRequest.Offset, "text-offset", 0, "zero-based UTF-8 text byte offset")
	flags.IntVar(&contentRequest.MaxBytes, "max-bytes", 4096, "maximum returned text bytes")
	flags.StringVar(&contentRequest.Cursor, "cursor", "", "source-bound continuation token")
	flags.StringVar(&contentRequest.Representation, "representation", "", "negotiated content representation; a continuation selects its own")
	flags.StringVar(&contentRequest.Part, "part", "", "exact source part ID")
	flags.StringVar(&contentRequest.Navigation, "navigation", "", "epub-nav/1 source: nav-doc or ncx")
	representationID := flags.String("representation-id", "", "sha256: representation id observed for this location")
	bookmarkInput := flags.String("input", "", "bookmark export to import")
	replaceBookmarks := flags.Bool("replace", false, "replace differing bookmark locations on import")
	flags.Int64Var(&contentRequest.Sample, "sample", 0, "encoded sample where an mpeg-audio/1 read starts")
	flags.BoolVar(&checkOptions.Scan, "scan", false, "invoke installed antivirus")
	flags.Int64Var(&checkOptions.ExpectedBytes, "expected-bytes", 0, "known expected file size")
	flags.StringVar(&checkOptions.ExpectedSHA256, "expected-sha256", "", "known expected SHA-256")
	flags.BoolVar(&limits.Hashes, "hashes", false, "hash eligible regular files")
	flags.IntVar(&limits.Entries, "max-entries", limits.Entries, "inventory entry limit")
	flags.IntVar(&limits.Depth, "max-depth", limits.Depth, "inventory depth limit")
	flags.Int64Var(&limits.FileBytes, "max-file-bytes", limits.FileBytes, "per-file read limit")
	flags.Int64Var(&limits.TotalBytes, "max-total-bytes", limits.TotalBytes, "total read limit")
	parseErr := parse(flags, arguments)
	write := func(data any, err error, code int) int {
		if *jsonMode {
			e := Envelope{SchemaVersion: 1, Command: command, Data: data}
			if err != nil {
				e.Error = err.Error()
			}
			if encodeErr := textsafe.WriteJSON(out, e, false); encodeErr != nil {
				return 1
			}
		} else if err != nil {
			if started(data) {
				if outputErr := printData(out, data); outputErr != nil {
					return 1
				}
			}
			_, _ = fmt.Fprintf(errOut, "nemalo: %s\n", present.Error(err))
		} else {
			if encodeErr := printData(out, data); encodeErr != nil {
				return 1
			}
		}
		return code
	}
	if errors.Is(parseErr, flag.ErrHelp) {
		_, err := io.WriteString(out, help)
		if err != nil {
			return 1
		}
		return 0
	}
	if parseErr != nil {
		return write(nil, parseErr, 2)
	}
	var invalidFlag string
	flags.Visit(func(f *flag.Flag) {
		if !flagApplies(command, action, f.Name) {
			invalidFlag = f.Name
		}
	})
	if invalidFlag != "" {
		return write(nil, fmt.Errorf("--%s does not apply to %s", invalidFlag, command), 2)
	}
	if command == "version" {
		if flags.NArg() != 0 {
			return write(nil, errors.New("version takes no arguments"), 2)
		}
		return write(Version, nil, 0)
	}
	if command == "content" && action == "capabilities" {
		if flags.NArg() != 0 {
			return write(nil, errors.New("content capabilities takes no arguments"), 2)
		}
		return write(service.ContentCapabilities(), nil, 0)
	}
	if command != "doctor" && command != "search" && command != "evaluate" && command != "acquire" && command != "inspect" && command != "check" && command != "tui" && command != "library" && command != "content" && command != "mcp" && command != "read" && command != "bookmark" && command != "catalog" {
		return write(nil, fmt.Errorf("unknown command %q; use nemalo help", command), 2)
	}
	p, err := paths()
	if err != nil {
		return write(nil, err, 2)
	}
	explicit := *configFile != ""
	if !explicit {
		*configFile = p.Config
	}
	c, err := config.Load(*configFile, explicit, env, config.Config{Library: *libraryDir, Review: *review, GutenbergCatalog: *catalogPath})
	if err != nil {
		return write(nil, err, 2)
	}
	if service.Quarantine == "" && p.State != "" {
		service.Quarantine = filepath.Join(p.State, "quarantine")
	}
	if service.OnDetection == "" {
		service.OnDetection = c.OnDetection
	}
	if service.Handoff == "" && p.State != "" {
		service.Handoff = filepath.Join(p.State, "handoff")
	}
	service.UseGutenbergCatalog(c.GutenbergCatalog)
	// next prints the command that continues the first-book path after human output.
	next := func(code int, step string) int {
		if step != "" && !*jsonMode {
			_, _ = io.WriteString(out, step)
		}
		return code
	}
	// guide prints one next step after human output when scan evidence is missing.
	guide := func(code int, antivirus string, scanRequested bool) int {
		if hint := service.ScanGuidance(antivirus, scanRequested); hint != "" && !*jsonMode {
			_, _ = fmt.Fprintln(out, "Next: "+hint)
		}
		return code
	}
	switch command {
	case "acquire":
		if flags.NArg() != 1 {
			return write(nil, errors.New("use acquire archive:ITEM or gutenberg:NUMBER --file NAME --output NEW_DIRECTORY"), 2)
		}
		detection := c.OnDetection
		flags.Visit(func(f *flag.Flag) {
			if f.Name == "on-detection" {
				detection = *onDetection
			}
		})
		fileLimit, recordingLimit := false, false
		flags.Visit(func(f *flag.Flag) {
			fileLimit = fileLimit || f.Name == "max-file-bytes"
			recordingLimit = recordingLimit || f.Name == "max-recording-bytes"
		})
		if *recordingFormat != "" || recordingLimit || (*resume && *sourceFile == "") {
			if *recordingFormat == "" || *sourceFile != "" || fileLimit {
				return write(nil, errors.New("use acquire archive:ITEM --recording FORMAT --output DIRECTORY [--resume] [--max-recording-bytes N], without --file or --max-file-bytes"), 2)
			}
			recording := acquisition.RecordingRequest{SourceID: flags.Arg(0), Format: *recordingFormat, Output: *output, MaxBytes: *maxRecording, Resume: *resume, NoScan: *noScan, OnDetection: detection}
			if err := recording.Validate(); err != nil {
				return write(nil, err, 2)
			}
			data, err := service.AcquireRecording(ctx, recording)
			if err != nil {
				return write(data, err, 1)
			}
			return guide(write(data, nil, 0), recordingAntivirus(data), true)
		}
		request := acquisition.Request{SourceID: flags.Arg(0), File: *sourceFile, Output: *output, MaxBytes: limits.FileBytes, NoScan: *noScan, OnDetection: detection, Resume: *resume}
		if err := request.Validate(); err != nil {
			return write(nil, err, 2)
		}
		data, err := service.Acquire(ctx, request)
		if err != nil {
			return write(data, err, 1)
		}
		return guide(next(write(data, nil, 0), present.NextAfterAcquisition(data, c.Library)), data.Antivirus, true)
	case "content":
		if flags.NArg() != 1 || (action != "units" && action != "read" && action != "source" && action != "resource" && action != "locate" && action != "navigation") {
			return write(nil, errors.New("use content units/navigation/read/locate/source/resource CATALOG --root DIRECTORY --asset sha256:HASH"), 2)
		}
		if action == "locate" {
			if *findText == "" {
				return write(nil, errors.New("use content locate CATALOG --root DIRECTORY --asset sha256:HASH --text TEXT"), 2)
			}
			contentRequest.Find = *findText
		}
		contentRequest.Catalog, contentRequest.Root, contentRequest.UnitsOnly = flags.Arg(0), *auditRoot, action == "units" || action == "navigation"
		if action == "navigation" {
			if contentRequest.Representation != "" && contentRequest.Representation != "epub-nav/1" {
				return write(nil, errors.New("content navigation lists representation epub-nav/1"), 2)
			}
			contentRequest.Representation = "epub-nav/1"
		}
		if action == "source" {
			contentRequest.Representation = "epub-source/1"
		}
		if action == "resource" {
			contentRequest.Representation, contentRequest.Resource = "epub-structure/1", true
		}
		if err := contentRequest.Validate(); err != nil {
			return write(nil, err, 2)
		}
		if contentRequest.Cursor != "" {
			conflict := false
			flags.Visit(func(f *flag.Flag) {
				if f.Name == "unit" || f.Name == "text-offset" {
					conflict = true
				}
			})
			if conflict {
				return write(nil, errors.New("cursor cannot be combined with explicit unit or text-offset"), 2)
			}
		}
		r, err := service.Content(ctx, contentRequest)
		if err != nil {
			return write(r, err, 1)
		}
		return write(r, nil, 0)
	case "read":
		switch {
		case *jsonMode:
			return write(nil, errors.New("read is interactive and has no --json"), 2)
		case flags.NArg() != 1:
			return write(nil, errors.New("use read CATALOG --root DIRECTORY --asset sha256:HASH"), 2)
		case *auditRoot == "":
			return write(nil, fmt.Errorf("read needs --root; for a managed library use the same folder: --root %s", flags.Arg(0)), 2)
		case contentRequest.AssetID == "":
			return write(nil, errors.New("read needs --asset sha256:HASH"), 2)
		case !stdinTerminal():
			return write(nil, errors.New("read requires an interactive terminal"), 2)
		}
		if *rate < reader.MinRate || *rate > reader.MaxRate {
			return write(nil, fmt.Errorf("--rate must be %d through %d words per minute", reader.MinRate, reader.MaxRate), 2)
		}
		contentRequest.Catalog, contentRequest.Root = flags.Arg(0), *auditRoot
		if err := contentRequest.Validate(); err != nil {
			return write(nil, err, 2)
		}
		stop, err := readTerminal(ctx, service, out, tui.ReadOptions{Catalog: contentRequest.Catalog, Root: contentRequest.Root, Asset: contentRequest.AssetID, Start: reader.Position{Unit: contentRequest.Unit, Offset: contentRequest.Offset}, Speed: *speed, Rate: *rate})
		if err != nil {
			return write(nil, err, 1)
		}
		if stop != nil {
			_, err = fmt.Fprintf(out, "Closed at unit %d, byte %d. Nothing was recorded. To return there, repeat this command with --unit %d --text-offset %d. Closing does not save a bookmark.\n", stop.Unit, stop.Offset, stop.Unit, stop.Offset)
			if err != nil {
				return 1
			}
		}
		return 0
	case "evaluate":
		id := ""
		if flags.NArg() == 1 {
			id = flags.Arg(0)
		}
		archiveID := discovery.ValidArchiveID(id)
		gutenbergID := discovery.ValidGutenbergID(id)
		workID := discovery.ValidOpenLibraryWork(id)
		editionID := discovery.ValidOpenLibraryEdition(id)
		if flags.NArg() != 1 || (!archiveID && !gutenbergID && !workID && !editionID) {
			return write(nil, errors.New("evaluate requires exactly one archive:ITEM, gutenberg:NUMBER, or openlibrary:WORK or openlibrary:EDITION identifier"), 2)
		}
		if *listFiles && *listEditions {
			return write(nil, errors.New("evaluate accepts either --files or --editions"), 2)
		}
		if visited(flags, "limit") || visited(flags, "offset") {
			if !*listEditions {
				return write(nil, errors.New("evaluate --limit and --offset apply to --editions"), 2)
			}
		}
		if *listEditions {
			if !workID {
				return write(nil, errors.New("evaluate --editions requires one openlibrary:WORK identifier"), 2)
			}
			data, err := service.EvaluateOpenLibraryEditions(ctx, id, *limit, *offset)
			if err != nil {
				return write(data, err, 1)
			}
			return write(data, nil, 0)
		}
		if workID {
			return write(nil, errors.New("an Open Library work has no selected edition; use evaluate openlibrary:WORK --editions"), 2)
		}
		if *listFiles {
			if !gutenbergID {
				return write(nil, errors.New("evaluate --files fetches one gutenberg:NUMBER bibliographic record"), 2)
			}
			data, err := service.EvaluateGutenbergFiles(ctx, id)
			if err != nil {
				return write(data, err, 1)
			}
			return next(write(data, nil, 0), present.NextAfterEvaluation(data, acquisition.Eligible(data, limits.FileBytes)))
		}
		data, err := service.Evaluate(ctx, id)
		if err != nil {
			return write(data, err, 1)
		}
		return next(write(data, nil, 0), present.NextAfterEvaluation(data, acquisition.Eligible(data, limits.FileBytes)))
	case "library":
		if action == "import" {
			if flags.NArg() != 2 {
				return write(nil, errors.New("use library import LIBRARY SOURCE"), 2)
			}
			data, err := service.Import(ctx, library.ImportRequest{Library: flags.Arg(0), Source: flags.Arg(1), Scan: checkOptions.Scan, Apply: *apply})
			if err != nil {
				return write(data, err, 1)
			}
			return guide(next(write(data, nil, 0), present.NextAfterImport(data)), data.Antivirus, checkOptions.Scan)
		}
		if action == "ingest" {
			if flags.NArg() != 2 {
				return write(nil, errors.New("use library ingest LIBRARY FOLDER [--apply] [--scan]"), 2)
			}
			data, err := service.Ingest(ctx, library.IngestRequest{Library: flags.Arg(0), Folder: flags.Arg(1), Apply: *apply, Scan: checkOptions.Scan})
			if err != nil {
				return write(data, err, 1)
			}
			return write(data, nil, 0)
		}
		if action == "cleanup" && *stop {
			if flags.NArg() != 1 || *apply || *planDigest != "" {
				return write(nil, errors.New("use library cleanup LIBRARY --stop"), 2)
			}
			data, err := service.Cleanup(ctx, library.CleanupRequest{Library: flags.Arg(0), Folder: flags.Arg(0), Stop: true})
			if err != nil {
				return write(data, err, 1)
			}
			return write(data, nil, 0)
		}
		if action == "cleanup" {
			if flags.NArg() != 2 || *apply != (*planDigest != "") {
				return write(nil, errors.New("use library cleanup LIBRARY FOLDER to preview, then add --apply --plan DIGEST"), 2)
			}
			data, err := service.Cleanup(ctx, library.CleanupRequest{Library: flags.Arg(0), Folder: flags.Arg(1), Apply: *planDigest})
			if err != nil {
				return write(data, err, 1)
			}
			return write(data, nil, 0)
		}
		if action == "restore" {
			if flags.NArg() != 1 || *cleanupID == "" {
				return write(nil, errors.New("use library restore LIBRARY --cleanup ID [--apply]"), 2)
			}
			data, err := service.Restore(ctx, library.RestoreRequest{Library: flags.Arg(0), CleanupID: *cleanupID, Apply: *apply, Stop: *stop})
			if err != nil {
				return write(data, err, 1)
			}
			return write(data, nil, 0)
		}
		if action == "check" {
			if flags.NArg() != 1 {
				return write(nil, errors.New("use library check LIBRARY"), 2)
			}
			data, err := service.CheckLibrary(ctx, flags.Arg(0))
			if err != nil {
				return write(data, err, 1)
			}
			return write(data, nil, 0)
		}
		if action == "recordings" {
			if flags.NArg() != 1 {
				return write(nil, errors.New("use library recordings LIBRARY [--source archive:ITEM]"), 2)
			}
			filter := ""
			flags.Visit(func(f *flag.Flag) {
				if f.Name == "source" {
					filter = *source
				}
			})
			data, err := service.Recordings(ctx, flags.Arg(0), filter)
			if err != nil {
				return write(data, err, 1)
			}
			return write(data, nil, 0)
		}
		if action == "open" || action == "export" {
			if flags.NArg() != 1 || contentRequest.AssetID == "" || (action == "export" && *output == "") {
				return write(nil, fmt.Errorf("use library %s LIBRARY --asset sha256:HASH%s", action, map[string]string{"open": " [--app APPLICATION]", "export": " --output DIRECTORY"}[action]), 2)
			}
			var data library.ExportResult
			var err error
			if action == "open" {
				data, err = service.Open(ctx, flags.Arg(0), contentRequest.AssetID, *application)
			} else {
				data, err = service.Export(ctx, library.ExportRequest{Library: flags.Arg(0), AssetID: contentRequest.AssetID, Directory: *output})
			}
			if err != nil {
				return write(data, err, 1)
			}
			return write(data, nil, 0)
		}
		if action == "rescan" {
			if flags.NArg() != 1 {
				return write(nil, errors.New("use library rescan LIBRARY [--apply]"), 2)
			}
			data, err := service.Rescan(ctx, library.RescanRequest{Library: flags.Arg(0), Apply: *apply})
			if err != nil {
				return write(data, err, 1)
			}
			return write(data, nil, 0)
		}
		if flags.NArg() != 1 || (action != "snapshot" && action != "list" && action != "audit" && action != "init" && action != "status") {
			return write(nil, errors.New("use library init/status DIRECTORY, import LIBRARY SOURCE, snapshot DIRECTORY, list CATALOG, or audit CATALOG or LIBRARY"), 2)
		}
		if action == "init" || action == "status" {
			var r library.State
			var err error
			if action == "init" {
				r, err = service.InitializeLibrary(ctx, flags.Arg(0))
			} else {
				r, err = service.LibraryStatus(ctx, flags.Arg(0))
			}
			if err != nil {
				return write(r, err, 1)
			}
			return write(r, nil, 0)
		}
		if action == "snapshot" {
			if *output == "" {
				return write(nil, errors.New("library snapshot requires --output FILE outside its root"), 2)
			}
			r, err := service.Snapshot(ctx, flags.Arg(0), *output, limits, *assess)
			if err != nil {
				return write(r, err, 1)
			}
			return write(r, nil, 0)
		}
		if action == "list" {
			if *limit < 1 || *limit > 100 || *offset < 0 || *offset > 100000 || len(*query) > 1000 {
				return write(nil, errors.New("library list requires limit 1-100, offset 0-100000, and query at most 1000 bytes"), 2)
			}
			if !library.ValidFormatFilter(*format) {
				return write(nil, errors.New("format filter must be all, epub, pdf, or mp3"), 2)
			}
			// A library directory lists managed holdings; a file is a snapshot.
			if info, statErr := os.Lstat(flags.Arg(0)); statErr == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
				r, err := service.ManagedHoldings(ctx, flags.Arg(0), *query, *format, *holdingStatus, *limit, *offset)
				if err != nil {
					return write(r, err, 1)
				}
				return write(r, nil, 0)
			}
			if *holdingStatus != "all" {
				return write(nil, errors.New("--status applies to a managed library directory, not a snapshot catalog"), 2)
			}
			r, err := service.HoldingsFormat(flags.Arg(0), *query, *format, *limit, *offset)
			if err != nil {
				return write(r, err, 1)
			}
			return write(r, nil, 0)
		}
		info, statErr := os.Lstat(flags.Arg(0))
		if statErr != nil {
			return write(nil, statErr, 1)
		}
		if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			extra := false
			flags.Visit(func(f *flag.Flag) {
				if f.Name == "root" || strings.HasPrefix(f.Name, "max-") {
					extra = true
				}
			})
			if extra {
				return write(nil, errors.New("managed library audit accepts only the library directory"), 2)
			}
			managed, err := service.AuditManaged(ctx, flags.Arg(0))
			if err != nil {
				return write(managed, err, 1)
			}
			return write(managed, nil, 0)
		}
		if *auditRoot == "" {
			return write(nil, errors.New("library audit requires an explicit --root DIRECTORY"), 2)
		}
		r, err := service.Audit(ctx, flags.Arg(0), *auditRoot, limits)
		if err != nil {
			return write(r, err, 1)
		}
		return write(r, nil, 0)
	case "bookmark":
		usage := "use bookmark enable/disable/list/save/note/delete/export/import/resume LIBRARY"
		if flags.NArg() != 1 {
			return write(nil, errors.New(usage), 2)
		}
		lib := flags.Arg(0)
		fail := func(data any, err error) int {
			if err != nil {
				switch value := data.(type) {
				case library.BookmarkState:
					if value.LibraryID == "" {
						data = nil
					}
				case library.Bookmark:
					if value.ID == "" {
						data = nil
					}
				case app.BookmarkResume:
					if value.Status == "" {
						data = nil
					}
				}
				return write(data, err, 1)
			}
			return write(data, nil, 0)
		}
		switch action {
		case "enable":
			data, err := service.EnableBookmarks(ctx, lib)
			return fail(data, err)
		case "disable":
			data, err := service.DisableBookmarks(ctx, lib)
			return fail(data, err)
		case "list":
			data, err := service.ListBookmarks(ctx, lib)
			return fail(data, err)
		case "save":
			if contentRequest.AssetID == "" || contentRequest.Representation == "" || *representationID == "" {
				return write(nil, errors.New("use bookmark save LIBRARY --asset sha256:HASH --representation ID --representation-id sha256:HASH --unit N --text-offset N [--cursor TOKEN]"), 2)
			}
			data, err := service.SaveBookmark(ctx, app.BookmarkSave{
				Library: lib, AssetID: contentRequest.AssetID, Representation: contentRequest.Representation,
				RepresentationID: *representationID, Unit: contentRequest.Unit, Offset: contentRequest.Offset, Cursor: contentRequest.Cursor,
			})
			return fail(data, err)
		case "note":
			if contentRequest.AssetID == "" {
				return write(nil, errors.New("use bookmark note LIBRARY --asset sha256:HASH [--text NOTE]"), 2)
			}
			data, err := service.SetBookmarkNote(ctx, lib, contentRequest.AssetID, *findText)
			return fail(data, err)
		case "delete":
			if contentRequest.AssetID == "" {
				return write(nil, errors.New("use bookmark delete LIBRARY --asset sha256:HASH"), 2)
			}
			data, err := service.DeleteBookmark(ctx, lib, contentRequest.AssetID)
			return fail(data, err)
		case "export":
			if *output == "" {
				return write(nil, errors.New("use bookmark export LIBRARY --output FILE"), 2)
			}
			data, err := service.ExportBookmarks(ctx, lib, *output)
			return fail(data, err)
		case "import":
			if *bookmarkInput == "" {
				return write(nil, errors.New("use bookmark import LIBRARY --input FILE [--replace]"), 2)
			}
			data, err := service.ImportBookmarks(ctx, lib, *bookmarkInput, *replaceBookmarks)
			return fail(data, err)
		case "resume":
			if contentRequest.AssetID == "" {
				return write(nil, errors.New("use bookmark resume LIBRARY --asset sha256:HASH"), 2)
			}
			data, err := service.ResumeBookmark(ctx, lib, contentRequest.AssetID)
			return fail(data, err)
		default:
			return write(nil, errors.New(usage), 2)
		}
	case "check":
		if flags.NArg() != 1 {
			return write(nil, errors.New("check requires exactly one file"), 2)
		}
		if err := checkOptions.Validate(); err != nil {
			return write(nil, err, 2)
		}
		data, err := service.Check(ctx, flags.Arg(0), checkOptions)
		if err != nil {
			return guide(write(data, err, 1), data.Antivirus.Status, checkOptions.Scan)
		}
		return guide(write(data, nil, 0), data.Antivirus.Status, checkOptions.Scan)
	case "doctor":
		if flags.NArg() != 0 {
			return write(nil, errors.New("doctor takes no arguments"), 2)
		}
		return write(service.Doctor(p, c, app.Lookup), nil, 0)
	case "catalog":
		if action != "fetch" || flags.NArg() != 1 || flags.Arg(0) != "gutenberg" || *output == "" {
			return write(nil, errors.New("use catalog fetch gutenberg --output FILE"), 2)
		}
		data, err := service.FetchGutenbergCatalog(ctx, *output)
		if err != nil {
			return write(data, err, 1)
		}
		return write(data, nil, 0)
	case "search":
		query := strings.Join(flags.Args(), " ")
		if *source != "openlibrary" && *source != "archive" && *source != "gutenberg" {
			return write(nil, errors.New("search source must be openlibrary, archive, or gutenberg"), 2)
		}
		if *source == "gutenberg" {
			if *sortOrder != discovery.OrderRelevance && *sortOrder != discovery.OrderIdentifier {
				return write(nil, errors.New("gutenberg search order must be relevance or identifier"), 2)
			}
		} else if *sortOrder != discovery.OrderRelevance && ((*sortOrder != discovery.OrderIdentifier && *sortOrder != discovery.OrderDownloads) || *source != "archive") {
			return write(nil, errors.New("search order must be relevance, or identifier or downloads with --source archive"), 2)
		}
		if strings.TrimSpace(query) == "" || len(query) > 1000 || *limit < 1 || *limit > 50 || *offset < 0 || *offset > 10000 {
			return write(nil, errors.New("search requires QUERY, limit 1-50, and offset 0-10000"), 2)
		}
		data, err := service.SearchSourceOrdered(ctx, *source, query, *sortOrder, *limit, *offset)
		if err != nil {
			// Human output is the error alone. JSON still carries the provider
			// value so its fields are escaped beside that error.
			if *jsonMode {
				return write(data, err, 1)
			}
			return write(nil, err, 1)
		}
		return write(data, nil, 0)
	case "inspect":
		if flags.NArg() != 1 {
			return write(nil, errors.New("inspect requires exactly one directory"), 2)
		}
		data, err := service.Inspect(ctx, flags.Arg(0), limits)
		if err != nil {
			return write(data, err, 1)
		}
		return write(data, nil, 0)
	case "tui":
		if *jsonMode || flags.NArg() != 0 {
			return write(nil, errors.New("tui takes no arguments and does not support --json"), 2)
		}
		if !stdinTerminal() {
			return write(nil, errors.New("tui requires an interactive terminal"), 2)
		}
		if err := terminal(ctx, service); err != nil {
			return write(nil, err, 1)
		}
	case "mcp":
		// Stdout belongs to the protocol from here on; diagnostics go to stderr.
		fail := func(err error, code int) int {
			_, _ = fmt.Fprintf(errOut, "nemalo: %s\n", present.Error(err))
			return code
		}
		if action != "serve" || flags.NArg() != 0 || *transport != "stdio" || *jsonMode {
			return fail(errors.New("use mcp serve --transport stdio --scope DIRECTORY"), 2)
		}
		// Configured folders join the scopes when they exist; a folder that has
		// not been created yet is reported and skipped rather than blocking the
		// explicit --scope list.
		for _, configured := range []string{c.Library, c.Review} {
			if configured == "" {
				continue
			}
			if info, err := os.Stat(configured); err != nil || !info.IsDir() {
				_, _ = fmt.Fprintf(errOut, "nemalo: configured folder %q is not an existing directory; not added to MCP scopes\n", configured)
				continue
			}
			scopes = append(scopes, configured)
		}
		server, err := mcpserver.New(service, mcpserver.Options{Scopes: scopes, Network: *allowNetwork, Version: Version})
		if err != nil {
			return fail(err, 2)
		}
		err = server.Run(ctx, &mcp.IOTransport{Reader: io.NopCloser(mcpInput), Writer: nopWriteCloser{out}})
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, mcp.ErrConnectionClosed) && ctx.Err() == nil {
			return fail(err, 1)
		}
	}
	return 0
}

// readTerminal runs the interactive reader; tests replace it.
var readTerminal = func(ctx context.Context, s app.Service, out io.Writer, o tui.ReadOptions) (*reader.Position, error) {
	return tui.Read(ctx, s, os.Stdin, out, o)
}

// stdinTerminal reports whether this process is attached to a terminal.
// Tests replace it. read and tui both need standard input and output.
var stdinTerminal = func() bool {
	return term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd())
}

// mcpInput is the protocol input stream; tests replace it.
var mcpInput io.Reader = os.Stdin

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// multiFlag collects a repeatable string flag.
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }

func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

func flagApplies(command, action, name string) bool {
	if name == "json" {
		return true
	}
	if command == "content" && action == "capabilities" {
		return false
	}
	if command == "read" {
		return name == "root" || name == "asset" || name == "unit" || name == "text-offset" || name == "speed" || name == "rate"
	}
	if command == "mcp" {
		return name == "scope" || name == "allow-network" || name == "transport" || name == "config" || name == "library" || name == "review"
	}
	if command == "version" {
		return false
	}
	if command == "bookmark" {
		switch action {
		case "save":
			return name == "asset" || name == "representation" || name == "representation-id" || name == "unit" || name == "text-offset" || name == "cursor"
		case "note":
			return name == "asset" || name == "text"
		case "delete", "resume":
			return name == "asset"
		case "export":
			return name == "output"
		case "import":
			return name == "input" || name == "replace"
		}
		return false
	}
	if name == "config" || name == "library" || name == "review" {
		return true
	}
	if command == "content" {
		if action != "units" && action != "read" && action != "source" && action != "resource" && action != "locate" && action != "navigation" {
			return false
		}
		if name == "root" || name == "asset" {
			return true
		}
		if action == "navigation" {
			return name == "representation" || name == "navigation"
		}
		if name == "navigation" {
			return action == "read" || action == "units"
		}
		if name == "representation" {
			return action == "read" || action == "units" || action == "locate"
		}
		if name == "text" {
			return action == "locate"
		}
		if action == "locate" {
			return false
		}
		if name == "max-bytes" || name == "cursor" {
			return action != "units"
		}
		return (action == "read" || action == "source") && (name == "part" || name == "unit" || name == "text-offset") || (action == "read" && name == "sample")
	}
	if command == "library" {
		switch action {
		case "snapshot":
			return name == "output" || name == "assess" || strings.HasPrefix(name, "max-")
		case "list":
			return name == "query" || name == "format" || name == "limit" || name == "offset" || name == "status"
		case "import":
			return name == "apply" || name == "scan"
		case "rescan":
			return name == "apply"
		case "ingest":
			return name == "apply" || name == "scan"
		case "recordings":
			return name == "source"
		case "check":
			return false
		case "cleanup":
			return name == "apply" || name == "plan" || name == "stop"
		case "restore":
			return name == "apply" || name == "cleanup" || name == "stop"
		case "open":
			return name == "asset" || name == "app"
		case "export":
			return name == "asset" || name == "output"
		case "audit":
			return name == "root" || strings.HasPrefix(name, "max-")
		}
	}
	switch command {
	case "acquire":
		return name == "file" || name == "output" || name == "max-file-bytes" || name == "no-scan" || name == "on-detection" || name == "recording" || name == "resume" || name == "max-recording-bytes"
	case "search":
		return name == "limit" || name == "offset" || name == "source" || name == "sort" || name == "catalog"
	case "evaluate":
		return name == "catalog" || name == "files" || name == "editions" || name == "limit" || name == "offset"
	case "tui":
		return name == "catalog"
	case "catalog":
		return action == "fetch" && name == "output"
	case "check":
		return name == "scan" || name == "expected-bytes" || name == "expected-sha256"
	case "inspect":
		return name == "hashes" || strings.HasPrefix(name, "max-")
	}
	return false
}

func visited(flags *flag.FlagSet, name string) bool {
	seen := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == name {
			seen = true
		}
	})
	return seen
}

// parse accepts flags before or after positional arguments, preserving an explicit --.
func parse(flags *flag.FlagSet, args []string) error {
	var options, positionals []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if len(a) < 2 || a[0] != '-' {
			positionals = append(positionals, a)
			continue
		}
		options = append(options, a)
		name := strings.TrimLeft(strings.SplitN(a, "=", 2)[0], "-")
		f := flags.Lookup(name)
		if f == nil || strings.Contains(a, "=") {
			continue
		}
		if boolean, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && boolean.IsBoolFlag() {
			continue
		}
		if i+1 >= len(args) {
			return fmt.Errorf("flag --%s requires a value", name)
		}
		i++
		options = append(options, args[i])
	}
	return flags.Parse(append(append(options, "--"), positionals...))
}

// started reports whether a failed command's result shows something that
// happened, such as bytes kept for --resume, a detection, or journal state. A
// result of empty values is left out of human output, so the error stands
// alone; --json still carries it.
func started(data any) bool {
	switch v := data.(type) {
	case nil:
		return false
	case acquisition.Result:
		return v.Output != "" || v.Transfer.Bytes > 0
	case library.State:
		return v.LibraryID != "" || v.Created || v.Resumed || (v.Status != "unavailable" && v.Status != "")
	case library.ImportResult:
		return v.Applied || v.Resumed || v.AssetID != ""
	case discovery.CatalogFile:
		return v.Path != ""
	}
	return true
}

func printData(out io.Writer, data any) error {
	switch value := data.(type) {
	case acquisition.Result:
		_, err := io.WriteString(out, present.Acquisition(value))
		return err
	case acquisition.RecordingResult:
		_, err := io.WriteString(out, present.Recording(value))
		return err
	case library.RecordingsResult:
		_, err := io.WriteString(out, present.Recordings(value))
		return err
	case library.CleanupResult:
		_, err := io.WriteString(out, present.Cleanup(value))
		return err
	case library.LibraryCheck:
		_, err := io.WriteString(out, present.LibraryCheck(value))
		return err
	case library.RestoreResult:
		_, err := io.WriteString(out, present.Restore(value))
		return err
	case library.State:
		_, err := io.WriteString(out, present.LibraryState(value))
		return err
	case discovery.Evaluation:
		_, err := io.WriteString(out, present.Evaluation(value))
		return err
	case library.Snapshot:
		_, err := io.WriteString(out, present.Snapshot(value))
		return err
	case library.Page:
		_, err := io.WriteString(out, present.Holdings(value))
		return err
	case library.ManagedPage:
		_, err := io.WriteString(out, present.ManagedHoldings(value))
		return err
	case library.Audit:
		_, err := io.WriteString(out, present.Audit(value))
		return err
	case library.ImportResult:
		_, err := io.WriteString(out, present.Import(value))
		return err
	case library.ManagedAudit:
		_, err := io.WriteString(out, present.ManagedAudit(value))
		return err
	case library.RescanResult:
		_, err := io.WriteString(out, present.Rescan(value))
		return err
	case library.ExportResult:
		_, err := io.WriteString(out, present.Export(value))
		return err
	case library.IngestResult:
		_, err := io.WriteString(out, present.Ingest(value))
		return err
	case assessment.Report:
		_, err := io.WriteString(out, present.Health(value))
		return err
	case discovery.Page:
		_, err := io.WriteString(out, present.Search(value))
		return err
	case discovery.CatalogFile:
		_, err := io.WriteString(out, present.Catalog(value))
		return err
	case content.Result:
		_, err := io.WriteString(out, present.ContentCommand(value))
		return err
	case library.BookmarkState:
		_, err := io.WriteString(out, present.Bookmarks(value))
		return err
	case library.Bookmark:
		_, err := io.WriteString(out, present.Bookmark(value))
		return err
	case app.BookmarkResume:
		_, err := io.WriteString(out, present.BookmarkResume(value.Status, value.Detail, value.Bookmark, value.Content))
		return err
	case inventory.Report:
		_, err := io.WriteString(out, present.Inventory(value))
		return err
	}
	if value, ok := data.(string); ok {
		_, err := fmt.Fprintln(out, value)
		return err
	}
	return textsafe.WriteJSON(out, data, true)
}

// recordingAntivirus summarizes track scans for the next-step hint: any track
// without a no-detection result means scan evidence is missing.
func recordingAntivirus(r acquisition.RecordingResult) string {
	for _, t := range r.Tracks {
		if t.Antivirus != "no_detections_reported" {
			return t.Antivirus
		}
	}
	return "no_detections_reported"
}
