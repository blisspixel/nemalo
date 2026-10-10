package tui

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/library"
)

func TestStateInitializationReviewAndDispatch(t *testing.T) {
	root := t.TempDir()
	m := newModel(context.Background(), app.New())
	m.Update(tea.KeyPressMsg{Code: '9', Mod: tea.ModAlt})
	if m.mode != "state" {
		t.Fatal("direct state navigation unavailable")
	}
	m.Update(key(tea.KeyF7))
	if m.confirmRoot != "" {
		t.Fatal("empty root entered write review")
	}
	m.input.SetValue(root)
	_, cmd := m.Update(key(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("status unavailable")
	}
	m.Update(cmd())
	if !strings.Contains(m.report, "not_initialized") {
		t.Fatal("status hidden", m.report)
	}
	previous := m.report
	m.Update(key(tea.KeyF7))
	if m.confirmRoot != root || !strings.Contains(m.report, strconv.Quote(root)) || !strings.Contains(m.report, "journal.jsonl") {
		t.Fatal("review lost explicit scope", m.report)
	}
	for _, keyMsg := range []tea.KeyPressMsg{{Code: 'x', Text: "x"}, {Code: '1', Mod: tea.ModAlt}, {Code: tea.KeyTab}, {Code: tea.KeyF6}} {
		m.Update(keyMsg)
	}
	if m.mode != "state" || m.input.Value() != root {
		t.Fatal("review scope changed")
	}
	m.Update(key(tea.KeyPgDown))
	m.Update(key(tea.KeyPgUp))
	if _, err := os.Stat(filepath.Join(root, ".nemalo")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("review mutated root")
	}
	m.Update(key(tea.KeyEscape))
	if m.confirmRoot != "" || m.report != previous || !m.input.Focused() {
		t.Fatal("cancel lost previous state")
	}
	m.Update(key(tea.KeyF7))
	_, cmd = m.Update(key(tea.KeyEnter))
	if cmd == nil || !m.busy || m.confirmRoot != "" {
		t.Fatal("confirmed initialization not dispatched")
	}
	if _, err := os.Stat(filepath.Join(root, ".nemalo")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("write happened outside service command")
	}
	m.Update(cmd())
	if m.busy || !strings.Contains(m.report, `Control state: "initialized"`) || !strings.Contains(m.report, "Content was not imported") {
		t.Fatal("initialization evidence missing", m.report)
	}
	if !m.input.Focused() {
		t.Fatal("completed modal operation left editing disabled")
	}
	_, cmd = m.Update(key(tea.KeyEnter))
	m.Update(cmd())
	if !strings.Contains(m.report, "Journal records: 2") {
		t.Fatal("status not routed to shared state")
	}
	m.Update(key(tea.KeyF7))
	_, cmd = m.Update(key(tea.KeyEnter))
	m.Update(key(tea.KeyEscape))
	if m.busy || !m.input.Focused() {
		t.Fatal("cancelled modal operation left editing disabled")
	}
	m.Update(cmd()) // The cancelled generation cannot replace the current state.
}

func TestStateReviewLayoutAndQuit(t *testing.T) {
	m := newModel(context.Background(), app.New())
	m.switchMode("state")
	m.input.SetValue(filepath.Join(t.TempDir(), strings.Repeat("long folder ", 50)))
	for _, size := range [][2]int{{40, 20}, {80, 24}, {112, 38}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.Update(key(tea.KeyF7))
		v := m.View().Content
		if lipgloss.Width(v) > size[0] || lipgloss.Height(v) > size[1] {
			t.Fatal("review overflow", size, lipgloss.Width(v), lipgloss.Height(v))
		}
		m.Update(key(tea.KeyF7))
		if m.confirmRoot != "" {
			t.Fatal("second F7 did not return")
		}
	}
	m.Update(key(tea.KeyF7))
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("quit blocked by review")
	}
}

func TestStateImportReviewCancelAndApply(t *testing.T) {
	root := t.TempDir()
	if _, err := library.Initialize(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	source := writeStateEPUB(t, t.TempDir())
	m := newModel(context.Background(), app.New())
	m.switchMode("state")
	m.Update(key(tea.KeyF8))
	if m.confirmRoot != "" {
		t.Fatal("empty import entered review")
	}
	m.input.SetValue(root)
	m.Update(key(tea.KeyF8))
	if m.confirmRoot != "" {
		t.Fatal("missing source entered review")
	}
	m.secondary.SetValue(source)
	m.Update(key(tea.KeyF8))
	if m.confirmKind != "import" || m.confirmRoot != root || !strings.Contains(m.report, strconv.Quote(source)) {
		t.Fatal("import review lost scope", m.report)
	}
	for _, size := range [][2]int{{40, 20}, {80, 24}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := m.View().Content
		if lipgloss.Width(view) > size[0] || lipgloss.Height(view) > size[1] {
			t.Fatal("import review overflow", size, lipgloss.Width(view), lipgloss.Height(view))
		}
	}
	m.Update(key(tea.KeyF7))
	if m.confirmRoot != "" {
		t.Fatal("cancelled import stayed in review")
	}
	if _, err := os.Stat(filepath.Join(root, ".nemalo", "operations.jsonl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled import wrote", err)
	}
	m.Update(key(tea.KeyF8))
	m.Update(key(tea.KeyEscape))
	m.Update(key(tea.KeyF8))
	m.Update(key(tea.KeyF8))
	if m.confirmRoot != "" {
		t.Fatal("second F8 did not return")
	}
	m.Update(key(tea.KeyF8))
	_, cmd := m.Update(key(tea.KeyEnter))
	if cmd == nil || !m.busy || m.confirmRoot != "" {
		t.Fatal("confirmed import not dispatched")
	}
	if _, err := os.Stat(filepath.Join(root, ".nemalo", "operations.jsonl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("write happened outside service command")
	}
	m.Update(cmd())
	if m.busy || !strings.Contains(m.report, `Status: "review"`) || !strings.Contains(m.report, `Title: "Test"`) {
		t.Fatal("import evidence missing", m.report)
	}
}

func writeStateEPUB(t *testing.T, dir string) string {
	t.Helper()
	files := map[string]string{
		"mimetype":               "application/epub+zip",
		"META-INF/container.xml": `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`,
		"book.opf":               `<package xmlns="http://www.idpf.org/2007/opf"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Test</dc:title><dc:language>ja</dc:language></metadata><manifest><item id="c" href="chapter.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="c"/></spine></package>`,
		"chapter.xhtml":          `<html xmlns="http://www.w3.org/1999/xhtml"><body><p>Hello</p></body></html>`,
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	header, err := z.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := header.Write([]byte(files["mimetype"])); err != nil {
		t.Fatal(err)
	}
	delete(files, "mimetype")
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(files[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "book.epub")
	if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
