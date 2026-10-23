package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/app"
)

// observeJobs is an explicit, one-shot read through the application service.
// Existing result generations and cancellation own its lifetime; no timer, job
// writer store or alternate relationship policy belongs in the terminal.
func (m *model) observeJobs() tea.Cmd {
	if m.busy || m.confirmRoot != "" || m.reviewingDownload() || m.mode != "state" {
		return nil
	}
	if m.input.Value() == "" {
		m.status = "Enter an existing initialized library folder before observing jobs."
		m.resize()
		return nil
	}
	request := app.JobSnapshotRequest{Library: m.input.Value(), JobsDirectory: m.service.JobsDirectory}
	service := m.service
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.id++
	id := m.id
	m.busy = true
	m.submittedInput, m.submittedSecond = m.input.Value(), m.secondary.Value()
	m.status = "Observing recorded jobs. Escape cancels the observation."
	m.setReport("")
	m.populate(nil)
	m.resize()
	return func() tea.Msg {
		data, err := service.JobSnapshot(ctx, request)
		return result{id, data, err}
	}
}
