package tui

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/pdftext/pdftest"
)

func TestSourceAccessStagesReadsRetriesAndRetainsFailedContinuation(t *testing.T) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	f, err := z.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write([]byte("application/epub+zip")); err != nil {
		t.Fatal(err)
	}
	for _, file := range []struct{ name, data string }{
		{"META-INF/container.xml", `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`},
		{"book.opf", `<package xmlns="http://www.idpf.org/2007/opf"><metadata><title>Test</title><language>ja</language></metadata><manifest><item id="c" href="c.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="c"/></spine></package>`},
		{"c.xhtml", `<html xml:lang="ja"><body><p>` + strings.Repeat("日本語 line<br/>", 800) + `</p></body></html>`},
	} {
		f, err := z.Create(file.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write([]byte(file.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	book := filepath.Join(root, "book.epub")
	if err = os.WriteFile(book, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	catalog := filepath.Join(t.TempDir(), "catalog.json")
	service := app.New()
	if _, err = service.Snapshot(context.Background(), root, catalog, inventory.Defaults(), false); err != nil {
		t.Fatal(err)
	}
	m := newModel(context.Background(), service)
	m.switchMode("holdings")
	m.input.SetValue(catalog)
	m.Update(m.start(true)())
	m.Update(key(tea.KeyF6))
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if m.mode != "read" || m.reader.asset == "" || m.secondary.Value() != "" || m.busy {
		t.Fatal("selection granted implicit root authority")
	}
	if cmd != nil {
		if _, request := cmd().(result); request {
			t.Fatal("staging performed read")
		}
	}
	if m.start(true) != nil {
		t.Fatal("root not required")
	}
	m.secondary.SetValue(root)
	m.Update(m.start(true)())
	if len(m.entries) != 1 || m.reader.next != "" {
		t.Fatal("unit manifest unavailable", m.status)
	}
	m.Update(key(tea.KeyF6))
	_, cmd = m.Update(key(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("unit read unavailable")
	}
	m.Update(cmd())
	if !strings.Contains(m.report, "日本語") || m.reader.next == "" {
		t.Fatal("range unavailable", m.status)
	}
	original, next := m.report, m.reader.next
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m.Update(cmd())
	if m.report != original || m.reader.next != next {
		t.Fatal("repeat advanced delivery")
	}
	m.switchMode("search")
	m.switchMode("read")
	if m.report != original || m.reader.next != next {
		t.Fatal("mode switch lost source position")
	}
	m.Update(key(tea.KeyF6))
	if err = os.Rename(book, book+".missing"); err != nil {
		t.Fatal(err)
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	m.Update(cmd())
	if !strings.Contains(m.status, "Incomplete") || m.reader.next != next {
		t.Fatal("failed read advanced reference")
	}
	if err = os.Rename(book+".missing", book); err != nil {
		t.Fatal(err)
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m.Update(cmd())
	if strings.Contains(m.status, "Incomplete") || m.reader.next == next {
		t.Fatal("failed range could not be retried")
	}
	m.secondary.SetValue(t.TempDir())
	if m.startRead(false, 0, m.reader.next, false) != nil {
		t.Fatal("edited root silently resumed")
	}
	// Even a failed manifest request under the new root cannot rebind the old
	// successful continuation to that root.
	m.Update(m.startRead(true, 0, "", false)())
	if m.startRead(false, 0, m.reader.next, false) != nil {
		t.Fatal("failed units request rebound prior continuation")
	}
	m.secondary.SetValue(root)
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'u', Text: "u"})
	m.Update(cmd())
	if len(m.entries) != 1 || m.reader.next != "" {
		t.Fatal("explicit return to units failed")
	}
	if _, err = os.Stat(filepath.Join(root, ".nemalo")); !os.IsNotExist(err) {
		t.Fatal("read created control state", err)
	}
}

func TestHoldingsStageMP3ForAudioIndexAndRanges(t *testing.T) {
	root := t.TempDir()
	frame := make([]byte, 417)
	copy(frame, []byte{0xFF, 0xFB, 0x90, 0x40})
	if err := os.WriteFile(filepath.Join(root, "track.mp3"), bytes.Repeat(frame, 400), 0600); err != nil {
		t.Fatal(err)
	}
	catalog := filepath.Join(t.TempDir(), "catalog.json")
	service := app.New()
	if _, err := service.Snapshot(context.Background(), root, catalog, inventory.Defaults(), false); err != nil {
		t.Fatal(err)
	}
	m := newModel(context.Background(), service)
	m.switchMode("holdings")
	m.format = "all"
	m.input.SetValue(catalog)
	m.Update(m.start(true)())
	m.Update(key(tea.KeyF6))
	m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if m.reader.representation != "mpeg-audio/1" {
		t.Fatal("MP3 holding staged as text", m.reader)
	}
	m.secondary.SetValue(root)
	m.Update(m.start(true)())
	if len(m.entries) != 1 || !strings.Contains(m.entries[0].title, "MP3 track") || !strings.Contains(m.report, "Indexed frames: 400") {
		t.Fatal("audio index missing", m.status, m.report)
	}
	m.Update(key(tea.KeyF6))
	_, cmd := m.Update(key(tea.KeyEnter))
	m.Update(cmd())
	if !strings.Contains(m.report, "Frames 0..") || m.reader.next == "" || strings.Contains(m.report, "\xff\xfb") {
		t.Fatal("audio range missing or bytes printed", m.status, m.report)
	}
}

func TestHoldingsListsAManagedLibraryAndStagesItsRoot(t *testing.T) {
	root := t.TempDir()
	if _, err := library.Initialize(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Import(context.Background(), library.ImportRequest{Library: root, Source: writeStateEPUB(t, t.TempDir()), Apply: true}); err != nil {
		t.Fatal(err)
	}
	m := newModel(context.Background(), app.New())
	m.switchMode("holdings")
	m.input.SetValue(root)
	m.Update(m.start(true)())
	if len(m.entries) != 1 || !m.entries[0].managed || !strings.Contains(m.report, "Matching holdings: 1") {
		t.Fatal("managed holdings not listed", m.status, m.report)
	}
	m.Update(key(tea.KeyF6))
	m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if m.mode != "read" || m.secondary.Value() != root || m.input.Value() != root {
		t.Fatal("managed root not staged", m.input.Value(), m.secondary.Value())
	}
	m.Update(m.start(true)())
	if len(m.entries) != 1 {
		t.Fatal("managed holding not readable", m.status, m.report)
	}
}

func TestHoldingsOpenKeepsTheListAndUsesACopy(t *testing.T) {
	root := t.TempDir()
	if _, err := library.Initialize(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Import(context.Background(), library.ImportRequest{Library: root, Source: writeStateEPUB(t, t.TempDir()), Apply: true}); err != nil {
		t.Fatal(err)
	}
	opened := ""
	service := app.Service{Handoff: filepath.Join(t.TempDir(), "handoff"), Launch: func(_ context.Context, _, file string) error { opened = file; return nil }}
	m := newModel(context.Background(), service)
	m.switchMode("holdings")
	m.input.SetValue(root)
	m.Update(m.start(true)())
	m.Update(key(tea.KeyF6))
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	if cmd == nil {
		t.Fatal("open not dispatched")
	}
	m.Update(cmd())
	if len(m.entries) != 1 || !strings.Contains(m.status, "read-only copy") || !strings.HasPrefix(opened, service.Handoff) {
		t.Fatal("open lost the list or used the stored file", m.status, opened)
	}
}

func TestHoldingsShowsATracksRecordingAndKeepsTheList(t *testing.T) {
	root := t.TempDir()
	if _, err := library.Initialize(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	frame := make([]byte, 417)
	copy(frame, []byte{0xFF, 0xFB, 0x90, 0x40})
	body := bytes.Repeat(frame, 20)
	sum := sha256.Sum256(body)
	receipt, _ := json.Marshal(map[string]any{"complete": true, "status": "untrusted_intake", "antivirus": "not_scanned",
		"transfer": map[string]any{"bytes": len(body), "sha256": hex.EncodeToString(sum[:])},
		"intent":   map[string]any{"request": map[string]any{"source_id": "archive:demo", "source_file": "one.mp3", "recording": map[string]any{"source_format": "64Kbps MP3", "position": 1, "tracks": 2, "title": "Chapter one"}}}})
	packet := filepath.Join(t.TempDir(), "0001")
	if err := os.MkdirAll(packet, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"content.mp3": body, "receipt.json": receipt} {
		if err := os.WriteFile(filepath.Join(packet, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := library.Import(context.Background(), library.ImportRequest{Library: root, Source: packet, Apply: true}); err != nil {
		t.Fatal(err)
	}
	m := newModel(context.Background(), app.Service{})
	m.switchMode("holdings")
	m.format = "mp3"
	m.input.SetValue(root)
	m.Update(m.start(true)())
	m.Update(key(tea.KeyF6))
	if len(m.entries) != 1 {
		t.Fatal("holdings", m.status, m.report)
	}
	if m.entries[0].recording != "archive:demo" || !strings.Contains(strings.Join(strings.Fields(m.footer()), " "), "t recording") {
		t.Fatal("recording not offered", m.entries[0], m.footer())
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	if cmd == nil {
		t.Fatal("recording not dispatched")
	}
	m.Update(cmd())
	if len(m.entries) != 1 || !strings.Contains(m.report, "1 of 2 tracks held") || !strings.Contains(m.report, "Missing positions: [2]") || !strings.Contains(m.report, "Chapter one") {
		t.Fatal("recording manifest", m.status, m.report)
	}
}

func TestHoldingsReadsAPDFByPageAndLabel(t *testing.T) {
	root := t.TempDir()
	if _, err := library.Initialize(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "paper.pdf")
	if err := os.WriteFile(source, pdftest.Pages("0 << /S /R /St 4 >>", "Page text here."), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Import(context.Background(), library.ImportRequest{Library: root, Source: source, Apply: true}); err != nil {
		t.Fatal(err)
	}
	m := newModel(context.Background(), app.Service{})
	m.switchMode("holdings")
	m.format = "pdf"
	m.input.SetValue(root)
	m.Update(m.start(true)())
	m.Update(key(tea.KeyF6))
	m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if m.reader.representation != "pdf-text/1" {
		t.Fatal("PDF holding not staged as pdf-text/1", m.reader)
	}
	m.Update(m.start(true)())
	if len(m.entries) != 1 || !strings.Contains(m.entries[0].title, `label "IV"`) {
		t.Fatal("pages", m.status, m.entries)
	}
	m.Update(key(tea.KeyF6))
	_, cmd := m.Update(key(tea.KeyEnter))
	m.Update(cmd())
	if !strings.Contains(m.report, "Page text here.") || !strings.Contains(m.report, `printed label "IV"`) {
		t.Fatal("page text", m.status, m.report)
	}
}
