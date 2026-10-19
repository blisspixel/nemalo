package tui

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/scanner"
)

type tuiScanner string

func (s tuiScanner) Scan(context.Context, string) scanner.Result {
	return scanner.Result{Status: string(s)}
}

type intakeProvider struct{ calls *int }

func (intakeProvider) Search(context.Context, string, int, int) (discovery.Page, error) {
	return discovery.Page{}, nil
}
func (p intakeProvider) Evaluate(context.Context, string) (discovery.Evaluation, error) {
	n := int64(len("%PDF-1.7\n%%EOF"))
	md := md5.Sum([]byte("%PDF-1.7\n%%EOF"))
	return discovery.Evaluation{ID: "archive:demo", Source: "archive", Complete: true, Access: "no_item_restriction_declared", Files: []discovery.OfferedFile{{Name: "book.pdf", Access: "public_file_candidate", Bytes: &n, MD5: hex.EncodeToString(md[:]), DownloadURL: "https://archive.org/download/demo/book.pdf"}}}, nil
}
func (p intakeProvider) Download(context.Context, discovery.OfferedFile) (*http.Response, error) {
	*p.calls++
	return &http.Response{StatusCode: 200, ContentLength: int64(len("%PDF-1.7\n%%EOF")), Header: http.Header{}, Body: io.NopCloser(strings.NewReader("%PDF-1.7\n%%EOF"))}, nil
}

func TestTerminalDownloadRequiresReviewAndRetainsExactSelection(t *testing.T) {
	calls := 0
	p := intakeProvider{&calls}
	m := newModel(context.Background(), app.Service{Providers: map[string]app.Searcher{"archive": p}, Scanner: tuiScanner("no_detections_reported"), Quarantine: filepath.Join(t.TempDir(), "q")})
	m.switchMode("evaluate")
	m.input.SetValue("archive:demo")
	e, _ := p.Evaluate(context.Background(), "archive:demo")
	m.submittedInput = "archive:demo"
	m.populate(e)
	m.secondary.SetValue(filepath.Join(t.TempDir(), "packet"))
	m.resultsFocused = false
	if handled, _ := m.browserKey("d"); handled || m.downloadReview != nil {
		t.Fatal("typing d authorized download")
	}
	m.resultsFocused = true
	m.browserKey("d")
	if m.downloadReview == nil || calls != 0 || !strings.Contains(m.View().Content, "Review acquisition") {
		t.Fatal("missing frozen review")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.downloadReview != nil || calls != 0 || cmd == nil {
		t.Fatal("cancelled review wrote")
	}
	m.resultsFocused = true
	m.browserKey("d")
	output := m.downloadReview.Output
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil || !m.busy {
		t.Fatal("confirmed request missing")
	}
	m.Update(cmd())
	if calls != 1 || m.busy || !strings.Contains(m.status, "no detections") || !strings.HasSuffix(m.drafts["check"].input, "content.pdf") {
		t.Fatal(calls, m.status)
	}
	if _, err := os.Stat(filepath.Join(output, "receipt.json")); err != nil {
		t.Fatal(err)
	}
}

func TestTerminalDownloadRejectsStaleAndIncompleteForms(t *testing.T) {
	m := newModel(context.Background(), app.Service{})
	m.mode = "evaluate"
	m.submittedInput = "archive:old"
	m.input.SetValue("archive:new")
	m.entries = []entry{{sourceID: "book.pdf"}}
	m.requestDownload()
	if m.downloadReview != nil || !strings.Contains(m.status, "changed") {
		t.Fatal(m.status)
	}
	m.input.SetValue("archive:old")
	m.requestDownload()
	if m.downloadReview != nil || !strings.Contains(m.status, "Ctrl+N") || strings.Contains(m.status, "acquire requires") {
		t.Fatal(m.status)
	}
	m.secondary.SetValue(filepath.Join(t.TempDir(), "packet"))
	m.requestDownload()
	if m.downloadReview == nil {
		t.Fatal(m.status)
	}
	if m.confirmDownload(tea.KeyPressMsg{Code: 'x'}) != nil || m.downloadReview == nil {
		t.Fatal("unrelated key confirmed")
	}
	if m.confirmDownload(tea.KeyPressMsg{Code: tea.KeyPgDown}) != nil {
		t.Fatal("unexpected scroll action")
	}
	if cmd := m.confirmDownload(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); cmd == nil {
		t.Fatal("quit unavailable")
	}
	cmd := m.confirmDownload(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(cmd())
	if !strings.Contains(m.status, "Incomplete") || !strings.Contains(m.report, "unavailable") {
		t.Fatal("provider failure hidden", m.status, m.report)
	}
}

func TestTerminalDetectionOffersTheItemsOtherFiles(t *testing.T) {
	calls := 0
	p := intakeProvider{&calls}
	m := newModel(context.Background(), app.Service{Providers: map[string]app.Searcher{"archive": p}, Scanner: tuiScanner("findings"), Quarantine: filepath.Join(t.TempDir(), "q")})
	m.switchMode("evaluate")
	m.input.SetValue("archive:demo")
	e, _ := p.Evaluate(context.Background(), "archive:demo")
	n := int64(14)
	e.Files = append(e.Files, discovery.OfferedFile{Name: "other.epub", Bytes: &n, Access: "public_file_candidate"})
	m.submittedInput = "archive:demo"
	m.id++
	m.Update(result{m.id, e, nil})
	m.secondary.SetValue(filepath.Join(t.TempDir(), "packet"))
	m.resultsFocused = true
	m.selectEntry(0)
	m.browserKey("d")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(cmd())
	if !strings.Contains(m.status, "Detection") || len(m.entries) != 1 || m.entries[0].sourceID != "other.epub" {
		t.Fatal("detection did not offer the other file", m.status, m.entries)
	}
	if !strings.Contains(m.report, "detected quarantined") || strings.Contains(m.drafts["check"].input, "content.pdf") {
		t.Fatal("detected file staged for checking", m.report)
	}
	if calls != 1 {
		t.Fatal("detection triggered another request", calls)
	}
	for _, tc := range []struct{ antivirus, want string }{{"not_scanned", "without a scan"}, {"unavailable", "counts as unscanned"}} {
		if got := acquisitionStatus(tc.antivirus, ""); !strings.Contains(got, tc.want) {
			t.Fatal(got)
		}
	}
}

type recordingTUIProvider struct{ calls *int }

func (recordingTUIProvider) Search(context.Context, string, int, int) (discovery.Page, error) {
	return discovery.Page{}, nil
}

func (recordingTUIProvider) Evaluate(context.Context, string) (discovery.Evaluation, error) {
	e := discovery.Evaluation{ID: "archive:demo", Source: "archive", Complete: true, Access: "no_item_restriction_declared"}
	r := discovery.Recording{Format: "64Kbps MP3", OrderSource: "declared_track", Acquirable: true}
	for i, name := range []string{"one_64kb.mp3", "two_64kb.mp3"} {
		body := "ID3 " + name
		n := int64(len(body))
		md := md5.Sum([]byte(body))
		e.Files = append(e.Files, discovery.OfferedFile{Name: name, Access: "public_file_candidate", Bytes: &n, MD5: hex.EncodeToString(md[:]), DownloadURL: "https://archive.org/download/demo/" + name})
		r.Tracks = append(r.Tracks, discovery.RecordingTrack{Position: i + 1, File: name, Bytes: n})
		r.Bytes += n
	}
	e.Recordings = []discovery.Recording{r}
	return e, nil
}

func (p recordingTUIProvider) Download(_ context.Context, f discovery.OfferedFile) (*http.Response, error) {
	*p.calls++
	body := "ID3 " + f.Name
	return &http.Response{StatusCode: 200, ContentLength: int64(len(body)), Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func TestTerminalRecordingAsksForAnIntakeFolder(t *testing.T) {
	p := recordingTUIProvider{new(int)}
	m := newModel(context.Background(), app.Service{Providers: map[string]app.Searcher{"archive": p}})
	m.switchMode("evaluate")
	m.input.SetValue("archive:demo")
	m.submittedInput = "archive:demo"
	e, _ := p.Evaluate(context.Background(), "archive:demo")
	m.id++
	m.Update(result{m.id, e, nil})
	m.resultsFocused = true
	m.selectEntry(1)
	if handled, _ := m.browserKey("a"); !handled || m.recordingReview != nil || !strings.Contains(m.status, "Ctrl+N") || strings.Contains(m.status, "acquire --recording") {
		t.Fatal(handled, m.status)
	}
}

func TestTerminalRecordingRequiresReviewAndAcquiresEveryTrack(t *testing.T) {
	calls := 0
	p := recordingTUIProvider{&calls}
	m := newModel(context.Background(), app.Service{Providers: map[string]app.Searcher{"archive": p}, Scanner: tuiScanner("no_detections_reported"), Quarantine: filepath.Join(t.TempDir(), "q")})
	m.switchMode("evaluate")
	m.input.SetValue("archive:demo")
	m.submittedInput = "archive:demo"
	e, _ := p.Evaluate(context.Background(), "archive:demo")
	m.id++
	m.Update(result{m.id, e, nil})
	m.secondary.SetValue(filepath.Join(t.TempDir(), "recording"))
	m.resultsFocused = true
	m.selectEntry(1)
	if !strings.Contains(strings.Join(strings.Fields(m.footer()), " "), "a all tracks") {
		t.Fatal("recording action not offered", m.footer())
	}
	m.browserKey("a")
	if m.recordingReview == nil || calls != 0 || !strings.Contains(m.report, "Acquire every track") || !strings.Contains(m.View().Content, "Review acquisition") {
		t.Fatal("missing recording review", m.report)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.recordingReview != nil || calls != 0 {
		t.Fatal("cancelled review wrote")
	}
	m.resultsFocused = true
	m.browserKey("a")
	output := m.recordingReview.Output
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil || !m.busy || m.recordingReview != nil {
		t.Fatal("confirmed recording missing")
	}
	m.Update(cmd())
	if calls != 2 || !strings.Contains(m.status, "2 track(s)") || !strings.Contains(m.report, "Track") {
		t.Fatal(calls, m.status, m.report)
	}
	if _, err := os.Stat(filepath.Join(output, "recording.json")); err != nil {
		t.Fatal(err)
	}
}

func TestTerminalRecordingIsNotOfferedForUnusableRecordings(t *testing.T) {
	calls := 0
	p := recordingTUIProvider{&calls}
	m := newModel(context.Background(), app.Service{Providers: map[string]app.Searcher{"archive": p}})
	m.switchMode("evaluate")
	m.input.SetValue("archive:demo")
	m.submittedInput = "archive:demo"
	e, _ := p.Evaluate(context.Background(), "archive:demo")
	e.Recordings[0].Acquirable = false
	m.id++
	m.Update(result{m.id, e, nil})
	m.resultsFocused = true
	m.selectEntry(0)
	if handled, _ := m.browserKey("a"); handled || m.recordingReview != nil || strings.Contains(strings.Join(strings.Fields(m.footer()), " "), "a all tracks") {
		t.Fatal("unusable recording offered")
	}
	m.input.SetValue("archive:other")
	e.Recordings[0].Acquirable = true
	m.lastEvaluation = &e
	if m.recordingFor("one_64kb.mp3") != nil {
		t.Fatal("stale evaluation offered a recording")
	}
}

// resumeTUIProvider cuts the first transfer short and then serves ranges.
type resumeTUIProvider struct{ cut bool }

func (resumeTUIProvider) Search(context.Context, string, int, int) (discovery.Page, error) {
	return discovery.Page{}, nil
}

const resumeBody = "%PDF-1.7\n% a longer body that will be interrupted part of the way\n%%EOF"

func (resumeTUIProvider) Evaluate(context.Context, string) (discovery.Evaluation, error) {
	n := int64(len(resumeBody))
	md := md5.Sum([]byte(resumeBody))
	return discovery.Evaluation{ID: "archive:demo", Source: "archive", Complete: true, Access: "no_item_restriction_declared", Files: []discovery.OfferedFile{{Name: "book.pdf", Access: "public_file_candidate", Bytes: &n, MD5: hex.EncodeToString(md[:]), DownloadURL: "https://archive.org/download/demo/book.pdf"}}}, nil
}

type failAfter struct {
	r io.Reader
	n int
}

func (f *failAfter) Read(b []byte) (int, error) {
	if f.n <= 0 {
		return 0, io.ErrUnexpectedEOF
	}
	if len(b) > f.n {
		b = b[:f.n]
	}
	n, err := f.r.Read(b)
	f.n -= n
	return n, err
}

func (p *resumeTUIProvider) Download(ctx context.Context, f discovery.OfferedFile) (*http.Response, error) {
	return p.DownloadFrom(ctx, f, 0, "")
}

func (p *resumeTUIProvider) DownloadFrom(_ context.Context, _ discovery.OfferedFile, offset int64, validator string) (*http.Response, error) {
	h := http.Header{"Etag": []string{`"v1"`}}
	if offset == 0 {
		var r io.Reader = strings.NewReader(resumeBody)
		if p.cut {
			r = &failAfter{r: r, n: 20}
		}
		return &http.Response{StatusCode: 200, ContentLength: int64(len(resumeBody)), Header: h, Body: io.NopCloser(r)}, nil
	}
	h.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, len(resumeBody)-1, len(resumeBody)))
	return &http.Response{StatusCode: 206, ContentLength: int64(len(resumeBody)) - offset, Header: h, Body: io.NopCloser(strings.NewReader(resumeBody[offset:]))}, nil
}

func TestTerminalDownloadOfAnInterruptedPacketReviewsResuming(t *testing.T) {
	p := &resumeTUIProvider{cut: true}
	m := newModel(context.Background(), app.Service{Providers: map[string]app.Searcher{"archive": p}, Scanner: tuiScanner("no_detections_reported"), Quarantine: filepath.Join(t.TempDir(), "q")})
	m.switchMode("evaluate")
	m.input.SetValue("archive:demo")
	m.submittedInput = "archive:demo"
	e, _ := p.Evaluate(context.Background(), "archive:demo")
	m.id++
	m.Update(result{m.id, e, nil})
	packet := filepath.Join(t.TempDir(), "packet")
	m.secondary.SetValue(packet)
	m.resultsFocused = true
	m.browserKey("d")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(cmd())
	if _, err := os.Stat(filepath.Join(packet, "receipt.json")); !os.IsNotExist(err) {
		t.Fatal("cut transfer completed")
	}
	p.cut = false
	m.resultsFocused = true
	m.browserKey("d")
	if m.downloadReview == nil || !m.downloadReview.Resume || !strings.Contains(m.report, "Resume an interrupted transfer") {
		t.Fatal("resume not reviewed", m.report)
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(cmd())
	got, err := os.ReadFile(filepath.Join(packet, "content.pdf"))
	if err != nil || string(got) != resumeBody || !strings.Contains(m.report, "Resumed from byte 20") {
		t.Fatal("resume", err, m.report)
	}
}
