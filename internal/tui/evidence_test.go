package tui

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/decode"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/pdftext/pdftest"
)

func evidenceModel(t *testing.T) (*model, string, library.ImportResult) {
	t.Helper()
	root := t.TempDir()
	ctx := context.Background()
	if _, err := library.Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	if _, err := library.EnableBookmarks(ctx, root); err != nil {
		t.Fatal(err)
	}
	if _, err := library.DisableBookmarks(ctx, root); err != nil {
		t.Fatal(err)
	}
	held := holdTUIBook(t, ctx, root, "evidence.epub", plainEPUB(t, strings.Repeat("Knowledge is power. 日本語. ", 400)+`<img src="missing.png" alt="Declared image"/>`))
	m := readModel(t, root, root, held.AssetID)
	m.service = contentsService(t)
	m.service.PCMDecoder = func(context.Context, string, string, int64, decode.PCMRequest) (decode.PCMRange, error) {
		t.Fatal("TUI evidence invoked a decoder")
		return decode.PCMRange{}, errors.New("unexpected decode")
	}
	return m, root, held
}

func evidenceExcerpt(t *testing.T, m *model) {
	t.Helper()
	cmd := m.startRead(false, 0, "", false)
	if cmd == nil {
		t.Fatal("missing excerpt", m.status)
	}
	m.Update(cmd())
	if m.reader.delivered == nil || m.reader.next == "" {
		t.Fatal("no successful bounded selection", m.status)
	}
}

func auxAction(t *testing.T, m *model, code rune) {
	t.Helper()
	_, cmd := m.Update(key(code))
	if cmd == nil {
		t.Fatal("missing explicit auxiliary action", code, m.status)
	}
	m.Update(cmd())
}

func TestReadEvidenceCitationReplayPreserveOrdinaryState(t *testing.T) {
	m, root, _ := evidenceModel(t)
	evidenceExcerpt(t, m)
	m.viewport.ScrollDown(3)
	m.resultsFocused = true
	before := terminalJobTree(t, root)
	reader, report, entries, selected, offset := m.reader, m.report, append([]entry(nil), m.entries...), m.selected, m.viewport.YOffset()
	auxAction(t, m, tea.KeyF2)
	if m.readAux == nil || !m.readAux.citation || !strings.Contains(m.readAux.report, "CSL-JSON item array:") || !strings.Contains(m.readAux.report, "Unknown fields") || !strings.Contains(m.readAux.report, "Portable citation") {
		t.Fatal("citation sidecar missing")
	}
	m.Update(key(tea.KeyEscape))
	auxAction(t, m, tea.KeyF4)
	if m.readAux == nil || m.readAux.reference == "" || !strings.Contains(m.readAux.report, "Portable evidence") || !strings.Contains(m.readAux.report, "Knowledge is power.") {
		t.Fatal("evidence missing", m.readAux)
	}
	token, proofReport := m.readAux.reference, m.readAux.report
	auxAction(t, m, tea.KeyF3)
	if m.readAux.reference != token || m.readAux.report != proofReport {
		t.Fatal("exact bounded replay changed")
	}
	m.Update(key(tea.KeyEscape))
	if m.readAux != nil || m.busy || !reflect.DeepEqual(m.reader, reader) || m.report != report || !reflect.DeepEqual(m.entries, entries) || m.selected != selected || m.viewport.YOffset() != offset || !m.resultsFocused {
		t.Fatal("auxiliary report replaced ordinary read state")
	}
	if !reflect.DeepEqual(before, terminalJobTree(t, root)) {
		t.Fatal("read-only views changed library or disabled bookmarks")
	}
}

func TestReadEvidenceRequiresSuccessfulCurrentExcerpt(t *testing.T) {
	for _, which := range []string{"none", "units", "pages", "contents", "failed", "cancelled", "catalog", "root", "asset", "restore"} {
		t.Run(which, func(t *testing.T) {
			m, root, _ := evidenceModel(t)
			if which != "none" {
				evidenceExcerpt(t, m)
			}
			switch which {
			case "units":
				m.Update(m.startRead(true, 0, "", false)())
			case "pages":
				m.Update(m.listPages()())
			case "contents":
				m.Update(m.listNavigation()())
			case "failed":
				m.Update(m.startRead(false, 999, "", false)())
			case "cancelled":
				cmd := m.startRead(false, 0, "", false)
				m.Update(key(tea.KeyEscape))
				m.Update(cmd())
			case "catalog":
				m.input.SetValue(filepath.Join(root, "other"))
			case "root":
				m.secondary.SetValue(filepath.Join(root, "other"))
			case "asset":
				m.reader.asset = "sha256:" + strings.Repeat("0", 64)
			case "restore":
				m.Update(m.restoreBookmark()())
			}
			if m.pager != nil {
				t.Fatal("disabled bookmark unexpectedly opened reader")
			}
			_, cmd := m.Update(key(tea.KeyF4))
			if cmd != nil || m.readAux != nil || m.busy || !strings.Contains(m.status, "successful excerpt") {
				t.Fatal("non-excerpt acquired evidence", m.status)
			}
		})
	}
}

func TestReadAuxCancellationLateRepliesAndScopeFence(t *testing.T) {
	for _, which := range []string{"cancel_before_run", "late_success", "replacement", "catalog", "root", "asset", "mode"} {
		t.Run(which, func(t *testing.T) {
			m, root, _ := evidenceModel(t)
			evidenceExcerpt(t, m)
			before := terminalJobTree(t, root)
			_, cmd := m.Update(key(tea.KeyF4))
			if cmd == nil {
				t.Fatal("no request")
			}
			var reply tea.Msg
			if which != "cancel_before_run" {
				reply = cmd()
			}
			switch which {
			case "cancel_before_run":
				m.Update(key(tea.KeyEscape))
				reply = cmd()
			case "late_success":
				m.Update(key(tea.KeyEscape))
			case "replacement":
				m.Update(key(tea.KeyEscape))
				auxAction(t, m, tea.KeyF2)
			case "catalog":
				m.input.SetValue("changed")
			case "root":
				m.secondary.SetValue("changed")
			case "asset":
				m.reader.asset = "sha256:" + strings.Repeat("0", 64)
			case "mode":
				m.switchMode("holdings")
			}
			m.Update(reply)
			if m.busy {
				t.Fatal("cancelled/changed request retained busy state")
			}
			if which == "replacement" {
				if m.readAux == nil || !m.readAux.citation || strings.Contains(m.readAux.report, "Portable evidence") {
					t.Fatal("late reply replaced current view")
				}
			} else if m.readAux != nil {
				t.Fatal("late or changed-scope response visible")
			}
			if !reflect.DeepEqual(before, terminalJobTree(t, root)) {
				t.Fatal("cancelled/late request wrote library")
			}
		})
	}
}

func TestReadAuxReplayFailureDiscardsPreviousPayloadAndToken(t *testing.T) {
	for _, which := range []string{"selection", "malformed", "metadata", "error_payload"} {
		t.Run(which, func(t *testing.T) {
			m, root, _ := evidenceModel(t)
			evidenceExcerpt(t, m)
			auxAction(t, m, tea.KeyF4)
			switch which {
			case "selection":
				m.readAux.request.MaxBytes++
			case "malformed":
				m.readAux.reference += "x"
			case "metadata":
				// A complete-prefix ordinary read cannot make a torn metadata tail
				// complete. Preserve the exact corrupt fixture for review.
				f, err := os.OpenFile(filepath.Join(root, ".nemalo", "operations.jsonl"), os.O_WRONLY|os.O_APPEND, 0)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.WriteString(`{"torn":`)
				if err != nil {
					t.Fatal(err)
				}
				if err = f.Close(); err != nil {
					t.Fatal(err)
				}
			}
			_, cmd := m.Update(key(tea.KeyF3))
			if cmd == nil {
				t.Fatal("missing explicit replay")
			}
			if m.readAux.reference != "" || m.readAux.report != "" {
				t.Fatal("pending replay retained old payload")
			}
			reply := cmd().(readAuxReply)
			if which == "error_payload" {
				reply.data = content.Result{Text: "untrusted partial payload", Evidence: &content.Evidence{Reference: "do not reuse"}}
				reply.err = errors.New("late failure")
			}
			before := terminalJobTree(t, root)
			m.Update(reply)
			if m.readAux.reference != "" || !strings.Contains(m.readAux.status, "discarded") || strings.Contains(m.readAux.report, "Knowledge is power.") || strings.Contains(m.readAux.report, "untrusted partial payload") {
				t.Fatal("failed replay leaked payload or token", m.readAux.status)
			}
			if !reflect.DeepEqual(before, terminalJobTree(t, root)) {
				t.Fatal("failure repaired or changed library")
			}
		})
	}
}

func TestReadAuxPhysicalKeysScopeOwnershipAndLayout(t *testing.T) {
	m, root, _ := evidenceModel(t)
	evidenceExcerpt(t, m)
	before := terminalJobTree(t, root)
	for _, name := range []string{"f2", "f4"} {
		m.Update(tea.KeyPressMsg{Code: 'x', Text: name})
		if m.readAux != nil {
			t.Fatal("committed text invoked action")
		}
	}
	// Restore the field after the ordinary form correctly received the text.
	m.input.SetValue(root)
	auxAction(t, m, tea.KeyF4)
	for _, name := range []string{"esc", "ctrl+c", "f3", "pgdown", "tab", "alt+1"} {
		_, cmd := m.Update(tea.KeyPressMsg{Code: 'x', Text: name})
		if cmd != nil || m.readAux == nil || m.mode != "read" || m.busy {
			t.Fatal("committed text invoked report control", name)
		}
	}
	m.Update(tea.PasteMsg{Content: "unexpected form edit"})
	for _, width := range []int{40, 80, 116, 40} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		view := m.View().Content
		if lipgloss.Height(view) > 24 || !strings.Contains(view, "Esc back") {
			t.Fatal("layout or exit control lost", width, view)
		}
		for _, line := range strings.Split(view, "\n") {
			if lipgloss.Width(line) > width {
				t.Fatal("report overflow", width, line)
			}
		}
		m.Update(key(tea.KeyEnd))
		if m.readAux.viewport.YOffset() == 0 {
			t.Fatal("long report did not scroll")
		}
		m.Update(key(tea.KeyHome))
	}
	if m.input.Value() != root || !reflect.DeepEqual(before, terminalJobTree(t, root)) {
		t.Fatal("report edited form or library")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil || m.readAux != nil {
		t.Fatal("physical quit unavailable")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("quit was not returned")
	}
}

func TestReadAuxManagedMetadataRequirementAndCitationWithoutBody(t *testing.T) {
	catalog, root, asset := handoffBook(t)
	m := readModel(t, catalog, root, asset)
	m.service = contentsService(t)
	evidenceExcerpt(t, m)
	auxAction(t, m, tea.KeyF4)
	if m.readAux.reference != "" || !strings.Contains(m.readAux.status, "discarded") {
		t.Fatal("snapshot became managed evidence")
	}
	m.Update(key(tea.KeyEscape))
	auxAction(t, m, tea.KeyF2)
	if !strings.Contains(m.readAux.status, "discarded") {
		t.Fatal("snapshot became citation library")
	}
	m, root, held := evidenceModel(t)
	// Citation metadata is independent of stored-body availability.
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(held.Path))); err != nil {
		t.Fatal(err)
	}
	before := terminalJobTree(t, root)
	auxAction(t, m, tea.KeyF2)
	if !strings.Contains(m.readAux.report, "CSL-JSON item array:") || strings.Contains(m.readAux.status, "discarded") || !reflect.DeepEqual(before, terminalJobTree(t, root)) {
		t.Fatal("citation opened or repaired stored content")
	}
}

func TestReadEvidenceMP3KeepsTypedRangeWithoutPrintingAudio(t *testing.T) {
	m, root, _ := evidenceModel(t)
	frame := make([]byte, 417)
	copy(frame, []byte{0xff, 0xfb, 0x90, 0x40})
	held := holdTUIBook(t, context.Background(), root, "fixture.mp3", bytes.Repeat(frame, 400))
	m.reader = readerState{asset: held.AssetID, representation: "mpeg-audio/1"}
	evidenceExcerpt(t, m)
	before := terminalJobTree(t, root)
	ordinary := m.report
	auxAction(t, m, tea.KeyF4)
	if m.readAux.reference == "" || !strings.Contains(m.readAux.report, `"encoded_samples"`) || !strings.Contains(m.readAux.report, "Frames 0..") || strings.Contains(m.readAux.report, "\xff\xfb") {
		t.Fatal("typed audio evidence missing or raw frames printed")
	}
	auxAction(t, m, tea.KeyF3)
	m.Update(key(tea.KeyEscape))
	if m.report != ordinary || !reflect.DeepEqual(before, terminalJobTree(t, root)) {
		t.Fatal("audio evidence changed read state or library")
	}
}

func TestReadEvidencePDFAndDeclaredEntryKeepTheirSelections(t *testing.T) {
	for _, format := range []string{"pdf", "navigation"} {
		t.Run(format, func(t *testing.T) {
			m, root, _ := evidenceModel(t)
			var held library.ImportResult
			if format == "pdf" {
				held = holdTUIBook(t, context.Background(), root, "quote.pdf", pdftest.Pages("", strings.Repeat("Precise PDF quotation. ", 400)))
				m.reader = readerState{asset: held.AssetID, representation: "pdf-text/1"}
			} else {
				held = holdTUIBook(t, context.Background(), root, "chapters.epub", chapterEPUB(t))
				m.reader = readerState{asset: held.AssetID, representation: "epub-structure/1"}
				m.Update(m.listNavigation()())
				if len(m.entries) == 0 {
					t.Fatal("no declared entries")
				}
			}
			cmd := m.startRead(false, 0, "", false)
			if cmd == nil {
				t.Fatal(m.status)
			}
			m.Update(cmd())
			if m.reader.delivered == nil {
				t.Fatal("selection unavailable", m.status)
			}
			selection := *m.reader.delivered
			before := terminalJobTree(t, root)
			auxAction(t, m, tea.KeyF4)
			if m.readAux.reference == "" || m.readAux.request != selection {
				t.Fatal("selection lost")
			}
			if format == "navigation" && (selection.Navigation == "" || selection.Representation != "epub-nav/1") {
				t.Fatal("declared boundary lost", selection)
			}
			auxAction(t, m, tea.KeyF3)
			m.Update(key(tea.KeyEscape))
			if *m.reader.delivered != selection || !reflect.DeepEqual(before, terminalJobTree(t, root)) {
				t.Fatal("replay moved or wrote source")
			}
		})
	}
}

func TestReadAuxMP4RemainsUnitsOnlyAndCancellationCannotReplay(t *testing.T) {
	m, root, _ := evidenceModel(t)
	held := holdTUIBook(t, context.Background(), root, "chapters.m4b", mp4Book())
	m.reader = readerState{asset: held.AssetID, representation: "mp4-audio/1"}
	m.Update(m.startRead(true, 0, "", false)())
	before := terminalJobTree(t, root)
	if cmd := m.startRead(false, 0, "", false); cmd != nil {
		t.Fatal("MP4 TUI read acquired decoder permission")
	}
	if cmd := m.openReadAux(false); cmd != nil || m.readAux != nil {
		t.Fatal("MP4 units became range evidence")
	}
	auxAction(t, m, tea.KeyF2)
	if cmd := m.requestReadAux(true); cmd != nil {
		t.Fatal("citation replay became a content read")
	}
	m.Update(key(tea.KeyEscape))
	if !reflect.DeepEqual(before, terminalJobTree(t, root)) {
		t.Fatal("MP4 metadata view changed library")
	}
	m, _, _ = evidenceModel(t)
	evidenceExcerpt(t, m)
	auxAction(t, m, tea.KeyF4)
	_, cmd := m.Update(key(tea.KeyF3))
	if cmd == nil {
		t.Fatal("missing replay")
	}
	m.cancel()
	m.Update(cmd())
	if m.readAux.reference != "" || !strings.Contains(m.readAux.status, "discarded") {
		t.Fatal("cancelled replay retained evidence")
	}
	if cmd := m.requestReadAux(true); cmd != nil {
		t.Fatal("failed replay reused old token")
	}
}
