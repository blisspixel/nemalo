package tui

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/inventory"
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
