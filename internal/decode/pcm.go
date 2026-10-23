package decode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrPCMBudget = errors.New("decoded audio work allowance exceeded")
var ErrPCMRange = errors.New("decoded sample selection is outside the available range")
var ErrPCMUnavailable = errors.New("installed audio decoder is unavailable")
var ErrPCMDecode = errors.New("complete decoded output could not be established")

const maxPCMBytes int64 = 8 << 30
const maxPCMSelection = 64 << 10

// PCMRequest selects actual mono s16le output samples, never encoded packet or
// edited presentation positions. UnitsOnly still measures the complete stream.
type PCMRequest struct {
	FirstSample int64
	MaxBytes    int
	UnitsOnly   bool
}

// PCMRange is one full sequential measurement and its bounded retained slice.
// A failure retains diagnostics only, never a usable partial payload or digest.
type PCMRange struct {
	Measurement             Result
	FullSHA256              string
	FirstSample, NextSample int64
	EndOfStream             bool
	Data                    []byte
}

// ReadPCM decodes an owned verified MP4 snapshot in full with installed tools.
// It keeps the same measurement policy as Audio, with additional capture,
// diagnostic/process/output bounds and no input seeking. The caller verifies
// the snapshot again and checks source identity before publishing this result.
func ReadPCM(ctx context.Context, file, format string, size int64, req PCMRequest) (PCMRange, error) {
	return readPCM(ctx, file, format, size, req, maxPCMBytes, maxOutput)
}

func readPCM(ctx context.Context, file, format string, size int64, req PCMRequest, outputLimit int64, diagnosticLimit int64) (PCMRange, error) {
	var out PCMRange
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if (format != "m4b" && format != "m4a") || size <= 0 || size > 2<<30 || req.FirstSample < 0 || req.FirstSample >= maxPCMBytes/2 || (!req.UnitsOnly && (req.MaxBytes < 2 || req.MaxBytes > maxPCMSelection)) || outputLimit < 2 || outputLimit > maxPCMBytes || diagnosticLimit < 1 || diagnosticLimit > maxOutput {
		return out, ErrPCMRange
	}
	ctx, timeout := context.WithTimeout(ctx, 5*time.Minute)
	defer timeout()
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	c := &pcmCapture{first: req.FirstSample * 2, limit: outputLimit, digest: sha256.New(), work: &pcmWork{cancel: cancel, limit: diagnosticLimit}}
	c.end = c.first + int64(req.MaxBytes&^1)
	if req.UnitsOnly {
		c.end = c.first
	}
	out.Measurement = measureAudio(ctx, file, format, size, c)
	if err := context.Cause(ctx); err != nil {
		return out, err
	}
	if out.Measurement.Status == "unavailable" {
		return out, ErrPCMUnavailable
	}
	if out.Measurement.Status != "passed" {
		return out, ErrPCMDecode
	}
	out.FullSHA256 = hex.EncodeToString(c.digest.Sum(nil))
	if req.UnitsOnly {
		return out, nil
	}
	if req.FirstSample >= out.Measurement.DecodedSamples {
		out.FullSHA256 = ""
		return out, ErrPCMRange
	}
	out.FirstSample = req.FirstSample
	out.NextSample = req.FirstSample + int64(len(c.data))/2
	out.EndOfStream = out.NextSample == out.Measurement.DecodedSamples
	out.Data = c.data
	return out, nil
}

type pcmCapture struct {
	first, end, n, limit int64
	data                 []byte
	digest               hash.Hash
	work                 *pcmWork
	processes            int
}

func (c *pcmCapture) Write(p []byte) (int, error) {
	if err := c.work.cause(); err != nil {
		return 0, err
	}
	before := c.n
	c.n += int64(len(p)) // Count observed failing chunks too; retain no over-cap bytes.
	if c.n > c.limit {
		c.work.stop(ErrPCMBudget)
		return 0, ErrPCMBudget
	}
	start, end := max(c.first-before, 0), min(c.end-before, int64(len(p)))
	if end > start {
		c.data = append(c.data, p[start:end]...)
	}
	_, _ = c.digest.Write(p) // SHA-256's Write cannot fail.
	return len(p), nil
}

type pcmWork struct {
	mu              sync.Mutex
	cancel          context.CancelCauseFunc
	observed, limit int64
	stopped         error
}

func (w *pcmWork) cause() error { w.mu.Lock(); defer w.mu.Unlock(); return w.stopped }
func (w *pcmWork) stop(err error) {
	if err == nil {
		return
	}
	w.mu.Lock()
	if w.stopped == nil {
		w.stopped = err
	}
	w.mu.Unlock()
	w.cancel(err)
}
func (w *pcmWork) charge(n int) error {
	w.mu.Lock()
	w.observed += int64(n)
	over := w.observed > w.limit
	err := w.stopped
	w.mu.Unlock()
	if over {
		w.stop(ErrPCMBudget)
		return ErrPCMBudget
	}
	return err
}

type pcmDiagnostic struct {
	work   *pcmWork
	buffer bytes.Buffer
}

func (d *pcmDiagnostic) Write(p []byte) (int, error) {
	if err := d.work.charge(len(p)); err != nil {
		return 0, err
	}
	return d.buffer.Write(p)
}

// run shares one lifetime diagnostic allowance across version, probe and decode.
// PCM stdout has its own streamed allowance. A refusal cancels the real child;
// bounded storage alone must not allow unlimited discarded diagnostic work.
func (c *pcmCapture) run(ctx context.Context, exe string, out io.Writer, args ...string) (string, int, error) {
	c.processes++
	if c.processes > 3 {
		c.work.stop(ErrPCMBudget)
		return "", -1, ErrPCMBudget
	}
	if err := ctx.Err(); err != nil {
		c.work.stop(err)
		return "", -1, err
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.WaitDelay = 2 * time.Second
	stdout, stderr := &pcmDiagnostic{work: c.work}, &pcmDiagnostic{work: c.work}
	metadata := out == io.Discard
	if metadata {
		cmd.Stdout = stdout
	} else {
		cmd.Stdout = out
	}
	cmd.Stderr = stderr
	err := cmd.Run()
	code := -1
	var exit *exec.ExitError
	if err == nil {
		code = 0
	} else if errors.As(err, &exit) {
		code = exit.ExitCode()
	}
	if ctx.Err() != nil {
		c.work.stop(ctx.Err())
	}
	if failure := c.work.cause(); failure != nil {
		return "", code, failure
	}
	if metadata {
		if stderr.buffer.Len() != 0 && err == nil {
			err = ErrPCMDecode
		}
		return stdout.buffer.String(), code, err
	}
	return stderr.buffer.String(), code, err
}

func validPCMProbe(s string) bool {
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		n, err := strconv.Atoi(value)
		if !ok || err != nil || strconv.Itoa(n) != value || seen[key] {
			return false
		}
		switch key {
		case "sample_rate":
			if n < 1 || n > 1_000_000 {
				return false
			}
		case "channels":
			if n < 1 || n > 64 {
				return false
			}
		default:
			return false
		}
		seen[key] = true
	}
	return len(seen) == 2
}
