package tui

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/discovery"
)

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
	m := newModel(context.Background(), app.Service{Providers: map[string]app.Searcher{"archive": p}})
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
	if calls != 1 || m.busy || !strings.Contains(m.status, "untrusted") || !strings.HasSuffix(m.drafts["check"].input, "content.pdf") {
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
	if m.downloadReview != nil || !strings.Contains(m.status, "error") {
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
