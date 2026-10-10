// Package cli implements the scriptable terminal interface over application services.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/blisspixel/nemalo/internal/acquisition"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/config"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/present"
	"github.com/blisspixel/nemalo/internal/textsafe"
	"github.com/blisspixel/nemalo/internal/tui"
)

const Version = "0.1.0-alpha.2"

const help = `Nemalo
Find knowledge. Care for it. Realize its potential.

Usage: nemalo <command> [options]

  tui                 Open the interactive terminal interface
  doctor              Report configuration and optional tool availability
  search QUERY        Search a selected catalog (explicit network access)
  evaluate archive:ITEM
                      Inspect source-declared files, rights, and access restrictions
  acquire archive:ITEM --file NAME --output NEW_DIRECTORY
                      Download a selected EPUB/PDF/MP3 into untrusted intake
  inspect DIRECTORY   Read-only inventory of an explicitly selected folder
  check FILE          Bounded file health assessment, without rendering content
  library snapshot DIRECTORY --output FILE
                      Save a new portable byte-identity catalog outside the root
  library list CATALOG [--query TEXT]
                      Browse local file holdings and optional EPUB metadata
  library import LIBRARY SOURCE [--apply] [--scan]
                      Assess one EPUB, PDF, MP3, or intake packet; --apply stores it
  library audit CATALOG --root DIRECTORY
                      Compare a snapshot catalog with files under an explicit root
  library audit LIBRARY
                      Compare managed holdings with their stored bytes
  library init DIRECTORY
                      Initialize local control state in an existing explicit folder
  library status DIRECTORY
                      Read control identity and journal state without creating files
  content units CATALOG --root DIRECTORY --asset sha256:HASH
                      List supported EPUB reading-order documents, offline
  content read CATALOG --root DIRECTORY --asset sha256:HASH
                      Retrieve bounded source text with exact continuation, offline
  content source CATALOG --root DIRECTORY --asset sha256:HASH
                      Read bounded original EPUB XML, optionally --part ID
  content resource CATALOG --root DIRECTORY --asset sha256:HASH --cursor TOKEN
                      Read referenced local image bytes as base64; never render
  content capabilities
                      Report supported representation versions and limits
  version             Show the development version
  help                Show this help

Shared options: --json, --config FILE, --library DIRECTORY, --review DIRECTORY
Search options: --source openlibrary|archive (default openlibrary), --limit 10, --offset 0
Acquire options: --file exact-source-filename, --output new-directory,
                 --max-file-bytes 268435456 (maximum 256 MiB)
Acquisition retains incomplete output, never overwrites or resumes it, and does
not scan antivirus or publish to a checked library. Rights remain unresolved.
Inspect options: --hashes, --max-entries 10000, --max-depth 32,
                 --max-file-bytes 268435456, --max-total-bytes 1073741824
Check options: --scan, --expected-bytes N, --expected-sha256 HASH
Library snapshot options: --output FILE, --assess (local health/EPUB metadata)
Library list options: --query TEXT, --format all|epub|pdf|mp3, --limit 10, --offset 0
Library import options: --apply, --scan
Library audit options: --root DIRECTORY for a snapshot catalog
Content read options: --unit 0, --text-offset 0, --max-bytes 4096, --cursor TOKEN
                     --representation epub-text/1|epub-structure/1, --part ID
Content extraction is plain text, not rendered layout or acknowledgement of reading.
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
	if (command == "library" || command == "content") && len(arguments) > 0 {
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
	limits := inventory.Defaults()
	checkOptions := assessment.Options{}
	output := flags.String("output", "", "new snapshot output path")
	sourceFile := flags.String("file", "", "exact selected source filename")
	query := flags.String("query", "", "literal holdings metadata filter")
	format := flags.String("format", "all", "exact holdings filename suffix filter")
	auditRoot := flags.String("root", "", "explicit audit root")
	assess := flags.Bool("assess", false, "include local health facts without antivirus")
	apply := flags.Bool("apply", false, "store the assessed import")
	contentRequest := content.Request{SchemaVersion: 1}
	flags.StringVar(&contentRequest.AssetID, "asset", "", "exact catalog asset ID")
	flags.IntVar(&contentRequest.Unit, "unit", 0, "zero-based reading-order document")
	flags.IntVar(&contentRequest.Offset, "text-offset", 0, "zero-based UTF-8 text byte offset")
	flags.IntVar(&contentRequest.MaxBytes, "max-bytes", 4096, "maximum returned text bytes")
	flags.StringVar(&contentRequest.Cursor, "cursor", "", "source-bound continuation token")
	flags.StringVar(&contentRequest.Representation, "representation", "epub-text/1", "negotiated content representation")
	flags.StringVar(&contentRequest.Part, "part", "", "exact source part ID")
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
			if data != nil {
				if outputErr := printData(out, data); outputErr != nil {
					return 1
				}
			}
			_, _ = fmt.Fprintf(errOut, "nemalo: %q\n", err.Error())
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
	if command != "doctor" && command != "search" && command != "evaluate" && command != "acquire" && command != "inspect" && command != "check" && command != "tui" && command != "library" && command != "content" {
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
	c, err := config.Load(*configFile, explicit, env, config.Config{Library: *libraryDir, Review: *review})
	if err != nil {
		return write(nil, err, 2)
	}
	switch command {
	case "acquire":
		if flags.NArg() != 1 {
			return write(nil, errors.New("use acquire archive:ITEM --file NAME --output NEW_DIRECTORY"), 2)
		}
		request := acquisition.Request{SourceID: flags.Arg(0), File: *sourceFile, Output: *output, MaxBytes: limits.FileBytes}
		if err := request.Validate(); err != nil {
			return write(nil, err, 2)
		}
		data, err := service.Acquire(ctx, request)
		if err != nil {
			return write(data, err, 1)
		}
		return write(data, nil, 0)
	case "content":
		if flags.NArg() != 1 || (action != "units" && action != "read" && action != "source" && action != "resource") {
			return write(nil, errors.New("use content units/read/source/resource CATALOG --root DIRECTORY --asset sha256:HASH"), 2)
		}
		contentRequest.Catalog, contentRequest.Root, contentRequest.UnitsOnly = flags.Arg(0), *auditRoot, action == "units"
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
	case "evaluate":
		if flags.NArg() != 1 || !discovery.ValidArchiveID(flags.Arg(0)) {
			return write(nil, errors.New("evaluate requires exactly one archive:ITEM identifier"), 2)
		}
		data, err := service.Evaluate(ctx, flags.Arg(0))
		if err != nil {
			return write(data, err, 1)
		}
		return write(data, nil, 0)
	case "library":
		if action == "import" {
			if flags.NArg() != 2 {
				return write(nil, errors.New("use library import LIBRARY SOURCE"), 2)
			}
			data, err := service.Import(ctx, library.ImportRequest{Library: flags.Arg(0), Source: flags.Arg(1), Scan: checkOptions.Scan, Apply: *apply})
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
	case "check":
		if flags.NArg() != 1 {
			return write(nil, errors.New("check requires exactly one file"), 2)
		}
		if err := checkOptions.Validate(); err != nil {
			return write(nil, err, 2)
		}
		data, err := service.Check(ctx, flags.Arg(0), checkOptions)
		if err != nil {
			return write(data, err, 1)
		}
		return write(data, nil, 0)
	case "doctor":
		if flags.NArg() != 0 {
			return write(nil, errors.New("doctor takes no arguments"), 2)
		}
		return write(service.Doctor(p, c, app.Lookup), nil, 0)
	case "search":
		query := strings.Join(flags.Args(), " ")
		if *source != "openlibrary" && *source != "archive" {
			return write(nil, errors.New("search source must be openlibrary or archive"), 2)
		}
		if strings.TrimSpace(query) == "" || len(query) > 1000 || *limit < 1 || *limit > 50 || *offset < 0 || *offset > 10000 {
			return write(nil, errors.New("search requires QUERY, limit 1-50, and offset 0-10000"), 2)
		}
		data, err := service.SearchSource(ctx, *source, query, *limit, *offset)
		if err != nil {
			return write(data, err, 1)
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
		if err := terminal(ctx, service); err != nil {
			return write(nil, err, 1)
		}
	}
	return 0
}

func flagApplies(command, action, name string) bool {
	if name == "json" {
		return true
	}
	if command == "content" && action == "capabilities" {
		return false
	}
	if command == "version" {
		return false
	}
	if name == "config" || name == "library" || name == "review" {
		return true
	}
	if command == "content" {
		if action != "units" && action != "read" && action != "source" && action != "resource" {
			return false
		}
		if name == "root" || name == "asset" {
			return true
		}
		if name == "representation" {
			return action == "read" || action == "units"
		}
		if name == "max-bytes" || name == "cursor" {
			return action != "units"
		}
		return (action == "read" || action == "source") && (name == "part" || name == "unit" || name == "text-offset")
	}
	if command == "library" {
		switch action {
		case "snapshot":
			return name == "output" || name == "assess" || strings.HasPrefix(name, "max-")
		case "list":
			return name == "query" || name == "format" || name == "limit" || name == "offset"
		case "import":
			return name == "apply" || name == "scan"
		case "audit":
			return name == "root" || strings.HasPrefix(name, "max-")
		}
	}
	switch command {
	case "acquire":
		return name == "file" || name == "output" || name == "max-file-bytes"
	case "search":
		return name == "limit" || name == "offset" || name == "source"
	case "check":
		return name == "scan" || name == "expected-bytes" || name == "expected-sha256"
	case "inspect":
		return name == "hashes" || strings.HasPrefix(name, "max-")
	}
	return false
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

func printData(out io.Writer, data any) error {
	switch value := data.(type) {
	case acquisition.Result:
		_, err := io.WriteString(out, present.Acquisition(value))
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
	case library.Audit:
		_, err := io.WriteString(out, present.Audit(value))
		return err
	case library.ImportResult:
		_, err := io.WriteString(out, present.Import(value))
		return err
	case library.ManagedAudit:
		_, err := io.WriteString(out, present.ManagedAudit(value))
		return err
	case assessment.Report:
		_, err := io.WriteString(out, present.Health(value))
		return err
	case discovery.Page:
		_, err := io.WriteString(out, present.Search(value))
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
