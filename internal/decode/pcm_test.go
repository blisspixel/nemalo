package decode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func pcmTestBytes() []byte {
	b := make([]byte, 3000)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}
func pcmDigest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func fakePCM(t *testing.T, mode string) string {
	t.Helper()
	fake(t, mode, "mov")
	t.Setenv("NEMALO_FAKE_PCM", "1")
	log := filepath.Join(t.TempDir(), "calls")
	t.Setenv("NEMALO_FAKE_PCM_CALLS", log)
	return log
}

func TestReadPCMSelectsExactFullSequentialOutputAndDigest(t *testing.T) {
	calls := fakePCM(t, "pcm")
	data := pcmTestBytes()
	for _, req := range []PCMRequest{{FirstSample: 0, MaxBytes: 18}, {FirstSample: 1, MaxBytes: 3}, {FirstSample: 717, MaxBytes: 400}, {FirstSample: 1498, MaxBytes: 16}, {UnitsOnly: true}} {
		out, err := ReadPCM(context.Background(), "/private/asset.m4a", "m4a", 10, req)
		if err != nil || out.Measurement.Status != "passed" || out.Measurement.Version != "9.9-test" || out.Measurement.SampleRate != 1000 || out.Measurement.Channels != 2 || out.Measurement.DecodedSamples != 1500 || out.FullSHA256 != pcmDigest(data) {
			t.Fatal(out, err)
		}
		if req.UnitsOnly {
			if out.Data != nil {
				t.Fatal("units retained a waveform")
			}
			continue
		}
		end := min(req.FirstSample+int64(req.MaxBytes/2), 1500)
		if !bytes.Equal(out.Data, data[req.FirstSample*2:end*2]) || out.FirstSample != req.FirstSample || out.NextSample != end || out.EndOfStream != (end == 1500) {
			t.Fatal("selected bytes or sample bounds differ", out)
		}
	}
	log, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.Split(strings.TrimSpace(string(log)), "\n")) != 15 || strings.Contains(string(log), "-ss ") {
		t.Fatal("process count or unexpected seeking", string(log))
	}
}

func TestReadPCMFailureNeverDeliversPartialBytesOrDigest(t *testing.T) {
	for _, mode := range []string{"pcm_late_error", "errors", "odd", "fail", "nostream"} {
		t.Run(mode, func(t *testing.T) {
			fakePCM(t, mode)
			out, err := ReadPCM(context.Background(), "/private/asset.m4b", "m4b", 10, PCMRequest{MaxBytes: 20})
			if !errors.Is(err, ErrPCMDecode) || out.Data != nil || out.FullSHA256 != "" || out.NextSample != 0 || out.EndOfStream {
				t.Fatal("failed decode exposed a partial range", out, err)
			}
		})
	}
	fakePCM(t, "pcm")
	out, err := ReadPCM(context.Background(), "/private/asset.m4a", "m4a", 10, PCMRequest{FirstSample: 1500, MaxBytes: 20})
	if !errors.Is(err, ErrPCMRange) || out.Data != nil || out.FullSHA256 != "" {
		t.Fatal("past-end request became a range", out, err)
	}
}

func TestReadPCMWorkLimitsStopTheChildAndDiscardEarlierSelection(t *testing.T) {
	for _, tc := range []struct {
		name                string
		output, diagnostics int64
	}{{"output", 128, maxOutput}, {"diagnostics", maxPCMBytes, 8}} {
		t.Run(tc.name, func(t *testing.T) {
			fakePCM(t, "pcm")
			out, err := readPCM(context.Background(), "/private/asset.m4a", "m4a", 10, PCMRequest{MaxBytes: 20}, tc.output, tc.diagnostics)
			if !errors.Is(err, ErrPCMBudget) || out.Data != nil || out.FullSHA256 != "" {
				t.Fatal(out, err)
			}
		})
	}
}

func TestReadPCMUnavailableCanceledAndInvalidRequests(t *testing.T) {
	saved := LookPath
	t.Cleanup(func() { LookPath = saved })
	LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	out, err := ReadPCM(context.Background(), "/private/asset.m4a", "m4a", 10, PCMRequest{MaxBytes: 20})
	if !errors.Is(err, ErrPCMUnavailable) || out.Measurement.Status != "unavailable" || out.Data != nil {
		t.Fatal(out, err)
	}
	for _, req := range []PCMRequest{{FirstSample: -1, MaxBytes: 20}, {FirstSample: maxPCMBytes / 2, MaxBytes: 20}, {MaxBytes: 1}, {MaxBytes: maxPCMSelection + 1}} {
		if out, err := ReadPCM(context.Background(), "/private/asset.m4a", "m4a", 10, req); !errors.Is(err, ErrPCMRange) || out.Data != nil {
			t.Fatal(req, out, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadPCM(ctx, "/private/asset.m4a", "m4a", 10, PCMRequest{MaxBytes: 20}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	fakePCM(t, "hang")
	ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	out, err = ReadPCM(ctx, "/private/asset.m4a", "m4a", 10, PCMRequest{MaxBytes: 20})
	if !errors.Is(err, context.DeadlineExceeded) || out.Data != nil || out.FullSHA256 != "" {
		t.Fatal(out, err)
	}
	if _, err := ReadPCM(context.Background(), "/private/asset.mp3", "mp3", 10, PCMRequest{MaxBytes: 20}); !errors.Is(err, ErrPCMRange) {
		t.Fatal("PCM format boundary", err)
	}
}

func TestPCMCaptureHandlesOddChunksAndChargesFailedWork(t *testing.T) {
	data := pcmTestBytes()
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	c := &pcmCapture{first: 14, end: 310, limit: 3000, digest: sha256.New(), work: &pcmWork{cancel: cancel, limit: maxOutput}}
	for off := 0; off < len(data); {
		end := min(off+101, len(data))
		n, err := c.Write(data[off:end])
		if err != nil {
			t.Fatal(err)
		}
		off += n
	}
	if c.n != 3000 || !bytes.Equal(c.data, data[14:310]) || hex.EncodeToString(c.digest.Sum(nil)) != pcmDigest(data) {
		t.Fatal("pipe chunk boundaries changed selection")
	}
	if _, err := c.Write([]byte{1}); !errors.Is(err, ErrPCMBudget) || c.n != 3001 || !errors.Is(context.Cause(ctx), ErrPCMBudget) {
		t.Fatal("observed failed output unaccounted", c.n, err)
	}
}

func TestPCMProbeRejectsAmbiguousOrUnboundedFields(t *testing.T) {
	if !validPCMProbe("sample_rate=44100\r\nchannels=2\r\n") {
		t.Fatal("canonical probe refused")
	}
	for _, s := range []string{"", "sample_rate=44100", "sample_rate=44100\nchannels=0", "sample_rate=1000001\nchannels=2", "sample_rate=44100\nchannels=65", "sample_rate=044100\nchannels=2", "sample_rate=44100\nchannels=2\nchannels=2", "sample_rate=44100\nchannels=2\nurl=http://example.invalid"} {
		if validPCMProbe(s) {
			t.Fatal("unsupported probe accepted", s)
		}
	}
}
