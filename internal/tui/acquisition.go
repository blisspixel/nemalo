package tui

import (
	"context"
	"fmt"
	"path/filepath"

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
	m.setReport(fmt.Sprintf("Acquire one source file\n\nSource: %q\nSelected file: %q\nNew intake directory: %q\nMaximum transfer: %d bytes\n\nRequests fresh Archive metadata and one permitted file, with checked HTTPS delivery destinations. Requires declared size and checksum. The selected source statements remain unverified rights evidence.\n\nCreates intent.json, content with its format suffix, and receipt.json on success. Existing output is refused. Incomplete packets remain for review; resume and automatic cleanup are unavailable.\n\nRuns local structure checks, without antivirus, rendering, or execution. This is untrusted intake, not checked publication.\n\nEnter acquires this exact selection. Escape returns without writing.\n", request.SourceID, request.File, request.Output, request.MaxBytes))
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
