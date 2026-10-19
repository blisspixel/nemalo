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
)

// IngestRequest imports every supported file, intake packet, and ZIP member
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

// ingestUnit is one import source: a file, an intake packet, or a ZIP archive
// whose members are imported in turn.
type ingestUnit struct {
	item    IngestItem
	file    string // absolute path of the file, packet folder, or archive
	packet  bool
	archive bool
	// volumes are a volume set's folder-relative paths in order; file is
	// the first, which names its members' provenance.
	volumes []string
}

// Ingest inventories the folder once, then imports each candidate through the
// same single-file import, journal, and recovery path as library import. An
// intake packet is imported as a packet. ZIP archives are extracted one at a
// time into a private temporary directory that is removed before the next
// archive; their members are recorded with the archive path as provenance
// ("ARCHIVE!/MEMBER"), which is also how an interrupted member import resumes.
// With Scan, files are scanned in ClamAV batches first, and each import binds
// that result to its own snapshot by SHA-256.
func Ingest(ctx context.Context, request IngestRequest) (result IngestResult, err error) {
	result = IngestResult{SchemaVersion: 1, Applied: request.Apply, Items: []IngestItem{}, Limitations: []string{
		"The folder and its archives are preserved. Nothing is moved or deleted; cleanup is a separate, later step.",
		"Only EPUB, PDF, MP3, M4B, and M4A files, intake packets, and members of ZIP archives, and of RAR and 7z archives when 7-Zip is installed, are imported. Other files and archive formats are listed, not imported.",
		"Each import is assessed and journaled like library import. A no-detection scan is not proof of safety.",
	}}
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
	pendingArchive, pendingMember, pendingLabel := strings.Cut(pending, "!/")
	if pending != "" {
		at := -1
		for i, u := range units {
			if pendingLabel && u.archive && sameImportSource(pendingArchive, u.file) || !pendingLabel && !u.archive && sameImportSource(pending, u.file) {
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
	importOne := func(item IngestItem, file, label string) {
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
			resume = pendingMember
		}
		dir := filepath.Join(tmp, fmt.Sprintf("%05d", n))
		if err := ingestArchive(ctx, src, folder, dir, u, resume, request.Scan, av, cache, &result, importOne); err != nil {
			return result, err
		}
	}
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
	if !result.Complete || result.Failed > 0 {
		return result, errors.New("ingest incomplete; see item outcomes")
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
	// Volume sets: NAME.7z.001, .002, ... and NAME.part1.rar, .part2.rar, ...
	// A complete set, numbered from 1 without gaps, is one archive read
	// through its first volume; the others are its parts. A set with a gap,
	// or a lone split volume, is preserved.
	type volume struct {
		path   string
		number int
	}
	sets := map[string][]volume{}
	for _, e := range entries {
		if e.Finding != "" || e.Kind == "link" || e.Kind == "special" || e.Kind == "unreadable" || e.Kind == "directory" || parts[e.Path] != "" {
			continue
		}
		if set, n, ok := staging.VolumeOf(e.Path); ok {
			sets[set] = append(sets[set], volume{e.Path, n})
		}
	}
	setOf, setFirst := map[string]string{}, map[string]bool{}
	setNames := make([]string, 0, len(sets))
	for set := range sets {
		setNames = append(setNames, set)
	}
	sort.Strings(setNames)
	for _, set := range setNames {
		vs := sets[set]
		sort.Slice(vs, func(i, j int) bool { return vs[i].number < vs[j].number })
		complete := len(vs) >= 2
		for i, v := range vs {
			complete = complete && v.number == i+1
		}
		if !complete {
			// A lone .part1.rar may be a whole archive; it is read as one.
			if len(vs) == 1 && vs[0].number == 1 && strings.HasSuffix(strings.ToLower(vs[0].path), ".rar") {
				continue
			}
			for _, v := range vs {
				setOf[v.path] = "incomplete"
			}
			continue
		}
		paths := make([]string, len(vs))
		for i, v := range vs {
			paths[i] = filepath.FromSlash(v.path)
			setOf[v.path] = vs[0].path
		}
		setFirst[vs[0].path] = true
		first := filepath.Join(folder, paths[0])
		if strings.Contains(first, "!/") {
			continue
		}
		units = append(units, ingestUnit{item: IngestItem{Source: vs[0].path, Kind: "archive"}, file: first, archive: true, volumes: paths})
	}
	for _, e := range entries {
		item := IngestItem{Source: e.Path, Kind: e.Kind}
		switch {
		case setFirst[e.Path]:
			continue
		case setOf[e.Path] == "incomplete":
			item.Outcome, item.Detail = "not_imported", "a volume of a set with a missing volume; preserved"
		case setOf[e.Path] != "":
			item.Outcome, item.Detail = "archive_volume", "a volume of the set read through "+setOf[e.Path]
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
// extracted members with "ARCHIVE!/MEMBER" provenance, and removes the staging
// directory before returning, so staging never holds more than one archive's
// extraction budget. resume names a pending member to import before any other.
func ingestArchive(ctx context.Context, src *os.Root, folder, dir string, u ingestUnit, resume string, scan bool, av assessment.Scanner, cache map[string]scanner.Result, result *IngestResult, importOne func(IngestItem, string, string)) (err error) {
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
	if len(u.volumes) > 0 {
		members, extractErr = staging.ExtractSet(ctx, src, u.volumes, stage, supportedImport, staging.Defaults())
	} else {
		members, extractErr = staging.Extract(ctx, src, rel, stage, supportedImport, staging.Defaults())
	}
	if errors.Is(extractErr, staging.ErrNoArchiveTool) {
		// Without 7-Zip a RAR or 7z is listed and preserved, as before.
		item.Outcome, item.Detail = "not_imported", extractErr.Error()
		result.Items = append(result.Items, item)
		if resume != "" {
			return pendingError(u.file + "!/" + resume)
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
			return pendingError(u.file + "!/" + resume)
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
		memberItem := IngestItem{Source: u.item.Source + "!/" + m.Name, Kind: "archive_member", Outcome: "not_imported", Detail: firstNonEmpty(m.Detail, m.Disposition)}
		if m.Disposition == "failed" {
			memberItem.Outcome = "failed"
		}
		result.Items = append(result.Items, memberItem)
	}
	if resume != "" {
		at := -1
		for i, m := range extracted {
			if m.Name == resume {
				at = i
				break
			}
		}
		if at < 0 {
			return pendingError(u.file + "!/" + resume)
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
		importOne(IngestItem{Source: u.item.Source + "!/" + m.Name, Kind: "archive_member"}, filepath.Join(dir, m.Staged), u.file+"!/"+m.Name)
	}
	return nil
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
