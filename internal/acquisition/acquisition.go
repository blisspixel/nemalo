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

	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/safeio"
	"github.com/blisspixel/nemalo/internal/transfer"
)

type Request struct {
	SourceID string `json:"source_id"`
	File     string `json:"source_file"`
	Output   string `json:"output"`
	MaxBytes int64  `json:"max_bytes"`
}

func (r Request) Validate() error {
	if !discovery.ValidArchiveID(r.SourceID) || r.File == "." || !fs.ValidPath(r.File) || len(r.File) > 1000 || strings.ContainsAny(r.File, "\\:\x00\r\n") || r.Output == "" || r.MaxBytes < 1 || r.MaxBytes > 256<<20 {
		return errors.New("acquire requires archive:ITEM, an exact source filename, a new output directory, and a 1-268435456 byte budget")
	}
	if format(r.File) == "" {
		return errors.New("acquisition currently supports selected EPUB, PDF, and MP3 files")
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
	SchemaVersion    int                `json:"schema_version"`
	Complete         bool               `json:"complete"`
	TransferComplete bool               `json:"transfer_complete"`
	Status           string             `json:"status"`
	Output           string             `json:"output,omitempty"`
	ContentPath      string             `json:"content_path,omitempty"`
	Intent           *Intent            `json:"intent,omitempty"`
	Transfer         transfer.Result    `json:"transfer"`
	FinalURL         string             `json:"final_url,omitempty"`
	CompletedAt      *time.Time         `json:"completed_at,omitempty"`
	Checks           *assessment.Checks `json:"checks,omitempty"`
	Antivirus        string             `json:"antivirus"`
	Durability       string             `json:"durability"`
}

// Acquire resolves metadata anew, reserves a new directory atomically, syncs
// intent before requesting bytes, and creates the receipt last. Existing output
// is never opened for writing. A failed/crashed packet is retained, not resumed,
// removed, adopted, or mistaken for a completed receipt.
func Acquire(ctx context.Context, provider Provider, request Request) (result Result, err error) {
	result = Result{SchemaVersion: 1, Status: "not_started", Antivirus: "not_scanned", Durability: "files_synced_directory_entries_filesystem_dependent"}
	if err := request.Validate(); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return result, err
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
	base := filepath.Base(abs)
	if _, err := parent.Lstat(base); !os.IsNotExist(err) {
		return result, errors.New("output must be a new directory; existing or inaccessible intake is preserved")
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
	if err := parent.Mkdir(base, 0700); err != nil {
		return result, err
	}
	result.Output, result.Status = abs, "retained_incomplete"
	root, err := safeio.OpenRootIn(parent, base)
	if err != nil {
		return result, err
	}
	defer root.Close()
	// Keep selected-asset provenance without copying an entire source inventory.
	evaluation.Files = []discovery.OfferedFile{file}
	request.Output = abs
	result.Intent = &Intent{SchemaVersion: 1, RequestedAt: time.Now().UTC(), Request: request, Selection: evaluation}
	if err := writeRecord(root, "intent.json", result.Intent); err != nil {
		return result, err
	}
	resp, err := provider.Download(ctx, file)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Uncompressed || (resp.Header.Get("Content-Encoding") != "" && resp.Header.Get("Content-Encoding") != "identity") {
		return result, fmt.Errorf("download requires an unencoded HTTP 200 response; received %d", resp.StatusCode)
	}
	if resp.ContentLength > request.MaxBytes || (resp.ContentLength >= 0 && resp.ContentLength != *file.Bytes) {
		return result, errors.New("HTTP size differs from source metadata or exceeds budget")
	}
	name := "content." + format(file.Name)
	f, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return result, err
	}
	result.ContentPath = filepath.Join(abs, name)
	defer f.Close()
	result.Transfer, err = transfer.Copy(ctx, f, resp.Body, request.MaxBytes, transfer.Expectation{Bytes: *file.Bytes, MD5: file.MD5, SHA1: file.SHA1})
	// Retain whatever was transferred on failure, including cancellation.
	if syncErr := f.Sync(); err != nil || syncErr != nil {
		return result, errors.Join(err, syncErr)
	}
	if err := resp.Body.Close(); err != nil {
		return result, err
	}
	result.TransferComplete = true
	result.FinalURL = file.DownloadURL
	if resp.Request != nil && resp.Request.URL != nil {
		result.FinalURL = resp.Request.URL.String()
	}
	checks, err := assessment.InspectContext(ctx, f, result.Transfer.Bytes, format(file.Name))
	result.Checks = &checks
	if err != nil {
		result.Status = "retained_for_review"
		return result, err
	}
	if err := sameContent(root, name, f); err != nil {
		return result, err
	}
	verified, err := transfer.Copy(ctx, io.Discard, io.NewSectionReader(f, 0, result.Transfer.Bytes+1), request.MaxBytes, transfer.Expectation{Bytes: result.Transfer.Bytes})
	if err != nil || verified.SHA256 != result.Transfer.SHA256 {
		return result, errors.Join(err, errors.New("intake bytes changed before receipt publication"))
	}
	if err := f.Close(); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	now := time.Now().UTC()
	result.CompletedAt, result.Complete, result.Status = &now, true, "untrusted_intake"
	if err := writeRecord(root, "receipt.json", result); err != nil {
		result.Complete, result.Status, result.CompletedAt = false, "retained_incomplete", nil
		return result, err
	}
	return result, nil
}

func selectFile(e discovery.Evaluation, request Request) (discovery.OfferedFile, error) {
	if !e.Complete || e.ID != request.SourceID || e.Source != "archive" || e.Access != "no_item_restriction_declared" {
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
	if f.Access != "public_file_candidate" || f.DownloadURL == "" || f.Bytes == nil || *f.Bytes < 1 || *f.Bytes > request.MaxBytes || (f.MD5 == "" && f.SHA1 == "") {
		return f, errors.New("selected file needs public availability, a positive size within budget, and a source checksum")
	}
	if err := (transfer.Expectation{Bytes: *f.Bytes, MD5: f.MD5, SHA1: f.SHA1}).Validate(request.MaxBytes); err != nil {
		return f, err
	}
	return f, nil
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
