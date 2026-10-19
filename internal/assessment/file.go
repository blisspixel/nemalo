package assessment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blisspixel/nemalo/internal/conformance"
	"github.com/blisspixel/nemalo/internal/safeio"
	"github.com/blisspixel/nemalo/internal/scanner"
)

type Options struct {
	Scan bool
	// EPUBCheck runs the installed EPUBCheck on an EPUB snapshot. Errors it
	// reports, and a requested check that did not complete, are findings.
	EPUBCheck      bool
	ExpectedBytes  int64
	ExpectedSHA256 string
}

type Scanner interface {
	Scan(context.Context, string) scanner.Result
}

type Report struct {
	File           string              `json:"file"`
	CheckedAt      time.Time           `json:"checked_at"`
	Bytes          int64               `json:"bytes"`
	SHA256         string              `json:"sha256"`
	DetectedFormat string              `json:"detected_format"`
	Status         string              `json:"status"`
	Checks         Checks              `json:"checks"`
	Antivirus      scanner.Result      `json:"antivirus"`
	Conformance    *conformance.Result `json:"conformance,omitempty"`
	Findings       []string            `json:"findings"`
	Limitations    []string            `json:"limitations"`
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
	report = Report{File: file, CheckedAt: time.Now().UTC(), Status: "incomplete", Findings: []string{}, Antivirus: scanner.Result{Status: "not_scanned", ExitCode: -1}, Limitations: []string{"Limited checks do not establish safety, semantic completeness, or edition quality.", "PDF page trees and active content are examined without reading page content; MP3 frames are indexed but audio is not decoded.", "EPUB text counts are tokenizer measurements, not rendered pages or proof of complete text."}}
	if err := options.Validate(); err != nil {
		return report, err
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		return report, err
	}
	report.File = abs
	root, err := safeio.OpenResolvedRoot(filepath.Dir(abs))
	if err != nil {
		return report, err
	}
	defer root.Close()
	report, err = CheckInRoot(ctx, root, filepath.Base(abs), options, av)
	report.File = abs
	return report, err
}

// CheckInRoot shares the file assessment pipeline with explicitly scoped callers.
// The supplied name cannot escape root, even if a parent is replaced by a link.
func CheckInRoot(ctx context.Context, root *os.Root, name string, options Options, av Scanner) (report Report, err error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	p, err := prepare(ctx, root, name, options)
	if p == nil {
		return incompleteReport(root, name), err
	}
	defer func() {
		if releaseErr := p.release(); releaseErr != nil {
			err = errors.Join(err, releaseErr)
			report.Status = "incomplete"
		}
	}()
	if err != nil {
		return p.report, err
	}
	if options.Scan {
		if av == nil {
			av = scanner.New()
		}
		if err := p.bindScan(ctx, av.Scan(ctx, p.snapshot)); err != nil {
			return p.report, err
		}
	}
	if options.EPUBCheck && p.report.DetectedFormat == "epub" {
		if err := p.bindConformance(ctx, conformance.CheckEPUB(ctx, p.snapshot)); err != nil {
			return p.report, err
		}
	}
	return p.finish()
}

func incompleteReport(root *os.Root, name string) Report {
	return Report{File: filepath.Join(root.Name(), filepath.FromSlash(name)), CheckedAt: time.Now().UTC(), Status: "incomplete", Findings: []string{}, Antivirus: scanner.Result{Status: "not_scanned", ExitCode: -1}, Limitations: []string{"Limited checks do not establish safety, semantic completeness, or edition quality.", "PDF page trees and active content are examined without reading page content; MP3 frames are indexed but audio is not decoded.", "EPUB text counts are tokenizer measurements, not rendered pages or proof of complete text."}}
}

// prepared is one private snapshot whose limited checks are complete and whose
// optional scan has not yet been bound. release removes the snapshot.
type prepared struct {
	report   Report
	dir      string
	snapshot string
	f        *os.File
	n        int64
}

// prepare snapshots and inspects one file. A nil result means nothing was
// created. A non-nil result must be released, even when an error is returned.
func prepare(ctx context.Context, root *os.Root, name string, options Options) (*prepared, error) {
	if !fs.ValidPath(name) || name == "." || strings.ContainsAny(name, "\\:\x00") {
		return nil, errors.New("invalid root-relative assessment path")
	}
	report := incompleteReport(root, name)
	if err := options.Validate(); err != nil {
		return nil, err
	}
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > 256<<20 {
		return nil, errors.New("check requires a regular, nonempty, non-symlink file at most 256 MiB")
	}
	source, err := safeio.OpenRegular(root, name, before, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	opened, err := source.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(before, opened) {
		return nil, errors.New("source changed while opening")
	}
	dir, err := os.MkdirTemp("", "nemalo-check-")
	if err != nil {
		return nil, err
	}
	// Controlled basename prevents a scanner treating a source name as an option.
	p := &prepared{report: report, dir: dir, snapshot: filepath.Join(dir, "asset"+safeExtension(name))}
	p.f, err = os.OpenFile(p.snapshot, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return p, err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(p.f, h), io.LimitReader(contextReader{ctx, source}, 256<<20+1))
	if err != nil {
		return p, err
	}
	after, err := source.Stat()
	if err != nil {
		return p, err
	}
	current, err := root.Lstat(name)
	if err != nil {
		return p, err
	}
	if n != before.Size() || !os.SameFile(before, current) || !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
		return p, errors.New("source changed during snapshot")
	}
	p.n = n
	p.report.Bytes, p.report.SHA256 = n, hex.EncodeToString(h.Sum(nil))
	if options.ExpectedBytes != 0 && options.ExpectedBytes != n {
		p.report.Findings = append(p.report.Findings, "size differs from supplied expectation")
	}
	if options.ExpectedSHA256 != "" && !strings.EqualFold(options.ExpectedSHA256, p.report.SHA256) {
		p.report.Findings = append(p.report.Findings, "SHA-256 differs from supplied expectation")
	}
	header := make([]byte, min(n, int64(8)))
	if _, err := p.f.ReadAt(header, 0); err != nil {
		return p, err
	}
	format := detect(header)
	p.report.DetectedFormat = format
	if format == "zip" {
		format = "epub"
	}
	checks, inspectErr := InspectContext(ctx, contextAt{ctx, p.f}, n, format)
	p.report.Checks = checks
	if inspectErr != nil {
		p.report.Status = "invalid_or_unsupported"
		p.report.Findings = append(p.report.Findings, inspectErr.Error())
	} else {
		p.report.Status = "limited_checks_passed"
		p.report.DetectedFormat = format
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	if ext != format {
		p.report.Findings = append(p.report.Findings, fmt.Sprintf("extension %q differs from assessed format %q", ext, format))
	}
	p.report.Findings = append(p.report.Findings, checks.Warnings...)
	if inspectErr == nil && len(p.report.Findings) > 0 {
		p.report.Status = "needs_review"
	}
	if ctx.Err() != nil {
		p.report.Status = "incomplete"
		return p, ctx.Err()
	}
	return p, nil
}

// bindConformance records an EPUBCheck run on the snapshot, revalidated as a
// scan is. Reported errors and a run that did not complete are findings.
func (p *prepared) bindConformance(ctx context.Context, result conformance.Result) error {
	p.report.Conformance = &result
	if err := p.revalidate(ctx); err != nil {
		p.report.Status = "incomplete"
		return errors.New("conformance snapshot changed or could not be revalidated")
	}
	switch result.Status {
	case "passed":
		return nil
	case "errors_reported":
		p.report.Findings = append(p.report.Findings, "EPUBCheck reported errors")
	default:
		p.report.Findings = append(p.report.Findings, "EPUBCheck was requested but did not complete: "+result.Status)
	}
	if p.report.Status == "limited_checks_passed" {
		p.report.Status = "needs_review"
	}
	return nil
}

// revalidate proves the snapshot still holds the assessed bytes.
func (p *prepared) revalidate(ctx context.Context) error {
	if _, err := p.f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	verified := sha256.New()
	_, hashErr := io.Copy(verified, io.LimitReader(contextReader{ctx, p.f}, p.n+1))
	info, statErr := os.Lstat(p.snapshot)
	identity, identityErr := p.f.Stat()
	if hashErr != nil || statErr != nil || identityErr != nil || !os.SameFile(info, identity) || !info.Mode().IsRegular() || info.Size() != p.n || hex.EncodeToString(verified.Sum(nil)) != p.report.SHA256 {
		return errors.New("snapshot changed")
	}
	return nil
}

// bindScan records a scan of the snapshot and proves the scanned bytes were
// the assessed bytes: the snapshot identity, size, and SHA-256 must be unchanged.
func (p *prepared) bindScan(ctx context.Context, result scanner.Result) error {
	p.report.Antivirus = result
	if _, err := p.f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	verified := sha256.New()
	_, hashErr := io.Copy(verified, io.LimitReader(contextReader{ctx, p.f}, p.n+1))
	info, statErr := os.Lstat(p.snapshot)
	identity, identityErr := p.f.Stat()
	if hashErr != nil || statErr != nil || identityErr != nil || !os.SameFile(info, identity) || !info.Mode().IsRegular() || info.Size() != p.n || hex.EncodeToString(verified.Sum(nil)) != p.report.SHA256 {
		p.report.Status = "incomplete"
		return errors.New("scanner snapshot changed or could not be revalidated")
	}
	if p.report.Antivirus.Status != "no_detections_reported" && p.report.Status == "limited_checks_passed" {
		p.report.Status = "needs_review"
	}
	return nil
}

func (p *prepared) finish() (Report, error) {
	if p.report.Status != "limited_checks_passed" {
		return p.report, errors.New("assessment requires review; see findings and scanner coverage")
	}
	return p.report, nil
}

// release closes and removes the snapshot. A cleanup failure is an error: the
// private copy of untrusted bytes must not be silently left behind.
func (p *prepared) release() error {
	var closeErr, removeErr error
	if p.f != nil {
		closeErr = p.f.Close()
		removeErr = os.Remove(p.snapshot)
	}
	return errors.Join(closeErr, removeErr, os.Remove(p.dir))
}

// BatchScanner scans several snapshots together; results align with files.
type BatchScanner interface {
	ScanMany(context.Context, []string) []scanner.Result
}

// Batch bounds keep private snapshot disk use and one scanner invocation finite.
const (
	maxBatchFiles = 32
	maxBatchBytes = 1 << 30
)

// CheckManyInRoot assesses each name exactly as CheckInRoot does. With Scan,
// up to 32 snapshots and 1 GiB are scanned per scanner invocation when the
// scanner supports batches, so ClamAV loads its signatures once per batch.
// Each scan is still bound to its own snapshot. Reports and errors align with
// names; one failure does not stop the others.
func CheckManyInRoot(ctx context.Context, root *os.Root, names []string, options []Options, av Scanner) ([]Report, []error) {
	reports, errs := make([]Report, len(names)), make([]error, len(names))
	if len(options) != len(names) {
		for i := range errs {
			reports[i], errs[i] = incompleteReport(root, names[i]), errors.New("one assessment option set is required per file")
		}
		return reports, errs
	}
	if av == nil {
		av = scanner.New()
	}
	batch, indexes, bytes := []*prepared{}, []int{}, int64(0)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		files := make([]string, len(batch))
		for i, p := range batch {
			files[i] = p.snapshot
		}
		var results []scanner.Result
		if many, ok := av.(BatchScanner); ok {
			results = many.ScanMany(ctx, files)
		}
		for i, p := range batch {
			result := scanner.Result{Status: "incomplete", ExitCode: -1}
			if len(results) == len(batch) {
				result = results[i]
			} else if results == nil {
				result = av.Scan(ctx, p.snapshot)
			}
			k := indexes[i]
			if err := p.bindScan(ctx, result); err != nil {
				reports[k], errs[k] = p.report, err
			} else {
				reports[k], errs[k] = p.finish()
			}
			if err := p.release(); err != nil {
				reports[k].Status, errs[k] = "incomplete", errors.Join(errs[k], err)
			}
		}
		batch, indexes, bytes = batch[:0], indexes[:0], 0
	}
	for i, name := range names {
		pctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		p, err := prepare(pctx, root, name, options[i])
		cancel()
		switch {
		case p == nil:
			reports[i], errs[i] = incompleteReport(root, name), err
			continue
		case err != nil || !options[i].Scan:
			reports[i], errs[i] = p.report, err
			if err == nil {
				reports[i], errs[i] = p.finish()
			}
			if releaseErr := p.release(); releaseErr != nil {
				reports[i].Status, errs[i] = "incomplete", errors.Join(errs[i], releaseErr)
			}
			continue
		}
		if len(batch) == maxBatchFiles || bytes+p.n > maxBatchBytes {
			flush()
		}
		batch, indexes, bytes = append(batch, p), append(indexes, i), bytes+p.n
	}
	flush()
	return reports, errs
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
