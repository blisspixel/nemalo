package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/present"
)

type collectionReview struct {
	request app.CollectionAcquireRequest
	report  string
}

func (m *model) nextCollectionField() tea.Cmd {
	m.resultsFocused = false
	m.input.Blur()
	m.secondary.Blur()
	m.intake.Blur()
	switch {
	case m.intakeFocused:
		m.intakeFocused = false
	case m.secondaryFocused:
		m.secondaryFocused, m.intakeFocused = false, true
	default:
		m.secondaryFocused = true
	}
	return m.focusForm()
}

func (m *model) requestCollectionPreview() tea.Cmd {
	if m.mode != "collections" || m.busy || m.collectionReview != nil {
		return nil
	}
	if m.input.Value() == "" || m.secondary.Value() == "" || m.intake.Value() == "" {
		m.status = "Enter collection, initialized library and absolute outside intake with Ctrl+N before F8."
		m.resize()
		return nil
	}
	limit := int64(1 << 30)
	if m.collectionLargeBudget {
		limit = 5 << 30
	}
	req := app.CollectionAcquireRequest{Collection: m.input.Value(), Library: m.secondary.Value(), Intake: m.intake.Value(), MaxTotalBytes: limit, MaxRecordingBytes: limit, OnDetection: "quarantine"}
	m.collectionPending = &req
	m.submittedInput, m.submittedSecond = m.input.Value(), m.secondary.Value()
	ctx, cancel := context.WithTimeout(m.ctx, 15*time.Second)
	m.cancel = cancel
	m.id++
	id, service := m.id, m.service
	m.busy = true
	m.status = "Previewing local collection and intake state. No provider request or write. Escape cancels."
	m.setReport("")
	m.populate(nil)
	m.resize()
	return func() tea.Msg { data, err := service.CollectionAcquire(ctx, req); return result{id, data, err} }
}

func (m *model) showCollectionPreview(data app.CollectionAcquireResult, request app.CollectionAcquireRequest) {
	m.populate(data)
	m.setReport(present.CollectionAcquire(data))
	m.status = "Collection acquisition preview only. Nothing fetched or written."
	transfer := false
	for _, step := range data.Steps {
		transfer = transfer || step.Action == app.StepAcquire || step.Action == app.StepResume
		if step.Action == app.StepRetained {
			m.outcome = outcomeAttention
		}
	}
	if transfer && data.PlanDigest != "" {
		request.Library, request.Collection, request.Intake = data.Status.Library, data.Status.Collection, data.Intake
		request.Apply, request.Plan = true, data.PlanDigest
		m.collectionReview = &collectionReview{request: request, report: m.report}
		m.input.Blur()
		m.secondary.Blur()
		m.intake.Blur()
		m.evidence = true
		m.status = "Review collection acquisition. Enter permits network transfer; Escape keeps the offline preview."
		m.setReport(m.report + fmt.Sprintf("\nMaximum total transfer: %d bytes\nMaximum recording: %d bytes\nOverall deadline: 30 minutes\n\nThe exact local preview is checked again before any request or intake write. Changed review facts require a fresh preview. Each selected source and packet is still revalidated by acquisition.\n\nThe installed antivirus runs under its own cloud/sample policy. A detection uses quarantine; missing or incomplete scanning never means clean.\nDownloads remain untrusted intake. Nothing is imported and no source or holding is removed. Interrupted packets stay for review or supported resume.\n\nEnter confirms this acquisition. Escape keeps the offline preview without transfer.\n", request.MaxTotalBytes, request.MaxRecordingBytes))
	}
	m.viewport.GotoTop()
	m.resize()
}

func (m *model) confirmCollection(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit
	case "esc":
		report := m.collectionReview.report
		m.collectionReview = nil
		m.evidence = false
		m.setReport(report)
		m.status = "Collection preview only. No acquisition started."
		m.resize()
		return m.focusForm()
	case "pgup", "pgdown":
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return cmd
	case "enter":
		request := m.collectionReview.request
		m.collectionReview = nil
		ctx, cancel := context.WithTimeout(m.ctx, 30*time.Minute)
		m.cancel = cancel
		m.id++
		id, service := m.id, m.service
		m.busy = true
		m.status = "Acquiring the reviewed collection to untrusted intake. Escape cancels; started packets remain."
		m.setReport("")
		m.populate(nil)
		m.resize()
		return func() tea.Msg { data, err := service.CollectionAcquire(ctx, request); return result{id, data, err} }
	}
	return nil
}

func collectionSelectionLabel(item library.CollectionItem) string {
	selection := item.File
	if item.Recording != "" {
		selection = "recording " + item.Recording
	}
	return present.Line(item.Source) + " " + present.Line(selection)
}

func collectionEntries(data any) []entry {
	var out []entry
	switch data := data.(type) {
	case library.CollectionStatusResult:
		for _, it := range data.Items {
			detail := rows("Selection", collectionSelectionLabel(it.Item), "Resolved status", present.Words(it.Status), "Holdings", present.List(it.Holdings), "Note", present.Line(it.Item.Note), "Detail", present.Line(it.Detail))
			if it.DeclaredTracks > 0 {
				detail += fmt.Sprintf("Tracks held: %d of %d declared\n", it.HeldTracks, it.DeclaredTracks)
			}
			detail += "\nResolved only by recorded exact selection. Held bytes are not reverified by status. F5 shows revision hash and limits.\n"
			out = append(out, entry{title: collectionSelectionLabel(it.Item), summary: present.Words(it.Status), detail: detail})
		}
	case library.CollectionDiffResult:
		if !data.Complete {
			break
		}
		if data.NameChanged {
			out = append(out, entry{title: "Collection name changed", summary: present.Line(data.Before.Name) + " -> " + present.Line(data.After.Name), detail: rows("Before", present.Line(data.Before.Name), "After", present.Line(data.After.Name))})
		}
		for _, group := range []struct {
			label string
			items []library.CollectionPosition
		}{{"Added", data.Added}, {"Removed", data.Removed}} {
			for _, item := range group.items {
				out = append(out, entry{title: group.label + ": " + collectionSelectionLabel(item.Item), summary: fmt.Sprintf("Position %d", item.Position), detail: rows("Change", group.label, "Selection", collectionSelectionLabel(item.Item), "Note", present.Line(item.Item.Note)) + "\nRemoved selections never remove holdings or notes. This grants no acquisition authority.\n"})
			}
		}
		for _, item := range data.NotesChanged {
			out = append(out, entry{title: "Note: " + collectionSelectionLabel(item.Item), summary: fmt.Sprintf("Position %d -> %d", item.BeforePosition, item.AfterPosition), detail: rows("Selection", collectionSelectionLabel(item.Item), "Before note", present.Line(item.BeforeNote), "After note", present.Line(item.Item.Note)) + "\nThe selection is unchanged. This note is not an acquisition.\n"})
		}
		for _, item := range data.RetainedOrder {
			out = append(out, entry{title: "Order: " + collectionSelectionLabel(item.Item), summary: fmt.Sprintf("Position %d -> %d", item.BeforePosition, item.AfterPosition), detail: rows("Selection", collectionSelectionLabel(item.Item), "Note", present.Line(item.Item.Note)) + "\nRetained selection in revised order. No holding is moved.\n"})
		}
	case app.CollectionAcquireResult:
		for _, item := range data.Steps {
			summary := present.Words(item.Action)
			if item.Outcome != "" {
				summary += "; " + present.Words(item.Outcome)
			}
			detail := rows("Selection", collectionSelectionLabel(item.Item), "Packet", present.Line(item.Packet), "Action", present.Words(item.Action), "Outcome", present.Words(item.Outcome), "Detail", present.Line(item.Detail), "Antivirus", present.Words(item.Antivirus))
			detail += fmt.Sprintf("Transferred this run: %d bytes\n", item.Bytes)
			if item.Action == app.StepResume {
				detail += fmt.Sprintf("Remaining declared bytes: %d\n", item.Remaining)
			}
			detail += "\nDownloads remain untrusted intake. Ingest is a separate action. F5 shows the full collection and limits.\n"
			out = append(out, entry{title: collectionSelectionLabel(item.Item), summary: strings.TrimSpace(summary), detail: detail})
		}
	}
	return out
}
