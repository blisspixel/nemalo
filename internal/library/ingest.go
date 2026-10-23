package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/safeio"
	"github.com/blisspixel/nemalo/internal/scanner"
	"github.com/blisspixel/nemalo/internal/staging"
	"github.com/blisspixel/nemalo/internal/textsafe"
)

// IngestRequest imports every supported file, intake packet, and archive member
// under one explicit folder. Without Apply nothing is written to the library.
type IngestRequest struct {
	Library string
	Folder  string
	Apply   bool
	Scan    bool
	Scanner assessment.Scanner
}

// IngestItem is the disposition of one folder entry or archive member.
type IngestItem struct {
	Source  string        `json:"source"`
	Kind    string        `json:"kind"`
	Outcome string        `json:"outcome"`
	Detail  string        `json:"detail,omitempty"`
	Import  *ImportResult `json:"import,omitempty"`
}

// IngestResult accounts for every inventory entry and archive member. The
// folder and its archives are never modified, moved, or deleted.
type IngestResult struct {
	SchemaVersion int          `json:"schema_version"`
	Applied       bool         `json:"applied"`
	Library       string       `json:"library"`
	Folder        string       `json:"folder"`
	Complete      bool         `json:"complete"`
	Items         []IngestItem `json:"items"`
	Imported      int          `json:"imported"`
	Checked       int          `json:"checked"`
	Review        int          `json:"review"`
	NotImported   int          `json:"not_imported"`
	Failed        int          `json:"failed"`
	Limitations   []string     `json:"limitations"`
}

var supportedImport = func(name string) bool { return importFormat(name) != "" }

// ingestUnit is one import source: a file, an intake packet, or an archive tree
// whose members are imported in turn.
type ingestUnit struct {
	item    IngestItem
	file    string // absolute path of the file, packet folder, or archive
	packet  bool
	archive bool
	// volumes are a volume set's folder-relative paths in order; file is
	// the first, which names its members' provenance.
	volumes []string
	// resumeSource is the current folder-relative part matching an older
	// pending label when that part was previously read as a standalone ZIP.
	resumeSource string
}

// Ingest inventories the folder once, then imports each candidate through the
// same single-file import, journal, and recovery path as library import. An
// intake packet is imported as a packet. Archive trees are extracted one at a
// time into a private temporary directory that is removed before the next
// archive; their members are recorded with the archive path as provenance
// ("ARCHIVE!/MEMBER"), which is also how an interrupted member import resumes.
// With Scan, files are scanned in ClamAV batches first, and each import binds
// that result to its own snapshot by SHA-256.
func Ingest(ctx context.Context, request IngestRequest) (result IngestResult, err error) {
	result = IngestResult{SchemaVersion: 1, Applied: request.Apply, Items: []IngestItem{}, Limitations: []string{
		"The folder and its archives are preserved. Nothing is moved or deleted; cleanup is a separate, later step.",
		"Only EPUB, PDF, MP3, M4B, and M4A files, completed intake packets, and supported archive members are imported. RAR, 7z, split ZIPs, and nested ZIPs need installed 7-Zip. Unsupported variants stay listed.",
		"Each outer archive tree has four archive levels, 10,000 members, and a shared 1 GiB copy allowance including failed attempts. Complete supported nested sets use the same checked adapters and shared bounds; uncertain sets remain preserved.",
		"Each import is assessed and journaled like library import. A no-detection scan is not proof of safety.",
	}}
	defer func() {
		for _, item := range result.Items {
			switch item.Outcome {
			case "checked":
				result.Checked++
				result.Imported++
			case "review":
				result.Review++
				result.Imported++
			case "failed":
				result.Failed++
			case "packet_part", "archive_volume":
			default:
				result.NotImported++
			}
		}
		if err != nil || result.Failed > 0 {
			result.Complete = false
		}
		if err == nil && !result.Complete {
			err = errors.New("ingest incomplete; see item outcomes")
		}
	}()
	libraryRoot, err := filepath.Abs(request.Library)
	if err != nil {
		return result, err
	}
	folder, err := filepath.Abs(request.Folder)
	if err != nil {
		return result, err
	}
	result.Library, result.Folder = libraryRoot, folder
	if overlapping(libraryRoot, folder) {
		return result, errors.New("the folder and the library must not contain each other")
	}
	state, err := LibraryStatus(ctx, libraryRoot)
	if err != nil {
		return result, err
	}
	if state.Status != "initialized" {
		return result, errors.New("ingest requires an initialized library; run library init on this explicit directory")
	}
	src, err := safeio.OpenRoot(folder)
	if err != nil {
		return result, err
	}
	defer src.Close()
	inv, invErr := inventory.InspectRoot(ctx, src, folder, inventory.Defaults())
	result.Complete = invErr == nil
	if invErr != nil && ctx.Err() != nil {
		return result, invErr
	}
	tmp, err := os.MkdirTemp("", "nemalo-ingest-")
	if err != nil {
		return result, err
	}
	defer func() {
		if cleanupErr := os.RemoveAll(tmp); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("remove private staging: %w", cleanupErr))
		}
	}()

	units := planIngest(folder, inv.Entries, &result)

	// An interrupted import must finish before any other: resume it first when
	// it belongs to this folder, otherwise stop before importing anything.
	pending, err := pendingSource(libraryRoot)
	if err != nil {
		return result, err
	}
	pendingArchive, _, pendingLabel := strings.Cut(pending, "!/")
	if pending != "" {
		at := -1
		for i, u := range units {
			archiveMatches := pendingLabel && u.archive && sameImportSource(pendingArchive, u.file)
			if pendingLabel && u.archive && !archiveMatches {
				for _, v := range u.volumes {
					if sameImportSource(pendingArchive, filepath.Join(folder, v)) {
						archiveMatches = true
						units[i].resumeSource = filepath.ToSlash(v)
						break
					}
				}
			}
			if archiveMatches || !pendingLabel && !u.archive && sameImportSource(pending, u.file) {
				at = i
				break
			}
		}
		if at < 0 {
			return result, pendingError(pending)
		}
		units = append([]ingestUnit{units[at]}, append(units[:at:at], units[at+1:]...)...)
	}
	av := request.Scanner
	if av == nil {
		av = scanner.New()
	}
	cache := map[string]scanner.Result{}
	if request.Scan {
		files := []string{}
		for _, u := range units {
			if !u.packet && !u.archive {
				files = append(files, u.file)
			}
		}
		prescan(ctx, files, av, cache)
		av = cachedScanner{cache: cache, fallback: av}
	}
	importOne := func(item IngestItem, file, label string) error {
		r, importErr := Import(ctx, ImportRequest{Library: libraryRoot, Source: file, Apply: request.Apply, Scan: request.Scan, Scanner: av, Label: label})
		item.Import = &r
		switch {
		case importErr != nil:
			item.Outcome, item.Detail = "failed", importErr.Error()
		case r.Status == "checked":
			item.Outcome = "checked"
		default:
			item.Outcome = "review"
		}
		result.Items = append(result.Items, item)
		return importErr
	}
	for n, u := range units {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if !u.archive {
			importOne(u.item, u.file, "")
			continue
		}
		resume := ""
		if n == 0 && pendingLabel {
			resume = pending
		}
		dir := filepath.Join(tmp, fmt.Sprintf("%05d", n))
		if err := ingestArchive(ctx, src, folder, dir, u, resume, request.Scan, av, cache, &result, importOne); err != nil {
			return result, err
		}
	}
	return result, nil
}

// planIngest turns inventory entries into import units and records every
// entry that is not imported. A packet's parts are its receipt.json, intent,
// and content file; every other entry, including other files inside a packet
// folder or beside a packet at the top level, is handled on its own.
func planIngest(folder string, entries []inventory.Entry, result *IngestResult) []ingestUnit {
	present := make(map[string]bool, len(entries))
	for _, e := range entries {
		present[e.Path] = true
	}
	has := func(name string) bool { return present[name] }
	packets, parts := map[string]bool{}, map[string]string{}
	for _, e := range entries {
		if path.Base(e.Path) != "receipt.json" {
			continue
		}
		dir := path.Dir(e.Path)
		for _, f := range packetContents {
			if has(path.Join(dir, f)) {
				packets[dir] = true
				parts[path.Join(dir, f)] = dir
			}
		}
		if packets[dir] {
			parts[e.Path] = dir
			if has(path.Join(dir, "intent.json")) {
				parts[path.Join(dir, "intent.json")] = dir
			}
		}
	}
	// A packet with an intent and content but no receipt is an interrupted or
	// failed acquisition. Its partial content is never imported as a file.
	incomplete := map[string]bool{}
	for _, e := range entries {
		dir := path.Dir(e.Path)
		if path.Base(e.Path) != "intent.json" || packets[dir] {
			continue
		}
		for _, f := range packetContents {
			if has(path.Join(dir, f)) {
				incomplete[path.Join(dir, f)] = true
				incomplete[e.Path] = true
			}
		}
	}
	dirs := make([]string, 0, len(packets))
	for dir := range packets {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	units := []ingestUnit{}
	for _, dir := range dirs {
		units = append(units, ingestUnit{item: IngestItem{Source: dir, Kind: "packet"}, file: filepath.Join(folder, filepath.FromSlash(dir)), packet: true})
	}
	// Shared grouping accounts for every supported volume family. A complete
	// set, numbered from 1 without gaps, is one archive read through its first
	// volume; the others are its parts. Gaps and ambiguity preserve the set.
	var candidates []string
	for _, e := range entries {
		if e.Finding != "" || e.Kind == "link" || e.Kind == "special" || e.Kind == "unreadable" || e.Kind == "directory" || parts[e.Path] != "" {
			continue
		}
		candidates = append(candidates, e.Path)
	}
	setOf, setFirst := map[string]string{}, map[string]bool{}
	brokenSet := map[string]string{}
	for _, set := range volumeSets(candidates) {
		if !set.complete {
			for _, v := range set.volumes {
				brokenSet[v] = set.reason
			}
			continue
		}
		paths := make([]string, len(set.volumes))
		for i, v := range set.volumes {
			paths[i] = filepath.FromSlash(v)
		}
		first := filepath.Join(folder, paths[0])
		if strings.Contains(first, "!/") {
			// "!/" separates an archive from its member in provenance labels.
			for _, v := range set.volumes {
				setOf[v] = "unlabelable"
			}
			continue
		}
		for _, v := range set.volumes {
			setOf[v] = set.volumes[0]
		}
		setFirst[set.volumes[0]] = true
		units = append(units, ingestUnit{item: IngestItem{Source: set.volumes[0], Kind: "archive"}, file: first, archive: true, volumes: paths})
	}
	for _, e := range entries {
		item := IngestItem{Source: e.Path, Kind: e.Kind}
		switch {
		case setFirst[e.Path]:
			continue
		case brokenSet[e.Path] != "":
			item.Outcome, item.Detail = "not_imported", brokenSet[e.Path]
		case setOf[e.Path] == "unlabelable":
			item.Kind, item.Outcome, item.Detail = "archive", "failed", "a volume of a set whose path contains \"!/\", which member provenance cannot represent; preserved"
		case setOf[e.Path] != "":
			item.Outcome, item.Detail = "archive_volume", "a volume of the set read through "+setOf[e.Path]+"; that archive's outcome is the set's"
		case e.Finding != "" || e.Kind == "link" || e.Kind == "special" || e.Kind == "unreadable":
			item.Outcome, item.Detail = "not_imported", firstNonEmpty(e.Finding, e.Kind)
		case e.Kind == "directory":
			continue
		case parts[e.Path] != "":
			item.Outcome, item.Detail = "packet_part", "imported with packet "+parts[e.Path]
		case incomplete[e.Path]:
			item.Outcome, item.Detail = "not_imported", "intake packet without a receipt; retained for review"
		case (path.Base(e.Path) == "recording.json" || path.Base(e.Path) == "recording-intent.json") && has(path.Join(path.Dir(e.Path), "recording-intent.json")):
			item.Outcome, item.Detail = "packet_part", "recording record; its track packets are imported on their own"
		case staging.Supported(e.Path):
			archive := filepath.Join(folder, filepath.FromSlash(e.Path))
			if strings.Contains(archive, "!/") {
				// "!/" separates an archive from its member in provenance labels.
				item.Kind, item.Outcome, item.Detail = "archive", "failed", "the archive path contains \"!/\", which member provenance cannot represent; preserved"
				break
			}
			units = append(units, ingestUnit{item: IngestItem{Source: e.Path, Kind: "archive"}, file: archive, archive: true})
			continue
		case supportedImport(e.Path):
			units = append(units, ingestUnit{item: item, file: filepath.Join(folder, filepath.FromSlash(e.Path))})
			continue
		default:
			item.Outcome, item.Detail = "not_imported", "not a supported format; preserved"
		}
		result.Items = append(result.Items, item)
	}
	return units
}

// ingestArchive extracts one ZIP, RAR, or 7z archive into its own staging
// directory, imports the
// extracted members with direct or nested selection provenance, and removes the staging
// directory before returning, so staging never holds more than one archive's
// extraction budget. resumeLabel binds the original pending member provenance.
func ingestArchive(ctx context.Context, src *os.Root, folder, dir string, u ingestUnit, resumeLabel string, scan bool, av assessment.Scanner, cache map[string]scanner.Result, result *IngestResult, importOne func(IngestItem, string, string) error) (err error) {
	_, resume, _ := strings.Cut(resumeLabel, "!/")
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	defer func() {
		if cleanupErr := os.RemoveAll(dir); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("remove private staging: %w", cleanupErr))
		}
	}()
	stage, err := safeio.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer stage.Close()
	rel, err := filepath.Rel(folder, u.file)
	if err != nil {
		return err
	}
	item := u.item
	var members staging.Extraction
	var extractErr error
	names := u.volumes
	if len(names) == 0 {
		names = []string{rel}
	}
	limits := staging.Defaults()
	limits.NestedVolumeSets = true
	members, extractErr = staging.ExtractTree(ctx, src, names, stage, supportedImport, limits)
	if errors.Is(extractErr, staging.ErrNoArchiveTool) {
		// Without 7-Zip a RAR, 7z, or split ZIP is listed and preserved.
		item.Outcome, item.Detail = "not_imported", extractErr.Error()
		result.Items = append(result.Items, item)
		if resume != "" {
			return pendingError(resumeLabel)
		}
		return nil
	}
	if extractErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		item.Outcome, item.Detail = "failed", extractErr.Error()
		result.Complete = false
		result.Items = append(result.Items, item)
		if resume != "" {
			return pendingError(resumeLabel)
		}
		return nil
	}
	if !members.Complete {
		result.Complete = false
	}
	item.Outcome, item.Detail = "preserved", fmt.Sprintf("%d member(s)", len(members.Members))
	result.Items = append(result.Items, item)
	extracted := []staging.Member{}
	for _, m := range members.Members {
		if m.Disposition == "extracted" {
			extracted = append(extracted, m)
			continue
		}
		memberItem := IngestItem{Source: u.item.Source + "!/" + archiveMemberSelection(m), Kind: "archive_member", Outcome: "not_imported", Detail: firstNonEmpty(m.Detail, m.Disposition)}
		if m.Disposition == "failed" {
			memberItem.Outcome = "failed"
		}
		result.Items = append(result.Items, memberItem)
	}
	if resume != "" {
		at := -1
		for i, m := range extracted {
			if archiveMemberSelection(m) == resume {
				at = i
				break
			}
		}
		if at < 0 {
			return pendingError(resumeLabel)
		}
		extracted = append([]staging.Member{extracted[at]}, append(extracted[:at:at], extracted[at+1:]...)...)
	}
	if scan {
		files := make([]string, len(extracted))
		for i, m := range extracted {
			files[i] = filepath.Join(dir, m.Staged)
		}
		prescan(ctx, files, av, cache)
	}
	for _, m := range extracted {
		if err := ctx.Err(); err != nil {
			return err
		}
		selection := archiveMemberSelection(m)
		source, label := u.item.Source+"!/"+selection, u.file+"!/"+selection
		if resume != "" && selection == resume {
			label = resumeLabel
			if u.resumeSource != "" {
				source = u.resumeSource + "!/" + selection
			}
		}
		importErr := importOne(IngestItem{Source: source, Kind: "archive_member"}, filepath.Join(dir, m.Staged), label)
		if resume != "" && selection == resume && importErr != nil {
			return importErr
		}
	}
	return nil
}

// A leading slash cannot name an eligible historical direct member. Reserve
// that suffix for a canonical JSON chain; keep every direct label unchanged.
func archiveMemberSelection(m staging.Member) string {
	if len(m.Chain) <= 1 {
		return m.Name
	}
	data, _ := textsafe.MarshalJSON(m.Chain, false) // A string slice always marshals.
	return "/" + string(data)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// prescan scans files in batched passes and records results in cache by
// SHA-256. Import scans its own private snapshot; cachedScanner hashes that
// snapshot and answers only for identical bytes, so a result stays bound to
// exactly the bytes imported. Only definitive results are cached: anything
// else is scanned again individually.
func prescan(ctx context.Context, paths []string, av assessment.Scanner, cache map[string]scanner.Result) {
	// Group by directory so each batch uses one capability-bound root.
	byDir := map[string][]string{}
	for _, p := range paths {
		byDir[filepath.Dir(p)] = append(byDir[filepath.Dir(p)], filepath.Base(p))
	}
	for dir, names := range byDir {
		root, err := safeio.OpenResolvedRoot(dir)
		if err != nil {
			continue
		}
		options := make([]assessment.Options, len(names))
		for i := range options {
			options[i].Scan = true
		}
		reports, _ := assessment.CheckManyInRoot(ctx, root, names, options, av)
		_ = root.Close()
		for _, r := range reports {
			if validHash(r.SHA256) && (r.Antivirus.Status == "no_detections_reported" || r.Antivirus.Status == "findings") {
				cache[r.SHA256] = r.Antivirus
			}
		}
	}
}

type cachedScanner struct {
	cache    map[string]scanner.Result
	fallback assessment.Scanner
}

func (c cachedScanner) Scan(ctx context.Context, snapshot string) scanner.Result {
	if sum, err := fileSHA256(snapshot); err == nil {
		if r, ok := c.cache[sum]; ok {
			return r
		}
	}
	return c.fallback.Scan(ctx, snapshot)
}

func fileSHA256(name string) (string, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, assessment.MaxMP4Bytes+1)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
