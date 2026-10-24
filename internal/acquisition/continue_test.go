package acquisition

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/safeio"
)

// flakyProvider drops every response after chunk bytes with an unexpected
// end of body, as a connection reset in the middle of a transfer does, and
// answers ranged requests with exactly the missing bytes.
type flakyProvider struct {
	*rangeProvider
	chunk  int
	failed error
}

func (p *flakyProvider) Download(ctx context.Context, f discovery.OfferedFile) (*http.Response, error) {
	return p.DownloadFrom(ctx, f, 0, "")
}

func (p *flakyProvider) DownloadFrom(ctx context.Context, f discovery.OfferedFile, offset int64, validator string) (*http.Response, error) {
	resp, err := p.rangeProvider.DownloadFrom(ctx, f, offset, validator)
	if err != nil {
		return resp, err
	}
	failure := p.failed
	if failure == nil {
		failure = io.ErrUnexpectedEOF
	}
	resp.Body = io.NopCloser(&dropReader{r: resp.Body, left: p.chunk, err: failure})
	return resp, nil
}

type dropReader struct {
	r    io.Reader
	left int
	err  error
}

func (d *dropReader) Read(b []byte) (int, error) {
	if d.left <= 0 {
		return 0, d.err
	}
	if len(b) > d.left {
		b = b[:d.left]
	}
	n, err := d.r.Read(b)
	d.left -= n
	return n, err
}

func quickContinuations(t *testing.T) {
	saved := continuationPause
	continuationPause = func(int) time.Duration { return 0 }
	t.Cleanup(func() { continuationPause = saved })
}

func TestADroppedTransferContinuesItselfAndVerifiesTheWholeFile(t *testing.T) {
	quickContinuations(t)
	body := "ID3 " + strings.Repeat("resumable audio bytes ", 20)
	p := &flakyProvider{rangeProvider: newRangeProvider(body), chunk: 150}
	r := Request{SourceID: "archive:demo", File: "track.mp3", Output: filepath.Join(t.TempDir(), "packet"), MaxBytes: 1 << 20}
	result, err := Acquire(context.Background(), p, r)
	if err != nil || !result.Complete || result.Continuations != 2 {
		t.Fatal(result.Status, result.Continuations, err)
	}
	if got := mustReadFile(t, filepath.Join(r.Output, "content.mp3")); got != body {
		t.Fatal("continued bytes differ")
	}
	if strings.Join(p.requests, ",") != "full,range,range" {
		t.Fatal(p.requests)
	}
}

func TestContinuationStopsWhenItCannotBeSafe(t *testing.T) {
	quickContinuations(t)
	body := "ID3 " + strings.Repeat("resumable audio bytes ", 20)
	for name, tc := range map[string]struct {
		change   func(p *flakyProvider)
		requests string
	}{
		// A source that ignores the range is a refused resume; the packet stays.
		"whole file instead of a range": {func(p *flakyProvider) { p.ignoreIf = true }, "full,range"},
		// Without a strong validator nothing may be continued.
		"weak validator": {func(p *flakyProvider) { p.etag = `W/"weak"` }, "full"},
		// A failure that is not a dropped connection is not retried.
		"not a transport failure": {func(p *flakyProvider) { p.failed = errors.New("disk full") }, "full"},
		// A source that keeps dropping is continued three times at most.
		"keeps dropping": {func(p *flakyProvider) { p.chunk = 10 }, "full,range,range,range"},
	} {
		t.Run(name, func(t *testing.T) {
			p := &flakyProvider{rangeProvider: newRangeProvider(body), chunk: 150}
			tc.change(p)
			r := Request{SourceID: "archive:demo", File: "track.mp3", Output: filepath.Join(t.TempDir(), "packet"), MaxBytes: 1 << 20}
			result, err := Acquire(context.Background(), p, r)
			if err == nil || result.Complete {
				t.Fatal("completed", result.Status)
			}
			if got := strings.Join(p.requests, ","); got != tc.requests {
				t.Fatalf("requests %s, want %s", got, tc.requests)
			}
			if _, err := os.Stat(filepath.Join(r.Output, "receipt.json")); !os.IsNotExist(err) {
				t.Fatal("receipt written")
			}
			if info, err := os.Stat(filepath.Join(r.Output, "content.mp3")); err != nil || info.Size() < 1 {
				t.Fatal("partial bytes not kept", err)
			}
		})
	}
}

func TestCancellationStopsContinuation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	saved := continuationPause
	pauses := 0
	continuationPause = func(attempt int) time.Duration {
		// Cancel at the first backoff boundary, after the initial response
		// dropped and its partial bytes were retained. A wall-clock sleep
		// could cancel during durable packet setup on a slower filesystem.
		pauses++
		if attempt != 1 {
			t.Fatal("unexpected continuation attempt", attempt)
		}
		cancel()
		return time.Hour
	}
	t.Cleanup(func() { continuationPause = saved })
	body := "ID3 " + strings.Repeat("resumable audio bytes ", 20)
	p := &flakyProvider{rangeProvider: newRangeProvider(body), chunk: 150}
	start := time.Now()
	r := Request{SourceID: "archive:demo", File: "track.mp3", Output: filepath.Join(t.TempDir(), "packet"), MaxBytes: 1 << 20}
	result, err := Acquire(ctx, p, r)
	if err == nil || time.Since(start) > 10*time.Second || !errors.Is(ctx.Err(), context.Canceled) || pauses != 1 || result.Complete || result.Continuations != 0 || result.Transfer.Bytes != 150 {
		t.Fatal("cancellation did not stop the wait", err)
	}
	if fmt.Sprint(p.requests) != "[full]" {
		t.Fatal(p.requests)
	}
	if got := mustReadFile(t, filepath.Join(r.Output, "content.mp3")); got != body[:150] {
		t.Fatal("cancellation changed the retained partial transfer")
	}
	if _, err := os.Stat(filepath.Join(r.Output, "receipt.json")); !os.IsNotExist(err) {
		t.Fatal("cancelled continuation wrote a receipt", err)
	}
}

// droppingRecording drops the first transfer of the second track mid-body.
type droppingRecording struct {
	*rangedRecording
	dropped bool
}

func (p *droppingRecording) Download(ctx context.Context, f discovery.OfferedFile) (*http.Response, error) {
	return p.DownloadFrom(ctx, f, 0, "")
}

func (p *droppingRecording) DownloadFrom(ctx context.Context, f discovery.OfferedFile, offset int64, validator string) (*http.Response, error) {
	resp, err := p.rangedRecording.DownloadFrom(ctx, f, offset, validator)
	if err == nil && offset == 0 && f.Name == "part_02_64kb.mp3" && !p.dropped {
		p.dropped = true
		resp.Body = io.NopCloser(&dropReader{r: resp.Body, left: 7, err: io.ErrUnexpectedEOF})
	}
	return resp, err
}

func TestARecordingTrackThatDropsContinuesInPlace(t *testing.T) {
	quickContinuations(t)
	p := &droppingRecording{rangedRecording: &rangedRecording{recordingProvider: recordingFixture()}}
	result, err := AcquireRecording(context.Background(), p, recordingRequest(t))
	if err != nil || !result.Complete {
		t.Fatal(result.Status, err)
	}
	if fmt.Sprint(p.ranges) != "[7]" {
		t.Fatal("track was not continued from its retained bytes", p.ranges)
	}
}

// A download that would not fit in the free space is refused before any
// folder is made or byte requested.
func TestAcquireRefusesADownloadThatWouldNotFit(t *testing.T) {
	saved := safeio.FreeSpace
	safeio.FreeSpace = func(string, *os.Root) (int64, error) { return 1 << 20, nil }
	t.Cleanup(func() { safeio.FreeSpace = saved })
	p := &flakyProvider{rangeProvider: newRangeProvider("ID3 small"), chunk: 1 << 20}
	r := Request{SourceID: "archive:demo", File: "track.mp3", Output: filepath.Join(t.TempDir(), "new", "packet"), MaxBytes: 1 << 20}
	_, err := Acquire(context.Background(), p, r)
	if !errors.Is(err, safeio.ErrNoSpace) || !strings.Contains(err.Error(), "are free") {
		t.Fatal(err)
	}
	if _, statErr := os.Lstat(filepath.Dir(r.Output)); !os.IsNotExist(statErr) || len(p.requests) != 0 {
		t.Fatal("a refused download made folders or requests", p.requests)
	}
}
