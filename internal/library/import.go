package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/safeio"
)

// ImportRequest copies one explicit file or completed intake packet into an
// initialized library. Without Apply it only assesses and reports the plan.
type ImportRequest struct {
	Library string
	Source  string
	Scan    bool
	Apply   bool
	Scanner assessment.Scanner
}

// ImportResult is the assessed disposition. Applied false means no library write.
type ImportResult struct {
	SchemaVersion int      `json:"schema_version"`
	Applied       bool     `json:"applied"`
	Resumed       bool     `json:"resumed"`
	AlreadyHeld   bool     `json:"already_held"`
	LibraryID     string   `json:"library_id,omitempty"`
	Library       string   `json:"library"`
	Source        string   `json:"source"`
	SourceKind    string   `json:"source_kind,omitempty"`
	AssetID       string   `json:"asset_id,omitempty"`
	Format        string   `json:"format,omitempty"`
	Bytes         int64    `json:"bytes,omitempty"`
	SHA256        string   `json:"sha256,omitempty"`
	Status        string   `json:"status,omitempty"`
	Reason        string   `json:"reason,omitempty"`
	Path          string   `json:"path,omitempty"`
	Titles        []string `json:"titles"`
	Languages     []string `json:"languages"`
	Antivirus     string   `json:"antivirus,omitempty"`
	Findings      []string `json:"findings"`
	Limitations   []string `json:"limitations"`
	Durability    string   `json:"durability"`
}

type selected struct {
	kind    string
	file    string
	receipt *packetReceipt
}

type packetReceipt struct {
	Complete bool   `json:"complete"`
	Status   string `json:"status"`
	FinalURL string `json:"final_url"`
	Transfer struct {
		SHA256 string `json:"sha256"`
		Bytes  int64  `json:"bytes"`
	} `json:"transfer"`
	Intent *struct {
		Request struct {
			SourceID string `json:"source_id"`
			File     string `json:"source_file"`
		} `json:"request"`
		Selection struct {
			Rights   []string `json:"rights_statements"`
			Licenses []string `json:"declared_license_urls"`
		} `json:"selection"`
	} `json:"intent"`
}

type importStage func(string) error

// errStoredObject means the managed file or its parent is not a single regular
// object with the expected bytes. Journals are left unchanged.
var errStoredObject = errors.New("managed asset needs review; stored files were left unchanged")

type storedObject struct {
	present   bool
	exclusive bool
}

// Import preserves the source. Checked status is only an EPUB with passed
// limited checks and a completed no-detection scan. The initialization journal
// is not extended.
func Import(ctx context.Context, request ImportRequest) (ImportResult, error) {
	return importLibrary(ctx, request, nil)
}

func importLibrary(ctx context.Context, request ImportRequest, stage importStage) (result ImportResult, err error) {
	result = ImportResult{SchemaVersion: 1, Library: request.Library, Source: request.Source, Titles: []string{}, Languages: []string{}, Findings: []string{}, Limitations: importLimitations(), Durability: "file_synced; directory entry power-loss durability depends on filesystem"}
	if request.Library == "" || request.Source == "" {
		return result, errors.New("library import requires an initialized library directory and a source file or intake packet")
	}
	libraryRoot, err := filepath.Abs(request.Library)
	if err != nil {
		return result, err
	}
	source, err := filepath.Abs(request.Source)
	if err != nil {
		return result, err
	}
	result.Library, result.Source = libraryRoot, source
	state, err := LibraryStatus(ctx, libraryRoot)
	if err != nil {
		return result, err
	}
	if state.Status != "initialized" {
		return result, errors.New("library import requires an initialized library; run library init on this explicit directory")
	}
	result.LibraryID = state.LibraryID
	selected, err := selectSource(source)
	if err != nil {
		return result, err
	}
	result.SourceKind = selected.kind
	if sourceTooLong(source) || sourceTooLong(selected.file) {
		return result, errors.New("import source path exceeds the stored record limit")
	}
	if within(libraryRoot, selected.file) || (selected.kind == "packet" && within(libraryRoot, source)) {
		return result, errors.New("import source must be outside the library")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if !request.Apply {
		pending, pendingErr := pendingSource(libraryRoot)
		if pendingErr != nil {
			return result, pendingErr
		}
		if pending != "" {
			return result, fmt.Errorf("import is pending for %s; repeat library import --apply with that source", pending)
		}
		return assessPlan(ctx, result, selected, request)
	}
	store, err := safeio.OpenRoot(libraryRoot)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, store.Close()) }()
	control, _, err := openControl(libraryRoot, true)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, control.close()) }()
	events, lines, err := lockedManaged(control, state.LibraryID)
	if err != nil {
		return result, err
	}
	if len(events) > 0 && events[len(events)-1].Event == "import_intent" {
		pending := events[len(events)-1].Source
		if !sameImportSource(pending, source) {
			return result, fmt.Errorf("import is pending for %s; repeat library import --apply with that source", pending)
		}
		return finish(ctx, control, store, state.LibraryID, events, lines, selected, request, stage, true)
	}
	return finish(ctx, control, store, state.LibraryID, events, lines, selected, request, stage, false)
}

func assessPlan(ctx context.Context, result ImportResult, source selected, request ImportRequest) (ImportResult, error) {
	report, err := assessment.Check(ctx, source.file, assessment.Options{Scan: request.Scan}, request.Scanner)
	report = normalizeFormat(report, source.file)
	fillReport(&result, report)
	if result.SHA256 == "" {
		if err == nil {
			err = errors.New("assessment incomplete; source unchanged and nothing imported")
		}
		return result, err
	}
	status, reason, classErr := classify(report, request.Scan)
	if classErr != nil {
		return result, classErr
	}
	if err = receiptAgrees(source.receipt, result.SHA256, result.Bytes); err != nil {
		return result, err
	}
	result.Status, result.Reason = status, reason
	result.Path = assetRel(result.SHA256, result.Format)
	result.Titles = metaList(report)
	result.Languages = languageList(report)
	if note := omittedDeclared(source.receipt); note != "" {
		result.Limitations = append(result.Limitations, note)
	}
	return result, nil
}

func finish(ctx context.Context, control *control, store *os.Root, libraryID string, events []operation, lines []Holding, source selected, request ImportRequest, stage importStage, resume bool) (ImportResult, error) {
	result := ImportResult{SchemaVersion: 1, Library: request.Library, Source: request.Source, SourceKind: source.kind, LibraryID: libraryID, Titles: []string{}, Languages: []string{}, Findings: []string{}, Limitations: importLimitations(), Durability: "file_synced; directory entry power-loss durability depends on filesystem", Resumed: resume}
	libraryRoot, err := filepath.Abs(request.Library)
	if err != nil {
		return result, err
	}
	userSource, err := filepath.Abs(request.Source)
	if err != nil {
		return result, err
	}
	result.Library, result.Source = libraryRoot, userSource
	var intent operation
	if resume {
		intent = events[len(events)-1]
	}
	report, err := assessment.Check(ctx, source.file, assessment.Options{Scan: request.Scan && !resume}, request.Scanner)
	report = normalizeFormat(report, source.file)
	fillReport(&result, report)
	if result.SHA256 == "" {
		if err == nil {
			err = errors.New("assessment incomplete; source unchanged and nothing imported")
		}
		return result, err
	}
	if err = receiptAgrees(source.receipt, result.SHA256, result.Bytes); err != nil {
		return result, err
	}
	status, reason, err := classify(report, request.Scan && !resume)
	if err != nil {
		return result, err
	}
	if resume {
		if result.SHA256 != intent.AssetID[7:] || result.Bytes != intent.Bytes || result.Format != intent.Format {
			return result, errors.New("pending import source changed; the journal was preserved")
		}
		status, reason = intent.Status, intent.Reason
	}
	result.Titles, result.Languages = metaList(report), languageList(report)
	current := latestHoldings(lines)[result.AssetID]
	if !resume && current.ID == result.AssetID && !request.Scan {
		status, reason = current.Status, current.Reason
	}
	result.Status, result.Reason = status, reason
	result.Path = assetRel(result.SHA256, result.Format)
	add := sourceRecord(source)
	holding := Holding{SchemaVersion: 1, LibraryID: libraryID, ID: result.AssetID, SHA256: result.SHA256, Bytes: result.Bytes, Format: result.Format, Status: status, Reason: reason, Path: result.Path, Titles: result.Titles, Languages: result.Languages, Sources: mergeSources(current, add)}
	if note := omittedDeclared(source.receipt); note != "" {
		result.Limitations = append(result.Limitations, note)
	}
	if !validHolding(holding, libraryID) {
		return result, errors.New("import record is outside the managed journal limits")
	}
	fact := operation{AssetID: holding.ID, Format: holding.Format, Bytes: holding.Bytes, Status: holding.Status, Reason: holding.Reason}
	if err = prepareStore(ctx, store, holding.Path); err != nil {
		return result, err
	}
	obj, err := inspectStored(ctx, store, holding.Path, holding.Bytes, holding.SHA256)
	if err != nil {
		return result, err
	}
	recorded := !resume && sameFact(current, fact) && sourceKnown(current, add)
	if obj.present && obj.exclusive && recorded {
		result.AlreadyHeld, result.Applied = true, true
		return result, nil
	}
	if obj.present && !obj.exclusive && recorded {
		if err = materialize(ctx, store, source.file, holding.Path, holding.Bytes, holding.SHA256, stage); err != nil {
			return result, err
		}
		result.AlreadyHeld, result.Applied = true, true
		return result, nil
	}
	writeHolding := !sameFact(current, fact) || !sourceKnown(current, add)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if !resume {
		intent = operation{SchemaVersion: 1, Sequence: len(events) + 1, LibraryID: libraryID, OperationID: newStateID("operation"), Event: "import_intent", Source: userSource, AssetID: holding.ID, Format: holding.Format, Bytes: holding.Bytes, Status: holding.Status, Reason: holding.Reason, At: now}
		if err = reserveNew(control, intent, holding, writeHolding); err != nil {
			return result, err
		}
		if err = appendCanonical(ctx, control, operationsName, maxOperations, intent); err != nil {
			return result, err
		}
		events = append(events, intent)
		if err = runStage(stage, "intent"); err != nil {
			return result, err
		}
	} else if err = reserveResume(control, intent, holding, writeHolding); err != nil {
		return result, err
	}
	if err = materialize(ctx, store, source.file, holding.Path, holding.Bytes, holding.SHA256, stage); err != nil {
		return result, err
	}
	if writeHolding {
		if err = appendCanonical(ctx, control, holdingsName, maxHoldings, holding); err != nil {
			return result, err
		}
		if err = runStage(stage, "recorded"); err != nil {
			return result, err
		}
	}
	done := intent
	done.Sequence, done.Event, done.At = len(events)+1, "import_completed", time.Now().UTC().Format(time.RFC3339Nano)
	if err = appendCanonical(ctx, control, operationsName, maxOperations, done); err != nil {
		return result, err
	}
	result.Applied = true
	return result, nil
}

func pendingSource(root string) (string, error) {
	control, _, err := openControl(root, false)
	if err != nil {
		return "", err
	}
	defer control.close()
	stateEvents, err := readInit(control)
	if err != nil {
		return "", err
	}
	events, _, err := lockedManaged(control, stateEvents[0].LibraryID)
	if err != nil {
		return "", err
	}
	if len(events) > 0 && events[len(events)-1].Event == "import_intent" {
		return events[len(events)-1].Source, nil
	}
	return "", nil
}

func lockedManaged(control *control, libraryID string) ([]operation, []Holding, error) {
	initEvents, err := readInit(control)
	if err != nil {
		return nil, nil, err
	}
	if len(initEvents) != 2 || initEvents[0].LibraryID != libraryID {
		return nil, nil, errors.New("library import requires an initialized library; run library init on this explicit directory")
	}
	if err = validateManaged(control.root, libraryID); err != nil {
		return nil, nil, err
	}
	ops, err := readOptional(control.root, operationsName, maxOperations)
	if err != nil {
		return nil, nil, err
	}
	holds, err := readOptional(control.root, holdingsName, maxHoldings)
	if err != nil {
		return nil, nil, err
	}
	events, err := parseOperations(ops, libraryID)
	if err != nil {
		return nil, nil, err
	}
	lines, err := parseHoldings(holds, libraryID)
	if err != nil {
		return nil, nil, err
	}
	return events, lines, nil
}

func readInit(control *control) ([]journalEvent, error) {
	f, err := openRegular(control.root, journalName, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readJournal(f)
}

func selectSource(source string) (selected, error) {
	info, err := os.Lstat(source)
	if err != nil {
		return selected{}, err
	}
	if info.Mode().IsRegular() {
		if importFormat(source) == "" || info.Size() < 1 {
			return selected{}, errors.New("import a regular EPUB, PDF, or MP3 file, or a completed intake packet")
		}
		return selected{kind: "file", file: source}, nil
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return selected{}, errors.New("import a regular EPUB, PDF, or MP3 file, or a completed intake packet")
	}
	root, err := safeio.OpenRoot(source)
	if err != nil {
		return selected{}, err
	}
	defer root.Close()
	found := []string{}
	for _, name := range []string{"content.epub", "content.pdf", "content.mp3"} {
		entry, err := root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !entry.Mode().IsRegular() || entry.Size() < 1 {
			return selected{}, errors.New("intake packet content must be one regular EPUB, PDF, or MP3 file")
		}
		found = append(found, name)
	}
	if len(found) != 1 {
		return selected{}, errors.New("intake packet requires exactly one of content.epub, content.pdf, or content.mp3")
	}
	receipt, err := readReceipt(root)
	if err != nil {
		return selected{}, err
	}
	return selected{kind: "packet", file: filepath.Join(source, found[0]), receipt: receipt}, nil
}

func readReceipt(root *os.Root) (*packetReceipt, error) {
	info, err := root.Lstat("receipt.json")
	if err != nil || !info.Mode().IsRegular() || info.Size() < 2 || info.Size() > 4<<20 {
		return nil, errors.New("intake packet requires a completed receipt.json")
	}
	f, err := safeio.OpenRegular(root, "receipt.json", info, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, info.Size()+1))
	if err != nil || int64(len(b)) != info.Size() {
		return nil, err
	}
	var receipt packetReceipt
	if err := json.Unmarshal(b, &receipt); err != nil || !receipt.Complete || receipt.Status != "untrusted_intake" || receipt.Transfer.Bytes < 1 || !validHash(receipt.Transfer.SHA256) {
		return nil, errors.New("intake packet receipt is incomplete or not untrusted intake")
	}
	return &receipt, nil
}

func receiptAgrees(receipt *packetReceipt, hash string, bytes int64) error {
	if receipt == nil {
		return nil
	}
	if !strings.EqualFold(receipt.Transfer.SHA256, hash) || receipt.Transfer.Bytes != bytes {
		return errors.New("intake receipt does not match the content bytes; the packet was preserved")
	}
	return nil
}

func sourceRecord(source selected) Source {
	record := Source{Kind: source.kind, Path: source.file}
	if source.receipt == nil || source.receipt.Intent == nil {
		return record
	}
	record.SourceID = boundToken(source.receipt.Intent.Request.SourceID)
	record.SourceFile = boundToken(source.receipt.Intent.Request.File)
	if url := source.receipt.FinalURL; strings.HasPrefix(url, "https://") && len(url) <= 2000 && !strings.ContainsAny(url, " \r\n\x00") {
		record.DeclaredURL = url
	}
	record.DeclaredRights, _ = cleanDeclared(source.receipt.Intent.Selection.Rights)
	record.DeclaredLicenses, _ = cleanDeclared(source.receipt.Intent.Selection.Licenses)
	return record
}

func omittedDeclared(receipt *packetReceipt) string {
	if receipt == nil || receipt.Intent == nil {
		return ""
	}
	_, rightsOmit := cleanDeclared(receipt.Intent.Selection.Rights)
	_, licenseOmit := cleanDeclared(receipt.Intent.Selection.Licenses)
	url := receipt.FinalURL
	urlOmit := url != "" && !(strings.HasPrefix(url, "https://") && len(url) <= 2000 && !strings.ContainsAny(url, " \r\n\x00"))
	if rightsOmit || licenseOmit || urlOmit {
		return "A declared rights, license, or URL field was omitted because it exceeded the stored record limit or was not an https URL."
	}
	return ""
}

func cleanDeclared(values []string) ([]string, bool) {
	if len(values) > 32 {
		return nil, true
	}
	out := []string{}
	for _, value := range values {
		if value == "" || len(value) > 2000 || strings.ContainsAny(value, "\x00\r\n") {
			return nil, true
		}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, false
}

func boundToken(value string) string {
	if value == "" || len(value) > 1000 || strings.ContainsAny(value, "\x00\r\n") {
		return ""
	}
	return value
}

func mergeSources(current Holding, add Source) []Source {
	out := append([]Source{}, current.Sources...)
	if !sourceKnown(current, add) {
		out = append(out, add)
	}
	if len(out) == 0 {
		out = []Source{add}
	}
	return out
}

func sourceKnown(current Holding, add Source) bool {
	for _, source := range current.Sources {
		if source.Kind == add.Kind && sameImportSource(source.Path, add.Path) {
			return true
		}
	}
	return false
}

func sameImportSource(recorded, requested string) bool {
	if recorded == requested {
		return true
	}
	if !strings.EqualFold(recorded, requested) {
		return false
	}
	a, errA := os.Lstat(recorded)
	b, errB := os.Lstat(requested)
	return errA == nil && errB == nil && os.SameFile(a, b)
}

func sourceTooLong(path string) bool {
	return len(path) > 4096 || strings.ContainsAny(path, "\x00\r\n")
}

func normalizeFormat(report assessment.Report, file string) assessment.Report {
	if report.DetectedFormat == "zip" && importFormat(file) == "epub" {
		report.DetectedFormat = "epub"
	}
	return report
}

func fillReport(result *ImportResult, report assessment.Report) {
	result.Bytes, result.SHA256, result.Format = report.Bytes, report.SHA256, report.DetectedFormat
	if validHash(report.SHA256) {
		result.AssetID = "sha256:" + report.SHA256
	}
	result.Antivirus = report.Antivirus.Status
	if report.Findings != nil {
		result.Findings = append([]string{}, report.Findings...)
	}
}

func metaList(report assessment.Report) []string {
	if report.Checks.EPUB == nil {
		return []string{}
	}
	return boundMeta(report.Checks.EPUB.Titles)
}

func languageList(report assessment.Report) []string {
	if report.Checks.EPUB == nil {
		return []string{}
	}
	return boundMeta(report.Checks.EPUB.Languages)
}

func boundMeta(values []string) []string {
	out := []string{}
	for _, value := range values {
		if value == "" || len(value) > 2000 || strings.ContainsAny(value, "\x00\r\n") || len(out) == 32 {
			continue
		}
		out = append(out, value)
	}
	return out
}

func materialize(ctx context.Context, root *os.Root, source, rel string, size int64, hash string, stage importStage) error {
	final, err := assetOSPath(rel)
	if err != nil {
		return err
	}
	partialName := rel + ".partial"
	partial, err := assetOSPath(partialName)
	if err != nil {
		return err
	}
	if err := mkdirSlash(root, path.Dir(rel)); err != nil {
		return err
	}
	switch err := hashAgrees(ctx, root, rel, size, hash); {
	case err == nil:
		if info, statErr := root.Lstat(partial); statErr == nil {
			if !info.Mode().IsRegular() {
				return errStoredObject
			}
			if rmErr := root.Remove(partial); rmErr != nil {
				return rmErr
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		if err = requireExclusive(root, rel); err != nil {
			return err
		}
		if err = runStage(stage, "copied"); err != nil {
			return err
		}
		return runStage(stage, "linked")
	case !errors.Is(err, os.ErrNotExist):
		return err
	}
	if info, statErr := root.Lstat(partial); statErr == nil {
		if !info.Mode().IsRegular() {
			return errStoredObject
		}
		n, nerr := regularLinkCount(root, partialName)
		if nerr != nil {
			return nerr
		}
		if n != 1 {
			return errStoredObject
		}
		if hashErr := hashAgrees(ctx, root, partialName, size, hash); hashErr == nil {
			if err := root.Link(partial, final); err != nil {
				return err
			}
			if err := root.Remove(partial); err != nil {
				return err
			}
			if err = requireExclusive(root, rel); err != nil {
				return err
			}
			if err := runStage(stage, "copied"); err != nil {
				return err
			}
			return runStage(stage, "linked")
		} else if err := root.Remove(partial); err != nil {
			return err
		}
	}
	if err := copyPartial(ctx, root, source, partial, size, hash); err != nil {
		return err
	}
	n, nerr := regularLinkCount(root, partialName)
	if nerr != nil {
		return nerr
	}
	if n != 1 {
		_ = root.Remove(partial)
		return errStoredObject
	}
	if err := runStage(stage, "copied"); err != nil {
		return err
	}
	if err := root.Link(partial, final); err != nil {
		return err
	}
	if err := root.Remove(partial); err != nil {
		return err
	}
	if err := requireExclusive(root, rel); err != nil {
		return err
	}
	return runStage(stage, "linked")
}

func prepareStore(ctx context.Context, root *os.Root, rel string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := mkdirSlash(root, path.Dir(rel)); err != nil {
		return err
	}
	partial := rel + ".partial"
	name, err := assetOSPath(partial)
	if err != nil {
		return err
	}
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return errStoredObject
	}
	n, err := regularLinkCount(root, partial)
	if err != nil {
		return err
	}
	if n == 1 || (n == 2 && partialIsFinal(root, rel)) {
		return nil
	}
	return errStoredObject
}

func inspectStored(ctx context.Context, root *os.Root, rel string, size int64, hash string) (storedObject, error) {
	err := hashAgrees(ctx, root, rel, size, hash)
	if errors.Is(err, os.ErrNotExist) {
		return storedObject{}, nil
	}
	if err != nil {
		return storedObject{}, err
	}
	n, err := regularLinkCount(root, rel)
	if err != nil {
		return storedObject{}, err
	}
	if n == 1 {
		return storedObject{present: true, exclusive: true}, nil
	}
	if n == 2 && partialIsFinal(root, rel) {
		return storedObject{present: true, exclusive: false}, nil
	}
	return storedObject{}, errStoredObject
}

func partialIsFinal(root *os.Root, rel string) bool {
	final, err := assetOSPath(rel)
	if err != nil {
		return false
	}
	partial, err := assetOSPath(rel + ".partial")
	if err != nil {
		return false
	}
	a, errA := root.Lstat(final)
	b, errB := root.Lstat(partial)
	return errA == nil && errB == nil && a.Mode().IsRegular() && b.Mode().IsRegular() && os.SameFile(a, b)
}

func requireExclusive(root *os.Root, rel string) error {
	n, err := regularLinkCount(root, rel)
	if err != nil {
		return err
	}
	if n != 1 {
		return errStoredObject
	}
	return nil
}

func regularLinkCount(root *os.Root, slash string) (uint64, error) {
	name, err := assetOSPath(slash)
	if err != nil {
		return 0, err
	}
	info, err := root.Lstat(name)
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, errStoredObject
	}
	f, err := safeio.OpenRegular(root, name, info, os.O_RDONLY)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	n, err := linkCount(f)
	if err != nil {
		return 0, err
	}
	current, err := root.Lstat(name)
	if err != nil || !os.SameFile(info, current) {
		return 0, errStoredObject
	}
	return n, nil
}

func reserveNew(control *control, intent operation, holding Holding, writeHolding bool) error {
	intentLine, err := recordBytes(intent)
	if err != nil {
		return err
	}
	completed := intent
	completed.Event = "import_completed"
	completed.Sequence = intent.Sequence + 1
	completedLine, err := recordBytes(completed)
	if err != nil {
		return err
	}
	if err = roomFor(control, operationsName, maxOperations, len(intentLine)+len(completedLine)+32); err != nil {
		return err
	}
	if !writeHolding {
		return nil
	}
	line, err := recordBytes(holding)
	if err != nil {
		return err
	}
	return roomFor(control, holdingsName, maxHoldings, len(line)+32)
}

func reserveResume(control *control, intent operation, holding Holding, writeHolding bool) error {
	completed := intent
	completed.Event = "import_completed"
	completed.Sequence = intent.Sequence + 1
	completed.At = time.Now().UTC().Format(time.RFC3339Nano)
	line, err := recordBytes(completed)
	if err != nil {
		return err
	}
	if err = roomFor(control, operationsName, maxOperations, len(line)+32); err != nil {
		return err
	}
	if !writeHolding {
		return nil
	}
	holdingLine, err := recordBytes(holding)
	if err != nil {
		return err
	}
	return roomFor(control, holdingsName, maxHoldings, len(holdingLine)+32)
}

func recordBytes(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	if len(data) > maxRecord {
		return nil, errors.New("library record exceeds 64 KiB")
	}
	return data, nil
}

func roomFor(control *control, name string, max int, need int) error {
	if need < 1 || need > max {
		return errors.New("library journal exceeds its byte limit; existing records were preserved")
	}
	info, err := control.root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return ErrStateReview
	}
	if info.Size() > int64(max-need) {
		return errors.New("library journal exceeds its byte limit; existing records were preserved")
	}
	return nil
}

func assetOSPath(slash string) (string, error) {
	base := strings.TrimSuffix(slash, ".partial")
	if (strings.HasSuffix(slash, ".partial") && base+".partial" != slash) || strings.Contains(base, `\`) || path.Clean(base) != base {
		return "", errors.New("managed asset path is outside the library store")
	}
	parts := strings.Split(base, "/")
	hash, format, ok := strings.Cut(parts[len(parts)-1], ".")
	if len(parts) != 4 || parts[0] != "assets" || parts[1] != "sha256" || !ok || strings.Contains(format, ".") || !validHash(hash) || parts[2] != hash[:2] || !validFormat(format) {
		return "", errors.New("managed asset path is outside the library store")
	}
	return filepath.FromSlash(slash), nil
}

func hashAgrees(ctx context.Context, root *os.Root, slash string, size int64, hash string) error {
	name, err := assetOSPath(slash)
	if err != nil {
		return err
	}
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != size {
		return errStoredObject
	}
	f, err := safeio.OpenRegular(root, name, info, os.O_RDONLY)
	if err != nil {
		return err
	}
	defer f.Close()
	sum := sha256.New()
	n, err := io.Copy(sum, io.LimitReader(importReader{ctx, f}, size+1))
	if err != nil || n != size || hex.EncodeToString(sum.Sum(nil)) != hash {
		return errStoredObject
	}
	current, err := root.Lstat(name)
	if err != nil || !os.SameFile(info, current) {
		return errStoredObject
	}
	return nil
}

func copyPartial(ctx context.Context, root *os.Root, source, partial string, size int64, hash string) error {
	parent, err := safeio.OpenResolvedRoot(filepath.Dir(source))
	if err != nil {
		return err
	}
	defer parent.Close()
	base := filepath.Base(source)
	before, err := parent.Lstat(base)
	if err != nil {
		return err
	}
	in, err := safeio.OpenRegular(parent, base, before, os.O_RDONLY)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := root.OpenFile(partial, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	sum := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(out, sum), io.LimitReader(importReader{ctx, in}, size+1))
	syncErr := out.Sync()
	closeErr := out.Close()
	after, statErr := in.Stat()
	current, currentErr := parent.Lstat(base)
	if copyErr != nil || syncErr != nil || closeErr != nil || statErr != nil || currentErr != nil || n != size || hex.EncodeToString(sum.Sum(nil)) != hash || !os.SameFile(before, current) || after.Size() != before.Size() {
		_ = root.Remove(partial)
		return errors.Join(copyErr, syncErr, closeErr, statErr, currentErr, errors.New("import copy did not match the assessed source"))
	}
	return nil
}

func mkdirSlash(root *os.Root, slash string) error {
	if slash == "" || slash == "." {
		return nil
	}
	if strings.Contains(slash, `\`) || path.Clean(slash) != slash || strings.HasPrefix(slash, "/") {
		return errStoredObject
	}
	acc := ""
	for _, part := range strings.Split(slash, "/") {
		if part == "" || part == "." || part == ".." {
			return errStoredObject
		}
		acc = path.Join(acc, part)
		name := filepath.FromSlash(acc)
		info, err := root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			if mkErr := root.Mkdir(name, 0700); mkErr != nil {
				return mkErr
			}
			info, err = root.Lstat(name)
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errStoredObject
		}
	}
	return nil
}

func appendCanonical(ctx context.Context, control *control, name string, max int, value any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateControl(control.root); err != nil {
		return err
	}
	if err := sameEntry(control.root, lockName, control.lock); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > maxRecord {
		return errors.New("library record exceeds 64 KiB")
	}
	info, err := control.root.Lstat(name)
	flags := os.O_RDWR
	if errors.Is(err, os.ErrNotExist) {
		flags |= os.O_CREATE | os.O_EXCL
	} else if err != nil {
		return err
	} else if info.Size()+int64(len(data)) > int64(max) {
		return errors.New("library journal exceeds its byte limit; existing records were preserved")
	}
	f, err := openRegular(control.root, name, flags)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.Size() > 0 {
		var tail [1]byte
		if _, err = f.ReadAt(tail[:], st.Size()-1); err != nil || tail[0] != '\n' {
			return ErrStateReview
		}
	}
	if st.Size()+int64(len(data)) > int64(max) {
		return errors.New("library journal exceeds its byte limit; existing records were preserved")
	}
	if _, err = f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}

func within(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func importFormat(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".epub", ".pdf", ".mp3":
		return strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	}
	return ""
}

func runStage(stage importStage, name string) error {
	if stage == nil || name == "" {
		return nil
	}
	return stage(name)
}

func importLimitations() []string {
	return []string{
		"Import copies the assessed bytes and preserves the source. Nothing is deleted.",
		"checked requires an EPUB whose limited checks passed and whose scan reported no detections. Other results stay in review.",
		"Review is not a safety guarantee. PDF page trees, audio decoding, archive extraction, and cleanup are not part of this operation.",
		"File sync is required. Directory-entry durability across power loss depends on the filesystem.",
	}
}

type importReader struct {
	ctx context.Context
	r   io.Reader
}

func (r importReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

// ManagedAsset resolves one committed holding for offline content reads.
// The returned location is relative to the library root.
func ManagedAsset(ctx context.Context, directory, assetID string) (Asset, string, error) {
	if !validAssetID(assetID) {
		return Asset{}, "", errors.New("invalid asset ID")
	}
	abs, err := filepath.Abs(directory)
	if err != nil {
		return Asset{}, "", err
	}
	state, err := LibraryStatus(ctx, abs)
	if err != nil || state.Status != "initialized" {
		if err == nil {
			err = errors.New("content library is not initialized")
		}
		return Asset{}, "", err
	}
	control, _, err := openControl(abs, false)
	if err != nil {
		return Asset{}, "", err
	}
	defer control.close()
	_, lines, err := lockedManaged(control, state.LibraryID)
	if err != nil {
		return Asset{}, "", err
	}
	holding, ok := latestHoldings(lines)[assetID]
	if !ok {
		return Asset{}, "", errors.New("asset not present in managed holdings")
	}
	return Asset{ID: holding.ID, SHA256: holding.SHA256, Bytes: holding.Bytes, Locations: []Location{{Path: filepath.FromSlash(holding.Path), CandidateKind: "managed_holding"}}}, state.LibraryID, nil
}

// ManagedAudit compares committed holdings with their stored bytes.
// It does not repair files or inventory unrelated library contents.
type ManagedAudit struct {
	LibraryID string    `json:"library_id"`
	Root      string    `json:"root"`
	Assets    int       `json:"assets"`
	Matching  int       `json:"matching_checked"`
	Findings  []Finding `json:"findings"`
	Security  string    `json:"security_status"`
}

func AuditManaged(ctx context.Context, directory string) (audit ManagedAudit, err error) {
	audit = ManagedAudit{Findings: []Finding{}, Security: "not_scanned"}
	abs, err := filepath.Abs(directory)
	if err != nil {
		return audit, err
	}
	audit.Root = abs
	state, err := LibraryStatus(ctx, abs)
	if err != nil {
		return audit, err
	}
	if state.Status != "initialized" {
		return audit, errors.New("managed audit requires an initialized library")
	}
	audit.LibraryID = state.LibraryID
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	store, err := safeio.OpenRoot(abs)
	if err != nil {
		return audit, err
	}
	defer func() { err = errors.Join(err, store.Close()) }()
	control, _, err := openControl(abs, false)
	if err != nil {
		return audit, err
	}
	defer control.close()
	_, lines, err := lockedManaged(control, state.LibraryID)
	if err != nil {
		return audit, err
	}
	var read int64
	for _, holding := range latestList(lines) {
		audit.Assets++
		if read > 5<<30-holding.Bytes {
			audit.Findings = append(audit.Findings, Finding{Path: holding.Path, Status: "unverified", Detail: "audit byte budget reached"})
			continue
		}
		hashErr := hashAgrees(ctx, store, holding.Path, holding.Bytes, holding.SHA256)
		if hashErr == nil {
			n, nerr := regularLinkCount(store, holding.Path)
			if nerr != nil || n != 1 {
				hashErr = errStoredObject
			}
		}
		if hashErr != nil {
			status := "changed"
			if errors.Is(hashErr, os.ErrNotExist) {
				status = "missing"
			}
			audit.Findings = append(audit.Findings, Finding{Path: holding.Path, Status: status, ExpectedSHA256: holding.SHA256, Detail: holding.Status + ": " + holding.Reason})
			continue
		}
		read += holding.Bytes
		if holding.Status != "checked" {
			audit.Findings = append(audit.Findings, Finding{Path: holding.Path, Status: "review", ExpectedSHA256: holding.SHA256, Detail: holding.Reason})
			continue
		}
		audit.Matching++
	}
	if len(audit.Findings) > 0 {
		return audit, errors.New("managed holdings need review; stored files were not changed")
	}
	return audit, nil
}

func latestList(lines []Holding) []Holding {
	index := map[string]int{}
	out := []Holding{}
	for _, holding := range lines {
		if at, ok := index[holding.ID]; ok {
			out[at] = holding
			continue
		}
		index[holding.ID] = len(out)
		out = append(out, holding)
	}
	return out
}
