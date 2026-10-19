// Package acquisition creates explicit, exclusive untrusted intake packets.
// Receipt creation records a completed transfer, never checked publication.
package acquisition

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/safeio"
	"github.com/blisspixel/nemalo/internal/scanner"
	"github.com/blisspixel/nemalo/internal/transfer"
)

type Request struct {
	SourceID string `json:"source_id"`
	File     string `json:"source_file"`
	Output   string `json:"output"`
	MaxBytes int64  `json:"max_bytes"`
	// NoScan skips the post-transfer scan. OnDetection is "quarantine" (the
	// default) or "delete". Quarantine is the Nemalo-owned directory that
	// receives detected files; it is required when a scanner is supplied.
	NoScan      bool   `json:"no_scan,omitempty"`
	OnDetection string `json:"on_detection,omitempty"`
	Quarantine  string `json:"quarantine,omitempty"`
	// Scanner is the installed antivirus adapter. Nil means no scan.
	Scanner assessment.Scanner `json:"-"`
	// Recording is set only for a track of acquire --recording.
	Recording *RecordingPosition `json:"recording,omitempty"`
	// Resume continues an interrupted transfer into its existing packet. It is
	// a property of the call, never of the recorded intent.
	Resume bool `json:"-"`
}

func (r Request) Validate() error {
	archiveID := discovery.ValidArchiveID(r.SourceID)
	gutenbergID := discovery.ValidGutenbergID(r.SourceID)
	if (!archiveID && !gutenbergID) || r.File == "." || !fs.ValidPath(r.File) || len(r.File) > 1000 || strings.ContainsAny(r.File, "\\:\x00\r\n") || r.Output == "" || r.MaxBytes < 1 || r.MaxBytes > 256<<20 {
		return errors.New("acquire requires archive:ITEM or gutenberg:NUMBER, an exact source filename, a new output directory, and a 1-268435456 byte budget")
	}
	if archiveID && format(r.File) == "" {
		return errors.New("acquisition currently supports selected EPUB, PDF, and MP3 files")
	}
	if gutenbergID && !discovery.GutenbergEPUBName(r.SourceID, r.File) {
		return errors.New("gutenberg acquisition supports one EPUB named by the bibliographic record, such as 1342.epub.noimages")
	}
	if r.OnDetection != "" && r.OnDetection != "quarantine" && r.OnDetection != "delete" {
		return errors.New("on-detection must be quarantine or delete")
	}
	if r.Quarantine != "" && !filepath.IsAbs(r.Quarantine) {
		return errors.New("quarantine directory must be absolute")
	}
	return nil
}

type Provider interface {
	Evaluate(context.Context, string) (discovery.Evaluation, error)
	Download(context.Context, discovery.OfferedFile) (*http.Response, error)
}

type Intent struct {
	SchemaVersion int                  `json:"schema_version"`
	RequestedAt   time.Time            `json:"requested_at"`
	Request       Request              `json:"request"`
	Selection     discovery.Evaluation `json:"selection"`
}

type Result struct {
	SchemaVersion    int    `json:"schema_version"`
	Complete         bool   `json:"complete"`
	TransferComplete bool   `json:"transfer_complete"`
	Status           string `json:"status"`
	Output           string `json:"output,omitempty"`
	ContentPath      string `json:"content_path,omitempty"`
	// SizeNote records a Gutenberg declared extent that differed from the
	// response length. The transfer checked the response length.
	SizeNote    string             `json:"size_note,omitempty"`
	Intent      *Intent            `json:"intent,omitempty"`
	Transfer    transfer.Result    `json:"transfer"`
	FinalURL    string             `json:"final_url,omitempty"`
	CompletedAt *time.Time         `json:"completed_at,omitempty"`
	Checks      *assessment.Checks `json:"checks,omitempty"`
	Antivirus   string             `json:"antivirus"`
	Scan        *scanner.Result    `json:"scan,omitempty"`
	// QuarantinedPath is where detected bytes were moved; empty if deleted.
	QuarantinedPath string `json:"quarantined_path,omitempty"`
	// Alternatives are other eligible files in the same fresh evaluation, and
	// SearchSuggestion a query for other sources. Both appear on a detection.
	Alternatives     []Alternative `json:"alternatives,omitempty"`
	SearchSuggestion string        `json:"search_suggestion,omitempty"`
	Durability       string        `json:"durability"`
	// ResumedFrom is the byte offset a resumed transfer continued from.
	ResumedFrom int64 `json:"resumed_from,omitempty"`
	// TransferRecorded is set once transfer.json is synced, before the body.
	// It is presentation state for this call. A receipt does not store it.
	TransferRecorded bool `json:"-"`
	// StrongValidator is presentation state for this call. Resume is offered
	// only when the response carried a strong ETag. A receipt does not store it.
	StrongValidator bool `json:"-"`
	// Continuations counts how often this call continued its own transfer
	// after a dropped connection, each as a checked resume.
	Continuations int `json:"continuations,omitempty"`
}

// Alternative is another acquirable file from the same evaluation. It carries
// source declarations only; rights, quality, and safety remain unverified.
type Alternative struct {
	File   string `json:"file"`
	Format string `json:"format"`
	Bytes  int64  `json:"declared_bytes"`
}

// ErrDetected reports that the installed scanner flagged the downloaded bytes.
var ErrDetected = errors.New("antivirus reported a detection; the download was not kept as intake")

// Acquire resolves metadata anew, reserves a new directory atomically, syncs
// intent before requesting bytes, scans the verified bytes when a scanner is
// supplied, and creates the receipt last. Existing output is never opened for
// writing. A failed/crashed packet is retained, not resumed, removed, adopted,
// or mistaken for a completed receipt. A detection moves the bytes to the
// Nemalo quarantine (or deletes them, if requested) so they never become intake.
//
// A transfer that drops after some bytes, from a source that sent a strong
// validator, is continued in its own packet up to maxContinuations times, each
// exactly as an explicit --resume: fresh metadata, the same validator, only
// the missing range, and the whole file verified at the end. A refused resume,
// a checksum or size mismatch, a budget, or cancellation is not retried.
func Acquire(ctx context.Context, provider Provider, request Request) (Result, error) {
	result, err := acquireOnce(ctx, provider, request)
	for attempt := 1; err != nil && attempt <= maxContinuations && continuable(ctx, result, err); attempt++ {
		select {
		case <-ctx.Done():
			return result, err
		case <-time.After(continuationPause(attempt)):
		}
		again := request
		again.Resume, again.Output = true, result.Output
		next, nextErr := acquireOnce(ctx, provider, again)
		if next.Transfer.Bytes == 0 {
			// The try failed before any body; the packet keeps what it had.
			next.Transfer, next.TransferRecorded, next.StrongValidator = result.Transfer, result.TransferRecorded, result.StrongValidator
		}
		next.Continuations = result.Continuations + 1
		result, err = next, nextErr
	}
	return result, err
}

// maxContinuations bounds automatic continuation of one acquisition.
const maxContinuations = 3

// continuationPause waits longer before each continuation; tests shorten it.
var continuationPause = func(attempt int) time.Duration { return time.Duration(attempt) * 2 * time.Second }

// continuable reports a transfer that stopped for a transport reason with
// bytes retained in a packet that a strong validator lets continue.
func continuable(ctx context.Context, r Result, err error) bool {
	if ctx.Err() != nil || r.Output == "" || r.TransferComplete || !r.TransferRecorded || !r.StrongValidator || r.Transfer.Bytes < 1 {
		return false
	}
	// A transfer deadline that passed while the caller still waits is progress
	// to continue, not a failure of the source.
	return !errors.Is(err, ErrResumeRefused) && transfer.Transient(err)
}

func acquireOnce(ctx context.Context, provider Provider, request Request) (result Result, err error) {
	result = Result{SchemaVersion: 1, Status: "not_started", Antivirus: "not_scanned", Durability: "files_synced_directory_entries_filesystem_dependent"}
	if err := request.Validate(); err != nil {
		return result, err
	}
	if request.Scanner != nil && !request.NoScan && request.Quarantine == "" && request.OnDetection != "delete" {
		return result, errors.New("scanning requires a quarantine directory for detected files")
	}
	outer := ctx
	ctx, cancel := context.WithTimeout(ctx, transferDeadline)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if request.Resume {
		return resumeAcquire(ctx, outer, provider, request, result)
	}
	abs, err := filepath.Abs(request.Output)
	if err != nil {
		return result, err
	}
	parentPath, base := filepath.Dir(abs), filepath.Base(abs)
	// A missing parent is created only after the source evaluation succeeds,
	// so a refused request leaves no new directories behind.
	parentMissing := false
	if _, statErr := os.Lstat(parentPath); errors.Is(statErr, os.ErrNotExist) {
		parentMissing = true
	} else if err := outputAvailable(parentPath, base); err != nil {
		return result, err
	}
	evaluation, err := provider.Evaluate(ctx, request.SourceID)
	if err != nil {
		return result, err
	}
	file, err := selectFile(evaluation, request)
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if parentMissing {
		if err := os.MkdirAll(parentPath, 0o755); err != nil {
			return result, fmt.Errorf("create output parent: %w", err)
		}
	}
	parent, err := safeio.OpenResolvedRoot(parentPath)
	if err != nil {
		return result, err
	}
	defer parent.Close()
	return acquireInto(ctx, outer, provider, parent, abs, evaluation, file, request, result)
}

// acquireInto creates the new packet directory abs inside parent and runs one
// selected file through intent, transfer, checks, scan, and receipt. ctx bounds
// the transfer phases; outer is the caller's context and bounds the scan.
func acquireInto(ctx, outer context.Context, provider Provider, parent *os.Root, abs string, evaluation discovery.Evaluation, file discovery.OfferedFile, request Request, result Result) (Result, error) {
	p, result, err := transferPacket(ctx, provider, parent, abs, evaluation, file, request, result)
	if err != nil {
		return result, err
	}
	var scan *scanner.Result
	if request.Scanner != nil && !request.NoScan {
		// The scan has its own scanner deadline and is not cut short by the
		// transfer deadline; the caller's cancellation still applies.
		s := request.Scanner.Scan(outer, result.ContentPath)
		scan = &s
	}
	return finishPacket(outer, p, result, scan)
}

// packetState is a transferred, inspected, and read-back-verified packet whose
// receipt is not yet written. It holds the packet root and the open content.
type packetState struct {
	root         *os.Root
	f            *os.File
	name         string
	request      Request
	evaluation   discovery.Evaluation
	alternatives []Alternative
}

func (p *packetState) close() {
	_ = p.f.Close()
	_ = p.root.Close()
}

// transferPacket reserves the packet directory, syncs intent, transfers and
// inspects the content, and verifies it by read-back. On success the caller
// must pass the state to finishPacket.
func transferPacket(ctx context.Context, provider Provider, parent *os.Root, abs string, evaluation discovery.Evaluation, file discovery.OfferedFile, request Request, result Result) (_ *packetState, _ Result, err error) {
	base := filepath.Base(abs)
	if err := parent.Mkdir(base, 0700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, result, errors.New("output must be a new directory; existing or inaccessible intake is preserved")
		}
		return nil, result, err
	}
	result.Output, result.Status = abs, "retained_incomplete"
	root, err := safeio.OpenRootIn(parent, base)
	if err != nil {
		return nil, result, err
	}
	p := &packetState{root: root, request: request}
	defer func() {
		if err != nil {
			if p.f != nil {
				_ = p.f.Close()
			}
			_ = root.Close()
		}
	}()
	p.alternatives = alternativesFor(evaluation, file, request.MaxBytes)
	// Keep selected-asset provenance without copying an entire source inventory.
	evaluation.Files, evaluation.Recordings = []discovery.OfferedFile{file}, nil
	p.evaluation = evaluation
	request.Output = abs
	result.Intent = &Intent{SchemaVersion: 1, RequestedAt: time.Now().UTC(), Request: request, Selection: evaluation}
	if err := writeRecord(root, "intent.json", result.Intent); err != nil {
		return nil, result, err
	}
	resp, err := provider.Download(ctx, file)
	if err != nil {
		return nil, result, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Uncompressed || (resp.Header.Get("Content-Encoding") != "" && resp.Header.Get("Content-Encoding") != "identity") {
		return nil, result, fmt.Errorf("download requires an unencoded HTTP 200 response; received %d", resp.StatusCode)
	}
	expect := transfer.Expectation{Bytes: *file.Bytes, MD5: file.MD5, SHA1: file.SHA1}
	if file.Access == discovery.GutenbergEPUBAccess {
		// The bibliographic extent is not a checksum. Require a response length
		// inside the budget and check the body against that length.
		if resp.ContentLength < 1 || resp.ContentLength > request.MaxBytes {
			return nil, result, errors.New("gutenberg download needs a Content-Length within the byte budget")
		}
		expect = transfer.Expectation{Bytes: resp.ContentLength}
		if resp.ContentLength != *file.Bytes {
			result.SizeNote = fmt.Sprintf("The bibliographic record declared %d bytes. The response length was %d. The transfer checked the response length. The record has no checksum.", *file.Bytes, resp.ContentLength)
		}
	} else if resp.ContentLength > request.MaxBytes || (resp.ContentLength >= 0 && resp.ContentLength != *file.Bytes) {
		return nil, result, errors.New("HTTP size differs from source metadata or exceeds budget")
	}
	// The response's validator is synced before the body, so an interrupted
	// transfer can later resume only against the same bytes.
	record := responseRecord(resp, file)
	if err := writeRecord(root, "transfer.json", record); err != nil {
		return nil, result, err
	}
	result.TransferRecorded = true
	result.StrongValidator = record.StrongETag
	p.name = "content." + contentFormat(file)
	p.f, err = root.OpenFile(p.name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, result, err
	}
	result.ContentPath = filepath.Join(abs, p.name)
	result.Transfer, err = transfer.Copy(ctx, p.f, resp.Body, request.MaxBytes, expect)
	return completeTransfer(ctx, p, result, resp, file, err)
}

// completeTransfer syncs the content after a copy, then inspects and verifies
// it. Whatever was transferred is retained on failure, including cancellation.
func completeTransfer(ctx context.Context, p *packetState, result Result, resp *http.Response, file discovery.OfferedFile, copyErr error) (*packetState, Result, error) {
	if syncErr := p.f.Sync(); copyErr != nil || syncErr != nil {
		return nil, result, errors.Join(copyErr, syncErr)
	}
	if err := resp.Body.Close(); err != nil {
		return nil, result, err
	}
	result.TransferComplete = true
	result.FinalURL = file.DownloadURL
	if resp.Request != nil && resp.Request.URL != nil {
		result.FinalURL = resp.Request.URL.String()
	}
	checks, err := assessment.InspectContext(ctx, p.f, result.Transfer.Bytes, contentFormat(file))
	result.Checks = &checks
	if err != nil {
		result.Status = "retained_for_review"
		return nil, result, err
	}
	if err := verifyIntake(ctx, p.root, p.name, p.f, result.Transfer, p.request.MaxBytes); err != nil {
		return nil, result, err
	}
	return p, result, nil
}

// finishPacket binds an optional scan result to the verified bytes and writes
// the receipt, or handles a detection. It always releases the packet state.
func finishPacket(outer context.Context, p *packetState, result Result, scan *scanner.Result) (Result, error) {
	defer p.close()
	if scan != nil {
		result.Scan, result.Antivirus = scan, scan.Status
		// Bind the scan to the receipt bytes. Host antivirus acting on its own
		// (removal, quarantine) surfaces here as a changed or missing file.
		if err := verifyIntake(outer, p.root, p.name, p.f, result.Transfer, p.request.MaxBytes); err != nil {
			changed := errors.Join(errors.New("intake bytes changed or were removed during the scan; host antivirus may have acted"), err)
			if scan.Status != "findings" {
				result.Antivirus = "incomplete"
				return result, changed
			}
			// A reported detection stays a detection, and is recorded, even
			// though the bytes can no longer be quarantined by Nemalo.
			return recordDetection(p.root, result, "detected_changed_during_scan", p.alternatives, p.evaluation, changed)
		}
		if scan.Status == "findings" {
			return detected(outer, p.root, p.name, p.f, result, p.request, p.alternatives, p.evaluation)
		}
	}
	if err := p.f.Close(); err != nil {
		return result, err
	}
	// Transfer phases ran under the five-minute deadline; a scan may finish
	// after it, so only the caller's cancellation gates the receipt.
	if err := outer.Err(); err != nil {
		return result, err
	}
	now := time.Now().UTC()
	result.CompletedAt, result.Complete, result.Status = &now, true, "untrusted_intake"
	if err := writeRecord(p.root, "receipt.json", result); err != nil {
		result.Complete, result.Status, result.CompletedAt = false, "retained_incomplete", nil
		return result, err
	}
	return result, nil
}

// outputAvailable refuses an existing or inaccessible output before any request.
func outputAvailable(parentPath, base string) error {
	parent, err := safeio.OpenResolvedRoot(parentPath)
	if err != nil {
		return err
	}
	defer parent.Close()
	if _, err := parent.Lstat(base); !os.IsNotExist(err) {
		return errors.New("output must be a new directory; existing or inaccessible intake is preserved")
	}
	return nil
}

// verifyIntake proves the named content is still the transferred file and its
// bytes still hash to the transfer SHA-256.
func verifyIntake(ctx context.Context, root *os.Root, name string, f *os.File, transferred transfer.Result, limit int64) error {
	if err := sameContent(root, name, f); err != nil {
		return err
	}
	verified, err := transfer.Copy(ctx, io.Discard, io.NewSectionReader(f, 0, transferred.Bytes+1), limit, transfer.Expectation{Bytes: transferred.Bytes})
	if err != nil || verified.SHA256 != transferred.SHA256 {
		return errors.Join(err, errors.New("intake bytes changed before receipt publication"))
	}
	return nil
}

// detected removes flagged bytes from the intake packet: by default it moves
// them into the Nemalo quarantine, otherwise it deletes them. The receipt
// records the detection, so the packet can never be imported.
func detected(ctx context.Context, root *os.Root, name string, f *os.File, result Result, request Request, alternatives []Alternative, evaluation discovery.Evaluation) (Result, error) {
	destination := ""
	var removeErr error
	if request.OnDetection != "delete" {
		destination, removeErr = quarantine(ctx, f, name, request.Quarantine, result.Transfer)
	}
	if err := f.Close(); err != nil && removeErr == nil {
		removeErr = err
	}
	if removeErr == nil {
		removeErr = root.Remove(name)
	}
	switch {
	case removeErr != nil:
		// The bytes stay in the packet, but the receipt still records the
		// detection, and import refuses any packet whose receipt is not intake.
		return recordDetection(root, result, "detected_retained", alternatives, evaluation, fmt.Errorf("the detected file could not be removed from the packet: %w", removeErr))
	case request.OnDetection == "delete":
		result.ContentPath = ""
		return recordDetection(root, result, "detected_deleted", alternatives, evaluation, nil)
	}
	result.ContentPath, result.QuarantinedPath = "", destination
	return recordDetection(root, result, "detected_quarantined", alternatives, evaluation, nil)
}

// recordDetection writes the receipt for every detection outcome, with the
// alternatives and search suggestion, and returns ErrDetected.
func recordDetection(root *os.Root, result Result, status string, alternatives []Alternative, evaluation discovery.Evaluation, cause error) (Result, error) {
	result.Status, result.Antivirus = status, "findings"
	result.Alternatives, result.SearchSuggestion = alternatives, searchSuggestion(evaluation)
	if err := writeRecord(root, "receipt.json", result); err != nil {
		return result, errors.Join(ErrDetected, cause, err)
	}
	return result, errors.Join(ErrDetected, cause)
}

// quarantine copies the verified packet content, through its open handle, into
// a new private directory under the quarantine root, named so no reader or
// thumbnailer treats it as a book. The copy is synced and verified before the
// caller removes the packet copy. Both directories are used only through their
// opened roots; no validated pathname is reopened.
func quarantine(ctx context.Context, f *os.File, name, directory string, transferred transfer.Result) (string, error) {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	q, err := safeio.OpenResolvedRoot(directory)
	if err != nil {
		return "", err
	}
	defer q.Close()
	entry := time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + transferred.SHA256[:16]
	if err := q.Mkdir(entry, 0o700); err != nil {
		return "", err
	}
	quarantined := name + ".quarantined"
	out, err := q.OpenFile(filepath.Join(entry, quarantined), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	copied, copyErr := transfer.Copy(ctx, out, io.NewSectionReader(f, 0, transferred.Bytes+1), transferred.Bytes, transfer.Expectation{Bytes: transferred.Bytes})
	syncErr, closeErr := out.Sync(), out.Close()
	if err := errors.Join(copyErr, syncErr, closeErr); err != nil || copied.SHA256 != transferred.SHA256 {
		return "", errors.Join(err, errors.New("quarantine copy did not verify; the packet copy was kept"))
	}
	return filepath.Join(directory, entry, quarantined), nil
}

// Eligible lists the files acquire would accept from this evaluation, in
// source order, at most ten, within limit bytes. It is the same check as
// selection; acquire still evaluates the source again before any transfer.
func Eligible(e discovery.Evaluation, limit int64) []Alternative {
	return alternativesFor(e, discovery.OfferedFile{}, limit)
}

// alternativesFor lists other eligible files from the same evaluation, in
// source order. It applies the same eligibility as selectFile.
func alternativesFor(e discovery.Evaluation, selected discovery.OfferedFile, limit int64) []Alternative {
	out := []Alternative{}
	for _, f := range e.Files {
		if f.Name == selected.Name || contentFormat(f) == "" || len(out) == 10 {
			continue
		}
		if _, err := selectFile(e, Request{SourceID: e.ID, File: f.Name, MaxBytes: limit}); err != nil {
			continue
		}
		out = append(out, Alternative{File: f.Name, Format: contentFormat(f), Bytes: *f.Bytes})
	}
	return out
}

// searchSuggestion builds a plain query from the first declared title and
// creator. It is a suggestion for the user to run, never an automatic request.
func searchSuggestion(e discovery.Evaluation) string {
	words := []string{}
	for _, values := range [][]string{e.Title, e.Creators} {
		if len(values) > 0 {
			words = append(words, strings.Join(strings.FieldsFunc(values[0], func(r rune) bool {
				return r == '"' || r == ':' || r == '(' || r == ')' || unicode.IsSpace(r) || unicode.IsControl(r)
			}), " "))
		}
	}
	query := strings.TrimSpace(strings.Join(words, " "))
	if len(query) > 200 {
		query = strings.ToValidUTF8(query[:200], "")
	}
	return query
}

func selectFile(e discovery.Evaluation, request Request) (discovery.OfferedFile, error) {
	if !e.Complete || e.ID != request.SourceID {
		return discovery.OfferedFile{}, errors.New("source evaluation is incomplete, restricted, uncertain, or mismatched")
	}
	var selected *discovery.OfferedFile
	for _, f := range e.Files {
		if f.Name != request.File {
			continue
		}
		if selected != nil {
			return f, errors.New("source returned duplicate selected files")
		}
		selected = &f
	}
	if selected == nil {
		return discovery.OfferedFile{}, errors.New("selected file is not in fresh source metadata")
	}
	f := *selected
	switch {
	case e.Source == "archive" && e.Access == "no_item_restriction_declared":
		if f.Access != "public_file_candidate" || f.DownloadURL == "" || f.Bytes == nil || *f.Bytes < 1 || *f.Bytes > request.MaxBytes || (f.MD5 == "" && f.SHA1 == "") {
			return f, errors.New("selected file needs public availability, a positive size within budget, and a source checksum")
		}
	case e.Source == "gutenberg" && e.Access == discovery.GutenbergMirrorAccess:
		if f.Access != discovery.GutenbergEPUBAccess || f.DownloadURL == "" || f.Bytes == nil || *f.Bytes < 1 || *f.Bytes > request.MaxBytes || contentFormat(f) != "epub" {
			return f, errors.New("selected file is not an offered Gutenberg EPUB with a positive declared extent within budget")
		}
	default:
		return f, errors.New("source evaluation is incomplete, restricted, uncertain, or mismatched")
	}
	if err := (transfer.Expectation{Bytes: *f.Bytes, MD5: f.MD5, SHA1: f.SHA1}).Validate(request.MaxBytes); err != nil {
		return f, err
	}
	return f, nil
}

// contentFormat is the packet suffix. A Gutenberg EPUB keeps its bibliographic
// name, which does not end in .epub, and is stored as content.epub.
func contentFormat(file discovery.OfferedFile) string {
	if file.Format == "application/epub+zip" {
		return "epub"
	}
	return format(file.Name)
}

func format(name string) string {
	switch ext := strings.ToLower(path.Ext(name)); ext {
	case ".epub", ".pdf", ".mp3":
		return ext[1:]
	}
	return ""
}

func writeRecord(root *os.Root, name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > 4<<20 {
		return errors.New("acquisition record exceeds 4 MiB")
	}
	pending := name + ".pending"
	f, err := root.OpenFile(pending, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(append(data, '\n'))
	syncErr := f.Sync()
	if err := errors.Join(writeErr, syncErr, f.Close()); err != nil {
		return err
	}
	if err := root.Link(pending, name); err != nil {
		return err
	}
	return root.Remove(pending)
}

func sameContent(root *os.Root, name string, file *os.File) error {
	a, err := root.Lstat(name)
	if err != nil {
		return err
	}
	b, err := file.Stat()
	if err != nil {
		return err
	}
	if !a.Mode().IsRegular() || !os.SameFile(a, b) {
		return errors.New("intake content path was replaced during acquisition")
	}
	return nil
}

// transferDeadline bounds metadata, transfer, and local checks; a scan runs
// after it under the scanner's own deadline. Tests shorten it.
var transferDeadline = 5 * time.Minute
