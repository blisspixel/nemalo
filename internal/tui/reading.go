package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/content"
)

// Session delivery references are navigation aids, never consumption records.
type readerState struct {
	asset                   string
	next                    string
	boundCatalog, boundRoot string
	last                    content.Request
	hasLast                 bool
}

func (m *model) startRead(units bool, unit int, cursor string, repeat bool) tea.Cmd {
	if m.busy {
		return nil
	}
	if m.reader.asset == "" || m.input.Value() == "" || m.secondary.Value() == "" {
		m.status = "Select an asset in Holdings with r and enter its explicit root."
		m.resize()
		return nil
	}
	repeatScopeChanged := !m.reader.hasLast || m.reader.last.Catalog != m.input.Value() || m.reader.last.Root != m.secondary.Value()
	nextScopeChanged := m.reader.boundCatalog != m.input.Value() || m.reader.boundRoot != m.secondary.Value()
	if (repeat && repeatScopeChanged) || (cursor != "" && nextScopeChanged) {
		m.status = "Inputs changed. Enter lists units for this root before continuing."
		m.resize()
		return nil
	}
	req := content.Request{
		SchemaVersion: 1, Catalog: m.input.Value(), Root: m.secondary.Value(),
		AssetID: m.reader.asset, Representation: "epub-structure/1",
		UnitsOnly: units, Unit: unit, Cursor: cursor, MaxBytes: 4096,
	}
	if repeat {
		req = m.reader.last
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.id++
	id, service := m.id, m.service
	m.reader.last, m.reader.hasLast = req, true
	m.busy = true
	m.status = "Retrieving source bytes. Escape cancels; no progress is recorded."
	m.setReport("")
	m.populate(nil)
	m.resize()
	return func() tea.Msg {
		data, err := service.Content(ctx, req)
		return result{id: id, data: data, err: err}
	}
}
