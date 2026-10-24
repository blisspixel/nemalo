package tui

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/pdftext/pdftest"
	"github.com/blisspixel/nemalo/internal/present"
)

// Drive actual key codes and application replies; cursor blink commands are
// presentation timers, not a reason to wait before the next simulated key.
func collectionKeys(m *model, keys ...string) {
	for _, key := range keys {
		msg := tea.KeyPressMsg{}
		switch key {
		case "enter":
			msg.Code = tea.KeyEnter
		case "esc":
			msg.Code = tea.KeyEscape
		case "tab":
			msg.Code = tea.KeyTab
		case "f3":
			msg.Code = tea.KeyF3
		case "f5":
			msg.Code = tea.KeyF5
		case "f6":
			msg.Code = tea.KeyF6
		case "f8":
			msg.Code = tea.KeyF8
		default:
			if strings.HasPrefix(key, "alt+") {
				msg.Code = []rune(strings.TrimPrefix(key, "alt+"))[0]
				msg.Mod = tea.ModAlt
			} else if strings.HasPrefix(key, "ctrl+") {
				msg.Code = []rune(strings.TrimPrefix(key, "ctrl+"))[0]
				msg.Mod = tea.ModCtrl
			} else {
				msg.Code = []rune(key)[0]
				msg.Text = key
			}
		}
		_, cmd := m.Update(msg)
		if m.busy && cmd != nil {
			m.Update(cmd())
		}
	}
}

type collectionTerminalProvider struct {
	mu                     sync.Mutex
	evaluations, downloads int
	fail                   string
}

func (*collectionTerminalProvider) Search(context.Context, string, int, int) (discovery.Page, error) {
	return discovery.Page{}, errors.New("collection unexpectedly searched a provider")
}
func (p *collectionTerminalProvider) Evaluate(_ context.Context, id string) (discovery.Evaluation, error) {
	p.mu.Lock()
	p.evaluations++
	fail := p.fail
	p.mu.Unlock()
	if id == fail {
		return discovery.Evaluation{}, errors.New("source unavailable\x1b[31m")
	}
	name := strings.TrimPrefix(id, "archive:") + ".pdf"
	b := pdftest.Pages("", "Selected "+id)
	n, sum := int64(len(b)), md5.Sum(b)
	return discovery.Evaluation{ID: id, Source: "archive", Complete: true, Access: "no_item_restriction_declared", Files: []discovery.OfferedFile{{Name: name, Format: "Text PDF", Access: "public_file_candidate", Bytes: &n, MD5: hex.EncodeToString(sum[:]), DownloadURL: "https://archive.org/download/demo/" + name}}}, nil
}
func (p *collectionTerminalProvider) Download(_ context.Context, file discovery.OfferedFile) (*http.Response, error) {
	p.mu.Lock()
	p.downloads++
	p.mu.Unlock()
	b := pdftest.Pages("", "Selected archive:"+strings.TrimSuffix(file.Name, ".pdf"))
	return &http.Response{StatusCode: 200, ContentLength: int64(len(b)), Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(b))}, nil
}
func (p *collectionTerminalProvider) counts() (int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.evaluations, p.downloads
}

func writeTerminalCollection(t *testing.T, path, name string, items ...library.CollectionItem) {
	t.Helper()
	b, err := json.Marshal(library.Collection{SchemaVersion: 1, Name: name, Items: items})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}

func terminalCollection(t *testing.T) (*model, *collectionTerminalProvider, string, string, string) {
	t.Helper()
	root := t.TempDir()
	if _, err := library.Initialize(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if _, err := library.EnableBookmarks(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if _, err := library.DisableBookmarks(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(t.TempDir(), "collection.json")
	writeTerminalCollection(t, manifest, "Terminal selections", library.CollectionItem{Source: "archive:one", File: "one.pdf", Note: "Exact selection"})
	intake := filepath.Join(t.TempDir(), "missing-intake")
	p := &collectionTerminalProvider{}
	m := newModel(context.Background(), app.Service{Providers: map[string]app.Searcher{"archive": p}, Scanner: tuiScanner("no_detections_reported"), Quarantine: filepath.Join(t.TempDir(), "quarantine")})
	m.Update(tea.WindowSizeMsg{Width: 116, Height: 36})
	collectionKeys(m, "alt+a")
	if m.mode != "collections" {
		t.Fatal("collection shortcut unavailable")
	}
	m.input.SetValue(manifest)
	m.secondary.SetValue(root)
	m.intake.SetValue(intake)
	return m, p, root, manifest, intake
}

func TestTerminalCollectionStatusDraftsAndOfflinePreviewPreserveLibrary(t *testing.T) {
	m, p, root, manifest, intake := terminalCollection(t)
	// A larger inventory read allowance does not enlarge acquisition consent.
	m.largeBudget = true
	before := terminalJobTree(t, root)
	collectionKeys(m, "enter")
	if len(m.entries) != 1 || m.entries[0].title != "archive:one one.pdf" || m.entries[0].summary != "not held" || !strings.Contains(m.report, "Collection SHA-256") {
		t.Fatal("wrong local status", m.report, m.entries)
	}
	collectionKeys(m, "f6", "enter")
	if !m.details || !strings.Contains(m.preview.GetContent(), "Exact selection") {
		t.Fatal("selection details unavailable")
	}
	collectionKeys(m, "f5")
	if !m.evidence || !strings.Contains(m.View().Content, "Collection SHA-256") {
		t.Fatal("full report unavailable")
	}
	collectionKeys(m, "f6", "ctrl+n", "ctrl+n")
	if !m.intakeFocused || !m.intake.Focused() || m.input.Focused() || m.secondary.Focused() {
		t.Fatal("third field did not gain exclusive focus")
	}
	collectionKeys(m, "ctrl+n")
	if m.intakeFocused || !m.input.Focused() {
		t.Fatal("field cycle did not wrap")
	}
	collectionKeys(m, "alt+0")
	if m.mode != "read" {
		t.Fatal("original Alt+0 changed")
	}
	collectionKeys(m, "alt+a")
	if m.input.Value() != manifest || m.secondary.Value() != root || m.intake.Value() != intake || m.intake.Focused() {
		t.Fatal("collection drafts or focus lost")
	}
	collectionKeys(m, "f8")
	if m.collectionReview == nil || m.collectionReview.request.MaxTotalBytes != 1<<30 || !strings.Contains(m.report, "Overall deadline: 30 minutes") {
		t.Fatal("offline review missing", m.status)
	}
	collectionKeys(m, "esc")
	if m.collectionReview != nil || !strings.Contains(m.status, "No acquisition started") {
		t.Fatal("review did not cancel")
	}
	if e, d := p.counts(); e != 0 || d != 0 {
		t.Fatal("passive action reached provider", e, d)
	}
	if _, err := os.Stat(intake); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("passive action created intake", err)
	}
	if !reflect.DeepEqual(before, terminalJobTree(t, root)) {
		t.Fatal("passive action changed library or disabled bookmarks")
	}
}

func TestTerminalCollectionHeldStatusAndUnavailableInputsNeverInitializeOrFetch(t *testing.T) {
	m, p, root, manifest, intake := terminalCollection(t)
	other := filepath.Join(t.TempDir(), "acquired")
	if r, err := m.service.CollectionAcquire(context.Background(), app.CollectionAcquireRequest{Library: root, Collection: manifest, Intake: other, Apply: true}); err != nil || !r.Complete {
		t.Fatal(r, err)
	}
	if r, err := m.service.Ingest(context.Background(), library.IngestRequest{Library: root, Folder: other, Apply: true}); err != nil || !r.Complete {
		t.Fatal(r, err)
	}
	e, d := p.counts()
	before := terminalJobTree(t, root)
	collectionKeys(m, "enter", "f8")
	if m.collectionReview != nil || len(m.entries) != 0 || !strings.Contains(m.report, "Every item is held") {
		t.Fatal("held collection offered acquisition", m.status, m.report)
	}
	collectionKeys(m, "enter")
	if len(m.entries) != 1 || !strings.Contains(m.entries[0].detail, "sha256:") || m.entries[0].summary == "not held" {
		t.Fatal("held selection unresolved", m.entries)
	}
	if _, err := os.Stat(intake); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("held preview created intake", err)
	}
	if !reflect.DeepEqual(before, terminalJobTree(t, root)) {
		t.Fatal("held status changed library")
	}
	missing := t.TempDir()
	m.secondary.SetValue(missing)
	collectionKeys(m, "enter", "f8")
	if m.outcome != outcomeFailed || m.collectionReview != nil || !strings.Contains(m.report, "Preview did not complete") {
		t.Fatal("uninitialized library accepted", m.status, m.report)
	}
	if entries, err := os.ReadDir(missing); err != nil || len(entries) != 0 {
		t.Fatal("passive action initialized library", entries, err)
	}
	m.secondary.SetValue(root)
	m.intake.SetValue("relative")
	collectionKeys(m, "f8")
	if m.collectionReview != nil || m.outcome != outcomeFailed || !strings.Contains(m.report, "absolute path") {
		t.Fatal("relative intake accepted", m.status, m.report)
	}
	m.intake.SetValue("")
	if cmd := m.requestCollectionPreview(); cmd != nil || m.busy || !strings.Contains(m.status, "Enter collection") {
		t.Fatal("missing input started work")
	}
	if gotE, gotD := p.counts(); gotE != e || gotD != d {
		t.Fatal("passive/refused input reached provider", gotE, gotD, e, d)
	}
}

func TestTerminalCollectionRevisionComparisonKeepsAllChangeKindsAndFiles(t *testing.T) {
	m, p, root, before, _ := terminalCollection(t)
	first := library.CollectionItem{Source: "archive:one", File: "one.pdf", Note: "Old"}
	second := library.CollectionItem{Source: "archive:two", File: "two.pdf"}
	removed := library.CollectionItem{Source: "archive:three", File: "three.pdf"}
	writeTerminalCollection(t, before, "Before", first, second, removed)
	after := filepath.Join(t.TempDir(), "after.json")
	first.Note = "New"
	writeTerminalCollection(t, after, "After", second, library.CollectionItem{Source: "archive:four", File: "four.pdf"}, first)
	state := terminalJobTree(t, root)
	collectionKeys(m, "alt+b")
	m.input.SetValue(before)
	m.secondary.SetValue(after)
	want, err := m.service.CollectionDiff(context.Background(), before, after)
	if err != nil {
		t.Fatal(err)
	}
	collectionKeys(m, "enter")
	if len(m.entries) != 6 || m.report != present.CollectionDiff(want) {
		t.Fatal("diff lost changes", m.entries, m.report)
	}
	for _, label := range []string{"Collection name changed", "Added:", "Removed:", "Note:", "Order:"} {
		found := false
		for _, e := range m.entries {
			found = found || strings.HasPrefix(e.title, label)
		}
		if !found {
			t.Fatal("missing change kind", label)
		}
	}
	for _, width := range []int{40, 80, 116} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		for _, line := range strings.Split(m.View().Content, "\n") {
			if lipgloss.Width(line) > width {
				t.Fatal("revision frame overflow", width, line)
			}
		}
	}
	if !strings.Contains(m.report, want.Before.SHA256) || !strings.Contains(m.report, want.After.SHA256) {
		t.Fatal("revision hashes absent")
	}
	if e, d := p.counts(); e != 0 || d != 0 {
		t.Fatal("comparison reached provider", e, d)
	}
	if !reflect.DeepEqual(state, terminalJobTree(t, root)) {
		t.Fatal("comparison changed holdings or notes")
	}
}

func TestTerminalCollectionReviewFreezesRequestAndAppliesOnlyToUntrustedIntake(t *testing.T) {
	m, p, root, _, intake := terminalCollection(t)
	before := terminalJobTree(t, root)
	collectionKeys(m, "f3", "f8")
	if m.collectionReview == nil {
		t.Fatal(m.status)
	}
	request := m.collectionReview.request
	if request.MaxTotalBytes != 5<<30 || request.MaxRecordingBytes != 5<<30 || request.NoScan || request.OnDetection != "quarantine" || !request.Apply || request.Plan == "" {
		t.Fatal("wrong frozen request", request)
	}
	for _, key := range []string{"f3", "f8", "tab", "alt+9", "ctrl+n", "x"} {
		collectionKeys(m, key)
	}
	m.Update(tea.PasteMsg{Content: "hidden change"})
	if !reflect.DeepEqual(m.collectionReview.request, request) || m.input.Focused() || m.secondary.Focused() || m.intake.Focused() {
		t.Fatal("review changed frozen request")
	}
	// Editing the live widgets cannot redirect the captured request either.
	m.input.SetValue("wrong-collection")
	m.secondary.SetValue(t.TempDir())
	m.intake.SetValue(filepath.Join(t.TempDir(), "wrong"))
	collectionKeys(m, "enter")
	if m.collectionReview != nil || m.busy || m.outcome != outcomeOK || !strings.Contains(m.status, "untrusted intake") {
		t.Fatal("confirmed result missing", m.status, m.outcome)
	}
	if e, d := p.counts(); e != 1 || d != 1 {
		t.Fatal("wrong acquisition count", e, d)
	}
	if _, err := os.Stat(filepath.Join(intake, library.CollectionItem{Source: "archive:one", File: "one.pdf"}.Packet(), "receipt.json")); err != nil {
		t.Fatal("reviewed intake unused", err)
	}
	if _, err := os.Stat(m.intake.Value()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("live widget redirected write", err)
	}
	if !reflect.DeepEqual(before, terminalJobTree(t, root)) {
		t.Fatal("acquisition imported or changed library/bookmarks")
	}
}

func TestTerminalCollectionChangedManifestRefusesAndCancellationIgnoresLatePreview(t *testing.T) {
	m, p, root, manifest, intake := terminalCollection(t)
	before := terminalJobTree(t, root)
	cmd := m.requestCollectionPreview()
	if cmd == nil {
		t.Fatal(m.status)
	}
	reply := cmd()
	collectionKeys(m, "esc")
	m.Update(reply)
	if m.collectionReview != nil || m.collectionPending != nil || m.busy {
		t.Fatal("late reply reopened cancelled review")
	}
	cmd = m.requestCollectionPreview()
	if cmd == nil {
		t.Fatal(m.status)
	}
	m.Update(reply)
	if m.collectionReview != nil || !m.busy {
		t.Fatal("old reply replaced new request")
	}
	m.Update(cmd())
	if m.collectionReview == nil {
		t.Fatal("fresh review unavailable")
	}
	writeTerminalCollection(t, manifest, "Changed", library.CollectionItem{Source: "archive:two", File: "two.pdf"})
	collectionKeys(m, "enter")
	if m.collectionReview != nil || m.outcome != outcomeFailed || !strings.Contains(m.report, "preview changed") {
		t.Fatal("stale acquisition accepted", m.status, m.report)
	}
	if e, d := p.counts(); e != 0 || d != 0 {
		t.Fatal("stale selection reached provider", e, d)
	}
	if _, err := os.Stat(intake); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refusal created intake", err)
	}
	if !reflect.DeepEqual(before, terminalJobTree(t, root)) {
		t.Fatal("cancel/refusal changed library")
	}
	cmd = m.requestCollectionPreview()
	collectionKeys(m, "esc")
	cancelled := cmd().(result)
	if !errors.Is(cancelled.err, context.Canceled) {
		t.Fatal("preview was not cancelled", cancelled.err)
	}
	collectionKeys(m, "alt+9")
	m.Update(cancelled)
	if m.mode != "state" || m.collectionReview != nil {
		t.Fatal("cancelled reply changed later mode")
	}
}

func TestTerminalCollectionRetainedAndReceiptedPacketsDoNotOfferAnotherTransfer(t *testing.T) {
	for _, retained := range []bool{false, true} {
		t.Run(map[bool]string{false: "receipted", true: "retained"}[retained], func(t *testing.T) {
			m, p, _, _, intake := terminalCollection(t)
			if retained {
				if err := os.MkdirAll(filepath.Join(intake, library.CollectionItem{Source: "archive:one", File: "one.pdf"}.Packet()), 0700); err != nil {
					t.Fatal(err)
				}
			} else {
				collectionKeys(m, "f8", "enter")
			}
			before := terminalJobTree(t, intake)
			e, d := p.counts()
			collectionKeys(m, "f8")
			if m.collectionReview != nil || len(m.entries) != 1 {
				t.Fatal("non-transfer action offered acquisition", m.status, m.entries)
			}
			if gotE, gotD := p.counts(); gotE != e || gotD != d {
				t.Fatal("preview fetched again")
			}
			if !reflect.DeepEqual(before, terminalJobTree(t, intake)) {
				t.Fatal("existing packet changed")
			}
		})
	}
}

func TestTerminalCollectionPartialFailureStaysVisibleAndEscaped(t *testing.T) {
	m, p, root, manifest, _ := terminalCollection(t)
	writeTerminalCollection(t, manifest, "Partial", library.CollectionItem{Source: "archive:one", File: "one.pdf"}, library.CollectionItem{Source: "archive:two", File: "two.pdf"})
	p.fail = "archive:two"
	before := terminalJobTree(t, root)
	collectionKeys(m, "f8", "enter")
	if m.outcome != outcomeFailed || !strings.Contains(m.status, "incomplete") || len(m.entries) != 2 || strings.ContainsRune(m.report, '\x1b') || !strings.Contains(m.report, "source unavailable") {
		t.Fatal("partial failure hidden or unsafe", m.status, m.report)
	}
	if e, d := p.counts(); e != 2 || d != 1 {
		t.Fatal("wrong partial acquisition", e, d)
	}
	if !reflect.DeepEqual(before, terminalJobTree(t, root)) {
		t.Fatal("partial acquisition changed library")
	}
}

func TestTerminalCollectionRestoredFramesStayWithinResizedScreen(t *testing.T) {
	m, _, _, manifest, intake := terminalCollection(t)
	packet := filepath.Join(intake, library.CollectionItem{Source: "archive:one", File: "one.pdf"}.Packet())
	if err := os.MkdirAll(packet, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packet, "receipt.json"), []byte(`{"complete":true,"status":"untrusted_intake","intent":{"request":{"source_id":"archive:one","source_file":"one.pdf"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	collectionKeys(m, "f8")
	if m.collectionReview != nil || !strings.Contains(m.report, "awaiting import") {
		t.Fatal("wrong passive fixture", m.report)
	}
	collectionKeys(m, "alt+b")
	m.input.SetValue(manifest)
	m.secondary.SetValue(manifest)
	collectionKeys(m, "enter")
	for _, order := range [][]string{{"read", "wide", "collections"}, {"read", "collections", "wide"}, {"wide", "read", "collections"}} {
		m.switchMode("revisions")
		m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
		for _, action := range order {
			switch action {
			case "read", "collections":
				m.switchMode(action)
			case "wide":
				m.Update(tea.WindowSizeMsg{Width: 116, Height: 36})
			}
			frame := m.View().Content
			if lipgloss.Height(frame) > m.height || lipgloss.Width(frame) > m.width {
				t.Fatalf("%v after %s: frame %dx%d exceeds terminal %dx%d\n%s", order, action, lipgloss.Width(frame), lipgloss.Height(frame), m.width, m.height, frame)
			}
		}
	}
}
