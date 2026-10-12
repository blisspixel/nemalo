package acquisition

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/scanner"
)

// rangeProvider serves one file, can cut a transfer short, and answers ranged
// requests the way Archive's data nodes do.
type rangeProvider struct {
	body       string
	etag       string
	cutAt      int    // a full transfer fails after this many bytes; 0 means never
	ignoreIf   bool   // answer a ranged request with the whole file
	badRange   string // override Content-Range on a ranged reply
	e          discovery.Evaluation
	requests   []string
	rangeAsked int64
}

func newRangeProvider(body string) *rangeProvider {
	n := int64(len(body))
	md := md5.Sum([]byte(body))
	return &rangeProvider{body: body, etag: `"4549eb81-5ecde2"`, e: discovery.Evaluation{Complete: true, ID: "archive:demo", Source: "archive", Access: "no_item_restriction_declared", Title: []string{"T"},
		Files: []discovery.OfferedFile{{Name: "track.mp3", Bytes: &n, MD5: hex.EncodeToString(md[:]), Access: "public_file_candidate", DownloadURL: "https://archive.org/download/demo/track.mp3"}}}}
}

func (p *rangeProvider) Evaluate(context.Context, string) (discovery.Evaluation, error) {
	return p.e, nil
}

type cutReader struct {
	r   io.Reader
	n   int
	cut int
}

func (c *cutReader) Read(b []byte) (int, error) {
	if c.cut > 0 && c.n >= c.cut {
		return 0, errors.New("connection reset")
	}
	if c.cut > 0 && c.n+len(b) > c.cut {
		b = b[:c.cut-c.n]
	}
	n, err := c.r.Read(b)
	c.n += n
	return n, err
}

func (p *rangeProvider) Download(ctx context.Context, f discovery.OfferedFile) (*http.Response, error) {
	return p.DownloadFrom(ctx, f, 0, "")
}

func (p *rangeProvider) DownloadFrom(_ context.Context, _ discovery.OfferedFile, offset int64, validator string) (*http.Response, error) {
	h := http.Header{"Etag": []string{p.etag}}
	if offset == 0 {
		p.requests = append(p.requests, "full")
		return &http.Response{StatusCode: 200, ContentLength: int64(len(p.body)), Header: h, Body: io.NopCloser(&cutReader{r: strings.NewReader(p.body), cut: p.cutAt})}, nil
	}
	p.requests = append(p.requests, "range")
	p.rangeAsked = offset
	if p.ignoreIf || validator != p.etag {
		return &http.Response{StatusCode: 200, ContentLength: int64(len(p.body)), Header: h, Body: io.NopCloser(strings.NewReader(p.body))}, nil
	}
	h.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, len(p.body)-1, len(p.body)))
	if p.badRange != "" {
		h.Set("Content-Range", p.badRange)
	}
	rest := p.body[offset:]
	return &http.Response{StatusCode: 206, ContentLength: int64(len(rest)), Header: h, Body: io.NopCloser(strings.NewReader(rest))}, nil
}

func interrupted(t *testing.T, p *rangeProvider) Request {
	t.Helper()
	p.cutAt = 40
	r := Request{SourceID: "archive:demo", File: "track.mp3", Output: filepath.Join(t.TempDir(), "packet"), MaxBytes: 1 << 20}
	if _, err := Acquire(context.Background(), p, r); err == nil {
		t.Fatal("cut transfer succeeded")
	}
	p.cutAt = 0
	if info, err := os.Stat(filepath.Join(r.Output, "content.mp3")); err != nil || info.Size() != 40 {
		t.Fatal("partial not retained", err)
	}
	r.Resume = true
	return r
}

func TestResumeContinuesTheSameBytesAndVerifiesTheWholeFile(t *testing.T) {
	body := "ID3 " + strings.Repeat("resumable audio bytes ", 20)
	p := newRangeProvider(body)
	r := interrupted(t, p)
	r.Quarantine = filepath.Join(t.TempDir(), "q")
	r.Scanner = scanFunc(func(context.Context, string) scanner.Result { return scanner.Result{Status: "no_detections_reported"} })
	result, err := Acquire(context.Background(), p, r)
	if err != nil || !result.Complete || result.ResumedFrom != 40 || p.rangeAsked != 40 || result.Antivirus != "no_detections_reported" {
		t.Fatal(result, err)
	}
	if got := mustReadFile(t, filepath.Join(r.Output, "content.mp3")); got != body {
		t.Fatal("resumed bytes differ")
	}
	if receipt := readReceipt(t, r.Output); receipt.ResumedFrom != 40 || receipt.Status != "untrusted_intake" {
		t.Fatal(receipt)
	}
	if _, err := Acquire(context.Background(), p, r); err == nil || !strings.Contains(err.Error(), "receipt") {
		t.Fatal("completed packet resumed again", err)
	}
}

func TestResumeRefusesAnythingButTheMissingRangeOfTheSameFile(t *testing.T) {
	body := "ID3 " + strings.Repeat("resumable audio bytes ", 20)
	for name, change := range map[string]func(p *rangeProvider){
		"whole file":     func(p *rangeProvider) { p.ignoreIf = true },
		"changed etag":   func(p *rangeProvider) { p.etag = `"other"` },
		"wrong range":    func(p *rangeProvider) { p.badRange = "bytes 0-9/999" },
		"changed sizes":  func(p *rangeProvider) { n := int64(5); p.e.Files[0].Bytes = &n },
		"changed digest": func(p *rangeProvider) { p.e.Files[0].MD5 = strings.Repeat("0", 32) },
	} {
		t.Run(name, func(t *testing.T) {
			p := newRangeProvider(body)
			r := interrupted(t, p)
			change(p)
			if _, err := Acquire(context.Background(), p, r); err == nil {
				t.Fatal("accepted")
			}
			if info, err := os.Stat(filepath.Join(r.Output, "content.mp3")); err != nil || info.Size() != 40 {
				t.Fatal("partial content changed", err)
			}
			if _, err := os.Stat(filepath.Join(r.Output, "receipt.json")); !os.IsNotExist(err) {
				t.Fatal("receipt written")
			}
		})
	}
}

func TestResumeNeedsAStrongValidatorAndAnExistingPacket(t *testing.T) {
	body := "ID3 " + strings.Repeat("resumable audio bytes ", 20)
	p := newRangeProvider(body)
	p.etag = `W/"weak"`
	r := interrupted(t, p)
	if _, err := Acquire(context.Background(), p, r); err == nil || !strings.Contains(err.Error(), "strong validator") {
		t.Fatal("weak validator accepted", err)
	}
	missing := Request{SourceID: "archive:demo", File: "track.mp3", Output: filepath.Join(t.TempDir(), "none"), MaxBytes: 1 << 20, Resume: true}
	if _, err := Acquire(context.Background(), newRangeProvider(body), missing); err == nil {
		t.Fatal("missing packet resumed")
	}
	for _, v := range []string{`"abc"`, `"4549eb81-5ecde2"`} {
		if !discovery.StrongETag(v) {
			t.Fatal("strong ETag refused", v)
		}
	}
	for _, v := range []string{``, `W/"abc"`, `abc`, `"a"b"`, "\"a\nb\""} {
		if discovery.StrongETag(v) {
			t.Fatal("weak or malformed ETag accepted", v)
		}
	}
}

func mustReadFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// rangedRecording serves a recording whose track 2 is cut short once, and
// resumes ranges with a strong validator.
type rangedRecording struct {
	*recordingProvider
	cut    bool
	ranges []int64
}

func (p *rangedRecording) Download(ctx context.Context, f discovery.OfferedFile) (*http.Response, error) {
	return p.DownloadFrom(ctx, f, 0, "")
}

func (p *rangedRecording) DownloadFrom(_ context.Context, f discovery.OfferedFile, offset int64, validator string) (*http.Response, error) {
	p.downloads = append(p.downloads, f.Name)
	body := p.bodies[f.Name]
	h := http.Header{"Etag": []string{`"v-` + f.Name + `"`}}
	if offset == 0 {
		var r io.Reader = strings.NewReader(body)
		if p.cut && f.Name == "part_02_64kb.mp3" {
			r = &cutReader{r: r, cut: 7}
		}
		return &http.Response{StatusCode: 200, ContentLength: int64(len(body)), Header: h, Body: io.NopCloser(r)}, nil
	}
	p.ranges = append(p.ranges, offset)
	if validator != h.Get("Etag") {
		return &http.Response{StatusCode: 200, ContentLength: int64(len(body)), Header: h, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	h.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, len(body)-1, len(body)))
	return &http.Response{StatusCode: 206, ContentLength: int64(len(body)) - offset, Header: h, Body: io.NopCloser(strings.NewReader(body[offset:]))}, nil
}

func TestRecordingResumeContinuesAnInterruptedTrackInPlace(t *testing.T) {
	p := &rangedRecording{recordingProvider: recordingFixture(), cut: true}
	r := recordingRequest(t)
	if _, err := AcquireRecording(context.Background(), p, r); err == nil {
		t.Fatal("cut track succeeded")
	}
	p.cut, p.downloads, r.Resume = false, nil, true
	done, err := AcquireRecording(context.Background(), p, r)
	if err != nil || !done.Complete {
		t.Fatal(done, err)
	}
	if done.Tracks[1].Packet != "0002" || len(p.ranges) != 1 || p.ranges[0] != 7 {
		t.Fatal("track 2 not resumed in place", done.Tracks[1], p.ranges)
	}
	if got := mustReadFile(t, filepath.Join(r.Output, "0002", "content.mp3")); got != p.bodies["part_02_64kb.mp3"] {
		t.Fatal("resumed track bytes differ")
	}
	if receipt := readReceipt(t, filepath.Join(r.Output, "0002")); receipt.ResumedFrom != 7 || receipt.Intent.Request.Recording == nil || receipt.Intent.Request.Recording.Position != 2 {
		t.Fatal(receipt)
	}
}
