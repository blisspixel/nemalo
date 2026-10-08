package assessment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blisspixel/nemalo/internal/scanner"
)

type Options struct {
	Scan           bool
	ExpectedBytes  int64
	ExpectedSHA256 string
}

type Scanner interface {
	Scan(context.Context, string) scanner.Result
}

type Report struct {
	File           string         `json:"file"`
	CheckedAt      time.Time      `json:"checked_at"`
	Bytes          int64          `json:"bytes"`
	SHA256         string         `json:"sha256"`
	DetectedFormat string         `json:"detected_format"`
	Status         string         `json:"status"`
	Checks         Checks         `json:"checks"`
	Antivirus      scanner.Result `json:"antivirus"`
	Findings       []string       `json:"findings"`
	Limitations    []string       `json:"limitations"`
}

func (o Options) Validate() error {
	if o.ExpectedBytes < 0 || o.ExpectedBytes > 256<<20 {
		return errors.New("expected bytes must be 0 (unspecified) through 268435456")
	}
	if o.ExpectedSHA256 != "" {
		decoded, err := hex.DecodeString(o.ExpectedSHA256)
		if err != nil || len(decoded) != 32 {
			return errors.New("expected SHA-256 must contain exactly 64 hexadecimal digits")
		}
	}
	return nil
}

// Check assesses a bounded private snapshot. It never opens a reader, renders a
// document, extracts to the source tree, or mutates the selected file.
func Check(ctx context.Context, file string, options Options, av Scanner) (report Report, err error) {
	report = Report{File: file, CheckedAt: time.Now().UTC(), Status: "incomplete", Findings: []string{}, Antivirus: scanner.Result{Status: "not_scanned", ExitCode: -1}, Limitations: []string{"Limited checks do not establish safety, semantic completeness, or edition quality.", "PDF page trees and active content, and audio decoding/duration are not inspected.", "EPUB text counts are tokenizer measurements, not rendered pages or proof of complete text."}}
	if err := options.Validate(); err != nil {
		return report, err
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		return report, err
	}
	report.File = abs
	root, err := os.OpenRoot(filepath.Dir(abs))
	if err != nil {
		return report, err
	}
	defer root.Close()
	name := filepath.Base(abs)
	before, err := root.Lstat(name)
	if err != nil {
		return report, err
	}
	if !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > 256<<20 {
		return report, errors.New("check requires a regular, nonempty, non-symlink file at most 256 MiB")
	}
	source, err := root.Open(name)
	if err != nil {
		return report, err
	}
	defer source.Close()
	opened, err := source.Stat()
	if err != nil {
		return report, err
	}
	if !os.SameFile(before, opened) {
		return report, errors.New("source changed while opening")
	}
	dir, err := os.MkdirTemp("", "nemalo-check-")
	if err != nil {
		return report, err
	}
	defer func() {
		if cleanupErr := os.Remove(dir); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
			report.Status = "incomplete"
		}
	}()
	// Controlled basename prevents a scanner treating a source name as an option.
	snapshot := filepath.Join(dir, "asset"+safeExtension(abs))
	f, err := os.OpenFile(snapshot, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return report, err
	}
	defer func() {
		closeErr := f.Close()
		removeErr := os.Remove(snapshot)
		if closeErr != nil || removeErr != nil {
			err = errors.Join(err, closeErr, removeErr)
			report.Status = "incomplete"
		}
	}()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(contextReader{ctx, source}, 256<<20+1))
	if err != nil {
		return report, err
	}
	after, err := source.Stat()
	if err != nil {
		return report, err
	}
	current, err := root.Lstat(name)
	if err != nil {
		return report, err
	}
	if n != before.Size() || !os.SameFile(before, current) || !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
		return report, errors.New("source changed during snapshot")
	}
	report.Bytes, report.SHA256 = n, hex.EncodeToString(h.Sum(nil))
	if options.ExpectedBytes != 0 && options.ExpectedBytes != n {
		report.Findings = append(report.Findings, "size differs from supplied expectation")
	}
	if options.ExpectedSHA256 != "" && !strings.EqualFold(options.ExpectedSHA256, report.SHA256) {
		report.Findings = append(report.Findings, "SHA-256 differs from supplied expectation")
	}
	header := make([]byte, min(n, int64(8)))
	if _, err := f.ReadAt(header, 0); err != nil {
		return report, err
	}
	format := detect(header)
	report.DetectedFormat = format
	if format == "zip" {
		format = "epub"
	}
	checks, inspectErr := InspectContext(ctx, contextAt{ctx, f}, n, format)
	report.Checks = checks
	if inspectErr != nil {
		report.Status = "invalid_or_unsupported"
		report.Findings = append(report.Findings, inspectErr.Error())
	} else {
		report.Status = "limited_checks_passed"
		report.DetectedFormat = format
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(abs)), ".")
	if ext != format {
		report.Findings = append(report.Findings, fmt.Sprintf("extension %q differs from assessed format %q", ext, format))
	}
	report.Findings = append(report.Findings, checks.Warnings...)
	if inspectErr == nil && len(report.Findings) > 0 {
		report.Status = "needs_review"
	}
	if ctx.Err() != nil {
		report.Status = "incomplete"
		return report, ctx.Err()
	}
	if options.Scan {
		if av == nil {
			av = scanner.New()
		}
		report.Antivirus = av.Scan(ctx, snapshot)
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return report, err
		}
		verified := sha256.New()
		_, hashErr := io.Copy(verified, io.LimitReader(contextReader{ctx, f}, n+1))
		info, statErr := os.Lstat(snapshot)
		identity, identityErr := f.Stat()
		if hashErr != nil || statErr != nil || identityErr != nil || !os.SameFile(info, identity) || !info.Mode().IsRegular() || info.Size() != n || hex.EncodeToString(verified.Sum(nil)) != report.SHA256 {
			report.Status = "incomplete"
			return report, errors.New("scanner snapshot changed or could not be revalidated")
		}
		if report.Antivirus.Status != "no_detections_reported" && report.Status == "limited_checks_passed" {
			report.Status = "needs_review"
		}
	}
	if report.Status != "limited_checks_passed" {
		return report, errors.New("assessment requires review; see findings and scanner coverage")
	}
	return report, nil
}

func safeExtension(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".epub", ".pdf", ".mp3":
		return ext
	}
	return ".bin"
}

func detect(header []byte) string {
	switch {
	case bytes.HasPrefix(header, []byte("PK\x03\x04")):
		return "zip"
	case bytes.HasPrefix(header, []byte("%PDF-")):
		return "pdf"
	case bytes.HasPrefix(header, []byte("ID3")) || (len(header) > 1 && header[0] == 0xff && header[1]&0xe0 == 0xe0):
		return "mp3"
	case bytes.HasPrefix(header, []byte("Rar!")):
		return "rar"
	case bytes.HasPrefix(header, []byte("MZ")) || bytes.HasPrefix(header, []byte("\x7fELF")):
		return "executable"
	default:
		return "unknown"
	}
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

type contextAt struct {
	ctx context.Context
	r   io.ReaderAt
}

func (r contextAt) ReadAt(p []byte, off int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.ReadAt(p, off)
}
