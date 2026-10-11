package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/acquisition"
)

func (m *model) requestDownload() {
	if m.input.Value() != m.submittedInput {
		m.status = "Item changed. Evaluate again before selecting a download."
		m.resize()
		return
	}
	request := acquisition.Request{SourceID: m.submittedInput, File: m.entries[m.selected].sourceID, Output: m.secondary.Value(), MaxBytes: 256 << 20}
	if err := request.Validate(); err != nil {
		m.status = fmt.Sprintf("Acquisition error: %q", err.Error())
		m.resize()
		return
	}
	abs, err := filepath.Abs(request.Output)
	if err != nil {
		m.status = fmt.Sprintf("Acquisition error: %q", err.Error())
		return
	}
	request.Output = abs
	m.downloadReview, m.confirmReport, m.confirmStatus = &request, m.report, m.status
	m.input.Blur()
	m.secondary.Blur()
	m.evidence = true
	m.status = "Review acquisition. Enter confirms; Escape returns without downloading."
	m.setReport(fmt.Sprintf("Acquire one source file\n\nSource: %q\nSelected file: %q\nNew intake directory: %q\nMaximum transfer: %d bytes\n\nRequests fresh Archive metadata and one permitted file, with checked HTTPS delivery destinations. Requires declared size and checksum. The selected source statements remain unverified rights evidence.\n\nCreates intent.json, content with its format suffix, and receipt.json on success. Existing output is refused. Incomplete packets remain for review; resume and automatic cleanup are unavailable.\n\nRuns local structure checks without rendering or execution, then scans the downloaded bytes once with the installed scanner, if one is installed. A detection moves the file to the Nemalo quarantine instead of intake and offers the item's other files. This is untrusted intake, not checked publication.\n\nEnter acquires this exact selection. Escape returns without writing.\n", request.SourceID, request.File, request.Output, request.MaxBytes))
	m.viewport.GotoTop()
	m.resize()
}

func (m *model) confirmDownload(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit
	case "esc":
		m.downloadReview = nil
		m.setReport(m.confirmReport)
		m.status = m.confirmStatus
		m.confirmReport, m.confirmStatus, m.evidence = "", "", false
		m.resize()
		return m.focusForm()
	case "pgup", "pgdown":
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return cmd
	case "enter":
		request := *m.downloadReview
		m.downloadReview = nil
		m.confirmReport, m.confirmStatus = "", ""
		ctx, cancel := context.WithCancel(m.ctx)
		m.cancel = cancel
		m.id++
		id := m.id
		m.busy = true
		m.status = "Acquiring to untrusted intake. Escape cancels; partial output is retained."
		m.setReport("")
		m.populate(nil)
		m.resize()
		return func() tea.Msg { data, err := m.service.Acquire(ctx, request); return result{id, data, err} }
	}
	return nil
}

// acquisitionStatus summarizes the scan outcome of a completed intake.
func acquisitionStatus(antivirus, guidance string) string {
	switch antivirus {
	case "no_detections_reported":
		return "Downloaded to untrusted intake. The scan reported no detections; import can record it as checked."
	case "not_scanned":
		return "Downloaded to untrusted intake without a scan."
	}
	status := "Downloaded to untrusted intake. Scan " + strings.ReplaceAll(antivirus, "_", " ") + "; the file counts as unscanned."
	if guidance != "" {
		status += " " + guidance
	}
	return status
}

// offerOtherFiles restores the evaluated item's file list without the
// detected file, so the user can choose another file deliberately. Nothing is
// requested until the user reviews and confirms a new download.
func (m *model) offerOtherFiles(r acquisition.Result) {
	m.status = "Detection: the download was not kept as intake. Choose another file (F6, then d) or edit the Item to evaluate another source."
	if m.lastEvaluation == nil || r.Intent == nil || m.lastEvaluation.ID != r.Intent.Request.SourceID {
		return
	}
	others := *m.lastEvaluation
	others.Files = nil
	for _, f := range m.lastEvaluation.Files {
		if f.Name != r.Intent.Request.File {
			others.Files = append(others.Files, f)
		}
	}
	m.populate(others)
	m.evidence = true
}
