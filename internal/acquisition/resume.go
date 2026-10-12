package acquisition

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/safeio"
	"github.com/blisspixel/nemalo/internal/scanner"
	"github.com/blisspixel/nemalo/internal/transfer"
)

// transferRecord is synced before a response body is read. Its strong ETag is
// the only validator a later resume will accept.
type transferRecord struct {
	SchemaVersion int    `json:"schema_version"`
	Status        int    `json:"status"`
	ETag          string `json:"etag,omitempty"`
	StrongETag    bool   `json:"strong_etag"`
	LastModified  string `json:"last_modified,omitempty"`
	ContentLength int64  `json:"content_length"`
	FinalURL      string `json:"final_url"`
	File          string `json:"source_file"`
}

func responseRecord(resp *http.Response, file discovery.OfferedFile) transferRecord {
	etag := resp.Header.Get("ETag")
	if len(etag) > 256 {
		etag = ""
	}
	final := file.DownloadURL
	if resp.Request != nil && resp.Request.URL != nil {
		final = resp.Request.URL.String()
	}
	modified := resp.Header.Get("Last-Modified")
	if len(modified) > 64 {
		modified = ""
	}
	return transferRecord{SchemaVersion: 1, Status: resp.StatusCode, ETag: etag, StrongETag: discovery.StrongETag(etag), LastModified: modified, ContentLength: resp.ContentLength, FinalURL: final, File: file.Name}
}

// RangeProvider can request a file from an offset, conditional on a validator.
type RangeProvider interface {
	DownloadFrom(ctx context.Context, file discovery.OfferedFile, offset int64, validator string) (*http.Response, error)
}

// ErrResumeRefused means the retained bytes cannot be continued safely: no
// strong validator, changed declarations, or a reply other than the missing
// range. The packet is left untouched.
var ErrResumeRefused = errors.New("transfer cannot be resumed")

func refused(msg string) error { return fmt.Errorf("%w: %s", ErrResumeRefused, msg) }

// resumeAcquire continues an interrupted transfer in its own packet. The
// packet must hold this operation's intent and transfer records, partial
// content, and no receipt. A fresh evaluation must still declare the same file,
// size, and checksums, and the server must answer the If-Range request with
// exactly the missing range of the same strong validator; anything else
// leaves the packet untouched. The whole file is then verified, inspected,
// scanned, and receipted exactly as a transfer done at once.
func resumeAcquire(ctx, outer context.Context, provider Provider, request Request, result Result) (Result, error) {
	ranged, ok := provider.(RangeProvider)
	if !ok {
		return result, refused("this source cannot resume a transfer")
	}
	abs, err := filepath.Abs(request.Output)
	if err != nil {
		return result, err
	}
	parent, err := safeio.OpenResolvedRoot(filepath.Dir(abs))
	if err != nil {
		return result, err
	}
	defer parent.Close()
	root, err := safeio.OpenRootIn(parent, filepath.Base(abs))
	if err != nil {
		return result, fmt.Errorf("resume requires the existing intake packet: %w", err)
	}
	defer root.Close()
	evaluation, err := provider.Evaluate(ctx, request.SourceID)
	if err != nil {
		return result, err
	}
	var intent Intent
	if err := readRecord(root, "intent.json", &intent); err == nil && intent.SchemaVersion == 1 {
		request.MaxBytes = max(request.MaxBytes, intent.Request.MaxBytes)
	}
	file, err := selectFile(evaluation, request)
	if err != nil {
		return result, err
	}
	state, result, err := resumeTransfer(ctx, ranged, root, abs, evaluation, file, request, result)
	if err != nil {
		return result, err
	}
	var scan *scanner.Result
	if request.Scanner != nil && !request.NoScan {
		s := request.Scanner.Scan(outer, result.ContentPath)
		scan = &s
	}
	return finishPacket(outer, state, result, scan)
}

// resumable reports whether a packet holds a transfer that resumeTransfer
// could continue for file: intent and transfer records for it with a strong
// validator, partial content, and no receipt.
func resumable(root *os.Root, file discovery.OfferedFile) bool {
	if _, err := root.Lstat("receipt.json"); !errors.Is(err, os.ErrNotExist) {
		return false
	}
	var intent Intent
	var rec transferRecord
	if readRecord(root, "intent.json", &intent) != nil || len(intent.Selection.Files) != 1 || intent.Selection.Files[0].Name != file.Name {
		return false
	}
	if readRecord(root, "transfer.json", &rec) != nil || !rec.StrongETag || !discovery.StrongETag(rec.ETag) || rec.File != file.Name {
		return false
	}
	info, err := root.Lstat("content." + format(file.Name))
	return err == nil && info.Mode().IsRegular() && file.Bytes != nil && info.Size() > 0 && info.Size() < *file.Bytes
}

// resumeTransfer continues the content of the packet in root and verifies the
// whole file. On success the returned state owns root's packet handles.
func resumeTransfer(ctx context.Context, ranged RangeProvider, root *os.Root, abs string, evaluation discovery.Evaluation, file discovery.OfferedFile, request Request, result Result) (*packetState, Result, error) {
	if _, err := root.Lstat("receipt.json"); !errors.Is(err, os.ErrNotExist) {
		return nil, result, errors.New("this packet already has a receipt; nothing to resume")
	}
	var intent Intent
	if err := readRecord(root, "intent.json", &intent); err != nil || intent.SchemaVersion != 1 || len(intent.Selection.Files) != 1 {
		return nil, result, refused("the packet has no intent from acquire")
	}
	var rec transferRecord
	if err := readRecord(root, "transfer.json", &rec); err != nil || rec.SchemaVersion != 1 || rec.Status != http.StatusOK {
		return nil, result, refused("this packet has no transfer record, so its bytes cannot be resumed; start a new output")
	}
	if !rec.StrongETag || !discovery.StrongETag(rec.ETag) {
		return nil, result, refused("the source sent no strong validator for this file, so it cannot be resumed safely; start a new output")
	}
	planned := intent.Selection.Files[0]
	if intent.Request.SourceID != request.SourceID || planned.Name != file.Name || rec.File != planned.Name {
		return nil, result, refused("the packet was created for another item or file")
	}
	if planned.Bytes == nil || file.Bytes == nil || *planned.Bytes != *file.Bytes || planned.MD5 != file.MD5 || planned.SHA1 != file.SHA1 {
		return nil, result, refused("source declarations for this file changed since the transfer began; start a new output")
	}
	name := "content." + format(file.Name)
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		return nil, result, refused("the packet has no partial content to resume")
	}
	offset := info.Size()
	if offset < 1 || offset >= *file.Bytes {
		return nil, result, refused(fmt.Sprintf("the partial content holds %d of %d bytes; nothing can be resumed", offset, *file.Bytes))
	}
	f, err := safeio.OpenRegular(root, name, info, os.O_RDWR)
	if err != nil {
		return nil, result, err
	}
	p := &packetState{root: root, f: f, name: name, request: intent.Request, evaluation: intent.Selection, alternatives: alternativesFor(evaluation, file, request.MaxBytes)}
	p.request.Scanner, p.request.NoScan, p.request.OnDetection, p.request.Quarantine = request.Scanner, request.NoScan, request.OnDetection, request.Quarantine
	p.request.MaxBytes = max(request.MaxBytes, intent.Request.MaxBytes)
	result.Output, result.Status, result.Intent = abs, "retained_incomplete", &intent
	result.ContentPath, result.ResumedFrom = filepath.Join(abs, name), offset
	resp, err := ranged.DownloadFrom(ctx, file, offset, rec.ETag)
	if err != nil {
		_ = f.Close()
		return nil, result, err
	}
	defer resp.Body.Close()
	if err := validRange(resp, offset, *file.Bytes, rec.ETag); err != nil {
		_ = f.Close()
		return nil, result, err
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		_ = f.Close()
		return nil, result, err
	}
	result.Transfer, err = transfer.Continue(ctx, f, io.NewSectionReader(f, 0, offset), offset, resp.Body, p.request.MaxBytes, transfer.Expectation{Bytes: *file.Bytes, MD5: file.MD5, SHA1: file.SHA1})
	state, result, err := completeTransfer(ctx, p, result, resp, file, err)
	if err != nil {
		_ = f.Close()
		return nil, result, err
	}
	return state, result, nil
}

// validRange accepts only a 206 response carrying exactly the missing bytes of
// the same representation: Content-Range from offset to the end of the
// declared size, a matching length, and, when sent, the same ETag.
func validRange(resp *http.Response, offset, size int64, etag string) error {
	if resp.StatusCode == http.StatusOK {
		return refused("the source sent the whole file instead of the missing range, so it changed or cannot resume; the partial content was kept")
	}
	if resp.StatusCode != http.StatusPartialContent || resp.Uncompressed || (resp.Header.Get("Content-Encoding") != "" && resp.Header.Get("Content-Encoding") != "identity") {
		return fmt.Errorf("resume requires an unencoded HTTP 206 response; received %d", resp.StatusCode)
	}
	if got := resp.Header.Get("ETag"); got != "" && got != etag {
		return refused("the source sent a different validator; the partial content was kept")
	}
	want := "bytes " + strconv.FormatInt(offset, 10) + "-" + strconv.FormatInt(size-1, 10) + "/" + strconv.FormatInt(size, 10)
	if strings.TrimSpace(resp.Header.Get("Content-Range")) != want || (resp.ContentLength >= 0 && resp.ContentLength != size-offset) {
		return refused("the source sent a different range than " + want)
	}
	return nil
}
