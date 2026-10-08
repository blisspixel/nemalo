// Package cli implements the scriptable terminal interface over application services.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/config"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/present"
	"github.com/blisspixel/nemalo/internal/tui"
)

const Version = "0.1.0-dev"

const help = `Nemalo
Find knowledge. Care for it. Put it to work.

Usage: nemalo <command> [options]

  tui                 Open the interactive terminal interface
  doctor              Report configuration and optional tool availability
  search QUERY        Search Open Library bibliographic metadata (network access)
  inspect DIRECTORY   Read-only inventory of an explicitly selected folder
  check FILE          Bounded file health assessment, without rendering content
  library snapshot DIRECTORY --output FILE
                      Save a new portable byte-identity catalog outside the root
  library list CATALOG [--query TEXT]
                      Browse local file holdings and optional EPUB metadata
  library audit CATALOG --root DIRECTORY
                      Report changed, missing, added, and unverified file locations
  version             Show the development version
  help                Show this help

Shared options: --json, --config FILE, --library DIRECTORY, --review DIRECTORY
Search options: --limit 10, --offset 0
Inspect options: --hashes, --max-entries 10000, --max-depth 32,
                 --max-file-bytes 268435456, --max-total-bytes 1073741824
Check options: --scan, --expected-bytes N, --expected-sha256 HASH
Library snapshot options: --output FILE, --assess (local health/EPUB metadata)
Library list options: --query TEXT, --limit 10, --offset 0
Library audit options: --root DIRECTORY
Snapshot/audit also accept inventory max-* limits. Snapshots hash all regular files,
account for excluded links, refuse incomplete inventories, and never overwrite.
Audit always requires an explicit root; the catalog cannot authorize a scan.

Check uses a private temporary snapshot (up to 256 MiB). EPUB text/document counts
are measured; fixed pages, PDF page counts, and audio duration are not inferred.
--scan invokes installed antivirus without remediation. External scanner cloud
and sample-submission settings apply. Missing/failed scans cannot mean clean.

Inventory does not extract archives, validate books, or scan for malware.
Production downloads, checked publication, cleanup, MCP, and audiobook management
are planned. PDF/MP3 checks currently establish candidate signatures only.
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
	if command == "library" && len(arguments) > 0 {
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
	library := flags.String("library", "", "explicit library directory")
	review := flags.String("review", "", "explicit review directory")
	limit, offset := flags.Int("limit", 10, "search page size"), flags.Int("offset", 0, "search offset")
	limits := inventory.Defaults()
	checkOptions := assessment.Options{}
	output := flags.String("output", "", "new snapshot output path")
	query := flags.String("query", "", "literal holdings metadata filter")
	auditRoot := flags.String("root", "", "explicit audit root")
	assess := flags.Bool("assess", false, "include local health facts without antivirus")
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
			if encodeErr := json.NewEncoder(out).Encode(e); encodeErr != nil {
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
	if command != "doctor" && command != "search" && command != "inspect" && command != "check" && command != "tui" && command != "library" {
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
	c, err := config.Load(*configFile, explicit, env, config.Config{Library: *library, Review: *review})
	if err != nil {
		return write(nil, err, 2)
	}
	switch command {
	case "library":
		if flags.NArg() != 1 || (action != "snapshot" && action != "list" && action != "audit") {
			return write(nil, errors.New("use library snapshot DIRECTORY, library list CATALOG, or library audit CATALOG"), 2)
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
			r, err := service.Holdings(flags.Arg(0), *query, *limit, *offset)
			if err != nil {
				return write(r, err, 1)
			}
			return write(r, nil, 0)
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
		if strings.TrimSpace(query) == "" || len(query) > 1000 || *limit < 1 || *limit > 50 || *offset < 0 || *offset > 10000 {
			return write(nil, errors.New("search requires QUERY, limit 1-50, and offset 0-10000"), 2)
		}
		data, err := service.Search(ctx, query, *limit, *offset)
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
	if command == "version" {
		return false
	}
	if name == "config" || name == "library" || name == "review" {
		return true
	}
	if command == "library" {
		switch action {
		case "snapshot":
			return name == "output" || name == "assess" || strings.HasPrefix(name, "max-")
		case "list":
			return name == "query" || name == "limit" || name == "offset"
		case "audit":
			return name == "root" || strings.HasPrefix(name, "max-")
		}
	}
	switch command {
	case "search":
		return name == "limit" || name == "offset"
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
	case library.Snapshot:
		_, err := io.WriteString(out, present.Snapshot(value))
		return err
	case library.Page:
		_, err := io.WriteString(out, present.Holdings(value))
		return err
	case library.Audit:
		_, err := io.WriteString(out, present.Audit(value))
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
	// JSON escaping keeps untrusted filenames and provider strings from emitting terminal controls.
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}
