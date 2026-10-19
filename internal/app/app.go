// Package app owns shared application operations for terminal and structured interfaces.
package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/blisspixel/nemalo/internal/acquisition"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/config"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/launch"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/safeio"
	"github.com/blisspixel/nemalo/internal/scanner"
)

type Searcher interface {
	Search(context.Context, string, int, int) (discovery.Page, error)
}

// Acquire scans verified intake bytes with the installed scanner unless the
// request opts out. Detected files go to the service quarantine directory.
func (s Service) Acquire(ctx context.Context, request acquisition.Request) (acquisition.Result, error) {
	provider, err := s.acquireProvider(request.SourceID)
	if err != nil {
		return acquisition.Result{}, err
	}
	if !request.NoScan && request.Scanner == nil {
		request.Scanner = s.scanner()
	}
	if request.Quarantine == "" {
		request.Quarantine = s.Quarantine
	}
	if request.OnDetection == "" {
		request.OnDetection = s.OnDetection
	}
	return acquisition.Acquire(ctx, provider, request)
}

// acquireProvider selects Archive for archive:ITEM and the bibliographic
// record for gutenberg:NUMBER. Recordings stay on Archive.
func (s Service) acquireProvider(id string) (acquisition.Provider, error) {
	if discovery.ValidGutenbergID(id) {
		g, ok := s.Providers["gutenberg"].(*discovery.Gutenberg)
		if !ok || g == nil {
			return nil, errors.New("gutenberg acquisition is unavailable")
		}
		return discovery.Files{Gutenberg: g}, nil
	}
	provider, ok := s.Providers["archive"].(acquisition.Provider)
	if !ok {
		return nil, errors.New("archive acquisition is unavailable")
	}
	return provider, nil
}

// AcquireRecording acquires every track of one declared recording with the
// same scan and detection policy as Acquire.
func (s Service) AcquireRecording(ctx context.Context, request acquisition.RecordingRequest) (acquisition.RecordingResult, error) {
	provider, ok := s.Providers["archive"].(acquisition.Provider)
	if !ok {
		return acquisition.RecordingResult{}, errors.New("archive acquisition is unavailable")
	}
	if !request.NoScan && request.Scanner == nil {
		request.Scanner = s.scanner()
	}
	if request.Quarantine == "" {
		request.Quarantine = s.Quarantine
	}
	if request.OnDetection == "" {
		request.OnDetection = s.OnDetection
	}
	return acquisition.AcquireRecording(ctx, provider, request)
}

type Service struct {
	Providers map[string]Searcher
	Scanner   assessment.Scanner
	// Quarantine is the Nemalo-owned directory for detected downloads, under
	// the platform state directory. It is created only when a file is detected.
	Quarantine string
	// OnDetection is the configured detection policy for requests that do not
	// name one: "quarantine" (also when empty) or "delete".
	OnDetection string
	// Handoff is the Nemalo-owned directory for read-only reader copies.
	Handoff string
	// Launch opens a file with app, or the default application when app is
	// empty. Nil uses the platform launcher; tests substitute it.
	Launch func(ctx context.Context, app, file string) error
}

// Export copies a holding into an explicit directory, verified and never
// overwriting.
func (s Service) Export(ctx context.Context, request library.ExportRequest) (library.ExportResult, error) {
	return library.Export(ctx, request)
}

// Open hands a reader or player a verified read-only copy in the handoff
// directory, never the stored file, then launches the application on it.
func (s Service) Open(ctx context.Context, libraryDir, assetID, app string) (library.ExportResult, error) {
	if s.Handoff == "" {
		return library.ExportResult{}, errors.New("open requires a handoff directory")
	}
	if err := os.MkdirAll(s.Handoff, 0o700); err != nil {
		return library.ExportResult{}, err
	}
	r, err := library.Export(ctx, library.ExportRequest{Library: libraryDir, AssetID: assetID, Directory: s.Handoff, ReadOnly: true, Reuse: true})
	if err != nil {
		return r, err
	}
	launcher := s.Launch
	if launcher == nil {
		launcher = func(ctx context.Context, app, file string) error {
			if app != "" {
				return launch.With(ctx, app, file)
			}
			return launch.Default(ctx, file)
		}
	}
	if err := launcher(ctx, app, r.Path); err != nil {
		return r, fmt.Errorf("the verified copy is at %s, but the application did not start: %w", r.Path, err)
	}
	r.Launched = app
	if app == "" {
		r.Launched = "default application"
	}
	return r, nil
}

func (s Service) scanner() assessment.Scanner {
	if s.Scanner != nil {
		return s.Scanner
	}
	return scanner.New()
}

// Ingest imports every supported file, packet, and ZIP member of a folder.
func (s Service) Ingest(ctx context.Context, request library.IngestRequest) (library.IngestResult, error) {
	if request.Scan && request.Scanner == nil {
		request.Scanner = s.scanner()
	}
	return library.Ingest(ctx, request)
}

// Rescan scans review holdings whose only gap is scan evidence.
func (s Service) Rescan(ctx context.Context, request library.RescanRequest) (library.RescanResult, error) {
	if request.Scanner == nil {
		request.Scanner = s.scanner()
	}
	return library.Rescan(ctx, request)
}

// ScanGuidance is a one-line next step after a check, import, or acquisition
// that has no usable scan evidence, or "" when none applies. A missing scanner
// gets the platform install command; an available but unused one gets --scan.
func (s Service) ScanGuidance(antivirus string, scanRequested bool) string {
	switch {
	case antivirus == "unavailable":
		return scanner.HostSetupHint(runtime.GOOS)
	case antivirus == "incomplete":
		return "The scan did not complete, so the file is treated as unscanned. Repeat it with check FILE --scan."
	case antivirus == "not_scanned" && !scanRequested:
		if kind, err := s.scannerKind(); err == nil {
			return "Add --scan to scan this file with " + scannerName(kind) + "."
		}
		return scanner.HostSetupHint(runtime.GOOS)
	}
	return ""
}

func (s Service) scannerKind() (string, error) {
	if k, ok := s.scanner().(interface{ Kind() (string, error) }); ok {
		return k.Kind()
	}
	return "installed antivirus", nil
}

func scannerName(kind string) string {
	switch kind {
	case "defender":
		return "Microsoft Defender"
	case "clamav":
		return "ClamAV"
	}
	return kind
}

func New() Service {
	return Service{Providers: map[string]Searcher{
		"openlibrary": discovery.NewOpenLibrary(),
		"archive":     archiveWithLibriVox(),
		"gutenberg":   discovery.NewGutenberg(),
	}}
}

// UseGutenbergCatalog points the Gutenberg provider at a local catalog.
// A service without that provider, or an empty path, leaves the provider unchanged.
func (s Service) UseGutenbergCatalog(path string) {
	g, ok := s.Providers["gutenberg"].(*discovery.Gutenberg)
	if !ok || strings.TrimSpace(path) == "" {
		return
	}
	g.Catalog = path
}

// SearchGutenberg reads one local catalog for this call. It does not change
// the shared provider and does not contact gutenberg.org.
func (s Service) SearchGutenberg(ctx context.Context, catalog, query, order string, limit, offset int) (discovery.Page, error) {
	if _, ok := s.Providers["gutenberg"]; !ok {
		return discovery.Page{}, errors.New("search provider unavailable")
	}
	return (&discovery.Gutenberg{Catalog: catalog}).SearchOrdered(ctx, query, order, limit, offset)
}

// FetchGutenbergCatalog downloads the official catalog to a new file.
func (s Service) FetchGutenbergCatalog(ctx context.Context, output string) (discovery.CatalogFile, error) {
	return discovery.FetchCatalog(ctx, output)
}

// EvaluateGutenbergFiles fetches one bibliographic record and lists file offers.
// It does not download an ebook. Catalog evaluation does not call it.
func (s Service) EvaluateGutenbergFiles(ctx context.Context, id string) (discovery.Evaluation, error) {
	g, ok := s.Providers["gutenberg"].(*discovery.Gutenberg)
	if !ok || g == nil {
		return discovery.Evaluation{}, errors.New("gutenberg evaluation is unavailable")
	}
	return g.EvaluateFiles(ctx, id)
}

func (s Service) Search(ctx context.Context, query string, limit, offset int) (discovery.Page, error) {
	return s.SearchSource(ctx, "openlibrary", query, limit, offset)
}

func (s Service) SearchSource(ctx context.Context, source, query string, limit, offset int) (discovery.Page, error) {
	return s.SearchSourceOrdered(ctx, source, query, discovery.OrderRelevance, limit, offset)
}

// OrderedSearcher is implemented by providers offering an order besides relevance.
type OrderedSearcher interface {
	SearchOrdered(context.Context, string, string, int, int) (discovery.Page, error)
}

// SearchSourceOrdered uses provider relevance unless another supported order is
// requested. An unsupported order fails rather than silently reverting.
func (s Service) SearchSourceOrdered(ctx context.Context, source, query, order string, limit, offset int) (discovery.Page, error) {
	p, ok := s.Providers[source]
	if !ok || p == nil {
		return discovery.Page{}, errors.New("search provider unavailable")
	}
	if order == "" || order == discovery.OrderRelevance {
		return p.Search(ctx, query, limit, offset)
	}
	ordered, ok := p.(OrderedSearcher)
	if !ok {
		return discovery.Page{}, errors.New("search provider supports relevance order only")
	}
	return ordered.SearchOrdered(ctx, query, order, limit, offset)
}

func (s Service) Evaluate(ctx context.Context, id string) (discovery.Evaluation, error) {
	if discovery.ValidOpenLibraryEdition(id) {
		return s.evaluateOpenLibraryEdition(ctx, id)
	}
	if discovery.ValidOpenLibraryWork(id) {
		return discovery.Evaluation{}, errors.New("an Open Library work has no selected edition; list editions before choosing an Archive item")
	}
	source, _, ok := strings.Cut(id, ":")
	if !ok {
		return discovery.Evaluation{}, errors.New("evaluation requires a namespaced item identifier")
	}
	p, ok := s.Providers[source].(interface {
		Evaluate(context.Context, string) (discovery.Evaluation, error)
	})
	if !ok {
		return discovery.Evaluation{}, errors.New("provider does not support item evaluation")
	}
	return p.Evaluate(ctx, id)
}

// EvaluateOpenLibraryEditions lists one page of editions for a work.
// It does not contact Internet Archive.
func (s Service) EvaluateOpenLibraryEditions(ctx context.Context, id string, limit, offset int) (discovery.Evaluation, error) {
	library, ok := s.Providers["openlibrary"].(*discovery.OpenLibrary)
	if !ok || library == nil {
		return discovery.Evaluation{}, errors.New("open library evaluation is unavailable")
	}
	return library.ListEditions(ctx, id, limit, offset)
}

func (s Service) evaluateOpenLibraryEdition(ctx context.Context, id string) (discovery.Evaluation, error) {
	library, ok := s.Providers["openlibrary"].(*discovery.OpenLibrary)
	if !ok || library == nil {
		return discovery.Evaluation{}, errors.New("open library evaluation is unavailable")
	}
	record, err := library.ReadEdition(ctx, id)
	if err != nil {
		return discovery.Evaluation{}, err
	}
	if record.ArchiveID == "" {
		return editionWithoutArchive(record), nil
	}
	archive, ok := s.Providers["archive"].(*discovery.Archive)
	if !ok || archive == nil {
		return discovery.Evaluation{}, errors.New("archive evaluation is unavailable")
	}
	evaluation, err := archive.Evaluate(ctx, record.ArchiveID)
	if err != nil {
		return discovery.Evaluation{}, err
	}
	evaluation.OpenLibraryEdition = record.ID
	note := fmt.Sprintf("Open Library edition %s names Archive item %s through ocaid. Work-level identifiers were not used.", record.ID, record.ArchiveID)
	if len(record.Languages) > 0 {
		note += " Edition languages: " + strings.Join(record.Languages, ", ") + "."
	}
	notes := []string{note, "The Open Library edition record SHA-256 is " + record.MetadataSHA256 + "."}
	if record.Title != "" && (len(evaluation.Title) == 0 || evaluation.Title[0] != record.Title) {
		notes = append(notes, "Open Library edition title: "+record.Title+".")
	}
	if len(record.OtherArchiveIDs) > 0 {
		notes = append(notes, "Other Archive identifiers in this edition's source records were not selected: "+strings.Join(record.OtherArchiveIDs, ", ")+".")
	}
	notes = append(notes, record.Findings...)
	evaluation.Limitations = append(notes, evaluation.Limitations...)
	return evaluation, nil
}

func editionWithoutArchive(record discovery.EditionRecord) discovery.Evaluation {
	titles := []string{}
	if record.Title != "" {
		titles = []string{record.Title}
	}
	limitations := []string{"This Open Library edition does not name an Archive item in ocaid. No Archive identifier was selected from the work or from source records."}
	if len(record.OtherArchiveIDs) > 0 {
		limitations = append(limitations, "Other Archive identifiers in this edition's source records were not selected: "+strings.Join(record.OtherArchiveIDs, ", ")+".")
	}
	limitations = append(limitations, record.Findings...)
	return discovery.Evaluation{
		Complete: true, ID: record.ID, Source: "openlibrary", Title: titles, Languages: record.Languages,
		Dates: record.Dates, Publishers: record.Publishers, LandingPage: record.LandingPage,
		MetadataURL: record.MetadataURL, MetadataSHA256: record.MetadataSHA256, CheckedAt: time.Now().UTC(),
		Access: discovery.OpenLibraryEditionRecord, Restrictions: map[string]string{}, Files: []discovery.OfferedFile{},
		Limitations: limitations,
	}
}

func (s Service) Inspect(ctx context.Context, root string, limits inventory.Limits) (inventory.Report, error) {
	return inventory.Inspect(ctx, root, limits)
}

func (s Service) Check(ctx context.Context, file string, options assessment.Options) (assessment.Report, error) {
	return assessment.Check(ctx, file, options, s.Scanner)
}

func (s Service) Snapshot(ctx context.Context, directory, output string, limits inventory.Limits, assess bool) (library.Snapshot, error) {
	if output == "" {
		return library.Snapshot{}, errors.New("snapshot requires an explicit output path")
	}
	if err := library.CheckDestination(directory, output); err != nil {
		return library.Snapshot{}, err
	}
	r, err := library.Build(ctx, directory, limits, assess)
	if err != nil {
		return r, err
	}
	err = library.Save(ctx, output, &r)
	return r, err
}

func (s Service) Holdings(file, query string, limit, offset int) (library.Page, error) {
	return s.HoldingsFormat(file, query, "all", limit, offset)
}

func (s Service) HoldingsFormat(file, query, format string, limit, offset int) (library.Page, error) {
	c, err := library.Load(file)
	if err != nil {
		return library.Page{}, err
	}
	return library.FindFormat(c, query, format, limit, offset)
}

// ManagedHoldings lists an initialized library's current holdings.
// Recordings lists held recordings; a source filter also verifies and indexes
// each held track's stored bytes.
func (s Service) Recordings(ctx context.Context, directory, source string) (library.RecordingsResult, error) {
	return library.Recordings(ctx, directory, source)
}

// CheckLibrary verifies a managed library read-only: stored and held bytes,
// untracked store files, and unfinished operations.
func (s Service) CheckLibrary(ctx context.Context, directory string) (library.LibraryCheck, error) {
	return library.CheckLibrary(ctx, directory)
}

// Cleanup plans, or with a plan digest applies, moving held sources of a
// folder into the library's recoverable holding area.
func (s Service) Cleanup(ctx context.Context, request library.CleanupRequest) (library.CleanupResult, error) {
	return library.Cleanup(ctx, request)
}

// Purge removes a completed cleanup's held copies that the library holds.
func (s Service) Purge(ctx context.Context, request library.PurgeRequest) (library.PurgeResult, error) {
	return library.Purge(ctx, request)
}

// Restore returns a cleanup's held files to their folder without replacing anything.
func (s Service) Restore(ctx context.Context, request library.RestoreRequest) (library.RestoreResult, error) {
	return library.Restore(ctx, request)
}

func (s Service) ManagedHoldings(ctx context.Context, directory, query, format, status string, limit, offset int) (library.ManagedPage, error) {
	return library.FindManaged(ctx, directory, query, format, status, limit, offset)
}

// ManagedHoldingsFiltered lists holdings with a base language filter.
func (s Service) ManagedHoldingsFiltered(ctx context.Context, directory string, filter library.ManagedFilter) (library.ManagedPage, error) {
	return library.FindManagedFiltered(ctx, directory, filter)
}

func (s Service) Audit(ctx context.Context, file, root string, limits inventory.Limits) (library.Audit, error) {
	c, err := library.Load(file)
	if err != nil {
		return library.Audit{}, err
	}
	return library.Verify(ctx, c, root, limits)
}

func (s Service) InitializeLibrary(ctx context.Context, root string) (library.State, error) {
	return library.Initialize(ctx, root)
}

func (s Service) LibraryStatus(ctx context.Context, root string) (library.State, error) {
	return library.LibraryStatus(ctx, root)
}

func (s Service) Import(ctx context.Context, request library.ImportRequest) (library.ImportResult, error) {
	if request.Scan && request.Scanner == nil {
		request.Scanner = s.Scanner
	}
	return library.Import(ctx, request)
}

func (s Service) AuditManaged(ctx context.Context, directory string) (library.ManagedAudit, error) {
	return library.AuditManaged(ctx, directory)
}

func (s Service) Content(ctx context.Context, req content.Request) (content.Result, error) {
	return content.Get(ctx, req)
}

// TextUnits lists an asset's units in the first text representation that
// reads it, epub-text/2 then pdf-text/1, for a reader that knows only the
// asset ID. Audio has no text representation; both failures are returned.
func (s Service) TextUnits(ctx context.Context, catalog, root, asset string) (content.Result, error) {
	var failures []error
	for _, representation := range []string{"epub-text/2", "pdf-text/1"} {
		r, err := s.Content(ctx, content.Request{SchemaVersion: 1, Catalog: catalog, Root: root, AssetID: asset, Representation: representation, UnitsOnly: true, MaxBytes: 4096})
		if err == nil {
			return r, nil
		}
		failures = append(failures, fmt.Errorf("%s: %w", representation, err))
		if ctx.Err() != nil || r.Status == "missing_asset" || r.Status == "invalid_request" {
			break
		}
	}
	return content.Result{}, errors.Join(failures...)
}

func (s Service) ContentCapabilities() content.Capabilities { return content.Available() }

type Capability struct {
	Name        string `json:"name"`
	Available   bool   `json:"available"`
	Recommended bool   `json:"recommended,omitempty"`
	Use         string `json:"use"`
}

type Doctor struct {
	Platform               string              `json:"platform"`
	Go                     string              `json:"go"`
	Paths                  config.Paths        `json:"paths"`
	Config                 config.Config       `json:"configuration"`
	Security               string              `json:"security_status"`
	ScannerRecommendation  string              `json:"scanner_recommendation"`
	ScannerSetup           string              `json:"scanner_setup"`
	Signatures             *scanner.Signatures `json:"signatures,omitempty"`
	ChecksumRecommendation string              `json:"checksum_recommendation"`
	Capabilities           []Capability        `json:"capabilities"`
}

// Doctor checks availability and reads signature database headers or Defender
// registry values. It never executes scanners or claims scan coverage.
func (s Service) Doctor(paths config.Paths, cfg config.Config, lookup func(string) (string, error)) Doctor {
	return s.doctor(paths, cfg, lookup, func() scanner.Signatures { return scanner.HostSignatures(runtime.GOOS, time.Now()) })
}

func (s Service) doctor(paths config.Paths, cfg config.Config, lookup func(string) (string, error), signatures func() scanner.Signatures) Doctor {
	d := Doctor{Platform: runtime.GOOS + "/" + runtime.GOARCH, Go: runtime.Version(), Paths: paths, Config: cfg, Security: "not_scanned", ScannerRecommendation: scannerRecommendation(), ChecksumRecommendation: checksumRecommendation, Capabilities: []Capability{}}
	for _, item := range []struct {
		name, use string
	}{
		{"clamscan", "Recommended on Linux and macOS for a file you did not produce. Windows uses Microsoft Defender instead. A no-detection result is not proof the file is safe."},
		{"MpCmdRun.exe", "Recommended on Windows for a file you did not produce. This is the built-in Microsoft Defender scanner. A no-detection result is not proof the file is safe."},
		{"7z", "reads RAR and 7z archives for library ingest; 7zz and 7za also work"},
		{"epubcheck", "optional EPUB conformance check for check --epubcheck"},
		{"ffmpeg", "optional full audio decode for check --decode, with ffprobe"},
		{"ffprobe", "reads the decoder's output rate for check --decode"},
	} {
		_, err := lookup(item.name)
		d.Capabilities = append(d.Capabilities, Capability{Name: item.name, Available: err == nil, Recommended: recommendedScanner(item.name), Use: item.use})
	}
	d.Capabilities = append(d.Capabilities, Capability{Name: "descriptor_bridge", Available: safeio.DescriptorBridge() == nil, Use: "required on Linux (/proc/self/fd) and macOS (/dev/fd) to reopen a library directory"})
	defaultScanner := "clamscan"
	if runtime.GOOS == "windows" {
		defaultScanner = "MpCmdRun.exe"
	}
	for _, c := range d.Capabilities {
		if c.Name == defaultScanner && !c.Available {
			d.ScannerSetup = scanner.HostSetupHint(runtime.GOOS)
		}
		if c.Name == defaultScanner && c.Available {
			sig := signatures()
			d.Signatures = &sig
		}
	}
	return d
}

const checksumRecommendation = "When a publisher supplies a SHA-256, pass it to check --expected-sha256 or keep the intake receipt. A match shows the bytes are the published bytes. Nemalo also records the SHA-256 it measured. A checksum does not prove those bytes are free of malware."

func recommendedScanner(name string) bool {
	if runtime.GOOS == "windows" {
		return name == "MpCmdRun.exe"
	}
	return name == "clamscan"
}

func scannerRecommendation() string {
	switch runtime.GOOS {
	case "windows":
		return "Microsoft Defender is the default scanner. Run check FILE --scan or library import --scan. ClamAV is a free second engine and is not the default on Windows. Nothing is installed automatically."
	case "darwin":
		return "ClamAV is the default free local scanner Nemalo can run. macOS does not provide an equivalent command-line scanner. Install ClamAV so clamscan is on PATH, update signatures with freshclam, then run check FILE --scan. Nothing is installed automatically."
	default:
		return "ClamAV is the default free local scanner. Install the distributor package that provides clamscan, update signatures with freshclam, then run check FILE --scan. On Arch, including Omarchy, that package is clamav. Nothing is installed automatically."
	}
}

func Lookup(name string) (string, error) {
	if name == "MpCmdRun.exe" {
		kind, path, err := scanner.Resolve()
		if err == nil && kind == "defender" {
			return path, nil
		}
		return "", errors.New("installed Defender unavailable")
	}
	return exec.LookPath(name)
}

// archiveWithLibriVox is the Archive provider that also reads the LibriVox
// project an audiobook item names, for its readers.
func archiveWithLibriVox() *discovery.Archive {
	a := discovery.NewArchive()
	a.LibriVox = discovery.NewLibriVox()
	return a
}
