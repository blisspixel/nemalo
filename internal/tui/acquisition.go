package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/acquisition"
	"github.com/blisspixel/nemalo/internal/discovery"
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
	// An existing packet for this file with intent and no receipt is an
	// interrupted transfer: d reviews resuming it instead of a new download.
	request.Resume = interruptedPacket(abs)
	m.downloadReview, m.confirmReport, m.confirmStatus = &request, m.report, m.status
	m.input.Blur()
	m.secondary.Blur()
	m.evidence = true
	m.status = "Review acquisition. Enter confirms; Escape returns without downloading."
	if request.Resume {
		m.setReport(fmt.Sprintf("Resume an interrupted transfer\n\nSource: %q\nSelected file: %q\nIntake packet: %q\n\nRequests fresh Archive metadata, then only the missing bytes, on condition that the source still serves the same file (If-Range with the strong validator recorded before the first transfer). A different reply, a weak validator, or changed declarations leave the packet untouched.\n\nThe whole file is then verified against the declared size and checksums, inspected, scanned, and receipted as one transfer.\n\nEnter resumes. Escape returns without downloading.\n", request.SourceID, request.File, request.Output))
		m.viewport.GotoTop()
		m.resize()
		return
	}
	m.setReport(fmt.Sprintf("Acquire one source file\n\nSource: %q\nSelected file: %q\nNew intake directory: %q\nMaximum transfer: %d bytes\n\nRequests fresh Archive metadata and one permitted file, with checked HTTPS delivery destinations. Requires declared size and checksum. The selected source statements remain unverified rights evidence.\n\nCreates intent.json, transfer.json, content with its format suffix, and receipt.json on success. Existing output is refused. An interrupted packet is kept; choosing it again with d reviews resuming it.\n\nRuns local structure checks without rendering or execution, then scans the downloaded bytes once with the installed scanner, if one is installed. A detection moves the file to the Nemalo quarantine instead of intake and offers the item's other files. This is untrusted intake, not checked publication.\n\nEnter acquires this exact selection. Escape returns without writing.\n", request.SourceID, request.File, request.Output, request.MaxBytes))
	m.viewport.GotoTop()
	m.resize()
}

func (m *model) confirmDownload(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit
	case "esc":
		m.downloadReview, m.recordingReview = nil, nil
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
		ctx, cancel := context.WithCancel(m.ctx)
		m.cancel = cancel
		m.id++
		id := m.id
		m.busy = true
		m.confirmReport, m.confirmStatus = "", ""
		m.status = "Acquiring to untrusted intake. Escape cancels; partial output is retained."
		m.setReport("")
		m.populate(nil)
		m.resize()
		if m.recordingReview != nil {
			request := *m.recordingReview
			m.recordingReview = nil
			m.status = "Acquiring every track to untrusted intake. Escape cancels; completed tracks are kept for a resume."
			return func() tea.Msg { data, err := m.service.AcquireRecording(ctx, request); return result{id, data, err} }
		}
		request := *m.downloadReview
		m.downloadReview = nil
		return func() tea.Msg { data, err := m.service.Acquire(ctx, request); return result{id, data, err} }
	}
	return nil
}

func (m *model) reviewingDownload() bool {
	return m.downloadReview != nil || m.recordingReview != nil
}

// recordingFor returns the declared recording that holds file in the item
// evaluated last, if it can be acquired as a whole.
func (m *model) recordingFor(file string) *discovery.Recording {
	if m.lastEvaluation == nil || m.lastEvaluation.ID != m.submittedInput || m.input.Value() != m.submittedInput {
		return nil
	}
	for i, r := range m.lastEvaluation.Recordings {
		for _, t := range r.Tracks {
			if t.File == file && r.Acquirable {
				return &m.lastEvaluation.Recordings[i]
			}
		}
	}
	return nil
}

// requestRecording reviews acquiring every track of the recording that holds
// the selected file. Nothing is requested until Enter.
func (m *model) requestRecording() {
	if m.input.Value() != m.submittedInput {
		m.status = "Item changed. Evaluate again before selecting a download."
		m.resize()
		return
	}
	recording := m.recordingFor(m.entries[m.selected].sourceID)
	// The review shows the declared total, so the budget is exactly that total;
	// a recording over the 16 GiB maximum is refused here, before review.
	request := acquisition.RecordingRequest{SourceID: m.submittedInput, Format: recording.Format, Output: m.secondary.Value(), MaxBytes: recording.Bytes}
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
	m.recordingReview, m.confirmReport, m.confirmStatus = &request, m.report, m.status
	m.input.Blur()
	m.secondary.Blur()
	m.evidence = true
	m.status = "Review acquisition. Enter confirms; Escape returns without downloading."
	m.setReport(fmt.Sprintf("Acquire every track of one recording\n\nSource: %q\nRecording: %q, %d track(s), %d declared bytes\nTrack order from: %q\nNew recording directory: %q\nBudget: the declared total, at most 16 GiB\n\nRequests fresh Archive metadata, then each track in declared order as its own intake packet (0001, 0002, ...), with the same checks, scan, and quarantine as a single file. A detection stops the recording. recording.json is written only when every track has a receipt.\n\nAn interrupted recording is retained; acquire --recording with --resume keeps completed tracks. Track order and titles are source declarations. This is untrusted intake, not checked publication.\n\nEnter acquires this recording. Escape returns without writing.\n", request.SourceID, recording.Format, len(recording.Tracks), recording.Bytes, recording.OrderSource, request.Output))
	m.viewport.GotoTop()
	m.resize()
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

// interruptedPacket reports whether dir holds an acquisition's intent and
// transfer records without a receipt. The service decides whether its bytes
// can actually be resumed.
func interruptedPacket(dir string) bool {
	_, intent := os.Stat(filepath.Join(dir, "intent.json"))
	_, transfer := os.Stat(filepath.Join(dir, "transfer.json"))
	_, receipt := os.Stat(filepath.Join(dir, "receipt.json"))
	return intent == nil && transfer == nil && os.IsNotExist(receipt)
}
